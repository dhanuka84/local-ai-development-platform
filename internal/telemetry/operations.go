package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type Finish func(error, ...domain.EvidenceReference) error

func Begin(ctx context.Context, repository domain.TraceRepository, name string, scope domain.OperationScope, refs []domain.EvidenceReference) (context.Context, Finish, error) {
	if repository == nil {
		return ctx, func(error, ...domain.EvidenceReference) error { return nil }, nil
	}
	ctx, span, record := Start(ctx, name, scope, refs)
	if err := repository.AppendOperation(ctx, record); err != nil {
		span.End()
		return ctx, nil, err
	}
	finish := func(operationErr error, refs ...domain.EvidenceReference) error {
		defer span.End()
		outcome := "success"
		if operationErr != nil {
			outcome = "failed"
			if errors.Is(operationErr, domain.ErrQualityBlocked) || errors.Is(operationErr, domain.ErrValidationRequired) || errors.Is(operationErr, domain.ErrForbidden) {
				outcome = "denied"
			}
		}
		result := Result(record, outcome, refs)
		// A client disconnect must not silently discard the execution receipt.
		evidenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return repository.AppendOperation(evidenceCtx, result)
	}
	return ctx, finish, nil
}

type embeddingIdentity interface{ EmbeddingIdentity() (string, string, int) }
type tracedEmbedder struct {
	domain.Embedder
	repository      domain.TraceRepository
	provider, model string
}

func WrapEmbedder(repository any, embedder domain.Embedder) domain.Embedder {
	r, ok := repository.(domain.TraceRepository)
	if !ok || embedder == nil {
		return embedder
	}
	if _, ok := embedder.(*tracedEmbedder); ok {
		return embedder
	}
	provider, model := "local", "unspecified"
	if identity, ok := embedder.(embeddingIdentity); ok {
		provider, model, _ = identity.EmbeddingIdentity()
	}
	return &tracedEmbedder{Embedder: embedder, repository: r, provider: provider, model: model}
}
func (e *tracedEmbedder) EmbeddingIdentity() (string, string, int) {
	if identity, ok := e.Embedder.(embeddingIdentity); ok {
		return identity.EmbeddingIdentity()
	}
	return e.provider, e.model, 0
}
func (e *tracedEmbedder) Embed(ctx context.Context, texts []string) (vectors [][]float32, err error) {
	scope := domain.ScopeFromContext(ctx)
	if scope.ProjectID == "" {
		scope.ProjectID = "system"
	}
	ctx, span, record := Start(ctx, "model.embedding", scope, []domain.EvidenceReference{{Kind: "batch", ID: fmt.Sprint(len(texts))}})
	record.Provider = e.provider
	record.Model = e.model
	if err = e.repository.AppendOperation(ctx, record); err != nil {
		span.End()
		return nil, err
	}
	defer func() {
		span.End()
		outcome := "success"
		if err != nil {
			outcome = "failed"
		}
		result := Result(record, outcome, nil)
		receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, e.repository.AppendOperation(receiptCtx, result))
	}()
	return e.Embedder.Embed(ctx, texts)
}
