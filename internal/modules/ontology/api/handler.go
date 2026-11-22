// Package api adapts the Ontology application service to the HTTP JSON
// protocol. It contains no business rules and never accesses the database.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ontology/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

// Handler serves the /api/v1/ontology routes.
type Handler struct {
	svc      service.Service
	guard    authz.DecisionGuard
	datasets DatasetReader
}

// DatasetFact is the tenant-scoped fact needed to reject a cross-tenant
// dataset binding before it is written.
type DatasetFact struct {
	ID       string
	TenantID string
}

// ErrDatasetNotFound lets the app adapter map a missing dataset to 404.
var ErrDatasetNotFound = errors.New("dataset not found")

// DatasetReader loads a dataset tenant-scoped for binding validation.
type DatasetReader interface {
	GetDatasetFact(ctx context.Context, tenantID, datasetID string) (DatasetFact, error)
}

// Option configures optional handler dependencies. Options are additive so
// existing construction sites keep compiling unchanged.
type Option func(*Handler)

// WithDecisionGuard injects the authorization decision guard used at every
// ontology seam. A nil guard preserves the legacy allow-all behavior from
// before the authorization boundary was wired.
func WithDecisionGuard(guard authz.DecisionGuard) Option {
	return func(h *Handler) { h.guard = guard }
}

// WithDatasetReader installs the tenant-scoped dataset loader used to validate
// dataset bindings. It is required whenever a DecisionGuard is configured.
func WithDatasetReader(reader DatasetReader) Option {
	return func(h *Handler) { h.datasets = reader }
}

// NewHandler creates an HTTP handler around svc. A nil service is allowed so
// the module can register fail-closed routes before its database pool exists.
func NewHandler(svc service.Service, opts ...Option) *Handler {
	h := &Handler{svc: svc}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// authorize runs the single authorization decision point for a route. A
// configured guard fails closed; a nil guard keeps legacy behavior.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, action authz.Action, resource authz.ResourceFact) bool {
	return authz.AuthorizeHTTP(w, r, h.guard, action, resource)
}

func tenantFact(tenantID string) authz.ResourceFact {
	return authz.ResourceFact{Type: "tenant", ID: tenantID, TenantID: tenantID}
}

func objectTypeFact(objectType *domain.ObjectType) authz.ResourceFact {
	return authz.ResourceFact{
		Type:     "object_type",
		ID:       objectType.ID,
		TenantID: objectType.TenantID,
		Status:   string(objectType.Status),
	}
}

// Register mounts all Ontology routes below /api/v1/ontology.
func (h *Handler) Register(r chi.Router) {
	r.Route("/api/v1/ontology", func(r chi.Router) {
		r.Post("/object-types", h.createObjectType)
		r.Get("/object-types", h.listObjectTypes)
		r.Get("/object-types/{id}", h.getObjectType)
		r.Put("/object-types/{id}", h.updateObjectType)
		r.Delete("/object-types/{id}", h.deleteObjectType)

		r.Post("/properties", h.createProperty)
		r.Get("/object-types/{objectTypeId}/properties", h.listProperties)
		r.Put("/properties/{id}", h.updateProperty)
		r.Delete("/properties/{id}", h.deleteProperty)

		r.Post("/link-types", h.createLinkType)
		r.Get("/link-types", h.listLinkTypes)
		r.Put("/link-types/{id}", h.updateLinkType)
		r.Delete("/link-types/{id}", h.deleteLinkType)

		r.Post("/object-types/{objectTypeId}/dataset-binding", h.bindDataset)
		r.Delete("/object-types/{objectTypeId}/dataset-binding", h.unbindDataset)
		r.Post("/object-types/{objectTypeId}/dataset-binding/materialize", h.materializeBinding)
	})
}

type createObjectTypeRequest struct {
	Name               string `json:"name"`
	DisplayName        string `json:"display_name"`
	Description        string `json:"description"`
	PrimaryKeyProperty string `json:"primary_key_property"`
}

type updateObjectTypeRequest struct {
	DisplayName *string `json:"display_name"`
	Description *string `json:"description"`
}

type createPropertyRequest struct {
	ObjectTypeID     string `json:"object_type_id"`
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	Description      string `json:"description"`
	PropertyType     string `json:"property_type"`
	Required         bool   `json:"required"`
	UniqueConstraint bool   `json:"unique_constraint"`
}

