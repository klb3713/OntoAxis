package search

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type fakeScanRow struct {
	id, tenantID, objectTypeID string
	properties                 []byte
	version                    int64
	createdAt, updatedAt       time.Time
	err                        error
}

func (r fakeScanRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string) = r.id
	*dest[1].(*string) = r.tenantID
	*dest[2].(*string) = r.objectTypeID
	*dest[3].(*[]byte) = r.properties
	*dest[4].(*int64) = r.version
	*dest[5].(*time.Time) = r.createdAt
	*dest[6].(*time.Time) = r.updatedAt
	return nil
}

func TestScanObjectRow(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := created.Add(time.Hour)
	row := fakeScanRow{
		id:           "22222222-2222-2222-2222-222222222222",
		tenantID:     "11111111-1111-1111-1111-111111111111",
		objectTypeID: "33333333-3333-3333-3333-333333333333",
		properties:   []byte(`{"name":"Widget","count":3,"active":true}`),
		version:      7,
		createdAt:    created,
		updatedAt:    updated,
	}
	object, err := scanObjectRow(row)
	if err != nil {
		t.Fatalf("scanObjectRow error = %v", err)
	}
	if object.ID.String() != row.id || object.TenantID.String() != row.tenantID || object.ObjectTypeID.String() != row.objectTypeID {
		t.Fatalf("object ids = %+v", object)
	}
	if object.Version != 7 || object.Properties["name"] != "Widget" || object.Properties["active"] != true {
		t.Fatalf("object = %+v", object)
	}
	if !object.CreatedAt.Equal(created) || !object.UpdatedAt.Equal(updated) {
		t.Fatalf("timestamps = %v, %v", object.CreatedAt, object.UpdatedAt)
	}
}

func TestScanObjectRowInvalidProperties(t *testing.T) {
	row := fakeScanRow{
		id:           "22222222-2222-2222-2222-222222222222",
		tenantID:     "11111111-1111-1111-1111-111111111111",
		objectTypeID: "33333333-3333-3333-3333-333333333333",
		properties:   []byte(`not-json`),
	}
	if _, err := scanObjectRow(row); err == nil {
		t.Fatal("scanObjectRow accepted invalid properties JSON")
	}
}

func TestTenantFilter(t *testing.T) {
	got := TenantFilter("11111111-1111-1111-1111-111111111111")
	if got != `tenant_id = "11111111-1111-1111-1111-111111111111"` {
		t.Fatalf("TenantFilter = %q", got)
	}
}

func TestRebuildWaitTimeoutBounds(t *testing.T) {
	if got := rebuildWaitTimeout(Config{SyncWindow: time.Second}); got != 10*time.Second {
		t.Fatalf("short sync timeout = %s, want 10s", got)
	}
	if got := rebuildWaitTimeout(Config{SyncWindow: 5 * time.Second}); got != 20*time.Second {
		t.Fatalf("default sync timeout = %s, want 20s", got)
	}
	if got := rebuildWaitTimeout(Config{SyncWindow: time.Minute}); got != 2*time.Minute {
		t.Fatalf("long sync timeout = %s, want 2m", got)
	}
}

func TestRebuildApplierWaitsForTask(t *testing.T) {
	// The concrete adapter is exercised through a fake RebuildClient so the
	// task-waiting contract is covered without a live Meilisearch.
	fake := &fakeRebuildClient{}
	applier := rebuildApplier{client: fake, wait: time.Second}
	if err := applier.UpsertDocuments(t.Context(), "staging", []Document{{ID: "1"}}); err != nil {
		t.Fatalf("UpsertDocuments error = %v", err)
	}
	if fake.upsertIndex != "staging" || fake.waitTaskUID != "task-1" || fake.waitIndex != "staging" {
		t.Fatalf("upsert calls = index %q task %q wait index %q", fake.upsertIndex, fake.waitTaskUID, fake.waitIndex)
	}
	if err := applier.DeleteDocuments(t.Context(), "staging", []string{"1"}); err != nil {
		t.Fatalf("DeleteDocuments error = %v", err)
	}
	if fake.deleteIndex != "staging" || fake.waitTaskUID != "task-2" {
		t.Fatalf("delete calls = index %q task %q", fake.deleteIndex, fake.waitTaskUID)
	}
}

type fakeRebuildClient struct {
	upsertIndex string
	deleteIndex string
	waitIndex   string
	waitTaskUID string
}

func (f *fakeRebuildClient) EnsureIndex(_ context.Context, _ string, _ json.RawMessage) error {
	return nil
}
func (f *fakeRebuildClient) DeleteIndex(_ context.Context, _ string) error { return nil }
func (f *fakeRebuildClient) UpsertDocuments(_ context.Context, indexUID string, _ []Document) (string, error) {
	f.upsertIndex = indexUID
	return "task-1", nil
}
func (f *fakeRebuildClient) DeleteDocuments(_ context.Context, indexUID string, _ []string) (string, error) {
	f.deleteIndex = indexUID
	return "task-2", nil
}
func (f *fakeRebuildClient) Search(_ context.Context, _ string, _ SearchRequest) (SearchResult, error) {
	return SearchResult{}, nil
}
func (f *fakeRebuildClient) SwapIndexes(_ context.Context, _ [][2]string) error { return nil }
func (f *fakeRebuildClient) WaitTask(_ context.Context, indexUID, taskUID string, _ time.Duration) error {
	f.waitIndex = indexUID
	f.waitTaskUID = taskUID
	return nil
}

func TestSummaryJSONFields(t *testing.T) {
	summary := Summary{IndexUID: "objects_v1_abc", Documents: 5}
	if summary.IndexUID == "" || !strings.Contains(summary.IndexUID, "objects_") {
		t.Fatalf("summary = %+v", summary)
	}
}
