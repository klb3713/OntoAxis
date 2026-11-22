package repo

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

// MemoryStore is an in-memory Repository used by unit and HTTP tests. It is
// not intended for production and deliberately mirrors the visibility rules
// of the PostgreSQL store (soft deletes, tenant scoping, unique names).
type MemoryStore struct {
	mu           sync.RWMutex
	datasets     map[string]domain.Dataset
	branches     map[string]domain.Branch
	transactions map[string]domain.Transaction
	views        map[string]domain.View
	files        map[string]domain.File
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		datasets:     map[string]domain.Dataset{},
		branches:     map[string]domain.Branch{},
		transactions: map[string]domain.Transaction{},
		views:        map[string]domain.View{},
		files:        map[string]domain.File{},
	}
}

func memoryKey(tenantID, id string) string { return tenantID + "/" + id }

func (m *MemoryStore) Ping(context.Context) error { return nil }

func (m *MemoryStore) CreateDataset(_ context.Context, dataset domain.Dataset, mainBranch domain.Branch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.datasets {
		if existing.TenantID == dataset.TenantID && existing.Name == dataset.Name {
			return domain.ErrAlreadyExists
		}
		if existing.RID == dataset.RID {
			return domain.ErrAlreadyExists
		}
	}
	m.datasets[memoryKey(dataset.TenantID, dataset.ID)] = dataset
	m.branches[memoryKey(mainBranch.TenantID, mainBranch.ID)] = mainBranch
	return nil
}

func (m *MemoryStore) GetDataset(_ context.Context, tenantID, datasetID string) (domain.Dataset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dataset, ok := m.datasets[memoryKey(tenantID, datasetID)]
	if !ok || dataset.DeletedAt != nil {
		return domain.Dataset{}, domain.ErrNotFound
	}
	return dataset, nil
}

func (m *MemoryStore) ListDatasets(_ context.Context, tenantID string, offset, limit int) ([]domain.Dataset, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]domain.Dataset, 0)
	for _, dataset := range m.datasets {
		if dataset.TenantID != tenantID || dataset.DeletedAt != nil {
			continue
		}
		items = append(items, dataset)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page, total := pageSlice(items, offset, limit)
	return page, total, nil
}

func (m *MemoryStore) UpdateDataset(_ context.Context, dataset domain.Dataset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryKey(dataset.TenantID, dataset.ID)
	existing, ok := m.datasets[key]
	if !ok || existing.DeletedAt != nil {
		return domain.ErrNotFound
	}
	m.datasets[key] = dataset
	return nil
}

func (m *MemoryStore) DeleteDataset(_ context.Context, tenantID, datasetID string, deletedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryKey(tenantID, datasetID)
	dataset, ok := m.datasets[key]
	if !ok || dataset.DeletedAt != nil {
		return domain.ErrNotFound
	}
	dataset.DeletedAt = &deletedAt
	dataset.UpdatedAt = deletedAt
	m.datasets[key] = dataset
	return nil
}

func (m *MemoryStore) CreateBranch(_ context.Context, branch domain.Branch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dataset, ok := m.datasets[memoryKey(branch.TenantID, branch.DatasetID)]
	if !ok || dataset.DeletedAt != nil {
		return domain.ErrNotFound
	}
	for _, existing := range m.branches {
		if existing.TenantID == branch.TenantID && existing.DatasetID == branch.DatasetID && existing.Name == branch.Name {
			return domain.ErrAlreadyExists
		}
	}
	m.branches[memoryKey(branch.TenantID, branch.ID)] = branch
	return nil
}

func (m *MemoryStore) GetBranch(_ context.Context, tenantID, datasetID, branchID string) (domain.Branch, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	branch, ok := m.branches[memoryKey(tenantID, branchID)]
	if !ok || branch.DatasetID != datasetID {
		return domain.Branch{}, domain.ErrNotFound
	}
	return branch, nil
}

