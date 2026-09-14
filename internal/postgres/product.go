package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/jackc/pgx/v5"
)

const productColumns = `r.record,CASE WHEN r.origin='source_adapter' THEN 'observed' ELSE COALESCE((SELECT CASE d.decision WHEN 'accept' THEN 'accepted' ELSE 'rejected' END FROM product_record_decisions d WHERE d.record_id=r.id),'pending') END`

func scanProduct(row pgx.Row) (out domain.ProductRecord, err error) {
	var raw []byte
	var status string
	if err = row.Scan(&raw, &status); err == nil {
		err = json.Unmarshal(raw, &out)
		out.Status = status
	}
	return
}

func (r *Repository) GetProductRecord(ctx context.Context, project, id string, current bool) (domain.ProductRecord, error) {
	query := `SELECT ` + productColumns + ` FROM product_records r WHERE r.project_id=$1 AND r.id::text=$2`
	if current {
		query += ` AND product_record_eligible(r.id)`
	}
	out, err := scanProduct(r.pool.QueryRow(ctx, query, project, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.ErrQualityBlocked
	}
	return out, err
}

func (r *Repository) ProductRecordForIndex(ctx context.Context, id string) (domain.ProductRecord, error) {
	out, err := scanProduct(r.pool.QueryRow(ctx, `SELECT `+productColumns+` FROM product_records r WHERE r.id::text=$1 AND product_record_eligible(r.id)`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.ErrQualityBlocked
	}
	return out, err
}

func requireProductActor(ctx context.Context, tx pgx.Tx, actor, project string, roles ...string) error {
	p, ok := identity.PrincipalFromContext(ctx)
	if !ok || p.ID != actor {
		return domain.ErrForbidden
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals p JOIN principal_role_bindings b ON b.principal_id=p.id WHERE p.id=$1 AND p.active AND b.project_id IN ('*',$2) AND b.role=ANY($3) AND b.valid_from<=now() AND (b.valid_until IS NULL OR b.valid_until>now()))`, actor, project, roles).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return domain.ErrForbidden
	}
	return nil
}

func saveProductArtifact(ctx context.Context, tx pgx.Tx, a domain.Artifact) error {
	_, err := tx.Exec(ctx, `INSERT INTO artifacts(sha256,uri,media_type,size_bytes) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, a.SHA256, a.URI, a.MediaType, a.SizeBytes)
	return err
}

func (r *Repository) PutProductRecord(ctx context.Context, in domain.ProductRecord, expected int) (domain.ProductRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return in, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireProductActor(ctx, tx, in.Actor, in.ProjectID, "development", "qa", "product_owner", "operations", "incident_diagnosis"); err != nil {
		return in, err
	}
	if in.SHA256 != in.Digest() || in.Version != expected+1 || in.Evidence.SHA256 == "" {
		return in, domain.ErrValidationRequired
	}
	// Only RecordSourceObservation can atomically bind an adapter observation
	// to its validated collection receipt. Generic capture cannot claim that origin.
	if in.Origin != "proposal" || in.Status != "pending" {
		return in, domain.ErrValidationRequired
	}
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, in.ProjectID+":"+in.ProductID+":"+in.Key)
	if err != nil {
		return in, err
	}
	var latest int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM product_records WHERE project_id=$1 AND product_id=$2 AND record_key=$3`, in.ProjectID, in.ProductID, in.Key).Scan(&latest); err != nil {
		return in, err
	}
	if latest != expected {
		return in, domain.ErrVersionConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO projects(id,display_name) VALUES($1,$1) ON CONFLICT DO NOTHING`, in.ProjectID); err != nil {
		return in, err
	}
	if err = saveProductArtifact(ctx, tx, in.Evidence); err != nil {
		return in, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return in, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO product_records(id,project_id,product_id,record_key,version,kind,origin,sha256,record,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, in.ID, in.ProjectID, in.ProductID, in.Key, in.Version, in.Kind, in.Origin, in.SHA256, raw, in.ExpiresAt)
	if err != nil {
		return in, err
	}
	if err = auditMutation(ctx, tx, "product.record.capture", domain.ScopeFromContext(ctx), domain.EvidenceReference{Kind: "product_record", ID: in.ID, Version: in.Version, SHA256: in.SHA256}, domain.EvidenceReference{Kind: "artifact", ID: in.Evidence.SHA256, SHA256: in.Evidence.SHA256}); err != nil {
		return in, err
	}
	return in, tx.Commit(ctx)
}

