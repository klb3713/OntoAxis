// Package api adapts HTTP requests to ingestion service calls. It contains no
// persistence or domain rules beyond request decoding and error translation.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/service"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

const (
	tenantHeader = "X-Ontoaxis-Tenant-Id"
	maxPerPage   = 100
)

// Service is the narrow API-facing ingestion use case interface.
type Service interface {
	CreateJob(ctx context.Context, tenantID, connectorID string) (*domain.SyncJob, error)
	GetJob(ctx context.Context, tenantID, id string) (*domain.SyncJob, error)
	ListJobs(ctx context.Context, tenantID, connectorID string, page, perPage int32) (*service.ListResult, error)
	CancelJob(ctx context.Context, tenantID, id string) (*domain.SyncJob, error)
	ListRuns(ctx context.Context, tenantID, jobID string) ([]domain.SyncRun, error)
	Watch(ctx context.Context, tenantID, jobID string) (<-chan *domain.SyncJob, <-chan error, error)
}

// Handler serves the /api/v1/ingestion routes.
type Handler struct {
	svc Service
}

// NewHandler builds an ingestion HTTP handler.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts all ingestion routes on the supplied chi router.
func (h *Handler) Register(r chi.Router) {
	r.Route("/api/v1/ingestion/jobs", func(r chi.Router) {
		r.Get("/", h.list)
		r.Post("/", h.create)
		r.Route("/{jobID}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Post("/cancel", h.cancel)
			r.Get("/watch", h.watch)
			r.Get("/runs", h.listRuns)
		})
	})
}

type jobDTO struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	ConnectorID string         `json:"connector_id"`
	Mode        string         `json:"mode"`
	Status      string         `json:"status"`
	Message     string         `json:"message"`
	Config      map[string]any `json:"config"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	FinishedAt  *time.Time     `json:"finished_at,omitempty"`
}

type runDTO struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	JobID      string         `json:"job_id"`
	Status     string         `json:"status"`
	Stats      map[string]any `json:"stats"`
	Error      string         `json:"error,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
}

type pageDTO struct {
	Page       int32 `json:"page"`
	PerPage    int32 `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int32 `json:"total_pages"`
}

type listResponse struct {
	Jobs []jobDTO `json:"jobs"`
	Page pageDTO  `json:"page"`
}

type createRequest struct {
	ConnectorID string `json:"connector_id"`
}

type runsResponse struct {
	Runs []runDTO `json:"runs"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid JSON request body"))
		return
	}
	if !h.requireService(w, r) {
		return
	}
	job, err := h.svc.CreateJob(r.Context(), tenantID, req.ConnectorID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toJobDTO(job))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	if !h.requireService(w, r) {
		return
	}
	job, err := h.svc.GetJob(r.Context(), tenantID, chi.URLParam(r, "jobID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toJobDTO(job))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	page, perPage, err := parsePage(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	connectorID := strings.TrimSpace(r.URL.Query().Get("connector_id"))
	if !h.requireService(w, r) {
		return
	}
	result, err := h.svc.ListJobs(r.Context(), tenantID, connectorID, page, perPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]jobDTO, 0, len(result.Jobs))
	for i := range result.Jobs {
		items = append(items, toJobDTO(&result.Jobs[i]))
	}
	writeJSON(w, http.StatusOK, listResponse{
		Jobs: items,
		Page: pageDTO{Page: result.Page, PerPage: result.PerPage, Total: result.Total, TotalPages: result.TotalPages},
	})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	if !h.requireService(w, r) {
		return
	}
	job, err := h.svc.CancelJob(r.Context(), tenantID, chi.URLParam(r, "jobID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toJobDTO(job))
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	if !h.requireService(w, r) {
		return
	}
	runs, err := h.svc.ListRuns(r.Context(), tenantID, chi.URLParam(r, "jobID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]runDTO, 0, len(runs))
	for i := range runs {
		items = append(items, toRunDTO(&runs[i]))
	}
	writeJSON(w, http.StatusOK, runsResponse{Runs: items})
}

func (h *Handler) watch(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	if !h.requireService(w, r) {
		return
	}
	updates, errs, err := h.svc.Watch(r.Context(), tenantID, chi.URLParam(r, "jobID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httperr.Write(w, r, httperr.Internal())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	for updates != nil || errs != nil {
		select {
		case job, open := <-updates:
			if !open {
				updates = nil
				continue
			}
			writeSSE(w, "status", toJobDTO(job))
			flusher.Flush()
		case watchErr, open := <-errs:
			if !open {
				errs = nil
				continue
			}
			if watchErr != nil {
				writeSSE(w, "error", map[string]string{"message": watchErr.Error()})
				flusher.Flush()
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Handler) requireService(w http.ResponseWriter, r *http.Request) bool {
	if h.svc == nil {
		httperr.Write(w, r, httperr.Unavailable("ingestion service is not initialized"))
		return false
	}
	return true
}

func tenantFromRequest(r *http.Request) (string, error) {
	if len(r.Header.Values("X-Tenant-Id")) > 0 {
		return "", errors.New("external X-Tenant-Id header is not accepted")
	}
	value := strings.TrimSpace(r.Header.Get(tenantHeader))
	if value == "" {
		return "", fmt.Errorf("%s header is required", tenantHeader)
	}
	if _, err := uuid.Parse(value); err != nil {
		return "", fmt.Errorf("%s header must be a UUID", tenantHeader)
	}
	return value, nil
}

func parsePage(r *http.Request) (int32, int32, error) {
	page := int32(1)
	perPage := int32(20)
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || value < 1 {
			return 0, 0, errors.New("page must be a positive integer")
		}
		page = int32(value)
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("per_page")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || value < 1 || value > maxPerPage {
			return 0, 0, fmt.Errorf("per_page must be between 1 and %d", maxPerPage)
		}
		perPage = int32(value)
	}
	return page, perPage, nil
}

func decodeJSON(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func toJobDTO(job *domain.SyncJob) jobDTO {
	config := job.Config
	if config == nil {
		config = map[string]any{}
	}
	return jobDTO{
		ID:          job.ID,
		TenantID:    job.TenantID,
		ConnectorID: job.ConnectorID,
		Mode:        job.Mode,
		Status:      string(job.Status),
		Message:     job.Message,
		Config:      config,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
		StartedAt:   job.StartedAt,
		FinishedAt:  job.FinishedAt,
	}
}

func toRunDTO(run *domain.SyncRun) runDTO {
	stats := run.Stats
	if stats == nil {
		stats = map[string]any{}
	}
	return runDTO{
		ID:         run.ID,
		TenantID:   run.TenantID,
		JobID:      run.JobID,
		Status:     string(run.Status),
		Stats:      stats,
		Error:      run.Error,
		CreatedAt:  run.CreatedAt,
		UpdatedAt:  run.UpdatedAt,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
	}
}

func writeSSE(w http.ResponseWriter, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidTenant),
		errors.Is(err, service.ErrInvalidInput),
		errors.Is(err, domain.ErrInvalidStatus):
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
	case errors.Is(err, domain.ErrNotFound):
		httperr.Write(w, r, httperr.NotFound(err.Error()))
	case errors.Is(err, domain.ErrConflict),
		errors.Is(err, domain.ErrInvalidTransition):
		httperr.Write(w, r, httperr.Conflict(err.Error()))
	case errors.Is(err, service.ErrUnavailable):
		httperr.Write(w, r, httperr.Unavailable(err.Error()))
	default:
		httperr.Write(w, r, httperr.Internal())
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
