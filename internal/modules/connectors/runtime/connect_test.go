package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
)

const (
	testSourceName = "oa-fa1ffa66-2ff3a64e-source"
	testSinkName   = "oa-fa1ffa66-2ff3a64e-sink"
)

func fastRetryPolicy(maxRetries int) runtime.RetryPolicy {
	return runtime.RetryPolicy{
		MaxRetries: maxRetries,
		BaseDelay:  time.Millisecond,
		MaxDelay:   5 * time.Millisecond,
	}
}

type recordedRequest struct {
	method      string
	path        string
	contentType string
	body        []byte
}

func recordingServer(t *testing.T, status int, response string, recorded *[]recordedRequest, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		mu.Lock()
		*recorded = append(*recorded, recordedRequest{
			method:      r.Method,
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			body:        body,
		})
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
}

func TestHTTPClientUpsertConnector(t *testing.T) {
	t.Parallel()

	var (
		recorded []recordedRequest
		mu       sync.Mutex
	)
	server := recordingServer(t, http.StatusOK, `{}`, &recorded, &mu)
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
	config := map[string]any{
		"connector.class":   "io.debezium.connector.postgresql.PostgresConnector",
		"database.password": "cdc_pass",
	}
	if err := client.UpsertConnector(context.Background(), testSourceName, config); err != nil {
		t.Fatalf("UpsertConnector() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(recorded) != 1 {
		t.Fatalf("requests = %d, want 1", len(recorded))
	}
	got := recorded[0]
	if got.method != http.MethodPut {
		t.Errorf("method = %q, want PUT", got.method)
	}
	if want := "/connectors/" + testSourceName + "/config"; got.path != want {
		t.Errorf("path = %q, want %q", got.path, want)
	}
	if !strings.Contains(got.contentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got.contentType)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got.body, &decoded); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if decoded["connector.class"] != config["connector.class"] || decoded["database.password"] != config["database.password"] {
		t.Errorf("request body = %v, want %v", decoded, config)
	}
}

func TestHTTPClientUpsertIsIdempotent(t *testing.T) {
	t.Parallel()

	var (
		recorded []recordedRequest
		mu       sync.Mutex
	)
	server := recordingServer(t, http.StatusOK, `{}`, &recorded, &mu)
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
	config := map[string]any{"connector.class": "x", "topics": "t"}
	for i := 0; i < 2; i++ {
		if err := client.UpsertConnector(context.Background(), testSinkName, config); err != nil {
			t.Fatalf("UpsertConnector() attempt %d error = %v", i+1, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(recorded) != 2 {
		t.Fatalf("requests = %d, want 2", len(recorded))
	}
	if string(recorded[0].body) != string(recorded[1].body) {
		t.Fatalf("repeated submit produced different payloads: %s != %s", recorded[0].body, recorded[1].body)
	}
}

func TestHTTPClientUpsertRetriesServerErrors(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		attempts int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		current := attempts
		mu.Unlock()
		if current < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(3)))
	if err := client.UpsertConnector(context.Background(), testSourceName, map[string]any{"connector.class": "x"}); err != nil {
		t.Fatalf("UpsertConnector() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestHTTPClientUpsertExhaustsRetries(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		attempts int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(2)))
	err := client.UpsertConnector(context.Background(), testSourceName, map[string]any{"connector.class": "x"})
	if !errors.Is(err, runtime.ErrConnectUnavailable) {
		t.Fatalf("UpsertConnector() error = %v, want %v", err, runtime.ErrConnectUnavailable)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (1 + 2 retries)", attempts)
	}
}

func TestHTTPClientUpsertPermanentClientError(t *testing.T) {
	t.Parallel()

	const leaked = "cdc_super_secret_password"
	var (
		mu       sync.Mutex
		attempts int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error_code":400,"message":"bad config password=`+leaked+`"}`)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(3)))
	err := client.UpsertConnector(context.Background(), testSourceName, map[string]any{"database.password": leaked})
	if !errors.Is(err, runtime.ErrConnectRejected) {
		t.Fatalf("UpsertConnector() error = %v, want %v", err, runtime.ErrConnectRejected)
	}
	if strings.Contains(err.Error(), leaked) {
		t.Fatalf("UpsertConnector() error leaks response body: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (4xx must not retry)", attempts)
	}
}

func TestHTTPClientUpsertConflict(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error_code":409}`)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
	err := client.UpsertConnector(context.Background(), testSourceName, map[string]any{"connector.class": "x"})
	if !errors.Is(err, runtime.ErrConnectConflict) {
		t.Fatalf("UpsertConnector() error = %v, want %v", err, runtime.ErrConnectConflict)
	}
}

func TestHTTPClientTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL,
		runtime.WithHTTPTimeout(10*time.Millisecond),
		runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)),
	)
	err := client.UpsertConnector(context.Background(), testSourceName, map[string]any{"connector.class": "x"})
	if !errors.Is(err, runtime.ErrConnectUnavailable) {
		t.Fatalf("UpsertConnector() error = %v, want %v", err, runtime.ErrConnectUnavailable)
	}
}

