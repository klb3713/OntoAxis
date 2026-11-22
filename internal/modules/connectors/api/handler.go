// Package api adapts HTTP requests to connectors service calls. It contains no
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

	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/service"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

const (
	tenantHeader = "X-Ontoaxis-Tenant-Id"
	userHeader   = "X-Ontoaxis-User-Id"
	maxPerPage   = 100
)

// Service is the narrow API-facing connectors use case interface.
type Service interface {
	CreateConnector(ctx context.Context, tenantID string, in service.CreateInput) (*domain.Connector, error)
	GetConnector(ctx context.Context, tenantID, id string) (*domain.Connector, error)
	ListConnectors(ctx context.Context, tenantID string, page, perPage int32) (*service.ListResult, error)
	UpdateConnector(ctx context.Context, tenantID, id string, in service.UpdateInput) (*domain.Connector, error)
	DeleteConnector(ctx context.Context, tenantID, id string) error
	TestConnector(ctx context.Context, tenantID, id string) (*service.TestResult, error)
	InferSchema(ctx context.Context, tenantID, id string) (map[string]any, error)
	StartSync(ctx context.Context, tenantID, connectorID string) (string, error)
}

// Handler serves the /api/v1/connectors routes.
type Handler struct {
	svc Service
}

// NewHandler builds a connectors HTTP handler.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts all connector routes on the supplied chi router.
func (h *Handler) Register(r chi.Router) {
	r.Route("/api/v1/connectors", func(r chi.Router) {
		r.Get("/", h.list)
		r.Post("/", h.create)
		r.Route("/{connectorID}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Patch("/", h.update)
			r.Delete("/", h.delete)
			r.Post("/test", h.test)
			r.Post("/sync", h.startSync)
			r.Post("/infer-schema", h.inferSchema)
		})
	})
}

type connectorDTO struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name,omitempty"`
	Type        string         `json:"type"`
	Config      map[string]any `json:"config"`
	Tags        []string       `json:"tags"`
	Status      string         `json:"status"`
	CreatedBy   string         `json:"created_by,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type pageDTO struct {
	Page       int32 `json:"page"`
	PerPage    int32 `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int32 `json:"total_pages"`
}

type listResponse struct {
	Connectors []connectorDTO `json:"connectors"`
	Page       pageDTO        `json:"page"`
}

type createRequest struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Type        string          `json:"type"`
	Config      json.RawMessage `json:"config"`
	Tags        []string        `json:"tags"`
	SecretRef   string          `json:"secret_ref"`
}

type updateRequest struct {
	Name        *string         `json:"name"`
	DisplayName *string         `json:"display_name"`
	Config      json.RawMessage `json:"config"`
	Tags        *[]string       `json:"tags"`
	SecretRef   *string         `json:"secret_ref"`
	Status      *string         `json:"status"`
}

type testResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type startSyncResponse struct {
	JobID string `json:"job_id"`
}

