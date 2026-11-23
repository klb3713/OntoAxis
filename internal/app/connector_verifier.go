package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	connectorsruntime "github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
	ingestionruntime "github.com/klb3713/ontoaxis/internal/modules/ingestion/runtime"
)

// catalogSnapshotReader reports how many rows the latest committed Iceberg
// snapshot of one Dataset holds. found=false means the table exists but no
// snapshot has been committed yet.
type catalogSnapshotReader interface {
	LatestSnapshotRows(ctx context.Context, datasetID, schema string, tables []string) (rows int64, found bool, err error)
}

// connectorConfigReader resolves a connector's persisted config. Sync jobs do
// not carry their config, so the verifier falls back to the connector record.
type connectorConfigReader interface {
	GetConnector(ctx context.Context, tenantID, id string) (*connectordomain.Connector, error)
}

// snapshotVerifier adapts the Iceberg catalog to the ingestion SnapshotVerifier
// port. It only reports a snapshot as done once every configured table has a
// committed snapshot; row counts are the sink-side total-records, so callers
// must not treat them as a source/sink equality check.
type snapshotVerifier struct {
	reader     catalogSnapshotReader
	connectors connectorConfigReader
}

var _ ingestionruntime.SnapshotVerifier = snapshotVerifier{}

func (v snapshotVerifier) VerifySnapshot(ctx context.Context, job *domain.SyncJob, _ string) (int64, bool, error) {
	if v.reader == nil {
		return 0, false, errors.New("snapshot verifier is not configured")
	}
	if job == nil {
		return 0, false, errors.New("snapshot verifier requires a sync job")
	}
	cfg, err := syncJobVerifyConfig(job.Config)
	if err != nil && v.connectors != nil && job.ConnectorID != "" {
		connector, lookupErr := v.connectors.GetConnector(ctx, job.TenantID, job.ConnectorID)
		if lookupErr != nil {
			return 0, false, fmt.Errorf("resolve connector for verification: %w", lookupErr)
		}
		cfg, err = syncJobVerifyConfig(connector.Config)
	}
	if err != nil {
		return 0, false, err
	}
	return v.reader.LatestSnapshotRows(ctx, cfg.DatasetID, cfg.Schema, cfg.Tables)
}

// verifyConfig is the subset of a sync job config needed to locate the sink
// tables. Values are decoded from the persisted JSON map.
type verifyConfig struct {
	DatasetID string
	Schema    string
	Tables    []string
}

func syncJobVerifyConfig(raw map[string]any) (verifyConfig, error) {
	if raw == nil {
		return verifyConfig{}, errors.New("sync job has no config")
	}
	datasetID, ok := raw["dataset_id"].(string)
	if !ok || strings.TrimSpace(datasetID) == "" {
		return verifyConfig{}, errors.New("sync job config is missing dataset_id")
	}
	schema, _ := raw["schema"].(string)
	schema = strings.TrimSpace(schema)
	if schema == "" {
		schema = "public"
	}
	var tables []string
	switch value := raw["tables"].(type) {
	case []any:
		for _, item := range value {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return verifyConfig{}, errors.New("sync job config has an invalid table entry")
			}
			tables = append(tables, text)
		}
	case []string:
		tables = append(tables, value...)
	default:
		return verifyConfig{}, errors.New("sync job config is missing tables")
	}
	if len(tables) == 0 {
		return verifyConfig{}, errors.New("sync job config is missing tables")
	}
	return verifyConfig{DatasetID: datasetID, Schema: schema, Tables: tables}, nil
}

// icebergCatalogReader reads committed snapshot row counts from the Iceberg
// JDBC catalog and the shared warehouse. It supports file:// warehouses only;
// object storage would need the corresponding client and is out of scope here.
type icebergCatalogReader struct {
	pool          *pgxpool.Pool
	warehouseRoot string
}

var _ catalogSnapshotReader = icebergCatalogReader{}

