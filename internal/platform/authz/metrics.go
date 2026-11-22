package authz

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics exposes only bounded-label authorization telemetry. Tenant, user,
// resource IDs, and policy text never become labels.
type Metrics struct {
	decisions *prometheus.CounterVec
	duration  *prometheus.HistogramVec
	errors    *prometheus.CounterVec
}

// NewMetrics registers the authorization collectors. It is safe to call more
// than once in tests because existing collectors are reused.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	return &Metrics{
		decisions: registerCounterVec(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ontoaxis_authz_decisions_total",
			Help: "Authorization decisions by action, decision, and reason.",
		}, []string{"action", "decision", "reason"})),
		duration: registerHistogramVec(reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ontoaxis_authz_duration_seconds",
			Help:    "Authorization engine latency.",
			Buckets: []float64{0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.3},
		}, []string{"engine"})),
		errors: registerCounterVec(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ontoaxis_authz_errors_total",
			Help: "Authorization dependency errors by engine and code.",
		}, []string{"engine", "code"})),
	}
}

func (m *Metrics) observeDecision(action Action, allow bool, reason Reason) {
	if m == nil || m.decisions == nil {
		return
	}
	decision := "deny"
	if allow {
		decision = "allow"
	}
	m.decisions.WithLabelValues(string(action), decision, string(reason)).Inc()
}

func (m *Metrics) observeDuration(engine string, seconds float64) {
	if m == nil || m.duration == nil {
		return
	}
	m.duration.WithLabelValues(engine).Observe(seconds)
}

func (m *Metrics) observeError(engine string, reason Reason) {
	if m == nil || m.errors == nil {
		return
	}
	m.errors.WithLabelValues(engine, string(reason)).Inc()
}

func registerCounterVec(reg prometheus.Registerer, vec *prometheus.CounterVec) *prometheus.CounterVec {
	if err := reg.Register(vec); err != nil {
		if already, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, ok := already.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
		panic(err)
	}
	return vec
}

func registerHistogramVec(reg prometheus.Registerer, vec *prometheus.HistogramVec) *prometheus.HistogramVec {
	if err := reg.Register(vec); err != nil {
		if already, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, ok := already.ExistingCollector.(*prometheus.HistogramVec); ok {
				return existing
			}
		}
		panic(err)
	}
	return vec
}
