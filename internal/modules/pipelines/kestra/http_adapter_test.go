package kestra

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

const (
	httpTestTenantID   = "11111111-1111-4111-8111-111111111111"
	httpTestPipelineID = "22222222-2222-4222-8222-222222222222"
)

const (
	httpTestNamespace = "oa_11111111111141118111111111111111.22222222222242228222222222222222"
	httpTestFlowID    = "22222222222242228222222222222222"
)

type httpCapturedRequest struct {
	Method      string
	EscapedPath string
	RawQuery    string
	Header      http.Header
	Body        []byte
}

type httpRequestLog struct {
	mu       sync.Mutex
	requests []httpCapturedRequest
	sleeps   []time.Duration
}

func (l *httpRequestLog) add(request httpCapturedRequest) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, request)
}

func (l *httpRequestLog) addSleep(delay time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sleeps = append(l.sleeps, delay)
}

func (l *httpRequestLog) requestsSnapshot() []httpCapturedRequest {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]httpCapturedRequest(nil), l.requests...)
}

func (l *httpRequestLog) requestCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.requests)
}

func (l *httpRequestLog) sleepSnapshot() []time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]time.Duration(nil), l.sleeps...)
}

type httpTestHandler func(w http.ResponseWriter, r *http.Request, body []byte)

// newHTTPTestAdapter starts an httptest server and returns an adapter whose
// backoff sleep is recorded instead of performed, so retry tests stay fast.
func newHTTPTestAdapter(t *testing.T, handler httpTestHandler) (*HTTPAdapter, *httpRequestLog) {
	t.Helper()
	return newHTTPTestAdapterWithConfig(t, HTTPConfig{}, handler)
}

func newHTTPTestAdapterWithConfig(t *testing.T, config HTTPConfig, handler httpTestHandler) (*HTTPAdapter, *httpRequestLog) {
	t.Helper()
	log := &httpRequestLog{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		_ = r.Body.Close()
		// Restore the body so handlers that parse forms still work.
		r.Body = io.NopCloser(bytes.NewReader(body))
		log.add(httpCapturedRequest{
			Method:      r.Method,
			EscapedPath: r.URL.EscapedPath(),
			RawQuery:    r.URL.RawQuery,
			Header:      r.Header.Clone(),
			Body:        body,
		})
		handler(w, r, body)
	}))
	t.Cleanup(server.Close)

	config.BaseURL = server.URL
	if config.APIToken == "" {
		config.APIToken = "test-token"
	}
	if config.Environment == "" {
		config.Environment = "test"
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultMaxAttempts
	}
	if config.BaseDelay <= 0 {
		config.BaseDelay = time.Millisecond
	}
	if config.Clock == nil {
		config.Clock = func() time.Time { return time.Unix(1_700_000_000, 0) }
	}
	adapter, err := NewHTTPAdapter(config)
	if err != nil {
		t.Fatalf("NewHTTPAdapter: %v", err)
	}
	adapter.sleep = func(ctx context.Context, delay time.Duration) error {
		log.addSleep(delay)
		return ctx.Err()
	}
	return adapter, log
}

func writeHTTPJSON(w http.ResponseWriter, statusCode int, payload string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = io.WriteString(w, payload)
}

func httpTestFlow() Flow {
	return Flow{
		TenantID:   httpTestTenantID,
		PipelineID: httpTestPipelineID,
		Name:       "orders_daily",
		Version:    1,
		DAG:        json.RawMessage(`{"dag_version":2,"tasks":[{"id":"build_orders","type":"dbt","depends_on":[],"model":"build_orders","sql":"select 1"}]}`),
		FlowYAML:   []byte("id: " + httpTestFlowID + "\nnamespace: " + httpTestNamespace + "\nrevision: 1\ntasks:\n  - id: t1\n    type: io.kestra.plugin.core.log.Log\n    message: ok\n"),
	}
}

func TestNewHTTPAdapterRequiresBaseURL(t *testing.T) {
	for _, baseURL := range []string{"", "   "} {
		adapter, err := NewHTTPAdapter(HTTPConfig{BaseURL: baseURL})
		if !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("NewHTTPAdapter(%q) error = %v, want ErrNotConfigured", baseURL, err)
		}
		if adapter != nil {
			t.Fatalf("NewHTTPAdapter(%q) returned a non-nil adapter", baseURL)
		}
	}
	if _, err := NewHTTPAdapter(HTTPConfig{BaseURL: "kestra"}); err == nil {
		t.Fatal("expected error for a base URL without scheme/host")
	} else if errors.Is(err, ErrNotConfigured) {
		t.Fatalf("invalid base URL should not be ErrNotConfigured: %v", err)
	}
}

