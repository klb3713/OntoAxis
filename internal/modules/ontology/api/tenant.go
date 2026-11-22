package api

import (
	"context"
	"strings"
)

type tenantContextKey struct{}

// WithTenantID returns a context carrying the authenticated tenant ID. It is
// the integration seam for the platform authentication middleware; the
// ontology module itself never trusts a request header or default principal.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, strings.TrimSpace(tenantID))
}

// TenantIDFromContext returns the tenant ID previously injected by trusted
// middleware. The boolean is false for a missing or blank tenant.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	value := ctx.Value(tenantContextKey{})
	tenantID, ok := value.(string)
	if !ok || strings.TrimSpace(tenantID) == "" {
		return "", false
	}
	return tenantID, true
}
