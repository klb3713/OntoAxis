package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/service"
)

func (h *Handler) createBranch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	var request createBranchRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeServiceError(w, r, err)
		return
	}
	branch, err := h.service.CreateBranch(r.Context(), tenantID, chi.URLParam(r, "datasetID"), service.CreateBranchInput{
		Name:           request.Name,
		ParentBranchID: request.ParentBranchID,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newBranchResponse(branch))
}

func (h *Handler) getBranch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	branch, err := h.service.GetBranch(r.Context(), tenantID, chi.URLParam(r, "datasetID"), chi.URLParam(r, "branchID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newBranchResponse(branch))
}

func (h *Handler) listBranches(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	branches, err := h.service.ListBranches(r.Context(), tenantID, chi.URLParam(r, "datasetID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]branchResponse, 0, len(branches))
	for _, branch := range branches {
		items = append(items, newBranchResponse(branch))
	}
	writeJSON(w, http.StatusOK, branchListResponse{Items: items})
}

func (h *Handler) beginTransaction(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	var request beginTransactionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeServiceError(w, r, err)
		return
	}
	transaction, err := h.service.BeginTransaction(r.Context(), tenantID, chi.URLParam(r, "datasetID"), service.BeginTransactionInput{
		BranchID:        request.BranchID,
		TransactionType: request.TransactionType,
		Message:         request.Message,
		Metadata:        request.Metadata,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newTransactionResponse(transaction))
}

func (h *Handler) getTransaction(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	transaction, err := h.service.GetTransaction(r.Context(), tenantID, chi.URLParam(r, "datasetID"), chi.URLParam(r, "transactionID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTransactionResponse(transaction))
}

func (h *Handler) listTransactions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	page, perPage, err := parsePage(r)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	result, err := h.service.ListTransactions(r.Context(), tenantID, chi.URLParam(r, "datasetID"), page, perPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]transactionResponse, 0, len(result.Items))
	for _, transaction := range result.Items {
		items = append(items, newTransactionResponse(transaction))
	}
	writeJSON(w, http.StatusOK, transactionListResponse{
		Items:      items,
		Page:       result.Page.Page,
		PerPage:    result.Page.PerPage,
		Total:      result.Page.Total,
		TotalPages: result.Page.TotalPages,
	})
}

func (h *Handler) commitTransaction(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	transaction, err := h.service.CommitTransaction(r.Context(), tenantID, chi.URLParam(r, "datasetID"), chi.URLParam(r, "transactionID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTransactionResponse(transaction))
}

func (h *Handler) abortTransaction(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	transaction, err := h.service.AbortTransaction(r.Context(), tenantID, chi.URLParam(r, "datasetID"), chi.URLParam(r, "transactionID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTransactionResponse(transaction))
}