func TestNewHTTPAdapterDefaults(t *testing.T) {
	adapter, err := NewHTTPAdapter(HTTPConfig{BaseURL: "http://kestra.local:8080/"})
	if err != nil {
		t.Fatalf("NewHTTPAdapter: %v", err)
	}
	if adapter.baseURL != "http://kestra.local:8080" {
		t.Fatalf("baseURL = %q", adapter.baseURL)
	}
	if adapter.maxAttempts != DefaultMaxAttempts {
		t.Fatalf("maxAttempts = %d, want %d", adapter.maxAttempts, DefaultMaxAttempts)
	}
	if adapter.baseDelay != DefaultBaseDelay {
		t.Fatalf("baseDelay = %s, want %s", adapter.baseDelay, DefaultBaseDelay)
	}
	if adapter.client == nil || adapter.client.Timeout != DefaultHTTPTimeout {
		t.Fatalf("default client = %+v", adapter.client)
	}
	if adapter.clock == nil || adapter.sleep == nil {
		t.Fatal("expected default clock and sleep hooks")
	}
}

func TestHTTPAdapterSendsBearerTokenAndTraceparent(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeHTTPJSON(w, http.StatusOK, `{}`)
	})
	if _, err := adapter.RegisterFlow(context.Background(), httpTestFlow()); err != nil {
		t.Fatalf("RegisterFlow: %v", err)
	}
	requests := log.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if got := requests[0].Header.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("Authorization = %q", got)
	}
	traceparentPattern := regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`)
	if traceparent := requests[0].Header.Get("traceparent"); !traceparentPattern.MatchString(traceparent) {
		t.Fatalf("traceparent = %q", traceparent)
	}
}

func TestHTTPAdapterSendsBasicAuthorizationForBasicToken(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		writeHTTPJSON(w, http.StatusOK, `{}`)
	}))
	t.Cleanup(server.Close)
	adapter, err := NewHTTPAdapter(HTTPConfig{BaseURL: server.URL, APIToken: "basic:user:pass"})
	if err != nil {
		t.Fatalf("NewHTTPAdapter: %v", err)
	}
	if _, err := adapter.RegisterFlow(context.Background(), httpTestFlow()); err != nil {
		t.Fatalf("RegisterFlow: %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func TestHTTPAdapterOmitsAuthorizationWithoutToken(t *testing.T) {
	captured := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		writeHTTPJSON(w, http.StatusOK, `{}`)
	}))
	t.Cleanup(server.Close)
	adapter, err := NewHTTPAdapter(HTTPConfig{
		BaseURL:     server.URL,
		BaseDelay:   time.Millisecond,
		Clock:       func() time.Time { return time.Unix(1_700_000_000, 0) },
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("NewHTTPAdapter: %v", err)
	}
	if _, err := adapter.RegisterFlow(context.Background(), httpTestFlow()); err != nil {
		t.Fatalf("RegisterFlow: %v", err)
	}
	headers := <-captured
	if got := headers.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty", got)
	}
}

func TestHTTPAdapterReusesActiveTraceparent(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeHTTPJSON(w, http.StatusOK, `{}`)
	})
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("TraceIDFromHex: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("SpanIDFromHex: %v", err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)
	if _, err := adapter.RegisterFlow(ctx, httpTestFlow()); err != nil {
		t.Fatalf("RegisterFlow: %v", err)
	}
	want := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	if got := log.requestsSnapshot()[0].Header.Get("traceparent"); got != want {
		t.Fatalf("traceparent = %q, want %q", got, want)
	}
}

func TestHTTPAdapterRegisterFlowUpserts(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeHTTPJSON(w, http.StatusOK, `{}`)
	})
	flow := httpTestFlow()
	firstRef, err := adapter.RegisterFlow(context.Background(), flow)
	if err != nil {
		t.Fatalf("RegisterFlow: %v", err)
	}
	secondRef, err := adapter.RegisterFlow(context.Background(), flow)
	if err != nil {
		t.Fatalf("RegisterFlow again: %v", err)
	}
	wantRef := FlowRef{Namespace: httpTestNamespace, FlowID: httpTestFlowID, Revision: 1}
	if firstRef != wantRef || secondRef != wantRef {
		t.Fatalf("refs = %+v / %+v, want %+v", firstRef, secondRef, wantRef)
	}

	requests := log.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	for index, request := range requests {
		if request.Method != http.MethodPost {
			t.Fatalf("request %d method = %s", index, request.Method)
		}
		if got := request.Header.Get("Content-Type"); got != "application/x-yaml" {
			t.Fatalf("request %d content type = %q", index, got)
		}
		wantPath := "/api/v1/main/flows/" + httpTestNamespace
		if request.EscapedPath != wantPath {
			t.Fatalf("request %d path = %q, want %q", index, request.EscapedPath, wantPath)
		}
		if !bytes.Equal(request.Body, flow.FlowYAML) {
			t.Fatalf("request %d body = %q, want rendered flow yaml", index, request.Body)
		}
	}
}

func TestHTTPAdapterPublishesFilesAsMultipart(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeHTTPJSON(w, http.StatusOK, `{}`)
	})
	files := []File{
		{Path: "pipeline_abc/v1/dbt_project.yml", Content: []byte("name: orders")},
		{Path: "pipeline_abc/v1/models/orders.sql", Content: []byte("select 1")},
	}
	if err := adapter.PublishFiles(context.Background(), httpTestNamespace, files); err != nil {
		t.Fatalf("PublishFiles: %v", err)
	}
	requests := log.requestsSnapshot()
	if len(requests) != len(files) {
		t.Fatalf("requests = %d, want %d", len(requests), len(files))
	}
	type uploaded struct {
		path     string
		filename string
		content  string
	}
	uploads := make([]uploaded, 0, len(requests))
	for index, request := range requests {
		if request.Method != http.MethodPost {
			t.Fatalf("request %d method = %s", index, request.Method)
		}
		wantPath := "/api/v1/main/namespaces/" + httpTestNamespace + "/files"
		if request.EscapedPath != wantPath {
			t.Fatalf("request %d path = %q, want %q", index, request.EscapedPath, wantPath)
		}
		mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("request %d content type = %q (%v)", index, request.Header.Get("Content-Type"), err)
		}
		form, err := multipart.NewReader(bytes.NewReader(request.Body), params["boundary"]).ReadForm(1 << 20)
		if err != nil {
			t.Fatalf("request %d parse multipart: %v", index, err)
		}
		paths := form.Value["path"]
		fileHeaders := form.File["fileContent"]
		if len(paths) != 0 || len(fileHeaders) != 1 {
			t.Fatalf("request %d form = path %v files %d", index, paths, len(fileHeaders))
		}
		queryPath := request.RawQuery
		parsedQuery, parseErr := url.ParseQuery(queryPath)
		if parseErr != nil {
			t.Fatalf("request %d query: %v", index, parseErr)
		}
		queryPaths := parsedQuery["path"]
		if len(queryPaths) != 1 {
			t.Fatalf("request %d query path = %v", index, queryPaths)
		}
		file, err := fileHeaders[0].Open()
		if err != nil {
			t.Fatalf("request %d open file: %v", index, err)
		}
		content, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			t.Fatalf("request %d read file: %v", index, err)
		}
		uploads = append(uploads, uploaded{path: queryPaths[0], filename: fileHeaders[0].Filename, content: string(content)})
		_ = form.RemoveAll()
	}
	wantUploads := []uploaded{
		{path: "pipeline_abc/v1/dbt_project.yml", filename: "dbt_project.yml", content: "name: orders"},
		{path: "pipeline_abc/v1/models/orders.sql", filename: "orders.sql", content: "select 1"},
	}
	if !reflect.DeepEqual(uploads, wantUploads) {
		t.Fatalf("uploads = %+v, want %+v", uploads, wantUploads)
	}

	before := log.requestCount()
	if err := adapter.PublishFiles(context.Background(), httpTestNamespace, []File{{Path: "../escape.sql", Content: []byte("x")}}); err == nil {
		t.Fatal("expected invalid path error")
	}
	if after := log.requestCount(); after != before {
		t.Fatalf("invalid path issued %d requests", after-before)
	}
}

func TestHTTPAdapterDeleteFiles(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		if strings.Contains(r.URL.RawQuery, "missing.sql") {
			writeHTTPJSON(w, http.StatusNotFound, `{"message":"missing"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	paths := []string{"pipeline_abc/v1/models/orders.sql", "missing.sql"}
	if err := adapter.DeleteFiles(context.Background(), httpTestNamespace, paths); err != nil {
		t.Fatalf("DeleteFiles: %v", err)
	}
	requests := log.requestsSnapshot()
	if len(requests) != len(paths) {
		t.Fatalf("requests = %d, want %d", len(requests), len(paths))
	}
	for index, request := range requests {
		wantPath := "/api/v1/main/namespaces/" + httpTestNamespace + "/files"
		if request.EscapedPath != wantPath {
			t.Fatalf("request %d path = %q, want %q", index, request.EscapedPath, wantPath)
		}
		query, err := url.ParseQuery(request.RawQuery)
		if err != nil || len(query["path"]) != 1 || query["path"][0] != paths[index] {
			t.Fatalf("request %d query = %q", index, request.RawQuery)
		}
	}
}

