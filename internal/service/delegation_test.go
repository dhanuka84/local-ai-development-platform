package service

import (
	"context"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

func TestDelegatedToolBoundary(t *testing.T) {
	d := &domain.TaskDelegation{ID: "delegation", ProjectID: "p", WorkflowID: "w", TaskID: "t", ExpiresAt: time.Now().Add(time.Hour)}
	p := domain.Principal{ID: "delegate", RoleBindings: map[string][]string{"p": {"development"}}, Delegation: d}
	ctx := identity.WithPrincipal(context.Background(), p)
	svc := &Service{}
	scope := domain.OperationScope{ProjectID: "p", WorkflowID: "w", TaskID: "t"}
	for _, tc := range []struct {
		name, input string
		allowed     bool
	}{
		{"knowledge_search", `{"project_id":"p","query":"lesson"}`, true},
		{"knowledge_search", `{"query":"lesson"}`, false},
		{"knowledge_search", `{"project_id":"","query":"lesson"}`, false},
		{"knowledge_get", `{"id":"approved"}`, true},
		{"knowledge_get", `{}`, false},
		{"workflow_task_get", `{"task_id":"t"}`, true},
		{"workflow_task_get", `{"task_id":"other"}`, false},
		{"workflow_run_get", `{"workflow_id":"other"}`, false},
		{"generation_capture", `{"project_id":"p","workflow_id":"w","provider":"ollama"}`, true},
		{"generation_capture", `{"project_id":"other","workflow_id":"w","provider":"ollama"}`, false},
		{"generation_capture", `{"project_id":"p","workflow_id":"w","provider":"openai"}`, false},
		{"generation_capture", `{"project_id":"p","provider":"ollama"}`, false},
		{"workflow_task_transition", `{"task_id":"t","provider":"ollama","event_type":"VALIDATION_FAILED"}`, true},
		{"workflow_task_transition", `{"provider":"ollama","event_type":"VALIDATION_PASSED"}`, false},
		{"workflow_task_transition", `{"task_id":"","provider":"ollama","event_type":"LOCAL_REVISION_RECORDED"}`, false},
		{"workflow_task_transition", `{"task_id":"t","provider":"ollama","event_type":"LEARNING_PROMOTED"}`, false},
		{"workflow_task_begin", `{"workflow_id":"w"}`, false},
		{"knowledge_candidate_decide", `{"project_id":"p"}`, false},
		{"knowledge_validation_record", `{"project_id":"p"}`, false},
		{"context_definition_decide", `{"project_id":"p"}`, false},
		{"new_unreviewed_tool", `{"project_id":"p"}`, false},
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
