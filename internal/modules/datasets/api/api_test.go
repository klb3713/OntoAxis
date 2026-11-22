package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/api"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/repo"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

type apiFixture struct {
	router   http.Handler
	tenantID string
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	svc := service.New(repo.NewMemoryStore())
	router := chi.NewRouter()
	api.NewHandler(svc, api.GatewayTenantResolver{}).RegisterRoutes(router)
	return &apiFixture{router: router, tenantID: testUUID(t)}
}

func testUUID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	return id.String()
}

func (f *apiFixture) request(t *testing.T, method, path string, body any, tenantID string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if tenantID != "" {
		request.Header.Set("X-Ontoaxis-Tenant-Id", tenantID)
	}
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, request)
	return recorder
}

func decode[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", recorder.Body.String(), err)
	}
	return value
}

func createDatasetHTTP(t *testing.T, fixture *apiFixture, name string) string {
	t.Helper()
	recorder := fixture.request(t, http.MethodPost, "/api/v1/datasets", map[string]any{
		"name":             name,
		"display_name":     "Test " + name,
		"format":           "iceberg",
		"storage_location": "s3://warehouse/" + name,
	}, fixture.tenantID)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST /datasets status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decode[struct {
		ID string `json:"id"`
	}](t, recorder)
	if response.ID == "" {
		t.Fatal("POST /datasets response has empty id")
	}
	return response.ID
}

func TestDatasetHTTPCRUDAndErrors(t *testing.T) {
	fixture := newAPIFixture(t)

	if recorder := fixture.request(t, http.MethodPost, "/api/v1/datasets", map[string]any{"name": "orders", "format": "iceberg"}, ""); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing tenant status = %d, want 401", recorder.Code)
	}
	recorder := fixture.request(t, http.MethodPost, "/api/v1/datasets", map[string]any{
		"name":             "orders",
		"display_name":     "Orders",
		"format":           "iceberg",
		"storage_location": "s3://warehouse/orders",
		"tags":             map[string]string{"domain": "sales"},
	}, fixture.tenantID)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	created := decode[struct {
		ID  string `json:"id"`
		RID string `json:"rid"`
	}](t, recorder)
	if created.ID == "" || created.RID != "ri.ontoaxis.main.dataset."+created.ID {
		t.Fatalf("created = %+v", created)
	}

	recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets/"+created.ID, nil, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets/"+created.ID, nil, ""); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("get without tenant status = %d, want 401", recorder.Code)
	}
	otherTenant := testUUID(t)
	if recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets/"+created.ID, nil, otherTenant); recorder.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant get status = %d, want 404", recorder.Code)
	}
	if recorder = fixture.request(t, http.MethodPatch, "/api/v1/datasets/"+created.ID, map[string]any{"display_name": "Stolen"}, otherTenant); recorder.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant patch status = %d, want 404", recorder.Code)
	}
	if recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets/"+testUUID(t), nil, fixture.tenantID); recorder.Code != http.StatusNotFound {
		t.Fatalf("get unknown status = %d, want 404", recorder.Code)
	}
	if recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets/not-a-uuid", nil, fixture.tenantID); recorder.Code != http.StatusBadRequest {
		t.Fatalf("get invalid uuid status = %d, want 400", recorder.Code)
	}

	recorder = fixture.request(t, http.MethodPost, "/api/v1/datasets", map[string]any{"name": "x", "format": "iceberg", "unknown": true}, fixture.tenantID)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want 400", recorder.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/datasets", bytes.NewBufferString("{"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Ontoaxis-Tenant-Id", fixture.tenantID)
	recorder = httptest.NewRecorder()
	fixture.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed json status = %d, want 400", recorder.Code)
	}

	recorder = fixture.request(t, http.MethodPatch, "/api/v1/datasets/"+created.ID, map[string]any{"display_name": "Orders v2"}, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder = fixture.request(t, http.MethodDelete, "/api/v1/datasets/"+created.ID, nil, fixture.tenantID); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", recorder.Code)
	}
	if recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets/"+created.ID, nil, fixture.tenantID); recorder.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, want 404", recorder.Code)
	}
}

