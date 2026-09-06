package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dhanuka84/hybrid-ai-platform/internal/contextregistry"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
	"time"
)

const definitionColumns = `definition,project_id,sha256,registry_sha256,status,validated_at,COALESCE(approved_by,''),approved_at,projected_at,projection_model,projection_dimension`

func scanDefinition(row pgx.Row) (d domain.GovernedDefinition, err error) {
	var raw []byte
	err = row.Scan(&raw, &d.ProjectID, &d.SHA256, &d.RegistrySHA256, &d.Status, &d.ValidatedAt, &d.ApprovedBy, &d.ApprovedAt, &d.ProjectionVerifiedAt, &d.ProjectionModel, &d.ProjectionDimension)
	if err == nil {
		err = json.Unmarshal(raw, &d.ContextDefinition)
	}
	return
}
func (r *Repository) ValidateContextRegistry(ctx context.Context, v domain.RegistryValidation, defs []domain.ContextDefinition) error {
	expected, sha, err := contextregistry.Load()
	if err != nil {
		return err
	}
	a, _ := json.Marshal(expected)
	b, _ := json.Marshal(defs)
	if sha != v.RegistrySHA256 || string(a) != string(b) || v.Evidence.SHA256 == "" || v.CompletedAt.After(time.Now().Add(time.Second)) {
		return domain.ErrValidationRequired
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireDatabaseHumanRole(ctx, tx, v.Actor, v.ProjectID, "qa"); err != nil {
		return err
	}
	for _, d := range defs {
		if d.Kind == "metric" {
			query, _ := contextregistry.SQL(d.ID)
			if _, err = tx.Exec(ctx, "EXPLAIN "+query, v.ProjectID, time.Now().Add(-time.Hour), time.Now(), ""); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO artifacts(sha256,uri,media_type,size_bytes) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, v.Evidence.SHA256, v.Evidence.URI, v.Evidence.MediaType, v.Evidence.SizeBytes); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO context_registry_validations(id,project_id,registry_sha256,actor,evidence_sha256,completed_at) VALUES($1,$2,$3,$4,$5,$6)`, v.ID, v.ProjectID, sha, v.Actor, v.Evidence.SHA256, v.CompletedAt); err != nil {
		return err
	}
	for _, d := range defs {
		raw, _ := json.Marshal(d)
		tag, err := tx.Exec(ctx, `INSERT INTO context_definitions(project_id,id,version,kind,definition,sha256,registry_sha256,validated_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(project_id,id,version) DO UPDATE SET validated_at=EXCLUDED.validated_at
 WHERE context_definitions.sha256=EXCLUDED.sha256 AND context_definitions.registry_sha256=EXCLUDED.registry_sha256`, v.ProjectID, d.ID, d.Version, d.Kind, raw, domain.Digest(raw), sha, v.CompletedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrVersionConflict
		}
	}
	if err = auditMutation(ctx, tx, "context.registry.validate", domain.OperationScope{ProjectID: v.ProjectID}, domain.EvidenceReference{Kind: "registry_validation", ID: v.ID, SHA256: sha}, domain.EvidenceReference{Kind: "artifact", ID: v.Evidence.SHA256, SHA256: v.Evidence.SHA256}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) ContextDefinition(ctx context.Context, project, id string, version int) (domain.GovernedDefinition, error) {
	return scanDefinition(r.pool.QueryRow(ctx, `SELECT `+definitionColumns+` FROM context_definitions WHERE project_id=$1 AND id=$2 AND version=$3`, project, id, version))
}
func (r *Repository) DecideContextDefinition(ctx context.Context, in domain.DefinitionDecision) (d domain.GovernedDefinition, err error) {
	if in.Reason == "" || in.IdempotencyKey == "" || (in.Decision != "approve" && in.Decision != "reject") {
		return d, domain.ErrValidationRequired
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return d, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireDatabaseHumanRole(ctx, tx, in.Actor, in.ProjectID, "product_owner"); err != nil {
		return d, err
	}
	d, err = scanDefinition(tx.QueryRow(ctx, `SELECT `+definitionColumns+` FROM context_definitions WHERE project_id=$1 AND id=$2 AND version=$3 FOR UPDATE`, in.ProjectID, in.DefinitionID, in.ExpectedVersion))
	if err != nil {
		return d, err
	}
	raw, _ := json.Marshal(in)
	sha := domain.Digest(append(raw, []byte(in.Actor)...))
	var previous string
	err = tx.QueryRow(ctx, `SELECT request_sha256 FROM context_definition_decisions WHERE project_id=$1 AND definition_id=$2 AND idempotency_key=$3`, in.ProjectID, in.DefinitionID, in.IdempotencyKey).Scan(&previous)
	if err == nil {
		if previous != sha {
			return d, domain.ErrVersionConflict
		}
		return d, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return d, err
	}
	if d.SHA256 != in.ExpectedSHA256 || d.Status != "pending" {
		return d, domain.ErrVersionConflict
	}
	_, registrySHA, err := contextregistry.Load()
	if err != nil {
		return d, err
	}
	if d.RegistrySHA256 != registrySHA {
		return d, domain.ErrQualityBlocked
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM context_registry_validations v JOIN principals p ON p.id=v.actor WHERE v.id::text=$1 AND v.project_id=$2 AND v.registry_sha256=$3 AND p.active AND p.kind='human' AND v.completed_at<=now() AND v.completed_at+interval '30 days'>now())`, in.ValidationID, in.ProjectID, registrySHA).Scan(&valid)
	if err != nil {
		return d, err
	}
	if !valid {
		return d, domain.ErrValidationRequired
	}
	id, err := domain.NewID()
	if err != nil {
		return d, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO context_definition_decisions(id,project_id,definition_id,version,validation_id,actor,decision,reason,idempotency_key,request_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, in.ProjectID, in.DefinitionID, in.ExpectedVersion, in.ValidationID, in.Actor, in.Decision, in.Reason, in.IdempotencyKey, sha); err != nil {
		return d, err
	}
	status := "rejected"
	if in.Decision == "approve" {
		status = "approved"
	}
	d, err = scanDefinition(tx.QueryRow(ctx, `UPDATE context_definitions SET status=$4,approved_by=CASE WHEN $4='approved' THEN $5 ELSE NULL END,approved_at=CASE WHEN $4='approved' THEN now() ELSE NULL END WHERE project_id=$1 AND id=$2 AND version=$3 RETURNING `+definitionColumns, in.ProjectID, in.DefinitionID, in.ExpectedVersion, status, in.Actor))
	if err != nil {
		return d, err
	}
	if status == "approved" {
		if _, err = tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic) VALUES($1,'context.upsert')`, id); err != nil {
			return d, err
		}
	}
	if err = auditMutation(ctx, tx, "context.definition."+in.Decision, domain.OperationScope{ProjectID: in.ProjectID}, domain.EvidenceReference{Kind: "definition", ID: d.ID, Version: d.Version, SHA256: d.SHA256}, domain.EvidenceReference{Kind: "definition_decision", ID: id}); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}
func (r *Repository) ApprovedContextDefinitions(ctx context.Context, project string) ([]domain.GovernedDefinition, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+definitionColumns+` FROM context_definitions WHERE project_id=$1 AND status='approved' ORDER BY id,version DESC`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.GovernedDefinition{}
	for rows.Next() {
		d, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
func (r *Repository) DefinitionByDecision(ctx context.Context, id string) (domain.GovernedDefinition, error) {
	return scanDefinition(r.pool.QueryRow(ctx, `SELECT `+definitionColumns+` FROM context_definitions WHERE (project_id,id,version)=(SELECT project_id,definition_id,version FROM context_definition_decisions WHERE id::text=$1 AND decision='approve')`, id))
}
func (r *Repository) RecordDefinitionProjection(ctx context.Context, d domain.GovernedDefinition) error {
	if d.ProjectionModel == "" || d.ProjectionDimension < 1 {
		return domain.ErrQualityBlocked
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE context_definitions SET projected_at=now(),projection_model=$6,projection_dimension=$7 WHERE project_id=$1 AND id=$2 AND version=$3 AND sha256=$4 AND registry_sha256=$5 AND status='approved' AND validated_at+interval '30 days'>now()`, d.ProjectID, d.ID, d.Version, d.SHA256, d.RegistrySHA256, d.ProjectionModel, d.ProjectionDimension)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrQualityBlocked
	}
	if err = auditMutation(ctx, tx, "context.projection.verify", domain.OperationScope{ProjectID: d.ProjectID}, domain.EvidenceReference{Kind: "definition", ID: d.ID, Version: d.Version, SHA256: d.ProjectionDigest()}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) ClaimDefinitionRefresh(ctx context.Context, limit int) ([]domain.GovernedDefinition, error) {
	if limit < 1 || limit > 100 {
		limit = 10
	}
	rows, err := r.pool.Query(ctx, `WITH selected AS(SELECT project_id,id,version FROM context_definitions WHERE status='approved' AND validated_at+interval '30 days'>now() AND projected_at<now()-interval '6 hours' AND next_projection_check_at<=now() ORDER BY next_projection_check_at LIMIT $1 FOR UPDATE SKIP LOCKED)
	UPDATE context_definitions d SET next_projection_check_at=now()+interval '15 minutes' WHERE (d.project_id,d.id,d.version) IN(SELECT project_id,id,version FROM selected) RETURNING `+definitionColumns, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GovernedDefinition{}
	for rows.Next() {
		d, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Administrative rebuild of a derived projection; this never approves a
// definition or advances its validation clock.
func (r *Repository) RequeueContextDefinitions(ctx context.Context) (int64, error) {
	_, sha, err := contextregistry.Load()
	if err != nil {
		return 0, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `INSERT INTO outbox_events(aggregate_id,topic) SELECT decision.id,'context.upsert' FROM context_definitions d JOIN LATERAL(SELECT id FROM context_definition_decisions WHERE project_id=d.project_id AND definition_id=d.id AND version=d.version AND decision='approve' ORDER BY created_at DESC LIMIT 1) decision ON true WHERE d.status='approved' AND d.registry_sha256=$1 AND d.validated_at+interval '30 days'>now() RETURNING aggregate_id::text`, sha)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		var project string
		if err = tx.QueryRow(ctx, `SELECT project_id FROM context_definition_decisions WHERE id=$1`, id).Scan(&project); err != nil {
			return 0, err
		}
		if err = auditMutation(ctx, tx, "context.projection.requeue", domain.OperationScope{ProjectID: project}, domain.EvidenceReference{Kind: "definition_decision", ID: id}); err != nil {
			return 0, err
		}
	}
	return int64(len(ids)), tx.Commit(ctx)
}
func (r *Repository) QueryPlatformMetric(ctx context.Context, in domain.MetricRequest, registrySHA string) (out domain.MetricResult, err error) {
	if err = contextregistry.ValidateRequest(in); err != nil {
		return out, err
	}
	_, actual, err := contextregistry.Load()
	if err != nil {
		return out, err
	}
	if registrySHA != actual {
		return out, domain.ErrQualityBlocked
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, err := scanDefinition(tx.QueryRow(ctx, `SELECT `+definitionColumns+` FROM context_definitions WHERE project_id=$1 AND id=$2 AND version=$3`, in.ProjectID, in.MetricID, in.Version))
	if err != nil {
		return out, err
	}
	if d.Status != "approved" || d.RegistrySHA256 != actual || time.Since(d.ValidatedAt) > 30*24*time.Hour {
		return out, domain.ErrQualityBlocked
	}
	query, _ := contextregistry.SQL(in.MetricID)
	out = domain.MetricResult{ProjectID: in.ProjectID, MetricID: in.MetricID, Version: in.Version, Unit: d.Unit, Start: in.Start.UTC(), End: in.End.UTC(), DefinitionSHA256: d.SHA256, RegistrySHA256: actual, Freshness: "authoritative repeatable-read PostgreSQL snapshot; definition validated within 30 days", Coverage: "platform-recorded events only; external unobserved work excluded"}
	if err = tx.QueryRow(ctx, `SELECT now()`).Scan(&out.QueriedAt); err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, query, in.ProjectID, in.Start, in.End, in.Dimensions["task_type"]).Scan(&out.Value, &out.Numerator, &out.Denominator, &out.Failures); err != nil {
		return out, err
	}
	if in.MetricID == "pending_index_age_seconds" {
		out.Backlog = out.Numerator
		out.Explanation = "Current snapshot backlog; requested time window is not applied."
	} else if out.Value == nil {
		out.Explanation = "No eligible denominator; value is unknown, not zero."
	}
	return out, tx.Commit(ctx)
}
