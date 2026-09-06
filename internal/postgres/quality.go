package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func (r *Repository) KnowledgeQuality(ctx context.Context, id string) (domain.KnowledgeQuality, error) {
	var result domain.KnowledgeQuality
	err := r.pool.QueryRow(ctx, qualitySQL(ctx, `SELECT k.id::text,k.project_id,k.version,COALESCE(k.approval_validation_id::text,''),
		COALESCE(p.version,0),COALESCE(p.owner,''),knowledge_eligible(k.id),knowledge_quality_reason(k.id),s.verified_at,v.completed_at,
		CASE WHEN x.version=k.version AND x.manifest IS NOT NULL AND x.manifest->>'source_manifest_sha256'=v.source_manifest_sha256::text AND x.verified_at<=now() AND x.verified_at+p.projection_max_age>now() THEN x.verified_at END
		FROM knowledge_items k LEFT JOIN knowledge_validations v ON v.id=k.approval_validation_id
		LEFT JOIN knowledge_sources s ON s.validation_id=v.id
		LEFT JOIN knowledge_projection_checks x ON x.knowledge_id=k.id
		LEFT JOIN LATERAL knowledge_quality_policy(k.project_id) p ON true WHERE k.id::text=$1`), id).Scan(
		&result.KnowledgeID, &result.ProjectID, &result.Version, &result.ValidationID, &result.PolicyVersion, &result.PolicyOwner,
		&result.Eligible, &result.Reason, &result.SourceVerifiedAt, &result.ContentValidatedAt, &result.ProjectionVerifiedAt)
	return result, err
}

