package main

import (
	"context"
	"encoding/json"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/platform"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

type traceFixture struct {
	domain.Repository
	domain.TraceRepository
	records []domain.OperationRecord
}

func (r *traceFixture) AppendOperation(_ context.Context, v domain.OperationRecord) error {
	r.records = append(r.records, v)
	return nil
}
func TestPilotRefusesUnisolatedExecution(t *testing.T) {
	t.Setenv("AGENT_READY_PILOT_ISOLATED", "")
	if err := run(context.Background(), []string{"missing.json"}); err == nil {
		t.Fatal("missing isolation accepted")
	}
	if err := localEndpoint(context.Background(), "http://8.8.8.8:11434"); err == nil {
		t.Fatal("public provider accepted")
	}
	if err := localEndpoint(context.Background(), "https://localhost:11434"); err == nil {
		t.Fatal("unexpected endpoint accepted")
	}
}
func TestLocalGenerationPreservesExactOutputAndDisclosureManifest(t *testing.T) {
	body := `{"model":"fixture:1","done":true,"response":"{\"patch\":\"fixture patch\",\"summary\":\"fixture summary\"}"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/generate" {
			t.Error("wrong local route")
		}
		var input map[string]any
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if think, ok := input["think"].(bool); !ok || think {
			t.Error("pilot must request final answer without thinking output")
		}
		if schema, ok := input["format"].(map[string]any); !ok || schema["type"] != "object" {
			t.Error("structured patch schema missing")
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	store := artifacts.NewLocalStore(t.TempDir())
	repo := &traceFixture{}
	r := runner{app: &platform.Platform{Service: service.New(repo, store, nil, nil, false, false)}, store: store, cfg: config.Config{OllamaURL: server.URL}, spec: spec{Model: "fixture:1"}}
	ctx := domain.WithOperationScope(context.Background(), domain.OperationScope{ProjectID: "pilot-fixture", WorkflowID: "fixture"})
	result, err := r.generate(ctx, "synthetic bounded prompt")
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]string
	if err = json.Unmarshal(result, &output); err != nil || output["patch"] != "fixture patch" {
		t.Fatal("unexpected model content")
	}
	raw, err := store.Read(ctx, domain.Digest([]byte(body)))
	if err != nil || string(raw) != body {
		t.Fatal("exact output not preserved")
	}
	foundPrompt, foundManifest := false, false
	for _, record := range repo.records {
		if record.Name != "model.generate.input" || record.Phase != "outcome" {
			continue
		}
		for _, reference := range record.References {
			data, err := store.Read(ctx, reference.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) == "synthetic bounded prompt" {
				foundPrompt = true
				continue
			}
			var manifest map[string]any
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest["schema"] == "hybrid-ai/disclosed-context/v1" && manifest["prompt_sha256"] == domain.Digest([]byte("synthetic bounded prompt")) {
				foundManifest = true
				if think, ok := manifest["think"].(bool); !ok || think || manifest["format"] == nil || manifest["options"] == nil {
					t.Fatal("generation settings missing from disclosed manifest")
				}
			}
		}
	}
	if !foundPrompt || !foundManifest {
		t.Fatal("exact prompt or disclosed manifest missing")
	}
	if _, err = os.Stat("fixture.txt"); err == nil {
		t.Fatal("generation applied patch without verifier")
	}
}

func TestThinkingOnlyOutputRemainsFailedEvidence(t *testing.T) {
	body := `{"model":"fixture:1","done":true,"response":"","thinking":"not a patch answer"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	store := artifacts.NewLocalStore(t.TempDir())
	repo := &traceFixture{}
	r := runner{app: &platform.Platform{Service: service.New(repo, store, nil, nil, false, false)}, store: store, cfg: config.Config{OllamaURL: server.URL}, spec: spec{Model: "fixture:1"}}
	ctx := domain.WithOperationScope(context.Background(), domain.OperationScope{ProjectID: "pilot-fixture"})
	if _, err := r.generate(ctx, "synthetic bounded prompt"); err == nil {
		t.Fatal("thinking was accepted as a generated patch")
	}
	if raw, err := store.Read(ctx, domain.Digest([]byte(body))); err != nil || string(raw) != body {
		t.Fatal("failed output not retained exactly")
	}
	last := repo.records[len(repo.records)-1]
	if last.Name != "model.generate" || last.Phase != "outcome" || last.Outcome != "failed" {
		t.Fatal("missing failed-generation receipt")
	}
}
