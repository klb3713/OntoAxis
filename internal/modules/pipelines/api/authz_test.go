package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/api"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/repo"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/authz/authtest"
)

// stateGuard applies the Pipeline rules from
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

// authzServer wires the pipelines API directly with a decision guard. The
// module constructor does not expose the guard option, so authorization tests
// build the API seam themselves.
type authzServer struct {
	server  *httptest.Server
	store   *repo.MemoryStore
	manager *service.Manager
}

func newAuthzServer(t *testing.T, guard authz.DecisionGuard) *authzServer {
	t.Helper()
	store := repo.NewMemoryStore()
	manager := service.New(store, service.WithKestraAdapter(kestra.NewMemoryAdapter()))
	handler := api.New(manager, manager, api.HeaderTenantResolver{}, api.WithDecisionGuard(guard))
	router := chi.NewRouter()
	handler.Register(router)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &authzServer{server: server, store: store, manager: manager}
}

func (s *authzServer) do(t *testing.T, method, path, tenant, body string) (int, map[string]any, http.Header) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, s.server.URL+path, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		request.Header.Set("X-Ontoaxis-Tenant-Id", tenant)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(raw) == 0 {
		return response.StatusCode, nil, response.Header
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("%s %s returned non-JSON body %q: %v", method, path, raw, err)
	}
	return response.StatusCode, payload, response.Header
}

func (s *authzServer) seedPipeline(t *testing.T, tenantID, name string, status domain.PipelineStatus) string {
	t.Helper()
	now := time.Now().UTC()
	pipeline := domain.Pipeline{
		ID:          uuid.NewString(),
		TenantID:    tenantID,
		Name:        name,
		DisplayName: name,
		Type:        domain.PipelineTypeBatch,
		DAG:         json.RawMessage(`{"dataset_ids":[]}`),
		Status:      status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := s.store.CreatePipeline(context.Background(), pipeline); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	return pipeline.ID
}

func pipelineIDFrom(t *testing.T, payload map[string]any) string {
	t.Helper()
	id, _ := payload["id"].(string)
	if id == "" {
		t.Fatalf("response has no pipeline id: %+v", payload)
	}
	return id
}

func lastGuardCall(t *testing.T, guard *authtest.FakeGuard) (authz.Action, authz.ResourceFact) {
	t.Helper()
	actions, resources := guard.Calls()
	if len(actions) == 0 {
		t.Fatalf("guard was not called")
	}
	return actions[len(actions)-1], resources[len(resources)-1]
}

func assertDecisionHeader(t *testing.T, header http.Header) {
	t.Helper()
	if got, want := header.Get(authz.DecisionHeader), "decision-test"; got != want {
		t.Fatalf("decision header = %q, want %q", got, want)
	}
}

func TestPipelinesCreateAuthorizesTenant(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	server := newAuthzServer(t, guard)

	status, payload, header := server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, createBody("authz_create"))
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201; payload=%+v", status, payload)
	}
	assertDecisionHeader(t, header)
	action, resource := lastGuardCall(t, guard)
	if action != "pipeline:create" {
		t.Fatalf("action = %q, want pipeline:create", action)
	}
	want := authz.ResourceFact{Type: "tenant", ID: tenantA, TenantID: tenantA}
	if !reflect.DeepEqual(resource, want) {
		t.Fatalf("resource = %+v, want %+v", resource, want)
	}
}

func TestPipelinesListAuthorizesTenant(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	server := newAuthzServer(t, guard)

	status, payload, header := server.do(t, http.MethodGet, "/api/v1/pipelines", tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; payload=%+v", status, payload)
	}
	assertDecisionHeader(t, header)
	action, resource := lastGuardCall(t, guard)
	if action != "pipeline:read" {
		t.Fatalf("action = %q, want pipeline:read", action)
	}
	want := authz.ResourceFact{Type: "tenant", ID: tenantA, TenantID: tenantA}
	if !reflect.DeepEqual(resource, want) {
		t.Fatalf("resource = %+v, want %+v", resource, want)
	}
}