func TestHTTPAdapterDeleteFlow(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		if strings.HasSuffix(r.URL.EscapedPath(), "/missing") {
			writeHTTPJSON(w, http.StatusNotFound, `{"message":"missing"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := adapter.DeleteFlow(context.Background(), FlowRef{Namespace: httpTestNamespace, FlowID: httpTestFlowID, Revision: 2}); err != nil {
		t.Fatalf("DeleteFlow: %v", err)
	}
	if err := adapter.DeleteFlow(context.Background(), FlowRef{Namespace: httpTestNamespace, FlowID: "missing", Revision: 2}); err != nil {
		t.Fatalf("DeleteFlow missing: %v", err)
	}
	requests := log.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	want := "/api/v1/main/flows/" + httpTestNamespace + "/" + httpTestFlowID
	if requests[0].EscapedPath != want {
		t.Fatalf("path = %q, want %q", requests[0].EscapedPath, want)
	}
}

func TestHTTPAdapterTriggerRun(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		if len(body) != 0 {
			t.Errorf("trigger body = %q, want empty", body)
		}
		labels := map[string]string{}
		for _, item := range r.URL.Query()["labels"] {
			key, value, ok := strings.Cut(item, ":")
			if ok {
				labels[key] = value
			}
		}
		if got := labels["ontoaxis.run_id"]; got != "run-9" {
			t.Errorf("run label = %q", got)
		}
		writeHTTPJSON(w, http.StatusOK, `{"id":"exec-1","state":{"current":"RUNNING"}}`)
	})
	result, err := adapter.TriggerRun(context.Background(), RunRequest{
		TenantID:   httpTestTenantID,
		PipelineID: httpTestPipelineID,
		Version:    3,
		RunID:      "run-9",
		DAG:        json.RawMessage(`{"dag_version":2,"tasks":[]}`),
	})
	if err != nil {
		t.Fatalf("TriggerRun: %v", err)
	}
	if result.ExecutionID != "exec-1" || result.Status != "RUNNING" {
		t.Fatalf("RunResult = %+v", result)
	}
	requests := log.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if requests[0].Method != http.MethodPost {
		t.Fatalf("method = %s", requests[0].Method)
	}
	wantPath := "/api/v1/main/executions/" + httpTestNamespace + "/" + httpTestFlowID
	if requests[0].EscapedPath != wantPath {
		t.Fatalf("path = %q, want %q", requests[0].EscapedPath, wantPath)
	}
	query, err := url.ParseQuery(requests[0].RawQuery)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	labels := map[string]string{}
	for _, item := range query["labels"] {
		key, value, ok := strings.Cut(item, ":")
		if ok {
			labels[key] = value
		}
	}
	wantLabels := map[string]string{
		"ontoaxis.tenant_id":        httpTestTenantID,
		"ontoaxis.pipeline_id":      httpTestPipelineID,
		"ontoaxis.pipeline_version": "3",
		"ontoaxis.dag_version":      "2",
		"ontoaxis.managed_by":       "ontoaxis-core",
		"ontoaxis.environment":      "test",
		"ontoaxis.run_id":           "run-9",
	}
	if !reflect.DeepEqual(labels, wantLabels) {
		t.Fatalf("labels = %+v, want %+v", labels, wantLabels)
	}
}

func TestHTTPAdapterGetExecutionMapping(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch r.URL.Path {
		case "/api/v1/main/executions/exec-1":
			writeHTTPJSON(w, http.StatusOK, `{"id":"exec-1","namespace":"`+httpTestNamespace+`","flowId":"`+httpTestFlowID+`","state":{"current":"SUCCESS"},"labels":[{"key":"ontoaxis.run_id","value":"run-9"}],"outputs":{"rows":3,"summary":"ok","empty":null}}`)
		case "/api/v1/main/executions/exec-2":
			writeHTTPJSON(w, http.StatusOK, `{"id":"exec-2","status":"FAILED"}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			writeHTTPJSON(w, http.StatusNotFound, `{}`)
		}
	})
	execution, err := adapter.GetExecution(context.Background(), ExecutionRef{ID: "exec-1"})
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if execution.ID != "exec-1" || execution.Status != "SUCCESS" {
		t.Fatalf("execution = %+v", execution)
	}
	if execution.Namespace != httpTestNamespace || execution.FlowID != httpTestFlowID {
		t.Fatalf("execution identity = %+v", execution)
	}
	if execution.Labels["ontoaxis.run_id"] != "run-9" {
		t.Fatalf("labels = %+v", execution.Labels)
	}
	if execution.Outputs["rows"] != "3" || execution.Outputs["summary"] != "ok" {
		t.Fatalf("outputs = %+v", execution.Outputs)
	}
	if _, ok := execution.Outputs["empty"]; ok {
		t.Fatalf("null output should be dropped: %+v", execution.Outputs)
	}

	flat, err := adapter.GetExecution(context.Background(), ExecutionRef{ID: "exec-2"})
	if err != nil {
		t.Fatalf("GetExecution flat: %v", err)
	}
	if flat.Status != "FAILED" {
		t.Fatalf("flat status = %q", flat.Status)
	}
	if log.requestCount() != 2 {
		t.Fatalf("requests = %d, want 2", log.requestCount())
	}
}

