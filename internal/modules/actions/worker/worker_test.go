package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	actionservice "github.com/klb3713/ontoaxis/internal/modules/actions/service"
	actiontemporal "github.com/klb3713/ontoaxis/internal/modules/actions/temporal"
)

func testConfig() actiontemporal.Config {
	return actiontemporal.Config{
		Namespace:          "ontoaxis",
		TaskQueue:          "ontoaxis-actions",
		Mode:               actionservice.ExecutionModeNoop,
		ApprovalTimeout:    actiontemporal.DefaultApprovalTimeout,
		WorkflowRunTimeout: actiontemporal.DefaultWorkflowRunTimeout,
	}
}

func TestHealthzIsAlwaysOK(t *testing.T) {
	worker := New(testConfig(), nil, nil, WithHealthCheck(func(context.Context) error {
		return errors.New("engine down")
	}))
	recorder := httptest.NewRecorder()
	worker.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("healthz body = %q", recorder.Body.String())
	}
}

func TestReadyzReportsUnhealthyEngine(t *testing.T) {
	worker := New(testConfig(), nil, nil, WithHealthCheck(func(context.Context) error {
		return errors.New("temporal unreachable")
	}))
	recorder := httptest.NewRecorder()
	worker.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), "not_ready") {
		t.Fatalf("readyz body = %q", recorder.Body.String())
	}
}

func TestReadyzReportsHealthyEngine(t *testing.T) {
	worker := New(testConfig(), nil, nil, WithHealthCheck(func(context.Context) error {
		return nil
	}))
	recorder := httptest.NewRecorder()
	worker.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("readyz status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), "ready") {
		t.Fatalf("readyz body = %q", recorder.Body.String())
	}
}

func TestNoopReadinessDoesNotRequireEngine(t *testing.T) {
	worker := New(testConfig(), nil, nil)
	if err := worker.Ready(context.Background()); err != nil {
		t.Fatalf("noop readiness error = %v", err)
	}
}

func TestTemporalReadinessFailsClosedWithoutClient(t *testing.T) {
	cfg := testConfig()
	cfg.Mode = actionservice.ExecutionModeTemporal
	cfg.HostPort = "127.0.0.1:7233"
	worker := New(cfg, nil, nil)
	err := worker.Ready(context.Background())
	if !errors.Is(err, actionservice.ErrEngineUnavailable) {
		t.Fatalf("readiness error = %v, want ErrEngineUnavailable", err)
	}
}

func TestMetricsEndpointExposesWhitelistedMetrics(t *testing.T) {
	metrics := NewMetrics(nil)
	metrics.ExecutionFinished("succeeded")
	metrics.ActivityRetried("ApplyStep")
	worker := New(testConfig(), nil, nil, WithMetrics(metrics))
	recorder := httptest.NewRecorder()
	worker.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		"ontoaxis_action_executions_total",
		"ontoaxis_action_activity_retries_total",
		"ontoaxis_action_approval_wait_seconds",
		"ontoaxis_action_workflows_in_flight",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body is missing %q", want)
		}
	}
	for _, forbidden := range []string{"tenant_id", "execution_id", "object_id", "user_id"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("metrics body leaks forbidden label %q", forbidden)
		}
	}
}

func TestRunRejectsNoopMode(t *testing.T) {
	worker := New(testConfig(), nil, nil)
	err := worker.Run(context.Background())
	if !errors.Is(err, actionservice.ErrEngineUnavailable) {
		t.Fatalf("run error = %v, want ErrEngineUnavailable", err)
	}
}

func TestRunRequiresTemporalClient(t *testing.T) {
	cfg := testConfig()
	cfg.Mode = actionservice.ExecutionModeTemporal
	cfg.HostPort = "127.0.0.1:7233"
	worker := New(cfg, nil, nil)
	err := worker.Run(context.Background())
	if !errors.Is(err, actionservice.ErrEngineUnavailable) {
		t.Fatalf("run error = %v, want ErrEngineUnavailable", err)
	}
}
