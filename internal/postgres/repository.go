package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Repository, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Pool() *pgxpool.Pool            { return r.pool }
func (r *Repository) Close()                         { r.pool.Close() }
func (r *Repository) Ping(ctx context.Context) error { return r.pool.Ping(ctx) }

func (r *Repository) RecordGeneration(ctx context.Context, capture domain.GenerationCapture) (domain.KnowledgeItem, error) {
	capture.Tags = nonNilStrings(capture.Tags)
	capture.Procedure = nonNilStrings(capture.Procedure)
	capture.ValidationEvidence = nonNilStrings(capture.ValidationEvidence)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.KnowledgeItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `INSERT INTO projects(id, display_name) VALUES($1,$1) ON CONFLICT (id) DO NOTHING`, capture.ProjectID); err != nil {
		return domain.KnowledgeItem{}, fmt.Errorf("ensure project: %w", err)
	}
	for _, artifact := range []domain.Artifact{capture.PromptArtifact, capture.OutputArtifact} {
		if _, err := tx.Exec(ctx, `INSERT INTO artifacts(sha256,uri,media_type,size_bytes)
            VALUES($1,$2,$3,$4) ON CONFLICT (sha256) DO NOTHING`, artifact.SHA256, artifact.URI, artifact.MediaType, artifact.SizeBytes); err != nil {
			return domain.KnowledgeItem{}, fmt.Errorf("record artifact: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO generations(
		id,project_id,session_id,task_type,provider,model,repository_revision,outcome,procedure,validation_evidence,
		prompt_artifact_sha256,output_artifact_sha256,workflow_id,workflow_step_id
	) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,'')::uuid,NULLIF($14,'')::uuid)`, capture.ID, capture.ProjectID, capture.SessionID,
		valueOr(capture.TaskType, "software-development"), capture.Provider, capture.Model, capture.RepositoryRevision,
		valueOr(capture.Outcome, "unknown"), capture.Procedure, capture.ValidationEvidence,
		capture.PromptArtifact.SHA256, capture.OutputArtifact.SHA256, capture.WorkflowID, capture.WorkflowStepID); err != nil {
		return domain.KnowledgeItem{}, fmt.Errorf("record generation: %w", err)
	}

	status := domain.CandidatePending
	actor := ""
	if capture.AutoApprove {
		status, actor = domain.CandidateApproved, "local-auto-approval"
	}
	title := valueOr(capture.Summary, truncate(capture.Prompt, 160))
	var item domain.KnowledgeItem
	var approvedAt *time.Time
	err = tx.QueryRow(ctx, `INSERT INTO knowledge_items(
		project_id,title,problem,summary,content,procedure,validation_evidence,task_type,language,tags,status,source_generation_id,approved_at,approved_by,workflow_id,workflow_step_id
	) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,CASE WHEN $11='approved' THEN now() END,NULLIF($13,''),NULLIF($14,'')::uuid,NULLIF($15,'')::uuid)
	RETURNING id::text,project_id,title,problem,summary,content,procedure,validation_evidence,task_type,language,tags,status,
		      source_generation_id::text,COALESCE(workflow_id::text,''),COALESCE(workflow_step_id::text,''),version,created_at,approved_at`,
		capture.ProjectID, title, capture.Prompt, capture.Summary, capture.Response, capture.Procedure, capture.ValidationEvidence,
		valueOr(capture.TaskType, "software-development"), capture.Language, capture.Tags, status, capture.ID, actor,
		capture.WorkflowID, capture.WorkflowStepID,
	).Scan(&item.ID, &item.ProjectID, &item.Title, &item.Problem, &item.Summary, &item.Content, &item.Procedure,
		&item.ValidationEvidence, &item.TaskType, &item.Language, &item.Tags, &item.Status, &item.SourceGenerationID,
		&item.WorkflowID, &item.WorkflowStepID, &item.Version, &item.CreatedAt, &approvedAt)
	if err != nil {
		return domain.KnowledgeItem{}, fmt.Errorf("create knowledge candidate: %w", err)
	}
	if approvedAt != nil {
		item.ApprovedAt = *approvedAt
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic) VALUES($1,'knowledge.upsert')`, item.ID); err != nil {
			return domain.KnowledgeItem{}, fmt.Errorf("enqueue knowledge index: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.KnowledgeItem{}, err
	}
	return item, nil
}

