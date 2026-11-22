// Package api adapts the dataset catalog service to HTTP/JSON. It performs
// protocol concerns only: routing, decoding, tenant resolution and error
// mapping. All business rules live in service/.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/service"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

const tenantHeader = "X-Ontoaxis-Tenant-Id"

// TenantResolver extracts the authenticated tenant from a request. The default
// implementation trusts only the internal identity-edge header; deployments
// should inject a resolver backed by the real authentication context.
type TenantResolver interface {
	ResolveTenant(r *http.Request) (string, error)
}

// GatewayTenantResolver is the fail-closed default used until identity-edge
// integration provides an authenticated resolver.
type GatewayTenantResolver struct{}

func (GatewayTenantResolver) ResolveTenant(r *http.Request) (string, error) {
	tenantID := strings.TrimSpace(r.Header.Get(tenantHeader))
	if tenantID == "" {
		return "", domain.ErrTenantRequired
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return "", domain.ErrTenantRequired
	}
	return tenantID, nil
}

type Handler struct {
	service service.Service
	tenants TenantResolver
}

func NewHandler(svc service.Service, tenants TenantResolver) *Handler {
	if tenants == nil {
		tenants = GatewayTenantResolver{}
	}
	return &Handler{service: svc, tenants: tenants}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/datasets", h.listDatasets)
	r.Post("/api/v1/datasets", h.createDataset)

	r.Get("/api/v1/datasets/{datasetID}", h.getDataset)
	r.Patch("/api/v1/datasets/{datasetID}", h.updateDataset)
	r.Delete("/api/v1/datasets/{datasetID}", h.deleteDataset)

	r.Get("/api/v1/datasets/{datasetID}/branches", h.listBranches)
	r.Post("/api/v1/datasets/{datasetID}/branches", h.createBranch)
	r.Get("/api/v1/datasets/{datasetID}/branches/{branchID}", h.getBranch)

	r.Get("/api/v1/datasets/{datasetID}/transactions", h.listTransactions)
	r.Post("/api/v1/datasets/{datasetID}/transactions", h.beginTransaction)
	r.Get("/api/v1/datasets/{datasetID}/transactions/{transactionID}", h.getTransaction)
	r.Post("/api/v1/datasets/{datasetID}/transactions/{transactionID}/commit", h.commitTransaction)
	r.Post("/api/v1/datasets/{datasetID}/transactions/{transactionID}/abort", h.abortTransaction)

	r.Get("/api/v1/datasets/{datasetID}/views", h.listViews)
	r.Post("/api/v1/datasets/{datasetID}/views", h.createView)
	r.Get("/api/v1/datasets/{datasetID}/views/{viewID}", h.getView)
	r.Patch("/api/v1/datasets/{datasetID}/views/{viewID}", h.updateView)
	r.Delete("/api/v1/datasets/{datasetID}/views/{viewID}", h.deleteView)

	r.Get("/api/v1/datasets/{datasetID}/files", h.listFiles)
	r.Post("/api/v1/datasets/{datasetID}/files", h.createFile)
	r.Get("/api/v1/datasets/{datasetID}/files/{fileID}", h.getFile)
	r.Delete("/api/v1/datasets/{datasetID}/files/{fileID}", h.deleteFile)
}

func (h *Handler) resolveTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID, err := h.tenants.ResolveTenant(r)
	if err != nil || strings.TrimSpace(tenantID) == "" {
		httperr.Write(w, r, httperr.Unauthorized("tenant context is required"))
		return "", false
	}
	return tenantID, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	if r.Body == nil {
		return fmt.Errorf("%w: request body is required", domain.ErrInvalidArgument)
	}
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: malformed JSON body: %v", domain.ErrInvalidArgument, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: request body must contain a single JSON object", domain.ErrInvalidArgument)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		httperr.Write(w, r, httperr.NotFound("dataset catalog resource not found"))
	case errors.Is(err, domain.ErrInvalidArgument):
		httperr.Write(w, r, httperr.Error{Code: httperr.CodeInvalidArgument, Message: detailMessage(err, domain.ErrInvalidArgument)})
	case errors.Is(err, domain.ErrAlreadyExists):
		httperr.Write(w, r, httperr.Error{Code: httperr.CodeAlreadyExists, Message: detailMessage(err, domain.ErrAlreadyExists)})
	case errors.Is(err, domain.ErrInvalidState):
		httperr.Write(w, r, httperr.Conflict(detailMessage(err, domain.ErrInvalidState)))
	case errors.Is(err, domain.ErrTenantRequired):
		httperr.Write(w, r, httperr.Unauthorized("tenant context is required"))
	case errors.Is(err, domain.ErrUnavailable):
		httperr.Write(w, r, httperr.Unavailable("dataset catalog storage is unavailable"))
	default:
		httperr.Write(w, r, httperr.Internal())
	}
}

func detailMessage(err error, sentinel error) string {
	message := err.Error()
	prefix := sentinel.Error() + ": "
	if strings.HasPrefix(message, prefix) {
		return strings.TrimPrefix(message, prefix)
	}
	return message
}

func parsePage(r *http.Request) (int, int, error) {
	page := 0
	perPage := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: page must be an integer", domain.ErrInvalidArgument)
		}
		page = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("per_page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: per_page must be an integer", domain.ErrInvalidArgument)
		}
		perPage = parsed
	}
	return page, perPage, nil
}
