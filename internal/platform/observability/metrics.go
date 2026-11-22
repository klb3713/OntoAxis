package observability

import "github.com/prometheus/client_golang/prometheus"

// DurationBuckets are the frozen latency buckets from the observability
// baseline. Do not change them per service.
var DurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Metrics owns the platform metric collectors and their single registry.
type Metrics struct {
	Registry *prometheus.Registry

	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPInFlight        prometheus.Gauge
	BuildInfo           *prometheus.GaugeVec
	ModuleReady         *prometheus.GaugeVec
	DBPoolConnections   *prometheus.GaugeVec
	DBQueryDuration     *prometheus.HistogramVec
	OutboundDuration    *prometheus.HistogramVec
	JobsTotal           *prometheus.CounterVec
}

// NewMetrics creates and registers the platform collectors onto reg. A nil
// registry creates a private registry; callers exposing /metrics must use it.
func NewMetrics(reg *prometheus.Registry) *Metrics {
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	m := &Metrics{
		Registry: reg,
		HTTPRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ontoaxis_http_requests_total", Help: "Total HTTP requests.",
		}, []string{"method", "route", "status"}),
		HTTPRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "ontoaxis_http_request_duration_seconds", Help: "HTTP request duration.",
			Buckets: DurationBuckets,
		}, []string{"method", "route"}),
		HTTPInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ontoaxis_http_requests_in_flight", Help: "In-flight HTTP requests.",
		}),
		BuildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ontoaxis_build_info", Help: "Build version information.",
		}, []string{"version", "commit"}),
		ModuleReady: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ontoaxis_module_ready", Help: "Module readiness: 1 ready, 0 not ready.",
		}, []string{"module"}),
		DBPoolConnections: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ontoaxis_db_pool_connections", Help: "Database pool connections by state.",
		}, []string{"state"}),
		DBQueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "ontoaxis_db_query_duration_seconds", Help: "Database query duration.",
			Buckets: DurationBuckets,
		}, []string{"operation"}),
		OutboundDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "ontoaxis_outbound_request_duration_seconds", Help: "Outbound dependency request duration.",
			Buckets: DurationBuckets,
		}, []string{"dependency", "operation", "result"}),
		JobsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ontoaxis_jobs_total", Help: "Background jobs by type and result.",
		}, []string{"type", "result"}),
	}
	m.Registry.MustRegister(
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.HTTPInFlight,
		m.BuildInfo,
		m.ModuleReady,
		m.DBPoolConnections,
		m.DBQueryDuration,
		m.OutboundDuration,
		m.JobsTotal,
	)
	m.BuildInfo.WithLabelValues("dev", "unknown").Set(1)
	return m
}

// SetBuildInfo overwrites the build information series.
func (m *Metrics) SetBuildInfo(version, commit string) {
	if m == nil {
		return
	}
	if version == "" {
		version = "dev"
	}
	if commit == "" {
		commit = "unknown"
	}
	m.BuildInfo.Reset()
	m.BuildInfo.WithLabelValues(version, commit).Set(1)
}
