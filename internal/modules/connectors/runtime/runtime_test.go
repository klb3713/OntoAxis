package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
)

const testSecretRef = "prod/pg-main"

func testSecrets() map[string]string {
	return map[string]string{
		"username":                 "cdc_user",
		"password":                 "cdc_pass",
		"iceberg_catalog_password": "iceberg_pass",
	}
}

type fakeResolver struct {
	values  map[string]string
	err     error
	calls   int
	refs    []string
	tenants []string
}

func (f *fakeResolver) Resolve(_ context.Context, tenantID, secretRef string) (map[string]string, error) {
	f.calls++
	f.refs = append(f.refs, secretRef)
	f.tenants = append(f.tenants, tenantID)
	if f.err != nil {
		return nil, f.err
	}
	return f.values, nil
}

type upsertCall struct {
	name   string
	config map[string]any
}

type fakeClient struct {
	mu sync.Mutex

	upserts     []upsertCall
	stops       []string
	deletes     []string
	statusCalls []string

	statuses  map[string]runtime.State
	statusErr map[string]error
	upsertErr map[string]error
	stopErr   map[string]error
	deleteErr map[string]error
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		statuses:  make(map[string]runtime.State),
		statusErr: make(map[string]error),
		upsertErr: make(map[string]error),
		stopErr:   make(map[string]error),
		deleteErr: make(map[string]error),
	}
}

func (f *fakeClient) UpsertConnector(_ context.Context, name string, config map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upserts = append(f.upserts, upsertCall{name: name, config: config})
	return f.upsertErr[name]
}

func (f *fakeClient) ConnectorStatus(_ context.Context, name string) (runtime.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusCalls = append(f.statusCalls, name)
	if err := f.statusErr[name]; err != nil {
		return runtime.StateUnknown, err
	}
	state, ok := f.statuses[name]
	if !ok {
		return runtime.StateMissing, nil
	}
	return state, nil
}

func (f *fakeClient) StopConnector(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops = append(f.stops, name)
	return f.stopErr[name]
}

func (f *fakeClient) DeleteConnector(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, name)
	return f.deleteErr[name]
}

func (f *fakeClient) upsertNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.upserts))
	for _, call := range f.upserts {
		names = append(names, call.name)
	}
	return names
}

type fakeHealthClient struct {
	*fakeClient
	pings   int
	pingErr error
}

func (f *fakeHealthClient) Ping(context.Context) error {
	f.pings++
	return f.pingErr
}

func testConnector(tenantID, connectorID string) *domain.Connector {
	return &domain.Connector{
		ID:        connectorID,
		TenantID:  tenantID,
		Name:      "pg-source",
		Type:      domain.TypePostgres,
		Status:    domain.StatusActive,
		SecretRef: testSecretRef,
		Config:    validSyncConfigMap(),
	}
}

func newTestRuntime(t *testing.T, client runtime.Client, resolver runtime.SecretResolver, opts ...runtime.Option) *runtime.Runtime {
	t.Helper()
	base := []runtime.Option{
		runtime.WithConnectClient(client),
		runtime.WithSecretResolver(resolver),
		runtime.WithWarehouse(testWarehouse()),
	}
	rt, err := runtime.NewRuntime(runtime.Config{}, append(base, opts...)...)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return rt
}

