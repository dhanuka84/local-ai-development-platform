package authorization

import (
	"context"
	"fmt"
	"time"

	cerbossdk "github.com/cerbos/cerbos-sdk-go/cerbos"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type Cerbos struct {
	client  *cerbossdk.GRPCClient
	timeout time.Duration
}

func NewCerbos(address string, timeout time.Duration) (*Cerbos, error) {
	client, err := cerbossdk.New(address, cerbossdk.WithPlaintext(), cerbossdk.WithConnectTimeout(timeout))
	if err != nil {
		return nil, fmt.Errorf("create Cerbos client: %w", err)
	}
	return &Cerbos{client: client, timeout: timeout}, nil
}

func (c *Cerbos) Authorize(ctx context.Context, request domain.AuthorizationRequest) (domain.AuthorizationDecision, error) {
	if c == nil || c.client == nil {
		return domain.AuthorizationDecision{}, fmt.Errorf("Cerbos authorizer is not configured")
	}
	projectID, _ := request.Attributes["project_id"].(string)
	principal := cerbossdk.NewPrincipal(request.Principal.ID, request.Principal.RolesFor(projectID)...)
	principal.WithAttributes(map[string]any{
		"human":       request.Principal.Human,
		"project_ids": request.Principal.ProjectIDs(),
	})
	resource := cerbossdk.NewResource(request.ResourceKind, request.ResourceID).WithAttributes(request.Attributes)
	batch := cerbossdk.NewResourceBatch().Add(resource, request.Action)

	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.client.CheckResources(checkCtx, principal, batch)
	if err != nil {
		return domain.AuthorizationDecision{}, fmt.Errorf("Cerbos CheckResources: %w", err)
	}
	if err := response.Errors(); err != nil {
		return domain.AuthorizationDecision{}, fmt.Errorf("Cerbos policy validation: %w", err)
	}
	result := response.GetResource(request.ResourceID, cerbossdk.MatchResourceKind(request.ResourceKind))
	if err := result.Err(); err != nil {
		return domain.AuthorizationDecision{}, fmt.Errorf("Cerbos result: %w", err)
	}
	policyVersion := ""
	if result.CheckResourcesResponse_ResultEntry != nil && result.Resource != nil {
		policyVersion = result.Resource.PolicyVersion
	}
	return domain.AuthorizationDecision{
		Allowed: result.IsAllowed(request.Action), CallID: response.GetCerbosCallId(), PolicyVersion: policyVersion,
	}, nil
}

func (c *Cerbos) Ping(ctx context.Context) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("Cerbos authorizer is not configured")
	}
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.client.ServerInfo(checkCtx)
	return err
}
