package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

const defaultTaskMatchThreshold = float32(0.75)

type BeginWorkflowTaskInput struct {
	WorkflowID     string
	TaskKey        string
	Title          string
	TaskType       string
	ExecutionMode  string
	RAGQuery       string
	MatchThreshold float32
	IdempotencyKey string
}

type TransitionWorkflowTaskInput struct {
	TaskID                string
	ExpectedVersion       int
	EventType             string
	IdempotencyKey        string
	Provider              string
	Model                 string
	CandidateID           string
	Evidence              string
	ReviewInfluenceWeight float32
	Payload               map[string]any
}

type taskTransitionSpec struct {
	to               string
	roles            []string
	provider         string
	evidenceRequired bool
	completed        bool
}

func (s *Service) BeginWorkflowTask(ctx context.Context, input BeginWorkflowTaskInput) (domain.WorkflowTaskCheckpoint, domain.WorkflowTaskEvent, error) {
	principal, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	input.WorkflowID = strings.TrimSpace(input.WorkflowID)
	input.TaskKey = strings.TrimSpace(input.TaskKey)
	input.Title = strings.TrimSpace(input.Title)
	input.TaskType = defaultString(strings.ToLower(strings.TrimSpace(input.TaskType)), "implementation")
	input.ExecutionMode = defaultString(strings.ToLower(strings.TrimSpace(input.ExecutionMode)), "auto")
	input.RAGQuery = strings.TrimSpace(input.RAGQuery)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.WorkflowID == "" || input.TaskKey == "" || input.Title == "" || input.RAGQuery == "" || input.IdempotencyKey == "" {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: workflow_id, task_key, title, rag_query, and idempotency_key are required", ErrInvalidInput)
	}
	if input.MatchThreshold == 0 {
		input.MatchThreshold = defaultTaskMatchThreshold
	}
	if input.MatchThreshold < 0 || input.MatchThreshold > 1 {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: match_threshold must be between 0 and 1", ErrInvalidInput)
	}
	if !oneOf(input.ExecutionMode, "auto", "manual") {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: execution_mode must be auto or manual", ErrInvalidInput)
	}
	run, err := s.repository.GetWorkflow(ctx, input.WorkflowID)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	if terminalState(run.State) {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: cannot begin a task for terminal workflow state %s", ErrInvalidInput, run.State)
	}
	actorRole, ok := selectRole(principal, run.ProjectID, []string{"controller", "development"})
	if !ok {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: principal lacks a task execution role", ErrForbidden)
	}
	decision, err := s.authorize(ctx, domain.AuthorizationRequest{
		Principal: principal, ResourceKind: "workflow_run", ResourceID: run.ID, Action: "transition",
		Attributes: workflowAttributes(run, "TASK_QUEUED"),
	})
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	requestRecord := map[string]any{
		"workflow_id": run.ID, "project_id": run.ProjectID, "task_key": input.TaskKey,
		"title": input.Title, "task_type": input.TaskType, "execution_mode": input.ExecutionMode, "query": input.RAGQuery,
		"match_threshold": input.MatchThreshold,
	}
	requestJSON, err := json.Marshal(requestRecord)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	requestArtifact, err := s.artifacts.Put(ctx, requestJSON, "application/json")
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	taskID, err := domain.NewID()
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	eventID, err := domain.NewID()
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	task := domain.WorkflowTaskCheckpoint{
		ID: taskID, WorkflowID: run.ID, ProjectID: run.ProjectID, TaskKey: input.TaskKey,
		Title: input.Title, TaskType: input.TaskType, State: domain.TaskStateQueued,
		Route: domain.TaskRoutePending, ExecutionMode: input.ExecutionMode, Version: 1, RAGQuery: input.RAGQuery, RAGBackend: "pending",
		RAGHitIDs: []string{}, MatchThreshold: input.MatchThreshold,
		RequestArtifact: requestArtifact, CreatedBy: principal.ID,
	}
	event := domain.WorkflowTaskEvent{
		ID: eventID, TaskID: task.ID, EventType: "TASK_QUEUED", FromState: "", ToState: task.State,
		ActorPrincipalID: principal.ID, ActorRole: actorRole, IdempotencyKey: input.IdempotencyKey,
		Payload:          map[string]any{"route": domain.TaskRoutePending},
		EvidenceArtifact: &requestArtifact, AuthorizationDecision: "allow", CerbosCallID: decision.CallID,
		PolicyVersion: decision.PolicyVersion,
	}
	created, queuedEvent, err := s.repository.CreateWorkflowTask(ctx, task, event)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	activated, activationEvent, ok, err := s.activateWorkflowQueueHead(ctx, run.ID)
	if err != nil {
		return created, queuedEvent, err
	}
	if ok && activated.ID == created.ID {
		return activated, activationEvent, nil
	}
	return created, queuedEvent, nil
}

