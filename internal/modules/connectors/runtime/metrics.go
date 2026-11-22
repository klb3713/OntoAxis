package runtime

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

const (
	// DependencyKafkaConnect is the bounded metric/trace dependency label.
	DependencyKafkaConnect = "kafka_connect"
	// ResultOK is the success result label.
	ResultOK = "ok"
	// ResultError is the terminal failure result label.
	ResultError = "error"
	// ResultRetryableError is the retryable failure result label.
	ResultRetryableError = "retryable_error"
)

// Metrics exposes only bounded-label connector runtime telemetry. Tenant,
// connector, topic, and runtime connector names never become labels.
type Metrics struct {
	requests *prometheus.CounterVec
	submit   *prometheus.HistogramVec
}

// NewMetrics registers the connector runtime collectors. It is safe to call more
// than once in tests because existing collectors are reused.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	return &Metrics{
		requests: registerCounterVec(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ontoaxis_connector_runtime_requests_total",
			Help: "Kafka Connect runtime requests by dependency, operation, and result.",
		}, []string{"dependency", "operation", "result"})),
		submit: registerHistogramVec(reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ontoaxis_connector_submit_seconds",
			Help:    "Connector runtime submit orchestration duration.",
			Buckets: observability.DurationBuckets,
		}, []string{"operation", "result"})),
	}
}

// ObserveRequest records one Kafka Connect REST request outcome.
func (m *Metrics) ObserveRequest(dependency, operation, result string) {
	if m == nil || m.requests == nil {
		return
	}
	m.requests.WithLabelValues(dependency, operation, result).Inc()
}

// ObserveSubmit records one orchestration step duration.
func (m *Metrics) ObserveSubmit(operation string, result string, duration time.Duration) {
	if m == nil || m.submit == nil {
		return
	}
	m.submit.WithLabelValues(operation, result).Observe(duration.Seconds())
}

// RequestResult maps an error to the bounded request result label.
func RequestResult(err error) string {
	switch {
	case err == nil:
		return ResultOK
	case errors.Is(err, ErrConnectUnavailable):
		return ResultRetryableError
	default:
		return ResultError
	}
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
