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
	entity    domain.CodeEntity
	completed int64
}

func (f *codeRepositoryFake) ClaimOutbox(context.Context, int) ([]domain.OutboxEvent, error) {
	return []domain.OutboxEvent{{ID: 7, AggregateID: f.entity.ID, Topic: "code_entity.upsert"}}, nil
}
func (f *codeRepositoryFake) GetCodeEntity(context.Context, string) (domain.CodeEntity, error) {
	return f.entity, nil
}
func (f *codeRepositoryFake) CompleteOutbox(_ context.Context, id int64) error {
	f.completed = id
	return nil
}

type codeEmbedderFake struct{ domain.Embedder }

func (*codeEmbedderFake) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1, 2, 3}}, nil
}

type codeVectorFake struct {
	domain.VectorStore
	entity    domain.CodeEntity
	embedding []float32
}

func (f *codeVectorFake) UpsertCodeEntity(_ context.Context, entity domain.CodeEntity, embedding []float32) error {
	f.entity = entity
	f.embedding = embedding
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
