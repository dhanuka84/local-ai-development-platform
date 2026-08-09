package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

var (
	ErrForbidden                = errors.New("forbidden")
	ErrAuthorizationUnavailable = errors.New("authorization unavailable")
)

type CreateWorkflowInput struct {
	ProjectID          string
	Kind               string
	Risk               string
	DataClassification string
	Request            string
	OpenClawFlowID     string
	IdempotencyKey     string
	Metadata           map[string]any
}

type TransitionWorkflowInput struct {
	WorkflowID      string
	ExpectedVersion int
	EventType       string
	IdempotencyKey  string
	Evidence        string
	Payload         map[string]any
}

type transitionSpec struct {
	to               string
	roles            []string
	gate             string
	action           string
	human            bool
	evidenceRequired bool
	setImplementedBy bool
	setQAValidatedBy bool
	terminal         bool
}

var workflowTransitions = map[string]map[string]transitionSpec{
	"intake": {
		"CLASSIFIED": {to: "classified", roles: []string{"controller", "development"}},
	},
	"classified": {
		"READY":                   {to: "ready", roles: []string{"controller", "development"}},
		"PLAN_APPROVAL_REQUESTED": {to: "awaiting_plan_approval", roles: []string{"controller", "development"}},
		"POLICY_REJECTED":         {to: "policy_rejected", roles: []string{"controller", "product_owner"}, terminal: true},
	},
	"awaiting_plan_approval": {
		"PLAN_APPROVED": {to: "ready", roles: []string{"product_owner"}, gate: "plan_approval", action: "plan_decide", human: true, evidenceRequired: true},
		"PLAN_REJECTED": {to: "policy_rejected", roles: []string{"product_owner"}, gate: "plan_approval", action: "plan_decide", human: true, evidenceRequired: true, terminal: true},
	},
	"ready": {
		"IMPLEMENTATION_STARTED": {to: "implementing", roles: []string{"controller", "development"}},
	},
	"revision_required": {
		"IMPLEMENTATION_STARTED": {to: "implementing", roles: []string{"controller", "development"}},
	},
	"implementing": {
		"IMPLEMENTATION_COMPLETED": {to: "verifying", roles: []string{"development"}, evidenceRequired: true, setImplementedBy: true},
		"IMPLEMENTATION_FAILED":    {to: "implementation_failed", roles: []string{"development"}, evidenceRequired: true},
	},
	"implementation_failed": {
		"REVISION_REQUESTED": {to: "revision_required", roles: []string{"controller", "development"}},
	},
	"verifying": {
		"VERIFICATION_PASSED": {to: "qa_validating", roles: []string{"controller", "development"}, evidenceRequired: true},
		"VERIFICATION_FAILED": {to: "revision_required", roles: []string{"controller", "development"}, evidenceRequired: true},
	},
	"qa_validating": {
		"HUMAN_QA_REQUESTED": {to: "awaiting_human_qa", roles: []string{"controller", "qa"}},
	},
	"awaiting_human_qa": {
		"QA_VALIDATED": {to: "qa_validated", roles: []string{"qa"}, gate: "qa", action: "qa_decide", human: true, evidenceRequired: true, setQAValidatedBy: true},
		"QA_FAILED":    {to: "qa_failed", roles: []string{"qa"}, gate: "qa", action: "qa_decide", human: true, evidenceRequired: true},
	},
	"qa_failed": {
		"REVISION_REQUESTED": {to: "revision_required", roles: []string{"controller", "qa", "development"}},
	},
	"qa_validated": {
		"PRODUCT_APPROVAL_REQUESTED": {to: "awaiting_product_approval", roles: []string{"controller", "product_owner"}},
	},
	"awaiting_product_approval": {
		"PRODUCT_APPROVED": {to: "promotion_pending", roles: []string{"product_owner"}, gate: "product_approval", action: "product_decide", human: true, evidenceRequired: true},
		"PRODUCT_REJECTED": {to: "product_rejected", roles: []string{"product_owner"}, gate: "product_approval", action: "product_decide", human: true, evidenceRequired: true, terminal: true},
	},
	"promotion_pending": {
		"PROMOTION_COMPLETED": {to: "completed", roles: []string{"controller", "product_owner"}, evidenceRequired: true, terminal: true},
	},
}

func (s *Service) AuthenticateToken(ctx context.Context, token string) (domain.Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return domain.Principal{}, identity.ErrUnauthenticated
	}
	hash := sha256.Sum256([]byte(token))
	principal, err := s.repository.AuthenticatePrincipal(ctx, hash[:])
	if err != nil {
		return domain.Principal{}, identity.ErrUnauthenticated
	}
	return principal, nil
}

