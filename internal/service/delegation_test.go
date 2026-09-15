package service

import (
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

func TestDelegatedToolBoundary(t *testing.T) {
	d := &domain.TaskDelegation{ID: "delegation", ProjectID: "p", WorkflowID: "w", TaskID: "t", ExpiresAt: time.Now().Add(time.Hour)}
	p := domain.Principal{ID: "delegate", RoleBindings: map[string][]string{"p": {"development"}}, Delegation: d}
	ctx := identity.WithPrincipal(t.Context(), p)
	svc := &Service{}
	scope := domain.OperationScope{ProjectID: "p", WorkflowID: "w", TaskID: "t"}
	for _, tc := range []struct {
		name, input string
		allowed     bool
	}{
		{name: "knowledge_search", input: `{"project_id":"p","query":"lesson"}`, allowed: true},
		{name: "knowledge_search", input: `{"query":"lesson"}`, allowed: false},
		{name: "knowledge_search", input: `{"project_id":"","query":"lesson"}`, allowed: false},
		{name: "knowledge_get", input: `{"id":"approved"}`, allowed: true},
		{name: "knowledge_get", input: `{}`, allowed: false},
		{name: "workflow_task_get", input: `{"task_id":"t"}`, allowed: true},
		{name: "workflow_task_get", input: `{"task_id":"other"}`, allowed: false},
		{name: "workflow_run_get", input: `{"workflow_id":"other"}`, allowed: false},
		{name: "generation_capture", input: `{"project_id":"p","workflow_id":"w","provider":"ollama"}`, allowed: true},
		{name: "generation_capture", input: `{"project_id":"other","workflow_id":"w","provider":"ollama"}`, allowed: false},
		{name: "generation_capture", input: `{"project_id":"p","workflow_id":"w","provider":"openai"}`, allowed: false},
		{name: "generation_capture", input: `{"project_id":"p","provider":"ollama"}`, allowed: false},
		{name: "workflow_task_transition", input: `{"task_id":"t","provider":"ollama","event_type":"VALIDATION_FAILED"}`, allowed: true},
		{name: "workflow_task_transition", input: `{"provider":"ollama","event_type":"VALIDATION_PASSED"}`, allowed: false},
		{name: "workflow_task_transition", input: `{"task_id":"","provider":"ollama","event_type":"LOCAL_REVISION_RECORDED"}`, allowed: false},
		{name: "workflow_task_transition", input: `{"task_id":"t","provider":"ollama","event_type":"LEARNING_PROMOTED"}`, allowed: false},
		{name: "workflow_task_begin", input: `{"workflow_id":"w"}`, allowed: false},
		{name: "knowledge_candidate_decide", input: `{"project_id":"p"}`, allowed: false},
		{name: "knowledge_validation_record", input: `{"project_id":"p"}`, allowed: false},
		{name: "context_definition_decide", input: `{"project_id":"p"}`, allowed: false},
		{name: "new_unreviewed_tool", input: `{"project_id":"p"}`, allowed: false},
	} {
		t.Run(tc.name+tc.input, func(t *testing.T) {
			if got := svc.checkDelegatedTool(ctx, tc.name, []byte(tc.input), scope) == nil; got != tc.allowed {
				t.Fatalf("allowed=%v want=%v", got, tc.allowed)
			}
		})
	}
	if svc.checkDelegatedTool(ctx, "knowledge_get", []byte(`{"id":"other-project-item"}`), domain.OperationScope{ProjectID: "other"}) == nil {
		t.Fatal("hydrated cross-project record allowed")
	}
	d.ExpiresAt = time.Now().Add(-time.Second)
	if svc.checkDelegatedTool(ctx, "knowledge_search", []byte(`{"project_id":"p"}`), scope) == nil {
		t.Fatal("expired delegation allowed")
	}
}
