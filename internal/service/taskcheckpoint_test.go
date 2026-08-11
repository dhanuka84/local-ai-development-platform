package service

import (
	"context"
	"errors"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

func checkpointWorkflow(classification string) domain.WorkflowRun {
	return domain.WorkflowRun{
		ID: "workflow", ProjectID: "product", Kind: "software-development", State: "implementing",
		Version: 3, Risk: "low", DataClassification: classification,
		Governance: domain.GovernancePolicy{ProjectID: "product", Profile: domain.GovernanceSolo},
	}
}

func checkpointService(repository *fakeRepository, vectors *fakeVectors) (*Service, context.Context) {
	svc := New(repository, &fakeArtifacts{}, &fakeEmbedder{vectors: [][]float32{{0.1, 0.2}}}, vectors, true, false)
	if err := svc.ConfigureAuthorization(&fakeAuthorizer{decision: domain.AuthorizationDecision{Allowed: true}}, true); err != nil {
		panic(err)
	}
	return svc, identity.WithPrincipal(context.Background(), soloDeveloper())
}

func TestWorkflowTaskQueueActivatesFirstAndQueuesSecond(t *testing.T) {
	repository := &fakeRepository{workflow: checkpointWorkflow("internal")}
	svc, ctx := checkpointService(repository, &fakeVectors{})

	first, event, err := svc.BeginWorkflowTask(ctx, BeginWorkflowTaskInput{
		WorkflowID: "workflow", TaskKey: "design", Title: "Design UI tests", TaskType: "design",
		RAGQuery: "validated UI test architecture", IdempotencyKey: "task:design",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.State != domain.TaskStateLocalExecution || first.Route != domain.TaskRouteRAGMissCloudReview || first.ExecutionMode != "auto" || event.EventType != "TASK_ACTIVATED" {
		t.Fatalf("first=%#v event=%#v", first, event)
	}

	second, event, err := svc.BeginWorkflowTask(ctx, BeginWorkflowTaskInput{
		WorkflowID: "workflow", TaskKey: "implement", Title: "Implement UI tests", TaskType: "implementation",
		RAGQuery: "validated Playwright implementation", IdempotencyKey: "task:implement",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.State != domain.TaskStateQueued || second.Route != domain.TaskRoutePending || event.EventType != "TASK_QUEUED" || second.Ordinal != 2 {
		t.Fatalf("second=%#v event=%#v", second, event)
	}
}

func TestManualModePausesBeforeCloudReview(t *testing.T) {
	repository := &fakeRepository{workflow: checkpointWorkflow("internal")}
	svc, ctx := checkpointService(repository, &fakeVectors{})
	task, _, err := svc.BeginWorkflowTask(ctx, BeginWorkflowTaskInput{
		WorkflowID: "workflow", TaskKey: "manual", Title: "Manual review task", ExecutionMode: "manual",
		RAGQuery: "new manual task", IdempotencyKey: "task:manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	repository.knowledge = map[string]domain.KnowledgeItem{
		"candidate": {ID: "candidate", ProjectID: "product", WorkflowID: "workflow", Status: domain.CandidatePending, Version: 1},
	}
	updated, _, err := svc.TransitionWorkflowTask(ctx, TransitionWorkflowTaskInput{
		TaskID: task.ID, ExpectedVersion: task.Version, EventType: "LOCAL_RESULT_RECORDED",
		IdempotencyKey: "manual:local", Provider: "ollama", Model: "qwen3.6:35b",
		CandidateID: "candidate", Evidence: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != domain.TaskStateReviewApprovalRequired {
		t.Fatalf("state=%s", updated.State)
	}
}

func TestWorkflowTaskStrongRAGHitSkipsCloudReview(t *testing.T) {
	repository := &fakeRepository{
		workflow: checkpointWorkflow("internal"),
		items:    []domain.KnowledgeItem{{ID: "lesson", ProjectID: "product", Status: domain.CandidateApproved}},
	}
	svc, ctx := checkpointService(repository, &fakeVectors{hits: []domain.VectorHit{{ID: "lesson", Score: 0.91}}})
	task, _, err := svc.BeginWorkflowTask(ctx, BeginWorkflowTaskInput{
		WorkflowID: "workflow", TaskKey: "design", Title: "Design", RAGQuery: "known design",
		IdempotencyKey: "task:design", MatchThreshold: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Route != domain.TaskRouteRAGHit || task.RAGBackend != "milvus" || len(task.RAGHitIDs) != 1 {
		t.Fatalf("task=%#v", task)
	}
	repository.knowledge = map[string]domain.KnowledgeItem{
		"candidate": {ID: "candidate", ProjectID: "product", WorkflowID: "workflow", Status: domain.CandidatePending, Version: 1},
	}
	updated, _, err := svc.TransitionWorkflowTask(ctx, TransitionWorkflowTaskInput{
		TaskID: task.ID, ExpectedVersion: task.Version, EventType: "LOCAL_RESULT_RECORDED",
		IdempotencyKey: "task:design:local", Provider: "ollama", Model: "qwen3.6:35b",
		CandidateID: "candidate", Evidence: `{"result":"captured"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != domain.TaskStateValidationRequired {
		t.Fatalf("state=%s", updated.State)
	}
}

func TestWorkflowTaskRestrictedMissStaysLocalOnly(t *testing.T) {
	repository := &fakeRepository{workflow: checkpointWorkflow("restricted")}
	svc, ctx := checkpointService(repository, &fakeVectors{})
	task, _, err := svc.BeginWorkflowTask(ctx, BeginWorkflowTaskInput{
		WorkflowID: "workflow", TaskKey: "secure", Title: "Restricted task", RAGQuery: "restricted task",
		IdempotencyKey: "task:secure",
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Route != domain.TaskRouteRAGMissLocalOnly {
		t.Fatalf("route=%s", task.Route)
	}
}

func TestWorkflowTaskProviderLanesFailClosed(t *testing.T) {
	repository := &fakeRepository{workflow: checkpointWorkflow("internal")}
	svc, ctx := checkpointService(repository, &fakeVectors{})
	task, _, err := svc.BeginWorkflowTask(ctx, BeginWorkflowTaskInput{
		WorkflowID: "workflow", TaskKey: "implement", Title: "Implement", RAGQuery: "new implementation",
		IdempotencyKey: "task:implement",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.TransitionWorkflowTask(ctx, TransitionWorkflowTaskInput{
		TaskID: task.ID, ExpectedVersion: task.Version, EventType: "LOCAL_RESULT_RECORDED",
		IdempotencyKey: "task:wrong-provider", Provider: "openai", Model: "cloud-model",
		CandidateID: "candidate", Evidence: `{}`,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v, want ErrInvalidInput", err)
	}
}

func TestCloudReviewCheckpointRequiresPersistedImmutableReviewEvidence(t *testing.T) {
	repository := &fakeRepository{
		workflow: checkpointWorkflow("internal"),
		tasks: []domain.WorkflowTaskCheckpoint{{
			ID: "cloud", WorkflowID: "workflow", ProjectID: "product", State: domain.TaskStateCloudReviewRequired,
			Route: domain.TaskRouteRAGMissCloudReview, Version: 2, CandidateID: "candidate",
		}},
		knowledge: map[string]domain.KnowledgeItem{
			"candidate": {ID: "candidate", ProjectID: "product", WorkflowID: "workflow", Status: domain.CandidatePending, Version: 1},
		},
	}
	svc, ctx := checkpointService(repository, &fakeVectors{})
	input := TransitionWorkflowTaskInput{
		TaskID: "cloud", ExpectedVersion: 2, EventType: "CLOUD_REVIEW_RECORDED",
		IdempotencyKey: "cloud:review", Provider: "openai", Model: "gpt-5.6-sol",
		Evidence: `{"verdict":"revise"}`, Payload: map[string]any{
			"review_artifact_sha256": "review-sha", "context_manifest_artifact_sha256": "manifest-sha",
		},
	}
	if _, _, err := svc.TransitionWorkflowTask(ctx, input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v, want ErrInvalidInput", err)
	}
	repository.reviewEvidence = true
	updated, _, err := svc.TransitionWorkflowTask(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != domain.TaskStateLocalRevisionRequired {
		t.Fatalf("state=%s", updated.State)
	}
}

func TestCompletedTaskAutomaticallyActivatesQueuedHead(t *testing.T) {
	approved := domain.KnowledgeItem{ID: "candidate", ProjectID: "product", WorkflowID: "workflow", Status: domain.CandidateApproved}
	repository := &fakeRepository{
		workflow: checkpointWorkflow("internal"), items: []domain.KnowledgeItem{approved},
		tasks: []domain.WorkflowTaskCheckpoint{
			{ID: "first", WorkflowID: "workflow", ProjectID: "product", TaskKey: "first", TaskType: "implementation", State: domain.TaskStateRAGReadbackRequired, Route: domain.TaskRouteRAGMissCloudReview, Version: 7, RAGQuery: "validated first lesson", MatchThreshold: 0.75, CandidateID: "candidate"},
			{ID: "second", WorkflowID: "workflow", ProjectID: "product", TaskKey: "second", TaskType: "testing", State: domain.TaskStateQueued, Route: domain.TaskRoutePending, Version: 1, RAGQuery: "validated second lesson", MatchThreshold: 0.75},
		},
	}
	svc, ctx := checkpointService(repository, &fakeVectors{hits: []domain.VectorHit{{ID: "candidate", Score: 0.92}}})
	completed, _, err := svc.TransitionWorkflowTask(ctx, TransitionWorkflowTaskInput{
		TaskID: "first", ExpectedVersion: 7, EventType: "RAG_READBACK_VERIFIED",
		IdempotencyKey: "first:readback",
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != domain.TaskStateCompleted || repository.tasks[1].State != domain.TaskStateLocalExecution {
		t.Fatalf("completed=%#v second=%#v", completed, repository.tasks[1])
	}
}

func TestQueuedTaskCanBeManuallyRejected(t *testing.T) {
	repository := &fakeRepository{
		workflow: checkpointWorkflow("internal"),
		tasks: []domain.WorkflowTaskCheckpoint{
			{ID: "active", WorkflowID: "workflow", ProjectID: "product", State: domain.TaskStateLocalExecution},
			{ID: "queued", WorkflowID: "workflow", ProjectID: "product", State: domain.TaskStateQueued, Version: 1},
		},
	}
	svc, ctx := checkpointService(repository, &fakeVectors{})
	updated, _, err := svc.TransitionWorkflowTask(ctx, TransitionWorkflowTaskInput{
		TaskID: "queued", ExpectedVersion: 1, EventType: "TASK_REJECTED",
		IdempotencyKey: "queued:reject", Evidence: `{"reason":"superseded"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != domain.TaskStateRejected {
		t.Fatalf("state=%s", updated.State)
	}
}
