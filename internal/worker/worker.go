package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
)

type Worker struct {
	repository     domain.Repository
	embedder       domain.Embedder
	vectors        domain.VectorStore
	logger         *slog.Logger
	interval       time.Duration
	batchSize      int
	projector      domain.GraphProjector
	refreshSources func(context.Context, string) error
	exporter       *telemetry.Exporter
}

func (w *Worker) ConfigureTraceExporter(exporter *telemetry.Exporter) { w.exporter = exporter }

func (w *Worker) ConfigureSourceVerifier(verify func(context.Context, string) error) {
	w.refreshSources = verify
}

type codeEntityBatchUpserter interface {
	UpsertCodeEntities(context.Context, []domain.CodeEntity, [][]float32) error
}

func New(repository domain.Repository, embedder domain.Embedder, vectors domain.VectorStore, logger *slog.Logger, interval time.Duration, batchSize int) *Worker {
	embedder = telemetry.WrapEmbedder(repository, embedder)
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
	if err := w.refreshDefinitions(ctx); err != nil {
		return 0, err
	}
	if w.exporter != nil {
		if err := w.exporter.ProcessOnce(ctx); err != nil {
			w.logger.Warn("local trace export deferred")
		}
	}
	if repository, ok := w.repository.(domain.KnowledgeQualityRepository); ok && w.refreshSources != nil {
		ids, err := repository.ListQualityRefreshIDs(ctx, w.batchSize)
		if err != nil {
			return 0, err
		}
		for _, id := range ids {
			if err := w.refreshSources(ctx, id); err != nil {
				w.logger.Warn("source verification failed", "knowledge_id", id)
				continue
			}
			// Independently re-read the projection. This is not a heartbeat and
			// does not generate another embedding or extend content validation.
			item, err := w.repository.GetKnowledge(ctx, id, false)
			if err != nil {
				continue
			}
			if manifests, ok := w.repository.(domain.ProjectionRepository); ok {
				manifest, err := manifests.KnowledgeProjection(ctx, item.ID)
				if err != nil {
					continue
				}
				identity, ok := w.embedder.(interface{ EmbeddingIdentity() (string, string, int) })
				if !ok {
					continue
				}
				provider, model, _ := identity.EmbeddingIdentity()
				if manifest.Provider != provider || manifest.Model != model {
					continue
				}
				item.Projection = &manifest
			}
			reader, ok := w.vectors.(interface {
				VerifyKnowledgeProjection(context.Context, domain.KnowledgeItem) error
			})
			if !ok {
				continue
			}
			if err := reader.VerifyKnowledgeProjection(ctx, item); err != nil {
				w.logger.Warn("projection verification failed", "knowledge_id", id)
				continue
			}
			if err := repository.RecordProjectionCheck(ctx, item); err != nil {
				return 0, err
			}
		}
	}
	events, err := w.repository.ClaimOutbox(ctx, w.batchSize)
	if err != nil {
		return 0, fmt.Errorf("claim outbox: %w", err)
	}
	for index := 0; index < len(events); {
		if events[index].Attempts > 10 {
			if err := w.finishEvent(ctx, events[index], fmt.Errorf("retry limit exceeded; operator recovery required")); err != nil {
				return len(events), err
			}
			index++
			continue
		}
		if events[index].Topic == "code_entity.upsert" {
			end := index + 1
			for end < len(events) && events[end].Topic == "code_entity.upsert" && events[end].Attempts <= 10 {
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

func (w *Worker) process(ctx context.Context, event domain.OutboxEvent) (err error) {
	if event.Topic == "knowledge.upsert" {
		item, lookupErr := w.repository.GetKnowledge(ctx, event.AggregateID, true)
		if lookupErr != nil {
			return lookupErr
		}
		repository, _ := w.repository.(domain.TraceRepository)
		scope := domain.OperationScope{ProjectID: item.ProjectID, WorkflowID: item.WorkflowID}
		var finish telemetry.Finish
		ctx, finish, err = telemetry.Begin(ctx, repository, "index.knowledge", scope, []domain.EvidenceReference{{Kind: "knowledge", ID: item.ID, Version: item.Version}, {Kind: "outbox", ID: fmt.Sprint(event.ID)}})
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, finish(err)) }()
	}
	return w.processEffect(ctx, event)
}

func (w *Worker) processEffect(ctx context.Context, event domain.OutboxEvent) error {
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
		if err := domain.ValidateEmbedding(embeddings[0], len(embeddings[0])); err != nil {
			return err
		}
		if manifests, ok := w.repository.(domain.ProjectionRepository); ok {
			identity, ok := w.embedder.(interface{ EmbeddingIdentity() (string, string, int) })
			if !ok {
				return domain.ErrQualityBlocked
			}
			provider, model, _ := identity.EmbeddingIdentity()
			manifest, err := manifests.BuildKnowledgeProjection(ctx, item, provider, model, len(embeddings[0]))
			if err != nil {
				return err
			}
			item.Projection = &manifest
		}
		if err := w.vectors.Upsert(ctx, item, embeddings[0]); err != nil {
			return err
		}
		if repository, ok := w.repository.(domain.KnowledgeQualityRepository); ok {
			reader, ok := w.vectors.(interface {
				VerifyKnowledgeProjection(context.Context, domain.KnowledgeItem) error
			})
			if !ok {
				return fmt.Errorf("projection readback unavailable")
			}
			if err := reader.VerifyKnowledgeProjection(ctx, item); err != nil {
				return err
			}
			if err := repository.RecordProjectionCheck(ctx, item); err != nil {
				return err
			}
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
	case "context.upsert":
		return w.indexDefinition(ctx, event)
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
