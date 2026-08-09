package mcpserver

import (
	"context"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type noopAnalyzer struct{}

func (noopAnalyzer) Name() string    { return "noop" }
func (noopAnalyzer) Version() string { return "1" }
func (noopAnalyzer) Analyze(context.Context, codegraph.Request) (codegraph.Snapshot, error) {
	return codegraph.Snapshot{}, nil
}

func TestServerPublishesValidatedToolSchemasAndSafetyHints(t *testing.T) {
	ctx := context.Background()
	svc := &service.Service{}
	if err := svc.ConfigureCodeGraph(noopAnalyzer{}, []string{t.TempDir()}, service.CodeGraphLimits{
		MaxFiles: 1, MaxEntities: 1, MaxRelations: 1,
	}); err != nil {
		t.Fatal(err)
	}
	server := New(svc)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 16 {
		t.Fatalf("tool count = %d, want 16", len(result.Tools))
	}
	tools := make(map[string]*mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
		if tool.InputSchema == nil {
			t.Fatalf("tool %s has no input schema", tool.Name)
		}
	}
	if !tools["knowledge_search"].Annotations.ReadOnlyHint {
		t.Fatal("knowledge_search is not marked read-only")
	}
	if tools["repository_relation_upsert"].Annotations.ReadOnlyHint {
		t.Fatal("repository_relation_upsert is incorrectly marked read-only")
	}
	if tools["code_repository_index"].Annotations.ReadOnlyHint {
		t.Fatal("code_repository_index is incorrectly marked read-only")
	}
	if !tools["code_graph_get"].Annotations.ReadOnlyHint {
		t.Fatal("code_graph_get is not marked read-only")
	}
}