func TestDatasetHTTPPagination(t *testing.T) {
	fixture := newAPIFixture(t)
	for _, name := range []string{"one", "two", "three"} {
		createDatasetHTTP(t, fixture, name)
	}
	recorder := fixture.request(t, http.MethodGet, "/api/v1/datasets?page=1&per_page=2", nil, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	list := decode[struct {
		Items      []json.RawMessage `json:"items"`
		Page       int               `json:"page"`
		PerPage    int               `json:"per_page"`
		Total      int64             `json:"total"`
		TotalPages int               `json:"total_pages"`
	}](t, recorder)
	if len(list.Items) != 2 || list.Total != 3 || list.TotalPages != 2 || list.Page != 1 || list.PerPage != 2 {
		t.Fatalf("list = %+v", list)
	}
	if recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets?per_page=201", nil, fixture.tenantID); recorder.Code != http.StatusBadRequest {
		t.Fatalf("per_page=201 status = %d, want 400", recorder.Code)
	}
	if recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets?page=abc", nil, fixture.tenantID); recorder.Code != http.StatusBadRequest {
		t.Fatalf("page=abc status = %d, want 400", recorder.Code)
	}
}

func TestBranchAndTransactionHTTP(t *testing.T) {
	fixture := newAPIFixture(t)
	datasetID := createDatasetHTTP(t, fixture, "events")

	recorder := fixture.request(t, http.MethodPost, "/api/v1/datasets/"+datasetID+"/branches", map[string]any{"name": "dev"}, fixture.tenantID)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create branch status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	branch := decode[struct {
		ID string `json:"id"`
	}](t, recorder)
	if branch.ID == "" {
		t.Fatal("branch id is empty")
	}
	recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets/"+datasetID+"/branches", nil, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list branches status = %d", recorder.Code)
	}

	recorder = fixture.request(t, http.MethodPost, "/api/v1/datasets/"+datasetID+"/transactions", map[string]any{
		"branch_id":        branch.ID,
		"transaction_type": "append",
		"message":          "ingest",
	}, fixture.tenantID)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("begin transaction status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	transaction := decode[struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}](t, recorder)
	if transaction.ID == "" || transaction.Status != "open" {
		t.Fatalf("transaction = %+v", transaction)
	}
	recorder = fixture.request(t, http.MethodPost, "/api/v1/datasets/"+datasetID+"/transactions/"+transaction.ID+"/commit", nil, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("commit status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	committed := decode[struct {
		Status string `json:"status"`
	}](t, recorder)
	if committed.Status != "committed" {
		t.Fatalf("committed status = %q", committed.Status)
	}
	recorder = fixture.request(t, http.MethodPost, "/api/v1/datasets/"+datasetID+"/transactions/"+transaction.ID+"/abort", nil, fixture.tenantID)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("abort committed status = %d, want 409 body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets/"+datasetID+"/transactions", nil, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list transactions status = %d", recorder.Code)
	}
}

func TestViewAndFileHTTP(t *testing.T) {
	fixture := newAPIFixture(t)
	datasetID := createDatasetHTTP(t, fixture, "customers")

	recorder := fixture.request(t, http.MethodPost, "/api/v1/datasets/"+datasetID+"/views", map[string]any{
		"name":  "active",
		"query": "SELECT * FROM customers",
	}, fixture.tenantID)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create view status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	view := decode[struct {
		ID      string `json:"id"`
		Version int32  `json:"version"`
	}](t, recorder)
	if view.ID == "" || view.Version != 1 {
		t.Fatalf("view = %+v", view)
	}
	recorder = fixture.request(t, http.MethodPatch, "/api/v1/datasets/"+datasetID+"/views/"+view.ID, map[string]any{"query": "SELECT 1"}, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update view status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	updated := decode[struct {
		Version int32 `json:"version"`
	}](t, recorder)
	if updated.Version != 2 {
		t.Fatalf("updated view version = %d, want 2", updated.Version)
	}
	recorder = fixture.request(t, http.MethodDelete, "/api/v1/datasets/"+datasetID+"/views/"+view.ID, nil, fixture.tenantID)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete view status = %d, want 204", recorder.Code)
	}

	recorder = fixture.request(t, http.MethodPost, "/api/v1/datasets/"+datasetID+"/files", map[string]any{
		"object_key":   "customers/part-0001.parquet",
		"size_bytes":   1024,
		"row_count":    10,
		"content_type": "application/vnd.apache.parquet",
	}, fixture.tenantID)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create file status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	file := decode[struct {
		ID string `json:"id"`
	}](t, recorder)
	if file.ID == "" {
		t.Fatal("file id is empty")
	}
	recorder = fixture.request(t, http.MethodPost, "/api/v1/datasets/"+datasetID+"/files", map[string]any{
		"object_key": "",
		"size_bytes": -1,
	}, fixture.tenantID)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid file metadata status = %d, want 400 body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets/"+datasetID+"/files/"+file.ID, nil, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get file status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = fixture.request(t, http.MethodGet, "/api/v1/datasets/"+datasetID+"/files", nil, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list files status = %d", recorder.Code)
	}
	files := decode[struct {
		Total int64 `json:"total"`
	}](t, recorder)
	if files.Total != 1 {
		t.Fatalf("files total = %d, want 1", files.Total)
	}
	recorder = fixture.request(t, http.MethodDelete, "/api/v1/datasets/"+datasetID+"/files/"+file.ID, nil, fixture.tenantID)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete file status = %d, want 204", recorder.Code)
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	fixture := newAPIFixture(t)
	recorder := fixture.request(t, http.MethodGet, "/api/v1/not-a-route", nil, fixture.tenantID)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, want 404", recorder.Code)
	}
}

