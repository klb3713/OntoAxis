package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/api"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/repo"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
)

type testServer struct {
	server  *httptest.Server
	store   *repo.MemoryStore
	adapter *kestra.MemoryAdapter
}

func newTestServer(t *testing.T, extra ...pipelines.Option) *testServer {
	t.Helper()
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()
	options := []pipelines.Option{
		pipelines.WithStore(store),
		pipelines.WithKestraAdapter(adapter),
		pipelines.WithTenantResolver(api.HeaderTenantResolver{}),
	}
	options = append(options, extra...)
	module := pipelines.New(nil, options...)
	router := chi.NewRouter()
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httperr.Write(w, r, httperr.NotFound("resource not found"))
	})
	module.RegisterRoutes(router)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &testServer{server: server, store: store, adapter: adapter}
}

func (s *testServer) do(t *testing.T, method, path, tenant, body string, headers ...string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, s.server.URL+path, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if tenant != "" {
		request.Header.Set("X-Ontoaxis-Tenant-Id", tenant)
	}
	request.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
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
		return response.StatusCode, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("%s %s returned non-JSON body %q: %v", method, path, raw, err)
	}
	return response.StatusCode, payload
}

func createBody(name string) string {
	return `{"name":"` + name + `","display_name":"Test","description":"desc","type":"batch","definition":{"dataset_ids":[]}}`
}

func errorCode(payload map[string]any) string {
	errorObject, _ := payload["error"].(map[string]any)
	code, _ := errorObject["code"].(string)
	return code
}

func TestTenantContextIsRequired(t *testing.T) {
	server := newTestServer(t)
	status, payload := server.do(t, http.MethodPost, "/api/v1/pipelines", "", createBody("no_tenant"))
	if status != http.StatusUnauthorized || errorCode(payload) != "unauthorized" {
		t.Fatalf("missing tenant = %d %+v", status, payload)
	}

	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, createBody("external_header"),
		"X-Tenant-Id", tenantB)
	if status != http.StatusUnauthorized {
		t.Fatalf("external tenant header = %d %+v", status, payload)
	}

	// The module's secure default rejects requests rather than failing open.
	defaultModule := pipelines.New(nil, pipelines.WithStore(repo.NewMemoryStore()))
	defaultRouter := chi.NewRouter()
	defaultModule.RegisterRoutes(defaultRouter)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", nil)
	recorder := httptest.NewRecorder()
	defaultRouter.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || errorCode(decode(t, recorder.Body.Bytes())) != "unauthorized" {
		t.Fatalf("default resolver = %d %s", recorder.Code, recorder.Body.String())
	}

	// A handler wired with no resolver at all fails closed with 503.
	unwired := api.New(nil, nil, nil)
	unwiredRouter := chi.NewRouter()
	unwired.Register(unwiredRouter)
	request = httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", nil)
	recorder = httptest.NewRecorder()
	unwiredRouter.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || errorCode(decode(t, recorder.Body.Bytes())) != "unavailable" {
		t.Fatalf("nil resolver = %d %s", recorder.Code, recorder.Body.String())
	}

	// A resolver without services must not panic; the API fails closed.
	noServices := api.New(nil, nil, api.HeaderTenantResolver{})
	noServicesRouter := chi.NewRouter()
	noServices.Register(noServicesRouter)
	request = httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", nil)
	request.Header.Set("X-Ontoaxis-Tenant-Id", tenantA)
	recorder = httptest.NewRecorder()
	noServicesRouter.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || errorCode(decode(t, recorder.Body.Bytes())) != "unavailable" {
		t.Fatalf("nil services = %d %s", recorder.Code, recorder.Body.String())
	}
}

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return payload
}