const knowledgeColumns = `id::text,project_id,title,problem,summary,content,procedure,validation_evidence,task_type,language,tags,status,
	COALESCE(source_generation_id::text,''),COALESCE(workflow_id::text,''),COALESCE(workflow_step_id::text,''),version,created_at,approved_at`

func scanKnowledge(row pgx.Row) (domain.KnowledgeItem, error) {
	var item domain.KnowledgeItem
	var approvedAt *time.Time
	err := row.Scan(&item.ID, &item.ProjectID, &item.Title, &item.Problem, &item.Summary, &item.Content,
		&item.Procedure, &item.ValidationEvidence, &item.TaskType, &item.Language, &item.Tags, &item.Status,
		&item.SourceGenerationID, &item.WorkflowID, &item.WorkflowStepID, &item.Version, &item.CreatedAt, &approvedAt)
	if approvedAt != nil {
		item.ApprovedAt = *approvedAt
	}
	return item, err
}

func (r *Repository) GetKnowledge(ctx context.Context, id string, includePending bool) (domain.KnowledgeItem, error) {
	query := `SELECT ` + knowledgeColumns + ` FROM knowledge_items WHERE id::text=$1`
	if !includePending {
		query += ` AND status='approved'`
	}
	item, err := scanKnowledge(r.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, fmt.Errorf("knowledge item %q not found", id)
	}
	return item, err
}

func (r *Repository) GetKnowledgeMany(ctx context.Context, ids []string) ([]domain.KnowledgeItem, error) {
	if len(ids) == 0 {
		return []domain.KnowledgeItem{}, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+knowledgeColumns+` FROM knowledge_items
        WHERE id::text=ANY($1) AND status='approved'`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.KnowledgeItem, 0, len(ids))
	for rows.Next() {
		item, err := scanKnowledge(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) SearchApprovedLexical(ctx context.Context, projectID, query string, limit int) ([]domain.SearchHit, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+knowledgeColumns+`,
        ts_rank_cd(search_document, websearch_to_tsquery('simple',$2)) AS score
      FROM knowledge_items
      WHERE project_id=$1 AND status='approved'
        AND (search_document @@ websearch_to_tsquery('simple',$2)
             OR title ILIKE '%' || $2 || '%' OR summary ILIKE '%' || $2 || '%')
      ORDER BY score DESC, approved_at DESC NULLS LAST LIMIT $3`, projectID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.SearchHit, 0, limit)
	for rows.Next() {
		var hit domain.SearchHit
		var approvedAt *time.Time
		if err := rows.Scan(&hit.ID, &hit.ProjectID, &hit.Title, &hit.Problem, &hit.Summary, &hit.Content,
			&hit.Procedure, &hit.ValidationEvidence, &hit.TaskType, &hit.Language, &hit.Tags, &hit.Status,
			&hit.SourceGenerationID, &hit.WorkflowID, &hit.WorkflowStepID, &hit.Version, &hit.CreatedAt, &approvedAt, &hit.Score); err != nil {
			return nil, err
		}
		if approvedAt != nil {
			hit.ApprovedAt = *approvedAt
		}
		result = append(result, hit)
	}
	return result, rows.Err()
}

func (r *Repository) ListCandidates(ctx context.Context, projectID string, limit int) ([]domain.KnowledgeItem, error) {
	query := `SELECT ` + knowledgeColumns + ` FROM knowledge_items WHERE status='pending'`
	args := []any{limit}
	if projectID != "" {
		query += ` AND project_id=$2`
		args = append(args, projectID)
	}
	query += ` ORDER BY created_at ASC LIMIT $1`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.KnowledgeItem, 0, limit)
	for rows.Next() {
		item, err := scanKnowledge(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) ApproveCandidate(ctx context.Context, id, actor string) (domain.KnowledgeItem, error) {
	return r.decide(ctx, id, actor, domain.CandidateApproved)
}

func (r *Repository) RejectCandidate(ctx context.Context, id, actor string) (domain.KnowledgeItem, error) {
	return r.decide(ctx, id, actor, domain.CandidateRejected)
}

func (r *Repository) decide(ctx context.Context, id, actor, status string) (domain.KnowledgeItem, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.KnowledgeItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := scanKnowledge(tx.QueryRow(ctx, `UPDATE knowledge_items SET status=$2,
        approved_at=CASE WHEN $2='approved' THEN now() ELSE NULL END,
        approved_by=CASE WHEN $2='approved' THEN $3 ELSE NULL END
      WHERE id::text=$1 AND status='pending' RETURNING `+knowledgeColumns, id, status, actor))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, fmt.Errorf("pending knowledge candidate %q not found", id)
	}
	if err != nil {
		return item, err
	}
	if status == domain.CandidateApproved {
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic) VALUES($1,'knowledge.upsert')`, id); err != nil {
			return item, err
		}
	}
	reviewID, err := domain.NewID()
	if err != nil {
		return item, err
	}
	verdict := "approve"
	if status == domain.CandidateRejected {
		verdict = "reject"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO review_records(id,knowledge_id,reviewer,verdict,comments)
        VALUES($1,$2,$3,$4,'candidate decision')`, reviewID, id, actor, verdict); err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}

