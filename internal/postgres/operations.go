package postgres

import (
	"context"
	"errors"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) FailedKnowledgeIndexes(ctx context.Context, project string) ([]domain.FailedIndex, error) {
	rows, err := r.pool.Query(ctx, `SELECT o.id,o.aggregate_id::text,o.attempts,o.failed_at FROM outbox_events o JOIN knowledge_items k ON k.id=o.aggregate_id WHERE k.project_id=$1 AND o.topic='knowledge.upsert' AND o.failed_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM outbox_retry_decisions d WHERE d.event_id=o.id) ORDER BY o.id LIMIT 100`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.FailedIndex{}
	for rows.Next() {
		var v domain.FailedIndex
		if err = rows.Scan(&v.EventID, &v.KnowledgeID, &v.Attempts, &v.FailedAt); err != nil {
			return nil, err
		}
		v.Recovery = "Resolve source/validation/model/vector failure, then explicitly retry this exact event. Old evidence remains immutable."
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) RetryKnowledgeIndex(ctx context.Context, in domain.RetryIndexInput) (int64, error) {
	if in.EventID < 1 || in.ExpectedAttempts < 1 || in.Reason == "" || in.IdempotencyKey == "" {
		return 0, domain.ErrValidationRequired
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireDatabaseHumanRole(ctx, tx, in.Actor, in.ProjectID, "operations"); err != nil {
		return 0, err
	}
	var knowledge string
	var attempts int
	err = tx.QueryRow(ctx, `SELECT o.aggregate_id::text,o.attempts FROM outbox_events o JOIN knowledge_items k ON k.id=o.aggregate_id WHERE o.id=$1 AND k.project_id=$2 AND o.topic='knowledge.upsert' AND o.failed_at IS NOT NULL FOR UPDATE OF o`, in.EventID, in.ProjectID).Scan(&knowledge, &attempts)
	if err != nil {
		return 0, err
	}
	var replacement int64
	var actor, reason string
	err = tx.QueryRow(ctx, `SELECT replacement_event_id,actor,reason FROM outbox_retry_decisions WHERE event_id=$1 AND idempotency_key=$2`, in.EventID, in.IdempotencyKey).Scan(&replacement, &actor, &reason)
	if err == nil {
		if actor != in.Actor || reason != in.Reason || attempts != in.ExpectedAttempts {
			return 0, domain.ErrVersionConflict
		}
		return replacement, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if attempts != in.ExpectedAttempts {
		return 0, domain.ErrVersionConflict
	}
	var eligible bool
	if err = tx.QueryRow(ctx, `SELECT knowledge_eligible($1::uuid)`, knowledge).Scan(&eligible); err != nil {
		return 0, err
	}
	if !eligible {
		return 0, domain.ErrQualityBlocked
	}
	// A failed intent can have only one recovery intent, irrespective of key.
	var retried bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM outbox_retry_decisions WHERE event_id=$1)`, in.EventID).Scan(&retried); err != nil {
		return 0, err
	}
	if retried {
		return 0, domain.ErrVersionConflict
	}
	if err = tx.QueryRow(ctx, `INSERT INTO outbox_events(aggregate_id,topic) VALUES($1,'knowledge.upsert') RETURNING id`, knowledge).Scan(&replacement); err != nil {
		return 0, err
	}
	id, err := domain.NewID()
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbox_retry_decisions(id,event_id,replacement_event_id,actor,reason,idempotency_key) VALUES($1,$2,$3,$4,$5,$6)`, id, in.EventID, replacement, in.Actor, in.Reason, in.IdempotencyKey); err != nil {
		return 0, err
	}
	if err = auditMutation(ctx, tx, "index.retry", domain.OperationScope{ProjectID: in.ProjectID}, domain.EvidenceReference{Kind: "retry_decision", ID: id}, domain.EvidenceReference{Kind: "knowledge", ID: knowledge}); err != nil {
		return 0, err
	}
	return replacement, tx.Commit(ctx)
}
func (r *Repository) EvidenceHealth(ctx context.Context, project string, retentionDays int) (v domain.EvidenceHealth, err error) {
	if retentionDays < 1 || retentionDays > 3650 {
		return v, domain.ErrValidationRequired
	}
	v.ProjectID = project
	v.RetentionDays = retentionDays
	v.Policy = "Immutable evidence retained; records older than policy require operator retention review, never automatic deletion. Missing outcomes older than five minutes alert as unknown, not success."
	err = r.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE o.phase='intent' AND o.recorded_at<now()-interval '5 minutes' AND NOT EXISTS(SELECT 1 FROM operation_records result WHERE result.operation_id=o.operation_id AND result.phase IN('outcome','reconciliation'))),count(*) FILTER(WHERE q.exported_at IS NULL AND q.attempts>=3),count(*) FILTER(WHERE q.exported_at IS NULL),count(*) FILTER(WHERE o.recorded_at<now()-$2*interval '1 day') FROM operation_records o JOIN trace_export_queue q ON q.record_id=o.id WHERE o.project_id=$1`, project, retentionDays).Scan(&v.UnknownOutcomes, &v.FailedExports, &v.Unexported, &v.RetentionReviewDue)
	return
}