func TestHTTPClientInvalidConnectorName(t *testing.T) {
	t.Parallel()

	client := runtime.NewHTTPClient("http://127.0.0.1:1")
	for _, name := range []string{"", "bad name", "with/slash", strings.Repeat("a", 300)} {
		if err := client.UpsertConnector(context.Background(), name, map[string]any{}); !errors.Is(err, runtime.ErrInvalidConnectorName) {
			t.Errorf("UpsertConnector(%q) error = %v, want %v", name, err, runtime.ErrInvalidConnectorName)
		}
	}
}

func TestHTTPClientConnectorStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		want    runtime.State
	}{
		{
			name:    "running",
			payload: `{"name":"x","connector":{"state":"RUNNING"},"tasks":[{"id":0,"state":"RUNNING"}]}`,
			want:    runtime.StateRunning,
		},
		{
			name:    "failed",
			payload: `{"name":"x","connector":{"state":"FAILED"},"tasks":[]}`,
			want:    runtime.StateFailed,
		},
		{
			name:    "task failed while connector running",
			payload: `{"name":"x","connector":{"state":"RUNNING"},"tasks":[{"id":0,"state":"FAILED"}]}`,
			want:    runtime.StateFailed,
		},
		{
			name:    "paused",
			payload: `{"name":"x","connector":{"state":"PAUSED"},"tasks":[]}`,
			want:    runtime.StatePaused,
		},
		{
			name:    "stopped",
			payload: `{"name":"x","connector":{"state":"STOPPED"},"tasks":[]}`,
			want:    runtime.StateStopped,
		},
		{
			name:    "unassigned",
			payload: `{"name":"x","connector":{"state":"UNASSIGNED"},"tasks":[]}`,
			want:    runtime.StateUnassigned,
		},
		{
			name:    "unknown state",
			payload: `{"name":"x","connector":{"state":"RECONCILING"},"tasks":[]}`,
			want:    runtime.StateUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/connectors/"+testSourceName+"/status" {
					t.Errorf("unexpected path %q", r.URL.Path)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tt.payload)
			}))
			defer server.Close()

			client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
			state, err := client.ConnectorStatus(context.Background(), testSourceName)
			if err != nil {
				t.Fatalf("ConnectorStatus() error = %v", err)
			}
			if state != tt.want {
				t.Fatalf("ConnectorStatus() = %q, want %q", state, tt.want)
			}
		})
	}
}

func TestHTTPClientConnectorStatusMissing(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
	state, err := client.ConnectorStatus(context.Background(), testSourceName)
	if err != nil {
		t.Fatalf("ConnectorStatus() error = %v", err)
	}
	if state != runtime.StateMissing {
		t.Fatalf("ConnectorStatus() = %q, want %q", state, runtime.StateMissing)
	}
}

func TestHTTPClientConnectorStatusInvalidJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"connector":`)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
	if _, err := client.ConnectorStatus(context.Background(), testSourceName); !errors.Is(err, runtime.ErrConnectUnavailable) {
		t.Fatalf("ConnectorStatus() error = %v, want %v", err, runtime.ErrConnectUnavailable)
	}
}

func TestHTTPClientConnectorStatusServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
	if _, err := client.ConnectorStatus(context.Background(), testSourceName); !errors.Is(err, runtime.ErrConnectUnavailable) {
		t.Fatalf("ConnectorStatus() error = %v, want %v", err, runtime.ErrConnectUnavailable)
	}
}

func TestHTTPClientStopConnector(t *testing.T) {
	t.Parallel()

	var (
		mu     sync.Mutex
		method string
		path   string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		method, path = r.Method, r.URL.Path
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
	if err := client.StopConnector(context.Background(), testSourceName); err != nil {
		t.Fatalf("StopConnector() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if method != http.MethodPut || path != "/connectors/"+testSourceName+"/stop" {
		t.Fatalf("StopConnector() sent %s %s", method, path)
	}
}

func TestHTTPClientStopAndDeleteMissingIsNoop(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
	if err := client.StopConnector(context.Background(), testSourceName); err != nil {
		t.Fatalf("StopConnector() error = %v, want nil for missing connector", err)
	}
	if err := client.DeleteConnector(context.Background(), testSourceName); err != nil {
		t.Fatalf("DeleteConnector() error = %v, want nil for missing connector", err)
	}
}

func TestHTTPClientDeleteConnector(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		if r.URL.Path != "/connectors/"+testSourceName {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
	if err := client.DeleteConnector(context.Background(), testSourceName); err != nil {
		t.Fatalf("DeleteConnector() error = %v", err)
	}
}

func TestHTTPClientPing(t *testing.T) {
	t.Parallel()

	t.Run("reachable", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
		if err := client.Ping(context.Background()); err != nil {
			t.Fatalf("Ping() error = %v", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()
		client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
		if err := client.Ping(context.Background()); !errors.Is(err, runtime.ErrConnectUnavailable) {
			t.Fatalf("Ping() error = %v, want %v", err, runtime.ErrConnectUnavailable)
		}
	})
}

func TestHTTPClientContextCanceled(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(3)))
	err := client.UpsertConnector(ctx, testSourceName, map[string]any{"connector.class": "x"})
	if !errors.Is(err, runtime.ErrConnectUnavailable) {
		t.Fatalf("UpsertConnector() error = %v, want %v", err, runtime.ErrConnectUnavailable)
	}
}

func TestDefaultRetryPolicyDelays(t *testing.T) {
	t.Parallel()

	policy := runtime.DefaultRetryPolicy()
	if policy.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", policy.MaxRetries)
	}

	want := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		16 * time.Second,
	}
	for attempt, expected := range want {
		if got := policy.Delay(attempt); got != expected {
			t.Errorf("Delay(%d) = %v, want %v", attempt, got, expected)
		}
	}
}

func TestNormalizeState(t *testing.T) {
	t.Parallel()

	tests := map[string]runtime.State{
		"RUNNING":    runtime.StateRunning,
		"running":    runtime.StateRunning,
		"FAILED":     runtime.StateFailed,
		"PAUSED":     runtime.StatePaused,
		"STOPPED":    runtime.StateStopped,
		"UNASSIGNED": runtime.StateUnassigned,
		"":           runtime.StateUnknown,
		"WHAT":       runtime.StateUnknown,
	}
	for raw, want := range tests {
		if got := runtime.NormalizeState(raw); got != want {
			t.Errorf("NormalizeState(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestClientErrorMessageIsRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = fmt.Fprint(w, `{"message":"password=cdc_super_secret"}`)
	}))
	defer server.Close()

	client := runtime.NewHTTPClient(server.URL, runtime.WithHTTPRetryPolicy(fastRetryPolicy(0)))
	err := client.UpsertConnector(context.Background(), testSourceName, map[string]any{"database.password": "cdc_super_secret"})
	if err == nil {
		t.Fatal("UpsertConnector() error = nil, want conflict")
	}
	if strings.Contains(err.Error(), "cdc_super_secret") {
		t.Fatalf("error leaks secret: %v", err)
	}
}
