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
	projector  domain.GraphProjector
}

type codeEntityBatchUpserter interface {
	UpsertCodeEntities(context.Context, []domain.CodeEntity, [][]float32) error
}

func New(repository domain.Repository, embedder domain.Embedder, vectors domain.VectorStore, logger *slog.Logger, interval time.Duration, batchSize int) *Worker {
	return &Worker{repository: repository, embedder: embedder, vectors: vectors, logger: logger, interval: interval, batchSize: batchSize}
}

func (w *Worker) ConfigureGraphProjector(projector domain.GraphProjector) {
	w.projector = projector
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
	for index := 0; index < len(events); {
		if events[index].Topic == "code_entity.upsert" {
			end := index + 1
			for end < len(events) && events[end].Topic == "code_entity.upsert" {
				end++
			}
			if err := w.processCodeEntityBatch(ctx, events[index:end]); err != nil {
				return len(events), err
			}
			index = end
			continue
		}
		if err := w.finishEvent(ctx, events[index], w.process(ctx, events[index])); err != nil {
			return len(events), err
		}
		index++
	}
	return len(events), nil
}

func (w *Worker) finishEvent(ctx context.Context, event domain.OutboxEvent, processErr error) error {
	if processErr != nil {
		w.logger.Warn("outbox event failed", "event_id", event.ID, "topic", event.Topic, "attempt", event.Attempts, "error", processErr)
		if err := w.repository.FailOutbox(ctx, event.ID, processErr.Error()); err != nil {
			return fmt.Errorf("record outbox failure: %w", err)
		}
		return nil
	}
	if err := w.repository.CompleteOutbox(ctx, event.ID); err != nil {
		return fmt.Errorf("complete outbox event: %w", err)
	}
	return nil
}

func (w *Worker) processCodeEntityBatch(ctx context.Context, events []domain.OutboxEvent) error {
	entities := make([]domain.CodeEntity, 0, len(events))
	activeEvents := make([]domain.OutboxEvent, 0, len(events))
	for _, event := range events {
		entity, err := w.repository.GetCodeEntity(ctx, event.AggregateID)
		if err != nil {
			if finishErr := w.finishEvent(ctx, event, err); finishErr != nil {
				return finishErr
			}
			continue
		}
		entities = append(entities, entity)
		activeEvents = append(activeEvents, event)
	}
	if len(entities) == 0 {
		return nil
	}
	texts := make([]string, 0, len(entities))
	for _, entity := range entities {
		texts = append(texts, entity.RetrievalText())
	}
	embeddings, err := w.embedder.Embed(ctx, texts)
	if err == nil && len(embeddings) != len(entities) {
		err = fmt.Errorf("expected %d embeddings, got %d", len(entities), len(embeddings))
	}
	if err != nil {
		for _, event := range activeEvents {
			if finishErr := w.finishEvent(ctx, event, err); finishErr != nil {
				return finishErr
			}
		}
		return nil
	}
	if batchStore, ok := w.vectors.(codeEntityBatchUpserter); ok {
		if err := batchStore.UpsertCodeEntities(ctx, entities, embeddings); err != nil {
			for _, event := range activeEvents {
				if finishErr := w.finishEvent(ctx, event, err); finishErr != nil {
					return finishErr
				}
			}
			return nil
		}
		for _, event := range activeEvents {
			if finishErr := w.finishEvent(ctx, event, nil); finishErr != nil {
				return finishErr
			}
		}
		return nil
	}
	for index, event := range activeEvents {
		if finishErr := w.finishEvent(ctx, event, w.vectors.UpsertCodeEntity(ctx, entities[index], embeddings[index])); finishErr != nil {
			return finishErr
		}
	}
	return nil
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
		if err := w.vectors.Upsert(ctx, item, embeddings[0]); err != nil {
			return err
		}
		if w.projector != nil {
			if err := w.projector.ProjectKnowledge(ctx, item.ID); err != nil {
				return err
			}
		}
		edges, err := w.repository.GetSemanticGraphEdgesForKnowledge(ctx, item.ID)
		if err != nil {
			return err
		}
		return w.indexGraphEdges(ctx, edges)
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
		if err := w.vectors.UpsertRelation(ctx, relation, embeddings[0]); err != nil {
			return err
		}
		if w.projector != nil {
			return w.projector.ProjectRepositoryRelation(ctx, relation)
		}
		return nil
	case "code_graph.project":
		if w.projector == nil {
			return nil
		}
		return w.projector.ProjectCodeGraph(ctx, event.AggregateID)
	case "code_relation.upsert":
		edge, active, err := w.repository.GetSemanticGraphEdge(ctx, event.AggregateID)
		if err != nil || !active {
			return err
		}
		return w.indexGraphEdges(ctx, []domain.SemanticGraphEdge{edge})
	default:
		return fmt.Errorf("unsupported outbox topic %q", event.Topic)
	}
}

func (w *Worker) indexGraphEdges(ctx context.Context, edges []domain.SemanticGraphEdge) error {
	if len(edges) == 0 {
		return nil
	}
	texts := make([]string, 0, len(edges))
	for _, edge := range edges {
		texts = append(texts, edge.RetrievalText())
	}
	embeddings, err := w.embedder.Embed(ctx, texts)
	if err != nil {
		return err
	}
	if len(embeddings) != len(edges) {
		return fmt.Errorf("expected %d graph-edge embeddings, got %d", len(edges), len(embeddings))
	}
	for index, edge := range edges {
		if err := w.vectors.UpsertGraphEdge(ctx, edge, embeddings[index]); err != nil {
			return err
		}
	}
	return nil
}
