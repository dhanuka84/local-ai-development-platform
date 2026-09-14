package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) RecordPackageEvaluation(ctx context.Context, in domain.PackageEvaluation) (domain.PackageEvaluation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return in, err
	}
	defer tx.Rollback(ctx)
	if err = executionActor(ctx, tx, in.Actor, in.ProjectID, "qa", true); err != nil {
		return in, err
	}
	if err = executionActor(ctx, tx, in.Actor, in.ProjectID, "operations", true); err != nil {
		return in, err
	}
	if err = saveProductArtifact(ctx, tx, in.Evidence); err != nil {
		return in, err
	}
	raw, _ := json.Marshal(in)
	if _, err = tx.Exec(ctx, `INSERT INTO sdlc_package_evaluations(id,project_id,package_id,package_sha256,actor,record) VALUES($1,$2,$3,$4,$5,$6)`, in.ID, in.ProjectID, in.PackageID, in.PackageSHA256, in.Actor, raw); err != nil {
		return in, err
	}
	if err = auditMutation(ctx, tx, "sdlc.package.evaluate", domain.OperationScope{ProjectID: in.ProjectID}, domain.EvidenceReference{Kind: "package_evaluation", ID: in.ID, SHA256: in.Evidence.SHA256}); err != nil {
		return in, err
	}
	return in, tx.Commit(ctx)
}
func (r *Repository) GetPackageEvaluation(ctx context.Context, project, id string) (out domain.PackageEvaluation, err error) {
	var raw []byte
	err = r.pool.QueryRow(ctx, `SELECT record FROM sdlc_package_evaluations WHERE project_id=$1 AND id::text=$2`, project, id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return
}
func (r *Repository) GetPackageActivation(ctx context.Context, project, target, role string) (out domain.PackageActivation, err error) {
	var raw []byte
	err = r.pool.QueryRow(ctx, `SELECT record FROM sdlc_package_activations WHERE project_id=$1 AND target_id=$2 AND role=$3`, project, target, role).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return
}
func (r *Repository) ActivatePackage(ctx context.Context, in domain.PackageActivation) (domain.PackageActivation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return in, err
	}
	defer tx.Rollback(ctx)
	if err = executionActor(ctx, tx, in.Actor, in.ProjectID, "qa", true); err != nil {
		return in, err
	}
	if err = executionActor(ctx, tx, in.Actor, in.ProjectID, "operations", true); err != nil {
		return in, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, in.ProjectID+":"+in.TargetID+":"+in.Role); err != nil {
		return in, err
	}
	var current domain.PackageActivation
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT record FROM sdlc_package_activations WHERE project_id=$1 AND target_id=$2 AND role=$3 FOR UPDATE`, in.ProjectID, in.TargetID, in.Role).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return in, err
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &current); err != nil {
			return in, err
		}
	}
	if current.PackageSHA256 != in.PreviousSHA256 {
		return in, domain.ErrVersionConflict
	}
	var evaluation domain.PackageEvaluation
	err = tx.QueryRow(ctx, `SELECT record FROM sdlc_package_evaluations WHERE project_id=$1 AND id::text=$2`, in.ProjectID, in.EvaluationID).Scan(&raw)
	if err != nil {
		return in, err
	}
	if json.Unmarshal(raw, &evaluation) != nil || evaluation.Outcome != "passed" || evaluation.PackageSHA256 != in.PackageSHA256 || evaluation.Role != in.Role {
		return in, domain.ErrValidationRequired
	}
	if in.Action == "rollback" {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sdlc_package_decisions WHERE project_id=$1 AND target_id=$2 AND role=$3 AND package_sha256=$4)`, in.ProjectID, in.TargetID, in.Role, in.PackageSHA256).Scan(&exists); err != nil {
			return in, err
		}
		if !exists || current.PackageSHA256 == in.PackageSHA256 {
			return in, domain.ErrValidationRequired
		}
	}
	in.Version = current.Version + 1
	raw, _ = json.Marshal(in)
	if _, err = tx.Exec(ctx, `INSERT INTO sdlc_package_activations(project_id,target_id,role,package_sha256,evaluation_id,record) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(project_id,target_id,role) DO UPDATE SET package_sha256=$4,evaluation_id=$5,record=$6`, in.ProjectID, in.TargetID, in.Role, in.PackageSHA256, in.EvaluationID, raw); err != nil {
		return in, err
	}
	id, err := domain.NewID()
	if err != nil {
		return in, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sdlc_package_decisions VALUES($1,$2,$3,$4,$5,$6,$7)`, id, in.ProjectID, in.TargetID, in.Role, in.PackageSHA256, in.Actor, raw); err != nil {
		return in, err
	}
	if err = auditMutation(ctx, tx, "sdlc.package."+in.Action, domain.OperationScope{ProjectID: in.ProjectID}, domain.EvidenceReference{Kind: "agent_package", ID: in.PackageID, SHA256: in.PackageSHA256}, domain.EvidenceReference{Kind: "package_evaluation", ID: in.EvaluationID}); err != nil {
		return in, err
	}
	return in, tx.Commit(ctx)
}
func lockActivePackages(ctx context.Context, tx pgx.Tx, run domain.ExecutionRun) error {
	if run.Target.Qualification {
		if run.Target.Environment != "disposable" {
			return domain.ErrForbidden
		}
		return nil
	}
	roles := []string{"sdlc_builder", "sdlc_evaluator", "sdlc_delivery"}
	if run.Kind == "incident" {
		roles = []string{"sdlc_diagnosis", "sdlc_evaluator", "sdlc_remediation"}
	}
	for _, role := range roles {
		var sha string
		err := tx.QueryRow(ctx, `SELECT package_sha256 FROM sdlc_package_activations WHERE project_id=$1 AND target_id=$2 AND role=$3 FOR SHARE`, run.ProjectID, run.Target.ID, role).Scan(&sha)
		if err != nil {
			return domain.ErrValidationRequired
		}
		if sha != run.Packages[role].Digest() {
			return domain.ErrVersionConflict
		}
	}
	return nil
}
