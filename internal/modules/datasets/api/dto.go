package api

import (
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/service"
)

type datasetResponse struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	DisplayName     string            `json:"display_name"`
	Description     string            `json:"description"`
	RID             string            `json:"rid"`
	Format          string            `json:"format"`
	StorageLocation string            `json:"storage_location"`
	OwnerID         string            `json:"owner_id,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

func newDatasetResponse(dataset domain.Dataset) datasetResponse {
	return datasetResponse{
		ID:              dataset.ID,
		Name:            dataset.Name,
		DisplayName:     dataset.DisplayName,
		Description:     dataset.Description,
		RID:             dataset.RID,
		Format:          string(dataset.Format),
		StorageLocation: dataset.StorageLocation,
		OwnerID:         dataset.OwnerID,
		Tags:            dataset.Tags,
		CreatedAt:       dataset.CreatedAt,
		UpdatedAt:       dataset.UpdatedAt,
	}
}

type datasetListResponse struct {
	Items      []datasetResponse `json:"items"`
	Page       int               `json:"page"`
	PerPage    int               `json:"per_page"`
	Total      int64             `json:"total"`
	TotalPages int               `json:"total_pages"`
}

type createDatasetRequest struct {
	Name            string            `json:"name"`
	DisplayName     string            `json:"display_name"`
	Description     string            `json:"description"`
	Format          string            `json:"format"`
	StorageLocation string            `json:"storage_location"`
	OwnerID         string            `json:"owner_id"`
	Tags            map[string]string `json:"tags"`
}

type updateDatasetRequest struct {
	DisplayName *string `json:"display_name"`
	Description *string `json:"description"`
}

type branchResponse struct {
	ID                       string    `json:"id"`
	DatasetID                string    `json:"dataset_id"`
	Name                     string    `json:"name"`
	ParentBranchID           string    `json:"parent_branch_id,omitempty"`
	HeadTransactionID        string    `json:"head_transaction_id,omitempty"`
	CreatedFromTransactionID string    `json:"created_from_transaction_id,omitempty"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

func newBranchResponse(branch domain.Branch) branchResponse {
	return branchResponse{
		ID:                       branch.ID,
		DatasetID:                branch.DatasetID,
		Name:                     branch.Name,
		ParentBranchID:           branch.ParentBranchID,
		HeadTransactionID:        branch.HeadTransactionID,
		CreatedFromTransactionID: branch.CreatedFromTransactionID,
		CreatedAt:                branch.CreatedAt,
		UpdatedAt:                branch.UpdatedAt,
	}
}

type branchListResponse struct {
	Items []branchResponse `json:"items"`
}

type createBranchRequest struct {
	Name           string `json:"name"`
	ParentBranchID string `json:"parent_branch_id"`
}

type transactionResponse struct {
	ID              string            `json:"id"`
	DatasetID       string            `json:"dataset_id"`
	BranchID        string            `json:"branch_id"`
	TransactionType string            `json:"transaction_type"`
	Status          string            `json:"status"`
	Message         string            `json:"message"`
	StartedAt       time.Time         `json:"started_at"`
	CommittedAt     *time.Time        `json:"committed_at,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

func newTransactionResponse(transaction domain.Transaction) transactionResponse {
	return transactionResponse{
		ID:              transaction.ID,
		DatasetID:       transaction.DatasetID,
		BranchID:        transaction.BranchID,
		TransactionType: string(transaction.TransactionType),
		Status:          string(transaction.Status),
		Message:         transaction.Message,
		StartedAt:       transaction.StartedAt,
		CommittedAt:     transaction.CommittedAt,
		Metadata:        transaction.Metadata,
		CreatedAt:       transaction.CreatedAt,
		UpdatedAt:       transaction.UpdatedAt,
	}
}

type transactionListResponse struct {
	Items      []transactionResponse `json:"items"`
	Page       int                   `json:"page"`
	PerPage    int                   `json:"per_page"`
	Total      int64                 `json:"total"`
	TotalPages int                   `json:"total_pages"`
}

type beginTransactionRequest struct {
	BranchID        string            `json:"branch_id"`
	TransactionType string            `json:"transaction_type"`
	Message         string            `json:"message"`
	Metadata        map[string]string `json:"metadata"`
}

type viewResponse struct {
	ID        string    `json:"id"`
	DatasetID string    `json:"dataset_id"`
	BranchID  string    `json:"branch_id,omitempty"`
	Name      string    `json:"name"`
	Query     string    `json:"query"`
	Version   int32     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func newViewResponse(view domain.View) viewResponse {
	return viewResponse{
		ID:        view.ID,
		DatasetID: view.DatasetID,
		BranchID:  view.BranchID,
		Name:      view.Name,
		Query:     view.Query,
		Version:   view.Version,
		CreatedAt: view.CreatedAt,
		UpdatedAt: view.UpdatedAt,
	}
}

type viewListResponse struct {
	Items      []viewResponse `json:"items"`
	Page       int            `json:"page"`
	PerPage    int            `json:"per_page"`
	Total      int64          `json:"total"`
	TotalPages int            `json:"total_pages"`
}

type createViewRequest struct {
	BranchID string `json:"branch_id"`
	Name     string `json:"name"`
	Query    string `json:"query"`
}

type updateViewRequest struct {
	Query *string `json:"query"`
}

type fileResponse struct {
	ID          string    `json:"id"`
	DatasetID   string    `json:"dataset_id"`
	BranchID    string    `json:"branch_id,omitempty"`
	ObjectKey   string    `json:"object_key"`
	SizeBytes   int64     `json:"size_bytes"`
	RowCount    int64     `json:"row_count"`
	ETag        string    `json:"etag"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func newFileResponse(file domain.File) fileResponse {
	return fileResponse{
		ID:          file.ID,
		DatasetID:   file.DatasetID,
		BranchID:    file.BranchID,
		ObjectKey:   file.ObjectKey,
		SizeBytes:   file.SizeBytes,
		RowCount:    file.RowCount,
		ETag:        file.ETag,
		ContentType: file.ContentType,
		CreatedAt:   file.CreatedAt,
		UpdatedAt:   file.UpdatedAt,
	}
}

type fileListResponse struct {
	Items      []fileResponse `json:"items"`
	Page       int            `json:"page"`
	PerPage    int            `json:"per_page"`
	Total      int64          `json:"total"`
	TotalPages int            `json:"total_pages"`
}

type createFileRequest struct {
	BranchID    string `json:"branch_id"`
	ObjectKey   string `json:"object_key"`
	SizeBytes   int64  `json:"size_bytes"`
	RowCount    int64  `json:"row_count"`
	ETag        string `json:"etag"`
	ContentType string `json:"content_type"`
}

func datasetList(items []domain.Dataset, page service.Page) datasetListResponse {
	out := datasetListResponse{
		Items:      make([]datasetResponse, 0, len(items)),
		Page:       page.Page,
		PerPage:    page.PerPage,
		Total:      page.Total,
		TotalPages: page.TotalPages,
	}
	for _, item := range items {
		out.Items = append(out.Items, newDatasetResponse(item))
	}
	return out
}