func TestPipelineCRUDAndErrorContract(t *testing.T) {
	server := newTestServer(t)

	status, payload := server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, createBody("orders_daily"))
	if status != http.StatusCreated {
		t.Fatalf("create = %d %+v", status, payload)
	}
	id, _ := payload["id"].(string)
	if id == "" || payload["status"] != "draft" || payload["version"] != float64(0) {
		t.Fatalf("created payload = %+v", payload)
	}
	if _, ok := payload["definition"].(map[string]any); !ok {
		t.Fatalf("definition = %+v", payload["definition"])
	}

	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, createBody("orders_daily"))
	if status != http.StatusConflict || errorCode(payload) != "already_exists" {
		t.Fatalf("duplicate = %d %+v", status, payload)
	}

	status, payload = server.do(t, http.MethodGet, "/api/v1/pipelines/"+id, tenantA, "")
	if status != http.StatusOK || payload["id"] != id {
		t.Fatalf("get = %d %+v", status, payload)
	}

	status, payload = server.do(t, http.MethodPatch, "/api/v1/pipelines/"+id, tenantA,
		`{"display_name":"Updated"}`)
	if status != http.StatusOK || payload["display_name"] != "Updated" {
		t.Fatalf("patch = %d %+v", status, payload)
	}

	status, _ = server.do(t, http.MethodGet, "/api/v1/pipelines/"+id, tenantB, "")
	if status != http.StatusNotFound {
		t.Fatalf("cross-tenant get = %d", status)
	}

	status, _ = server.do(t, http.MethodDelete, "/api/v1/pipelines/"+id, tenantA, "")
	if status != http.StatusNoContent {
		t.Fatalf("delete = %d", status)
	}
	status, payload = server.do(t, http.MethodGet, "/api/v1/pipelines/"+id, tenantA, "")
	if status != http.StatusNotFound || errorCode(payload) != "not_found" {
		t.Fatalf("get after delete = %d %+v", status, payload)
	}
}

func TestValidationErrors(t *testing.T) {
	server := newTestServer(t)
	cases := []struct {
		name string
		body string
	}{
		{"invalid type", `{"name":"bad_type","type":"streaming"}`},
		{"invalid definition", `{"name":"bad_dag","type":"batch","definition":[]}`},
		{"unknown field", `{"name":"unknown","type":"batch","extra":true}`},
		{"malformed json", `{`},
	}
	for _, testCase := range cases {
		status, payload := server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, testCase.body)
		if status != http.StatusBadRequest || errorCode(payload) != "invalid_argument" {
			t.Fatalf("%s = %d %+v", testCase.name, status, payload)
		}
	}

	status, _ := server.do(t, http.MethodGet, "/api/v1/unknown", tenantA, "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown route = %d", status)
	}
}

func TestPaginationContract(t *testing.T) {
	server := newTestServer(t)
	for _, name := range []string{"one", "two", "three"} {
		if status, payload := server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, createBody(name)); status != http.StatusCreated {
			t.Fatalf("create %s = %d %+v", name, status, payload)
		}
	}
	status, payload := server.do(t, http.MethodGet, "/api/v1/pipelines?page=2&per_page=1", tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("list = %d %+v", status, payload)
	}
	pipelinesValue, _ := payload["pipelines"].([]any)
	page, _ := payload["page"].(map[string]any)
	if len(pipelinesValue) != 1 || page["total"] != float64(3) || page["total_pages"] != float64(3) || page["page"] != float64(2) {
		t.Fatalf("page payload = %+v", payload)
	}

	for _, query := range []string{"per_page=201", "per_page=abc", "page=-1", "status=unknown"} {
		status, payload := server.do(t, http.MethodGet, "/api/v1/pipelines?"+query, tenantA, "")
		if status != http.StatusBadRequest {
			t.Fatalf("query %s = %d %+v", query, status, payload)
		}
	}
}

func TestPublishAndRunEndpoints(t *testing.T) {
	server := newTestServer(t)
	status, payload := server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, createBody("runnable"))
	if status != http.StatusCreated {
		t.Fatalf("create = %d %+v", status, payload)
	}
	id := payload["id"].(string)

	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/runs", tenantA, "")
	if status != http.StatusConflict || errorCode(payload) != "conflict" {
		t.Fatalf("trigger draft = %d %+v", status, payload)
	}

	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/publish", tenantA, `{"published_by":"tester"}`)
	if status != http.StatusOK || payload["version"] != float64(1) || payload["status"] != "published" {
		t.Fatalf("publish = %d %+v", status, payload)
	}
	if payload["published_version_id"] == "" {
		t.Fatalf("publish response missing version id: %+v", payload)
	}

	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/runs", tenantA, "")
	if status != http.StatusAccepted || payload["status"] != "running" || payload["trigger_type"] != "manual" {
		t.Fatalf("trigger = %d %+v", status, payload)
	}
	runID, _ := payload["id"].(string)

	status, payload = server.do(t, http.MethodGet, "/api/v1/pipelines/"+id+"/runs/"+runID, tenantA, "")
	if status != http.StatusOK || payload["id"] != runID {
		t.Fatalf("get run = %d %+v", status, payload)
	}
	status, payload = server.do(t, http.MethodGet, "/api/v1/pipelines/"+id+"/runs", tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("list runs = %d %+v", status, payload)
	}
	runs, _ := payload["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("runs = %+v", payload)
	}

	server.adapter.TriggerError = io.ErrUnexpectedEOF
	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/runs", tenantA, "")
	if status != http.StatusServiceUnavailable || errorCode(payload) != "unavailable" {
		t.Fatalf("adapter failure = %d %+v", status, payload)
	}
	status, payload = server.do(t, http.MethodGet, "/api/v1/pipelines/"+id+"/runs?status=failed", tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("list failed runs = %d %+v", status, payload)
	}

	// Editing the executable definition returns the Pipeline to draft and the
	// old published version must not be triggered with the new draft DAG.
	status, payload = server.do(t, http.MethodPatch, "/api/v1/pipelines/"+id, tenantA,
		`{"definition":{"dataset_ids":[],"stages":[{"id":"edited"}]}}`)
	if status != http.StatusOK || payload["status"] != "draft" || payload["version"] != float64(1) {
		t.Fatalf("edit published pipeline = %d %+v", status, payload)
	}
	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/runs", tenantA, "")
	if status != http.StatusConflict || errorCode(payload) != "conflict" {
		t.Fatalf("trigger edited draft = %d %+v", status, payload)
	}
}

