package worker

import (
	"context"
	"errors"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
)

func (w *Worker) indexProduct(ctx context.Context, event domain.OutboxEvent) (err error) {
	r, ok := w.repository.(domain.ProductRepository)
	if !ok {
		return domain.ErrEvidenceUnavailable
	}
	v, ok := w.vectors.(domain.ProductVectorStore)
	if !ok {
		return domain.ErrEvidenceUnavailable
	}
	record, err := r.ProductRecordForIndex(ctx, event.AggregateID)
	if errors.Is(err, domain.ErrQualityBlocked) {
		// An accepted head may be superseded while its projection is queued.
		// Retrieval rehydrates current eligibility, so an old hit is harmless.
		return nil
	}
	if err != nil {
		return err
	}
	identity, ok := w.embedder.(interface{ EmbeddingIdentity() (string, string, int) })
	if !ok {
		return domain.ErrEvidenceUnavailable
	}
	provider, model, _ := identity.EmbeddingIdentity()
	if provider != "ollama" || model == "" {
		return domain.ErrForbidden
	}
	trace, _ := w.repository.(domain.TraceRepository)
	ctx, finish, err := telemetry.Begin(ctx, trace, "index.product", domain.OperationScope{ProjectID: record.ProjectID}, []domain.EvidenceReference{{Kind: "product_record", ID: record.ID, Version: record.Version, SHA256: record.SHA256}})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, finish(err)) }()
	embeddings, err := w.embedder.Embed(ctx, []string{record.RetrievalText()})
	if err != nil {
		return err
	}
	if len(embeddings) != 1 {
		return domain.ErrEvidenceUnavailable
	}
	p := domain.ProductProjection{RecordID: record.ID, SHA256: record.SHA256, Model: model, Dimension: len(embeddings[0])}
	if err = v.UpsertProductRecord(ctx, record, p, embeddings[0]); err != nil {
		return err
	}
	if err = v.VerifyProductProjection(ctx, record, p); err != nil {
		return err
	}
	if err = r.RecordProductProjection(ctx, p); err != nil {
		return err
	}
	if projector, ok := w.projector.(domain.ProductGraphProjector); ok {
		relations, err := r.ProductRelations(ctx, record.ProjectID, record.ProductID, []string{record.ID}, 200)
		if err != nil {
			return err
		}
		return projector.ProjectProductRecord(ctx, record, relations)
	}
	return nil
}
