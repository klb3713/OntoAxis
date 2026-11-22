package worker

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	actiontemporal "github.com/klb3713/ontoaxis/internal/modules/actions/temporal"
)

// Metrics implements actiontemporal.MetricsRecorder and exposes the reviewed
// action execution metrics. Label values are a whitelist (status and activity
// names); tenant and execution identifiers are never emitted.
type Metrics struct {
	registry        *prometheus.Registry
	executionsTotal *prometheus.CounterVec
	activityRetries *prometheus.CounterVec
	approvalWait    prometheus.Histogram
	inFlight        prometheus.Gauge
}

var _ actiontemporal.MetricsRecorder = (*Metrics)(nil)

// NewMetrics registers the worker metrics on the given registry. A nil
// registry creates a private one, which is convenient for tests.
func NewMetrics(registry *prometheus.Registry) *Metrics {
	if registry == nil {
		registry = prometheus.NewRegistry()
	}
	metrics := &Metrics{
		registry: registry,
		executionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ontoaxis_action_executions_total",
			Help: "Action executions that reached a terminal status, labeled by status.",
		}, []string{"status"}),
		activityRetries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ontoaxis_action_activity_retries_total",
			Help: "Activity retries observed by the actions worker.",
		}, []string{"activity"}),
		approvalWait: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "ontoaxis_action_approval_wait_seconds",
			Help:    "Time an execution workflow waited for a human approval decision.",
			Buckets: prometheus.DefBuckets,
		}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ontoaxis_action_workflows_in_flight",
			Help: "Action execution workflows currently owned by this worker.",
		}),
	}
	registry.MustRegister(metrics.executionsTotal, metrics.activityRetries, metrics.approvalWait, metrics.inFlight)
	return metrics
}

// Registry returns the registry the metrics were registered on.
func (m *Metrics) Registry() *prometheus.Registry {
	if m == nil {
		return nil
	}
	return m.registry
}

// ExecutionFinished counts one terminal execution.
func (m *Metrics) ExecutionFinished(status string) {
	if m == nil {
		return
	}
	m.executionsTotal.WithLabelValues(status).Inc()
}

// ApprovalWait observes one approval waiting duration.
func (m *Metrics) ApprovalWait(wait time.Duration) {
	if m == nil || wait <= 0 {
		return
	}
	m.approvalWait.Observe(wait.Seconds())
}

// ActivityRetried counts one retried activity.
func (m *Metrics) ActivityRetried(activity string) {
	if m == nil {
		return
	}
	m.activityRetries.WithLabelValues(activity).Inc()
}

// WorkflowStarted tracks workflows owned by this worker.
func (m *Metrics) WorkflowStarted() {
	if m == nil {
		return
	}
	m.inFlight.Inc()
}

// WorkflowFinished tracks workflows owned by this worker.
func (m *Metrics) WorkflowFinished() {
	if m == nil {
		return
	}
	m.inFlight.Dec()
}
