package service

import (
	"context"
	"errors"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

type fakeAuthorizer struct {
	decision domain.AuthorizationDecision
	err      error
	requests []domain.AuthorizationRequest
}

func (f *fakeAuthorizer) Authorize(_ context.Context, request domain.AuthorizationRequest) (domain.AuthorizationDecision, error) {
	f.requests = append(f.requests, request)
	return f.decision, f.err
}

func (f *fakeAuthorizer) Ping(context.Context) error { return f.err }

func soloDeveloper() domain.Principal {
	return domain.Principal{
		ID: "human:local-developer", DisplayName: "Local developer", Human: true,
		RoleBindings: map[string][]string{"*": {"development", "qa", "product_owner", "operations"}},
	}
}

func workflowService(repository *fakeRepository, artifacts *fakeArtifacts, authorizer *fakeAuthorizer) *Service {
	service := New(repository, artifacts, &fakeEmbedder{}, &fakeVectors{}, true, false)
	if err := service.ConfigureAuthorization(authorizer, true); err != nil {
		panic(err)
	}
	return service
}

func TestSoloDeveloperCreatesAndCoordinatesWorkflow(t *testing.T) {
	repository := &fakeRepository{}
	artifacts := &fakeArtifacts{}
	authorizer := &fakeAuthorizer{decision: domain.AuthorizationDecision{Allowed: true, CallID: "call-1", PolicyVersion: "default"}}
	service := workflowService(repository, artifacts, authorizer)
	ctx := identity.WithPrincipal(context.Background(), soloDeveloper())

	run, err := service.CreateWorkflow(ctx, CreateWorkflowInput{
		ProjectID: "product", Request: "Implement the feature", IdempotencyKey: "issue-42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.CreatedBy != "human:local-developer" || repository.workflowEvent.ActorRole != "development" {
		t.Fatalf("run=%#v event=%#v", run, repository.workflowEvent)
	}
	if artifacts.count != 1 || authorizer.requests[0].Action != "create" {
		t.Fatalf("artifacts=%d authorization=%#v", artifacts.count, authorizer.requests)
	}

	updated, event, err := service.TransitionWorkflow(ctx, TransitionWorkflowInput{
		WorkflowID: run.ID, ExpectedVersion: 1, EventType: "CLASSIFIED", IdempotencyKey: "issue-42:classified",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != "classified" || event.ActorRole != "development" || !repository.transitioned {
		t.Fatalf("updated=%#v event=%#v", updated, event)
	}
}

func TestSoloDeveloperUsesExplicitQARoleAtHumanGate(t *testing.T) {
	repository := &fakeRepository{workflow: domain.WorkflowRun{
		ID: "workflow", ProjectID: "product", State: "awaiting_human_qa", Version: 7,
		ImplementedBy: "human:local-developer",
		Governance:    domain.GovernancePolicy{ProjectID: "product", Profile: domain.GovernanceSolo, AllowRoleOverlap: true},
	}}
	artifacts := &fakeArtifacts{}
	authorizer := &fakeAuthorizer{decision: domain.AuthorizationDecision{Allowed: true}}
	service := workflowService(repository, artifacts, authorizer)
	ctx := identity.WithPrincipal(context.Background(), soloDeveloper())

	updated, event, err := service.TransitionWorkflow(ctx, TransitionWorkflowInput{
		WorkflowID: "workflow", ExpectedVersion: 7, EventType: "QA_VALIDATED",
		IdempotencyKey: "workflow:qa:1", Evidence: `{"checks":["go test ./..."]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != "qa_validated" || updated.QAValidatedBy != "human:local-developer" || event.ActorRole != "qa" {
		t.Fatalf("updated=%#v event=%#v", updated, event)
	}
	if got := authorizer.requests[0]; got.ResourceKind != "workflow_gate" || got.Action != "qa_decide" {
		t.Fatalf("authorization=%#v", got)
	}
}

func TestRegulatedGovernanceRejectsSameImplementerAtQAGate(t *testing.T) {
	repository := &fakeRepository{workflow: domain.WorkflowRun{
		ID: "workflow", ProjectID: "product", State: "awaiting_human_qa", Version: 7,
		ImplementedBy: "human:local-developer",
		Governance:    domain.GovernancePolicy{ProjectID: "product", Profile: domain.GovernanceRegulated, AllowRoleOverlap: false},
	}}
	artifacts := &fakeArtifacts{}
	authorizer := &fakeAuthorizer{decision: domain.AuthorizationDecision{Allowed: true}}
	service := workflowService(repository, artifacts, authorizer)
	ctx := identity.WithPrincipal(context.Background(), soloDeveloper())

	_, _, err := service.TransitionWorkflow(ctx, TransitionWorkflowInput{
		WorkflowID: "workflow", ExpectedVersion: 7, EventType: "QA_VALIDATED",
		IdempotencyKey: "workflow:qa:1", Evidence: `{}`,
	})
	if !errors.Is(err, ErrForbidden) || repository.transitioned || artifacts.count != 0 || len(authorizer.requests) != 0 {
		t.Fatalf("err=%v transitioned=%v artifacts=%d requests=%d", err, repository.transitioned, artifacts.count, len(authorizer.requests))
	}
}

func TestWorkflowAuthorizationFailureIsFailClosed(t *testing.T) {
	repository := &fakeRepository{workflow: domain.WorkflowRun{
		ID: "workflow", ProjectID: "product", State: "implementing", Version: 3,
		Governance: domain.GovernancePolicy{ProjectID: "product", Profile: domain.GovernanceSolo},
	}}
	artifacts := &fakeArtifacts{}
	authorizer := &fakeAuthorizer{err: errors.New("pdp unavailable")}
	service := workflowService(repository, artifacts, authorizer)
	ctx := identity.WithPrincipal(context.Background(), soloDeveloper())

	_, _, err := service.TransitionWorkflow(ctx, TransitionWorkflowInput{
		WorkflowID: "workflow", ExpectedVersion: 3, EventType: "IMPLEMENTATION_COMPLETED",
		IdempotencyKey: "workflow:implementation:1", Evidence: `{}`,
	})
	if !errors.Is(err, ErrAuthorizationUnavailable) || repository.transitioned || artifacts.count != 0 {
		t.Fatalf("err=%v transitioned=%v artifacts=%d", err, repository.transitioned, artifacts.count)
	}
}

func TestNonHumanPrincipalCannotDecideHumanGate(t *testing.T) {
	repository := &fakeRepository{workflow: domain.WorkflowRun{
		ID: "workflow", ProjectID: "product", State: "awaiting_product_approval", Version: 9,
		Governance: domain.GovernancePolicy{ProjectID: "product", Profile: domain.GovernanceSolo},
	}}
	service := workflowService(repository, &fakeArtifacts{}, &fakeAuthorizer{decision: domain.AuthorizationDecision{Allowed: true}})
	ctx := identity.WithPrincipal(context.Background(), domain.Principal{
		ID: "agent:po", Human: false, RoleBindings: map[string][]string{"product": {"product_owner"}},
	})

	_, _, err := service.TransitionWorkflow(ctx, TransitionWorkflowInput{
		WorkflowID: "workflow", ExpectedVersion: 9, EventType: "PRODUCT_APPROVED",
		IdempotencyKey: "workflow:product:1", Evidence: `{}`,
	})
	if !errors.Is(err, ErrForbidden) || repository.transitioned {
		t.Fatalf("err=%v transitioned=%v", err, repository.transitioned)
	}
}

func TestProjectActionUsesAuthenticatedIdentityAndProjectContext(t *testing.T) {
	authorizer := &fakeAuthorizer{decision: domain.AuthorizationDecision{Allowed: true}}
	service := workflowService(&fakeRepository{}, &fakeArtifacts{}, authorizer)
	principal, err := service.AuthorizeProjectAction(
		identity.WithPrincipal(context.Background(), soloDeveloper()),
		"product", "knowledge_candidate", "candidate", "approve", map[string]any{"status": "pending"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if principal.ID != "human:local-developer" || len(authorizer.requests) != 1 {
		t.Fatalf("principal=%#v requests=%#v", principal, authorizer.requests)
	}
	request := authorizer.requests[0]
	if request.Principal.ID != principal.ID || request.Attributes["project_id"] != "product" || request.Action != "approve" {
		t.Fatalf("request=%#v", request)
	}
}