func TestHTTPAdapterGetExecutionNotFound(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeHTTPJSON(w, http.StatusNotFound, `{"message":"not found"}`)
	})
	if _, err := adapter.GetExecution(context.Background(), ExecutionRef{ID: "exec-missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetExecution error = %v, want ErrNotFound", err)
	}
	if log.requestCount() != 1 {
		t.Fatalf("requests = %d, want no retry for 404", log.requestCount())
	}
}

func TestHTTPAdapterGetExecutionRetriesTransientFailure(t *testing.T) {
	var attempts atomic.Int32
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		if attempts.Add(1) == 1 {
			writeHTTPJSON(w, http.StatusServiceUnavailable, `{"message":"busy"}`)
			return
		}
		writeHTTPJSON(w, http.StatusOK, `{"id":"exec-1","status":"RUNNING"}`)
	})
	execution, err := adapter.GetExecution(context.Background(), ExecutionRef{ID: "exec-1"})
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if execution.ID != "exec-1" || execution.Status != "RUNNING" {
		t.Fatalf("execution = %+v", execution)
	}
	if log.requestCount() != 2 || len(log.sleepSnapshot()) != 1 {
		t.Fatalf("requests = %d, sleeps = %d", log.requestCount(), len(log.sleepSnapshot()))
	}
}

