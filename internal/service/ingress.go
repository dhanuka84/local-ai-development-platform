package service

import (
	"context"
	"encoding/json"
	"regexp"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
)

var auditIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)

// This outer boundary runs before SDK schema validation, delegation hydration
// and tool lookup. Only bounded correlation identifiers and a digest survive;
// rejected bodies and credentials never enter audit records.
func (s *Service) BeginToolEnvelope(ctx context.Context, name string, raw []byte) (context.Context, telemetry.Finish, error) {
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &fields)
	read := func(key string) string {
		var value string
		if json.Unmarshal(fields[key], &value) != nil || !auditIdentifier.MatchString(value) {
			return ""
		}
		return value
	}
	scope := domain.OperationScope{ProjectID: read("project_id"), WorkflowID: read("workflow_id"), TaskID: read("task_id"), ExecutionID: read("run_id"), ExecutionStepID: read("step_id")}
	if scope.ProjectID == "" {
		scope.ProjectID = "system"
	}
	if !auditIdentifier.MatchString(name) {
		name = "invalid_tool_name"
	}
	return s.BeginOperation(ctx, "mcp.envelope."+name, scope, domain.EvidenceReference{Kind: "tool_input", ID: name, SHA256: domain.Digest(raw)})
}

func (s *Service) AuditAuthenticationFailure(ctx context.Context) error {
	ctx = identity.WithPrincipal(ctx, domain.Principal{ID: "unauthenticated"})
	_, finish, err := s.BeginOperation(ctx, "http.authentication", domain.OperationScope{ProjectID: "system"})
	if err != nil {
		return err
	}
	return finish(ErrForbidden)
}
