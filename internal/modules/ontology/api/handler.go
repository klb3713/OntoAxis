// Package api adapts the Ontology application service to the HTTP JSON
// protocol. It contains no business rules and never accesses the database.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ontology/service"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

// Handler serves the /api/v1/ontology routes.
type Handler struct {
	svc service.Service
}

// NewHandler creates an HTTP handler around svc. A nil service is allowed so
// the module can register fail-closed routes before its database pool exists.
func NewHandler(svc service.Service) *Handler {
	return &Handler{svc: svc}
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
	properties, err := h.svc.ListProperties(r.Context(), tenantID, chi.URLParam(r, "objectTypeId"))
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
	objectType, err := h.svc.BindDataset(r.Context(), tenantID, service.BindDatasetInput{
		ObjectTypeID: chi.URLParam(r, "objectTypeId"),
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
	objectType, err := h.svc.UnbindDataset(r.Context(), tenantID, chi.URLParam(r, "objectTypeId"))
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
	operationID, err := h.svc.MaterializeBinding(r.Context(), tenantID, chi.URLParam(r, "objectTypeId"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, materializeBindingDTO{OperationID: operationID})
}