func TestPipelinesGetAuthorizesPipeline(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	server := newAuthzServer(t, guard)
	pipelineID := server.seedPipeline(t, tenantA, "authz_get", domain.PipelineStatusDraft)

	status, payload, header := server.do(t, http.MethodGet, "/api/v1/pipelines/"+pipelineID, tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; payload=%+v", status, payload)
	}
	assertDecisionHeader(t, header)
	action, resource := lastGuardCall(t, guard)
	if action != "pipeline:read" {
		t.Fatalf("action = %q, want pipeline:read", action)
	}
	want := authz.ResourceFact{Type: "pipeline", ID: pipelineID, TenantID: tenantA, Status: string(domain.PipelineStatusDraft)}
	if !reflect.DeepEqual(resource, want) {
		t.Fatalf("resource = %+v, want %+v", resource, want)
	}
}

func TestPipelinesCrossTenantGetIsNotFoundBeforeAuthorization(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	server := newAuthzServer(t, guard)
	pipelineID := server.seedPipeline(t, tenantA, "authz_cross_tenant", domain.PipelineStatusDraft)

	status, payload, _ := server.do(t, http.MethodGet, "/api/v1/pipelines/"+pipelineID, tenantB, "")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; payload=%+v", status, payload)
	}
	if actions, _ := guard.Calls(); len(actions) != 0 {
		t.Fatalf("guard was called %d times for a cross-tenant load", len(actions))
	}
}

func TestPipelinesUpdateAndDeleteAuthorizePipeline(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	server := newAuthzServer(t, guard)
	pipelineID := server.seedPipeline(t, tenantA, "authz_update_delete", domain.PipelineStatusDraft)

	status, payload, _ := server.do(t, http.MethodPatch, "/api/v1/pipelines/"+pipelineID, tenantA, `{"description":"updated"}`)
	if status != http.StatusOK {
		t.Fatalf("update status = %d, want 200; payload=%+v", status, payload)
	}
	action, resource := lastGuardCall(t, guard)
	if action != "pipeline:update" {
		t.Fatalf("update action = %q, want pipeline:update", action)
	}
	if resource.Type != "pipeline" || resource.ID != pipelineID || resource.Status != string(domain.PipelineStatusDraft) {
		t.Fatalf("update resource = %+v", resource)
	}

	status, payload, _ = server.do(t, http.MethodDelete, "/api/v1/pipelines/"+pipelineID, tenantA, "")
	if status != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204; payload=%+v", status, payload)
	}
	action, resource = lastGuardCall(t, guard)
	if action != "pipeline:delete" {
		t.Fatalf("delete action = %q, want pipeline:delete", action)
	}
	if resource.Type != "pipeline" || resource.ID != pipelineID {
		t.Fatalf("delete resource = %+v", resource)
	}
}

func TestPipelinesPublishAndInvokeAuthorizePipeline(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	server := newAuthzServer(t, guard)
	pipelineID := server.seedPipeline(t, tenantA, "authz_publish_invoke", domain.PipelineStatusDraft)

	status, payload, _ := server.do(t, http.MethodPost, "/api/v1/pipelines/"+pipelineID+"/publish", tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("publish status = %d, want 200; payload=%+v", status, payload)
	}
	action, resource := lastGuardCall(t, guard)
	if action != "pipeline:publish" {
		t.Fatalf("publish action = %q, want pipeline:publish", action)
	}
	if resource.Type != "pipeline" || resource.ID != pipelineID || resource.Status != string(domain.PipelineStatusDraft) {
		t.Fatalf("publish resource = %+v", resource)
	}

	status, payload, _ = server.do(t, http.MethodPost, "/api/v1/pipelines/"+pipelineID+"/runs", tenantA, "")
	if status != http.StatusAccepted {
		t.Fatalf("trigger status = %d, want 202; payload=%+v", status, payload)
	}
	action, resource = lastGuardCall(t, guard)
	if action != "pipeline:invoke" {
		t.Fatalf("trigger action = %q, want pipeline:invoke", action)
	}
	if resource.Type != "pipeline" || resource.ID != pipelineID || resource.Status != string(domain.PipelineStatusPublished) {
		t.Fatalf("trigger resource = %+v", resource)
	}
}

