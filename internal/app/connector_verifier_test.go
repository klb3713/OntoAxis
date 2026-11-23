package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

type fakeSnapshotReader struct {
	rows  int64
	found bool
	err   error
	calls []string
}

func (f *fakeSnapshotReader) LatestSnapshotRows(_ context.Context, datasetID, schema string, tables []string) (int64, bool, error) {
	f.calls = append(f.calls, datasetID+"|"+schema)
	_ = tables
	return f.rows, f.found, f.err
}

func TestSyncJobVerifyConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     map[string]any
		wantErr bool
		dataset string
		schema  string
		tables  int
	}{
		{
			name:    "valid",
			raw:     map[string]any{"dataset_id": "019c0002-0000-7000-8000-000000000003", "schema": "public", "tables": []any{"customers", "orders"}},
			dataset: "019c0002-0000-7000-8000-000000000003",
			schema:  "public",
			tables:  2,
		},
		{
			name:    "schema defaults to public",
			raw:     map[string]any{"dataset_id": "019c0002-0000-7000-8000-000000000003", "tables": []string{"customers"}},
			dataset: "019c0002-0000-7000-8000-000000000003",
			schema:  "public",
			tables:  1,
		},
		{name: "missing dataset", raw: map[string]any{"tables": []string{"customers"}}, wantErr: true},
		{name: "missing tables", raw: map[string]any{"dataset_id": "d"}, wantErr: true},
		{name: "empty table entry", raw: map[string]any{"dataset_id": "d", "tables": []any{""}}, wantErr: true},
		{name: "nil config", raw: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := syncJobVerifyConfig(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("syncJobVerifyConfig() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("syncJobVerifyConfig() error = %v", err)
			}
			if got.DatasetID != tt.dataset || got.Schema != tt.schema || len(got.Tables) != tt.tables {
				t.Fatalf("syncJobVerifyConfig() = %+v", got)
			}
		})
	}
}

func TestSnapshotVerifierPassesThroughReaderResult(t *testing.T) {
	t.Parallel()

	reader := &fakeSnapshotReader{rows: 5, found: true}
	verifier := snapshotVerifier{reader: reader}
	job := &domain.SyncJob{Config: map[string]any{
		"dataset_id": "019c0002-0000-7000-8000-000000000003",
		"schema":     "public",
		"tables":     []any{"customers"},
	}}

	rows, done, err := verifier.VerifySnapshot(context.Background(), job, "oa-sink")
	if err != nil {
		t.Fatalf("VerifySnapshot() error = %v", err)
	}
	if !done || rows != 5 {
		t.Fatalf("VerifySnapshot() = (%d, %v), want (5, true)", rows, done)
	}

	reader.found = false
	_, done, err = verifier.VerifySnapshot(context.Background(), job, "oa-sink")
	if err != nil || done {
		t.Fatalf("VerifySnapshot(uncommitted) = (_, %v, %v), want (false, nil)", done, err)
	}
}

func TestSnapshotVerifierRejectsMissingConfig(t *testing.T) {
	t.Parallel()

	verifier := snapshotVerifier{reader: &fakeSnapshotReader{}}
	if _, _, err := verifier.VerifySnapshot(context.Background(), &domain.SyncJob{}, "oa-sink"); err == nil {
		t.Fatal("VerifySnapshot() error = nil, want error for a job without config")
	}
	if _, _, err := (snapshotVerifier{}).VerifySnapshot(context.Background(), &domain.SyncJob{}, "oa-sink"); err == nil {
		t.Fatal("VerifySnapshot() error = nil, want error without a reader")
	}
}

func TestIcebergCatalogReaderSnapshotRows(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	reader := icebergCatalogReader{warehouseRoot: root}

	// No snapshot yet: the metadata exists but current-snapshot-id is -1.
	empty := writeMetadata(t, root, "empty", map[string]any{
		"current-snapshot-id": -1,
		"snapshots":           []any{},
	})
	rows, committed, err := reader.snapshotRows(empty)
	if err != nil {
		t.Fatalf("snapshotRows(empty) error = %v", err)
	}
	if committed || rows != 0 {
		t.Fatalf("snapshotRows(empty) = (%d, %v), want (0, false)", rows, committed)
	}

	committedPath := writeMetadata(t, root, "committed", map[string]any{
		"current-snapshot-id": 42,
		"snapshots": []any{
			map[string]any{"snapshot-id": 41, "summary": map[string]string{"total-records": "3"}},
			map[string]any{"snapshot-id": 42, "summary": map[string]string{"total-records": "5"}},
		},
	})
	rows, committed, err = reader.snapshotRows(committedPath)
	if err != nil {
		t.Fatalf("snapshotRows(committed) error = %v", err)
	}
	if !committed || rows != 5 {
		t.Fatalf("snapshotRows(committed) = (%d, %v), want (5, true)", rows, committed)
	}
}

func TestIcebergCatalogReaderRejectsPathOutsideWarehouse(t *testing.T) {
	t.Parallel()

	reader := icebergCatalogReader{warehouseRoot: t.TempDir()}
	if _, _, err := reader.snapshotRows("/etc/passwd"); err == nil {
		t.Fatal("snapshotRows() accepted a path outside the warehouse root")
	}
}

func TestLocalWarehousePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{name: "triple slash", uri: "file:///warehouse/", want: "/warehouse"},
		{name: "single slash", uri: "file:/warehouse", want: "/warehouse"},
		{name: "plain path", uri: "/data/warehouse", want: "/data/warehouse"},
		{name: "s3 unsupported", uri: "s3://bucket/warehouse", wantErr: true},
		{name: "empty", uri: "  ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := localWarehousePath(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("localWarehousePath(%q) error = nil, want error", tt.uri)
				}
				return
			}
			if err != nil {
				t.Fatalf("localWarehousePath(%q) error = %v", tt.uri, err)
			}
			if got != tt.want {
				t.Fatalf("localWarehousePath(%q) = %q, want %q", tt.uri, got, tt.want)
			}
		})
	}
}

func writeMetadata(t *testing.T, root, name string, payload map[string]any) string {
	t.Helper()
	dir := filepath.Join(root, name, "metadata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir metadata: %v", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	path := filepath.Join(dir, "00000-test.metadata.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	return path
}
