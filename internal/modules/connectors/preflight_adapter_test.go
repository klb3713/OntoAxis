package connectors_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/connectors"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/service"
)

const (
	adapterTenant    = "11111111-1111-7111-8111-111111111111"
	adapterConnector = "019c0000-0000-7000-8000-000000000001"
)

type stubResolver struct{}

func (stubResolver) Resolve(context.Context, string, string) (map[string]string, error) {
	return map[string]string{
		"username":                 "cdc_user",
		"password":                 "cdc_pass",
		"iceberg_catalog_password": "iceberg_pass",
	}, nil
}

type stubClient struct {
	mu      sync.Mutex
	pings   int
	pingErr error
	upserts int
}

func (c *stubClient) UpsertConnector(context.Context, string, map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.upserts++
	return nil
}

func (c *stubClient) ConnectorStatus(context.Context, string) (runtime.State, error) {
	return runtime.StateMissing, nil
}

func (c *stubClient) StopConnector(context.Context, string) error { return nil }

func (c *stubClient) DeleteConnector(context.Context, string) error { return nil }

func (c *stubClient) Ping(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pings++
	return c.pingErr
}

func (c *stubClient) upsertCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.upserts
}

type stubStarter struct {
	mu    sync.Mutex
	calls int
}

func (s *stubStarter) StartSync(context.Context, string, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return "job-1", nil
}

func (s *stubStarter) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type stubPreflight struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (p *stubPreflight) Preflight(context.Context, string, *domain.Connector) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.err
}

func (p *stubPreflight) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type stubStore struct {
	mu         sync.Mutex
	connectors map[string]*domain.Connector
}

func newStubStore(connector *domain.Connector) *stubStore {
	store := &stubStore{connectors: make(map[string]*domain.Connector)}
	if connector != nil {
		store.connectors[connector.ID] = connector
	}
	return store
}

func (s *stubStore) CreateConnector(_ context.Context, connector *domain.Connector) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connectors[connector.ID] = connector
	return nil
}

func (s *stubStore) GetConnector(_ context.Context, tenantID, id string) (*domain.Connector, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	connector, ok := s.connectors[id]
	if !ok || connector.TenantID != tenantID || connector.DeletedAt != nil {
		return nil, domain.ErrNotFound
	}
	return connector, nil
}

func (s *stubStore) ListConnectors(context.Context, string, int32, int32) ([]domain.Connector, int64, error) {
	return nil, 0, nil
}

func (s *stubStore) UpdateConnector(_ context.Context, connector *domain.Connector) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connectors[connector.ID] = connector
	return nil
}

func (s *stubStore) SoftDeleteConnector(_ context.Context, tenantID, id string, deletedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	connector, ok := s.connectors[id]
	if !ok || connector.TenantID != tenantID {
		return domain.ErrNotFound
	}
	connector.DeletedAt = &deletedAt
	return nil
}

func adapterTestConnector() *domain.Connector {
	return &domain.Connector{
		ID:        adapterConnector,
		TenantID:  adapterTenant,
		Name:      "pg-main",
		Type:      domain.TypePostgres,
		Status:    domain.StatusActive,
		SecretRef: "prod/pg-main",
		Config: map[string]any{
			"host":       "source-db.internal",
			"port":       float64(5432),
			"database":   "app",
			"schema":     "public",
			"tables":     []string{"orders"},
			"dataset_id": "019c0002-0000-7000-8000-000000000003",
		},
	}
}

func newAdapterRuntime(t *testing.T, client runtime.Client) *runtime.Runtime {
	t.Helper()
	rt, err := runtime.NewRuntime(
		runtime.Config{
			Warehouse: runtime.Warehouse{
				CatalogURI:    "jdbc:postgresql://catalog-db:5432/iceberg",
				WarehousePath: "s3://ontoaxis-warehouse/iceberg",
				Namespace:     "oa",
			},
		},
		runtime.WithConnectClient(client),
		runtime.WithSecretResolver(stubResolver{}),
	)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return rt
}

func TestSyncRuntimePreflightNilRuntime(t *testing.T) {
	t.Parallel()

	preflight := connectors.SyncRuntimePreflight(nil)
	err := preflight.Preflight(context.Background(), adapterTenant, adapterTestConnector())
	if !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("Preflight() error = %v, want %v", err, service.ErrUnavailable)
	}
}

