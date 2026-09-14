package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) GetProductEvaluation(ctx context.Context, project, id string) (out domain.ObservationEvaluation, err error) {
	var raw []byte
	err = r.pool.QueryRow(ctx, `SELECT evaluation FROM product_evaluations WHERE project_id=$1 AND id::text=$2`, project, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.ErrQualityBlocked
	}
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return
}

func (r *Repository) RecordProductEvaluation(ctx context.Context, in domain.ObservationEvaluation) (out domain.ObservationEvaluation, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireExecutionSourceActor(ctx, tx, in.Actor, in.ProjectID, in.ProductID, ""); err != nil {
		return out, err
	}
	if in.Method != "hybrid-ai/observation-reconciliation/v1" || (in.Outcome != "satisfied" && in.Outcome != "violated" && in.Outcome != "inconclusive") {
		return out, domain.ErrValidationRequired
	}
	for _, binding := range []domain.ProductBinding{in.Intent, in.Left, in.Right} {
		var matches bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_records WHERE id::text=$1 AND project_id=$2 AND product_id=$3 AND sha256=$4 AND kind=$5 AND product_record_eligible(id))`, binding.RecordID, in.ProjectID, in.ProductID, binding.SHA256, binding.Kind).Scan(&matches); err != nil {
			return out, err
		}
		if !matches {
			return out, domain.ErrQualityBlocked
		}
	}
	if err = saveProductArtifact(ctx, tx, in.Evidence); err != nil {
		return out, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO product_evaluations(id,project_id,actor,request_sha256,evaluation) VALUES($1,$2,$3,$4,$5) ON CONFLICT(project_id,actor,request_sha256) DO NOTHING`, in.ID, in.ProjectID, in.Actor, in.RequestSHA256, raw)
	if err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, `SELECT evaluation FROM product_evaluations WHERE project_id=$1 AND actor=$2 AND request_sha256=$3`, in.ProjectID, in.Actor, in.RequestSHA256).Scan(&raw); err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if err = auditMutation(ctx, tx, "product.observations.evaluate", domain.ScopeFromContext(ctx), domain.EvidenceReference{Kind: "product_evaluation", ID: out.ID, SHA256: out.RequestSHA256}, domain.EvidenceReference{Kind: "artifact", ID: out.Evidence.SHA256, SHA256: out.Evidence.SHA256}); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
