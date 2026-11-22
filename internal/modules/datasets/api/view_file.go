package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/service"
)

func (h *Handler) createView(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	var request createViewRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeServiceError(w, r, err)
		return
	}
	view, err := h.service.CreateView(r.Context(), tenantID, chi.URLParam(r, "datasetID"), service.CreateViewInput{
		BranchID: request.BranchID,
		Name:     request.Name,
		Query:    request.Query,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newViewResponse(view))
}

func (h *Handler) getView(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	view, err := h.service.GetView(r.Context(), tenantID, chi.URLParam(r, "datasetID"), chi.URLParam(r, "viewID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newViewResponse(view))
}

func (h *Handler) listViews(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	page, perPage, err := parsePage(r)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	result, err := h.service.ListViews(r.Context(), tenantID, chi.URLParam(r, "datasetID"), page, perPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]viewResponse, 0, len(result.Items))
	for _, view := range result.Items {
		items = append(items, newViewResponse(view))
	}
	writeJSON(w, http.StatusOK, viewListResponse{
		Items:      items,
		Page:       result.Page.Page,
		PerPage:    result.Page.PerPage,
		Total:      result.Page.Total,
		TotalPages: result.Page.TotalPages,
	})
}

func (h *Handler) updateView(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	var request updateViewRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeServiceError(w, r, err)
		return
	}
	view, err := h.service.UpdateView(r.Context(), tenantID, chi.URLParam(r, "datasetID"), chi.URLParam(r, "viewID"), service.UpdateViewInput{
		Query: request.Query,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newViewResponse(view))
}

func (h *Handler) deleteView(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteView(r.Context(), tenantID, chi.URLParam(r, "datasetID"), chi.URLParam(r, "viewID")); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createFile(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	var request createFileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeServiceError(w, r, err)
		return
	}
	file, err := h.service.CreateFile(r.Context(), tenantID, chi.URLParam(r, "datasetID"), service.CreateFileInput{
		BranchID:    request.BranchID,
		ObjectKey:   request.ObjectKey,
		SizeBytes:   request.SizeBytes,
		RowCount:    request.RowCount,
		ETag:        request.ETag,
		ContentType: request.ContentType,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newFileResponse(file))
}

func (h *Handler) getFile(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	file, err := h.service.GetFile(r.Context(), tenantID, chi.URLParam(r, "datasetID"), chi.URLParam(r, "fileID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newFileResponse(file))
}

func (h *Handler) listFiles(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	page, perPage, err := parsePage(r)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	result, err := h.service.ListFiles(r.Context(), tenantID, chi.URLParam(r, "datasetID"), r.URL.Query().Get("branch_id"), page, perPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]fileResponse, 0, len(result.Items))
	for _, file := range result.Items {
		items = append(items, newFileResponse(file))
	}
	writeJSON(w, http.StatusOK, fileListResponse{
		Items:      items,
		Page:       result.Page.Page,
		PerPage:    result.Page.PerPage,
		Total:      result.Page.Total,
		TotalPages: result.Page.TotalPages,
	})
}

func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteFile(r.Context(), tenantID, chi.URLParam(r, "datasetID"), chi.URLParam(r, "fileID")); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