type updatePropertyRequest struct {
	DisplayName      *string `json:"display_name"`
	Description      *string `json:"description"`
	Required         *bool   `json:"required"`
	UniqueConstraint *bool   `json:"unique_constraint"`
}

type createLinkTypeRequest struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	Description  string `json:"description"`
	SourceTypeID string `json:"source_type_id"`
	TargetTypeID string `json:"target_type_id"`
	Cardinality  string `json:"cardinality"`
}

type updateLinkTypeRequest struct {
	DisplayName *string `json:"display_name"`
	Description *string `json:"description"`
}

type bindDatasetRequest struct {
	DatasetID string `json:"dataset_id"`
	SyncMode  string `json:"sync_mode"`
}

func decodeJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain exactly one JSON object")
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
	case errors.Is(err, domain.ErrInvalidArgument):
		httperr.Write(w, r, httperr.Error{Code: httperr.CodeInvalidArgument, Message: err.Error()})
	case errors.Is(err, domain.ErrNotFound):
		httperr.Write(w, r, httperr.NotFound(err.Error()))
	case errors.Is(err, domain.ErrAlreadyExists):
		httperr.Write(w, r, httperr.Error{Code: httperr.CodeAlreadyExists, Message: err.Error()})
	case errors.Is(err, domain.ErrFailedPrecondition):
		httperr.Write(w, r, httperr.Conflict(err.Error()))
	default:
		httperr.Write(w, r, httperr.Internal())
	}
}

func (h *Handler) requireService(w http.ResponseWriter, r *http.Request) bool {
	if h.svc == nil {
		httperr.Write(w, r, httperr.Unavailable("ontology service is not ready"))
		return false
	}
	return true
}

func requireTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID, ok := TenantIDFromContext(r.Context())
	if !ok {
		httperr.Write(w, r, httperr.Unauthorized("tenant context is required"))
		return "", false
	}
	return tenantID, true
}

func parsePage(r *http.Request) (domain.Page, error) {
	page, err := positiveQuery(r, "page", domain.DefaultPage)
	if err != nil {
		return domain.Page{}, err
	}
	perPage, err := positiveQuery(r, "per_page", domain.DefaultPerPage)
	if err != nil {
		return domain.Page{}, err
	}
	return domain.NormalizePage(page, perPage)
}

func positiveQuery(r *http.Request, key string, fallback int) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, errors.New("invalid pagination: " + key + " must be a positive integer")
	}
	return value, nil
}

func (h *Handler) createObjectType(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	var req createObjectTypeRequest
	if err := decodeJSON(r, &req); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid request body: "+err.Error()))
		return
	}
	if !h.authorize(w, r, "ontology:object_type:create", tenantFact(tenantID)) {
		return
	}
	objectType, err := h.svc.CreateObjectType(r.Context(), tenantID, service.CreateObjectTypeInput{
		Name:               req.Name,
		DisplayName:        req.DisplayName,
		Description:        req.Description,
		PrimaryKeyProperty: req.PrimaryKeyProperty,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toObjectTypeDTO(*objectType))
}

func (h *Handler) getObjectType(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	objectType, err := h.svc.GetObjectType(r.Context(), tenantID, chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "ontology:object_type:read", objectTypeFact(objectType)) {
		return
	}
	writeJSON(w, http.StatusOK, toObjectTypeDTO(*objectType))
}

func (h *Handler) listObjectTypes(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	page, err := parsePage(r)
	if err != nil {
		httperr.Write(w, r, httperr.Error{Code: httperr.CodeInvalidArgument, Message: err.Error()})
		return
	}
	if !h.authorize(w, r, "ontology:object_type:read", tenantFact(tenantID)) {
		return
	}
	result, err := h.svc.ListObjectTypes(r.Context(), tenantID, page)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]objectTypeDTO, 0, len(result.Items))
	for _, objectType := range result.Items {
		items = append(items, toObjectTypeDTO(objectType))
	}
	writeJSON(w, http.StatusOK, objectTypeListDTO{
		Items:   items,
		Total:   result.Total,
		Page:    result.Page,
		PerPage: result.PerPage,
	})
}