func TestHTTPAdapterFindExecutionByRun(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if got := r.URL.Query().Get("filters[labels][EQUALS][ontoaxis.run_id]"); got != "run-9" {
			t.Errorf("labels query = %q", got)
		}
		if got := r.URL.Query().Get("size"); got != "100" {
			t.Errorf("size query = %q", got)
		}
		writeHTTPJSON(w, http.StatusOK, `{"results":[{"id":"exec-9","status":"SUCCESS","namespace":"`+httpTestNamespace+`","flowId":"`+httpTestFlowID+`","labels":{"ontoaxis.tenant_id":"`+httpTestTenantID+`","ontoaxis.run_id":"run-9"}}]}`)
	})
	execution, err := adapter.FindExecutionByRun(context.Background(), httpTestTenantID, "run-9")
	if err != nil {
		t.Fatalf("FindExecutionByRun: %v", err)
	}
	if execution.ID != "exec-9" || execution.Status != "SUCCESS" {
		t.Fatalf("execution = %+v", execution)
	}
	if log.requestCount() != 1 {
		t.Fatalf("requests = %d, want 1", log.requestCount())
	}
	if request := log.requestsSnapshot()[0]; request.Method != http.MethodGet || request.EscapedPath != "/api/v1/main/executions/search" {
		t.Fatalf("request = %+v", request)
	}
}

func TestHTTPAdapterFindExecutionByRunMissing(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeHTTPJSON(w, http.StatusOK, `{"results":[]}`)
	})
	if _, err := adapter.FindExecutionByRun(context.Background(), httpTestTenantID, "run-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindExecutionByRun error = %v, want ErrNotFound", err)
	}
	if log.requestCount() != 1 {
		t.Fatalf("requests = %d, want 1", log.requestCount())
	}
}