func (m *MemoryStore) ListBranches(_ context.Context, tenantID, datasetID string) ([]domain.Branch, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]domain.Branch, 0)
	for _, branch := range m.branches {
		if branch.TenantID == tenantID && branch.DatasetID == datasetID {
			items = append(items, branch)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (m *MemoryStore) CreateTransaction(_ context.Context, transaction domain.Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.datasets[memoryKey(transaction.TenantID, transaction.DatasetID)]; !ok {
		return domain.ErrNotFound
	}
	branch, ok := m.branches[memoryKey(transaction.TenantID, transaction.BranchID)]
	if !ok || branch.DatasetID != transaction.DatasetID {
		return domain.ErrNotFound
	}
	m.transactions[memoryKey(transaction.TenantID, transaction.ID)] = transaction
	return nil
}

func (m *MemoryStore) GetTransaction(_ context.Context, tenantID, datasetID, transactionID string) (domain.Transaction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	transaction, ok := m.transactions[memoryKey(tenantID, transactionID)]
	if !ok || transaction.DatasetID != datasetID {
		return domain.Transaction{}, domain.ErrNotFound
	}
	return transaction, nil
}

func (m *MemoryStore) ListTransactions(_ context.Context, tenantID, datasetID string, offset, limit int) ([]domain.Transaction, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]domain.Transaction, 0)
	for _, transaction := range m.transactions {
		if transaction.TenantID == tenantID && transaction.DatasetID == datasetID {
			items = append(items, transaction)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartedAt.Equal(items[j].StartedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	page, total := pageSlice(items, offset, limit)
	return page, total, nil
}

func (m *MemoryStore) CommitTransaction(_ context.Context, tenantID, datasetID, transactionID string, committedAt time.Time) (domain.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryKey(tenantID, transactionID)
	transaction, ok := m.transactions[key]
	if !ok || transaction.DatasetID != datasetID {
		return domain.Transaction{}, domain.ErrNotFound
	}
	if err := transaction.Commit(committedAt); err != nil {
		return domain.Transaction{}, err
	}
	m.transactions[key] = transaction
	if branch, ok := m.branches[memoryKey(tenantID, transaction.BranchID)]; ok && branch.DatasetID == datasetID {
		branch.HeadTransactionID = transaction.ID
		branch.UpdatedAt = committedAt
		m.branches[memoryKey(tenantID, branch.ID)] = branch
	}
	return transaction, nil
}

func (m *MemoryStore) AbortTransaction(_ context.Context, tenantID, datasetID, transactionID string, abortedAt time.Time) (domain.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryKey(tenantID, transactionID)
	transaction, ok := m.transactions[key]
	if !ok || transaction.DatasetID != datasetID {
		return domain.Transaction{}, domain.ErrNotFound
	}
	if err := transaction.Abort(abortedAt); err != nil {
		return domain.Transaction{}, err
	}
	m.transactions[key] = transaction
	return transaction, nil
}

func (m *MemoryStore) CreateView(_ context.Context, view domain.View) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dataset, ok := m.datasets[memoryKey(view.TenantID, view.DatasetID)]
	if !ok || dataset.DeletedAt != nil {
		return domain.ErrNotFound
	}
	for _, existing := range m.views {
		if existing.TenantID == view.TenantID && existing.DatasetID == view.DatasetID && existing.Name == view.Name {
			return domain.ErrAlreadyExists
		}
	}
	m.views[memoryKey(view.TenantID, view.ID)] = view
	return nil
}

func (m *MemoryStore) GetView(_ context.Context, tenantID, datasetID, viewID string) (domain.View, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	view, ok := m.views[memoryKey(tenantID, viewID)]
	if !ok || view.DatasetID != datasetID || view.DeletedAt != nil {
		return domain.View{}, domain.ErrNotFound
	}
	return view, nil
}

func (m *MemoryStore) ListViews(_ context.Context, tenantID, datasetID string, offset, limit int) ([]domain.View, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]domain.View, 0)
	for _, view := range m.views {
		if view.TenantID == tenantID && view.DatasetID == datasetID && view.DeletedAt == nil {
			items = append(items, view)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page, total := pageSlice(items, offset, limit)
	return page, total, nil
}

func (m *MemoryStore) UpdateView(_ context.Context, view domain.View) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryKey(view.TenantID, view.ID)
	existing, ok := m.views[key]
	if !ok || existing.DeletedAt != nil {
		return domain.ErrNotFound
	}
	m.views[key] = view
	return nil
}

func (m *MemoryStore) DeleteView(_ context.Context, tenantID, datasetID, viewID string, deletedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryKey(tenantID, viewID)
	view, ok := m.views[key]
	if !ok || view.DatasetID != datasetID || view.DeletedAt != nil {
		return domain.ErrNotFound
	}
	view.DeletedAt = &deletedAt
	view.UpdatedAt = deletedAt
	m.views[key] = view
	return nil
}

func (m *MemoryStore) CreateFile(_ context.Context, file domain.File) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dataset, ok := m.datasets[memoryKey(file.TenantID, file.DatasetID)]
	if !ok || dataset.DeletedAt != nil {
		return domain.ErrNotFound
	}
	for _, existing := range m.files {
		if existing.TenantID == file.TenantID && existing.DatasetID == file.DatasetID &&
			existing.ObjectKey == file.ObjectKey && existing.DeletedAt == nil {
			return domain.ErrAlreadyExists
		}
	}
	m.files[memoryKey(file.TenantID, file.ID)] = file
	return nil
}

func (m *MemoryStore) GetFile(_ context.Context, tenantID, datasetID, fileID string) (domain.File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	file, ok := m.files[memoryKey(tenantID, fileID)]
	if !ok || file.DatasetID != datasetID || file.DeletedAt != nil {
		return domain.File{}, domain.ErrNotFound
	}
	return file, nil
}

func (m *MemoryStore) ListFiles(_ context.Context, tenantID, datasetID, branchID string, offset, limit int) ([]domain.File, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]domain.File, 0)
	for _, file := range m.files {
		if file.TenantID != tenantID || file.DatasetID != datasetID || file.DeletedAt != nil {
			continue
		}
		if branchID != "" && file.BranchID != branchID {
			continue
		}
		items = append(items, file)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page, total := pageSlice(items, offset, limit)
	return page, total, nil
}

func (m *MemoryStore) DeleteFile(_ context.Context, tenantID, datasetID, fileID string, deletedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryKey(tenantID, fileID)
	file, ok := m.files[key]
	if !ok || file.DatasetID != datasetID || file.DeletedAt != nil {
		return domain.ErrNotFound
	}
	file.DeletedAt = &deletedAt
	file.UpdatedAt = deletedAt
	m.files[key] = file
	return nil
}

func pageSlice[T any](items []T, offset, limit int) ([]T, int64) {
	total := int64(len(items))
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []T{}, total
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return append([]T(nil), items[offset:end]...), total
}