func (h *Handler) updateObjectType(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	var req updateObjectTypeRequest
	if err := decodeJSON(r, &req); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid request body: "+err.Error()))
		return
	}
	current, err := h.svc.GetObjectType(r.Context(), tenantID, chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "ontology:object_type:update", objectTypeFact(current)) {
		return
	}
	objectType, err := h.svc.UpdateObjectType(r.Context(), tenantID, chi.URLParam(r, "id"), service.UpdateObjectTypeInput{
		DisplayName: req.DisplayName,
		Description: req.Description,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toObjectTypeDTO(*objectType))
}

func (h *Handler) deleteObjectType(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	current, err := h.svc.GetObjectType(r.Context(), tenantID, chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "ontology:object_type:delete", objectTypeFact(current)) {
		return
	}
	if err := h.svc.DeleteObjectType(r.Context(), tenantID, chi.URLParam(r, "id")); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createProperty(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	var req createPropertyRequest
	if err := decodeJSON(r, &req); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid request body: "+err.Error()))
		return
	}
	objectType, err := h.svc.GetObjectType(r.Context(), tenantID, req.ObjectTypeID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "ontology:property:create", objectTypeFact(objectType)) {
		return
	}
	property, err := h.svc.CreateProperty(r.Context(), tenantID, service.CreatePropertyInput{
		ObjectTypeID:     req.ObjectTypeID,
		Name:             req.Name,
		DisplayName:      req.DisplayName,
		Description:      req.Description,
		PropertyType:     req.PropertyType,
		Required:         req.Required,
		UniqueConstraint: req.UniqueConstraint,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toPropertyDTO(*property))
}

func (h *Handler) listProperties(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	objectTypeID := chi.URLParam(r, "objectTypeId")
	objectType, err := h.svc.GetObjectType(r.Context(), tenantID, objectTypeID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "ontology:property:read", objectTypeFact(objectType)) {
		return
	}
	properties, err := h.svc.ListProperties(r.Context(), tenantID, objectTypeID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]propertyDTO, 0, len(properties))
	for _, property := range properties {
		items = append(items, toPropertyDTO(property))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) updateProperty(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	var req updatePropertyRequest
	if err := decodeJSON(r, &req); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid request body: "+err.Error()))
		return
	}
	// The service exposes no property getter, so the parent ObjectType cannot be
	// loaded from the property ID without changing service semantics. Authorize
	// on the tenant fact instead; the repository update stays tenant-scoped.
	if !h.authorize(w, r, "ontology:property:update", tenantFact(tenantID)) {
		return
	}
	property, err := h.svc.UpdateProperty(r.Context(), tenantID, chi.URLParam(r, "id"), service.UpdatePropertyInput{
		DisplayName:      req.DisplayName,
		Description:      req.Description,
		Required:         req.Required,
		UniqueConstraint: req.UniqueConstraint,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPropertyDTO(*property))
}

func (h *Handler) deleteProperty(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	// The service exposes no property getter, so the parent ObjectType cannot be
	// loaded from the property ID without changing service semantics. Authorize
	// on the tenant fact instead; the repository delete stays tenant-scoped.
	if !h.authorize(w, r, "ontology:property:delete", tenantFact(tenantID)) {
		return
	}
	if err := h.svc.DeleteProperty(r.Context(), tenantID, chi.URLParam(r, "id")); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createLinkType(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	var req createLinkTypeRequest
	if err := decodeJSON(r, &req); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid request body: "+err.Error()))
		return
	}
	// Link types hang off their source object type; authorize against the loaded
	// parent before the relation is created.
	sourceObjectType, err := h.svc.GetObjectType(r.Context(), tenantID, req.SourceTypeID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "ontology:link_type:create", objectTypeFact(sourceObjectType)) {
		return
	}
	linkType, err := h.svc.CreateLinkType(r.Context(), tenantID, service.CreateLinkTypeInput{
		Name:         req.Name,
		DisplayName:  req.DisplayName,
		Description:  req.Description,
		SourceTypeID: req.SourceTypeID,
		TargetTypeID: req.TargetTypeID,
		Cardinality:  domain.Cardinality(req.Cardinality),
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toLinkTypeDTO(*linkType))
}

func (h *Handler) listLinkTypes(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	page, err := parsePage(r)
	if err != nil {
		httperr.Write(w, r, httperr.Error{Code: httperr.CodeInvalidArgument, Message: err.Error()})
		return
	}
	// The link-type list is tenant-wide and unfiltered, so it authorizes against
	// the tenant fact per the boundary's tenant-level list rule.
	if !h.authorize(w, r, "ontology:link_type:read", tenantFact(tenantID)) {
		return
	}
	result, err := h.svc.ListLinkTypes(r.Context(), tenantID, page)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]linkTypeDTO, 0, len(result.Items))
	for _, linkType := range result.Items {
		items = append(items, toLinkTypeDTO(linkType))
	}
	writeJSON(w, http.StatusOK, linkTypeListDTO{
		Items:   items,
		Total:   result.Total,
		Page:    result.Page,
		PerPage: result.PerPage,
	})
}

