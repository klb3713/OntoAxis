package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/api"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/authz/authtest"
)

const otherTenant = "99999999-9999-7999-8999-999999999999"

// stateGuard applies the IngestJob rule from
// policies/platform/resource-state.cedar on top of authtest.FakeGuard, so the
// module tests can prove that handlers pass authoritative database state to the
// guard instead of trusting the request.
type stateGuard struct {
	*authtest.FakeGuard
}

func (g stateGuard) Require(ctx context.Context, action authz.Action, resource authz.ResourceFact) (authz.Decision, error) {
	decision, err := g.FakeGuard.Require(ctx, action, resource)
	if err != nil {
		return decision, err
	}
	if reason := resourceStateDenial(action, resource); reason != "" {
		return authz.Decision{DecisionID: decision.DecisionID, Reason: reason}, &authz.Error{Reason: reason}
	}
	return decision, nil
}

func resourceStateDenial(action authz.Action, resource authz.ResourceFact) authz.Reason {
	switch action {
	case "ingestion:cancel":
		if resource.Type == "ingest_job" && resource.Status != "queued" && resource.Status != "running" {
			return authz.ReasonResourceStateDenied
		}
	case "pipeline:invoke":
		if resource.Type == "pipeline" && resource.Status != "published" {
			return authz.ReasonResourceStateDenied
		}
	case "pipeline:update", "pipeline:delete", "pipeline:publish":
		if resource.Type == "pipeline" && resource.Status == "archived" {
			return authz.ReasonResourceStateDenied
		}
	}
	return ""
}

// authzIngestionService records the service calls so tests can assert that a
// denied request produced no side effect.
type authzIngestionService struct {
	job       *domain.SyncJob
	jobs      []domain.SyncJob
	runs      []domain.SyncRun
	getErr    error
	cancelErr error
	calls     []string
}

func (s *authzIngestionService) CreateJob(_ context.Context, _, _ string) (*domain.SyncJob, error) {
	s.calls = append(s.calls, "CreateJob")
	return s.job, nil
}

func (s *authzIngestionService) GetJob(_ context.Context, _, _ string) (*domain.SyncJob, error) {
	s.calls = append(s.calls, "GetJob")
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.job, nil
}

func (s *authzIngestionService) ListJobs(_ context.Context, _, _ string, _, _ int32) (*service.ListResult, error) {
	s.calls = append(s.calls, "ListJobs")
	return &service.ListResult{Jobs: s.jobs, Page: 1, PerPage: 20, Total: int64(len(s.jobs)), TotalPages: 1}, nil
}

func (s *authzIngestionService) CancelJob(_ context.Context, _, _ string) (*domain.SyncJob, error) {
	s.calls = append(s.calls, "CancelJob")
	if s.cancelErr != nil {
		return nil, s.cancelErr
	}
	return s.job, nil
}

func (s *authzIngestionService) ListRuns(_ context.Context, _, _ string) ([]domain.SyncRun, error) {
	s.calls = append(s.calls, "ListRuns")
	return s.runs, nil
}

func (s *authzIngestionService) Watch(_ context.Context, _, _ string) (<-chan *domain.SyncJob, <-chan error, error) {
	s.calls = append(s.calls, "Watch")
	updates := make(chan *domain.SyncJob)
	close(updates)
	errs := make(chan error)
	close(errs)
	return updates, errs, nil
}

func (s *authzIngestionService) called(method string) bool {
	for _, call := range s.calls {
		if call == method {
			return true
		}
	}
	return false
}

type fakeConnectorReader struct {
	connector   *connectordomain.Connector
	err         error
	tenantID    string
	connectorID string
}

func (f *fakeConnectorReader) GetConnector(_ context.Context, tenantID, id string) (*connectordomain.Connector, error) {
	f.tenantID, f.connectorID = tenantID, id
	if f.err != nil {
		return nil, f.err
	}
	return f.connector, nil
}

func newAuthzRouter(svc api.Service, guard authz.DecisionGuard, reader api.ConnectorReader) chi.Router {
	opts := []api.Option{api.WithDecisionGuard(guard)}
	if reader != nil {
		opts = append(opts, api.WithConnectorReader(reader))
	}
	router := chi.NewRouter()
	api.NewHandler(svc, opts...).Register(router)
	return router
}