func newIcebergCatalogReader(pool *pgxpool.Pool, warehouseURI string) (icebergCatalogReader, error) {
	if pool == nil {
		return icebergCatalogReader{}, errors.New("iceberg catalog reader requires a database pool")
	}
	root, err := localWarehousePath(warehouseURI)
	if err != nil {
		return icebergCatalogReader{}, err
	}
	return icebergCatalogReader{pool: pool, warehouseRoot: root}, nil
}

func (r icebergCatalogReader) LatestSnapshotRows(ctx context.Context, datasetID, schema string, tables []string) (int64, bool, error) {
	namespace, err := connectorsruntime.DatasetNamespace(datasetID, schema)
	if err != nil {
		return 0, false, fmt.Errorf("resolve dataset namespace: %w", err)
	}
	var total int64
	for _, table := range tables {
		location, found, err := r.metadataLocation(ctx, namespace, table)
		if err != nil {
			return 0, false, err
		}
		if !found {
			return 0, false, nil
		}
		rows, committed, err := r.snapshotRows(location)
		if err != nil {
			return 0, false, err
		}
		if !committed {
			return 0, false, nil
		}
		total += rows
	}
	return total, true, nil
}

func (r icebergCatalogReader) metadataLocation(ctx context.Context, namespace, table string) (string, bool, error) {
	var location string
	err := r.pool.QueryRow(ctx,
		`SELECT metadata_location FROM public.iceberg_tables WHERE table_namespace = $1 AND table_name = $2`,
		namespace, table,
	).Scan(&location)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read iceberg catalog row: %w", err)
	}
	if strings.TrimSpace(location) == "" {
		return "", false, nil
	}
	return location, true, nil
}

// snapshotRows parses one metadata JSON file and returns the latest snapshot's
// total-records. committed=false means the table has no snapshot yet.
func (r icebergCatalogReader) snapshotRows(metadataLocation string) (int64, bool, error) {
	path, err := localWarehousePath(metadataLocation)
	if err != nil {
		return 0, false, err
	}
	if !strings.HasPrefix(path, r.warehouseRoot+"/") && path != r.warehouseRoot {
		return 0, false, fmt.Errorf("metadata path escapes the warehouse root")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false, fmt.Errorf("read iceberg metadata: %w", err)
	}
	var metadata struct {
		CurrentSnapshotID *int64 `json:"current-snapshot-id"`
		Snapshots         []struct {
			SnapshotID int64             `json:"snapshot-id"`
			Summary    map[string]string `json:"summary"`
		} `json:"snapshots"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return 0, false, fmt.Errorf("parse iceberg metadata: %w", err)
	}
	if metadata.CurrentSnapshotID == nil || len(metadata.Snapshots) == 0 {
		return 0, false, nil
	}
	for _, snapshot := range metadata.Snapshots {
		if snapshot.SnapshotID != *metadata.CurrentSnapshotID {
			continue
		}
		var rows int64
		if _, err := fmt.Sscanf(snapshot.Summary["total-records"], "%d", &rows); err != nil {
			return 0, false, fmt.Errorf("parse snapshot total-records: %w", err)
		}
		return rows, true, nil
	}
	return 0, false, nil
}

// localWarehousePath converts a file URI into a local path and rejects other
// schemes; S3/GCS warehouses need their own reader.
func localWarehousePath(uri string) (string, error) {
	trimmed := strings.TrimSpace(uri)
	switch {
	case strings.HasPrefix(trimmed, "file://"):
		path := strings.TrimPrefix(trimmed, "file://")
		if path == "" || !strings.HasPrefix(path, "/") {
			return "", fmt.Errorf("unsupported file warehouse uri: %q", trimmed)
		}
		return strings.TrimRight(path, "/"), nil
	case strings.HasPrefix(trimmed, "file:"):
		path := strings.TrimPrefix(trimmed, "file:")
		if path == "" || !strings.HasPrefix(path, "/") {
			return "", fmt.Errorf("unsupported file warehouse uri: %q", trimmed)
		}
		return strings.TrimRight(path, "/"), nil
	case strings.HasPrefix(trimmed, "/"):
		return strings.TrimRight(trimmed, "/"), nil
	default:
		return "", fmt.Errorf("unsupported warehouse uri scheme (only file:// is supported by the snapshot verifier): %q", trimmed)
	}
}