// This is an internal receipt boundary: callers must actually verify the
// immutable manifest, not infer freshness from a heartbeat or index timestamp.
func (r *Repository) RecordSourceCheck(ctx context.Context, id string, version int, validationID string, valid bool, reason string) error {
	if valid {
		reason = "verified"
	} else if reason != "source_changed" && reason != "source_unavailable" && reason != "evidence_unavailable" {
		return fmt.Errorf("invalid source failure reason")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var currentVersion int
	var currentValidation string
	if err := tx.QueryRow(ctx, `SELECT version,COALESCE(approval_validation_id::text,'') FROM knowledge_items WHERE id::text=$1 FOR UPDATE`, id).Scan(&currentVersion, &currentValidation); err != nil {
		return err
	}
	if version != currentVersion || validationID != currentValidation {
		return domain.ErrVersionConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO knowledge_source_check_receipts(validation_id,valid,reason) VALUES($1,$2,$3)`, validationID, valid, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO knowledge_sources(validation_id,manifest_sha256,verified_at,valid,reason)
		SELECT id,source_manifest_sha256,CASE WHEN $2 THEN now() ELSE completed_at END,$2,$3 FROM knowledge_validations WHERE id::text=$1
		ON CONFLICT(validation_id) DO UPDATE SET verified_at=CASE WHEN EXCLUDED.valid THEN EXCLUDED.verified_at ELSE knowledge_sources.verified_at END,valid=EXCLUDED.valid,reason=EXCLUDED.reason`, validationID, valid, reason); err != nil {
		return err
	}
	if valid {
		_, err = tx.Exec(ctx, `UPDATE quality_review_cases SET resolved_at=now() WHERE knowledge_id::text=$1 AND candidate_version=$2
			AND reason IN ('source_changed','source_unavailable','source_expired','source_unverified','evidence_unavailable') AND resolved_at IS NULL`, id, version)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO quality_review_cases(knowledge_id,candidate_version,reason) VALUES($1,$2,$3)
			ON CONFLICT(knowledge_id,candidate_version,reason) DO UPDATE SET last_observed_at=now(),resolved_at=NULL`, id, version, reason)
	}
	if err != nil {
		return err
	}
	var scope domain.OperationScope
	if err := tx.QueryRow(ctx, `SELECT project_id,COALESCE(workflow_id::text,'') FROM knowledge_items WHERE id::text=$1`, id).Scan(&scope.ProjectID, &scope.WorkflowID); err != nil {
		return err
	}
	if err := auditMutation(ctx, tx, "knowledge.source."+reason, scope, domain.EvidenceReference{Kind: "knowledge", ID: id, Version: version}, domain.EvidenceReference{Kind: "validation", ID: validationID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) RecordProjectionCheck(ctx context.Context, item domain.KnowledgeItem) error {
	if item.Projection == nil || item.Projection.Check() != nil || item.Projection.KnowledgeID != item.ID || item.Projection.Version != item.Version || item.Projection.ContentSHA256 != domain.Digest([]byte(item.RetrievalText())) {
		return domain.ErrQualityBlocked
	}
	// Serialized with publication/revalidation. A stale worker cannot mark a new
	// candidate version current, nor can this write extend validation/source TTLs.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanKnowledge(tx.QueryRow(ctx, `SELECT `+knowledgeColumns+` FROM knowledge_items WHERE id::text=$1 FOR UPDATE`, item.ID))
	if err != nil {
		return err
	}
	if current.Version != item.Version || current.RetrievalText() != item.RetrievalText() || current.Status != domain.CandidateApproved {
		return domain.ErrVersionConflict
	}
	var eligible bool
	if err := tx.QueryRow(ctx, `SELECT knowledge_eligible($1)`, item.ID).Scan(&eligible); err != nil {
		return err
	}
	if !eligible {
		return domain.ErrQualityBlocked
	}
	var source string
	if err := tx.QueryRow(ctx, `SELECT v.source_manifest_sha256::text FROM knowledge_items k JOIN knowledge_validations v ON v.id=k.approval_validation_id WHERE k.id::text=$1`, item.ID).Scan(&source); err != nil {
		return err
	}
	if source != item.Projection.SourceManifestSHA256 {
		return domain.ErrVersionConflict
	}
	manifest, err := json.Marshal(item.Projection)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO knowledge_projection_checks(knowledge_id,version,retrieval_sha256,verified_at,manifest) VALUES($1,$2,$3,now(),$4)
		ON CONFLICT(knowledge_id) DO UPDATE SET version=EXCLUDED.version,retrieval_sha256=EXCLUDED.retrieval_sha256,verified_at=EXCLUDED.verified_at,manifest=EXCLUDED.manifest`, item.ID, item.Version, domain.Digest([]byte(item.RetrievalText())), manifest)
	if err != nil {
		return err
	}
	if err := auditMutation(ctx, tx, "index.projection_verified", domain.OperationScope{ProjectID: item.ProjectID, WorkflowID: item.WorkflowID}, domain.EvidenceReference{Kind: "projection", ID: item.Projection.VerificationID, Version: item.Version, SHA256: item.Projection.Digest()}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) ListQualityRefreshIDs(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	// Quarter-TTL scheduling leaves time to recover transient failures. Oldest
	// checks first ensures one broken source cannot starve the rest of the queue.
	rows, err := r.pool.Query(ctx, `SELECT k.id::text FROM knowledge_items k
		LEFT JOIN knowledge_sources s ON s.validation_id=k.approval_validation_id
		LEFT JOIN LATERAL (SELECT checked_at FROM knowledge_source_check_receipts WHERE validation_id=k.approval_validation_id ORDER BY checked_at DESC LIMIT 1) attempt ON true
		JOIN LATERAL knowledge_quality_policy(k.project_id) p ON true
		WHERE k.status='approved' AND k.approval_validation_id IS NOT NULL
		AND (greatest(s.verified_at,attempt.checked_at) IS NULL OR greatest(s.verified_at,attempt.checked_at)+p.source_max_age/4<=now())
		ORDER BY greatest(s.verified_at,attempt.checked_at) NULLS FIRST,k.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) ListQualityReviews(ctx context.Context, project string, limit int) ([]domain.KnowledgeQuality, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text FROM knowledge_items WHERE project_id=$1 AND status='approved' AND NOT knowledge_eligible(id) ORDER BY id LIMIT $2`, project, limit)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	results := make([]domain.KnowledgeQuality, 0, len(ids))
	for _, id := range ids {
		item, err := r.KnowledgeQuality(ctx, id)
		if err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, nil
}

var _ domain.KnowledgeQualityRepository = (*Repository)(nil)
