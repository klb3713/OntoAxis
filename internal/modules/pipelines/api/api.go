// Package api exposes the pipelines module over the REST JSON contract at
// /api/v1/pipelines. It intentionally does not use generated connect handlers:
// the Stage 1 core uses the same JSON conventions as the datasets module.
package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

// ErrNoTenant means no authenticated tenant was presented.
var ErrNoTenant = errors.New("tenant context missing")

// ErrTenantResolverUnavailable means the module was built without any tenant
// resolver and must fail closed.
var ErrTenantResolverUnavailable = errors.New("tenant resolver is not configured")

// TenantResolver turns an authenticated request into a tenant ID. Real
// implementations belong to the identity-edge layer; this module only defines
// the seam so it has no dependency on internal/platform/authn.
type TenantResolver interface {
	ResolveTenant(r *http.Request) (string, error)
}

// RejectAllTenantResolver is the secure default: every request is rejected.
type RejectAllTenantResolver struct{}

// ResolveTenant always reports a missing tenant context.
func (RejectAllTenantResolver) ResolveTenant(*http.Request) (string, error) {
	return "", ErrNoTenant
}

// HeaderTenantResolver reads the internal gateway header. It is intended for
// tests and local integration until identity-edge injects a real resolver.
type HeaderTenantResolver struct{}

// ResolveTenant validates X-Ontoaxis-Tenant-Id and rejects a client-supplied
// X-Tenant-Id to avoid ambiguous tenant contexts.
func (HeaderTenantResolver) ResolveTenant(r *http.Request) (string, error) {
	if strings.TrimSpace(r.Header.Get("X-Tenant-Id")) != "" {
		return "", ErrNoTenant
	}
	raw := strings.TrimSpace(r.Header.Get("X-Ontoaxis-Tenant-Id"))
	if raw == "" {
		return "", ErrNoTenant
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return "", ErrNoTenant
	}
	return parsed.String(), nil
}

// API groups the pipeline and schedule handlers.
type API struct {
	pipelines service.Service
	schedules service.ScheduleService
	tenants   TenantResolver
}

// New constructs the HTTP API. The same service.Manager usually satisfies both
// service interfaces.
func New(pipelines service.Service, schedules service.ScheduleService, tenants TenantResolver) *API {
	return &API{pipelines: pipelines, schedules: schedules, tenants: tenants}
}

func (a *API) requireTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	if a == nil || a.pipelines == nil || a.schedules == nil || a.tenants == nil {
		writeFailure(w, r, httperr.CodeUnavailable, "pipelines service is not configured")
		return "", false
	}
	tenantID, err := a.tenants.ResolveTenant(r)
	if err != nil {
		if errors.Is(err, ErrTenantResolverUnavailable) {
			writeFailure(w, r, httperr.CodeUnavailable, "tenant resolver is not configured")
			return "", false
		}
		writeFailure(w, r, httperr.CodeUnauthorized, "authenticated tenant is required")
		return "", false
	}
	tenantID = strings.TrimSpace(tenantID)
	parsed, err := uuid.Parse(tenantID)
	if err != nil {
		writeFailure(w, r, httperr.CodeUnauthorized, "authenticated tenant is required")
		return "", false
	}
	return parsed.String(), true
}
