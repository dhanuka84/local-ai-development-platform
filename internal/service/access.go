package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

// AuthorizeProjectAction is the shared MCP enforcement seam for domain
// operations that predate the workflow state machine. Resource attributes
// must be hydrated or validated by the caller before invoking the mutation.
func (s *Service) AuthorizeProjectAction(ctx context.Context, projectID, resourceKind, resourceID, action string, attributes map[string]any) (domain.Principal, error) {
	principal, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.Principal{}, err
	}
	projectID = strings.TrimSpace(projectID)
	resourceKind = strings.TrimSpace(resourceKind)
	resourceID = strings.TrimSpace(resourceID)
	action = strings.TrimSpace(action)
	if projectID == "" || resourceKind == "" || resourceID == "" || action == "" {
		return domain.Principal{}, fmt.Errorf("%w: project, resource kind/id, and action are required", ErrInvalidInput)
	}
	if attributes == nil {
		attributes = make(map[string]any)
	}
	attributes["project_id"] = projectID
	if _, err := s.authorize(ctx, domain.AuthorizationRequest{
		Principal: principal, ResourceKind: resourceKind, ResourceID: resourceID, Action: action, Attributes: attributes,
	}); err != nil {
		return domain.Principal{}, err
	}
	return principal, nil
}