func (h *Handler) updateLinkType(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	var req updateLinkTypeRequest
	if err := decodeJSON(r, &req); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid request body: "+err.Error()))
		return
	}
	// The service exposes no link-type getter, so the parent ObjectType cannot be
	// loaded from the link type ID without changing service semantics. Authorize
	// on the tenant fact instead; the repository update stays tenant-scoped.
	if !h.authorize(w, r, "ontology:link_type:update", tenantFact(tenantID)) {
		return
	}
	linkType, err := h.svc.UpdateLinkType(r.Context(), tenantID, chi.URLParam(r, "id"), service.UpdateLinkTypeInput{
		DisplayName: req.DisplayName,
		Description: req.Description,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toLinkTypeDTO(*linkType))
}

func (h *Handler) deleteLinkType(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	// The service exposes no link-type getter, so the parent ObjectType cannot be
	// loaded from the link type ID without changing service semantics. Authorize
	// on the tenant fact instead; the repository delete stays tenant-scoped.
	if !h.authorize(w, r, "ontology:link_type:delete", tenantFact(tenantID)) {
		return
	}
	if err := h.svc.DeleteLinkType(r.Context(), tenantID, chi.URLParam(r, "id")); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) bindDataset(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	var req bindDatasetRequest
	if err := decodeJSON(r, &req); err != nil {
		httperr.Write(w, r, httperr.BadRequest("invalid request body: "+err.Error()))
		return
	}
	objectTypeID := chi.URLParam(r, "objectTypeId")
	objectType, err := h.svc.GetObjectType(r.Context(), tenantID, objectTypeID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if h.guard != nil && req.DatasetID != "" {
		if h.datasets == nil {
			httperr.Write(w, r, httperr.Unavailable("authorization dataset reader is not configured"))
			return
		}
		if _, err := h.datasets.GetDatasetFact(r.Context(), tenantID, req.DatasetID); err != nil {
			if errors.Is(err, ErrDatasetNotFound) {
				httperr.Write(w, r, httperr.NotFound("dataset not found"))
				return
			}
			httperr.Write(w, r, httperr.Unavailable("authorization dataset lookup failed"))
			return
		}
	}
	if !h.authorize(w, r, "ontology:binding:write", objectTypeFact(objectType)) {
		return
	}
	objectType, err = h.svc.BindDataset(r.Context(), tenantID, service.BindDatasetInput{
		ObjectTypeID: objectTypeID,
		DatasetID:    req.DatasetID,
		SyncMode:     req.SyncMode,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toObjectTypeDTO(*objectType))
}

func (h *Handler) unbindDataset(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	objectTypeID := chi.URLParam(r, "objectTypeId")
	loaded, err := h.svc.GetObjectType(r.Context(), tenantID, objectTypeID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "ontology:binding:write", objectTypeFact(loaded)) {
		return
	}
	objectType, err := h.svc.UnbindDataset(r.Context(), tenantID, objectTypeID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toObjectTypeDTO(*objectType))
}

func (h *Handler) materializeBinding(w http.ResponseWriter, r *http.Request) {
	if !h.requireService(w, r) {
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	objectTypeID := chi.URLParam(r, "objectTypeId")
	loaded, err := h.svc.GetObjectType(r.Context(), tenantID, objectTypeID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "ontology:binding:materialize", objectTypeFact(loaded)) {
		return
	}
	operationID, err := h.svc.MaterializeBinding(r.Context(), tenantID, objectTypeID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, materializeBindingDTO{OperationID: operationID})
}
