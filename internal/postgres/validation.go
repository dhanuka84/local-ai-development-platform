package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/contracts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) RecordKnowledgeValidation(ctx context.Context, report domain.KnowledgeValidation) (domain.KnowledgeValidation, error) {
	if err := report.Check(); err != nil {
		return report, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return report, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := scanKnowledge(tx.QueryRow(ctx, `SELECT `+knowledgeColumns+` FROM knowledge_items WHERE id::text=$1 FOR UPDATE`, report.KnowledgeID))
	if err != nil {
		return report, err
	}
	if item.ProjectID != report.ProjectID || item.Version != report.CandidateVersion || domain.Digest([]byte(item.Content)) != report.ContentSHA256 || (item.Status != domain.CandidatePending && item.Status != domain.CandidateApproved) {
		return report, domain.ErrVersionConflict
	}
	if humanErr := requireDatabaseHumanRole(ctx, tx, report.ValidatedBy, item.ProjectID, "qa"); humanErr != nil {
		var executor bool
		if report.Method != "workpacket" {
			return report, humanErr
		}
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals p JOIN principal_role_bindings b ON b.principal_id=p.id WHERE p.id=$1 AND p.active AND p.kind='workload' AND b.project_id IN('*',$2) AND b.role='validation_executor' AND b.valid_from<=now() AND (b.valid_until IS NULL OR b.valid_until>now()))`, report.ValidatedBy, item.ProjectID).Scan(&executor)
		if err != nil {
			return report, err
		}
		if !executor {
			return report, humanErr
		}
	}
	if err := requireKnowledgeRoleSeparation(ctx, tx, item, report.ValidatedBy, "", "qa"); err != nil {
		return report, err
	}
	if report.CompletedAt.After(time.Now().Add(time.Second)) {
		return report, domain.ErrValidationRequired
	}
	for _, artifact := range []domain.Artifact{report.SourceArtifact, report.ReportArtifact} {
		if _, err := tx.Exec(ctx, `INSERT INTO artifacts(sha256,uri,media_type,size_bytes) VALUES($1,$2,$3,$4) ON CONFLICT (sha256) DO NOTHING`, artifact.SHA256, artifact.URI, artifact.MediaType, artifact.SizeBytes); err != nil {
			return report, err
		}
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return report, err
	}
	if err := contracts.Validate("knowledge/v1/validation-report.schema.json", payload); err != nil {
		return report, fmt.Errorf("%w: %v", domain.ErrValidationRequired, err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO knowledge_validations(id,knowledge_id,project_id,candidate_version,content_sha256,
		source_manifest_sha256,report_artifact_sha256,method,verdict,validated_by,started_at,completed_at,valid_until,report)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		report.ID, report.KnowledgeID, report.ProjectID, report.CandidateVersion, report.ContentSHA256,
		report.SourceManifestSHA256, report.ReportArtifact.SHA256, report.Method, report.Verdict, report.ValidatedBy,
		report.StartedAt, report.CompletedAt, report.ValidUntil, payload)
	if err != nil {
		return report, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO knowledge_sources(validation_id,manifest_sha256,verified_at,valid,reason) VALUES($1,$2,$3,true,'verified')`, report.ID, report.SourceManifestSHA256, report.CompletedAt); err != nil {
		return report, err
	}
	if err := auditMutation(ctx, tx, "knowledge.validation", domain.OperationScope{ProjectID: item.ProjectID, WorkflowID: item.WorkflowID, TaskID: domain.ScopeFromContext(ctx).TaskID}, domain.EvidenceReference{Kind: "validation", ID: report.ID, Version: item.Version}, domain.EvidenceReference{Kind: "artifact", ID: report.ReportArtifact.SHA256, SHA256: report.ReportArtifact.SHA256}); err != nil {
		return report, err
	}
	return report, tx.Commit(ctx)
}

func (r *Repository) GetKnowledgeValidation(ctx context.Context, id string) (domain.KnowledgeValidation, error) {
	var report domain.KnowledgeValidation
	var payload []byte
	if err := r.pool.QueryRow(ctx, `SELECT report FROM knowledge_validations WHERE id::text=$1`, id).Scan(&payload); err != nil {
		return report, err
	}
	return report, json.Unmarshal(payload, &report)
}

