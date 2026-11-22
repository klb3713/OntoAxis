package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/api"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/service"
)

type fakeService struct {
	create      func(context.Context, string, service.CreateInput) (*domain.Connector, error)
	get         func(context.Context, string, string) (*domain.Connector, error)
	list        func(context.Context, string, int32, int32) (*service.ListResult, error)
	update      func(context.Context, string, string, service.UpdateInput) (*domain.Connector, error)
	delete      func(context.Context, string, string) error
	test        func(context.Context, string, string) (*service.TestResult, error)
	inferSchema func(context.Context, string, string) (map[string]any, error)
	startSync   func(context.Context, string, string) (string, error)
}

func (f *fakeService) CreateConnector(ctx context.Context, tenantID string, in service.CreateInput) (*domain.Connector, error) {
	if f.create != nil {
		return f.create(ctx, tenantID, in)
	}
	return nil, nil
}
func (f *fakeService) GetConnector(ctx context.Context, tenantID, id string) (*domain.Connector, error) {
	if f.get != nil {
		return f.get(ctx, tenantID, id)
	}
	return nil, nil
}
func (f *fakeService) ListConnectors(ctx context.Context, tenantID string, page, perPage int32) (*service.ListResult, error) {
	if f.list != nil {
		return f.list(ctx, tenantID, page, perPage)
	}
	return &service.ListResult{}, nil
}
func (f *fakeService) UpdateConnector(ctx context.Context, tenantID, id string, in service.UpdateInput) (*domain.Connector, error) {
	if f.update != nil {
		return f.update(ctx, tenantID, id, in)
	}
	return nil, nil
}
func (f *fakeService) DeleteConnector(ctx context.Context, tenantID, id string) error {
	if f.delete != nil {
		return f.delete(ctx, tenantID, id)
	}
	return nil
}
func (f *fakeService) TestConnector(ctx context.Context, tenantID, id string) (*service.TestResult, error) {
	if f.test != nil {
		return f.test(ctx, tenantID, id)
	}
	return &service.TestResult{}, nil
}
func (f *fakeService) InferSchema(ctx context.Context, tenantID, id string) (map[string]any, error) {
	if f.inferSchema != nil {
		return f.inferSchema(ctx, tenantID, id)
	}
	return map[string]any{}, nil
}
func (f *fakeService) StartSync(ctx context.Context, tenantID, connectorID string) (string, error) {
	if f.startSync != nil {
		return f.startSync(ctx, tenantID, connectorID)
	}
	return "job-1", nil
}

const tenantID = "11111111-1111-7111-8111-111111111111"

func newRouter(svc api.Service) http.Handler {
	r := chi.NewRouter()
	api.NewHandler(svc).Register(r)
	return r
}

func TestCreateRequiresTenantHeader(t *testing.T) {
	t.Parallel()
	router := newRouter(&fakeService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connectors", strings.NewReader(`{"name":"pg","type":"postgres"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRejectsExternalTenantHeader(t *testing.T) {
	t.Parallel()
	router := newRouter(&fakeService{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connectors", nil)
	req.Header.Set(tenantHeaderForTest, tenantID)
	req.Header.Set("X-Tenant-Id", tenantID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	// Presence of the external header is rejected even when its value is empty.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/connectors", nil)
	req.Header.Set(tenantHeaderForTest, tenantID)
	req.Header.Set("X-Tenant-Id", "")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty external header status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

const tenantHeaderForTest = "X-Ontoaxis-Tenant-Id"

func TestCreateResponseDoesNotExposeSecretRef(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	svc := &fakeService{create: func(context.Context, string, service.CreateInput) (*domain.Connector, error) {
		return &domain.Connector{
			ID: "019c0000-0000-7000-8000-000000000001", TenantID: tenantID, Name: "pg-main",
			Type: domain.TypePostgres, Config: map[string]any{"host": "db"}, Tags: []string{"prod"},
			SecretRef: "vault://tenant/secret", Status: domain.StatusActive, CreatedAt: now, UpdatedAt: now,
		}, nil
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connectors", strings.NewReader(`{"name":"pg-main","type":"postgres","config":{"host":"db"}}`))
	req.Header.Set(tenantHeaderForTest, tenantID)
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret_ref") || strings.Contains(body, "vault://tenant/secret") {
		t.Fatalf("response leaked secret_ref: %s", body)
	}
	if !strings.Contains(body, `"status":"active"`) {
		t.Fatalf("response missing status: %s", body)
	}
}

func TestCreateRejectsNonObjectConfig(t *testing.T) {
	t.Parallel()
	router := newRouter(&fakeService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connectors", strings.NewReader(`{"name":"pg","type":"postgres","config":["not","object"]}`))
	req.Header.Set(tenantHeaderForTest, tenantID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListReturnsPageShape(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	svc := &fakeService{list: func(context.Context, string, int32, int32) (*service.ListResult, error) {
		return &service.ListResult{
			Connectors: []domain.Connector{{ID: "019c0000-0000-7000-8000-000000000001", Name: "pg", Type: domain.TypePostgres, Status: domain.StatusActive, CreatedAt: now, UpdatedAt: now}},
			Page:       1, PerPage: 20, Total: 1, TotalPages: 1,
		}, nil
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connectors?page=1&per_page=20", nil)
	req.Header.Set(tenantHeaderForTest, tenantID)
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Connectors []map[string]any `json:"connectors"`
		Page       map[string]any   `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Connectors) != 1 || body.Page["total"] != float64(1) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestListRejectsPerPageAboveMaximum(t *testing.T) {
	t.Parallel()
	router := newRouter(&fakeService{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connectors?per_page=101", nil)
	req.Header.Set(tenantHeaderForTest, tenantID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestErrorMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"invalid", domain.ErrInvalidType, http.StatusBadRequest},
		{"not found", domain.ErrNotFound, http.StatusNotFound},
		{"conflict", domain.ErrConflict, http.StatusConflict},
		{"unavailable", service.ErrUnavailable, http.StatusServiceUnavailable},
		{"not implemented", service.ErrNotImplemented, http.StatusNotImplemented},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeService{get: func(context.Context, string, string) (*domain.Connector, error) {
				return nil, tt.err
			}}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/connectors/019c0000-0000-7000-8000-000000000001", nil)
			req.Header.Set(tenantHeaderForTest, tenantID)
			rec := httptest.NewRecorder()
			newRouter(svc).ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"error"`) {
				t.Fatalf("missing error envelope: %s", rec.Body.String())
			}
		})
	}
}

func TestTestAndInferSchemaRoutes(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		test: func(context.Context, string, string) (*service.TestResult, error) {
			return &service.TestResult{OK: true, Message: "reachable"}, nil
		},
		inferSchema: func(context.Context, string, string) (map[string]any, error) {
			return map[string]any{"tables": []any{"users"}}, nil
		},
	}
	base := "/api/v1/connectors/019c0000-0000-7000-8000-000000000001"
	for _, route := range []string{base + "/test", base + "/infer-schema"} {
		req := httptest.NewRequest(http.MethodPost, route, nil)
		req.Header.Set(tenantHeaderForTest, tenantID)
		rec := httptest.NewRecorder()
		newRouter(svc).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", route, rec.Code, rec.Body.String())
		}
	}
}

func TestStartSyncReturnsJobID(t *testing.T) {
	t.Parallel()
	svc := &fakeService{startSync: func(context.Context, string, string) (string, error) {
		return "job-123", nil
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connectors/019c0000-0000-7000-8000-000000000001/sync", nil)
	req.Header.Set(tenantHeaderForTest, tenantID)
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "job-123") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
