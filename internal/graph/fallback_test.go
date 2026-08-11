package graph

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type storeFake struct {
	domain.GraphStore
	err      error
	subgraph domain.KnowledgeSubgraph
}

func (f *storeFake) ExpandKnowledgeGraph(context.Context, domain.KnowledgeGraphRequest) (domain.KnowledgeSubgraph, error) {
	return f.subgraph, f.err
}

func TestFallbackUsesRecursiveStoreWhenAGEFails(t *testing.T) {
	primary := &storeFake{err: errors.New("projection stale")}
	fallback := &storeFake{subgraph: domain.KnowledgeSubgraph{Backend: "postgres-recursive"}}
	store := WithFallback(primary, fallback, slog.New(slog.NewTextHandler(io.Discard, nil)))
	result, err := store.ExpandKnowledgeGraph(context.Background(), domain.KnowledgeGraphRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Backend != "postgres-recursive" {
		t.Fatalf("fallback result = %#v", result)
	}
}
