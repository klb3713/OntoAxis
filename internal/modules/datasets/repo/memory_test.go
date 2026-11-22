package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

func testID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	return id.String()
}

func seedDataset(t *testing.T, store *MemoryStore, tenantID, name string) (domain.Dataset, domain.Branch) {
	t.Helper()
	now := time.Now().UTC()
	dataset := domain.Dataset{
		ID:        testID(t),
		TenantID:  tenantID,
		Name:      name,
		RID:       domain.DatasetRID(testID(t)),
		Format:    domain.DatasetFormatIceberg,
		CreatedAt: now,
		UpdatedAt: now,
	}
	branch := domain.Branch{
		ID:        testID(t),
		TenantID:  tenantID,
		DatasetID: dataset.ID,
		Name:      "main",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateDataset(context.Background(), dataset, branch); err != nil {
		t.Fatalf("CreateDataset() error = %v", err)
	}
	return dataset, branch
}

func TestMemoryStoreDatasetLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	tenantID := testID(t)
	dataset, _ := seedDataset(t, store, tenantID, "orders")

	got, err := store.GetDataset(ctx, tenantID, dataset.ID)
	if err != nil {
		t.Fatalf("GetDataset() error = %v", err)
	}
	if got.Name != "orders" {
		t.Fatalf("GetDataset().Name = %q, want orders", got.Name)
	}
	if _, err := store.GetDataset(ctx, testID(t), dataset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant GetDataset() error = %v, want ErrNotFound", err)
	}

	duplicate := dataset
	duplicate.ID = testID(t)
	if err := store.CreateDataset(ctx, duplicate, domain.Branch{ID: testID(t), TenantID: tenantID, DatasetID: duplicate.ID, Name: "main"}); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate CreateDataset() error = %v, want ErrAlreadyExists", err)
	}

	items, total, err := store.ListDatasets(ctx, tenantID, 0, 10)
	if err != nil {
		t.Fatalf("ListDatasets() error = %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("ListDatasets() total=%d len=%d, want 1/1", total, len(items))
	}

	dataset.DisplayName = "Orders"
	dataset.UpdatedAt = time.Now().UTC()
	if err := store.UpdateDataset(ctx, dataset); err != nil {
		t.Fatalf("UpdateDataset() error = %v", err)
	}
	got, err = store.GetDataset(ctx, tenantID, dataset.ID)
	if err != nil {
		t.Fatalf("GetDataset() after update error = %v", err)
	}
	if got.DisplayName != "Orders" {
		t.Fatalf("DisplayName = %q, want Orders", got.DisplayName)
	}

	if err := store.DeleteDataset(ctx, tenantID, dataset.ID, time.Now().UTC()); err != nil {
		t.Fatalf("DeleteDataset() error = %v", err)
	}
	if _, err := store.GetDataset(ctx, tenantID, dataset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetDataset() after delete error = %v, want ErrNotFound", err)
	}
	if _, total, _ := store.ListDatasets(ctx, tenantID, 0, 10); total != 0 {
		t.Fatalf("ListDatasets() after delete total = %d, want 0", total)
	}
}

func TestMemoryStoreBranchAndTransactionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	tenantID := testID(t)
	dataset, mainBranch := seedDataset(t, store, tenantID, "events")

	now := time.Now().UTC()
	devBranch := domain.Branch{
		ID:             testID(t),
		TenantID:       tenantID,
		DatasetID:      dataset.ID,
		Name:           "dev",
		ParentBranchID: mainBranch.ID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.CreateBranch(ctx, devBranch); err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}
	if err := store.CreateBranch(ctx, devBranch); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate CreateBranch() error = %v, want ErrAlreadyExists", err)
	}

	branches, err := store.ListBranches(ctx, tenantID, dataset.ID)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("ListBranches() len = %d, want 2", len(branches))
	}

	transaction := domain.Transaction{
		ID:              testID(t),
		TenantID:        tenantID,
		DatasetID:       dataset.ID,
		BranchID:        devBranch.ID,
		TransactionType: domain.TransactionTypeAppend,
		Status:          domain.TransactionStatusOpen,
		StartedAt:       now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := store.CreateTransaction(ctx, transaction); err != nil {
		t.Fatalf("CreateTransaction() error = %v", err)
	}
	committed, err := store.CommitTransaction(ctx, tenantID, dataset.ID, transaction.ID, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("CommitTransaction() error = %v", err)
	}
	if committed.Status != domain.TransactionStatusCommitted || committed.CommittedAt == nil {
		t.Fatalf("committed transaction = %+v", committed)
	}
	if _, err := store.CommitTransaction(ctx, tenantID, dataset.ID, transaction.ID, now.Add(2*time.Minute)); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("second commit error = %v, want ErrInvalidState", err)
	}
	branch, err := store.GetBranch(ctx, tenantID, dataset.ID, devBranch.ID)
	if err != nil {
		t.Fatalf("GetBranch() error = %v", err)
	}
	if branch.HeadTransactionID != transaction.ID {
		t.Fatalf("HeadTransactionID = %q, want %q", branch.HeadTransactionID, transaction.ID)
	}

	aborted := transaction
	aborted.ID = testID(t)
	aborted.Status = domain.TransactionStatusOpen
	aborted.CommittedAt = nil
	if err := store.CreateTransaction(ctx, aborted); err != nil {
		t.Fatalf("CreateTransaction(aborted) error = %v", err)
	}
	if _, err := store.AbortTransaction(ctx, tenantID, dataset.ID, aborted.ID, now); err != nil {
		t.Fatalf("AbortTransaction() error = %v", err)
	}
	if _, err := store.AbortTransaction(ctx, tenantID, dataset.ID, aborted.ID, now); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("second abort error = %v, want ErrInvalidState", err)
	}
}

func TestMemoryStoreViewsAndFiles(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	tenantID := testID(t)
	dataset, branch := seedDataset(t, store, tenantID, "customers")

	view := domain.View{
		ID:        testID(t),
		TenantID:  tenantID,
		DatasetID: dataset.ID,
		BranchID:  branch.ID,
		Name:      "active",
		Query:     "SELECT * FROM customers WHERE active",
		Version:   1,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateView(ctx, view); err != nil {
		t.Fatalf("CreateView() error = %v", err)
	}
	if err := store.CreateView(ctx, view); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate CreateView() error = %v, want ErrAlreadyExists", err)
	}
	view.Query = "SELECT * FROM customers WHERE active = true"
	view.Version = 2
	if err := store.UpdateView(ctx, view); err != nil {
		t.Fatalf("UpdateView() error = %v", err)
	}
	views, total, err := store.ListViews(ctx, tenantID, dataset.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListViews() error = %v", err)
	}
	if total != 1 || len(views) != 1 || views[0].Version != 2 {
		t.Fatalf("ListViews() = %+v, total=%d", views, total)
	}

	file := domain.File{
		ID:        testID(t),
		TenantID:  tenantID,
		DatasetID: dataset.ID,
		BranchID:  branch.ID,
		ObjectKey: "customers/part-0001.parquet",
		SizeBytes: 1024,
		RowCount:  10,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateFile(ctx, file); err != nil {
		t.Fatalf("CreateFile() error = %v", err)
	}
	if err := store.CreateFile(ctx, file); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate CreateFile() error = %v, want ErrAlreadyExists", err)
	}
	files, total, err := store.ListFiles(ctx, tenantID, dataset.ID, branch.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if total != 1 || len(files) != 1 {
		t.Fatalf("ListFiles() total=%d len=%d", total, len(files))
	}
	if files, _, _ := store.ListFiles(ctx, tenantID, dataset.ID, testID(t), 0, 10); len(files) != 0 {
		t.Fatalf("ListFiles() with other branch len = %d, want 0", len(files))
	}

	if err := store.DeleteView(ctx, tenantID, dataset.ID, view.ID, time.Now().UTC()); err != nil {
		t.Fatalf("DeleteView() error = %v", err)
	}
	if _, err := store.GetView(ctx, tenantID, dataset.ID, view.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetView() after delete error = %v", err)
	}
	if err := store.DeleteFile(ctx, tenantID, dataset.ID, file.ID, time.Now().UTC()); err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}
	if _, err := store.GetFile(ctx, tenantID, dataset.ID, file.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetFile() after delete error = %v", err)
	}
}
