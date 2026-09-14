package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) GetSourceReceipt(ctx context.Context, project, source, actor, key string) (out domain.SourceReceipt, err error) {
	var raw []byte
	err = r.pool.QueryRow(ctx, `SELECT receipt FROM product_source_receipts WHERE project_id=$1 AND source_id=$2 AND actor=$3 AND idempotency_key=$4`, project, source, actor, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return
}

func (r *Repository) RecordSourceObservation(ctx context.Context, receipt domain.SourceReceipt, record *domain.ProductRecord) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireExecutionSourceActor(ctx, tx, receipt.Actor, receipt.ProjectID, receipt.ProductID, receipt.QuerySHA256); err != nil {
		return err
	}
	if err = requireDatabaseHumanRole(ctx, tx, receipt.Owner, receipt.ProjectID, "operations"); err != nil {
		return err
	}
	if receipt.ID == "" || receipt.QuerySHA256 == "" || receipt.DescriptorSHA256 == "" || receipt.IdempotencyKey == "" {
		return domain.ErrValidationRequired
	}
	if (receipt.Status == "complete" || receipt.Status == "partial") != (record != nil) {
		return domain.ErrValidationRequired
	}
	if record != nil {
		if record.ID != receipt.RecordID || record.ProjectID != receipt.ProjectID || record.ProductID != receipt.ProductID || record.Actor != receipt.Actor || record.Origin != "source_adapter" || record.Kind != "observation" || record.Status != "observed" || record.SHA256 != record.Digest() || record.Source.ReceiptID != receipt.ID || record.Version != 1 || record.Evidence != receipt.Evidence {
			return domain.ErrValidationRequired
		}
		if _, err = tx.Exec(ctx, `INSERT INTO projects(id,display_name) VALUES($1,$1) ON CONFLICT DO NOTHING`, record.ProjectID); err != nil {
			return err
		}
		if err = saveProductArtifact(ctx, tx, record.Evidence); err != nil {
			return err
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO product_records(id,project_id,product_id,record_key,version,kind,origin,sha256,record,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, record.ID, record.ProjectID, record.ProductID, record.Key, record.Version, record.Kind, record.Origin, record.SHA256, raw, record.ExpiresAt)
		if err != nil {
			return err
		}
		if err = advanceProductHead(ctx, tx, *record); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO product_source_receipts(id,project_id,source_id,actor,idempotency_key,receipt) VALUES($1,$2,$3,$4,$5,$6)`, receipt.ID, receipt.ProjectID, receipt.SourceID, receipt.Actor, receipt.IdempotencyKey, raw)
	if err != nil {
		return err
	}
	refs := []domain.EvidenceReference{{Kind: "source_receipt", ID: receipt.ID, SHA256: receipt.QuerySHA256}}
	if record != nil {
		refs = append(refs, domain.EvidenceReference{Kind: "product_record", ID: record.ID, Version: record.Version, SHA256: record.SHA256}, domain.EvidenceReference{Kind: "artifact", ID: record.Evidence.SHA256, SHA256: record.Evidence.SHA256})
	}
	if err = auditMutation(ctx, tx, "source.receipt.record", domain.ScopeFromContext(ctx), refs...); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
