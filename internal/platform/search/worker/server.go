package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const readinessTimeout = 2 * time.Second

// DatabasePinger is the subset of pgxpool.Pool used by readiness checks.
type DatabasePinger interface {
	Ping(ctx context.Context) error
}

// SearchHealth is the subset of search.Client used by readiness checks.
type SearchHealth interface {
	Health(ctx context.Context) error
}

// Server exposes process health, dependency readiness, and Prometheus metrics.
type Server struct {
	db      DatabasePinger
	search  SearchHealth
	metrics *Metrics
}

// NewServer constructs the HTTP handler set.
func NewServer(db DatabasePinger, searchClient SearchHealth, metrics *Metrics) *Server {
	return &Server{db: db, search: searchClient, metrics: metrics}
}

// HTTPHandler returns the worker's HTTP routes.
func (s *Server) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	if s.metrics != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(s.metrics.Registry(), promhttp.HandlerOpts{}))
	}
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "dependency": "postgres"})
		return
	}
	if err := s.db.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "dependency": "postgres"})
		return
	}
	if s.search == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "dependency": "search"})
		return
	}
	if err := s.search.Health(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "dependency": "search"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
