package postgres

import (
	"context"
	"encoding/json"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func (r *Repository) BuildKnowledgeProjection(ctx context.Context, item domain.KnowledgeItem, provider, model string, dimension int) (domain.ProjectionManifest, error) {
	manifest := domain.ProjectionManifest{SchemaVersion: "hybrid-ai/knowledge-projection/v1", KnowledgeID: item.ID, Version: item.Version, ContentSHA256: domain.Digest([]byte(item.RetrievalText())), Provider: provider, Model: model, Dimension: dimension, Coverage: 1}
	err := r.pool.QueryRow(ctx, `SELECT v.source_manifest_sha256::text FROM knowledge_items k JOIN knowledge_validations v ON v.id=k.approval_validation_id WHERE k.id::text=$1 AND k.version=$2 AND knowledge_eligible(k.id)`, item.ID, item.Version).Scan(&manifest.SourceManifestSHA256)
	if err != nil {
		return manifest, err
	}
	manifest.VerificationID, err = domain.NewID()
	if err != nil {
		return manifest, err
	}
	return manifest, manifest.Check()
}
func (r *Repository) KnowledgeProjection(ctx context.Context, id string) (domain.ProjectionManifest, error) {
	var manifest domain.ProjectionManifest
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT manifest FROM knowledge_projection_checks WHERE knowledge_id::text=$1 AND manifest IS NOT NULL`, id).Scan(&raw)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, err
	}
	return manifest, manifest.Check()
}