func TestInvalidTenantHeaderReturns401(t *testing.T) {
	fixture := newAPIFixture(t)
	recorder := fixture.request(t, http.MethodGet, "/api/v1/datasets", nil, "not-a-uuid")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("invalid tenant status = %d, want 401", recorder.Code)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", recorder.Body.String(), err)
	}
	if payload.Error.Code != "unauthorized" {
		t.Fatalf("invalid tenant error code = %q, want unauthorized", payload.Error.Code)
	}
}

type fakeDecisionGuard struct {
	allow     bool
	actions   []string
	resources []string
}

func (f *fakeDecisionGuard) Require(_ context.Context, action authz.Action, resource authz.ResourceFact) (authz.Decision, error) {
	f.actions = append(f.actions, string(action))
	f.resources = append(f.resources, resource.Type+":"+resource.ID)
	if f.allow {
		return authz.Decision{Allow: true, DecisionID: "decision-test", Action: action, Resource: resource}, nil
	}
	return authz.Decision{DecisionID: "decision-test", Action: action, Resource: resource}, &authz.Error{Reason: authz.ReasonRoleNotPermitted, Engine: "test"}
}

func (f *fakeDecisionGuard) WriteHTTP(w http.ResponseWriter, r *http.Request, _ error) {
	httperr.Write(w, r, httperr.Forbidden("permission denied"))
}

func (f *fakeDecisionGuard) SetDecisionHeader(w http.ResponseWriter, decision authz.Decision) {
	w.Header().Set(authz.DecisionHeader, decision.DecisionID)
}

func newAuthorizedFixture(t *testing.T, allow bool) (*apiFixture, *fakeDecisionGuard) {
	t.Helper()
	svc := service.New(repo.NewMemoryStore())
	guard := &fakeDecisionGuard{allow: allow}
	router := chi.NewRouter()
	api.NewHandler(svc, api.GatewayTenantResolver{}, api.WithDecisionGuard(guard)).RegisterRoutes(router)
	return &apiFixture{router: router, tenantID: testUUID(t)}, guard
}

func TestDatasetAuthorizationSeam(t *testing.T) {
	fixture, guard := newAuthorizedFixture(t, true)
	id := createDatasetHTTP(t, fixture, "orders")
	recorder := fixture.request(t, http.MethodGet, "/api/v1/datasets/"+id, nil, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get(authz.DecisionHeader); got != "decision-test" {
		t.Fatalf("decision header = %q", got)
	}
	if len(guard.actions) != 2 || guard.actions[0] != "dataset:create" || guard.actions[1] != "dataset:read" {
		t.Fatalf("unexpected authorization actions: %v", guard.actions)
	}
	if guard.resources[0] != "tenant:"+fixture.tenantID {
		t.Fatalf("create must authorize tenant scope, got %q", guard.resources[0])
	}
}

func TestDatasetAuthorizationDeny(t *testing.T) {
	fixture, _ := newAuthorizedFixture(t, false)
	recorder := fixture.request(t, http.MethodPost, "/api/v1/datasets", map[string]any{"name": "orders", "format": "iceberg"}, fixture.tenantID)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("denied create status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDatasetCrossTenantIsNotFoundBeforeAuthorization(t *testing.T) {
	fixture, guard := newAuthorizedFixture(t, true)
	id := createDatasetHTTP(t, fixture, "orders")
	recorder := fixture.request(t, http.MethodGet, "/api/v1/datasets/"+id, nil, testUUID(t))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant GET status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	for _, action := range guard.actions {
		if action == "dataset:read" {
			t.Fatal("cross-tenant lookup must not reach authorization")
		}
	}
}

func TestDatasetListProducesSingleDecision(t *testing.T) {
	fixture, guard := newAuthorizedFixture(t, true)
	recorder := fixture.request(t, http.MethodGet, "/api/v1/datasets", nil, fixture.tenantID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET list status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(guard.actions) != 1 || guard.actions[0] != "dataset:read" {
		t.Fatalf("list authorization actions = %v, want one dataset:read", guard.actions)
	}
	if guard.resources[0] != "tenant:"+fixture.tenantID {
		t.Fatalf("list must authorize tenant scope, got %q", guard.resources[0])
	}
}