func advanceProductHead(ctx context.Context, tx pgx.Tx, in domain.ProductRecord) error {
	_, err := tx.Exec(ctx, `INSERT INTO product_record_heads(project_id,product_id,record_key,record_id) VALUES($1,$2,$3,$4) ON CONFLICT(project_id,product_id,record_key) DO UPDATE SET record_id=EXCLUDED.record_id`, in.ProjectID, in.ProductID, in.Key, in.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic) VALUES($1,'product.upsert')`, in.ID)
	return err
}

func (r *Repository) ValidateProductRecord(ctx context.Context, in domain.ProductValidation) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireDatabaseHumanRole(ctx, tx, in.Actor, in.ProjectID, "qa"); err != nil {
		return err
	}
	p, ok := identity.PrincipalFromContext(ctx)
	if !ok || p.ID != in.Actor {
		return domain.ErrForbidden
	}
	record, err := scanProduct(tx.QueryRow(ctx, `SELECT `+productColumns+` FROM product_records r WHERE r.project_id=$1 AND r.id::text=$2 FOR UPDATE`, in.ProjectID, in.RecordID))
	if err != nil {
		return err
	}
	if record.Status != "pending" || record.SHA256 != in.SHA256 || in.Method != "human_attestation" || !in.ValidUntil.After(time.Now()) || in.ValidUntil.After(time.Now().Add(30*24*time.Hour)) {
		return domain.ErrValidationRequired
	}
	if err = saveProductArtifact(ctx, tx, in.Evidence); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO product_record_validations(id,project_id,record_id,sha256,actor,method,evidence_sha256,valid_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, in.ID, in.ProjectID, in.RecordID, in.SHA256, in.Actor, in.Method, in.Evidence.SHA256, in.ValidUntil)
	if err != nil {
		return err
	}
	if err = auditMutation(ctx, tx, "product.record.validate", domain.ScopeFromContext(ctx), domain.EvidenceReference{Kind: "validation", ID: in.ID, SHA256: in.SHA256}, domain.EvidenceReference{Kind: "artifact", ID: in.Evidence.SHA256, SHA256: in.Evidence.SHA256}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) DecideProductRecord(ctx context.Context, in domain.ProductDecision) (domain.ProductRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ProductRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireDatabaseHumanRole(ctx, tx, in.Actor, in.ProjectID, "product_owner"); err != nil {
		return domain.ProductRecord{}, err
	}
	p, ok := identity.PrincipalFromContext(ctx)
	if !ok || p.ID != in.Actor {
		return domain.ProductRecord{}, domain.ErrForbidden
	}
	out, err := scanProduct(tx.QueryRow(ctx, `SELECT `+productColumns+` FROM product_records r WHERE r.project_id=$1 AND r.id::text=$2 FOR UPDATE`, in.ProjectID, in.RecordID))
	if err != nil {
		return out, err
	}
	raw, _ := json.Marshal([]any{in.ProjectID, in.RecordID, in.ExpectedSHA256, in.Decision, in.Reason, in.ValidationID, in.IdempotencyKey, in.Actor})
	sha := domain.Digest(raw)
	var old string
	err = tx.QueryRow(ctx, `SELECT request_sha256 FROM product_record_decisions WHERE record_id=$1`, out.ID).Scan(&old)
	if err == nil {
		if old != sha {
			return out, domain.ErrVersionConflict
		}
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if out.Status != "pending" || out.SHA256 != in.ExpectedSHA256 || in.IdempotencyKey == "" || in.Reason == "" || (in.Decision != "accept" && in.Decision != "reject") {
		return out, domain.ErrValidationRequired
	}
	if in.Decision == "accept" {
		var valid bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_record_validations WHERE id::text=$1 AND project_id=$2 AND record_id=$3 AND sha256=$4 AND valid_until>now())`, in.ValidationID, in.ProjectID, out.ID, out.SHA256).Scan(&valid)
		if err != nil {
			return out, err
		}
		if !valid {
			return out, domain.ErrValidationRequired
		}
		// A delayed decision cannot roll the active context back over a newer accepted version.
		_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, out.ProjectID+":"+out.ProductID+":"+out.Key)
		if err != nil {
			return out, err
		}
		var current int
		err = tx.QueryRow(ctx, `SELECT COALESCE(max(r.version),0) FROM product_record_heads h JOIN product_records r ON r.id=h.record_id WHERE h.project_id=$1 AND h.product_id=$2 AND h.record_key=$3`, out.ProjectID, out.ProductID, out.Key).Scan(&current)
		if err != nil {
			return out, err
		}
		if current >= out.Version {
			return out, domain.ErrVersionConflict
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO product_record_decisions(record_id,decision,actor,validation_id,reason,idempotency_key,request_sha256) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7)`, out.ID, in.Decision, in.Actor, in.ValidationID, in.Reason, in.IdempotencyKey, sha)
	if err != nil {
		return out, err
	}
	if in.Decision == "accept" {
		out.Status = "accepted"
		if err = advanceProductHead(ctx, tx, out); err != nil {
			return out, err
		}
	} else {
		out.Status = "rejected"
	}
	if err = auditMutation(ctx, tx, "product.record.decide", domain.ScopeFromContext(ctx), domain.EvidenceReference{Kind: "product_record", ID: out.ID, Version: out.Version, SHA256: out.SHA256}); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (r *Repository) SearchProductRecords(ctx context.Context, project, product, query string, limit int) ([]domain.ProductRecord, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("invalid product search limit")
	}
	rows, err := r.pool.Query(ctx, `SELECT `+productColumns+` FROM product_records r WHERE project_id=$1 AND product_id=$2 AND product_record_eligible(r.id) AND ($3='' OR to_tsvector('simple',concat_ws(' ',r.record->>'title',r.record->>'content')) @@ plainto_tsquery('simple',$3)) ORDER BY r.created_at DESC,r.id LIMIT $4`, project, product, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProductRecord{}
	for rows.Next() {
		v, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) PutProductRelation(ctx context.Context, in domain.ProductRelation) (domain.ProductRelation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return in, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireProductActor(ctx, tx, in.Actor, in.ProjectID, "development", "qa", "product_owner", "operations", "incident_diagnosis"); err != nil {
		return in, err
	}
	if !domain.ValidProductRelation(in.Kind) || in.FromID == in.ToID || in.Evidence == "" {
		return in, domain.ErrValidationRequired
	}
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM product_records WHERE project_id=$1 AND product_id=$2 AND id::text=ANY($3) AND product_record_eligible(id)`, in.ProjectID, in.ProductID, []string{in.FromID, in.ToID}).Scan(&count)
	if err != nil {
		return in, err
	}
	if count != 2 {
		return in, domain.ErrQualityBlocked
	}
	err = tx.QueryRow(ctx, `INSERT INTO product_relations(id,project_id,product_id,from_id,to_id,kind,evidence,actor) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING RETURNING id::text`, in.ID, in.ProjectID, in.ProductID, in.FromID, in.ToID, in.Kind, in.Evidence, in.Actor).Scan(&in.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return in, domain.ErrVersionConflict
	}
	if err != nil {
		return in, err
	}
	for _, id := range []string{in.FromID, in.ToID} {
		if _, err = tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic) VALUES($1,'product.upsert')`, id); err != nil {
			return in, err
		}
	}
	if err = auditMutation(ctx, tx, "product.relation.record", domain.ScopeFromContext(ctx), domain.EvidenceReference{Kind: "product_relation", ID: in.ID}); err != nil {
		return in, err
	}
	return in, tx.Commit(ctx)
}

func (r *Repository) ProductRelations(ctx context.Context, project, product string, roots []string, limit int) ([]domain.ProductRelation, error) {
	if len(roots) > 100 || limit < 1 || limit > 200 {
		return nil, domain.ErrValidationRequired
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text,project_id,product_id,from_id::text,to_id::text,kind,evidence,actor FROM product_relations WHERE project_id=$1 AND product_id=$2 AND (from_id::text=ANY($3) OR to_id::text=ANY($3)) AND product_record_eligible(from_id) AND product_record_eligible(to_id) ORDER BY id LIMIT $4`, project, product, roots, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProductRelation{}
	for rows.Next() {
		var v domain.ProductRelation
		if err = rows.Scan(&v.ID, &v.ProjectID, &v.ProductID, &v.FromID, &v.ToID, &v.Kind, &v.Evidence, &v.Actor); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) RecordProductProjection(ctx context.Context, in domain.ProductProjection) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO product_projections(record_id,sha256,model,dimension) SELECT id,$2,$3,$4 FROM product_records WHERE id::text=$1 AND sha256=$2 AND product_record_eligible(id) ON CONFLICT(record_id) DO UPDATE SET sha256=EXCLUDED.sha256,model=EXCLUDED.model,dimension=EXCLUDED.dimension,verified_at=now()`, in.RecordID, in.SHA256, in.Model, in.Dimension)
	return err
}
func (r *Repository) ProductProjection(ctx context.Context, id string) (domain.ProductProjection, error) {
	var p domain.ProductProjection
	err := r.pool.QueryRow(ctx, `SELECT record_id::text,sha256,model,dimension FROM product_projections WHERE record_id::text=$1`, id).Scan(&p.RecordID, &p.SHA256, &p.Model, &p.Dimension)
	return p, err
}

func (r *Repository) RequeueProductRecords(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic) SELECT id,'product.upsert' FROM product_records WHERE product_record_eligible(id)`)
	return result.RowsAffected(), err
}