func taskRoute(run domain.WorkflowRun, backend string, strongHit bool, taskType string) string {
	if backend == "milvus" && strongHit {
		return domain.TaskRouteRAGHit
	}
	if run.Kind == "software-development" && taskType != "maintenance" &&
		(run.DataClassification == "public" || run.DataClassification == "internal") {
		return domain.TaskRouteRAGMissCloudReview
	}
	return domain.TaskRouteRAGMissLocalOnly
}

func (s *Service) GetWorkflowTask(ctx context.Context, taskID string) (domain.WorkflowTaskCheckpoint, error) {
	principal, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, err
	}
	task, err := s.repository.GetWorkflowTask(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, err
	}
	run, err := s.repository.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, err
	}
	if _, err := s.authorize(ctx, domain.AuthorizationRequest{
		Principal: principal, ResourceKind: "workflow_run", ResourceID: run.ID, Action: "read",
		Attributes: workflowAttributes(run, "TASK_READ"),
	}); err != nil {
		return domain.WorkflowTaskCheckpoint{}, err
	}
	return task, nil
}

func (s *Service) GetWorkflowTaskByCandidate(ctx context.Context, candidateID string) (domain.WorkflowTaskCheckpoint, error) {
	return s.repository.GetWorkflowTaskByCandidate(ctx, strings.TrimSpace(candidateID))
}

