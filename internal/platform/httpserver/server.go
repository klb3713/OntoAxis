package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/klb3713/ontoaxis/internal/platform/httperr"
	"github.com/klb3713/ontoaxis/internal/platform/module"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

type DBPinger interface {
	Ping(ctx context.Context) error
}

type Options struct {
	ServiceName     string
	Version         string
	HTTPAddr        string
	MetricsAddr     string
	ShutdownTimeout time.Duration
	Logger          *slog.Logger
	DB              DBPinger
	Modules         []module.Module
	// Authenticate wraps all module-owned routes. Health and readiness stay
	// public; when nil, module routes are left unwrapped (tests only).
	Authenticate func(http.Handler) http.Handler
	// EdgeAuth is the forward-auth endpoint used by external gateways. It is
	// always mounted at /internal/edge-auth and never wrapped by Authenticate.
	EdgeAuth http.Handler
	// Metrics is optional; when nil the server creates its own registry.
	Metrics *observability.Metrics
	// Tracer starts the inbound HTTP span; nil uses a no-op tracer.
	Tracer trace.Tracer
	// Commit is exposed through ontoaxis_build_info.
	Commit string
}

type Server struct {
	opts       Options
	mux        *chi.Mux
	metrics    *observability.Metrics
	httpSrv    *http.Server
	metricsSrv *http.Server
}

func New(opts Options) *Server {
	if opts.ServiceName == "" {
		opts.ServiceName = "ontoaxis-core"
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	metrics := opts.Metrics
	if metrics == nil {
		metrics = observability.NewMetrics(prometheus.NewRegistry())
	}
	metrics.SetBuildInfo(opts.Version, opts.Commit)
	mux := chi.NewRouter()
	s := &Server{opts: opts, mux: mux, metrics: metrics}
	s.initReadinessMetrics()
	mux.Use(s.requestID)
	mux.Use(observability.HTTPMiddleware(opts.Tracer))
	mux.Use(s.recoverer)
	mux.Use(s.accessLog)
	mux.Use(s.metricsMiddleware)
	mux.Get("/healthz", s.healthz)
	mux.Get("/readyz", s.readyz)
	if opts.EdgeAuth != nil {
		mux.Handle("/internal/edge-auth", opts.EdgeAuth)
		mux.Handle("/internal/edge-auth/*", opts.EdgeAuth)
	}
	if opts.Authenticate != nil {
		mux.Group(func(r chi.Router) {
			r.Use(opts.Authenticate)
			for _, m := range opts.Modules {
				m.RegisterRoutes(r)
			}
		})
	} else {
		for _, m := range opts.Modules {
			m.RegisterRoutes(mux)
		}
	}
	mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httperr.Write(w, r, httperr.NotFound("route not found"))
	})
	s.httpSrv = &http.Server{Addr: opts.HTTPAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	s.metricsSrv = &http.Server{Addr: opts.MetricsAddr, Handler: promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{})}
	return s
}

func (s *Server) Router() http.Handler { return s.mux }

// initReadinessMetrics makes readiness and pool gauges visible before the first
// /readyz call. The readiness loop still flips them to 0 on failure.
func (s *Server) initReadinessMetrics() {
	if s.opts.DB != nil {
		s.metrics.ModuleReady.WithLabelValues("database").Set(1)
		s.observeDBPool()
	}
	for _, m := range s.opts.Modules {
		s.metrics.ModuleReady.WithLabelValues(m.Name()).Set(1)
	}
}

func (s *Server) observeDBPool() {
	statter, ok := s.opts.DB.(interface{ Stat() *pgxpool.Stat })
	if !ok {
		return
	}
	stat := statter.Stat()
	s.metrics.DBPoolConnections.WithLabelValues("total").Set(float64(stat.TotalConns()))
	s.metrics.DBPoolConnections.WithLabelValues("idle").Set(float64(stat.IdleConns()))
	s.metrics.DBPoolConnections.WithLabelValues("acquired").Set(float64(stat.AcquiredConns()))
	s.metrics.DBPoolConnections.WithLabelValues("max").Set(float64(stat.MaxConns()))
}

// Metrics exposes the platform collectors for tests and /metrics.
func (s *Server) Metrics() *observability.Metrics { return s.metrics }

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(observability.WithRequestID(r.Context(), id)))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.opts.Logger.Error("request panic", "request_id", observability.RequestID(r.Context()), "error", fmt.Sprint(rec))
				httperr.Write(w, r, httperr.Internal())
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush preserves streaming support for SSE/WebSocket-adjacent handlers.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		attrs := []any{"method", r.Method, "path", r.URL.Path, "status", sw.status, "duration_ms", time.Since(start).Milliseconds(), "request_id", observability.RequestID(r.Context())}
		if sc := trace.SpanFromContext(r.Context()).SpanContext(); sc.IsValid() {
			attrs = append(attrs, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
		}
		s.opts.Logger.Info("http request", attrs...)
	})
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.metrics.HTTPInFlight.Inc()
		defer s.metrics.HTTPInFlight.Dec()
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		route := "unmatched"
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
			route = rc.RoutePattern()
		}
		s.metrics.HTTPRequestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(sw.status)).Inc()
		s.metrics.HTTPRequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": s.opts.ServiceName, "version": s.opts.Version, "timestamp": time.Now().UTC().Format(time.RFC3339)})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	status := http.StatusOK
	if s.opts.DB != nil {
		if err := s.opts.DB.Ping(r.Context()); err != nil {
			checks["database"] = "down"
			s.metrics.ModuleReady.WithLabelValues("database").Set(0)
			status = http.StatusServiceUnavailable
		} else {
			checks["database"] = "ok"
			s.metrics.ModuleReady.WithLabelValues("database").Set(1)
		}
		if statter, ok := s.opts.DB.(interface{ Stat() *pgxpool.Stat }); ok {
			stat := statter.Stat()
			s.metrics.DBPoolConnections.WithLabelValues("total").Set(float64(stat.TotalConns()))
			s.metrics.DBPoolConnections.WithLabelValues("idle").Set(float64(stat.IdleConns()))
			s.metrics.DBPoolConnections.WithLabelValues("acquired").Set(float64(stat.AcquiredConns()))
		}
	}
	for _, m := range s.opts.Modules {
		if err := m.Ready(r.Context()); err != nil {
			checks[m.Name()] = "not_ready"
			s.metrics.ModuleReady.WithLabelValues(m.Name()).Set(0)
			status = http.StatusServiceUnavailable
		} else {
			checks[m.Name()] = "ok"
			s.metrics.ModuleReady.WithLabelValues(m.Name()).Set(1)
		}
	}
	writeJSON(w, status, map[string]any{"status": map[bool]string{true: "ok", false: "not_ready"}[status == http.StatusOK], "checks": checks})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Run starts the public and metrics HTTP servers and gracefully shuts down on
// context cancellation.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 2)
	go func() {
		if err := s.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()
	if s.opts.MetricsAddr != "" {
		go func() {
			if err := s.metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("metrics server: %w", err)
			}
		}()
	}
	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.opts.ShutdownTimeout)
	defer cancel()
	_ = s.httpSrv.Shutdown(shutdownCtx)
	_ = s.metricsSrv.Shutdown(shutdownCtx)
	return nil
}
