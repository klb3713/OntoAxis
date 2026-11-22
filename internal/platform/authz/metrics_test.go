package authz

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsExposeDecisionCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.observeDecision("dataset:read", true, ReasonAllow)
	m.observeDecision("dataset:read", false, ReasonRoleNotPermitted)
	m.observeDuration("fga", 0.01)
	m.observeError("openfga", ReasonEngineTimeout)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	names := map[string]bool{}
	for _, family := range families {
		names[family.GetName()] = true
	}
	for _, want := range []string{
		"ontoaxis_authz_decisions_total",
		"ontoaxis_authz_duration_seconds",
		"ontoaxis_authz_errors_total",
	} {
		if !names[want] {
			t.Fatalf("metric %q not registered; got %v", want, names)
		}
	}
}