func TestSubmitOrchestratesSourceThenSink(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	resolver := &fakeResolver{values: testSecrets()}
	rt := newTestRuntime(t, client, resolver)

	ref, err := rt.Submit(context.Background(), testTenantA, testConnector(testTenantA, testConnA))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if want := "oa-fa1ffa66-2ff3a64e-sink"; ref != want {
		t.Fatalf("Submit() runtimeRef = %q, want %q", ref, want)
	}
	if strings.Contains(ref, "cdc_pass") || strings.Contains(ref, "iceberg_pass") {
		t.Fatalf("runtimeRef leaks credentials: %q", ref)
	}

	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls)
	}
	if resolver.refs[0] != testSecretRef || resolver.tenants[0] != testTenantA {
		t.Fatalf("resolver called with tenant=%q ref=%q", resolver.tenants[0], resolver.refs[0])
	}

	names := client.upsertNames()
	wantNames := []string{"oa-fa1ffa66-2ff3a64e-source", "oa-fa1ffa66-2ff3a64e-sink"}
	if len(names) != len(wantNames) || names[0] != wantNames[0] || names[1] != wantNames[1] {
		t.Fatalf("upsert order = %v, want %v", names, wantNames)
	}

	source := client.upserts[0].config
	if source["connector.class"] != runtime.DebeziumPostgresConnectorClass {
		t.Errorf("source connector.class = %v", source["connector.class"])
	}
	if source["database.password"] != "cdc_pass" {
		t.Errorf("source database.password = %v", source["database.password"])
	}
	sink := client.upserts[1].config
	if sink["connector.class"] != runtime.IcebergSinkConnectorClass {
		t.Errorf("sink connector.class = %v", sink["connector.class"])
	}
	if sink["iceberg.catalog.jdbc.password"] != "iceberg_pass" {
		t.Errorf("sink iceberg.catalog.jdbc.password = %v", sink["iceberg.catalog.jdbc.password"])
	}
}

func TestSubmitIdempotent(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	resolver := &fakeResolver{values: testSecrets()}
	rt := newTestRuntime(t, client, resolver)

	connector := testConnector(testTenantA, testConnA)
	first, err := rt.Submit(context.Background(), testTenantA, connector)
	if err != nil {
		t.Fatalf("Submit() first error = %v", err)
	}
	second, err := rt.Submit(context.Background(), testTenantA, connector)
	if err != nil {
		t.Fatalf("Submit() second error = %v", err)
	}
	if first != second {
		t.Fatalf("repeated Submit() returned different refs: %q != %q", first, second)
	}
	if len(client.upserts) != 4 {
		t.Fatalf("upserts = %d, want 4 (source+sink twice)", len(client.upserts))
	}
}

func TestSubmitSinkFailureRollsBackSource(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	sinkName := "oa-fa1ffa66-2ff3a64e-sink"
	client.upsertErr[sinkName] = errors.New("kafka connect rejected sink")
	resolver := &fakeResolver{values: testSecrets()}
	rt := newTestRuntime(t, client, resolver)

	ref, err := rt.Submit(context.Background(), testTenantA, testConnector(testTenantA, testConnA))
	if err == nil {
		t.Fatal("Submit() error = nil, want sink failure")
	}
	if ref != "" {
		t.Fatalf("Submit() runtimeRef = %q, want empty on failure", ref)
	}
	if len(client.deletes) != 1 || client.deletes[0] != "oa-fa1ffa66-2ff3a64e-source" {
		t.Fatalf("rollback deletes = %v, want [source]", client.deletes)
	}
	for _, secret := range []string{"cdc_pass", "iceberg_pass"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("Submit() error leaks credential %q: %v", secret, err)
		}
	}
}

func TestSubmitPreflightRejections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		tenantID  string
		connector func() *domain.Connector
		wantErr   error
	}{
		{
			name:     "invalid tenant",
			tenantID: "tenant-a",
			connector: func() *domain.Connector {
				return testConnector(testTenantA, testConnA)
			},
			wantErr: runtime.ErrInvalidTenant,
		},
		{
			name:     "nil connector",
			tenantID: testTenantA,
			connector: func() *domain.Connector {
				return nil
			},
			wantErr: runtime.ErrInvalidConnector,
		},
		{
			name:     "foreign tenant connector",
			tenantID: testTenantA,
			connector: func() *domain.Connector {
				return testConnector(testTenantB, testConnA)
			},
			wantErr: domain.ErrNotFound,
		},
		{
			name:     "unsupported connector type",
			tenantID: testTenantA,
			connector: func() *domain.Connector {
				c := testConnector(testTenantA, testConnA)
				c.Type = domain.TypeMySQL
				return c
			},
			wantErr: runtime.ErrUnsupportedType,
		},
		{
			name:     "inactive connector",
			tenantID: testTenantA,
			connector: func() *domain.Connector {
				c := testConnector(testTenantA, testConnA)
				c.Status = domain.StatusPaused
				return c
			},
			wantErr: domain.ErrConflict,
		},
		{
			name:     "invalid sync config",
			tenantID: testTenantA,
			connector: func() *domain.Connector {
				c := testConnector(testTenantA, testConnA)
				c.Config = map[string]any{"host": "db", "port": float64(5432)}
				return c
			},
			wantErr: runtime.ErrInvalidSyncConfig,
		},
		{
			name:     "missing secret ref",
			tenantID: testTenantA,
			connector: func() *domain.Connector {
				c := testConnector(testTenantA, testConnA)
				c.SecretRef = ""
				return c
			},
			wantErr: runtime.ErrSecretUnavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newFakeClient()
			resolver := &fakeResolver{values: testSecrets()}
			rt := newTestRuntime(t, client, resolver)

			if _, err := rt.Submit(context.Background(), tt.tenantID, tt.connector()); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Submit() error = %v, want %v", err, tt.wantErr)
			}
			if len(client.upserts) != 0 {
				t.Fatalf("rejected submit still upserted connectors: %v", client.upsertNames())
			}
		})
	}
}