func TestScheduleEndpoints(t *testing.T) {
	server := newTestServer(t)
	status, payload := server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, createBody("scheduled"))
	if status != http.StatusCreated {
		t.Fatalf("create pipeline = %d %+v", status, payload)
	}
	pipelineID := payload["id"].(string)

	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/"+pipelineID+"/schedules", tenantA,
		`{"cron":"0 * * * *","timezone":"Asia/Shanghai"}`)
	if status != http.StatusCreated || payload["enabled"] != true || payload["next_run_at"] == nil {
		t.Fatalf("create schedule = %d %+v", status, payload)
	}
	scheduleID, _ := payload["id"].(string)

	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/"+pipelineID+"/schedules", tenantA,
		`{"cron":"0 * * * *","timezone":"UTC"}`)
	if status != http.StatusConflict || errorCode(payload) != "already_exists" {
		t.Fatalf("duplicate schedule = %d %+v", status, payload)
	}

	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/"+pipelineID+"/schedules", tenantA,
		`{"cron":"not-a-cron"}`)
	if status != http.StatusBadRequest || errorCode(payload) != "invalid_argument" {
		t.Fatalf("invalid schedule = %d %+v", status, payload)
	}

	status, payload = server.do(t, http.MethodGet, "/api/v1/pipelines/"+pipelineID+"/schedules", tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("list schedules = %d %+v", status, payload)
	}
	schedules, _ := payload["schedules"].([]any)
	if len(schedules) != 1 {
		t.Fatalf("schedules = %+v", payload)
	}

	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/schedules/"+scheduleID+"/pause", tenantA, "")
	if status != http.StatusOK || payload["enabled"] != false {
		t.Fatalf("pause = %d %+v", status, payload)
	}
	if _, exists := payload["next_run_at"]; exists {
		t.Fatalf("paused schedule still has next_run_at: %+v", payload)
	}
	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/schedules/"+scheduleID+"/resume", tenantA, "")
	if status != http.StatusOK || payload["enabled"] != true || payload["next_run_at"] == nil {
		t.Fatalf("resume = %d %+v", status, payload)
	}

	status, _ = server.do(t, http.MethodDelete, "/api/v1/pipelines/schedules/"+scheduleID, tenantA, "")
	if status != http.StatusNoContent {
		t.Fatalf("delete schedule = %d", status)
	}
	status, payload = server.do(t, http.MethodGet, "/api/v1/pipelines/schedules/"+scheduleID, tenantA, "")
	if status != http.StatusNotFound || errorCode(payload) != "not_found" {
		t.Fatalf("get deleted schedule = %d %+v", status, payload)
	}
}

func TestDatasetResolverIsEnforcedByPublish(t *testing.T) {
	datasetID := "33333333-3333-4333-8333-333333333333"
	resolver := fakeResolver{exists: map[string]bool{}}
	server := newTestServer(t, pipelines.WithDatasetResolver(resolver))
	body := `{"name":"with_dataset","type":"batch","definition":{"dataset_ids":["` + datasetID + `"]}}`
	status, payload := server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, body)
	if status != http.StatusCreated {
		t.Fatalf("create = %d %+v", status, payload)
	}
	id := payload["id"].(string)
	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/publish", tenantA, "")
	if status != http.StatusBadRequest || errorCode(payload) != "invalid_argument" {
		t.Fatalf("publish missing dataset = %d %+v", status, payload)
	}

	server = newTestServer(t, pipelines.WithDatasetResolver(fakeResolver{exists: map[string]bool{datasetID: true}}))
	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, body)
	if status != http.StatusCreated {
		t.Fatalf("create = %d %+v", status, payload)
	}
	id = payload["id"].(string)
	status, payload = server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/publish", tenantA, "")
	if status != http.StatusOK {
		t.Fatalf("publish existing dataset = %d %+v", status, payload)
	}
}

