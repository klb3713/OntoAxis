package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/query/domain"
	"github.com/klb3713/ontoaxis/internal/modules/query/service"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

const maxBody = 1 << 20

// Canonical platform authorization actions for the query module.
const (
	queryExecute     authz.Action = "query:execute"
	queryExplain     authz.Action = "query:explain"
	savedQueryCreate authz.Action = "saved_query:create"
	savedQueryRead   authz.Action = "saved_query:read"
	savedQueryDelete authz.Action = "saved_query:delete"
	objectRead       authz.Action = "object:read"
)

type Handler struct {
	service service.Service
	guard   authz.DecisionGuard
}

// Option customizes a Handler without changing existing callers.
type Option func(*Handler)

// WithDecisionGuard installs the authorization seam. A nil guard keeps the
// legacy behaviour (authorization disabled) until integration wiring passes one.
func WithDecisionGuard(guard authz.DecisionGuard) Option {
	return func(h *Handler) { h.guard = guard }
}

func NewHandler(svc service.Service, opts ...Option) *Handler {
	h := &Handler{service: svc}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// authorize evaluates the decision for one resource fact. A nil guard allows the
// request, which keeps pre-authorization callers working.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, action authz.Action, resource authz.ResourceFact) bool {
	return authz.AuthorizeHTTP(w, r, h.guard, action, resource)
}

func tenantFact(tenantID uuid.UUID) authz.ResourceFact {
	return authz.ResourceFact{Type: "tenant", ID: tenantID.String(), TenantID: tenantID.String()}
}

func savedQueryFact(item domain.SavedQuery) authz.ResourceFact {
	fact := authz.ResourceFact{
		Type:     "saved_query",
		ID:       item.ID.String(),
		TenantID: item.TenantID.String(),
	}
	if item.OwnerID != uuid.Nil {
		fact.OwnerID = item.OwnerID.String()
	}
	return fact
}

// savedQueryScanPage bounds the tenant-scoped page scan used to load one saved
// query for the delete decision. The query service interface exposes no
// single-row getter, so the row is located through the tenant-filtered list.
const savedQueryScanPage = 200

func (h *Handler) loadSavedQuery(ctx context.Context, tenantID, id uuid.UUID) (domain.SavedQuery, error) {
	for offset := 0; ; offset += savedQueryScanPage {
		rows, total, err := h.service.ListSavedQueries(ctx, tenantID, savedQueryScanPage, offset)
		if err != nil {
			return domain.SavedQuery{}, err
		}
		for _, row := range rows {
			if row.ID == id {
				if row.TenantID != tenantID {
					return domain.SavedQuery{}, domain.ErrNotFound
				}
				return row, nil
			}
		}
		if len(rows) == 0 || int64(offset+len(rows)) >= total {
			return domain.SavedQuery{}, domain.ErrNotFound
		}
	}
}

func (h *Handler) Register(r chi.Router) {
	r.Post("/execute", h.execute)
	r.Post("/explain", h.explain)
	r.Get("/saved-queries", h.listSavedQueries)
	r.Post("/saved-queries", h.saveQuery)
	r.Delete("/saved-queries/{id}", h.deleteSavedQuery)
	r.Get("/objects", h.queryObjects)
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	tc, ok := tenant(w, r)
	if !ok {
		return
	}
	var body struct {
		SQL   string `json:"sql"`
		Limit int    `json:"limit"`
	}
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.SQL) == "" {
		httperr.Write(w, r, httperr.BadRequest("sql is required"))
		return
	}
	if !h.authorize(w, r, queryExecute, tenantFact(tc.TenantID)) {
		return
	}
	result, err := h.service.Execute(r.Context(), body.SQL, body.Limit)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) explain(w http.ResponseWriter, r *http.Request) {
	tc, ok := tenant(w, r)
	if !ok {
		return
	}
	var body struct {
		SQL string `json:"sql"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !h.authorize(w, r, queryExplain, tenantFact(tc.TenantID)) {
		return
	}
	plan, err := h.service.Explain(r.Context(), body.SQL)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"plan": plan})
}

func (h *Handler) listSavedQueries(w http.ResponseWriter, r *http.Request) {
	tc, ok := tenant(w, r)
	if !ok {
		return
	}
	page, perPage := pagination(r)
	if !h.authorize(w, r, savedQueryRead, tenantFact(tc.TenantID)) {
		return
	}
	items, total, err := h.service.ListSavedQueries(r.Context(), tc.TenantID, perPage, (page-1)*perPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "total": total, "page": page, "per_page": perPage})
}

func (h *Handler) saveQuery(w http.ResponseWriter, r *http.Request) {
	tc, ok := tenant(w, r)
	if !ok {
		return
	}
	if tc.UserID == uuid.Nil {
		httperr.Write(w, r, httperr.Unauthorized("authenticated user is required"))
		return
	}
	var body struct {
		Name string `json:"name"`
		SQL  string `json:"sql"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !h.authorize(w, r, savedQueryCreate, tenantFact(tc.TenantID)) {
		return
	}
	item, err := h.service.SaveQuery(r.Context(), tc.TenantID, tc.UserID, body.Name, body.SQL)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) deleteSavedQuery(w http.ResponseWriter, r *http.Request) {
	tc, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid saved query id"))
		return
	}
	saved, err := h.loadSavedQuery(r.Context(), tc.TenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, savedQueryDelete, savedQueryFact(saved)) {
		return
	}
	if err := h.service.DeleteSavedQuery(r.Context(), tc.TenantID, id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func tenant(w http.ResponseWriter, r *http.Request) (authn.TenantContext, bool) {
	tc, err := authn.RequireTenant(r.Context())
	if err == nil && tc.TenantID != uuid.Nil {
		return tc, true
	}
	if header := strings.TrimSpace(r.Header.Get("X-Tenant-Id")); header != "" {
		if id, parseErr := uuid.Parse(header); parseErr == nil {
			return authn.TenantContext{TenantID: id}, true
		}
	}
	httperr.Write(w, r, httperr.Unauthorized("authenticated tenant is required"))
	return authn.TenantContext{}, false
}

func pagination(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}
	return page, perPage
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid JSON body"))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		httperr.Write(w, r, httperr.BadRequest(err.Error()))
	case errors.Is(err, domain.ErrNotFound):
		httperr.Write(w, r, httperr.NotFound("saved query not found"))
	case errors.Is(err, domain.ErrConflict):
		httperr.Write(w, r, httperr.Conflict("saved query name already exists"))
	case errors.Is(err, domain.ErrUnavailable):
		httperr.Write(w, r, httperr.Unavailable("query engine is not configured"))
	default:
		httperr.Write(w, r, httperr.Internal())
	}
}
