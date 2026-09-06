package milvus

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

func TestVersionedKnowledgeProjectionIntegration(t *testing.T) {
	address := os.Getenv("TEST_MILVUS_ADDRESS")
	if address == "" {
		t.Skip("TEST_MILVUS_ADDRESS is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	id, _ := domain.NewID()
	collection := "agent_ready_fixture_" + strings.ReplaceAll(id, "-", "")
	s, err := Open(ctx, address, "default", "", collection, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close(context.Background()) }()
	if err := s.EnsureCollection(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, err := s.client.HasCollection(context.Background(), milvusclient.NewHasCollectionOption(collection))
		if err == nil {
			_ = s.client.DropCollection(context.Background(), milvusclient.NewDropCollectionOption(collection))
		}
	}()
	// Synthetic adapter fixture in its own collection, not a knowledge approval.
	item := domain.KnowledgeItem{ID: id, ProjectID: "synthetic-only", Version: 1, Status: domain.CandidateApproved, Title: "fixture", Content: "first content"}
	manifest := domain.ProjectionManifest{SchemaVersion: "hybrid-ai/knowledge-projection/v1", KnowledgeID: id, Version: 1, ContentSHA256: domain.Digest([]byte(item.RetrievalText())), SourceManifestSHA256: domain.Digest([]byte("synthetic source")), Provider: "ollama", Model: "synthetic-fixture", Dimension: 4, Coverage: 1, VerificationID: id}
	item.Projection = &manifest
	vector := []float32{1, 0, 0, 0}
	if err := s.Upsert(ctx, item, vector); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyKnowledgeProjection(ctx, item); err != nil {
		t.Fatal(err)
	}
	old := item
	item.Version = 2
	item.Content = "second content"
	updatedManifest := manifest
	updatedManifest.Version = 2
	updatedManifest.ContentSHA256 = domain.Digest([]byte(item.RetrievalText()))
	item.Projection = &updatedManifest
	if err := s.Upsert(ctx, item, vector); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyKnowledgeProjection(ctx, old); !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatalf("stale version readback: %v", err)
	}
	if err := s.VerifyKnowledgeProjection(ctx, item); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(ctx, item.ProjectID, vector, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !hits[0].Matches(item, item.ProjectID) {
		t.Fatalf("version/digest metadata lost: %+v", hits)
	}
	foreign := item
	foreign.ProjectID = "another-project"
	if err := s.VerifyKnowledgeProjection(ctx, foreign); !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatalf("cross-project readback: %v", err)
	}
	legacyID, _ := domain.NewID()
	_, err = s.client.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(collection).WithVarcharColumn("id", []string{legacyID}).WithVarcharColumn("project_id", []string{item.ProjectID}).WithVarcharColumn("document_type", []string{"knowledge"}).WithFloatVectorColumn("embedding", 4, [][]float32{vector}))
	if err != nil {
		t.Fatal(err)
	}
	legacy := item
	legacy.ID = legacyID
	if err := s.VerifyKnowledgeProjection(ctx, legacy); !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatalf("legacy projection readback: %v", err)
	}
}
