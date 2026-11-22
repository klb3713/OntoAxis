package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/repo"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/service"
)

func testUUID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	return id.String()
}

func newFixture(t *testing.T) (service.Service, string) {
	t.Helper()
	return service.New(repo.NewMemoryStore()), testUUID(t)
}

func createDataset(t *testing.T, svc service.Service, tenantID, name string) service.CreateDatasetResult {
	t.Helper()
	result, err := svc.CreateDataset(context.Background(), tenantID, service.CreateDatasetInput{
		Name:            name,
		DisplayName:     "Test " + name,
		Description:     "dataset for tests",
		Format:          "iceberg",
		StorageLocation: "s3://warehouse/" + name,
	})
	if err != nil {
		t.Fatalf("CreateDataset(%q) error = %v", name, err)
	}
	return result
}

func TestCreateAndListDatasets(t *testing.T) {
	ctx := context.Background()
	svc, tenantID := newFixture(t)
	result := createDataset(t, svc, tenantID, "orders")

	if result.Dataset.RID != domain.DatasetRID(result.Dataset.ID) {
		t.Fatalf("RID = %q, want %q", result.Dataset.RID, domain.DatasetRID(result.Dataset.ID))
	}
	if result.MainBranch.Name != "main" || result.MainBranch.DatasetID != result.Dataset.ID {
		t.Fatalf("main branch = %+v", result.MainBranch)
	}
	if _, err := uuid.Parse(result.Dataset.ID); err != nil {
		t.Fatalf("dataset ID is not a UUID: %v", err)
	}

	page, err := svc.ListDatasets(ctx, tenantID, 0, 0)
	if err != nil {
		t.Fatalf("ListDatasets() error = %v", err)
	}
	if page.Page.Page != 1 || page.Page.PerPage != 50 || page.Page.Total != 1 || page.Page.TotalPages != 1 {
		t.Fatalf("default page = %+v", page.Page)
	}

	if _, err := svc.CreateDataset(ctx, tenantID, service.CreateDatasetInput{Name: "orders", Format: "iceberg"}); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate dataset error = %v, want ErrAlreadyExists", err)
	}
	if _, err := svc.CreateDataset(ctx, tenantID, service.CreateDatasetInput{Name: "bad", Format: "csv"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("invalid format error = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.CreateDataset(ctx, tenantID, service.CreateDatasetInput{Name: " ", Format: "iceberg"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("empty name error = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.CreateDataset(ctx, "not-a-uuid", service.CreateDatasetInput{Name: "x", Format: "iceberg"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("invalid tenant error = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.CreateDataset(ctx, "", service.CreateDatasetInput{Name: "x", Format: "iceberg"}); !errors.Is(err, domain.ErrTenantRequired) {
		t.Fatalf("empty tenant error = %v, want ErrTenantRequired", err)
	}

	updated, err := svc.UpdateDataset(ctx, tenantID, result.Dataset.ID, service.UpdateDatasetInput{DisplayName: stringPointer("Orders")})
	if err != nil {
		t.Fatalf("UpdateDataset() error = %v", err)
	}
	if updated.DisplayName != "Orders" {
		t.Fatalf("DisplayName = %q, want Orders", updated.DisplayName)
	}
}

func TestListDatasetsPaginationAndIsolation(t *testing.T) {
	ctx := context.Background()
	svc, tenantID := newFixture(t)
	otherTenant := testUUID(t)
	for _, name := range []string{"one", "two", "three"} {
		createDataset(t, svc, tenantID, name)
	}
	createDataset(t, svc, otherTenant, "other")

	page, err := svc.ListDatasets(ctx, tenantID, 2, 2)
	if err != nil {
		t.Fatalf("ListDatasets(page=2) error = %v", err)
	}
	if page.Page.Total != 3 || page.Page.TotalPages != 2 || len(page.Items) != 1 {
		t.Fatalf("page = %+v len=%d", page.Page, len(page.Items))
	}
	if _, err := svc.ListDatasets(ctx, tenantID, 1, 201); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("per_page=201 error = %v, want ErrInvalidArgument", err)
	}
	foreign, err := svc.ListDatasets(ctx, otherTenant, 0, 10)
	if err != nil {
		t.Fatalf("ListDatasets(other) error = %v", err)
	}
	if foreign.Page.Total != 1 || len(foreign.Items) != 1 || foreign.Items[0].Name != "other" {
		t.Fatalf("other tenant list = %+v", foreign)
	}
	if _, err := svc.GetDataset(ctx, otherTenant, page.Items[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant GetDataset() error = %v, want ErrNotFound", err)
	}
}

func TestTransactionStateMachine(t *testing.T) {
	ctx := context.Background()
	svc, tenantID := newFixture(t)
	created := createDataset(t, svc, tenantID, "events")

	transaction, err := svc.BeginTransaction(ctx, tenantID, created.Dataset.ID, service.BeginTransactionInput{
		BranchID:        created.MainBranch.ID,
		TransactionType: "append",
		Message:         "ingest",
	})
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	if transaction.Status != domain.TransactionStatusOpen || transaction.TransactionType != domain.TransactionTypeAppend {
		t.Fatalf("transaction = %+v", transaction)
	}

	committed, err := svc.CommitTransaction(ctx, tenantID, created.Dataset.ID, transaction.ID)
	if err != nil {
		t.Fatalf("CommitTransaction() error = %v", err)
	}
	if committed.Status != domain.TransactionStatusCommitted || committed.CommittedAt == nil {
		t.Fatalf("committed = %+v", committed)
	}
	if _, err := svc.CommitTransaction(ctx, tenantID, created.Dataset.ID, transaction.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("second commit error = %v, want ErrInvalidState", err)
	}
	if _, err := svc.AbortTransaction(ctx, tenantID, created.Dataset.ID, transaction.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("abort committed error = %v, want ErrInvalidState", err)
	}

	branches, err := svc.ListBranches(ctx, tenantID, created.Dataset.ID)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	foundHead := false
	for _, branch := range branches {
		if branch.ID == created.MainBranch.ID && branch.HeadTransactionID == transaction.ID {
			foundHead = true
		}
	}
	if !foundHead {
		t.Fatalf("main branch head not advanced: %+v", branches)
	}

	aborted, err := svc.BeginTransaction(ctx, tenantID, created.Dataset.ID, service.BeginTransactionInput{BranchID: created.MainBranch.ID})
	if err != nil {
		t.Fatalf("BeginTransaction() second error = %v", err)
	}
	got, err := svc.AbortTransaction(ctx, tenantID, created.Dataset.ID, aborted.ID)
	if err != nil {
		t.Fatalf("AbortTransaction() error = %v", err)
	}
	if got.Status != domain.TransactionStatusAborted {
		t.Fatalf("aborted status = %q", got.Status)
	}
	if _, err := svc.BeginTransaction(ctx, tenantID, created.Dataset.ID, service.BeginTransactionInput{BranchID: created.MainBranch.ID, TransactionType: "merge"}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("invalid transaction type error = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.BeginTransaction(ctx, tenantID, created.Dataset.ID, service.BeginTransactionInput{BranchID: testUUID(t)}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown branch error = %v, want ErrNotFound", err)
	}
}

func TestViewsAndFiles(t *testing.T) {
	ctx := context.Background()
	svc, tenantID := newFixture(t)
	created := createDataset(t, svc, tenantID, "customers")

	view, err := svc.CreateView(ctx, tenantID, created.Dataset.ID, service.CreateViewInput{
		BranchID: created.MainBranch.ID,
		Name:     "active",
		Query:    "SELECT * FROM customers",
	})
	if err != nil {
		t.Fatalf("CreateView() error = %v", err)
	}
	if view.Version != 1 {
		t.Fatalf("Version = %d, want 1", view.Version)
	}
	if _, err := svc.CreateView(ctx, tenantID, created.Dataset.ID, service.CreateViewInput{Name: "active", Query: "SELECT 1"}); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate view error = %v, want ErrAlreadyExists", err)
	}
	if _, err := svc.CreateView(ctx, tenantID, created.Dataset.ID, service.CreateViewInput{Name: "bad", Query: " "}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("empty query error = %v, want ErrInvalidArgument", err)
	}
	updated, err := svc.UpdateView(ctx, tenantID, created.Dataset.ID, view.ID, service.UpdateViewInput{Query: stringPointer("SELECT * FROM customers WHERE active")})
	if err != nil {
		t.Fatalf("UpdateView() error = %v", err)
	}
	if updated.Version != 2 || updated.Query == "SELECT * FROM customers" {
		t.Fatalf("updated view = %+v", updated)
	}
	if _, err := svc.UpdateView(ctx, tenantID, created.Dataset.ID, view.ID, service.UpdateViewInput{Query: stringPointer("")}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("empty update query error = %v, want ErrInvalidArgument", err)
	}

	file, err := svc.CreateFile(ctx, tenantID, created.Dataset.ID, service.CreateFileInput{
		BranchID:    created.MainBranch.ID,
		ObjectKey:   "customers/part-0001.parquet",
		SizeBytes:   1024,
		RowCount:    10,
		ETag:        "abc",
		ContentType: "application/vnd.apache.parquet",
	})
	if err != nil {
		t.Fatalf("CreateFile() error = %v", err)
	}
	if file.ObjectKey == "" || file.SizeBytes != 1024 {
		t.Fatalf("file = %+v", file)
	}
	if _, err := svc.CreateFile(ctx, tenantID, created.Dataset.ID, service.CreateFileInput{ObjectKey: " "}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("empty object key error = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.CreateFile(ctx, tenantID, created.Dataset.ID, service.CreateFileInput{ObjectKey: "bad", SizeBytes: -1}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("negative size error = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.CreateFile(ctx, tenantID, created.Dataset.ID, service.CreateFileInput{ObjectKey: "customers/part-0001.parquet", BranchID: created.MainBranch.ID}); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate file error = %v, want ErrAlreadyExists", err)
	}
	files, err := svc.ListFiles(ctx, tenantID, created.Dataset.ID, created.MainBranch.ID, 1, 10)
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if files.Page.Total != 1 || len(files.Items) != 1 {
		t.Fatalf("files = %+v total=%d", files.Items, files.Page.Total)
	}
	if err := svc.DeleteFile(ctx, tenantID, created.Dataset.ID, file.ID); err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}
	if _, err := svc.GetFile(ctx, tenantID, created.Dataset.ID, file.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetFile() after delete error = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteView(ctx, tenantID, created.Dataset.ID, view.ID); err != nil {
		t.Fatalf("DeleteView() error = %v", err)
	}
	if _, err := svc.GetView(ctx, tenantID, created.Dataset.ID, view.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetView() after delete error = %v, want ErrNotFound", err)
	}
}

func TestNilRepositoryFailsClosed(t *testing.T) {
	svc := service.New(nil)
	if _, err := svc.ListDatasets(context.Background(), testUUID(t), 0, 0); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("ListDatasets() error = %v, want ErrUnavailable", err)
	}
	if _, err := svc.GetDataset(context.Background(), testUUID(t), testUUID(t)); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("GetDataset() error = %v, want ErrUnavailable", err)
	}
}

func stringPointer(value string) *string {
	return &value
}
