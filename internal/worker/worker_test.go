package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type codeRepositoryFake struct {
	domain.Repository
	entity       domain.CodeEntity
	events       []domain.OutboxEvent
	completed    int64
	completeMany []int64
}

func (f *codeRepositoryFake) ClaimOutbox(context.Context, int) ([]domain.OutboxEvent, error) {
	if len(f.events) > 0 {
		return f.events, nil
	}
	return []domain.OutboxEvent{{ID: 7, AggregateID: f.entity.ID, Topic: "code_entity.upsert"}}, nil
}
func (f *codeRepositoryFake) GetCodeEntity(_ context.Context, id string) (domain.CodeEntity, error) {
	entity := f.entity
	entity.ID = id
	return entity, nil
}
func (f *codeRepositoryFake) CompleteOutbox(_ context.Context, id int64) error {
	f.completed = id
	f.completeMany = append(f.completeMany, id)
	return nil
}

type codeEmbedderFake struct {
	domain.Embedder
	calls int
}

func (f *codeEmbedderFake) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	embeddings := make([][]float32, len(texts))
	for index := range texts {
		embeddings[index] = []float32{1, 2, 3}
	}
	return embeddings, nil
}

type codeVectorFake struct {
	domain.VectorStore
	entity     domain.CodeEntity
	embedding  []float32
	relation   domain.RepositoryRelation
	entityIDs  []string
	batchCalls int
}

func (f *codeVectorFake) UpsertCodeEntity(_ context.Context, entity domain.CodeEntity, embedding []float32) error {
	f.entity = entity
	f.embedding = embedding
	f.entityIDs = append(f.entityIDs, entity.ID)
	return nil
}

func (f *codeVectorFake) UpsertCodeEntities(_ context.Context, entities []domain.CodeEntity, embeddings [][]float32) error {
	f.batchCalls++
	for index, entity := range entities {
		if err := f.UpsertCodeEntity(context.Background(), entity, embeddings[index]); err != nil {
			return err
		}
	}
	return nil
}

func TestProcessCodeEntitiesUsesOneEmbeddingBatch(t *testing.T) {
	repository := &codeRepositoryFake{
		entity: domain.CodeEntity{ProjectID: "product", RepositoryID: "repository", QualifiedName: "example/api.Save", Kind: "function", Revision: "abc"},
		events: []domain.OutboxEvent{
			{ID: 7, AggregateID: "entity-1", Topic: "code_entity.upsert"},
			{ID: 8, AggregateID: "entity-2", Topic: "code_entity.upsert"},
		},
	}
	embedder := &codeEmbedderFake{}
	vectors := &codeVectorFake{}
	worker := New(repository, embedder, vectors, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, 10)
	processed, err := worker.ProcessOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 2 || embedder.calls != 1 || vectors.batchCalls != 1 || len(vectors.entityIDs) != 2 || len(repository.completeMany) != 2 {
		t.Fatalf("processed=%d embed_calls=%d batch_calls=%d vectors=%v completed=%v", processed, embedder.calls, vectors.batchCalls, vectors.entityIDs, repository.completeMany)
	}
}
func (f *codeVectorFake) UpsertRelation(_ context.Context, relation domain.RepositoryRelation, _ []float32) error {
	f.relation = relation
	return nil
}

func TestProcessCodeEntityProjectionUsesPostgreSQLEntityID(t *testing.T) {
	repository := &codeRepositoryFake{entity: domain.CodeEntity{
		ID: "stable-postgres-uuid", ProjectID: "product", RepositoryID: "repository",
		QualifiedName: "example/api.Save", Kind: "function", Revision: "abc",
	}}
	vectors := &codeVectorFake{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker := New(repository, &codeEmbedderFake{}, vectors, logger, time.Second, 10)
	processed, err := worker.ProcessOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || repository.completed != 7 || vectors.entity.ID != repository.entity.ID || len(vectors.embedding) != 3 {
		t.Fatalf("processed=%d completed=%d vector=%#v embedding=%#v", processed, repository.completed, vectors.entity, vectors.embedding)
	}
}

type relationRepositoryFake struct {
	domain.Repository
	relation  domain.RepositoryRelation
	completed int64
}

func (f *relationRepositoryFake) ClaimOutbox(context.Context, int) ([]domain.OutboxEvent, error) {
	return []domain.OutboxEvent{{ID: 9, AggregateID: f.relation.ID, Topic: "repository_relation.upsert"}}, nil
}
func (f *relationRepositoryFake) GetRepositoryRelation(context.Context, string) (domain.RepositoryRelation, error) {
	return f.relation, nil
}
func (f *relationRepositoryFake) CompleteOutbox(_ context.Context, id int64) error {
	f.completed = id
	return nil
}

type projectorFake struct {
	domain.GraphProjector
	relation domain.RepositoryRelation
}

func (f *projectorFake) ProjectRepositoryRelation(_ context.Context, relation domain.RepositoryRelation) error {
	f.relation = relation
	return nil
}

func TestRepositoryRelationUpdatesVectorAndAGEProjections(t *testing.T) {
	relation := domain.RepositoryRelation{ID: "relation", ProjectID: "product", RelationType: "depends_on"}
	repository := &relationRepositoryFake{relation: relation}
	vectors := &codeVectorFake{}
	projector := &projectorFake{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker := New(repository, &codeEmbedderFake{}, vectors, logger, time.Second, 10)
	worker.ConfigureGraphProjector(projector)
	processed, err := worker.ProcessOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || repository.completed != 9 || vectors.relation.ID != relation.ID || projector.relation.ID != relation.ID {
		t.Fatalf("processed=%d completed=%d vector=%#v projected=%#v", processed, repository.completed, vectors.relation, projector.relation)
	}
}
