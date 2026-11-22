package service

import (
	"context"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

func (s *impl) BeginTransaction(ctx context.Context, tenantID, datasetID string, input BeginTransactionInput) (domain.Transaction, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.Transaction{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return domain.Transaction{}, err
	}
	branch, err := s.getBranch(ctx, dataset.TenantID, dataset.ID, input.BranchID)
	if err != nil {
		return domain.Transaction{}, err
	}
	transactionType, err := domain.ParseTransactionType(input.TransactionType)
	if err != nil {
		return domain.Transaction{}, err
	}
	id, err := newID()
	if err != nil {
		return domain.Transaction{}, err
	}
	now := time.Now().UTC()
	transaction := domain.Transaction{
		ID:              id,
		TenantID:        dataset.TenantID,
		DatasetID:       dataset.ID,
		BranchID:        branch.ID,
		TransactionType: transactionType,
		Status:          domain.TransactionStatusOpen,
		Message:         input.Message,
		StartedAt:       now,
		Metadata:        copyTags(input.Metadata),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := repo.CreateTransaction(ctx, transaction); err != nil {
		return domain.Transaction{}, err
	}
	return transaction, nil
}

func (s *impl) GetTransaction(ctx context.Context, tenantID, datasetID, transactionID string) (domain.Transaction, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.Transaction{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return domain.Transaction{}, err
	}
	id, err := normalizeUUID("transaction_id", transactionID)
	if err != nil {
		return domain.Transaction{}, err
	}
	transaction, err := repo.GetTransaction(ctx, dataset.TenantID, dataset.ID, id)
	if err != nil {
		return domain.Transaction{}, err
	}
	if transaction.TenantID != dataset.TenantID || transaction.DatasetID != dataset.ID {
		return domain.Transaction{}, domain.ErrNotFound
	}
	return transaction, nil
}

func (s *impl) ListTransactions(ctx context.Context, tenantID, datasetID string, page, perPage int) (TransactionPage, error) {
	repo, err := s.repository()
	if err != nil {
		return TransactionPage{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return TransactionPage{}, err
	}
	meta, offset, limit, err := normalizePage(page, perPage)
	if err != nil {
		return TransactionPage{}, err
	}
	items, total, err := repo.ListTransactions(ctx, dataset.TenantID, dataset.ID, offset, limit)
	if err != nil {
		return TransactionPage{}, err
	}
	if items == nil {
		items = []domain.Transaction{}
	}
	return TransactionPage{Items: items, Page: finishPage(meta, total)}, nil
}

func (s *impl) CommitTransaction(ctx context.Context, tenantID, datasetID, transactionID string) (domain.Transaction, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.Transaction{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return domain.Transaction{}, err
	}
	id, err := normalizeUUID("transaction_id", transactionID)
	if err != nil {
		return domain.Transaction{}, err
	}
	transaction, err := repo.CommitTransaction(ctx, dataset.TenantID, dataset.ID, id, time.Now().UTC())
	if err != nil {
		return domain.Transaction{}, err
	}
	return transaction, nil
}

func (s *impl) AbortTransaction(ctx context.Context, tenantID, datasetID, transactionID string) (domain.Transaction, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.Transaction{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return domain.Transaction{}, err
	}
	id, err := normalizeUUID("transaction_id", transactionID)
	if err != nil {
		return domain.Transaction{}, err
	}
	transaction, err := repo.AbortTransaction(ctx, dataset.TenantID, dataset.ID, id, time.Now().UTC())
	if err != nil {
		return domain.Transaction{}, err
	}
	return transaction, nil
}
