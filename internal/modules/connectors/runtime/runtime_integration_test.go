//go:build integration

package runtime_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
)

// These tests talk to a real Kafka Connect worker (local connector-runtime
// profile) and a real Iceberg catalog. They are gated behind
// ONTOAXIS_CONNECT_INTEGRATION=1 so the default unit suite never needs Docker.
//
//	ONTOAXIS_CONNECT_INTEGRATION=1 \
//	ONTOAXIS_CONNECT_IT_URL=http://127.0.0.1:18089 \
//	go test -tags=integration ./internal/modules/connectors/runtime/ -run Integration -v

const integrationTenant = "11111111-1111-4111-8111-111111111111"

const validSourceSecret = `{"username":"ontoaxis_source","password":"ontoaxis_source_local","iceberg_catalog_password":"ontoaxis"}`

func integrationConnectURL() string {
	url := strings.TrimSpace(os.Getenv("ONTOAXIS_CONNECT_IT_URL"))
	if url == "" {
		return "http://127.0.0.1:18089"
	}
	return url
}

func newIntegrationRuntime(t *testing.T, secretJSON, connectURL string, timeout time.Duration) *runtime.Runtime {
	t.Helper()
	if os.Getenv("ONTOAXIS_CONNECT_INTEGRATION") != "1" {
		t.Skip("set ONTOAXIS_CONNECT_INTEGRATION=1 to run Kafka Connect integration tests")
	}
	t.Setenv("ONTOAXIS_SECRET_SOURCEPG", secretJSON)

	rt, err := runtime.NewRuntime(runtime.Config{
		ConnectURL:      connectURL,
		ConnectTimeout:  timeout,
		ConnectRetryMax: 0,
		Warehouse: runtime.Warehouse{
			CatalogURI:    "jdbc:postgresql://postgres:5432/ontoaxis",
			WarehousePath: "file:///warehouse",
			Namespace:     "ontoaxis",
			CatalogUser:   "ontoaxis",
		},
	}, runtime.WithSecretResolver(runtime.NewEnvSecretResolver()))
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return rt
}

func integrationConnector(t *testing.T) *connectordomain.Connector {
	t.Helper()
	return &connectordomain.Connector{
		ID:        uuid.NewString(),
		TenantID:  integrationTenant,
		Name:      "it-" + uuid.NewString()[:8],
		Type:      connectordomain.TypePostgres,
		SecretRef: "SOURCEPG",
		Status:    connectordomain.StatusActive,
		Config: map[string]any{
			"host":             "source-postgres",
			"port":             float64(5432),
			"database":         "ontoaxis_source",
			"schema":           "public",
			"tables":           []any{"customers"},
			"publication_name": "oa_publication",
			"dataset_id":       uuid.NewString(),
		},
	}
}

// integrationSourceDSN points at the synthetic source database from the host.
func integrationSourceDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("ONTOAXIS_SOURCE_PG_DSN")); dsn != "" {
		return dsn
	}
	return "postgres://ontoaxis_source:ontoaxis_source_local@127.0.0.1:15434/ontoaxis_source?sslmode=disable"
}

// cleanupInactiveIntegrationSlots drops leftover inactive replication slots
// created by earlier integration runs for this tenant. Every run uses a random
// connector id, so slots would otherwise exhaust max_replication_slots.
func cleanupInactiveIntegrationSlots(t *testing.T) {
	t.Helper()
	names, err := runtime.NewNaming().NamesFor(integrationTenant, uuid.NewString())
	if err != nil {
		t.Fatalf("NamesFor() error = %v", err)
	}
	prefix := "oa_" + names.TenantHash + "_%"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, integrationSourceDSN())
	if err != nil {
		t.Logf("skipping replication slot cleanup: %v", err)
		return
	}
	defer func() { _ = conn.Close(ctx) }()

	rows, err := conn.Query(ctx, `SELECT slot_name FROM pg_replication_slots WHERE NOT active AND slot_name LIKE $1`, prefix)
	if err != nil {
		t.Fatalf("list replication slots: %v", err)
	}
	var slots []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan replication slot: %v", err)
		}
		slots = append(slots, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate replication slots: %v", err)
	}
	for _, name := range slots {
		if _, err := conn.Exec(ctx, `SELECT pg_drop_replication_slot($1)`, name); err != nil {
			t.Fatalf("drop replication slot %q: %v", name, err)
		}
	}
	t.Logf("dropped %d inactive integration replication slot(s)", len(slots))
}

