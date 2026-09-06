package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
	"github.com/jackc/pgx/v5"
)

func appendOperation(ctx context.Context, tx pgx.Tx, record domain.OperationRecord) error {
	if record.ProjectID == "" {
		return fmt.Errorf("%w: operation scope missing", domain.ErrEvidenceUnavailable)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO operation_records(id,operation_id,project_id,workflow_id,task_id,trace_id,span_id,phase,outcome,record,recorded_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, record.ID, record.OperationID, record.ProjectID, record.WorkflowID, record.TaskID, record.TraceID, record.SpanID, record.Phase, record.Outcome, data, record.RecordedAt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO trace_export_queue(record_id) VALUES($1)`, record.ID)
	return err
}
func (r *Repository) AppendOperation(ctx context.Context, record domain.OperationRecord) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := appendOperation(ctx, tx, record); err != nil {
		return fmt.Errorf("%w: operation persistence failed", domain.ErrEvidenceUnavailable)
	}
	return tx.Commit(ctx)
}
func auditMutation(ctx context.Context, tx pgx.Tx, name string, scope domain.OperationScope, refs ...domain.EvidenceReference) error {
	_, span, record := telemetry.Start(ctx, name, scope, refs)
	defer span.End()
	record.Phase = "commit"
	record.Outcome = "success"
	record.Rationale = "state_and_evidence_committed_atomically"
	return appendOperation(ctx, tx, record)
}
func (r *Repository) ReadWorkflowTrace(ctx context.Context, project, workflow string, limit int) (domain.WorkflowTrace, error) {
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	result := domain.WorkflowTrace{ProjectID: project, WorkflowID: workflow, Records: []domain.OperationRecord{}, Missing: []string{}, Coverage: "platform_owned_boundaries; external client actions require explicit evidence"}
	rows, err := r.pool.Query(ctx, `SELECT record FROM operation_records WHERE project_id=$1 AND workflow_id=$2 AND record->>'name'<>'mcp.workflow_trace_get' ORDER BY recorded_at,id LIMIT $3`, project, workflow, limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	intents := map[string]bool{}
	outcomes := map[string]bool{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return result, err
		}
		var record domain.OperationRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return result, err
		}
		result.Records = append(result.Records, record)
		if record.Phase == "intent" {
			intents[record.OperationID] = true
		}
		if record.Phase == "outcome" || record.Phase == "reconciliation" {
			outcomes[record.OperationID] = true
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	for id := range intents {
		if !outcomes[id] {
			result.Missing = append(result.Missing, "outcome:"+id)
		}
	}
	if len(result.Records) > limit {
		result.Records = result.Records[:limit]
		result.Missing = append(result.Missing, "record_limit")
	}
	if len(result.Records) == 0 {
		result.Missing = append(result.Missing, "owned_operation_evidence")
	}
	// A task checkpoint or provider name is not execution evidence. Every
	// completed task must link a trusted, locally executed validation receipt.
	rows.Close()
	var missingValidation int
	err = r.pool.QueryRow(ctx, `SELECT count(*) FROM workflow_task_checkpoints t JOIN workflow_runs w ON w.id=t.workflow_id
		WHERE w.project_id=$1 AND t.workflow_id::text=$2 AND t.state='completed' AND NOT EXISTS(
		SELECT 1 FROM operation_records o WHERE o.task_id=t.id::text AND o.project_id=$1 AND o.record->>'name'='local.verifier'
		AND o.phase='outcome' AND o.outcome='success')`, project, workflow).Scan(&missingValidation)
	if err != nil {
		return result, err
	}
	if missingValidation > 0 {
		result.Missing = append(result.Missing, "external_or_missing_task_execution")
	}
	result.Complete = len(result.Missing) == 0
	return result, nil
}
func (r *Repository) PendingTraceExports(ctx context.Context, limit int) ([]domain.OperationRecord, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	rows, err := r.pool.Query(ctx, `SELECT o.record FROM trace_export_queue q JOIN operation_records o ON o.id=q.record_id WHERE q.exported_at IS NULL AND q.next_attempt_at<=now() ORDER BY o.recorded_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.OperationRecord{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record domain.OperationRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}
func (r *Repository) FinishTraceExport(ctx context.Context, id string, success bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE trace_export_queue SET attempts=attempts+1,exported_at=CASE WHEN $2 THEN now() ELSE NULL END,
		next_attempt_at=now()+least(attempts+1,60)*interval '10 seconds' WHERE record_id::text=$1 AND exported_at IS NULL`, id, success)
	return err
}
