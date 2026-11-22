// Package api exposes the pipelines module over the REST JSON contract at
// /api/v1/pipelines. It intentionally does not use generated connect handlers:
// the Stage 1 core uses the same JSON conventions as the datasets module.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
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
	guard     authz.DecisionGuard
}

// Option customises the HTTP API wiring.
type Option func(*API)

// WithDecisionGuard injects the authorization guard. A nil guard preserves the
// legacy behaviour: every route is allowed exactly as before the seam existed.
func (a *API) SetDecisionGuard(guard authz.DecisionGuard) { a.guard = guard }

func WithDecisionGuard(guard authz.DecisionGuard) Option {
	return func(a *API) { a.guard = guard }
}

// New constructs the HTTP API. The same service.Manager usually satisfies both
// service interfaces.
func New(pipelines service.Service, schedules service.ScheduleService, tenants TenantResolver, opts ...Option) *API {
	a := &API{pipelines: pipelines, schedules: schedules, tenants: tenants}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// authorize delegates to the platform guard. A nil guard authorizes without a
// side effect, which is the documented fail-closed seam default.
func (a *API) authorize(w http.ResponseWriter, r *http.Request, action authz.Action, resource authz.ResourceFact) bool {
	return authz.AuthorizeHTTP(w, r, a.guard, action, resource)
}

func tenantFact(tenantID string) authz.ResourceFact {
	return authz.ResourceFact{Type: "tenant", ID: tenantID, TenantID: tenantID}
}

func pipelineFact(pipeline domain.Pipeline) authz.ResourceFact {
	return authz.ResourceFact{
		Type:     "pipeline",
		ID:       pipeline.ID,
		TenantID: pipeline.TenantID,
		Status:   string(pipeline.Status),
	}
}

func pipelineRunFact(run domain.PipelineRun) authz.ResourceFact {
	return authz.ResourceFact{
		Type:     "pipeline_run",
		ID:       run.ID,
		TenantID: run.TenantID,
		Status:   string(run.Status),
		Ancestors: []authz.Ancestor{
			{Type: "pipeline", ID: run.PipelineID},
			{Type: "tenant", ID: run.TenantID},
		},
	}
}

func pipelineScheduleFact(schedule domain.Schedule) authz.ResourceFact {
	return authz.ResourceFact{
		Type:     "pipeline_schedule",
		ID:       schedule.ID,
		TenantID: schedule.TenantID,
		Ancestors: []authz.Ancestor{
			{Type: "pipeline", ID: schedule.PipelineID},
			{Type: "tenant", ID: schedule.TenantID},
		},
	}
}

// loadPipelineForAuth loads the tenant-scoped pipeline row that authorization
// is based on. A pipeline outside the caller's tenant yields 404 before the
// guard is consulted.
func (a *API) loadPipelineForAuth(w http.ResponseWriter, r *http.Request, tenantID string) (domain.Pipeline, bool) {
	pipeline, err := a.pipelines.GetPipeline(r.Context(), tenantID, chi.URLParam(r, "pipelineID"))
	if err != nil {
		writeServiceError(w, r, err)
		return domain.Pipeline{}, false
	}
	return pipeline, true
}

// loadScheduleForAuth loads the tenant-scoped schedule row that authorization is
// based on. A schedule outside the caller's tenant yields 404 before the guard
// is consulted.
func (a *API) loadScheduleForAuth(w http.ResponseWriter, r *http.Request, tenantID string) (domain.Schedule, bool) {
	schedule, err := a.schedules.GetSchedule(r.Context(), tenantID, chi.URLParam(r, "scheduleID"))
	if err != nil {
		writeServiceError(w, r, err)
		return domain.Schedule{}, false
	}
	return schedule, true
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

// parseIdempotencyKey validates an optional Idempotency-Key header. Publish and
// trigger accept it; absent means legacy semantics. It is intentionally opaque
// (no trimming/reformatting) so replays match byte-for-byte.
func parseIdempotencyKey(r *http.Request) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return "", true
	}
	if len(key) > 255 {
		return "", false
	}
	for _, c := range key {
		if c < 0x21 || c > 0x7e {
			return "", false
		}
	}
	return key, true
}

// idempotencyHash returns the hex SHA-256 of value.
func idempotencyHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

var idempotencyLocks sync.Map

// lockIdempotency serialises same-key requests inside one process. The database
// unique constraint is the cross-process backstop.
func lockIdempotency(key string) func() {
	value, _ := idempotencyLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
