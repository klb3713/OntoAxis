package worker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsExposeD12Names(t *testing.T) {
	metrics := NewMetrics()
	metrics.Event("objects.object.created.v1", "success")
	metrics.Lag(0.25)
	metrics.Batch("success", 10*time.Millisecond)
	metrics.DeadLetter("objects.object.created.v1")
	metrics.Rebuild("success", time.Second, 3)

	families, err := metrics.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather error = %v", err)
	}
	names := make(map[string]bool, len(families))
	for _, family := range families {
		names[family.GetName()] = true
	}
	for _, want := range []string{
		"ontoaxis_search_index_events_total",
		"ontoaxis_search_index_lag_seconds",
		"ontoaxis_search_index_batch_duration_seconds",
		"ontoaxis_search_dead_letters_total",
		"ontoaxis_search_rebuild_documents_total",
		"ontoaxis_search_rebuild_duration_seconds",
	} {
		if !names[want] {
			t.Fatalf("metric %q not registered; got %v", want, names)
		}
	}
}

func TestServerMetricsEndpoint(t *testing.T) {
	metrics := NewMetrics()
	metrics.Event("objects.object.updated.v1", "retry")
	server := NewServer(fakePinger{}, fakeHealth{}, metrics)
	recorder := httptest.NewRecorder()
	server.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{"ontoaxis_search_index_events_total", `event_type="objects.object.updated.v1"`, `result="retry"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("/metrics missing %q:\n%s", want, body)
		}
	}
}

func TestNilMetricsSafe(t *testing.T) {
	var metrics *Metrics
	metrics.Event("a", "b")
	metrics.Lag(1)
	metrics.Batch("ok", time.Second)
	metrics.DeadLetter("a")
	metrics.Rebuild("ok", time.Second, 1)
	if metrics.Registry() == nil {
		t.Fatal("nil metrics registry is nil")
	}
}