func TestPipelinesInvokeDraftPipelineDeniedByState(t *testing.T) {
	t.Parallel()
	guard := stateGuard{FakeGuard: authtest.Guard()}
	server := newAuthzServer(t, guard)
	pipelineID := server.seedPipeline(t, tenantA, "authz_invoke_draft", domain.PipelineStatusDraft)

	status, payload, _ := server.do(t, http.MethodPost, "/api/v1/pipelines/"+pipelineID+"/runs", tenantA, "")
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; payload=%+v", status, payload)
	}
	action, resource := lastGuardCall(t, guard.FakeGuard)
	if action != "pipeline:invoke" {
		t.Fatalf("action = %q, want pipeline:invoke", action)
	}
	if resource.Status != string(domain.PipelineStatusDraft) {
		t.Fatalf("resource status = %q, want draft", resource.Status)
	}
	page, err := server.manager.ListPipelineRuns(context.Background(), service.ListPipelineRunsRequest{
		TenantID:   tenantA,
		PipelineID: pipelineID,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("runs were created despite the state denial: %+v", page.Items)
	}
}

func TestPipelinesArchivedPipelineUpdateDeniedByState(t *testing.T) {
	t.Parallel()
	guard := stateGuard{FakeGuard: authtest.Guard()}
	server := newAuthzServer(t, guard)
	pipelineID := server.seedPipeline(t, tenantA, "authz_archived_update", domain.PipelineStatusArchived)

	status, payload, _ := server.do(t, http.MethodPatch, "/api/v1/pipelines/"+pipelineID, tenantA, `{"description":"should not apply"}`)
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; payload=%+v", status, payload)
	}
	action, resource := lastGuardCall(t, guard.FakeGuard)
	if action != "pipeline:update" {
		t.Fatalf("action = %q, want pipeline:update", action)
	}
	if resource.Type != "pipeline" || resource.Status != string(domain.PipelineStatusArchived) {
		t.Fatalf("resource = %+v, want archived pipeline", resource)
	}
	stored, err := server.store.GetPipeline(context.Background(), tenantA, pipelineID)
	if err != nil {
		t.Fatalf("get pipeline: %v", err)
	}
	if stored.Description == "should not apply" {
		t.Fatalf("archived pipeline was updated despite the state denial")
	}
}

func TestPipelinesRunAuthorizations(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	server := newAuthzServer(t, guard)
	pipelineID := server.seedPipeline(t, tenantA, "authz_runs", domain.PipelineStatusPublished)

	status, payload, _ := server.do(t, http.MethodPost, "/api/v1/pipelines/"+pipelineID+"/runs", tenantA, "")
	if status != http.StatusAccepted {
		t.Fatalf("trigger status = %d, want 202; payload=%+v", status, payload)
	}
	runID, _ := payload["id"].(string)
	if runID == "" {
		t.Fatalf("run response has no id: %+v", payload)
	}

	status, payload, _ = server.do(t, http.MethodGet, "/api/v1/pipelines/"+pipelineID+"/runs/"+runID, tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("get run status = %d, want 200; payload=%+v", status, payload)
	}
	action, resource := lastGuardCall(t, guard)
	if action != "pipeline_run:read" {
		t.Fatalf("get run action = %q, want pipeline_run:read", action)
	}
	runStatus, _ := payload["status"].(string)
	if runStatus == "" {
		t.Fatalf("run response has no status: %+v", payload)
	}
	want := authz.ResourceFact{
		Type:     "pipeline_run",
		ID:       runID,
		TenantID: tenantA,
		Status:   runStatus,
		Ancestors: []authz.Ancestor{
			{Type: "pipeline", ID: pipelineID},
			{Type: "tenant", ID: tenantA},
		},
	}
	if !reflect.DeepEqual(resource, want) {
		t.Fatalf("run resource = %+v, want %+v", resource, want)
	}

	status, payload, _ = server.do(t, http.MethodGet, "/api/v1/pipelines/"+pipelineID+"/runs", tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("list runs status = %d, want 200; payload=%+v", status, payload)
	}
	action, resource = lastGuardCall(t, guard)
	if action != "pipeline_run:read" {
		t.Fatalf("list runs action = %q, want pipeline_run:read", action)
	}
	if resource.Type != "pipeline" || resource.ID != pipelineID {
		t.Fatalf("list runs resource = %+v, want pipeline %s", resource, pipelineID)
	}
}

