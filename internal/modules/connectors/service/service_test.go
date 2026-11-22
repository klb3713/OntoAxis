package service_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/service"
)

type fakeStore struct {
	mu         sync.Mutex
	connectors map[string]*domain.Connector
}

func newFakeStore() *fakeStore {
	return &fakeStore{connectors: make(map[string]*domain.Connector)}
}

func (f *fakeStore) CreateConnector(_ context.Context, connector *domain.Connector) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.connectors {
		if existing.TenantID == connector.TenantID && existing.Name == connector.Name && existing.DeletedAt == nil {
			return fmt.Errorf("%w: connector name already exists", domain.ErrConflict)
		}
	}
	copied := *connector
	f.connectors[connector.ID] = &copied
	return nil
}

func (f *fakeStore) GetConnector(_ context.Context, tenantID, id string) (*domain.Connector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	connector, ok := f.connectors[id]
	if !ok || connector.TenantID != tenantID || connector.DeletedAt != nil {
		return nil, fmt.Errorf("%w: connector %s", domain.ErrNotFound, id)
	}
	copied := *connector
	return &copied, nil
}

func (f *fakeStore) ListConnectors(_ context.Context, tenantID string, limit, offset int32) ([]domain.Connector, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []domain.Connector
	for _, connector := range f.connectors {
		if connector.TenantID == tenantID && connector.DeletedAt == nil {
			items = append(items, *connector)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	total := int64(len(items))
	start := int(offset)
	if start > len(items) {
		start = len(items)
	}
	end := start + int(limit)
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}

func (f *fakeStore) UpdateConnector(_ context.Context, connector *domain.Connector) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.connectors[connector.ID]
	if !ok || existing.TenantID != connector.TenantID || existing.DeletedAt != nil {
		return fmt.Errorf("%w: connector %s", domain.ErrNotFound, connector.ID)
	}
	for id, other := range f.connectors {
		if id != connector.ID && other.TenantID == connector.TenantID && other.Name == connector.Name && other.DeletedAt == nil {
			return fmt.Errorf("%w: connector name already exists", domain.ErrConflict)
		}
	}
	copied := *connector
	f.connectors[connector.ID] = &copied
	return nil
}

func (f *fakeStore) SoftDeleteConnector(_ context.Context, tenantID, id string, deletedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	connector, ok := f.connectors[id]
	if !ok || connector.TenantID != tenantID || connector.DeletedAt != nil {
		return fmt.Errorf("%w: connector %s", domain.ErrNotFound, id)
	}
	connector.DeletedAt = &deletedAt
	return nil
}

type fakeTester struct {
	ok      bool
	message string
	err     error
}

func (f fakeTester) Test(context.Context, string, *domain.Connector) (bool, string, error) {
	return f.ok, f.message, f.err
}

type fakeInferrer struct {
	schema map[string]any
	err    error
}

func (f fakeInferrer) InferSchema(context.Context, string, *domain.Connector) (map[string]any, error) {
	return f.schema, f.err
}

type fakeStarter struct {
	jobID string
	err   error
}

func (f fakeStarter) StartSync(context.Context, string, string) (string, error) {
	return f.jobID, f.err
}

const (
	tenantA = "11111111-1111-7111-8111-111111111111"
	tenantB = "22222222-2222-7222-8222-222222222222"
)

func newService(t *testing.T, opts ...service.Option) (service.Service, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	counter := 0
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		counter++
		return base.Add(time.Duration(counter) * time.Second)
	}
	opts = append([]service.Option{service.WithClock(clock)}, opts...)
	return service.NewService(store, opts...), store
}

func createInput(name string) service.CreateInput {
	return service.CreateInput{
		Name:        name,
		DisplayName: "Test Connector",
		Type:        "postgres",
		Config:      map[string]any{"host": "db.internal", "port": float64(5432)},
		Tags:        []string{"test"},
	}
}

func TestCreateConnector(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	connector, err := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
	if err != nil {
		t.Fatalf("CreateConnector() error = %v", err)
	}
	if connector.Status != domain.StatusActive {
		t.Fatalf("status = %q, want active", connector.Status)
	}
	if connector.TenantID != tenantA {
		t.Fatalf("tenant = %q", connector.TenantID)
	}
	parsed, err := uuid.Parse(connector.ID)
	if err != nil {
		t.Fatalf("id is not a uuid: %v", err)
	}
	if parsed.Version() != 7 {
		t.Fatalf("uuid version = %d, want 7", parsed.Version())
	}
	if connector.CreatedAt.IsZero() || connector.UpdatedAt.IsZero() {
		t.Fatal("timestamps must be set")
	}
}

func TestCreateConnectorRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	cases := []service.CreateInput{
		{Name: "", Type: "postgres", Config: map[string]any{}},
		{Name: "bad name", Type: "postgres", Config: map[string]any{}},
		{Name: "ok", Type: "oracle", Config: map[string]any{}},
		{Name: "ok", Type: "postgres", Config: nil},
		{Name: "ok", Type: "postgres", Config: map[string]any{"password": "x"}},
	}
	for i, in := range cases {
		if _, err := svc.CreateConnector(context.Background(), tenantA, in); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
	if _, err := svc.CreateConnector(context.Background(), "bad-tenant", createInput("ok")); !errors.Is(err, service.ErrInvalidTenant) {
		t.Fatalf("bad tenant error = %v", err)
	}
}

