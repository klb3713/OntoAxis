package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
)

func (h *Handler) createDataset(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	var request createDatasetRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "dataset:create", authz.ResourceFact{Type: "tenant", ID: tenantID, TenantID: tenantID}) {
		return
	}
	result, err := h.service.CreateDataset(r.Context(), tenantID, service.CreateDatasetInput{
		Name:            request.Name,
		DisplayName:     request.DisplayName,
		Description:     request.Description,
		Format:          request.Format,
		StorageLocation: request.StorageLocation,
		OwnerID:         request.OwnerID,
		Tags:            request.Tags,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newDatasetResponse(result.Dataset))
}

func (h *Handler) getDataset(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	dataset, err := h.service.GetDataset(r.Context(), tenantID, chi.URLParam(r, "datasetID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "dataset:read", datasetFact(dataset)) {
		return
	}
	writeJSON(w, http.StatusOK, newDatasetResponse(dataset))
}

func (h *Handler) listDatasets(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	page, perPage, err := parsePage(r)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "dataset:read", authz.ResourceFact{Type: "tenant", ID: tenantID, TenantID: tenantID}) {
		return
	}
	result, err := h.service.ListDatasets(r.Context(), tenantID, page, perPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, datasetList(result.Items, result.Page))
}

func (h *Handler) updateDataset(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	current, err := h.service.GetDataset(r.Context(), tenantID, chi.URLParam(r, "datasetID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "dataset:update", datasetFact(current)) {
		return
	}
	var request updateDatasetRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeServiceError(w, r, err)
		return
	}
	dataset, err := h.service.UpdateDataset(r.Context(), tenantID, chi.URLParam(r, "datasetID"), service.UpdateDatasetInput{
		DisplayName: request.DisplayName,
		Description: request.Description,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newDatasetResponse(dataset))
}

func (h *Handler) deleteDataset(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	current, err := h.service.GetDataset(r.Context(), tenantID, chi.URLParam(r, "datasetID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !h.authorize(w, r, "dataset:delete", datasetFact(current)) {
		return
	}
	if err := h.service.DeleteDataset(r.Context(), tenantID, chi.URLParam(r, "datasetID")); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func datasetFact(dataset domain.Dataset) authz.ResourceFact {
	return authz.ResourceFact{
		Type:     "dataset",
		ID:       dataset.ID,
		TenantID: dataset.TenantID,
		OwnerID:  dataset.OwnerID,
	}
}