type inferSchemaResponse struct {
	Schema map[string]any `json:"schema"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	createdBy, err := userFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid JSON request body"))
		return
	}
	config, err := decodeConfig(req.Config, true)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	if !h.requireService(w, r) {
		return
	}
	connector, err := h.svc.CreateConnector(r.Context(), tenantID, service.CreateInput{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Type:        req.Type,
		Config:      config,
		Tags:        req.Tags,
		SecretRef:   req.SecretRef,
		CreatedBy:   createdBy,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDTO(connector))
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
	connector, err := h.svc.GetConnector(r.Context(), tenantID, chi.URLParam(r, "connectorID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(connector))
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
	if !h.requireService(w, r) {
		return
	}
	result, err := h.svc.ListConnectors(r.Context(), tenantID, page, perPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]connectorDTO, 0, len(result.Connectors))
	for i := range result.Connectors {
		items = append(items, toDTO(&result.Connectors[i]))
	}
	writeJSON(w, http.StatusOK, listResponse{
		Connectors: items,
		Page:       pageDTO{Page: result.Page, PerPage: result.PerPage, Total: result.Total, TotalPages: result.TotalPages},
	})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	var req updateRequest
	if err := decodeJSON(r, &req); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid JSON request body"))
		return
	}
	in := service.UpdateInput{Name: req.Name, DisplayName: req.DisplayName, Tags: req.Tags, SecretRef: req.SecretRef, Status: req.Status}
	if len(req.Config) > 0 {
		config, err := decodeConfig(req.Config, false)
		if err != nil {
			httperr.Write(w, r, httperr.BadRequest(err.Error()))
			return
		}
		in.Config = &config
	}
	if !h.requireService(w, r) {
		return
	}
	connector, err := h.svc.UpdateConnector(r.Context(), tenantID, chi.URLParam(r, "connectorID"), in)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(connector))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	if !h.requireService(w, r) {
		return
	}
	if err := h.svc.DeleteConnector(r.Context(), tenantID, chi.URLParam(r, "connectorID")); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	if !h.requireService(w, r) {
		return
	}
	result, err := h.svc.TestConnector(r.Context(), tenantID, chi.URLParam(r, "connectorID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, testResponse{OK: result.OK, Message: result.Message})
}

func (h *Handler) startSync(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	if !h.requireService(w, r) {
		return
	}
	jobID, err := h.svc.StartSync(r.Context(), tenantID, chi.URLParam(r, "connectorID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, startSyncResponse{JobID: jobID})
}

func (h *Handler) inferSchema(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
		return
	}
	if !h.requireService(w, r) {
		return
	}
	schema, err := h.svc.InferSchema(r.Context(), tenantID, chi.URLParam(r, "connectorID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, inferSchemaResponse{Schema: schema})
}

func (h *Handler) requireService(w http.ResponseWriter, r *http.Request) bool {
	if h.svc == nil {
		httperr.Write(w, r, httperr.Unavailable("connectors service is not initialized"))
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

func userFromRequest(r *http.Request) (string, error) {
	if len(r.Header.Values("X-User-Id")) > 0 {
		return "", errors.New("external X-User-Id header is not accepted")
	}
	value := strings.TrimSpace(r.Header.Get(userHeader))
	if value == "" {
		return "", nil
	}
	if _, err := uuid.Parse(value); err != nil {
		return "", fmt.Errorf("%s header must be a UUID", userHeader)
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
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	return nil
}

func decodeConfig(raw json.RawMessage, allowEmpty bool) (map[string]any, error) {
	if len(raw) == 0 {
		if allowEmpty {
			return map[string]any{}, nil
		}
		return nil, errors.New("config must be a JSON object")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, errors.New("config must be a JSON object")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("config must be a JSON object")
	}
	return object, nil
}

func toDTO(connector *domain.Connector) connectorDTO {
	config := connector.Config
	if config == nil {
		config = map[string]any{}
	}
	tags := connector.Tags
	if tags == nil {
		tags = []string{}
	}
	return connectorDTO{
		ID:          connector.ID,
		TenantID:    connector.TenantID,
		Name:        connector.Name,
		DisplayName: connector.DisplayName,
		Type:        string(connector.Type),
		Config:      config,
		Tags:        tags,
		Status:      string(connector.Status),
		CreatedBy:   connector.CreatedBy,
		CreatedAt:   connector.CreatedAt,
		UpdatedAt:   connector.UpdatedAt,
	}
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidTenant),
		errors.Is(err, service.ErrInvalidInput),
		errors.Is(err, domain.ErrInvalidName),
		errors.Is(err, domain.ErrInvalidType),
		errors.Is(err, domain.ErrInvalidStatus),
		errors.Is(err, domain.ErrInvalidConfig),
		errors.Is(err, domain.ErrSensitiveConfig),
		errors.Is(err, domain.ErrInvalidTags),
		errors.Is(err, domain.ErrInvalidCreatedBy):
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
	case errors.Is(err, domain.ErrNotFound):
		httperr.Write(w, r, httperr.NotFound(err.Error()))
	case errors.Is(err, domain.ErrConflict):
		httperr.Write(w, r, httperr.Conflict(err.Error()))
	case errors.Is(err, service.ErrUnavailable):
		httperr.Write(w, r, httperr.Unavailable(err.Error()))
	case errors.Is(err, service.ErrNotImplemented):
		httperr.Write(w, r, httperr.NotImplemented(err.Error()))
	default:
		httperr.Write(w, r, httperr.Internal())
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