func TestSubmitResolverFailureCreatesNoRuntime(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	resolver := &fakeResolver{err: fmt.Errorf("%w: backend unavailable", runtime.ErrSecretUnavailable)}
	rt := newTestRuntime(t, client, resolver)

	if _, err := rt.Submit(context.Background(), testTenantA, testConnector(testTenantA, testConnA)); !errors.Is(err, runtime.ErrSecretUnavailable) {
		t.Fatalf("Submit() error = %v, want %v", err, runtime.ErrSecretUnavailable)
	}
	if len(client.upserts) != 0 {
		t.Fatalf("secret failure still upserted connectors: %v", client.upsertNames())
	}
}

func TestPreflightHealthCheck(t *testing.T) {
	t.Parallel()

	t.Run("reachable", func(t *testing.T) {
		t.Parallel()
		client := &fakeHealthClient{fakeClient: newFakeClient()}
		resolver := &fakeResolver{values: testSecrets()}
		rt := newTestRuntime(t, client, resolver)

		if err := rt.Preflight(context.Background(), testTenantA, testConnector(testTenantA, testConnA)); err != nil {
			t.Fatalf("Preflight() error = %v", err)
		}
		if client.pings != 1 {
			t.Errorf("Ping() calls = %d, want 1", client.pings)
		}
		if len(client.upserts) != 0 {
			t.Errorf("Preflight() must not create runtime connectors")
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		t.Parallel()
		client := &fakeHealthClient{fakeClient: newFakeClient(), pingErr: fmt.Errorf("%w: connect down", runtime.ErrConnectUnavailable)}
		rt := newTestRuntime(t, client, &fakeResolver{values: testSecrets()})

		if err := rt.Preflight(context.Background(), testTenantA, testConnector(testTenantA, testConnA)); !errors.Is(err, runtime.ErrConnectUnavailable) {
			t.Fatalf("Preflight() error = %v, want %v", err, runtime.ErrConnectUnavailable)
		}
		if len(client.upserts) != 0 {
			t.Errorf("Preflight() must not create runtime connectors")
		}
	})
}

func TestPreflightStatusProbeFallback(t *testing.T) {
	t.Parallel()

	t.Run("probe reaches connect", func(t *testing.T) {
		t.Parallel()
		client := newFakeClient()
		rt := newTestRuntime(t, client, &fakeResolver{values: testSecrets()})
		if err := rt.Preflight(context.Background(), testTenantA, testConnector(testTenantA, testConnA)); err != nil {
			t.Fatalf("Preflight() error = %v", err)
		}
	})

	t.Run("probe fails", func(t *testing.T) {
		t.Parallel()
		client := newFakeClient()
		client.statusErr["oa-fa1ffa66-2ff3a64e-source"] = fmt.Errorf("%w: unreachable", runtime.ErrConnectUnavailable)
		rt := newTestRuntime(t, client, &fakeResolver{values: testSecrets()})
		if err := rt.Preflight(context.Background(), testTenantA, testConnector(testTenantA, testConnA)); !errors.Is(err, runtime.ErrConnectUnavailable) {
			t.Fatalf("Preflight() error = %v, want %v", err, runtime.ErrConnectUnavailable)
		}
	})
}

func TestStopStopsAndDeletesBothConnectors(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	rt := newTestRuntime(t, client, &fakeResolver{values: testSecrets()})
	ref := mustNames(t).Sink

	if err := rt.Stop(context.Background(), testTenantA, ref); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	want := []string{"oa-fa1ffa66-2ff3a64e-source", "oa-fa1ffa66-2ff3a64e-sink"}
	for i, name := range want {
		if client.stops[i] != name {
			t.Errorf("Stop() stop call %d = %q, want %q", i, client.stops[i], name)
		}
		if client.deletes[i] != name {
			t.Errorf("Stop() delete call %d = %q, want %q", i, client.deletes[i], name)
		}
	}
}

func TestStopTenantIsolation(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	rt := newTestRuntime(t, client, &fakeResolver{values: testSecrets()})

	if err := rt.Stop(context.Background(), testTenantB, mustNames(t).Sink); !errors.Is(err, runtime.ErrInvalidRuntimeRef) {
		t.Fatalf("Stop() error = %v, want %v", err, runtime.ErrInvalidRuntimeRef)
	}
	if len(client.stops) != 0 || len(client.deletes) != 0 {
		t.Fatalf("cross-tenant Stop() touched runtime connectors: stops=%v deletes=%v", client.stops, client.deletes)
	}
}

func TestStopBestEffortContinuesOnFailure(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	client.stopErr["oa-fa1ffa66-2ff3a64e-source"] = errors.New("stop failed")
	client.deleteErr["oa-fa1ffa66-2ff3a64e-sink"] = errors.New("delete failed")
	rt := newTestRuntime(t, client, &fakeResolver{values: testSecrets()})

	err := rt.Stop(context.Background(), testTenantA, mustNames(t).Sink)
	if err == nil {
		t.Fatal("Stop() error = nil, want joined failures")
	}
	if len(client.stops) != 2 || len(client.deletes) != 2 {
		t.Fatalf("Stop() must attempt all operations: stops=%v deletes=%v", client.stops, client.deletes)
	}
}

func TestStopMissingRuntimeIsNoop(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	rt := newTestRuntime(t, client, &fakeResolver{values: testSecrets()})
	if err := rt.Stop(context.Background(), testTenantA, mustNames(t).Sink); err != nil {
		t.Fatalf("Stop() error = %v, want nil for absent runtime connectors", err)
	}
}

func TestStatusNormalizesSourceAndSink(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	client.statuses["oa-fa1ffa66-2ff3a64e-source"] = runtime.StateRunning
	client.statuses["oa-fa1ffa66-2ff3a64e-sink"] = runtime.StateFailed
	rt := newTestRuntime(t, client, &fakeResolver{values: testSecrets()})

	status, err := rt.Status(context.Background(), testTenantA, mustNames(t).Sink)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Source != runtime.StateRunning || status.Sink != runtime.StateFailed {
		t.Fatalf("Status() = %+v, want source=running sink=failed", status)
	}
}

func TestStatusTenantIsolation(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	rt := newTestRuntime(t, client, &fakeResolver{values: testSecrets()})

	if _, err := rt.Status(context.Background(), testTenantB, mustNames(t).Sink); !errors.Is(err, runtime.ErrInvalidRuntimeRef) {
		t.Fatalf("Status() error = %v, want %v", err, runtime.ErrInvalidRuntimeRef)
	}
	if len(client.statusCalls) != 0 {
		t.Fatalf("cross-tenant Status() read runtime connectors: %v", client.statusCalls)
	}
}

func TestNewRuntimeRequiresConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := runtime.NewRuntime(runtime.Config{}); !errors.Is(err, runtime.ErrRuntimeNotConfigured) {
		t.Fatalf("NewRuntime() error = %v, want %v", err, runtime.ErrRuntimeNotConfigured)
	}
	if _, err := runtime.NewRuntime(runtime.Config{ConnectURL: "http://127.0.0.1:8083"}); !errors.Is(err, runtime.ErrRuntimeNotConfigured) {
		t.Fatalf("NewRuntime() without warehouse error = %v, want %v", err, runtime.ErrRuntimeNotConfigured)
	}
}

func TestVerifyMaxRowsDefault(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(t, newFakeClient(), &fakeResolver{values: testSecrets()})
	if got, want := rt.VerifyMaxRows(), 1_000_000; got != want {
		t.Fatalf("VerifyMaxRows() = %d, want %d", got, want)
	}
}
