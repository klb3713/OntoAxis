package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsContract(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	m.SetBuildInfo("0.1.0", "abc123")
	m.HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/objects/{id}", "200").Inc()
	m.HTTPInFlight.Inc()
	m.ModuleReady.WithLabelValues("objects").Set(1)
	m.DBPoolConnections.WithLabelValues("total").Set(10)
	m.DBQueryDuration.WithLabelValues("select").Observe(0.02)
	m.OutboundDuration.WithLabelValues("trino", "query", "ok").Observe(0.3)
	m.JobsTotal.WithLabelValues("index", "ok").Inc()

	if got := testutil.ToFloat64(m.HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/objects/{id}", "200")); got != 1 {
		t.Fatalf("requests_total = %v", got)
	}
	if got := testutil.ToFloat64(m.BuildInfo.WithLabelValues("0.1.0", "abc123")); got != 1 {
		t.Fatalf("build_info = %v", got)
	}
	if got := testutil.ToFloat64(m.HTTPInFlight); got != 1 {
		t.Fatalf("in_flight = %v", got)
	}
}