func TestHTTPAdapterCancelExecution(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.WriteHeader(http.StatusOK)
	})
	if err := adapter.CancelExecution(context.Background(), ExecutionRef{ID: "exec-1"}); err != nil {
		t.Fatalf("CancelExecution: %v", err)
	}
	requests := log.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	wantPath := "/api/v1/main/executions/exec-1/actions/kill"
	if requests[0].Method != http.MethodDelete || requests[0].EscapedPath != wantPath {
		t.Fatalf("request = %+v, want POST %s", requests[0], wantPath)
	}
}

func TestHTTPAdapterCancelExecutionToleratesFinishedExecution(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeHTTPJSON(w, http.StatusNotFound, `{"message":"execution not found"}`)
	})
	if err := adapter.CancelExecution(context.Background(), ExecutionRef{ID: "exec-done"}); err != nil {
		t.Fatalf("CancelExecution finished = %v, want nil", err)
	}
	if log.requestCount() != 1 {
		t.Fatalf("requests = %d, want 1", log.requestCount())
	}
}

func TestHTTPAdapterClassifiesStatuses(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		code       string
		wantRetry  bool
		notFound   bool
	}{
		{name: "bad_request", statusCode: http.StatusBadRequest, code: "invalid_argument"},
		{name: "unauthorized", statusCode: http.StatusUnauthorized, code: "invalid_argument"},
		{name: "forbidden", statusCode: http.StatusForbidden, code: "invalid_argument"},
		{name: "not_found", statusCode: http.StatusNotFound, code: "not_found", notFound: true},
		{name: "conflict", statusCode: http.StatusConflict, code: "conflict"},
		{name: "unprocessable", statusCode: http.StatusUnprocessableEntity, code: "invalid_argument"},
		{name: "request_timeout", statusCode: http.StatusRequestTimeout, code: "timeout", wantRetry: true},
		{name: "rate_limited", statusCode: http.StatusTooManyRequests, code: "rate_limited", wantRetry: true},
		{name: "internal_error", statusCode: http.StatusInternalServerError, code: "unavailable", wantRetry: true},
		{name: "bad_gateway", statusCode: http.StatusBadGateway, code: "unavailable", wantRetry: true},
		{name: "unavailable", statusCode: http.StatusServiceUnavailable, code: "unavailable", wantRetry: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			adapter, log := newHTTPTestAdapterWithConfig(t, HTTPConfig{MaxAttempts: 3}, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeHTTPJSON(w, testCase.statusCode, `{"message":"engine rejected the request"}`)
			})
			_, err := adapter.RegisterFlow(context.Background(), httpTestFlow())
			if err == nil {
				t.Fatal("expected error")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %T %v, want *APIError", err, err)
			}
			if apiErr.StatusCode != testCase.statusCode || apiErr.Code != testCase.code {
				t.Fatalf("APIError = %+v, want status %d code %q", apiErr, testCase.statusCode, testCase.code)
			}
			if apiErr.Retryable != testCase.wantRetry {
				t.Fatalf("Retryable = %v, want %v", apiErr.Retryable, testCase.wantRetry)
			}
			if IsRetryable(err) != testCase.wantRetry {
				t.Fatalf("IsRetryable = %v, want %v", IsRetryable(err), testCase.wantRetry)
			}
			if testCase.notFound && !errors.Is(err, ErrNotFound) {
				t.Fatalf("error does not unwrap to ErrNotFound: %v", err)
			}
			wantRequests := 1
			if testCase.wantRetry {
				wantRequests = 3
			}
			if log.requestCount() != wantRequests {
				t.Fatalf("requests = %d, want %d", log.requestCount(), wantRequests)
			}
		})
	}
}

func TestHTTPAdapterRetryBackoffBounds(t *testing.T) {
	adapter, log := newHTTPTestAdapterWithConfig(t, HTTPConfig{MaxAttempts: 3, BaseDelay: 100 * time.Millisecond}, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeHTTPJSON(w, http.StatusServiceUnavailable, `{"message":"busy"}`)
	})
	if _, err := adapter.RegisterFlow(context.Background(), httpTestFlow()); err == nil {
		t.Fatal("expected error")
	}
	sleeps := log.sleepSnapshot()
	if len(sleeps) != 2 {
		t.Fatalf("sleeps = %v, want 2", sleeps)
	}
	if sleeps[0] < 80*time.Millisecond || sleeps[0] > 120*time.Millisecond {
		t.Fatalf("first backoff = %s, want 100ms +/-20%%", sleeps[0])
	}
	if sleeps[1] < 160*time.Millisecond || sleeps[1] > 240*time.Millisecond {
		t.Fatalf("second backoff = %s, want 200ms +/-20%%", sleeps[1])
	}
	if log.requestCount() != 3 {
		t.Fatalf("requests = %d, want 3", log.requestCount())
	}
}

