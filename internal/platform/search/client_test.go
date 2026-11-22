package search

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(server.URL, "test-key", WithTimeout(2*time.Second), WithWaitTimeout(time.Second))
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}
	return client
}

func TestNewClientValidatesURL(t *testing.T) {
	for _, baseURL := range []string{"", "localhost:7700", "ftp://example.com"} {
		if _, err := NewClient(baseURL, "key"); err == nil {
			t.Fatalf("NewClient(%q) = nil error, want error", baseURL)
		} else if !errors.Is(err, ErrInvalid) {
			t.Fatalf("NewClient(%q) error = %v, want ErrInvalid", baseURL, err)
		}
	}
}

func TestClientEnsureIndexCreateThenIdempotent(t *testing.T) {
	var (
		getCount      int
		created       map[string]any
		settingsCalls int
		lastSettings  map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/indexes/objects_v1_deadbeef":
			getCount++
			if getCount == 1 {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"message":"Index not found","code":"index_not_found","type":"invalid_request"}`)
				return
			}
			_, _ = io.WriteString(w, `{"uid":"objects_v1_deadbeef","primaryKey":"id"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/indexes":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			_, _ = io.WriteString(w, `{"taskUid":1,"indexUid":"objects_v1_deadbeef"}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/indexes/objects_v1_deadbeef/settings":
			settingsCalls++
			if err := json.NewDecoder(r.Body).Decode(&lastSettings); err != nil {
				t.Errorf("decode settings body: %v", err)
			}
			_, _ = io.WriteString(w, `{"taskUid":2,"indexUid":"objects_v1_deadbeef"}`)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tasks/"):
			_, _ = io.WriteString(w, `{"uid":2,"indexUid":"objects_v1_deadbeef","status":"succeeded","type":"settingsUpdate"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server)

	if err := client.EnsureIndex(context.Background(), "objects_v1_deadbeef", SettingsJSON()); err != nil {
		t.Fatalf("EnsureIndex create error = %v", err)
	}
	if err := client.EnsureIndex(context.Background(), "objects_v1_deadbeef", SettingsJSON()); err != nil {
		t.Fatalf("EnsureIndex existing error = %v", err)
	}
	if getCount != 2 {
		t.Fatalf("GET index count = %d, want 2", getCount)
	}
	if created["uid"] != "objects_v1_deadbeef" || created["primaryKey"] != PrimaryKey {
		t.Fatalf("create body = %#v", created)
	}
	if settingsCalls != 2 {
		t.Fatalf("settings calls = %d, want 2", settingsCalls)
	}
}

func TestClientDeleteIndexIgnoresNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/indexes/gone" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"not found","code":"index_not_found"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	if err := client.DeleteIndex(context.Background(), "gone"); err != nil {
		t.Fatalf("DeleteIndex(404) error = %v, want nil", err)
	}
}

func TestClientUpsertDocumentsHeadersAndTask(t *testing.T) {
	oldPropagator := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer otel.SetTextMapPropagator(oldPropagator)

	var (
		gotAuth        string
		gotRequestID   string
		gotTraceparent string
		gotPrimaryKey  string
		gotDocs        []Document
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/indexes/idx/documents" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		gotAuth = r.Header.Get("Authorization")
		gotRequestID = r.Header.Get("X-Request-ID")
		gotTraceparent = r.Header.Get("Traceparent")
		gotPrimaryKey = r.URL.Query().Get("primaryKey")
		if err := json.NewDecoder(r.Body).Decode(&gotDocs); err != nil {
			t.Errorf("decode documents: %v", err)
		}
		_, _ = io.WriteString(w, `{"taskUid":42,"indexUid":"idx"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server)

	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(observability.WithRequestID(context.Background(), "req-123"), spanContext)
	taskUID, err := client.UpsertDocuments(ctx, "idx", []Document{{ID: "doc-1", TenantID: "tenant-1"}})
	if err != nil {
		t.Fatalf("UpsertDocuments error = %v", err)
	}
	if taskUID != "42" {
		t.Fatalf("task UID = %q, want 42", taskUID)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotRequestID != "req-123" {
		t.Fatalf("X-Request-ID = %q", gotRequestID)
	}
	if !strings.HasPrefix(gotTraceparent, "00-") || !strings.Contains(gotTraceparent, traceID.String()) {
		t.Fatalf("traceparent = %q, want W3C trace header", gotTraceparent)
	}
	if gotPrimaryKey != PrimaryKey {
		t.Fatalf("primaryKey = %q", gotPrimaryKey)
	}
	if len(gotDocs) != 1 || gotDocs[0].ID != "doc-1" {
		t.Fatalf("documents = %#v", gotDocs)
	}
}

func TestClientDeleteDocuments(t *testing.T) {
	var gotIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/indexes/idx/documents/delete-batch" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotIDs); err != nil {
			t.Errorf("decode ids: %v", err)
		}
		_, _ = io.WriteString(w, `{"taskUid":7}`)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	taskUID, err := client.DeleteDocuments(context.Background(), "idx", []string{"a", "b"})
	if err != nil {
		t.Fatalf("DeleteDocuments error = %v", err)
	}
	if taskUID != "7" || len(gotIDs) != 2 || gotIDs[0] != "a" || gotIDs[1] != "b" {
		t.Fatalf("taskUID = %q, ids = %#v", taskUID, gotIDs)
	}
}

func TestClientSearchAndTruncation(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/indexes/idx/search" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode search body: %v", err)
		}
		_, _ = io.WriteString(w, `{
			"hits":[{"id":"a","tenant_id":"t","object_type_id":"o","display_name":"A","properties_text":"x=y","version":1,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}],
			"estimatedTotalHits":15000,
			"limit":10,
			"offset":5
		}`)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	result, err := client.Search(context.Background(), "idx", SearchRequest{
		Q:      "alpha",
		Filter: `tenant_id = "tenant-1"`,
		Sort:   []string{"updated_at:desc"},
		Limit:  10,
		Offset: 5,
	})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(result.Hits) != 1 || result.Hits[0].ID != "a" {
		t.Fatalf("hits = %#v", result.Hits)
	}
	if result.EstimatedTotalHits != 15000 || !result.Truncated {
		t.Fatalf("result = %#v, want truncated at 15000", result)
	}
	if body["q"] != "alpha" || body["filter"] != `tenant_id = "tenant-1"` || body["limit"] != float64(10) || body["offset"] != float64(5) {
		t.Fatalf("search body = %#v", body)
	}
}

func TestClientSearchDefaultsLimit(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"hits":[],"estimatedTotalHits":0,"limit":20,"offset":0}`)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	if _, err := client.Search(context.Background(), "idx", SearchRequest{}); err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if body["limit"] != float64(20) {
		t.Fatalf("default limit body = %#v", body)
	}
	if _, ok := body["filter"]; ok {
		t.Fatalf("empty filter should be omitted: %#v", body)
	}
}

func TestClientWaitTask(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/task-1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, `{"uid":1,"status":"enqueued"}`)
			return
		}
		_, _ = io.WriteString(w, `{"uid":1,"status":"succeeded"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	if err := client.WaitTask(context.Background(), "idx", "task-1", time.Second); err != nil {
		t.Fatalf("WaitTask error = %v", err)
	}
	if calls < 2 {
		t.Fatalf("task polled %d times, want at least 2", calls)
	}
}

func TestClientWaitTaskFailureKinds(t *testing.T) {
	tests := []struct {
		name string
		code string
		want error
	}{
		{name: "invalid document", code: "invalid_document", want: ErrInvalid},
		{name: "internal", code: "internal", want: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"status":"failed","error":{"code":"`+tt.code+`","message":"hidden details"}}`)
			}))
			defer server.Close()
			client := newTestClient(t, server)
			err := client.WaitTask(context.Background(), "", "task-x", time.Second)
			if err == nil {
				t.Fatal("WaitTask returned nil for failed task")
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "hidden details") {
				t.Fatalf("error leaks backend message: %v", err)
			}
		})
	}
}

func TestClientWaitTaskTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"enqueued"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	err := client.WaitTask(context.Background(), "", "task-timeout", 120*time.Millisecond)
	if err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("WaitTask timeout error = %v, want ErrUnavailable", err)
	}
}

func TestClientSwapIndexes(t *testing.T) {
	var pairs []map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/swap-indexes":
			if err := json.NewDecoder(r.Body).Decode(&pairs); err != nil {
				t.Errorf("decode swap body: %v", err)
			}
			_, _ = io.WriteString(w, `{"taskUid":9}`)
		case "/tasks/9":
			_, _ = io.WriteString(w, `{"status":"succeeded"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server)
	if err := client.SwapIndexes(context.Background(), [][2]string{{"staging", "active"}}); err != nil {
		t.Fatalf("SwapIndexes error = %v", err)
	}
	if len(pairs) != 1 || len(pairs[0]["indexes"]) != 2 || pairs[0]["indexes"][0] != "staging" || pairs[0]["indexes"][1] != "active" {
		t.Fatalf("swap body = %#v", pairs)
	}
}

func TestClientHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"available"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health error = %v", err)
	}

	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"maintenance"}`)
	}))
	defer unhealthy.Close()
	if err := newTestClient(t, unhealthy).Health(context.Background()); err == nil {
		t.Fatal("Health accepted maintenance status")
	}
}

func TestClientErrorMappingDoesNotLeakBody(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "bad request", status: http.StatusBadRequest, want: ErrInvalid},
		{name: "unprocessable", status: http.StatusUnprocessableEntity, want: ErrInvalid},
		{name: "server error", status: http.StatusInternalServerError, want: ErrUnavailable},
		{name: "not found", status: http.StatusNotFound, want: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"message":"super-secret-body","code":"bad_thing"}`)
			}))
			defer server.Close()
			client := newTestClient(t, server)
			err := client.Health(context.Background())
			if err == nil {
				t.Fatal("Health returned nil for error status")
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want kind %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "super-secret-body") {
				t.Fatalf("error leaks response body: %v", err)
			}
		})
	}
}

func TestClientTransportFailureIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	client := newTestClient(t, server)
	server.Close()
	if err := client.Health(context.Background()); err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Health after server close error = %v, want ErrUnavailable", err)
	}
}
