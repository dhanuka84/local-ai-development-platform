package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
)

func exerciseTaskDelegations(t *testing.T, s *Service, r *postgres.Repository, human domain.PrincipalBootstrap, task domain.WorkflowTaskCheckpoint) {
	t.Helper()
	ctx := context.Background()
	p, err := s.AuthenticateToken(ctx, human.Token)
	if err != nil {
		t.Fatal(err)
	}
	actorCtx := identity.WithPrincipal(ctx, p)
	issue := func() (domain.TaskDelegation, string) {
		t.Helper()
		d, token, err := s.IssueTaskCredential(actorCtx, task.ID, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return d, token
	}
	d, token := issue()
	delegate, err := s.AuthenticateToken(ctx, token)
	if err != nil || delegate.Human || delegate.Delegation == nil || delegate.Delegation.DelegatedBy != human.ID || !delegate.HasRole(task.ProjectID, "development") || len(delegate.RolesFor(task.ProjectID)) != 1 || len(delegate.RolesFor("other-project")) != 0 {
		t.Fatal("delegation identity or scope is wrong", err)
	}
	delegateCtx := identity.WithPrincipal(ctx, delegate)
	captureInput := CaptureInput{ProjectID: task.ProjectID, WorkflowID: task.WorkflowID, Prompt: "Synthetic delegated task", Response: "Synthetic pending delegated lesson", Summary: "Synthetic delegated lesson", Provider: "ollama", Model: "synthetic-fixture", TaskType: "maintenance"}
	captureJSON, _ := json.Marshal(map[string]string{"project_id": task.ProjectID, "workflow_id": task.WorkflowID, "provider": "ollama"})
	captureCtx, captureFinish, err := s.BeginToolOperation(delegateCtx, "generation_capture", captureJSON)
	if err != nil {
		t.Fatal(err)
	}
	ownedCandidate, err := s.Capture(captureCtx, captureInput)
	if err != nil {
		t.Fatal(err)
	}
	if err = captureFinish(nil); err != nil {
		t.Fatal(err)
	}
	foreignCandidate, err := s.Capture(actorCtx, captureInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []domain.KnowledgeItem{ownedCandidate, foreignCandidate} {
		payload, _ := json.Marshal(map[string]any{"task_id": task.ID, "candidate_id": candidate.ID, "provider": "ollama", "event_type": "LOCAL_RESULT_RECORDED"})
		_, end, gateErr := s.BeginToolOperation(delegateCtx, "workflow_task_transition", payload)
		if (gateErr == nil) != (candidate.ID == ownedCandidate.ID) {
			t.Fatal("candidate from another actor or task was attachable", gateErr)
		}
		if gateErr == nil {
			if err = end(nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, _, err = s.IssueTaskCredential(delegateCtx, task.ID, time.Minute); err == nil {
		t.Fatal("recursive delegation allowed")
	}
	if _, _, err = s.IssueTaskCredential(actorCtx, task.ID, 61*time.Minute); err == nil {
		t.Fatal("unbounded TTL allowed")
	}
	for _, name := range []string{"workflow_run_create", "knowledge_candidate_decide", "knowledge_validation_record", "context_definition_decide", "knowledge_index_retry"} {
		input, _ := json.Marshal(map[string]string{"project_id": task.ProjectID})
		if _, _, err = s.BeginToolOperation(delegateCtx, name, input); err == nil {
			t.Fatalf("delegation allowed %s", name)
		}
	}
	input, _ := json.Marshal(map[string]string{"task_id": task.ID})
	toolCtx, finish, err := s.BeginToolOperation(delegateCtx, "workflow_task_get", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetWorkflowTask(toolCtx, task.ID); err != nil {
		t.Fatal(err)
	}
	if err = finish(nil); err != nil {
		t.Fatal(err)
	}
	other := identity.WithPrincipal(ctx, domain.Principal{ID: "human:other", Human: true})
	if _, err = s.RevokeTaskCredential(other, d.ID); err == nil {
		t.Fatal("another actor revoked delegation")
	}
	if _, err = s.RevokeTaskCredential(actorCtx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RevokeTaskCredential(actorCtx, d.ID); err != nil {
		t.Fatal("idempotent revoke", err)
	}
	if _, err = s.AuthenticateToken(ctx, token); err == nil {
		t.Fatal("revoked delegation authenticated")
	}
	if _, _, err = s.BeginToolOperation(delegateCtx, "workflow_task_get", input); err == nil {
		t.Fatal("stale authenticated context bypassed revocation")
	}
	d, token = issue()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := r.Pool().Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	refused := func(reason string) {
		t.Helper()
		if _, err := s.AuthenticateToken(ctx, token); err == nil {
			t.Fatal(reason)
		}
	}
	exec(`UPDATE principal_credentials SET revoked_at=now() WHERE id::text=$1`, p.CredentialID)
	refused("revoked parent accepted")
	exec(`UPDATE principal_credentials SET revoked_at=NULL WHERE id::text=$1`, p.CredentialID)
	exec(`UPDATE principal_role_bindings SET valid_until=now()-interval '1 second' WHERE principal_id=$1 AND role='development'`, p.ID)
	refused("removed parent authority accepted")
	exec(`UPDATE principal_role_bindings SET valid_until=NULL WHERE principal_id=$1 AND role='development'`, p.ID)
	exec(`UPDATE principals SET active=false WHERE id=$1`, p.ID)
	refused("inactive issuer accepted")
	exec(`UPDATE principals SET active=true WHERE id=$1`, p.ID)
	exec(`UPDATE workflow_task_checkpoints SET state='completed',completed_at=now() WHERE id::text=$1`, task.ID)
	refused("completed task accepted")
	exec(`UPDATE workflow_task_checkpoints SET state=$2,completed_at=NULL WHERE id::text=$1`, task.ID, task.State)
	exec(`UPDATE principal_credentials SET expires_at=now()-interval '1 second' WHERE principal_id=$1`, d.PrincipalID)
	refused("expired credential accepted")
	if _, err = r.Pool().Exec(ctx, `UPDATE task_delegations SET expires_at=expires_at+interval '1 minute' WHERE id::text=$1`, d.ID); err == nil {
		t.Fatal("delegation scope changed after issue")
	}
	if _, err = r.Pool().Exec(ctx, `DELETE FROM task_delegations WHERE id::text=$1`, d.ID); err == nil {
		t.Fatal("delegation attribution deleted")
	}
	parentExpiry := time.Now().Add(30 * time.Second)
	exec(`UPDATE principal_credentials SET expires_at=$2 WHERE id::text=$1`, p.CredentialID, parentExpiry)
	capped, _ := issue()
	if capped.ExpiresAt.After(parentExpiry) {
		t.Fatal("delegation outlived parent expiry")
	}
	exec(`UPDATE principal_credentials SET expires_at=NULL WHERE id::text=$1`, p.CredentialID)
	constraint := "deny_delegation_" + strings.ReplaceAll(task.ProjectID, "-", "")
	exec(fmt.Sprintf("ALTER TABLE operation_records ADD CONSTRAINT %s CHECK(project_id<>'%s') NOT VALID", constraint, task.ProjectID))
	var before, after int
	if err = r.Pool().QueryRow(ctx, `SELECT count(*) FROM task_delegations WHERE project_id=$1`, task.ProjectID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	_, _, issueErr := s.IssueTaskCredential(actorCtx, task.ID, time.Minute)
	exec("ALTER TABLE operation_records DROP CONSTRAINT " + constraint)
	if issueErr == nil {
		t.Fatal("credential issued without durable evidence")
	}
	if err = r.Pool().QueryRow(ctx, `SELECT count(*) FROM task_delegations WHERE project_id=$1`, task.ProjectID).Scan(&after); err != nil || before != after {
		t.Fatal("failed issue partially committed", err)
	}
	var disclosed bool
	if err = r.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operation_records WHERE project_id=$1 AND record::text LIKE '%' || $2 || '%')`, task.ProjectID, token).Scan(&disclosed); err != nil || disclosed {
		t.Fatal("credential leaked into operation evidence", err)
	}
}