func TestCreateConnectorNameUniquenessAcrossTenants(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	if _, err := svc.CreateConnector(context.Background(), tenantA, createInput("shared-name")); err != nil {
		t.Fatalf("tenant A create: %v", err)
	}
	if _, err := svc.CreateConnector(context.Background(), tenantB, createInput("shared-name")); err != nil {
		t.Fatalf("tenant B create: %v", err)
	}
	if _, err := svc.CreateConnector(context.Background(), tenantA, createInput("shared-name")); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate error = %v, want ErrConflict", err)
	}
}

func TestGetConnectorTenantIsolation(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	created, _ := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
	if _, err := svc.GetConnector(context.Background(), tenantB, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant get error = %v, want ErrNotFound", err)
	}
}

func TestListConnectorsPaginationAndSoftDelete(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	for _, name := range []string{"one", "two", "three"} {
		if _, err := svc.CreateConnector(context.Background(), tenantA, createInput(name)); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	page, err := svc.ListConnectors(context.Background(), tenantA, 1, 2)
	if err != nil {
		t.Fatalf("ListConnectors() error = %v", err)
	}
	if page.Total != 3 || page.TotalPages != 2 || len(page.Connectors) != 2 {
		t.Fatalf("page = %+v", page)
	}
	if page.Connectors[0].Name != "three" {
		t.Fatalf("expected newest first, got %q", page.Connectors[0].Name)
	}
	if err := svc.DeleteConnector(context.Background(), tenantA, page.Connectors[0].ID); err != nil {
		t.Fatalf("DeleteConnector() error = %v", err)
	}
	if _, err := svc.GetConnector(context.Background(), tenantA, page.Connectors[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted get error = %v", err)
	}
}

func TestUpdateConnectorStatus(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	created, _ := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
	status := "paused"
	updated, err := svc.UpdateConnector(context.Background(), tenantA, created.ID, service.UpdateInput{Status: &status})
	if err != nil {
		t.Fatalf("UpdateConnector() error = %v", err)
	}
	if updated.Status != domain.StatusPaused {
		t.Fatalf("status = %q", updated.Status)
	}
	if !updated.UpdatedAt.After(created.CreatedAt) {
		t.Fatalf("updated_at %v must be after created_at %v", updated.UpdatedAt, created.CreatedAt)
	}
	bad := "deleted"
	if _, err := svc.UpdateConnector(context.Background(), tenantA, created.ID, service.UpdateInput{Status: &bad}); !errors.Is(err, domain.ErrInvalidStatus) {
		t.Fatalf("invalid status error = %v", err)
	}
}

func TestTestConnectorAndInferSchema(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return base }
	svc := service.NewService(store,
		service.WithClock(clock),
		service.WithConnectorTester(fakeTester{ok: true, message: "reachable"}),
		service.WithSchemaInferrer(fakeInferrer{schema: map[string]any{"tables": []any{"users"}}}),
	)
	created, _ := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
	result, err := svc.TestConnector(context.Background(), tenantA, created.ID)
	if err != nil {
		t.Fatalf("TestConnector() error = %v", err)
	}
	if !result.OK || result.Message != "reachable" {
		t.Fatalf("result = %+v", result)
	}
	schema, err := svc.InferSchema(context.Background(), tenantA, created.ID)
	if err != nil {
		t.Fatalf("InferSchema() error = %v", err)
	}
	if len(schema) == 0 {
		t.Fatal("schema should not be empty")
	}

	withoutAdapters := service.NewService(store, service.WithClock(clock))
	if _, err := withoutAdapters.TestConnector(context.Background(), tenantA, created.ID); !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("missing tester error = %v, want ErrUnavailable", err)
	}
	if _, err := withoutAdapters.InferSchema(context.Background(), tenantA, created.ID); !errors.Is(err, service.ErrNotImplemented) {
		t.Fatalf("missing inferrer error = %v, want ErrNotImplemented", err)
	}
}

func TestStartSync(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return base }
	svc := service.NewService(store, service.WithClock(clock), service.WithSyncStarter(fakeStarter{jobID: "job-1"}))
	created, _ := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
	jobID, err := svc.StartSync(context.Background(), tenantA, created.ID)
	if err != nil || jobID != "job-1" {
		t.Fatalf("StartSync() = %q, %v", jobID, err)
	}

	paused := "paused"
	if _, err := svc.UpdateConnector(context.Background(), tenantA, created.ID, service.UpdateInput{Status: &paused}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, err := svc.StartSync(context.Background(), tenantA, created.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("paused StartSync error = %v, want ErrConflict", err)
	}

	withoutStarter := service.NewService(store, service.WithClock(clock))
	active, _ := withoutStarter.CreateConnector(context.Background(), tenantA, createInput("pg-second"))
	if _, err := withoutStarter.StartSync(context.Background(), tenantA, active.ID); !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("missing starter error = %v, want ErrUnavailable", err)
	}
}
