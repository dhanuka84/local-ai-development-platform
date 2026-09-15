// Package authorization implements policy decisions for authenticated operations.
// Database mutation boundaries separately enforce their authorization invariants.
package authorization

import (
	"context"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type Disabled struct{}

func (Disabled) Authorize(context.Context, domain.AuthorizationRequest) (domain.AuthorizationDecision, error) {
	return domain.AuthorizationDecision{Allowed: true, CallID: "authorization-disabled", PolicyVersion: "local-none"}, nil
}

func (Disabled) Ping(context.Context) error { return nil }