type fakeResolver struct {
	exists map[string]bool
}

func (f fakeResolver) DatasetExists(context.Context, string, string) (bool, error) {
	return f.exists["33333333-3333-4333-8333-333333333333"], nil
}

func TestIdempotencyKeyValidation(t *testing.T) {
	server := newTestServer(t)
	pipelineID := "11111111-1111-4111-8111-111111111111"
	longKey := ""
	for i := 0; i < 256; i++ {
		longKey += "a"
	}
	status, body := server.do(t, http.MethodPost, "/api/v1/pipelines/"+pipelineID+"/publish", "11111111-1111-4111-8111-111111111111", `{}`, "Idempotency-Key", longKey)
	if status != http.StatusBadRequest {
		t.Fatalf("publish status=%d body=%v", status, body)
	}
	status, body = server.do(t, http.MethodPost, "/api/v1/pipelines/"+pipelineID+"/runs", "11111111-1111-4111-8111-111111111111", "", "Idempotency-Key", "bad key")
	if status != http.StatusBadRequest {
		t.Fatalf("trigger status=%d body=%v", status, body)
	}
}

func TestIdempotentPublishAndTriggerReplay(t *testing.T) {
	server := newTestServer(t)
	status, payload := server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, createBody("idem"))
	if status != http.StatusCreated {
		t.Fatalf("create = %d %+v", status, payload)
	}
	id := payload["id"].(string)

	firstStatus, first := server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/publish", tenantA, `{"published_by":"tester"}`, "Idempotency-Key", "pub-1")
	if firstStatus != http.StatusOK {
		t.Fatalf("publish = %d %+v", firstStatus, first)
	}
	secondStatus, second := server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/publish", tenantA, `{"published_by":"tester"}`, "Idempotency-Key", "pub-1")
	if secondStatus != http.StatusOK || second["published_version_id"] != first["published_version_id"] {
		t.Fatalf("replay publish = %d %+v want version %v", secondStatus, second, first["published_version_id"])
	}
	status, conflict := server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/publish", tenantA, `{"published_by":"other"}`, "Idempotency-Key", "pub-1")
	if status != http.StatusConflict || errorCode(conflict) != "already_exists" {
		t.Fatalf("conflict publish = %d %+v", status, conflict)
	}

	status, run := server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/runs", tenantA, "", "Idempotency-Key", "run-1")
	if status != http.StatusAccepted {
		t.Fatalf("trigger = %d %+v", status, run)
	}
	if server.adapter.TriggeredCount != 1 {
		t.Fatalf("triggered count = %d, want 1", server.adapter.TriggeredCount)
	}
	status, replay := server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/runs", tenantA, "", "Idempotency-Key", "run-1")
	if status != http.StatusAccepted || replay["id"] != run["id"] {
		t.Fatalf("replay trigger = %d %+v want run %v", status, replay, run["id"])
	}
	if server.adapter.TriggeredCount != 1 {
		t.Fatalf("replay invoked engine: %d", server.adapter.TriggeredCount)
	}
	status, _ = server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/runs", tenantA, "", "Idempotency-Key", "run-2")
	if status != http.StatusAccepted || server.adapter.TriggeredCount != 2 {
		t.Fatalf("new key status=%d count=%d", status, server.adapter.TriggeredCount)
	}
}

func TestIdempotentTriggerConcurrent(t *testing.T) {
	server := newTestServer(t)
	status, payload := server.do(t, http.MethodPost, "/api/v1/pipelines", tenantA, createBody("idem-concurrent"))
	if status != http.StatusCreated {
		t.Fatalf("create = %d %+v", status, payload)
	}
	id := payload["id"].(string)
	status, _ = server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/publish", tenantA, `{"published_by":"tester"}`)
	if status != http.StatusOK {
		t.Fatalf("publish = %d", status)
	}
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, body := server.do(t, http.MethodPost, "/api/v1/pipelines/"+id+"/runs", tenantA, "", "Idempotency-Key", "concurrent-1")
			if status != http.StatusAccepted {
				errs <- fmt.Sprintf("status=%d body=%v", status, body)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent trigger: %s", err)
	}
	if server.adapter.TriggeredCount != 1 {
		t.Fatalf("triggered count = %d, want 1", server.adapter.TriggeredCount)
	}
}