func (s *Service) CreateWorkflow(ctx context.Context, input CreateWorkflowInput) (domain.WorkflowRun, error) {
	principal, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Kind = defaultString(strings.TrimSpace(input.Kind), "software-development")
	input.Risk = defaultString(strings.ToLower(strings.TrimSpace(input.Risk)), "low")
	input.DataClassification = defaultString(strings.ToLower(strings.TrimSpace(input.DataClassification)), "internal")
	input.Request = strings.TrimSpace(input.Request)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ProjectID == "" || input.Request == "" || input.IdempotencyKey == "" {
		return domain.WorkflowRun{}, fmt.Errorf("%w: project_id, request, and idempotency_key are required", ErrInvalidInput)
	}
	if !oneOf(input.Risk, "low", "medium", "high", "critical") {
		return domain.WorkflowRun{}, fmt.Errorf("%w: unsupported risk %q", ErrInvalidInput, input.Risk)
	}
	if !oneOf(input.DataClassification, "public", "internal", "confidential", "restricted") {
		return domain.WorkflowRun{}, fmt.Errorf("%w: unsupported data classification %q", ErrInvalidInput, input.DataClassification)
	}
	actorRole, ok := selectRole(principal, input.ProjectID, []string{"controller", "development", "product_owner", "operations"})
	if !ok {
		return domain.WorkflowRun{}, fmt.Errorf("%w: principal has no workflow creation role for project", ErrForbidden)
	}
	workflowID, err := domain.NewID()
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	decision, err := s.authorize(ctx, domain.AuthorizationRequest{
		Principal: principal, ResourceKind: "workflow_run", ResourceID: workflowID, Action: "create",
		Attributes: map[string]any{
			"project_id": input.ProjectID, "current_state": "intake", "event": "WORKFLOW_CREATED",
			"risk": input.Risk, "data_classification": input.DataClassification,
		},
	})
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	requestArtifact, err := s.artifacts.Put(ctx, []byte(input.Request), "text/markdown; charset=utf-8")
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	eventID, err := domain.NewID()
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	run := domain.WorkflowRun{
		ID: workflowID, ProjectID: input.ProjectID, Kind: input.Kind, State: "intake", Version: 1,
		Risk: input.Risk, DataClassification: input.DataClassification, RequestArtifact: requestArtifact,
		OpenClawFlowID: strings.TrimSpace(input.OpenClawFlowID), IdempotencyKey: input.IdempotencyKey,
		CreatedBy: principal.ID, Metadata: nonNilMap(input.Metadata),
	}
	event := domain.WorkflowEvent{
		ID: eventID, WorkflowID: workflowID, Sequence: 1, EventType: "WORKFLOW_CREATED",
		FromState: "", ToState: "intake", ActorPrincipalID: principal.ID, ActorRole: actorRole,
		IdempotencyKey: input.IdempotencyKey + ":created", Payload: map[string]any{},
		AuthorizationDecision: "allow", CerbosCallID: decision.CallID, PolicyVersion: decision.PolicyVersion,
	}
	return s.repository.CreateWorkflow(ctx, run, event)
}

func (s *Service) GetWorkflow(ctx context.Context, workflowID string) (domain.WorkflowRun, error) {
	principal, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	run, err := s.repository.GetWorkflow(ctx, strings.TrimSpace(workflowID))
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	if _, err := s.authorize(ctx, domain.AuthorizationRequest{
		Principal: principal, ResourceKind: "workflow_run", ResourceID: run.ID, Action: "read",
		Attributes: workflowAttributes(run, "READ"),
	}); err != nil {
		return domain.WorkflowRun{}, err
	}
	return run, nil
}

