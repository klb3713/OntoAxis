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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

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
}

type Server struct {
	opts       Options
	mux        *chi.Mux
	metrics    *prometheus.Registry
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
	metrics := prometheus.NewRegistry()
	mux := chi.NewRouter()
	s := &Server{opts: opts, mux: mux, metrics: metrics}
	mux.Use(s.requestID)
	mux.Use(s.recoverer)
	mux.Use(s.accessLog)
	mux.Use(s.metricsMiddleware)
	mux.Get("/healthz", s.healthz)
	mux.Get("/readyz", s.readyz)
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
	s.metricsSrv = &http.Server{Addr: opts.MetricsAddr, Handler: promhttp.HandlerFor(metrics, promhttp.HandlerOpts{})}
	return s
}

func (s *Server) Router() http.Handler { return s.mux }

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

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		s.opts.Logger.Info("http request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration_ms", time.Since(start).Milliseconds(), "request_id", observability.RequestID(r.Context()))
	})
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "ontoaxis_http_requests_total", Help: "Total HTTP requests."}, []string{"method", "route", "status"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "ontoaxis_http_request_duration_seconds", Help: "HTTP request duration."}, []string{"method", "route"})
	s.metrics.MustRegister(requests, duration)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		requests.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(sw.status)).Inc()
		duration.WithLabelValues(r.Method, r.URL.Path).Observe(time.Since(start).Seconds())
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
			status = http.StatusServiceUnavailable
		} else {
			checks["database"] = "ok"
		}
	}
	for _, m := range s.opts.Modules {
		if err := m.Ready(r.Context()); err != nil {
			checks[m.Name()] = "not_ready"
			status = http.StatusServiceUnavailable
		} else {
			checks[m.Name()] = "ok"
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
