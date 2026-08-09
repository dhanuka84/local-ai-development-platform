package identity

import (
	"context"
	"errors"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

var ErrUnauthenticated = errors.New("authenticated principal is required")

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal domain.Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (domain.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(domain.Principal)
	return principal, ok && principal.ID != ""
}

func RequirePrincipal(ctx context.Context) (domain.Principal, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return domain.Principal{}, ErrUnauthenticated
	}
	return principal, nil
}