func (s *Service) TransitionWorkflowTask(ctx context.Context, input TransitionWorkflowTaskInput) (domain.WorkflowTaskCheckpoint, domain.WorkflowTaskEvent, error) {
	principal, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.EventType = strings.ToUpper(strings.TrimSpace(input.EventType))
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.Model = strings.TrimSpace(input.Model)
	input.CandidateID = strings.TrimSpace(input.CandidateID)
	input.Evidence = strings.TrimSpace(input.Evidence)
	if input.TaskID == "" || input.ExpectedVersion < 1 || input.EventType == "" || input.IdempotencyKey == "" {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: task_id, expected_version, event_type, and idempotency_key are required", ErrInvalidInput)
	}
	task, err := s.repository.GetWorkflowTask(ctx, input.TaskID)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	run, err := s.repository.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	spec, ok := workflowTaskTransition(task, input.EventType)
	if !ok {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: event %s is invalid from task state %s", ErrInvalidInput, input.EventType, task.State)
	}
	actorRole, ok := selectRole(principal, run.ProjectID, spec.roles)
	if !ok {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: principal lacks the required role for %s", ErrForbidden, input.EventType)
	}
	if spec.provider != "" {
		if input.Provider != spec.provider || input.Model == "" {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: %s requires provider %s and an explicit model", ErrInvalidInput, input.EventType, spec.provider)
		}
	}
	if spec.evidenceRequired && input.Evidence == "" {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: %s requires immutable evidence", ErrInvalidInput, input.EventType)
	}
	if input.ReviewInfluenceWeight < 0 || input.ReviewInfluenceWeight > 1 {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: review_influence_weight must be between 0 and 1", ErrInvalidInput)
	}
	var activationRoute, activationBackend string
	var activationHitIDs []string
	var activationMaxScore float32
	var lookupArtifact *domain.Artifact
	if input.EventType == "TASK_ACTIVATED" {
		head, activatable, lookupErr := s.repository.GetActivatableWorkflowTask(ctx, task.WorkflowID)
		if lookupErr != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, lookupErr
		}
		if !activatable || head.ID != task.ID {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: only the FIFO queue head can be activated", ErrInvalidInput)
		}
		hits, backend, searchErr := s.Search(ctx, run.ProjectID, task.RAGQuery, 10)
		if searchErr != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("RAG lookup failed closed: %w", searchErr)
		}
		activationBackend = backend
		for _, hit := range hits {
			activationHitIDs = append(activationHitIDs, hit.ID)
			if hit.Score > activationMaxScore {
				activationMaxScore = hit.Score
			}
		}
		activationRoute = taskRoute(run, backend, len(hits) > 0 && activationMaxScore >= task.MatchThreshold, task.TaskType)
		lookupJSON, marshalErr := json.Marshal(map[string]any{
			"workflow_id": run.ID, "project_id": run.ProjectID, "task_id": task.ID,
			"query": task.RAGQuery, "backend": backend, "match_threshold": task.MatchThreshold,
			"route": activationRoute, "results": hits,
		})
		if marshalErr != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, marshalErr
		}
		artifact, artifactErr := s.artifacts.Put(ctx, lookupJSON, "application/json")
		if artifactErr != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, artifactErr
		}
		lookupArtifact = &artifact
		input.Evidence = string(lookupJSON)
		input.Payload = map[string]any{
			"route": activationRoute, "rag_backend": backend,
			"rag_hit_ids": activationHitIDs, "rag_max_score": activationMaxScore,
		}
	}

	candidateID := task.CandidateID
	if input.EventType == "LOCAL_RESULT_RECORDED" {
		candidateID = input.CandidateID
		if candidateID == "" {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: LOCAL_RESULT_RECORDED requires candidate_id from generation_capture", ErrInvalidInput)
		}
	}
	var candidate domain.KnowledgeItem
	if candidateID != "" {
		candidate, err = s.repository.GetKnowledge(ctx, candidateID, true)
		if err != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
		}
		if candidate.WorkflowID != task.WorkflowID || candidate.ProjectID != task.ProjectID {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: candidate is not linked to this workflow and project", ErrInvalidInput)
		}
	}
	if input.EventType == "LOCAL_REVISION_RECORDED" && (candidate.Status != domain.CandidatePending || candidate.Version < 2) {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: local revision must update the pending candidate before this checkpoint", ErrInvalidInput)
	}
	if input.EventType == "CLOUD_REVIEW_RECORDED" {
		reviewArtifactSHA256 := payloadString(input.Payload, "review_artifact_sha256")
		contextManifestArtifactSHA256 := payloadString(input.Payload, "context_manifest_artifact_sha256")
		if candidateID == "" || reviewArtifactSHA256 == "" || contextManifestArtifactSHA256 == "" {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: cloud review requires candidate and immutable review/context artifact hashes", ErrInvalidInput)
		}
		exists, evidenceErr := s.repository.ReviewEvidenceExists(
			ctx, candidateID, task.WorkflowID, input.Provider, input.Model,
			reviewArtifactSHA256, contextManifestArtifactSHA256,
		)
		if evidenceErr != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, evidenceErr
		}
		if !exists {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: cloud review checkpoint does not match a persisted immutable review record", ErrInvalidInput)
		}
		if input.ReviewInfluenceWeight == 0 {
			input.ReviewInfluenceWeight = 0.8
		}
	}
	if input.EventType == "VALIDATION_PASSED" && candidate.Status != domain.CandidatePending {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: validation requires a pending candidate", ErrInvalidInput)
	}
	if input.EventType == "LEARNING_PROMOTED" && candidate.Status != domain.CandidateApproved {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("%w: candidate must be approved before promotion is recorded", ErrInvalidInput)
	}
	if input.EventType == "RAG_READBACK_VERIFIED" {
		hits, backend, searchErr := s.Search(ctx, task.ProjectID, task.RAGQuery, 25)
		if searchErr != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("RAG read-back failed closed: %w", searchErr)
		}
		found := false
		for _, hit := range hits {
			if hit.ID == task.CandidateID {
				found = true
				break
			}
		}
		if backend != "milvus" || !found {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("RAG read-back failed closed: promoted candidate is not retrievable from Milvus")
		}
		readback, marshalErr := json.Marshal(map[string]any{"backend": backend, "candidate_id": task.CandidateID, "results": hits})
		if marshalErr != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, marshalErr
		}
		input.Evidence = string(readback)
	}
	decision, err := s.authorize(ctx, domain.AuthorizationRequest{
		Principal: principal, ResourceKind: "workflow_run", ResourceID: run.ID, Action: "transition",
		Attributes: workflowAttributes(run, input.EventType),
	})
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	var evidenceArtifact *domain.Artifact
	if input.Evidence != "" {
		artifact, err := s.artifacts.Put(ctx, []byte(input.Evidence), "application/json")
		if err != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
		}
		evidenceArtifact = &artifact
	}
	eventID, err := domain.NewID()
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	event := domain.WorkflowTaskEvent{
		ID: eventID, TaskID: task.ID, EventType: input.EventType, FromState: task.State, ToState: spec.to,
		ActorPrincipalID: principal.ID, ActorRole: actorRole, Provider: input.Provider, Model: input.Model,
		IdempotencyKey: input.IdempotencyKey, Payload: nonNilMap(input.Payload), EvidenceArtifact: evidenceArtifact,
		AuthorizationDecision: "allow", CerbosCallID: decision.CallID, PolicyVersion: decision.PolicyVersion,
	}
	transition := domain.WorkflowTaskTransition{
		TaskID: task.ID, ExpectedVersion: input.ExpectedVersion, ExpectedState: task.State,
		ResultingState: spec.to, CandidateID: candidateID, InfluenceWeight: input.ReviewInfluenceWeight,
		RAGRoute: activationRoute, RAGBackend: activationBackend, RAGHitIDs: activationHitIDs,
		RAGMaxScore: activationMaxScore, LookupArtifact: lookupArtifact,
		Completed: spec.completed, Event: event,
	}
	if input.Provider == "ollama" {
		transition.LocalProvider, transition.LocalModel = input.Provider, input.Model
	}
	if input.Provider == "openai" {
		transition.CloudProvider, transition.CloudModel = input.Provider, input.Model
	}
	updated, recorded, err := s.repository.TransitionWorkflowTask(ctx, transition)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	if spec.completed {
		if _, _, _, activationErr := s.activateWorkflowQueueHead(ctx, task.WorkflowID); activationErr != nil {
			return updated, recorded, fmt.Errorf("task reached %s but activating the next queued task failed: %w", updated.State, activationErr)
		}
	}
	return updated, recorded, nil
}