func (s *Service) TransitionWorkflow(ctx context.Context, input TransitionWorkflowInput) (domain.WorkflowRun, domain.WorkflowEvent, error) {
	principal, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	input.WorkflowID = strings.TrimSpace(input.WorkflowID)
	input.EventType = strings.ToUpper(strings.TrimSpace(input.EventType))
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.WorkflowID == "" || input.ExpectedVersion < 1 || input.EventType == "" || input.IdempotencyKey == "" {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, fmt.Errorf("%w: workflow_id, expected_version, event_type, and idempotency_key are required", ErrInvalidInput)
	}
	run, err := s.repository.GetWorkflow(ctx, input.WorkflowID)
	if err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	spec, ok := transitionFor(run.State, input.EventType)
	if !ok {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, fmt.Errorf("%w: event %s is invalid from state %s", ErrInvalidInput, input.EventType, run.State)
	}
	actorRole, ok := selectRole(principal, run.ProjectID, spec.roles)
	if !ok {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, fmt.Errorf("%w: principal lacks the required role for %s", ErrForbidden, input.EventType)
	}
	if spec.human && !principal.Human {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, fmt.Errorf("%w: %s is a human gate", ErrForbidden, input.EventType)
	}
	input.Evidence = strings.TrimSpace(input.Evidence)
	if spec.evidenceRequired && input.Evidence == "" {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, fmt.Errorf("%w: %s requires evidence", ErrInvalidInput, input.EventType)
	}
	priorActorIDs := priorActors(run, spec.gate)
	distinctRequired := run.Governance.RequiresDistinctPrincipal(spec.gate)
	if distinctRequired && contains(priorActorIDs, principal.ID) {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, fmt.Errorf("%w: governance requires a distinct principal for %s", ErrForbidden, spec.gate)
	}
	resourceKind, resourceID, action := "workflow_run", run.ID, "transition"
	attributes := workflowAttributes(run, input.EventType)
	if spec.gate != "" {
		resourceKind, resourceID, action = "workflow_gate", fmt.Sprintf("%s:%s:%d", run.ID, spec.gate, run.Version), spec.action
		attributes = map[string]any{
			"project_id": run.ProjectID, "gate": spec.gate, "workflow_state": run.State,
			"evidence_present": input.Evidence != "", "distinct_principal_required": distinctRequired,
			"prior_actor_ids": priorActorIDs,
		}
	}
	decision, err := s.authorize(ctx, domain.AuthorizationRequest{
		Principal: principal, ResourceKind: resourceKind, ResourceID: resourceID, Action: action, Attributes: attributes,
	})
	if err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	var evidenceArtifact *domain.Artifact
	if input.Evidence != "" {
		artifact, err := s.artifacts.Put(ctx, []byte(input.Evidence), "application/json")
		if err != nil {
			return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
		}
		evidenceArtifact = &artifact
	}
	eventID, err := domain.NewID()
	if err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	event := domain.WorkflowEvent{
		ID: eventID, WorkflowID: run.ID, EventType: input.EventType, FromState: run.State, ToState: spec.to,
		ActorPrincipalID: principal.ID, ActorRole: actorRole, IdempotencyKey: input.IdempotencyKey,
		Payload: nonNilMap(input.Payload), EvidenceArtifact: evidenceArtifact,
		AuthorizationDecision: "allow", CerbosCallID: decision.CallID, PolicyVersion: decision.PolicyVersion,
	}
	return s.repository.TransitionWorkflow(ctx, domain.WorkflowTransition{
		WorkflowID: run.ID, ExpectedVersion: input.ExpectedVersion, Event: event,
		ExpectedState: run.State, ResultingState: spec.to, SetImplementedBy: spec.setImplementedBy,
		SetQAValidatedBy: spec.setQAValidatedBy, Terminal: spec.terminal,
	})
}

func (s *Service) authorize(ctx context.Context, request domain.AuthorizationRequest) (domain.AuthorizationDecision, error) {
	if s.authorizer == nil {
		return domain.AuthorizationDecision{}, fmt.Errorf("%w: authorizer is not configured", ErrAuthorizationUnavailable)
	}
	decision, err := s.authorizer.Authorize(ctx, request)
	if err != nil {
		return domain.AuthorizationDecision{}, fmt.Errorf("%w: %v", ErrAuthorizationUnavailable, err)
	}
	if !decision.Allowed {
		return decision, fmt.Errorf("%w: action %s on %s was denied", ErrForbidden, request.Action, request.ResourceKind)
	}
	return decision, nil
}

func transitionFor(state, event string) (transitionSpec, bool) {
	if byEvent, ok := workflowTransitions[state]; ok {
		if spec, ok := byEvent[event]; ok {
			return spec, true
		}
	}
	if event == "CANCEL_REQUESTED" && !terminalState(state) {
		return transitionSpec{to: "cancel_requested", roles: []string{"controller", "operations"}}, true
	}
	if event == "CANCELLED" && state == "cancel_requested" {
		return transitionSpec{to: "cancelled", roles: []string{"controller", "operations"}, terminal: true}, true
	}
	if event == "BLOCKED" && !terminalState(state) {
		return transitionSpec{to: "blocked", roles: []string{"controller", "operations"}, evidenceRequired: true}, true
	}
	if event == "FAILED" && !terminalState(state) {
		return transitionSpec{to: "failed", roles: []string{"controller", "operations"}, evidenceRequired: true, terminal: true}, true
	}
	return transitionSpec{}, false
}

func workflowAttributes(run domain.WorkflowRun, event string) map[string]any {
	return map[string]any{
		"project_id": run.ProjectID, "current_state": run.State, "event": event,
		"risk": run.Risk, "data_classification": run.DataClassification,
	}
}

func selectRole(principal domain.Principal, projectID string, allowed []string) (string, bool) {
	for _, role := range allowed {
		if principal.HasRole(projectID, role) {
			return role, true
		}
	}
	return "", false
}

func priorActors(run domain.WorkflowRun, gate string) []string {
	result := make([]string, 0, 2)
	if gate == "qa" || gate == "product_approval" {
		if run.ImplementedBy != "" {
			result = append(result, run.ImplementedBy)
		}
	}
	if gate == "product_approval" && run.QAValidatedBy != "" && !contains(result, run.QAValidatedBy) {
		result = append(result, run.QAValidatedBy)
	}
	return result
}

func terminalState(state string) bool {
	return oneOf(state, "policy_rejected", "product_rejected", "completed", "cancelled", "failed")
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
