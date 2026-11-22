package authn

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// TenantContext is the trusted per-request identity derived from a verified
// JWT or from gateway-injected internal headers. It must only be written by
// authn.Middleware.
type TenantContext struct {
	TenantID     uuid.UUID
	UserID       uuid.UUID
	OrgIDs       []uuid.UUID
	WorkspaceIDs []uuid.UUID
	Roles        []string
	RequestID    string
}

type tenantContextKey struct{}

// FromContext returns the TenantContext stored by authn.Middleware. The
// boolean is false when the context does not carry a tenant identity.
func FromContext(ctx context.Context) (TenantContext, bool) {
	if ctx == nil {
		return TenantContext{}, false
	}
	tc, ok := ctx.Value(tenantContextKey{}).(TenantContext)
	if !ok {
		return TenantContext{}, false
	}
	return tc, true
}

// RequireTenant returns the TenantContext or ErrUnauthenticated when no
// trusted identity is present. Handlers should treat this as a fail-closed
// condition, never as an anonymous fallback.
func RequireTenant(ctx context.Context) (TenantContext, error) {
	tc, ok := FromContext(ctx)
	if !ok {
		return TenantContext{}, ErrUnauthenticated
	}
	return tc, nil
}

func withTenantContext(ctx context.Context, tc TenantContext) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tc)
}

// WithTenantContext injects a trusted tenant context. Intended for tests and
// for middleware assembly; production request paths must use Middleware.
func WithTenantContext(ctx context.Context, tc TenantContext) context.Context {
	return withTenantContext(ctx, tc)
}

// ErrUnauthenticated marks a missing or invalid authentication credential.
var ErrUnauthenticated = errors.New("unauthenticated")
