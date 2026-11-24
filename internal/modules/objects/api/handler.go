package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	"github.com/klb3713/ontoaxis/internal/modules/objects/service"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

const maxBody = 1 << 20

// Handler serves the objects HTTP routes. Authorization resources are loaded
// through the optional readers; they are required once a guard is configured.
type Handler struct {
	svc service.Service

	guard       authz.DecisionGuard
	objectTypes ObjectTypeReader
	links       LinkReader

	search        SearchBackend
	searchGuard   authz.DecisionGuard
	searchEnabled bool
}

// Option configures optional Handler collaborators.
type Option func(*Handler)

// WithDecisionGuard installs the platform authorization guard. A nil guard
// keeps the legacy behavior and performs no authorization decisions.
func WithDecisionGuard(guard authz.DecisionGuard) Option {
	return func(h *Handler) { h.guard = guard }
}

// WithObjectTypeReader installs the tenant-scoped object type loader used to
// authorize object creation and object type-filtered lists and searches.
func WithObjectTypeReader(reader ObjectTypeReader) Option {
	return func(h *Handler) { h.objectTypes = reader }
}

// WithLinkReader installs the tenant-scoped link loader used to authorize link
// deletion against the link's source object.
func WithLinkReader(reader LinkReader) Option {
	return func(h *Handler) { h.links = reader }
}

// NewHandler builds an objects HTTP handler.
func NewHandler(svc service.Service, opts ...Option) *Handler {
	h := &Handler{svc: svc}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

func (h *Handler) Register(r chi.Router) {
	r.Post("/", h.createObject)
	r.Get("/", h.listObjects)
	if h.searchEnabled {
		// Static route must be registered before the /{id} wildcard.
		r.Get("/search", h.searchObjects)
	} else {
		// With search disabled the endpoint is not available. /{id} would
		// otherwise match "search" and answer 400 (invalid UUID); keep the
		// unregistered-route contract instead (404).
		r.Get("/search", http.NotFound)
	}
	r.Get("/{id}", h.getObject)
	r.Patch("/{id}", h.updateObject)
	r.Delete("/{id}", h.deleteObject)
	r.Post("/links", h.createLink)
	r.Get("/{id}/links", h.listLinks)
	r.Delete("/links/{id}", h.deleteLink)
}

type objectRequest struct {
	ObjectTypeID string         `json:"object_type_id"`
	Properties   map[string]any `json:"properties"`
	Replace      bool           `json:"replace"`
}

type linkRequest struct {
	LinkTypeID string         `json:"link_type_id"`
	SourceID   string         `json:"source_id"`
	TargetID   string         `json:"target_id"`
	Properties map[string]any `json:"properties"`
}

type objectResponse struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	ObjectTypeID string         `json:"object_type_id"`
	Version      int64          `json:"version"`
	Properties   map[string]any `json:"properties"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

type linkResponse struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	LinkTypeID string         `json:"link_type_id"`
	SourceID   string         `json:"source_id"`
	TargetID   string         `json:"target_id"`
	Properties map[string]any `json:"properties,omitempty"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
}

type listResponse[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
}

func (h *Handler) createObject(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	var req objectRequest
	if !decode(w, r, &req) {
		return
	}
	objectTypeID, err := uuid.Parse(req.ObjectTypeID)
	if err != nil {
		writeError(w, r, httperr.BadRequest("object_type_id must be a UUID"))
		return
	}
	if !h.authorizeObjectType(w, r, tenantID, objectTypeID, "object:create") {
		return
	}
	obj, err := h.svc.CreateObject(r.Context(), tenantID, objectTypeID, req.Properties)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toObjectResponse(obj))
}

func (h *Handler) listObjects(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	var objectTypeID uuid.UUID
	if raw := r.URL.Query().Get("object_type_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, r, httperr.BadRequest("object_type_id must be a UUID"))
			return
		}
		objectTypeID = id
	}
	if objectTypeID != uuid.Nil {
		if !h.authorizeObjectType(w, r, tenantID, objectTypeID, "object:read") {
			return
		}
	} else if !h.authorizeTenant(w, r, tenantID, "object:read") {
		return
	}
	limit, offset := page(r)
	items, total, err := h.svc.ListObjects(r.Context(), tenantID, objectTypeID, limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	out := make([]objectResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toObjectResponse(item))
	}
	writeJSON(w, http.StatusOK, listResponse[objectResponse]{Items: out, Total: total})
}