type httpRoundTripFunc func(*http.Request) (*http.Response, error)

func (f httpRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type httpTimeoutError struct{}

func (httpTimeoutError) Error() string   { return "i/o timeout" }
func (httpTimeoutError) Timeout() bool   { return true }
func (httpTimeoutError) Temporary() bool { return true }

func TestHTTPAdapterTransportErrorsAreRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{
			name: "timeout",
			err:  &url.Error{Op: "Post", URL: "http://kestra.invalid", Err: httpTimeoutError{}},
			code: "timeout",
		},
		{
			name: "connection",
			err:  &url.Error{Op: "Post", URL: "http://kestra.invalid", Err: errors.New("connection refused")},
			code: "unavailable",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var attempts atomic.Int32
			client := &http.Client{Transport: httpRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
				attempts.Add(1)
				return nil, testCase.err
			})}
			adapter, err := NewHTTPAdapter(HTTPConfig{
				BaseURL:     "http://kestra.invalid",
				APIToken:    "test-token",
				HTTPClient:  client,
				MaxAttempts: 3,
				BaseDelay:   time.Millisecond,
				Clock:       func() time.Time { return time.Unix(1_700_000_000, 0) },
			})
			if err != nil {
				t.Fatalf("NewHTTPAdapter: %v", err)
			}
			adapter.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
			_, err = adapter.RegisterFlow(context.Background(), httpTestFlow())
			if err == nil {
				t.Fatal("expected error")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %T %v, want *APIError", err, err)
			}
			if apiErr.Code != testCase.code || !apiErr.Retryable || !IsRetryable(err) {
				t.Fatalf("APIError = %+v", apiErr)
			}
			if got := attempts.Load(); got != 3 {
				t.Fatalf("attempts = %d, want 3", got)
			}
		})
	}
}

func TestHTTPAdapterRedactsCredentials(t *testing.T) {
	adapter, log := newHTTPTestAdapterWithConfig(t, HTTPConfig{APIToken: "super-secret-token", MaxAttempts: 1}, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		message := "engine rejected " + r.Header.Get("Authorization") +
			` dsn=postgres://user:dsn-secret@db:5432/ontoaxis password=hunter2 token=topsecret`
		writeHTTPJSON(w, http.StatusInternalServerError, `{"message":`+jsonString(t, message)+`}`)
	})
	_, err := adapter.RegisterFlow(context.Background(), httpTestFlow())
	if err == nil {
		t.Fatal("expected error")
	}
	message := err.Error()
	for _, secret := range []string{"super-secret-token", "dsn-secret", "hunter2", "topsecret"} {
		if strings.Contains(message, secret) {
			t.Fatalf("error leaks %q: %s", secret, message)
		}
	}
	if !strings.Contains(message, "[REDACTED]") {
		t.Fatalf("error was not redacted: %s", message)
	}
	if got := log.requestsSnapshot()[0].Header.Get("Authorization"); got != "Bearer super-secret-token" {
		t.Fatalf("Authorization = %q", got)
	}
}

// jsonString renders value as a JSON string literal for building error bodies.
func jsonString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %q: %v", value, err)
	}
	return string(encoded)
}

func TestHTTPAdapterAdoptsExecutionAfterLostTriggerResponse(t *testing.T) {
	var posts atomic.Int32
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			writeHTTPJSON(w, http.StatusBadGateway, `{"message":"gateway down"}`)
			return
		}
		writeHTTPJSON(w, http.StatusOK, `{"results":[{"id":"exec-adopted","status":"RUNNING","labels":{"ontoaxis.tenant_id":"`+httpTestTenantID+`","ontoaxis.run_id":"run-lost"}}]}`)
	})
	result, err := adapter.TriggerRun(context.Background(), RunRequest{
		TenantID:   httpTestTenantID,
		PipelineID: httpTestPipelineID,
		Version:    1,
		RunID:      "run-lost",
	})
	if err != nil {
		t.Fatalf("TriggerRun: %v", err)
	}
	if result.ExecutionID != "exec-adopted" || result.Status != "RUNNING" {
		t.Fatalf("RunResult = %+v", result)
	}
	if posts.Load() != 1 {
		t.Fatalf("trigger POSTs = %d, want 1 (no duplicate execution)", posts.Load())
	}
	if log.requestCount() != 2 {
		t.Fatalf("requests = %d, want POST + adoption GET", log.requestCount())
	}
}

