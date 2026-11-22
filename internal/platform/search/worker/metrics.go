// Package worker hosts the search worker runtime: HTTP endpoints, Prometheus
// metrics, and the consume/rebuild/replay/migrate entry points used by the
// ontoaxis-search command.
package worker

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/klb3713/ontoaxis/internal/platform/observability"
	"github.com/klb3713/ontoaxis/internal/platform/search"
)

// Metrics implements search.Recorder with the D12 metric names and owns its
// Prometheus registry.
type Metrics struct {
	registry        *prometheus.Registry
	events          *prometheus.CounterVec
	lag             prometheus.Histogram
	batchDuration   *prometheus.HistogramVec
	deadLetters     *prometheus.CounterVec
	rebuildDocs     *prometheus.CounterVec
	rebuildDuration *prometheus.HistogramVec
}

// NewMetrics builds and registers the search metrics.
func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	m := &Metrics{
		registry: registry,
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ontoaxis_search_index_events_total",
			Help: "Object index events processed by result.",
		}, []string{"event_type", "result"}),
		lag: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "ontoaxis_search_index_lag_seconds",
			Help:    "Lag between an outbox event being created and being processed.",
			Buckets: observability.DurationBuckets,
		}),
		batchDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ontoaxis_search_index_batch_duration_seconds",
			Help:    "Duration of one consumer batch.",
			Buckets: observability.DurationBuckets,
		}, []string{"result"}),
		deadLetters: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ontoaxis_search_dead_letters_total",
			Help: "Search outbox events dead-lettered by event type.",
		}, []string{"event_type"}),
		rebuildDocs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ontoaxis_search_rebuild_documents_total",
			Help: "Documents written by rebuilds by result.",
		}, []string{"result"}),
		rebuildDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ontoaxis_search_rebuild_duration_seconds",
			Help:    "Duration of index rebuilds by result.",
			Buckets: observability.DurationBuckets,
		}, []string{"result"}),
	}
	registry.MustRegister(
		m.events,
		m.lag,
		m.batchDuration,
		m.deadLetters,
		m.rebuildDocs,
		m.rebuildDuration,
	)
	return m
}

// Registry returns the Prometheus registry for /metrics.
func (m *Metrics) Registry() *prometheus.Registry {
	if m == nil {
		return prometheus.NewRegistry()
	}
	return m.registry
}

// Event implements search.Recorder.
func (m *Metrics) Event(eventType, result string) {
	if m == nil {
		return
	}
	m.events.WithLabelValues(eventType, result).Inc()
}

// Lag implements search.Recorder.
func (m *Metrics) Lag(seconds float64) {
	if m == nil {
		return
	}
	m.lag.Observe(seconds)
}

// Batch implements search.Recorder.
func (m *Metrics) Batch(result string, d time.Duration) {
	if m == nil {
		return
	}
	m.batchDuration.WithLabelValues(result).Observe(d.Seconds())
}

// DeadLetter implements search.Recorder.
func (m *Metrics) DeadLetter(eventType string) {
	if m == nil {
		return
	}
	m.deadLetters.WithLabelValues(eventType).Inc()
}

// Rebuild implements search.Recorder.
func (m *Metrics) Rebuild(result string, d time.Duration, docs int) {
	if m == nil {
		return
	}
	m.rebuildDuration.WithLabelValues(result).Observe(d.Seconds())
	if docs > 0 {
		m.rebuildDocs.WithLabelValues(result).Add(float64(docs))
	}
}

var _ search.Recorder = (*Metrics)(nil)