func waitFor(t *testing.T, timeout time.Duration, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestIntegrationSubmitStatusStop(t *testing.T) {
	ctx := context.Background()
	rt := newIntegrationRuntime(t, validSourceSecret, integrationConnectURL(), 10*time.Second)
	cleanupInactiveIntegrationSlots(t)
	t.Cleanup(func() { cleanupInactiveIntegrationSlots(t) })
	connector := integrationConnector(t)

	ref, err := rt.Submit(ctx, integrationTenant, connector)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if ref == "" {
		t.Fatal("Submit() returned an empty runtime reference")
	}
	t.Cleanup(func() { _ = rt.Stop(context.Background(), integrationTenant, ref) })

	waitFor(t, 120*time.Second, "source and sink RUNNING", func() bool {
		status, err := rt.Status(ctx, integrationTenant, ref)
		if err != nil {
			t.Logf("Status() error (retrying): %v", err)
			return false
		}
		t.Logf("status source=%s sink=%s", status.Source, status.Sink)
		return status.Source == runtime.StateRunning && status.Sink == runtime.StateRunning
	})

	// Upserting the same connector must be idempotent and keep the same runtime
	// reference (deterministic naming).
	again, err := rt.Submit(ctx, integrationTenant, connector)
	if err != nil {
		t.Fatalf("second Submit() error = %v", err)
	}
	if again != ref {
		t.Fatalf("second Submit() ref = %q, want %q", again, ref)
	}

	if err := rt.Stop(ctx, integrationTenant, ref); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	waitFor(t, 60*time.Second, "runtime resources removed", func() bool {
		status, err := rt.Status(ctx, integrationTenant, ref)
		if err != nil {
			return true // an unreachable/missing connector is the expected end state
		}
		return status.Source == runtime.StateMissing && status.Sink == runtime.StateMissing
	})
}

func TestIntegrationBadCredentialsDoNotLeak(t *testing.T) {
	ctx := context.Background()
	const badSecret = `{"username":"ontoaxis_source","password":"wrong-password-marker","iceberg_catalog_password":"ontoaxis"}`
	rt := newIntegrationRuntime(t, badSecret, integrationConnectURL(), 10*time.Second)
	connector := integrationConnector(t)

	ref, err := rt.Submit(ctx, integrationTenant, connector)
	if err != nil {
		// Submit may fail immediately; the error must never echo the credential.
		if strings.Contains(err.Error(), "wrong-password-marker") {
			t.Fatalf("Submit() error leaked the credential: %v", err)
		}
		return
	}
	t.Cleanup(func() { _ = rt.Stop(context.Background(), integrationTenant, ref) })

	// Otherwise the source task must fail, and the failure must stay redacted.
	waitFor(t, 120*time.Second, "source task FAILED", func() bool {
		status, err := rt.Status(ctx, integrationTenant, ref)
		if err != nil {
			if strings.Contains(err.Error(), "wrong-password-marker") {
				t.Fatalf("Status() error leaked the credential: %v", err)
			}
			return false
		}
		return status.Source == runtime.StateFailed || status.Sink == runtime.StateFailed
	})
}

func TestIntegrationConnectUnavailable(t *testing.T) {
	ctx := context.Background()
	rt := newIntegrationRuntime(t, validSourceSecret, "http://127.0.0.1:1", 500*time.Millisecond)
	connector := integrationConnector(t)

	_, err := rt.Submit(ctx, integrationTenant, connector)
	if err == nil {
		t.Fatal("Submit() error = nil, want unavailable when Kafka Connect is down")
	}
	if !errors.Is(err, runtime.ErrConnectUnavailable) {
		t.Fatalf("Submit() error = %v, want %v", err, runtime.ErrConnectUnavailable)
	}
	if strings.Contains(err.Error(), "ontoaxis_source_local") {
		t.Fatalf("Submit() error leaked a credential: %v", err)
	}
}