func TestHTTPAdapterTriggerRunAdoptsWhenResponseHasNoID(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method == http.MethodPost {
			writeHTTPJSON(w, http.StatusOK, `{"status":"RUNNING"}`)
			return
		}
		writeHTTPJSON(w, http.StatusOK, `{"results":[{"id":"exec-adopted","status":"RUNNING","labels":{"ontoaxis.tenant_id":"`+httpTestTenantID+`","ontoaxis.run_id":"run-9"}}]}`)
	})
	result, err := adapter.TriggerRun(context.Background(), RunRequest{
		TenantID:   httpTestTenantID,
		PipelineID: httpTestPipelineID,
		Version:    1,
		RunID:      "run-9",
	})
	if err != nil {
		t.Fatalf("TriggerRun: %v", err)
	}
	if result.ExecutionID != "exec-adopted" {
		t.Fatalf("RunResult = %+v", result)
	}
	if log.requestCount() != 2 {
		t.Fatalf("requests = %d, want POST + adoption GET", log.requestCount())
	}
}

func TestHTTPAdapterCancellationStopsRetries(t *testing.T) {
	adapter, log := newHTTPTestAdapterWithConfig(t, HTTPConfig{MaxAttempts: 5}, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeHTTPJSON(w, http.StatusServiceUnavailable, `{"message":"busy"}`)
	})
	adapter.sleep = func(context.Context, time.Duration) error { return context.Canceled }
	if _, err := adapter.RegisterFlow(context.Background(), httpTestFlow()); !errors.Is(err, context.Canceled) {
		t.Fatalf("RegisterFlow error = %v, want context.Canceled", err)
	}
	if log.requestCount() != 1 {
		t.Fatalf("requests = %d, want 1 before cancellation", log.requestCount())
	}
}

func TestHTTPAdapterRecordsOutboundMetrics(t *testing.T) {
	metrics := observability.NewMetrics(nil)
	adapter, _ := newHTTPTestAdapterWithConfig(t, HTTPConfig{Metrics: metrics}, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeHTTPJSON(w, http.StatusOK, `{"id":"exec-m","status":"SUCCESS"}`)
	})
	if _, err := adapter.GetExecution(context.Background(), ExecutionRef{ID: "exec-m"}); err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "ontoaxis_outbound_request_duration_seconds" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, l := range metric.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["dependency"] == "kestra" && labels["operation"] == "executions.get" && labels["result"] == "success" && metric.GetHistogram().GetSampleCount() == 1 {
				return
			}
		}
	}
	t.Fatal("expected kestra executions.get success metric")
}

func TestHTTPAdapterGetRunResults(t *testing.T) {
	adapter, log := newHTTPTestAdapter(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch r.URL.EscapedPath() {
		case "/api/v1/main/outputs/tasks/exec-1":
			writeHTTPJSON(w, http.StatusOK, `[{"taskId":"build_orders","taskRunId":"task-run-1"}]`)
		case "/api/v1/main/outputs/tasks/exec-1/task-run-1":
			writeHTTPJSON(w, http.StatusOK, `{"outputFiles":{"target/run_results.json":"kestra:///test.json","target/run_results.run.json":"kestra:///run.json"}}`)
		case "/api/v1/main/executions/exec-1/file":
			if got := r.URL.Query().Get("path"); got != "kestra:///run.json" {
				t.Errorf("download path = %q, want run_results.run.json URI", got)
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				t.Errorf("download Accept = %q, want application/octet-stream", got)
			}
			writeHTTPJSON(w, http.StatusOK, `{"results":[{"unique_id":"model.x","status":"success"}]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
		}
	})

	raw, err := adapter.GetRunResults(context.Background(), "exec-1")
	if err != nil {
		t.Fatalf("GetRunResults: %v", err)
	}
	if !strings.Contains(string(raw), `"status":"success"`) {
		t.Fatalf("artifact = %s", raw)
	}
	if got := len(log.requestsSnapshot()); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}
