package runtime

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"

	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

func metricFamilies(t *testing.T, reg *prometheus.Registry) map[string]*dto.MetricFamily {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	result := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		result[family.GetName()] = family
	}
	return result
}

func labelNames(family *dto.MetricFamily) map[string]bool {
	names := make(map[string]bool)
	for _, label := range family.GetMetric()[0].GetLabel() {
		names[label.GetName()] = true
	}
	return names
}

func TestMetricsContract(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	metrics := NewMetrics(reg)
	metrics.ObserveRequest(DependencyKafkaConnect, "upsert", ResultOK)
	metrics.ObserveSubmit("submit", ResultOK, 2*time.Second)

	families := metricFamilies(t, reg)

	requests, ok := families["ontoaxis_connector_runtime_requests_total"]
	if !ok {
		t.Fatalf("missing metric ontoaxis_connector_runtime_requests_total, got %v", families)
	}
	labels := labelNames(requests)
	for _, name := range []string{"dependency", "operation", "result"} {
		if !labels[name] {
			t.Errorf("requests metric missing label %q, got %v", name, labels)
		}
	}
	for _, forbidden := range []string{"tenant_id", "connector_id", "topic", "runtime_ref"} {
		if labels[forbidden] {
			t.Errorf("requests metric must not carry label %q", forbidden)
		}
	}
	if value := requests.GetMetric()[0].GetCounter().GetValue(); value != 1 {
		t.Errorf("requests counter = %v, want 1", value)
	}

	submit, ok := families["ontoaxis_connector_submit_seconds"]
	if !ok {
		t.Fatalf("missing metric ontoaxis_connector_submit_seconds, got %v", families)
	}
	histogram := submit.GetMetric()[0].GetHistogram()
	if got, want := histogram.GetSampleCount(), uint64(1); got != want {
		t.Errorf("submit sample count = %d, want %d", got, want)
	}
	if got, want := len(histogram.GetBucket()), len(observability.DurationBuckets); got != want {
		t.Errorf("submit buckets = %d, want %d (frozen observability buckets)", got, want)
	}
	submitLabels := labelNames(submit)
	for _, forbidden := range []string{"tenant_id", "connector_id", "topic", "runtime_ref"} {
		if submitLabels[forbidden] {
			t.Errorf("submit metric must not carry label %q", forbidden)
		}
	}
}

func TestMetricsNilSafe(t *testing.T) {
	t.Parallel()

	var metrics *Metrics
	metrics.ObserveRequest(DependencyKafkaConnect, "upsert", ResultOK)
	metrics.ObserveSubmit("submit", ResultOK, time.Second)
}

func TestRequestResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "success", err: nil, want: ResultOK},
		{name: "retryable", err: fmt.Errorf("%w: timeout", ErrConnectUnavailable), want: ResultRetryableError},
		{name: "rejected", err: fmt.Errorf("%w: status 400", ErrConnectRejected), want: ResultError},
		{name: "plain error", err: errors.New("boom"), want: ResultError},
	}
	for _, tt := range tests {
		if got := RequestResult(tt.err); got != tt.want {
			t.Errorf("RequestResult(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestMetricsRegisteredTwiceReusesCollectors(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	first := NewMetrics(reg)
	second := NewMetrics(reg)
	if first.requests != second.requests || first.submit != second.submit {
		t.Fatal("NewMetrics() on an existing registry must reuse the registered collectors")
	}
	if got := testutil.ToFloat64(first.requests.WithLabelValues(DependencyKafkaConnect, "status", ResultOK)); got != 0 {
		t.Fatalf("counter = %v, want 0 before observation", got)
	}
}
