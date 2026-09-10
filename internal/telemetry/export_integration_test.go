package telemetry

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

// A real collector validates our OTLP encoding; fake HTTP handlers cannot
// establish that the actual receiver accepts the trace/span identifiers.
func TestActualLocalCollectorAcceptsDurableReceiptIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_OTEL_ENDPOINT")
	if endpoint == "" {
		t.Skip("requires a disposable local TEST_OTEL_ENDPOINT")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repo := &testRepository{}
	_, finish, err := Begin(ctx, repo, "synthetic.collector.check", domain.OperationScope{ProjectID: "synthetic-collector-fixture"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = finish(nil); err != nil {
		t.Fatal(err)
	}
	exporter, err := NewExporter(repo, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	for {
		repo.exports = nil
		if err = exporter.ProcessOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if len(repo.exports) == 2 && repo.exports[0] && repo.exports[1] {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("collector did not accept both immutable receipts: %v", repo.exports)
		case <-time.After(250 * time.Millisecond):
		}
	}
}