func (r *Repository) RecordReview(ctx context.Context, review domain.ReviewRecord) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, artifact := range []domain.Artifact{review.ReviewArtifact, review.ContextManifestArtifact} {
		if artifact.SHA256 == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO artifacts(sha256,uri,media_type,size_bytes)
            VALUES($1,$2,$3,$4) ON CONFLICT (sha256) DO NOTHING`, artifact.SHA256, artifact.URI, artifact.MediaType, artifact.SizeBytes); err != nil {
			return fmt.Errorf("record review artifact: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO review_records(
		id,knowledge_id,reviewer,provider,model,verdict,comments,improved_content,
		review_artifact_sha256,context_manifest_artifact_sha256,workflow_id,workflow_step_id
	  ) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,'')::uuid,NULLIF($12,'')::uuid)`, review.ID, review.KnowledgeID, review.Reviewer,
		review.Provider, review.Model, review.Verdict, review.Comments, review.ImprovedContent,
		review.ReviewArtifact.SHA256, review.ContextManifestArtifact.SHA256, review.WorkflowID, review.WorkflowStepID); err != nil {
		return err
	}
	if review.Verdict == "revise" && review.ImprovedContent != "" {
		result, err := tx.Exec(ctx, `UPDATE knowledge_items
			SET content=$2,validation_evidence=$3,version=version+1
			WHERE id::text=$1 AND status='pending'`, review.KnowledgeID, review.ImprovedContent, nonNilStrings(review.ValidationEvidence))
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("pending knowledge candidate %q not found for revision", review.KnowledgeID)
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) ClaimOutbox(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	rows, err := r.pool.Query(ctx, `WITH picked AS (
        SELECT id FROM outbox_events
        WHERE completed_at IS NULL AND next_attempt_at <= now()
          AND (locked_at IS NULL OR locked_at < now() - interval '5 minutes')
        ORDER BY id FOR UPDATE SKIP LOCKED LIMIT $1
      )
      UPDATE outbox_events e SET locked_at=now(), attempts=e.attempts+1
      FROM picked WHERE e.id=picked.id
      RETURNING e.id,e.aggregate_id::text,e.topic,e.attempts`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.OutboxEvent, 0, limit)
	for rows.Next() {
		var event domain.OutboxEvent
		if err := rows.Scan(&event.ID, &event.AggregateID, &event.Topic, &event.Attempts); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (r *Repository) CompleteOutbox(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox_events SET completed_at=now(),locked_at=NULL,last_error='' WHERE id=$1`, id)
	return err
}

func (r *Repository) FailOutbox(ctx context.Context, id int64, message string) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox_events SET locked_at=NULL,last_error=$2,
        next_attempt_at=now() + least(attempts,10) * interval '5 seconds' WHERE id=$1`, id, truncate(message, 2000))
	return err
}

func (r *Repository) RequeueApprovedKnowledge(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic)
        SELECT id,'knowledge.upsert' FROM knowledge_items WHERE status='approved'`)
	return result.RowsAffected(), err
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
