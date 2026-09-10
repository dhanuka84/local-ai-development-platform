package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

func (s *Service) recordDelegationDenial(ctx context.Context, name string, p domain.Principal) error {
	if p.Delegation == nil {
		return ErrForbidden
	}
	d := p.Delegation
	_, finish, err := s.BeginOperation(ctx, "mcp."+name, domain.OperationScope{ProjectID: d.ProjectID, WorkflowID: d.WorkflowID, TaskID: d.TaskID}, domain.EvidenceReference{Kind: "delegation", ID: d.ID})
	if err != nil {
		return errors.Join(ErrForbidden, err)
	}
	return errors.Join(ErrForbidden, finish(ErrForbidden))
}

// IssueTaskCredential is an operator CLI operation, deliberately not an MCP
// tool: the bearer secret goes directly to a private file, never model context.
func (s *Service) IssueTaskCredential(ctx context.Context, taskID string, ttl time.Duration) (domain.TaskDelegation, string, error) {
	p, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.TaskDelegation{}, "", err
	}
	if !p.Human || p.Delegation != nil || p.CredentialID == "" || !s.reportAuthorizerDependency || ttl < time.Minute || ttl > time.Hour {
		return domain.TaskDelegation{}, "", ErrForbidden
	}
	task, err := s.GetWorkflowTask(ctx, taskID)
	if err != nil {
		return domain.TaskDelegation{}, "", err
	}
	if !p.HasRole(task.ProjectID, "development") {
		return domain.TaskDelegation{}, "", ErrForbidden
	}
	repository, ok := s.repository.(domain.DelegationRepository)
	if !ok {
		return domain.TaskDelegation{}, "", domain.ErrEvidenceUnavailable
	}
	id, err := domain.NewID()
	if err != nil {
		return domain.TaskDelegation{}, "", err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return domain.TaskDelegation{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	d, err := repository.IssueTaskDelegation(ctx, domain.TaskDelegation{ID: id, PrincipalID: "delegate:" + id, DelegatedBy: p.ID, ParentCredentialID: p.CredentialID, ProjectID: task.ProjectID, WorkflowID: task.WorkflowID, TaskID: task.ID, CreatedAt: now, ExpiresAt: now.Add(ttl)}, hash[:])
	if err != nil {
		return domain.TaskDelegation{}, "", err
	}
	return d, token, nil
}

func (s *Service) RevokeTaskCredential(ctx context.Context, id string) (domain.TaskDelegation, error) {
	p, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.TaskDelegation{}, err
	}
	if !p.Human || p.Delegation != nil {
		return domain.TaskDelegation{}, ErrForbidden
	}
	r, ok := s.repository.(domain.DelegationRepository)
	if !ok {
		return domain.TaskDelegation{}, domain.ErrEvidenceUnavailable
	}
	return r.RevokeTaskDelegation(ctx, id, p.ID)
}

// Delegation adds a deny-by-default tool boundary to ordinary project policy.
// Existing Cerbos, eligibility and workflow gates still run in each handler.
func (s *Service) checkDelegatedTool(ctx context.Context, name string, input []byte, scope domain.OperationScope) error {
	p, ok := identity.PrincipalFromContext(ctx)
	if !ok || p.Delegation == nil {
		return nil
	}
	d := p.Delegation
	if p.Human || d.RevokedAt != nil || !d.ExpiresAt.After(time.Now()) || scope.ProjectID != d.ProjectID {
		return ErrForbidden
	}
	var in struct {
		ID             string `json:"id"`
		ProjectID      string `json:"project_id"`
		WorkflowID     string `json:"workflow_id"`
		TaskID         string `json:"task_id"`
		KnowledgeID    string `json:"knowledge_id"`
		CandidateID    string `json:"candidate_id"`
		WorkflowStepID string `json:"workflow_step_id"`
		Provider       string `json:"provider"`
		Event          string `json:"event_type"`
	}
	if json.Unmarshal(input, &in) != nil {
		return ErrForbidden
	}
	if (in.ProjectID != "" && in.ProjectID != d.ProjectID) || (in.TaskID != "" && in.TaskID != d.TaskID) || (in.WorkflowID != "" && in.WorkflowID != d.WorkflowID) || in.WorkflowStepID != "" {
		return ErrForbidden
	}
	switch name {
	case "knowledge_search", "repository_graph_get", "repository_relation_search", "code_symbol_search", "code_graph_get", "graph_context_search", "context_definition_search", "platform_metric_query":
		if in.ProjectID == d.ProjectID {
			return nil
		}
	case "knowledge_get":
		if in.ID != "" {
			return nil
		}
	case "workflow_run_get", "workflow_trace_get":
		if in.WorkflowID == d.WorkflowID {
			return nil
		}
	case "workflow_task_get", "workflow_task_context_record":
		if in.TaskID == d.TaskID {
			return nil
		}
	case "generation_capture":
		if in.ProjectID == d.ProjectID && in.WorkflowID == d.WorkflowID && in.Provider == "ollama" {
			return nil
		}
	case "review_record":
		if in.Provider != "ollama" || in.WorkflowID != d.WorkflowID {
			return ErrForbidden
		}
		task, err := s.repository.GetWorkflowTask(ctx, d.TaskID)
		if err == nil && task.CandidateID != "" && in.KnowledgeID == task.CandidateID {
			return nil
		}
	case "workflow_task_transition":
		if in.TaskID != d.TaskID || in.Provider != "ollama" {
			return ErrForbidden
		}
		switch in.Event {
		case "LOCAL_RESULT_RECORDED":
			r, ok := s.repository.(domain.DelegationRepository)
			if !ok {
				return ErrForbidden
			}
			owned, err := r.OwnsDelegatedCandidate(ctx, p.ID, d.TaskID, in.CandidateID)
			if err == nil && owned {
				return nil
			}
		case "LOCAL_REVISION_RECORDED", "VALIDATION_PASSED", "VALIDATION_FAILED", "VALIDATED_REUSE_COMPLETED", "RAG_READBACK_VERIFIED":
			return nil
		}
	}
	return ErrForbidden
}