func workflowTaskTransition(task domain.WorkflowTaskCheckpoint, event string) (taskTransitionSpec, bool) {
	switch task.State {
	case domain.TaskStateQueued:
		switch event {
		case "TASK_ACTIVATED":
			return taskTransitionSpec{to: domain.TaskStateLocalExecution, roles: []string{"development", "controller"}}, true
		case "TASK_REJECTED":
			return taskTransitionSpec{to: domain.TaskStateRejected, roles: []string{"product_owner", "operations"}, evidenceRequired: true, completed: true}, true
		}
	case domain.TaskStateLocalExecution:
		if event == "LOCAL_RESULT_RECORDED" {
			to := domain.TaskStateValidationRequired
			if task.Route == domain.TaskRouteRAGMissCloudReview {
				to = domain.TaskStateCloudReviewRequired
				if task.ExecutionMode == "manual" {
					to = domain.TaskStateReviewApprovalRequired
				}
			}
			return taskTransitionSpec{to: to, roles: []string{"development", "controller"}, provider: "ollama", evidenceRequired: true}, true
		}
	case domain.TaskStateReviewApprovalRequired:
		switch event {
		case "CLOUD_REVIEW_APPROVED":
			return taskTransitionSpec{to: domain.TaskStateCloudReviewRequired, roles: []string{"product_owner"}, evidenceRequired: true}, true
		case "CLOUD_REVIEW_REJECTED":
			return taskTransitionSpec{to: domain.TaskStateBlocked, roles: []string{"product_owner"}, evidenceRequired: true}, true
		}
	case domain.TaskStateCloudReviewRequired:
		if event == "CLOUD_REVIEW_RECORDED" {
			return taskTransitionSpec{to: domain.TaskStateLocalRevisionRequired, roles: []string{"cloud_reviewer", "development", "controller"}, provider: "openai", evidenceRequired: true}, true
		}
	case domain.TaskStateLocalRevisionRequired:
		if event == "LOCAL_REVISION_RECORDED" {
			return taskTransitionSpec{to: domain.TaskStateValidationRequired, roles: []string{"development", "controller"}, provider: "ollama", evidenceRequired: true}, true
		}
	case domain.TaskStateValidationRequired:
		switch event {
		case "VALIDATION_PASSED":
			return taskTransitionSpec{to: domain.TaskStatePromotionRequired, roles: []string{"development", "qa", "controller"}, provider: "ollama", evidenceRequired: true}, true
		case "VALIDATION_FAILED":
			return taskTransitionSpec{to: domain.TaskStateLocalRevisionRequired, roles: []string{"development", "qa", "controller"}, provider: "ollama", evidenceRequired: true}, true
		}
	case domain.TaskStatePromotionRequired:
		if event == "LEARNING_PROMOTED" {
			return taskTransitionSpec{to: domain.TaskStateRAGReadbackRequired, roles: []string{"product_owner"}, evidenceRequired: true}, true
		}
	case domain.TaskStateRAGReadbackRequired:
		if event == "RAG_READBACK_VERIFIED" {
			return taskTransitionSpec{to: domain.TaskStateCompleted, roles: []string{"development", "controller"}, completed: true}, true
		}
	}
	return taskTransitionSpec{}, false
}

func (s *Service) activateWorkflowQueueHead(ctx context.Context, workflowID string) (domain.WorkflowTaskCheckpoint, domain.WorkflowTaskEvent, bool, error) {
	head, ok, err := s.repository.GetActivatableWorkflowTask(ctx, workflowID)
	if err != nil || !ok {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, false, err
	}
	updated, event, err := s.TransitionWorkflowTask(ctx, TransitionWorkflowTaskInput{
		TaskID: head.ID, ExpectedVersion: head.Version, EventType: "TASK_ACTIVATED",
		IdempotencyKey: fmt.Sprintf("%s:activate:%d", head.ID, head.Version),
	})
	if err == nil {
		return updated, event, true, nil
	}
	current, currentErr := s.repository.GetWorkflowTask(ctx, head.ID)
	if currentErr == nil && current.State != domain.TaskStateQueued {
		// Another submitter activated or dispositioned the same FIFO head after
		// our read. Queue submission remains successful and idempotent.
		return current, domain.WorkflowTaskEvent{}, false, nil
	}
	return updated, event, false, err
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}
