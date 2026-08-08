package mcpserver

import (
	"context"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerPublishesValidatedToolSchemasAndSafetyHints(t *testing.T) {
	ctx := context.Background()
	server := New(&service.Service{})
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
	if len(result.Tools) != 10 {
		t.Fatalf("tool count = %d, want 10", len(result.Tools))
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
}