func (h *Handler) getObject(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	obj, err := h.svc.GetObject(r.Context(), tenantID, id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if h.guard != nil && obj.TenantID != tenantID {
		writeError(w, r, httperr.NotFound("object not found"))
		return
	}
	if !h.authorize(w, r, "object:read", objectFact(obj)) {
		return
	}
	writeJSON(w, http.StatusOK, toObjectResponse(obj))
}

func (h *Handler) updateObject(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	var req objectRequest
	if !decode(w, r, &req) {
		return
	}
	if h.guard != nil {
		current, ok := h.loadObjectForAuthorization(w, r, tenantID, id)
		if !ok {
			return
		}
		if !h.authorize(w, r, "object:update", objectFact(current)) {
			return
		}
	}
	obj, err := h.svc.UpdateObject(r.Context(), tenantID, id, req.Properties, req.Replace)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toObjectResponse(obj))
}

func (h *Handler) deleteObject(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	if h.guard != nil {
		current, ok := h.loadObjectForAuthorization(w, r, tenantID, id)
		if !ok {
			return
		}
		if !h.authorize(w, r, "object:delete", objectFact(current)) {
			return
		}
	}
	if err := h.svc.DeleteObject(r.Context(), tenantID, id); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createLink(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	var req linkRequest
	if !decode(w, r, &req) {
		return
	}
	linkTypeID, err := uuid.Parse(req.LinkTypeID)
	if err != nil {
		writeError(w, r, httperr.BadRequest("link_type_id must be a UUID"))
		return
	}
	sourceID, err := uuid.Parse(req.SourceID)
	if err != nil {
		writeError(w, r, httperr.BadRequest("source_id must be a UUID"))
		return
	}
	targetID, err := uuid.Parse(req.TargetID)
	if err != nil {
		writeError(w, r, httperr.BadRequest("target_id must be a UUID"))
		return
	}
	if !h.authorizeLinkEndpoints(w, r, tenantID, sourceID, targetID, "object_link:create") {
		return
	}
	link, err := h.svc.CreateLink(r.Context(), tenantID, linkTypeID, sourceID, targetID, req.Properties)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toLinkResponse(link))
}

func (h *Handler) listLinks(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	if h.guard != nil {
		source, ok := h.loadObjectForAuthorization(w, r, tenantID, id)
		if !ok {
			return
		}
		if !h.authorize(w, r, "object_link:read", objectFact(source)) {
			return
		}
	}
	limit, offset := page(r)
	items, total, err := h.svc.ListLinks(r.Context(), tenantID, id, limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	out := make([]linkResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toLinkResponse(item))
	}
	writeJSON(w, http.StatusOK, listResponse[linkResponse]{Items: out, Total: total})
}

func (h *Handler) deleteLink(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	if !h.authorizeLink(w, r, tenantID, id, "object_link:delete") {
		return
	}
	if err := h.svc.DeleteLink(r.Context(), tenantID, id); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, r, httperr.NotFound("object not found"))
	case errors.Is(err, domain.ErrConflict):
		writeError(w, r, httperr.Conflict("object link already exists"))
	case errors.Is(err, domain.ErrInvalidInput):
		writeError(w, r, httperr.BadRequest("invalid object input"))
	default:
		writeError(w, r, httperr.Internal())
	}
}

func tenant(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if tc, ok := authn.FromContext(r.Context()); ok && tc.TenantID != uuid.Nil {
		return tc.TenantID, true
	}
	raw := r.Header.Get("X-Tenant-Id")
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		writeError(w, r, httperr.Unauthorized("tenant context is required"))
		return uuid.Nil, false
	}
	return id, true
}

func page(r *http.Request) (int, int) {
	limit := 50
	offset := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeError(w, r, httperr.BadRequest("request body is required"))
		return false
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, r, httperr.BadRequest("invalid JSON request body"))
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, r, httperr.BadRequest("request body must contain a single JSON object"))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, e httperr.Error) {
	httperr.Write(w, r, e)
}

func toObjectResponse(o domain.Object) objectResponse {
	return objectResponse{
		ID: o.ID.String(), TenantID: o.TenantID.String(), ObjectTypeID: o.ObjectTypeID.String(),
		Version: o.Version, Properties: o.Properties,
		CreatedAt: o.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: o.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toLinkResponse(l domain.Link) linkResponse {
	return linkResponse{
		ID: l.ID.String(), TenantID: l.TenantID.String(), LinkTypeID: l.LinkTypeID.String(),
		SourceID: l.SourceID.String(), TargetID: l.TargetID.String(), Properties: l.Properties,
		CreatedAt: l.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: l.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}
