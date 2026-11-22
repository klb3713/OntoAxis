package api

import (
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
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

const maxBody = 1 << 20

type Handler struct{ service service.Service }

func NewHandler(svc service.Service) *Handler { return &Handler{service: svc} }

func (h *Handler) Register(r chi.Router) {
	r.Post("/execute", h.execute)
	r.Post("/explain", h.explain)
	r.Get("/saved-queries", h.listSavedQueries)
	r.Post("/saved-queries", h.saveQuery)
	r.Delete("/saved-queries/{id}", h.deleteSavedQuery)
	r.Get("/objects", h.queryObjects)
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	if _, ok := tenant(w, r); !ok {
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
	result, err := h.service.Execute(r.Context(), body.SQL, body.Limit)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) explain(w http.ResponseWriter, r *http.Request) {
	if _, ok := tenant(w, r); !ok {
		return
	}
	var body struct {
		SQL string `json:"sql"`
	}
	if !decode(w, r, &body) {
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