func requireDatabaseHumanRole(ctx context.Context, tx pgx.Tx, actor, project, role string) error {
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals p JOIN principal_role_bindings b ON b.principal_id=p.id
		WHERE p.id=$1 AND p.active AND p.kind='human' AND b.project_id IN ('*',$2) AND b.role=$3
		AND b.valid_from<=now() AND (b.valid_until IS NULL OR b.valid_until>now()))`, actor, project, role).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("forbidden: active human role required")
	}
	return nil
}

func (r *Repository) DecideKnowledge(ctx context.Context, decision domain.KnowledgeDecision) (domain.KnowledgeItem, error) {
	var item domain.KnowledgeItem
	if decision.ExpectedVersion < 1 || decision.Actor == "" || decision.Reason == "" || decision.IdempotencyKey == "" || decision.RequestSHA256 == "" ||
		(decision.Decision != "approve" && decision.Decision != "reject") || !decision.Authorization.Allowed {
		return item, domain.ErrValidationRequired
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return item, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err = scanKnowledge(tx.QueryRow(ctx, `SELECT `+knowledgeColumns+` FROM knowledge_items WHERE id::text=$1 FOR UPDATE`, decision.KnowledgeID))
	if err != nil {
		return item, err
	}
	if err := requireDatabaseHumanRole(ctx, tx, decision.Actor, item.ProjectID, "product_owner"); err != nil {
		return item, err
	}
	var priorHash string
	err = tx.QueryRow(ctx, `SELECT request_sha256 FROM knowledge_decisions WHERE knowledge_id=$1 AND idempotency_key=$2`, item.ID, decision.IdempotencyKey).Scan(&priorHash)
	if err == nil {
		if priorHash != decision.RequestSHA256 {
			return item, domain.ErrVersionConflict
		}
		return item, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return item, err
	}
	if (item.Status != domain.CandidatePending && !(item.Status == domain.CandidateApproved && decision.Decision == "approve")) || item.Version != decision.ExpectedVersion {
		return item, domain.ErrVersionConflict
	}
	if decision.Decision == "approve" {
		taskType := item.TaskType
		var linkedType string
		lookupErr := tx.QueryRow(ctx, `SELECT task_type FROM workflow_task_checkpoints WHERE candidate_id=$1`, item.ID).Scan(&linkedType)
		if lookupErr == nil {
			taskType = linkedType
		} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return item, lookupErr
		}
		purpose := domain.TaskPurpose(taskType)
		var validator string
		if err := tx.QueryRow(ctx, `SELECT validated_by FROM knowledge_validations WHERE id::text=$1`, decision.ValidationID).Scan(&validator); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return item, domain.ErrValidationRequired
			}
			return item, err
		}
		if err := requireKnowledgeRoleSeparation(ctx, tx, item, decision.Actor, validator, "product_approval"); err != nil {
			return item, err
		}
		var eligible bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_validations v JOIN knowledge_sources s ON s.validation_id=v.id
			JOIN LATERAL knowledge_quality_policy(v.project_id,'software_knowledge',$6) p ON true
			WHERE v.id::text=$1 AND v.knowledge_id=$2 AND v.project_id=$3 AND v.candidate_version=$4
			AND v.content_sha256=$5 AND v.verdict='pass' AND v.completed_at<=now() AND v.valid_until>now()
			AND (p.required_method='any' OR p.required_method=v.method)
			AND v.completed_at+CASE WHEN v.method='workpacket' THEN p.patch_max_age ELSE p.content_max_age END>now()
			AND s.valid AND s.manifest_sha256=v.source_manifest_sha256 AND s.verified_at<=now() AND s.verified_at+p.source_max_age>now())`,
			decision.ValidationID, item.ID, item.ProjectID, item.Version, domain.Digest([]byte(item.Content)), purpose).Scan(&eligible)
		if err != nil {
			return item, err
		}
		if !eligible {
			return item, domain.ErrValidationRequired
		}
		if item.WorkflowID != "" {
			// Lock both the workflow and its candidate task before accepting a
			// managed gate, so a concurrent rejection cannot change the premise.
			run, err := getWorkflowByID(ctx, tx, item.WorkflowID, true)
			if err != nil {
				return item, err
			}
			var taskState string
			taskErr := tx.QueryRow(ctx, `SELECT state FROM workflow_task_checkpoints
				WHERE candidate_id=$1 AND workflow_id=$2 FOR UPDATE`, item.ID, item.WorkflowID).Scan(&taskState)
			if taskErr != nil && !errors.Is(taskErr, pgx.ErrNoRows) {
				return item, taskErr
			}
			taskValidated := taskErr == nil && taskState == domain.TaskStatePromotionRequired
			if !taskValidated && !(run.State == "promotion_pending" && run.QAValidatedBy != "") {
				return item, fmt.Errorf("%w: workflow QA gate is not ready", domain.ErrValidationRequired)
			}
		}
	}
	authorization, err := json.Marshal(decision.Authorization)
	if err != nil {
		return item, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO knowledge_decisions(id,knowledge_id,candidate_version,validation_id,decision,reason,actor,idempotency_key,request_sha256,policy_decision)
		VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10)`, decision.ID, item.ID, item.Version, decision.ValidationID,
		decision.Decision, decision.Reason, decision.Actor, decision.IdempotencyKey, decision.RequestSHA256, authorization)
	if err != nil {
		return item, err
	}
	status := domain.CandidateRejected
	if decision.Decision == "approve" {
		status = domain.CandidateApproved
	}
	item, err = scanKnowledge(tx.QueryRow(ctx, `UPDATE knowledge_items SET status=$2,
		approved_at=CASE WHEN $2='approved' THEN now() ELSE NULL END,
		approved_by=CASE WHEN $2='approved' THEN $3 ELSE NULL END,
		approval_validation_id=CASE WHEN $2='approved' THEN NULLIF($4,'')::uuid ELSE NULL END
		WHERE id=$1 RETURNING `+knowledgeColumns, item.ID, status, decision.Actor, decision.ValidationID))
	if err != nil {
		return item, err
	}
	if status == domain.CandidateApproved {
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic) VALUES($1,'knowledge.upsert')`, item.ID); err != nil {
			return item, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO review_records(id,knowledge_id,reviewer,verdict,comments) VALUES($1,$2,$3,$4,$5)`, decision.ID, item.ID, decision.Actor, decision.Decision, decision.Reason); err != nil {
		return item, err
	}
	if err := auditMutation(ctx, tx, "knowledge."+decision.Decision, domain.OperationScope{ProjectID: item.ProjectID, WorkflowID: item.WorkflowID}, domain.EvidenceReference{Kind: "decision", ID: decision.ID, Version: item.Version}, domain.EvidenceReference{Kind: "validation", ID: decision.ValidationID, Version: item.Version}); err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}

func requireKnowledgeRoleSeparation(ctx context.Context, tx pgx.Tx, item domain.KnowledgeItem, actor, validator, gate string) error {
	policy := domain.GovernancePolicy{Profile: domain.GovernanceSolo, AllowRoleOverlap: true}
	err := tx.QueryRow(ctx, `SELECT profile,allow_role_overlap,distinct_principal_gates FROM project_governance_policies WHERE project_id=$1 FOR SHARE`, item.ProjectID).Scan(&policy.Profile, &policy.AllowRoleOverlap, &policy.DistinctPrincipalGates)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if policy.RequiresDistinctPrincipal(gate) || !policy.AllowRoleOverlap {
		if validator != "" && validator == actor {
			return errors.New("forbidden: governance requires a distinct product approver")
		}
		if item.WorkflowID != "" {
			run, err := getWorkflowByID(ctx, tx, item.WorkflowID, false)
			if err != nil {
				return err
			}
			if run.ImplementedBy == actor || (gate == "product_approval" && run.QAValidatedBy == actor) {
				return errors.New("forbidden: governance requires distinct workflow actors")
			}
		}
	}
	return nil
}