func doRequest(router http.Handler, method, path, tenant, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if tenant != "" {
		req.Header.Set("X-Ontoaxis-Tenant-Id", tenant)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func activeConnector() *connectordomain.Connector {
	return &connectordomain.Connector{
		ID:       "33333333-3333-7333-8333-333333333333",
		TenantID: testTenant,
		Status:   connectordomain.StatusActive,
	}
}

func singleGuardCall(t *testing.T, guard *authtest.FakeGuard) (authz.Action, authz.ResourceFact) {
	t.Helper()
	actions, resources := guard.Calls()
	if len(actions) != 1 || len(resources) != 1 {
		t.Fatalf("guard calls = %d actions / %d resources, want exactly 1", len(actions), len(resources))
	}
	return actions[0], resources[0]
}

func TestIngestionCreateAuthorizesConnector(t *testing.T) {
	t.Parallel()
	job := sampleJob()
	svc := &authzIngestionService{job: job}
	guard := authtest.Guard()
	reader := &fakeConnectorReader{connector: activeConnector()}
	router := newAuthzRouter(svc, guard, reader)

	rec := doRequest(router, http.MethodPost, "/api/v1/ingestion/jobs", testTenant, `{"connector_id":"33333333-3333-7333-8333-333333333333"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get(authz.DecisionHeader), "decision-test"; got != want {
		t.Fatalf("decision header = %q, want %q", got, want)
	}
	action, resource := singleGuardCall(t, guard)
	if action != "ingestion:create" {
		t.Fatalf("action = %q, want ingestion:create", action)
	}
	want := authz.ResourceFact{
		Type:     "connector",
		ID:       "33333333-3333-7333-8333-333333333333",
		TenantID: testTenant,
		Status:   string(connectordomain.StatusActive),
	}
	if !reflect.DeepEqual(resource, want) {
		t.Fatalf("resource = %+v, want %+v", resource, want)
	}
	if reader.tenantID != testTenant || reader.connectorID != "33333333-3333-7333-8333-333333333333" {
		t.Fatalf("connector reader got tenant=%q connector=%q", reader.tenantID, reader.connectorID)
	}
	if !svc.called("CreateJob") {
		t.Fatalf("CreateJob was not called after authorization")
	}
}

func TestIngestionCreateDeniedByGuard(t *testing.T) {
	t.Parallel()
	svc := &authzIngestionService{job: sampleJob()}
	guard := authtest.Deny(authz.ReasonRoleNotPermitted)
	reader := &fakeConnectorReader{connector: activeConnector()}
	router := newAuthzRouter(svc, guard, reader)

	rec := doRequest(router, http.MethodPost, "/api/v1/ingestion/jobs", testTenant, `{"connector_id":"33333333-3333-7333-8333-333333333333"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if svc.called("CreateJob") {
		t.Fatalf("CreateJob ran despite denied authorization")
	}
	action, _ := singleGuardCall(t, guard)
	if action != "ingestion:create" {
		t.Fatalf("action = %q, want ingestion:create", action)
	}
}

func TestIngestionCreateCrossTenantConnectorIsNotFoundBeforeAuthorization(t *testing.T) {
	t.Parallel()
	svc := &authzIngestionService{job: sampleJob()}
	guard := authtest.Guard()
	reader := &fakeConnectorReader{err: connectordomain.ErrNotFound}
	router := newAuthzRouter(svc, guard, reader)

	rec := doRequest(router, http.MethodPost, "/api/v1/ingestion/jobs", otherTenant, `{"connector_id":"33333333-3333-7333-8333-333333333333"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if actions, _ := guard.Calls(); len(actions) != 0 {
		t.Fatalf("guard was called %d times before the tenant-scoped load failed", len(actions))
	}
	if svc.called("CreateJob") {
		t.Fatalf("CreateJob ran despite a failed tenant-scoped connector load")
	}
}

func TestIngestionCreateFailsClosedWithoutConnectorReader(t *testing.T) {
	t.Parallel()
	svc := &authzIngestionService{job: sampleJob()}
	router := newAuthzRouter(svc, authtest.Guard(), nil)

	rec := doRequest(router, http.MethodPost, "/api/v1/ingestion/jobs", testTenant, `{"connector_id":"33333333-3333-7333-8333-333333333333"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	if svc.called("CreateJob") {
		t.Fatalf("CreateJob ran without an authorization decision")
	}
}

func TestIngestionGetAuthorizesIngestJob(t *testing.T) {
	t.Parallel()
	job := sampleJob()
	svc := &authzIngestionService{job: job}
	guard := authtest.Guard()
	router := newAuthzRouter(svc, guard, nil)

	rec := doRequest(router, http.MethodGet, "/api/v1/ingestion/jobs/"+job.ID, testTenant, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	action, resource := singleGuardCall(t, guard)
	if action != "ingestion:read" {
		t.Fatalf("action = %q, want ingestion:read", action)
	}
	want := authz.ResourceFact{
		Type:     "ingest_job",
		ID:       job.ID,
		TenantID: testTenant,
		Status:   string(domain.StatusQueued),
		Ancestors: []authz.Ancestor{
			{Type: "connector", ID: job.ConnectorID},
			{Type: "tenant", ID: testTenant},
		},
	}
	if !reflect.DeepEqual(resource, want) {
		t.Fatalf("resource = %+v, want %+v", resource, want)
	}
}

func TestIngestionListAuthorizesTenant(t *testing.T) {
	t.Parallel()
	svc := &authzIngestionService{jobs: []domain.SyncJob{*sampleJob()}}
	guard := authtest.Guard()
	router := newAuthzRouter(svc, guard, nil)

	rec := doRequest(router, http.MethodGet, "/api/v1/ingestion/jobs/", testTenant, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	action, resource := singleGuardCall(t, guard)
	if action != "ingestion:read" {
		t.Fatalf("action = %q, want ingestion:read", action)
	}
	want := authz.ResourceFact{Type: "tenant", ID: testTenant, TenantID: testTenant}
	if !reflect.DeepEqual(resource, want) {
		t.Fatalf("resource = %+v, want %+v", resource, want)
	}
}

func TestIngestionWatchAuthorizesIngestJob(t *testing.T) {
	t.Parallel()
	job := sampleJob()
	svc := &authzIngestionService{job: job}
	guard := authtest.Guard()
	router := newAuthzRouter(svc, guard, nil)

	rec := doRequest(router, http.MethodGet, "/api/v1/ingestion/jobs/"+job.ID+"/watch", testTenant, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	action, resource := singleGuardCall(t, guard)
	if action != "ingestion:watch" {
		t.Fatalf("action = %q, want ingestion:watch", action)
	}
	if resource.Type != "ingest_job" || resource.ID != job.ID {
		t.Fatalf("resource = %+v, want ingest_job %s", resource, job.ID)
	}
}

func TestIngestionListRunsAuthorizesParentJob(t *testing.T) {
	t.Parallel()
	job := sampleJob()
	svc := &authzIngestionService{job: job}
	guard := authtest.Guard()
	router := newAuthzRouter(svc, guard, nil)

	rec := doRequest(router, http.MethodGet, "/api/v1/ingestion/jobs/"+job.ID+"/runs", testTenant, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	action, resource := singleGuardCall(t, guard)
	if action != "ingestion:read" {
		t.Fatalf("action = %q, want ingestion:read", action)
	}
	if resource.Type != "ingest_job" || resource.ID != job.ID {
		t.Fatalf("resource = %+v, want ingest_job %s", resource, job.ID)
	}
}

func TestIngestionGetCrossTenantIsNotFoundBeforeAuthorization(t *testing.T) {
	t.Parallel()
	svc := &authzIngestionService{getErr: domain.ErrNotFound}
	guard := authtest.Guard()
	router := newAuthzRouter(svc, guard, nil)

	rec := doRequest(router, http.MethodGet, "/api/v1/ingestion/jobs/22222222-2222-7222-8222-222222222222", otherTenant, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if actions, _ := guard.Calls(); len(actions) != 0 {
		t.Fatalf("guard was called %d times for a cross-tenant load", len(actions))
	}
}

func TestIngestionCancelRejectsTerminalJobBeforeSideEffect(t *testing.T) {
	t.Parallel()
	job := sampleJob()
	job.Status = domain.StatusSucceeded
	svc := &authzIngestionService{job: job}
	guard := stateGuard{FakeGuard: authtest.Guard()}
	router := newAuthzRouter(svc, guard, nil)

	rec := doRequest(router, http.MethodPost, "/api/v1/ingestion/jobs/"+job.ID+"/cancel", testTenant, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if svc.called("CancelJob") {
		t.Fatalf("CancelJob ran for a terminal job")
	}
	action, resource := singleGuardCall(t, guard.FakeGuard)
	if action != "ingestion:cancel" {
		t.Fatalf("action = %q, want ingestion:cancel", action)
	}
	if resource.Status != string(domain.StatusSucceeded) {
		t.Fatalf("resource status = %q, want %q", resource.Status, domain.StatusSucceeded)
	}
}

func TestIngestionCancelAllowsRunningJob(t *testing.T) {
	t.Parallel()
	job := sampleJob()
	job.Status = domain.StatusRunning
	svc := &authzIngestionService{job: job}
	guard := stateGuard{FakeGuard: authtest.Guard()}
	router := newAuthzRouter(svc, guard, nil)

	rec := doRequest(router, http.MethodPost, "/api/v1/ingestion/jobs/"+job.ID+"/cancel", testTenant, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !svc.called("CancelJob") {
		t.Fatalf("CancelJob did not run after authorization")
	}
	if got, want := rec.Header().Get(authz.DecisionHeader), "decision-test"; got != want {
		t.Fatalf("decision header = %q, want %q", got, want)
	}
}
