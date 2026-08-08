package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type Worker struct {
	repository domain.Repository
	embedder   domain.Embedder
	vectors    domain.VectorStore
	logger     *slog.Logger
	interval   time.Duration
	batchSize  int
}

func New(repository domain.Repository, embedder domain.Embedder, vectors domain.VectorStore, logger *slog.Logger, interval time.Duration, batchSize int) *Worker {
	return &Worker{repository: repository, embedder: embedder, vectors: vectors, logger: logger, interval: interval, batchSize: batchSize}
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		processed, err := w.ProcessOnce(ctx)
		if err != nil {
			w.logger.Error("worker batch failed", "error", err)
		}
		if processed > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Worker) ProcessOnce(ctx context.Context) (int, error) {
	events, err := w.repository.ClaimOutbox(ctx, w.batchSize)
	if err != nil {
		return 0, fmt.Errorf("claim outbox: %w", err)
	}
	for _, event := range events {
		if err := w.process(ctx, event); err != nil {
			w.logger.Warn("outbox event failed", "event_id", event.ID, "topic", event.Topic, "attempt", event.Attempts, "error", err)
			if failErr := w.repository.FailOutbox(ctx, event.ID, err.Error()); failErr != nil {
				return len(events), fmt.Errorf("record outbox failure: %w", failErr)
			}
			continue
		}
		if err := w.repository.CompleteOutbox(ctx, event.ID); err != nil {
			return len(events), fmt.Errorf("complete outbox event: %w", err)
		}
	}
	return len(events), nil
}

func (w *Worker) process(ctx context.Context, event domain.OutboxEvent) error {
	switch event.Topic {
	case "knowledge.upsert":
		item, err := w.repository.GetKnowledge(ctx, event.AggregateID, false)
		if err != nil {
			return err
		}
		embeddings, err := w.embedder.Embed(ctx, []string{item.RetrievalText()})
		if err != nil {
			return err
		}
		if len(embeddings) != 1 {
			return fmt.Errorf("expected one embedding, got %d", len(embeddings))
		}
		return w.vectors.Upsert(ctx, item, embeddings[0])
	case "repository_relation.upsert":
		relation, err := w.repository.GetRepositoryRelation(ctx, event.AggregateID)
		if err != nil {
			return err
		}
		embeddings, err := w.embedder.Embed(ctx, []string{relation.RetrievalText()})
		if err != nil {
			return err
		}
		if len(embeddings) != 1 {
			return fmt.Errorf("expected one embedding, got %d", len(embeddings))
		}
		return w.vectors.UpsertRelation(ctx, relation, embeddings[0])
	case "code_entity.upsert":
		entity, err := w.repository.GetCodeEntity(ctx, event.AggregateID)
		if err != nil {
			return err
		}
		embeddings, err := w.embedder.Embed(ctx, []string{entity.RetrievalText()})
		if err != nil {
			return err
		}
		if len(embeddings) != 1 {
			return fmt.Errorf("expected one embedding, got %d", len(embeddings))
		}
		return w.vectors.UpsertCodeEntity(ctx, entity, embeddings[0])
	default:
		return fmt.Errorf("unsupported outbox topic %q", event.Topic)
	}
}
