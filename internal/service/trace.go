package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
)

func (s *Service) BeginOperation(ctx context.Context, name string, scope domain.OperationScope, refs ...domain.EvidenceReference) (context.Context, telemetry.Finish, error) {
	repository, _ := s.repository.(domain.TraceRepository)
	return telemetry.Begin(ctx, repository, name, scope, refs)
}

func (s *Service) BeginToolOperation(ctx context.Context, name string, input []byte) (context.Context, telemetry.Finish, error) {
	var fields struct {
		ProjectID       string `json:"project_id"`
		WorkflowID      string `json:"workflow_id"`
		TaskID          string `json:"task_id"`
		KnowledgeID     string `json:"knowledge_id"`
		ID              string `json:"id"`
		ExpectedVersion int    `json:"expected_version"`
	}
	if err := json.Unmarshal(input, &fields); err != nil {
		return ctx, nil, err
	}
	scope := domain.OperationScope{ProjectID: fields.ProjectID, WorkflowID: fields.WorkflowID, TaskID: fields.TaskID}
	if fields.ID != "" && name == "knowledge_get" {
		fields.KnowledgeID = fields.ID
	}
	refs := []domain.EvidenceReference{}
	if fields.KnowledgeID != "" {
		item, err := s.repository.GetKnowledge(ctx, fields.KnowledgeID, true)
		if err != nil {
			return ctx, nil, err
		}
		scope.ProjectID = item.ProjectID
		scope.WorkflowID = item.WorkflowID
		refs = append(refs, domain.EvidenceReference{Kind: "knowledge", ID: item.ID, Version: item.Version, SHA256: domain.Digest([]byte(item.Content))})
	}
	if scope.TaskID != "" {
		task, err := s.repository.GetWorkflowTask(ctx, scope.TaskID)
		if err != nil {
			return ctx, nil, err
		}
		scope.ProjectID = task.ProjectID
		scope.WorkflowID = task.WorkflowID
		ctx = domain.WithPurpose(ctx, domain.TaskPurpose(task.TaskType))
	}
	if scope.WorkflowID != "" {
		run, err := s.repository.GetWorkflow(ctx, scope.WorkflowID)
		if err != nil {
			return ctx, nil, err
		}
		scope.ProjectID = run.ProjectID
	}
	if scope.ProjectID == "" {
		scope.ProjectID = "system"
	}
	return s.BeginOperation(ctx, "mcp."+name, scope, refs...)
}

func (s *Service) WorkflowTrace(ctx context.Context, workflowID string) (domain.WorkflowTrace, error) {
	run, err := s.GetWorkflow(ctx, workflowID)
	if err != nil {
		return domain.WorkflowTrace{}, err
	}
	repository, ok := s.repository.(domain.TraceRepository)
	if !ok {
		return domain.WorkflowTrace{}, domain.ErrEvidenceUnavailable
	}
	result, err := repository.ReadWorkflowTrace(ctx, run.ProjectID, run.ID, 1000)
	if err != nil {
		return result, err
	}
	reader, ok := s.artifacts.(interface {
		Read(context.Context, string) ([]byte, error)
	})
	seen := map[string]bool{}
	for _, record := range result.Records {
		for _, ref := range record.References {
			if ref.Kind != "artifact" || seen[ref.SHA256] {
				continue
			}
			seen[ref.SHA256] = true
			if !ok {
				result.Missing = append(result.Missing, "artifact_reader")
				break
			}
			if _, err := reader.Read(ctx, ref.SHA256); err != nil {
				result.Missing = append(result.Missing, "artifact:"+ref.SHA256)
			}
		}
	}
	result.Complete = len(result.Missing) == 0
	return result, nil
}

func (s *Service) RecordOperationEvidence(ctx context.Context, name string, artifact domain.Artifact) error {
	_, finish, err := s.BeginOperation(ctx, name, domain.ScopeFromContext(ctx), domain.EvidenceReference{Kind: "artifact", ID: artifact.SHA256, SHA256: artifact.SHA256})
	if err != nil {
		return err
	}
	return finish(nil)
}

// Authorization failures are sanitized; raw errors may contain remote response
// bodies and never become trace attributes.
func (s *Service) tracedAuthorization(ctx context.Context, request domain.AuthorizationRequest) (decision domain.AuthorizationDecision, err error) {
	scope := domain.ScopeFromContext(ctx)
	if project, ok := request.Attributes["project_id"].(string); ok {
		scope.ProjectID = project
	}
	if scope.ProjectID == "" {
		scope.ProjectID = "system"
	}
	repository, ok := s.repository.(domain.TraceRepository)
	if !ok {
		return s.authorizeUntraced(ctx, request)
	}
	ctx = identity.WithPrincipal(ctx, request.Principal)
	ctx, span, record := telemetry.Start(ctx, "authorization."+request.ResourceKind+"."+request.Action, scope, nil)
	defer span.End()
	if err := repository.AppendOperation(ctx, record); err != nil {
		return decision, err
	}
	decision, err = s.authorizeUntraced(ctx, request)
	outcome := "success"
	policyDecision := "allow"
	if err != nil {
		outcome = "denied"
		policyDecision = "deny"
	}
	result := telemetry.Result(record, outcome, nil)
	result.PolicyVersion = decision.PolicyVersion
	result.PolicyDecision = policyDecision
	result.Rationale = "authenticated_project_role_and_policy_check"
	return decision, errors.Join(err, repository.AppendOperation(ctx, result))
}
