package worker

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dhanuka84/hybrid-ai-platform/internal/contextregistry"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
	"time"
)

func (w *Worker) refreshDefinitions(ctx context.Context) error {
	repository, ok := w.repository.(domain.SemanticRepository)
	if !ok {
		return nil
	}
	vectors, ok := w.vectors.(domain.SemanticVectorStore)
	if !ok {
		return nil
	}
	identity, ok := w.embedder.(interface{ EmbeddingIdentity() (string, string, int) })
	if !ok {
		return domain.ErrQualityBlocked
	}
	provider, model, _ := identity.EmbeddingIdentity()
	if provider != "ollama" {
		return domain.ErrQualityBlocked
	}
	_, sha, err := contextregistry.Load()
	if err != nil {
		return err
	}
	defs, err := repository.ClaimDefinitionRefresh(ctx, 10)
	if err != nil {
		return err
	}
	traceRepository, _ := w.repository.(domain.TraceRepository)
	for _, d := range defs {
		if d.RegistrySHA256 != sha || d.ProjectionModel != model {
			continue
		}
		operationCtx, finish, err := telemetry.Begin(ctx, traceRepository, "context.projection.readback", domain.OperationScope{ProjectID: d.ProjectID}, []domain.EvidenceReference{{Kind: "definition", ID: d.ID, Version: d.Version, SHA256: d.ProjectionDigest()}})
		if err != nil {
			return err
		}
		verifyErr := vectors.VerifyDefinitionProjection(operationCtx, d)
		if verifyErr == nil {
			verifyErr = repository.RecordDefinitionProjection(operationCtx, d)
		}
		if err = finish(verifyErr); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) indexDefinition(ctx context.Context, event domain.OutboxEvent) (err error) {
	repository, ok := w.repository.(domain.SemanticRepository)
	if !ok {
		return domain.ErrEvidenceUnavailable
	}
	vectors, ok := w.vectors.(domain.SemanticVectorStore)
	if !ok {
		return domain.ErrEvidenceUnavailable
	}
	d, err := repository.DefinitionByDecision(ctx, event.AggregateID)
	if err != nil {
		return err
	}
	_, sha, err := contextregistry.Load()
	if err != nil {
		return err
	}
	if d.Status != "approved" || d.RegistrySHA256 != sha || time.Since(d.ValidatedAt) > 30*24*time.Hour {
		return domain.ErrQualityBlocked
	}
	model, ok := w.embedder.(interface{ EmbeddingIdentity() (string, string, int) })
	if !ok {
		return domain.ErrQualityBlocked
	}
	provider, name, _ := model.EmbeddingIdentity()
	if provider != "ollama" || name == "" {
		return domain.ErrQualityBlocked
	}
	traceRepository, _ := w.repository.(domain.TraceRepository)
	ctx, finish, err := telemetry.Begin(ctx, traceRepository, "index.context", domain.OperationScope{ProjectID: d.ProjectID}, []domain.EvidenceReference{{Kind: "definition", ID: d.ID, Version: d.Version, SHA256: d.SHA256}})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, finish(err)) }()
	raw, _ := json.Marshal(d.ContextDefinition)
	embeddings, err := w.embedder.Embed(ctx, []string{string(raw)})
	if err != nil {
		return err
	}
	if len(embeddings) != 1 {
		return domain.ErrQualityBlocked
	}
	d.ProjectionModel = name
	d.ProjectionDimension = len(embeddings[0])
	if err = vectors.UpsertDefinition(ctx, d, embeddings[0]); err != nil {
		return err
	}
	if err = vectors.VerifyDefinitionProjection(ctx, d); err != nil {
		return err
	}
	return repository.RecordDefinitionProjection(ctx, d)
}
