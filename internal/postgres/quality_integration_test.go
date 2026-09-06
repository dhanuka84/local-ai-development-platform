package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/migrations"
)

func TestKnowledgeQualityAcrossRetrievalAndRecoveryIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	r, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := migrations.Apply(ctx, r.Pool()); err != nil {
		t.Fatal(err)
	}
	project, _ := domain.NewID()
	actor := "human:quality-fixture-" + project
	if err := r.BootstrapPrincipals(ctx, []domain.PrincipalBootstrap{{ID: actor, Human: true, Roles: []string{"qa", "product_owner", "operations"}, ProjectIDs: []string{project}}}); err != nil {
		t.Fatal(err)
	}
	store := artifacts.NewLocalStore(t.TempDir())
	artifact, err := store.Put(ctx, []byte("synthetic quality fixture"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	approve := func(item domain.KnowledgeItem, validation domain.KnowledgeValidation, key string) domain.KnowledgeItem {
		id, _ := domain.NewID()
		item, err := r.DecideKnowledge(ctx, domain.KnowledgeDecision{ID: id, KnowledgeID: item.ID, ExpectedVersion: item.Version, ValidationID: validation.ID, Decision: "approve", Reason: "synthetic fixture approval", IdempotencyKey: key, Actor: actor, RequestSHA256: domain.Digest([]byte(key)), Authorization: domain.AuthorizationDecision{Allowed: true}})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	create := func(label string) domain.KnowledgeItem {
		id, _ := domain.NewID()
		item, err := r.RecordGeneration(ctx, domain.GenerationCapture{ID: id, ProjectID: project, Prompt: label, Response: "synthetic solution " + label, Summary: label, Procedure: []string{"Run the fixture"}, PromptArtifact: artifact, OutputArtifact: artifact})
		if err != nil {
			t.Fatal(err)
		}
		return approve(item, fixtureValidation(t, ctx, r, item, actor, time.Now().UTC()), label)
	}
	a, b, c := create("chain-a"), create("chain-b"), create("chain-c")
	// Guidance can use a human attestation; code-changing consumers cannot
	// weaken their server-selected policy to reuse the same manual report.
	if _, err := r.GetKnowledge(domain.WithPurpose(ctx, domain.PurposeCodeChange), a.ID, false); !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatal("code-change purpose allowed manual report", err)
	}
	duplicateID, _ := domain.NewID()
	duplicate, err := r.RecordGeneration(ctx, domain.GenerationCapture{ID: duplicateID, ProjectID: project, Prompt: "duplicate", Response: a.Content, Summary: "duplicate", Procedure: []string{"fixture"}, PromptArtifact: artifact, OutputArtifact: artifact})
	if err != nil {
		t.Fatal(err)
	}
	duplicateValidation := fixtureValidation(t, ctx, r, duplicate, actor, time.Now().UTC())
	decisionID, _ := domain.NewID()
	if _, err = r.DecideKnowledge(ctx, domain.KnowledgeDecision{ID: decisionID, KnowledgeID: duplicate.ID, ExpectedVersion: 1, ValidationID: duplicateValidation.ID, Decision: "approve", Reason: "synthetic duplicate rejection", IdempotencyKey: "duplicate", Actor: actor, RequestSHA256: domain.Digest([]byte("duplicate")), Authorization: domain.AuthorizationDecision{Allowed: true}}); err == nil {
		t.Fatal("exact duplicate publication allowed")
	}
	var failedID int64
	if err = r.Pool().QueryRow(ctx, `UPDATE outbox_events SET attempts=10,failed_at=now() WHERE aggregate_id=$1 RETURNING id`, c.ID).Scan(&failedID); err != nil {
		t.Fatal(err)
	}
	retry := domain.RetryIndexInput{ProjectID: project, EventID: failedID, ExpectedAttempts: 10, Actor: actor, Reason: "synthetic repaired dependency", IdempotencyKey: "recovery"}
	replacement, err := r.RetryKnowledgeIndex(ctx, retry)
	if err != nil || replacement == failedID {
		t.Fatal("scoped recovery", replacement, err)
	}
	if again, err := r.RetryKnowledgeIndex(ctx, retry); err != nil || again != replacement {
		t.Fatal("idempotent recovery", again, err)
	}
	retry.Reason = "different payload"
	if _, err = r.RetryKnowledgeIndex(ctx, retry); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatal("retry payload conflict", err)
	}
	if _, err := r.Pool().Exec(ctx, `INSERT INTO knowledge_relations(from_id,to_id,relation_type,confidence) VALUES($1,$2,'related_to',1),($2,$3,'related_to',1)`, a.ID, b.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	quality, err := r.KnowledgeQuality(ctx, b.ID)
	if err != nil || !quality.Eligible {
		t.Fatalf("quality=%+v err=%v", quality, err)
	}
	manifest, err := r.BuildKnowledgeProjection(ctx, b, "ollama", "synthetic-fixture", 4)
	if err != nil {
		t.Fatal(err)
	}
	b.Projection = &manifest
	if err := r.RecordProjectionCheck(ctx, b); err != nil {
		t.Fatal(err)
	}
	after, err := r.KnowledgeQuality(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.SourceVerifiedAt.Equal(*quality.SourceVerifiedAt) || !after.ContentValidatedAt.Equal(*quality.ContentValidatedAt) || after.ProjectionVerifiedAt == nil {
		t.Fatal("projection write changed source/content clocks")
	}
	stale := b
	stale.Version++
	if err := r.RecordProjectionCheck(ctx, stale); !errors.Is(err, domain.ErrVersionConflict) && !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatalf("stale worker: %v", err)
	}
	if err := r.RecordSourceCheck(ctx, b.ID, b.Version, quality.ValidationID, false, "source_changed"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetKnowledge(ctx, b.ID, false); !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatalf("direct get leaked stale content: %v", err)
	}
	items, err := r.GetKnowledgeMany(ctx, []string{a.ID, b.ID, c.ID})
	if err != nil || len(items) != 2 {
		t.Fatalf("hydration=%v err=%v", items, err)
	}
	if _, err := r.SearchApprovedLexical(ctx, project, "chain-b", 10); !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatalf("lexical fallback bypass: %v", err)
	}
	graph, err := NewRecursiveGraphStore(r).ExpandKnowledgeGraph(ctx, domain.KnowledgeGraphRequest{ProjectID: project, KnowledgeSeedIDs: []string{a.ID}, MaxHops: 3, MaxNodes: 10, MaxEdges: 10})
	if err != nil || len(graph.Knowledge) != 1 || graph.Knowledge[0].ID != a.ID {
		t.Fatalf("traversed quarantined intermediate: %+v %v", graph, err)
	}
	foreign, err := r.HydrateKnowledgeSubgraph(ctx, "different-project", []domain.GraphNode{{ID: a.ID, Type: domain.GraphNodeKnowledgeItem}}, 10)
	if err != nil || len(foreign.Knowledge) != 0 || len(foreign.Nodes) != 0 {
		t.Fatalf("cross-project hydration: %+v %v", foreign, err)
	}
	stored, err := r.GetKnowledge(ctx, b.ID, true)
	if err != nil || stored.Status != domain.CandidateApproved {
		t.Fatal("quarantine rewrote approval history")
	}
	reviews, err := r.ListQualityReviews(ctx, project, 10)
	if err != nil || len(reviews) != 1 || reviews[0].KnowledgeID != b.ID {
		t.Fatalf("reviews=%+v err=%v", reviews, err)
	}
	if err := r.RecordSourceCheck(ctx, b.ID, b.Version, quality.ValidationID, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetKnowledge(ctx, b.ID, false); err != nil {
		t.Fatal(err)
	}
	// A heartbeat cannot renew evidence: expiry is computed at read time.
	if _, err := r.Pool().Exec(ctx, `UPDATE knowledge_sources SET verified_at=now()-interval '2 days' WHERE validation_id=$1`, quality.ValidationID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetKnowledge(ctx, b.ID, false); !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatal("expired source remained eligible")
	}
	if err := r.RecordSourceCheck(ctx, b.ID, b.Version, quality.ValidationID, true, ""); err != nil {
		t.Fatal(err)
	}
	// Revalidation creates another report and accountable decision, not a
	// mutation of the old attestation or an automatic reapproval by the worker.
	renewed := fixtureValidation(t, ctx, r, b, actor, time.Now().UTC())
	b = approve(b, renewed, "revalidated")
	var count int
	if err := r.Pool().QueryRow(ctx, `SELECT count(*) FROM knowledge_decisions WHERE knowledge_id=$1`, b.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("approval history count=%d err=%v", count, err)
	}
	if _, err := r.Pool().Exec(ctx, `INSERT INTO data_quality_policies VALUES($1,1,'synthetic-owner',now(),interval '1 day',interval '1 microsecond',interval '1 day',interval '1 day')`, project); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordSourceCheck(ctx, b.ID, b.Version, renewed.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	quality, err = r.KnowledgeQuality(ctx, b.ID)
	if err != nil || quality.Reason != "validation_expired" {
		t.Fatalf("source refresh extended content validation: %+v %v", quality, err)
	}
}

func TestOutboxRetryExhaustionIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	r, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := migrations.Apply(ctx, r.Pool()); err != nil {
		t.Fatal(err)
	}
	entity, _ := domain.NewID()
	var id int64
	if err := r.Pool().QueryRow(ctx, `INSERT INTO outbox_events(aggregate_id,topic,attempts) VALUES($1,'knowledge.upsert',10) RETURNING id`, entity).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := r.FailOutbox(ctx, id, "synthetic failure"); err != nil {
		t.Fatal(err)
	}
	var failed bool
	if err := r.Pool().QueryRow(ctx, `SELECT failed_at IS NOT NULL FROM outbox_events WHERE id=$1`, id).Scan(&failed); err != nil || !failed {
		t.Fatalf("failed queue=%v err=%v", failed, err)
	}
	if _, err := r.Pool().Exec(ctx, `UPDATE outbox_events SET next_attempt_at=now()-interval '1 day' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	events, err := r.ClaimOutbox(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.ID == id {
			t.Fatal("exhausted event reclaimed")
		}
	}
}