func TestSyncRuntimePreflightClassification(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		rt := newAdapterRuntime(t, &stubClient{})
		if err := connectors.SyncRuntimePreflight(rt).Preflight(context.Background(), adapterTenant, adapterTestConnector()); err != nil {
			t.Fatalf("Preflight() error = %v", err)
		}
	})

	t.Run("invalid input becomes ErrInvalidInput", func(t *testing.T) {
		t.Parallel()
		rt := newAdapterRuntime(t, &stubClient{})
		connector := adapterTestConnector()
		connector.Type = domain.TypeMySQL

		err := connectors.SyncRuntimePreflight(rt).Preflight(context.Background(), adapterTenant, connector)
		if !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("Preflight() error = %v, want %v", err, service.ErrInvalidInput)
		}
	})

	t.Run("conflict passes through", func(t *testing.T) {
		t.Parallel()
		rt := newAdapterRuntime(t, &stubClient{})
		connector := adapterTestConnector()
		connector.Status = domain.StatusPaused

		err := connectors.SyncRuntimePreflight(rt).Preflight(context.Background(), adapterTenant, connector)
		if !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("Preflight() error = %v, want %v", err, domain.ErrConflict)
		}
	})

	t.Run("unreachable becomes ErrUnavailable", func(t *testing.T) {
		t.Parallel()
		client := &stubClient{pingErr: errors.New("connection refused")}
		rt := newAdapterRuntime(t, client)

		err := connectors.SyncRuntimePreflight(rt).Preflight(context.Background(), adapterTenant, adapterTestConnector())
		if !errors.Is(err, service.ErrUnavailable) {
			t.Fatalf("Preflight() error = %v, want %v", err, service.ErrUnavailable)
		}
		if errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("Preflight() error = %v, must not be %v", err, service.ErrInvalidInput)
		}
	})
}

func TestModuleSetSyncPreflightBlocksStartSync(t *testing.T) {
	t.Parallel()

	starter := &stubStarter{}
	module := connectors.NewWithStore(newStubStore(adapterTestConnector()), service.WithSyncStarter(starter))
	preflight := &stubPreflight{err: errors.New("kafka connect unavailable")}
	module.SetSyncPreflight(preflight)

	jobID, err := module.Service().StartSync(context.Background(), adapterTenant, adapterConnector)
	if !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("StartSync() error = %v, want %v", err, service.ErrUnavailable)
	}
	if jobID != "" {
		t.Fatalf("StartSync() jobID = %q, want empty", jobID)
	}
	if preflight.callCount() != 1 {
		t.Fatalf("preflight calls = %d, want 1", preflight.callCount())
	}
	if starter.callCount() != 0 {
		t.Fatalf("sync starter calls = %d, want 0", starter.callCount())
	}
}

func TestModuleSetSyncRuntimeUnconfiguredIsUnavailable(t *testing.T) {
	t.Parallel()

	starter := &stubStarter{}
	module := connectors.NewWithStore(newStubStore(adapterTestConnector()), service.WithSyncStarter(starter))
	module.SetSyncRuntime(nil)

	if _, err := module.Service().StartSync(context.Background(), adapterTenant, adapterConnector); !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("StartSync() error = %v, want %v", err, service.ErrUnavailable)
	}
	if starter.callCount() != 0 {
		t.Fatalf("sync starter calls = %d, want 0 (D9: no job without runtime)", starter.callCount())
	}
}

func TestModuleSetSyncRuntimeCreatesNoRuntimeOnPreflightFailure(t *testing.T) {
	t.Parallel()

	client := &stubClient{pingErr: errors.New("connection refused")}
	module := connectors.NewWithStore(newStubStore(adapterTestConnector()), service.WithSyncStarter(&stubStarter{}))
	module.SetSyncRuntime(newAdapterRuntime(t, client))

	if _, err := module.Service().StartSync(context.Background(), adapterTenant, adapterConnector); !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("StartSync() error = %v, want %v", err, service.ErrUnavailable)
	}
	if client.upsertCount() != 0 {
		t.Fatalf("runtime connectors upserted = %d, want 0 before ingestion job", client.upsertCount())
	}
}

func TestModuleSetSyncRuntimeConfigErrorIsInvalidInput(t *testing.T) {
	t.Parallel()

	connector := adapterTestConnector()
	connector.Config = map[string]any{"host": "source-db.internal", "port": float64(5432)}
	module := connectors.NewWithStore(newStubStore(connector), service.WithSyncStarter(&stubStarter{}))
	module.SetSyncRuntime(newAdapterRuntime(t, &stubClient{}))

	if _, err := module.Service().StartSync(context.Background(), adapterTenant, adapterConnector); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("StartSync() error = %v, want %v", err, service.ErrInvalidInput)
	}
}