func TestPipelinesScheduleAuthorizations(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	server := newAuthzServer(t, guard)
	pipelineID := server.seedPipeline(t, tenantA, "authz_schedules", domain.PipelineStatusDraft)

	status, payload, _ := server.do(t, http.MethodPost, "/api/v1/pipelines/"+pipelineID+"/schedules", tenantA,
		`{"cron":"0 * * * *","timezone":"UTC"}`)
	if status != http.StatusCreated {
		t.Fatalf("create schedule status = %d, want 201; payload=%+v", status, payload)
	}
	scheduleID, _ := payload["id"].(string)
	if scheduleID == "" {
		t.Fatalf("schedule response has no id: %+v", payload)
	}
	action, resource := lastGuardCall(t, guard)
	if action != "pipeline_schedule:write" {
		t.Fatalf("create schedule action = %q, want pipeline_schedule:write", action)
	}
	if resource.Type != "pipeline" || resource.ID != pipelineID || resource.Status != string(domain.PipelineStatusDraft) {
		t.Fatalf("create schedule resource = %+v", resource)
	}

	status, payload, _ = server.do(t, http.MethodGet, "/api/v1/pipelines/"+pipelineID+"/schedules", tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("list schedules status = %d, want 200; payload=%+v", status, payload)
	}
	action, resource = lastGuardCall(t, guard)
	if action != "pipeline_schedule:read" {
		t.Fatalf("list schedules action = %q, want pipeline_schedule:read", action)
	}
	if resource.Type != "pipeline" || resource.ID != pipelineID {
		t.Fatalf("list schedules resource = %+v", resource)
	}

	wantSchedule := authz.ResourceFact{
		Type:     "pipeline_schedule",
		ID:       scheduleID,
		TenantID: tenantA,
		Ancestors: []authz.Ancestor{
			{Type: "pipeline", ID: pipelineID},
			{Type: "tenant", ID: tenantA},
		},
	}

	status, payload, _ = server.do(t, http.MethodGet, "/api/v1/pipelines/schedules/"+scheduleID, tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("get schedule status = %d, want 200; payload=%+v", status, payload)
	}
	action, resource = lastGuardCall(t, guard)
	if action != "pipeline_schedule:read" {
		t.Fatalf("get schedule action = %q, want pipeline_schedule:read", action)
	}
	if !reflect.DeepEqual(resource, wantSchedule) {
		t.Fatalf("get schedule resource = %+v, want %+v", resource, wantSchedule)
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
		action authz.Action
	}{
		{name: "pause", method: http.MethodPost, path: "/api/v1/pipelines/schedules/" + scheduleID + "/pause", action: "pipeline_schedule:write"},
		{name: "resume", method: http.MethodPost, path: "/api/v1/pipelines/schedules/" + scheduleID + "/resume", action: "pipeline_schedule:write"},
		{name: "update", method: http.MethodPatch, path: "/api/v1/pipelines/schedules/" + scheduleID, action: "pipeline_schedule:write"},
		{name: "delete", method: http.MethodDelete, path: "/api/v1/pipelines/schedules/" + scheduleID, action: "pipeline_schedule:delete"},
	} {
		var body string
		if tc.name == "update" {
			body = `{"enabled":true}`
		}
		status, payload, _ := server.do(t, tc.method, tc.path, tenantA, body)
		if status != http.StatusOK && status != http.StatusNoContent {
			t.Fatalf("%s status = %d; payload=%+v", tc.name, status, payload)
		}
		action, resource := lastGuardCall(t, guard)
		if action != tc.action {
			t.Fatalf("%s action = %q, want %q", tc.name, action, tc.action)
		}
		if resource.Type != "pipeline_schedule" || resource.ID != scheduleID {
			t.Fatalf("%s resource = %+v, want pipeline_schedule %s", tc.name, resource, scheduleID)
		}
	}
}

func TestPipelinesDeleteDeniedByGuard(t *testing.T) {
	t.Parallel()
	guard := authtest.Deny(authz.ReasonRoleNotPermitted)
	server := newAuthzServer(t, guard)
	pipelineID := server.seedPipeline(t, tenantA, "authz_delete_denied", domain.PipelineStatusDraft)

	status, payload, _ := server.do(t, http.MethodDelete, "/api/v1/pipelines/"+pipelineID, tenantA, "")
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; payload=%+v", status, payload)
	}
	if _, err := server.store.GetPipeline(context.Background(), tenantA, pipelineID); err != nil {
		t.Fatalf("pipeline was deleted despite denied authorization: %v", err)
	}
	action, _ := lastGuardCall(t, guard)
	if action != "pipeline:delete" {
		t.Fatalf("action = %q, want pipeline:delete", action)
	}
}
