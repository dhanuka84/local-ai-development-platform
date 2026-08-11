package postgres

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/migrations"
)

func TestRepositoryWorkflowIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	repository, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if err := migrations.Apply(ctx, repository.Pool()); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Pool().Exec(ctx, `TRUNCATE projects CASCADE; TRUNCATE outbox_events RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
	principals := []domain.PrincipalBootstrap{{
		ID: "human:integration-developer", DisplayName: "Integration developer", Token: "integration-test-token",
		Human: true, Roles: []string{"development", "qa", "product_owner", "operations"}, ProjectIDs: []string{"*"},
	}}
	if err := repository.BootstrapPrincipals(ctx, principals); err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte(principals[0].Token))
	principal, err := repository.AuthenticatePrincipal(ctx, tokenHash[:])
	if err != nil || principal.ID != principals[0].ID || !principal.HasRole("product", "qa") {
		t.Fatalf("authenticated principal=%#v err=%v", principal, err)
	}

	requestArtifact := domain.Artifact{
		SHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		URI:    "file:///workflow-request", MediaType: "text/markdown", SizeBytes: 11,
	}
	workflowID, _ := domain.NewID()
	createdEventID, _ := domain.NewID()
	workflow, err := repository.CreateWorkflow(ctx, domain.WorkflowRun{
		ID: workflowID, ProjectID: "product", Kind: "software-development", State: "intake", Version: 1,
		Risk: "low", DataClassification: "internal", RequestArtifact: requestArtifact,
		IdempotencyKey: "integration-workflow", CreatedBy: principal.ID, Metadata: map[string]any{"source": "integration"},
	}, domain.WorkflowEvent{
		ID: createdEventID, WorkflowID: workflowID, EventType: "WORKFLOW_CREATED", ToState: "intake",
		ActorPrincipalID: principal.ID, ActorRole: "development", IdempotencyKey: "integration-workflow:created",
		Payload: map[string]any{}, AuthorizationDecision: "allow", CerbosCallID: "test-call", PolicyVersion: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if workflow.State != "intake" || workflow.Governance.Profile != domain.GovernanceSolo {
		t.Fatalf("workflow=%#v", workflow)
	}
	transitionEventID, _ := domain.NewID()
	workflow, event, err := repository.TransitionWorkflow(ctx, domain.WorkflowTransition{
		WorkflowID: workflow.ID, ExpectedVersion: 1, ExpectedState: "intake", ResultingState: "classified",
		Event: domain.WorkflowEvent{
			ID: transitionEventID, WorkflowID: workflow.ID, EventType: "CLASSIFIED", FromState: "intake", ToState: "classified",
			ActorPrincipalID: principal.ID, ActorRole: "development", IdempotencyKey: "integration-workflow:classified",
			Payload: map[string]any{"risk": "low"}, AuthorizationDecision: "allow", CerbosCallID: "test-call-2", PolicyVersion: "default",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if workflow.State != "classified" || workflow.Version != 2 || event.Sequence != 2 {
		t.Fatalf("workflow=%#v event=%#v", workflow, event)
	}
	idempotentWorkflow, idempotentEvent, err := repository.TransitionWorkflow(ctx, domain.WorkflowTransition{
		WorkflowID: workflow.ID, ExpectedVersion: 1, ExpectedState: "intake", ResultingState: "classified",
		Event: event,
	})
	if err != nil || idempotentWorkflow.Version != 2 || idempotentEvent.ID != event.ID {
		t.Fatalf("idempotent workflow=%#v event=%#v err=%v", idempotentWorkflow, idempotentEvent, err)
	}
	staleEventID, _ := domain.NewID()
	_, _, err = repository.TransitionWorkflow(ctx, domain.WorkflowTransition{
		WorkflowID: workflow.ID, ExpectedVersion: 1, ExpectedState: "classified", ResultingState: "ready",
		Event: domain.WorkflowEvent{
			ID: staleEventID, WorkflowID: workflow.ID, EventType: "READY", FromState: "classified", ToState: "ready",
			ActorPrincipalID: principal.ID, ActorRole: "development", IdempotencyKey: "integration-workflow:stale",
			Payload: map[string]any{}, AuthorizationDecision: "allow",
		},
	})
	if err == nil {
		t.Fatal("stale transition unexpectedly succeeded")
	}

	requestTaskArtifact := domain.Artifact{
		SHA256: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		URI:    "file:///task-request", MediaType: "application/json", SizeBytes: 12,
	}
	firstTaskID, _ := domain.NewID()
	firstTaskEventID, _ := domain.NewID()
	firstTask, _, err := repository.CreateWorkflowTask(ctx, domain.WorkflowTaskCheckpoint{
		ID: firstTaskID, WorkflowID: workflow.ID, TaskKey: "design", Title: "Design tests",
		TaskType: "design", State: domain.TaskStateQueued, Route: domain.TaskRoutePending,
		ExecutionMode: "auto", Version: 1, RAGQuery: "validated test design", RAGBackend: "pending",
		RAGHitIDs: []string{}, MatchThreshold: 0.75, RequestArtifact: requestTaskArtifact, CreatedBy: principal.ID,
	}, domain.WorkflowTaskEvent{
		ID: firstTaskEventID, TaskID: firstTaskID, EventType: "TASK_QUEUED", ToState: domain.TaskStateQueued,
		ActorPrincipalID: principal.ID, ActorRole: "development", IdempotencyKey: "task:design",
		Payload: map[string]any{}, AuthorizationDecision: "allow",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondTaskID, _ := domain.NewID()
	secondTaskEventID, _ := domain.NewID()
	secondTask, _, err := repository.CreateWorkflowTask(ctx, domain.WorkflowTaskCheckpoint{
		ID: secondTaskID, WorkflowID: workflow.ID, TaskKey: "implement", Title: "Implement tests",
		TaskType: "implementation", State: domain.TaskStateQueued, Route: domain.TaskRoutePending,
		ExecutionMode: "auto", Version: 1, RAGQuery: "validated test implementation", RAGBackend: "pending",
		RAGHitIDs: []string{}, MatchThreshold: 0.75, RequestArtifact: requestTaskArtifact, CreatedBy: principal.ID,
	}, domain.WorkflowTaskEvent{
		ID: secondTaskEventID, TaskID: secondTaskID, EventType: "TASK_QUEUED", ToState: domain.TaskStateQueued,
		ActorPrincipalID: principal.ID, ActorRole: "development", IdempotencyKey: "task:implement",
		Payload: map[string]any{}, AuthorizationDecision: "allow",
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstTask.Ordinal != 1 || secondTask.Ordinal != 2 || firstTask.ExecutionMode != "auto" {
		t.Fatalf("first task=%#v second task=%#v", firstTask, secondTask)
	}
	type concurrentTaskResult struct {
		task domain.WorkflowTaskCheckpoint
		err  error
	}
	concurrentResults := make(chan concurrentTaskResult, 2)
	for i := 0; i < 2; i++ {
		taskID, _ := domain.NewID()
		eventID, _ := domain.NewID()
		go func() {
			task, _, createErr := repository.CreateWorkflowTask(ctx, domain.WorkflowTaskCheckpoint{
				ID: taskID, WorkflowID: workflow.ID, TaskKey: "concurrent", Title: "Concurrent task",
				TaskType: "testing", State: domain.TaskStateQueued, Route: domain.TaskRoutePending,
				ExecutionMode: "auto", Version: 1, RAGQuery: "validated concurrent task", RAGBackend: "pending",
				RAGHitIDs: []string{}, MatchThreshold: 0.75, RequestArtifact: requestTaskArtifact, CreatedBy: principal.ID,
			}, domain.WorkflowTaskEvent{
				ID: eventID, TaskID: taskID, EventType: "TASK_QUEUED", ToState: domain.TaskStateQueued,
				ActorPrincipalID: principal.ID, ActorRole: "development", IdempotencyKey: "task:concurrent",
				Payload: map[string]any{}, AuthorizationDecision: "allow",
			})
			concurrentResults <- concurrentTaskResult{task: task, err: createErr}
		}()
	}
	concurrentA, concurrentB := <-concurrentResults, <-concurrentResults
	if concurrentA.err != nil || concurrentB.err != nil || concurrentA.task.ID != concurrentB.task.ID || concurrentA.task.Ordinal != 3 {
		t.Fatalf("concurrent A=%#v err=%v B=%#v err=%v", concurrentA.task, concurrentA.err, concurrentB.task, concurrentB.err)
	}
	head, ok, err := repository.GetActivatableWorkflowTask(ctx, workflow.ID)
	if err != nil || !ok || head.ID != firstTask.ID {
		t.Fatalf("activatable head=%#v ok=%v err=%v", head, ok, err)
	}
	rejectEventID, _ := domain.NewID()
	firstTask, _, err = repository.TransitionWorkflowTask(ctx, domain.WorkflowTaskTransition{
		TaskID: firstTask.ID, ExpectedVersion: firstTask.Version, ExpectedState: domain.TaskStateQueued,
		ResultingState: domain.TaskStateRejected, Completed: true,
		Event: domain.WorkflowTaskEvent{
			ID: rejectEventID, TaskID: firstTask.ID, EventType: "TASK_REJECTED",
			FromState: domain.TaskStateQueued, ToState: domain.TaskStateRejected,
			ActorPrincipalID: principal.ID, ActorRole: "product_owner", IdempotencyKey: "task:design:reject",
			Payload: map[string]any{"reason": "superseded"}, AuthorizationDecision: "allow",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	head, ok, err = repository.GetActivatableWorkflowTask(ctx, workflow.ID)
	if err != nil || !ok || head.ID != secondTask.ID || firstTask.State != domain.TaskStateRejected {
		t.Fatalf("next head=%#v first=%#v ok=%v err=%v", head, firstTask, ok, err)
	}

	generationID, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	artifactA := domain.Artifact{SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", URI: "file:///a", MediaType: "text/plain", SizeBytes: 7}
	artifactB := domain.Artifact{SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", URI: "file:///b", MediaType: "text/plain", SizeBytes: 8}
	candidate, err := repository.RecordGeneration(ctx, domain.GenerationCapture{
		ID: generationID, ProjectID: "product", WorkflowID: workflow.ID, SessionID: "session", TaskType: "debugging",
		Prompt: "fix repository contract mismatch", Response: "original solution", Summary: "repair shared contract",
		Procedure: []string{"inspect schema", "update client"}, ValidationEvidence: []string{"go test ./... passed"},
		Provider: "local", Model: "test", PromptArtifact: artifactA, OutputArtifact: artifactB,
	})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != domain.CandidatePending || candidate.WorkflowID != workflow.ID || candidate.Problem == "" || len(candidate.Procedure) != 2 {
		t.Fatalf("candidate = %#v", candidate)
	}
	reviewID, _ := domain.NewID()
	reviewArtifact := domain.Artifact{SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", URI: "file:///review", MediaType: "text/markdown", SizeBytes: 9}
	manifestArtifact := domain.Artifact{SHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", URI: "file:///manifest", MediaType: "application/json", SizeBytes: 10}
	if err := repository.RecordReview(ctx, domain.ReviewRecord{
		ID: reviewID, KnowledgeID: candidate.ID, WorkflowID: workflow.ID, Reviewer: "codex", Provider: "openai", Model: "review-model",
		Verdict: "revise", Comments: "make validation explicit", ImprovedContent: "reviewed solution",
		ValidationEvidence: []string{"go test ./... passed after revision"},
		ReviewArtifact:     reviewArtifact, ContextManifestArtifact: manifestArtifact,
	}); err != nil {
		t.Fatal(err)
	}
	var reviewArtifactSHA, manifestArtifactSHA, reviewWorkflowID string
	if err := repository.Pool().QueryRow(ctx, `SELECT review_artifact_sha256,context_manifest_artifact_sha256,workflow_id::text
		FROM review_records WHERE id=$1`, reviewID).Scan(&reviewArtifactSHA, &manifestArtifactSHA, &reviewWorkflowID); err != nil {
		t.Fatal(err)
	}
	if reviewArtifactSHA != reviewArtifact.SHA256 || manifestArtifactSHA != manifestArtifact.SHA256 || reviewWorkflowID != workflow.ID {
		t.Fatalf("review refs = %q, %q, %q", reviewArtifactSHA, manifestArtifactSHA, reviewWorkflowID)
	}
	reviewEvidenceExists, err := repository.ReviewEvidenceExists(
		ctx, candidate.ID, workflow.ID, "openai", "review-model", reviewArtifact.SHA256, manifestArtifact.SHA256,
	)
	if err != nil || !reviewEvidenceExists {
		t.Fatalf("review evidence exists=%v err=%v", reviewEvidenceExists, err)
	}
	revised, err := repository.GetKnowledge(ctx, candidate.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if revised.Content != "reviewed solution" || revised.Version != 2 || len(revised.ValidationEvidence) != 1 || revised.ValidationEvidence[0] != "go test ./... passed after revision" {
		t.Fatalf("revised candidate = %#v", revised)
	}
	approved, err := repository.ApproveCandidate(ctx, candidate.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != domain.CandidateApproved || approved.ApprovedAt.IsZero() {
		t.Fatalf("approved = %#v", approved)
	}

	relation, err := repository.UpsertRepositoryRelation(ctx, domain.RepositoryRelation{
		ProjectID:    "product",
		From:         domain.SoftwareRepository{Name: "api", CanonicalURL: "https://git.example/api.git", DefaultBranch: "main", Revision: "abc"},
		To:           domain.SoftwareRepository{Name: "web", CanonicalURL: "https://git.example/web.git", DefaultBranch: "main", Revision: "def"},
		RelationType: "provides_api_to", Evidence: "web/go.mod imports api client", Confidence: .95, ApprovedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := repository.GetRepositoryGraph(ctx, "product", relation.From.CanonicalURL, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph) != 1 || graph[0].ID != relation.ID {
		t.Fatalf("graph = %#v", graph)
	}

	events, err := repository.ClaimOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("outbox events = %#v", events)
	}
	for _, event := range events {
		if err := repository.CompleteOutbox(ctx, event.ID); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UTC()
	snapshot := codegraph.Snapshot{
		RepositoryPath: "/tmp/api", RepositoryName: "api", Branch: "main", Revision: "abc",
		Analyzer: "integration", AnalyzerVersion: "1",
		StartedAt: now, CompletedAt: now,
		Entities: []codegraph.Entity{
			{Key: "go:repository:.", Language: "go", Kind: codegraph.EntityRepository, Name: "api", QualifiedName: "."},
			{Key: "go:file:store.go", Language: "go", Kind: codegraph.EntityFile, Name: "store.go", QualifiedName: "store.go"},
			{Key: "go:function:example/api.Save", Language: "go", Kind: codegraph.EntityFunction, Name: "Save", QualifiedName: "example/api.Save", Signature: "func Save()", Location: codegraph.Location{FilePath: "store.go", Start: codegraph.Position{Line: 3, Column: 1}, End: codegraph.Position{Line: 5, Column: 2}}},
		},
		Relations: []codegraph.Relation{
			{SourceKey: "go:repository:.", TargetKey: "go:file:store.go", Kind: codegraph.RelationContains, Confidence: 1},
			{SourceKey: "go:file:store.go", TargetKey: "go:function:example/api.Save", Kind: codegraph.RelationDefines, Confidence: 1},
		},
	}
	analysis, err := repository.StoreCodeGraph(ctx, "product", domain.SoftwareRepository{
		Name: "api", CanonicalURL: "https://git.example/api.git", DefaultBranch: "main",
	}, "codex", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.EntityCount != 3 || analysis.RelationCount != 2 || analysis.Repository.ID == "" || analysis.Branch != "main" {
		t.Fatalf("analysis = %#v", analysis)
	}
	symbols, err := repository.SearchCodeEntitiesLexical(ctx, "product", analysis.Repository.ID, "Save", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 1 || symbols[0].QualifiedName != "example/api.Save" ||
		symbols[0].RepositoryName != "api" || symbols[0].Branch != "main" {
		t.Fatalf("symbols = %#v", symbols)
	}
	codeGraph, err := repository.GetCodeGraph(ctx, "product", analysis.Repository.ID, "example/api.Save", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(codeGraph.Entities) != 3 || len(codeGraph.Relations) != 2 {
		t.Fatalf("code graph = %#v", codeGraph)
	}
	codeEvents, err := repository.ClaimOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(codeEvents) != 2 {
		t.Fatalf("code outbox events = %#v", codeEvents)
	}
	codeEntityEventFound, graphEventFound := false, false
	for _, event := range codeEvents {
		if event.Topic == "code_entity.upsert" && event.AggregateID == symbols[0].ID {
			codeEntityEventFound = true
		}
		if event.Topic == "code_graph.project" && event.AggregateID == analysis.ID {
			graphEventFound = true
		}
		if err := repository.CompleteOutbox(ctx, event.ID); err != nil {
			t.Fatal(err)
		}
	}
	if !codeEntityEventFound || !graphEventFound {
		t.Fatalf("missing code projection events: %#v", codeEvents)
	}

	// A later revision reuses the logical entity UUID. Milvus therefore
	// overwrites the same primary key while PostgreSQL advances the active head.
	snapshot.Revision = "def"
	snapshot.StartedAt = now.Add(time.Minute)
	snapshot.CompletedAt = now.Add(time.Minute)
	for index := range snapshot.Entities {
		if snapshot.Entities[index].Key == "go:function:example/api.Save" {
			snapshot.Entities[index].Signature = "func Save(ctx context.Context)"
		}
	}
	secondAnalysis, err := repository.StoreCodeGraph(ctx, "product", domain.SoftwareRepository{
		Name: "api", CanonicalURL: "https://git.example/api.git", DefaultBranch: "main",
	}, "codex", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if secondAnalysis.ID == analysis.ID || secondAnalysis.Revision != "def" {
		t.Fatalf("second analysis = %#v", secondAnalysis)
	}
	updated, err := repository.SearchCodeEntitiesLexical(ctx, "product", secondAnalysis.Repository.ID, "Save", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].ID != symbols[0].ID || updated[0].Revision != "def" ||
		updated[0].Signature != "func Save(ctx context.Context)" {
		t.Fatalf("stable entity update = %#v", updated)
	}
	updatedEvents, err := repository.ClaimOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(updatedEvents) != 2 {
		t.Fatalf("updated projection events = %#v", updatedEvents)
	}
	codeEntityEventFound, graphEventFound = false, false
	for _, event := range updatedEvents {
		if event.Topic == "code_entity.upsert" && event.AggregateID == symbols[0].ID {
			codeEntityEventFound = true
		}
		if event.Topic == "code_graph.project" && event.AggregateID == secondAnalysis.ID {
			graphEventFound = true
		}
	}
	if !codeEntityEventFound || !graphEventFound {
		t.Fatalf("missing updated code projection events: %#v", updatedEvents)
	}
}
