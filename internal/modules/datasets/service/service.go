// Package service exposes the dataset catalog use cases. It is the only
// package that api/ may call; repository access stays behind Repository.
package service

import (
	"context"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

// Repository is the persistence contract for the dataset catalog. Both the
// PostgreSQL store and the in-memory fake implement it.
type Repository interface {
	Ping(ctx context.Context) error

	CreateDataset(ctx context.Context, dataset domain.Dataset, mainBranch domain.Branch) error
	GetDataset(ctx context.Context, tenantID, datasetID string) (domain.Dataset, error)
	ListDatasets(ctx context.Context, tenantID string, offset, limit int) ([]domain.Dataset, int64, error)
	UpdateDataset(ctx context.Context, dataset domain.Dataset) error
	DeleteDataset(ctx context.Context, tenantID, datasetID string, deletedAt time.Time) error

	CreateBranch(ctx context.Context, branch domain.Branch) error
	GetBranch(ctx context.Context, tenantID, datasetID, branchID string) (domain.Branch, error)
	ListBranches(ctx context.Context, tenantID, datasetID string) ([]domain.Branch, error)

	CreateTransaction(ctx context.Context, transaction domain.Transaction) error
	GetTransaction(ctx context.Context, tenantID, datasetID, transactionID string) (domain.Transaction, error)
	ListTransactions(ctx context.Context, tenantID, datasetID string, offset, limit int) ([]domain.Transaction, int64, error)
	CommitTransaction(ctx context.Context, tenantID, datasetID, transactionID string, committedAt time.Time) (domain.Transaction, error)
	AbortTransaction(ctx context.Context, tenantID, datasetID, transactionID string, abortedAt time.Time) (domain.Transaction, error)

	CreateView(ctx context.Context, view domain.View) error
	GetView(ctx context.Context, tenantID, datasetID, viewID string) (domain.View, error)
	ListViews(ctx context.Context, tenantID, datasetID string, offset, limit int) ([]domain.View, int64, error)
	UpdateView(ctx context.Context, view domain.View) error
	DeleteView(ctx context.Context, tenantID, datasetID, viewID string, deletedAt time.Time) error

	CreateFile(ctx context.Context, file domain.File) error
	GetFile(ctx context.Context, tenantID, datasetID, fileID string) (domain.File, error)
	ListFiles(ctx context.Context, tenantID, datasetID, branchID string, offset, limit int) ([]domain.File, int64, error)
	DeleteFile(ctx context.Context, tenantID, datasetID, fileID string, deletedAt time.Time) error
}

// Service is the dataset catalog application boundary.
type Service interface {
	CreateDataset(ctx context.Context, tenantID string, input CreateDatasetInput) (CreateDatasetResult, error)
	GetDataset(ctx context.Context, tenantID, datasetID string) (domain.Dataset, error)
	ListDatasets(ctx context.Context, tenantID string, page, perPage int) (DatasetPage, error)
	UpdateDataset(ctx context.Context, tenantID, datasetID string, input UpdateDatasetInput) (domain.Dataset, error)
	DeleteDataset(ctx context.Context, tenantID, datasetID string) error

	CreateBranch(ctx context.Context, tenantID, datasetID string, input CreateBranchInput) (domain.Branch, error)
	GetBranch(ctx context.Context, tenantID, datasetID, branchID string) (domain.Branch, error)
	ListBranches(ctx context.Context, tenantID, datasetID string) ([]domain.Branch, error)

	BeginTransaction(ctx context.Context, tenantID, datasetID string, input BeginTransactionInput) (domain.Transaction, error)
	GetTransaction(ctx context.Context, tenantID, datasetID, transactionID string) (domain.Transaction, error)
	ListTransactions(ctx context.Context, tenantID, datasetID string, page, perPage int) (TransactionPage, error)
	CommitTransaction(ctx context.Context, tenantID, datasetID, transactionID string) (domain.Transaction, error)
	AbortTransaction(ctx context.Context, tenantID, datasetID, transactionID string) (domain.Transaction, error)

	CreateView(ctx context.Context, tenantID, datasetID string, input CreateViewInput) (domain.View, error)
	GetView(ctx context.Context, tenantID, datasetID, viewID string) (domain.View, error)
	ListViews(ctx context.Context, tenantID, datasetID string, page, perPage int) (ViewPage, error)
	UpdateView(ctx context.Context, tenantID, datasetID, viewID string, input UpdateViewInput) (domain.View, error)
	DeleteView(ctx context.Context, tenantID, datasetID, viewID string) error

	CreateFile(ctx context.Context, tenantID, datasetID string, input CreateFileInput) (domain.File, error)
	GetFile(ctx context.Context, tenantID, datasetID, fileID string) (domain.File, error)
	ListFiles(ctx context.Context, tenantID, datasetID, branchID string, page, perPage int) (FilePage, error)
	DeleteFile(ctx context.Context, tenantID, datasetID, fileID string) error
}

// Page is the shared pagination envelope. Page and PerPage echo the normalized
// request values.
type Page struct {
	Page       int
	PerPage    int
	Total      int64
	TotalPages int
}

type CreateDatasetInput struct {
	Name            string
	DisplayName     string
	Description     string
	Format          string
	StorageLocation string
	OwnerID         string
	Tags            map[string]string
}

type UpdateDatasetInput struct {
	DisplayName *string
	Description *string
}

type CreateDatasetResult struct {
	Dataset    domain.Dataset
	MainBranch domain.Branch
}

type DatasetPage struct {
	Items []domain.Dataset
	Page  Page
}

type CreateBranchInput struct {
	Name           string
	ParentBranchID string
}

type BeginTransactionInput struct {
	BranchID        string
	TransactionType string
	Message         string
	Metadata        map[string]string
}

type TransactionPage struct {
	Items []domain.Transaction
	Page  Page
}

type CreateViewInput struct {
	BranchID string
	Name     string
	Query    string
}

type UpdateViewInput struct {
	Query *string
}

type ViewPage struct {
	Items []domain.View
	Page  Page
}

type CreateFileInput struct {
	BranchID    string
	ObjectKey   string
	SizeBytes   int64
	RowCount    int64
	ETag        string
	ContentType string
}

type FilePage struct {
	Items []domain.File
	Page  Page
}
