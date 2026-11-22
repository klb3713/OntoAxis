package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ontology/service"
)

type stubService struct {
	service.Service
	createObjectType func(ctx context.Context, tenantID string, in service.CreateObjectTypeInput) (*domain.ObjectType, error)
	getObjectType    func(ctx context.Context, tenantID, id string) (*domain.ObjectType, error)
	listObjectTypes  func(ctx context.Context, tenantID string, page domain.Page) (*domain.PageResult[domain.ObjectType], error)
}

func (s *stubService) CreateObjectType(ctx context.Context, tenantID string, in service.CreateObjectTypeInput) (*domain.ObjectType, error) {
	return s.createObjectType(ctx, tenantID, in)
}

func (s *stubService) GetObjectType(ctx context.Context, tenantID, id string) (*domain.ObjectType, error) {
	return s.getObjectType(ctx, tenantID, id)
}

func (s *stubService) ListObjectTypes(ctx context.Context, tenantID string, page domain.Page) (*domain.PageResult[domain.ObjectType], error) {
	return s.listObjectTypes(ctx, tenantID, page)
}

func newRouter(svc service.Service) http.Handler {
	router := chi.NewRouter()
	NewHandler(svc).Register(router)
	return router
}

func withTenant(request *http.Request, tenantID string) *http.Request {
	return request.WithContext(WithTenantID(request.Context(), tenantID))
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", recorder.Body.String(), err)
	}
	errorBody, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("error envelope missing: %s", recorder.Body.String())
	}
	return errorBody
}

func TestCreateObjectTypeContract(t *testing.T) {
	now := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	stub := &stubService{
		createObjectType: func(_ context.Context, tenantID string, in service.CreateObjectTypeInput) (*domain.ObjectType, error) {
			if tenantID != "tenant-a" {
				t.Fatalf("tenantID = %q, want tenant-a", tenantID)
			}
			if in.Name != "customer" || in.PrimaryKeyProperty != "customer_id" {
				t.Fatalf("unexpected input: %+v", in)
			}
			return &domain.ObjectType{
				ID:                 "018f0000-0000-7000-8000-000000000001",
				TenantID:           tenantID,
				Name:               in.Name,
				DisplayName:        "Customer",
				PrimaryKeyProperty: in.PrimaryKeyProperty,
				Status:             domain.StatusActive,
				CreatedAt:          now,
				UpdatedAt:          now,
			}, nil
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ontology/object-types", strings.NewReader(`{
		"name":"customer","display_name":"Customer","primary_key_property":"customer_id"
	}`))
	request = withTenant(request, "tenant-a")
	recorder := httptest.NewRecorder()

	newRouter(stub).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["id"] != "018f0000-0000-7000-8000-000000000001" || body["name"] != "customer" || body["status"] != "active" {
		t.Fatalf("unexpected body: %v", body)
	}
	if _, ok := body["properties"].([]any); !ok {
		t.Fatalf("properties must be an array: %v", body["properties"])
	}
	if body["created_at"] == nil || body["updated_at"] == nil {
		t.Fatalf("timestamps missing: %v", body)
	}
}

func TestTenantContextIsRequired(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/ontology/object-types/missing", nil)
	recorder := httptest.NewRecorder()
	newRouter(&stubService{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if code := decodeError(t, recorder)["code"]; code != "unauthorized" {
		t.Fatalf("code = %v, want unauthorized", code)
	}
}

func TestServiceErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"invalid argument", fmt.Errorf("%w: bad", domain.ErrInvalidArgument), http.StatusBadRequest, "invalid_argument"},
		{"not found", fmt.Errorf("%w: missing", domain.ErrNotFound), http.StatusNotFound, "not_found"},
		{"already exists", fmt.Errorf("%w: duplicate", domain.ErrAlreadyExists), http.StatusConflict, "already_exists"},
		{"failed precondition", fmt.Errorf("%w: referenced", domain.ErrFailedPrecondition), http.StatusConflict, "conflict"},
		{"internal", errors.New("database exploded"), http.StatusInternalServerError, "internal"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &stubService{
				getObjectType: func(context.Context, string, string) (*domain.ObjectType, error) {
					return nil, test.err
				},
			}
			request := withTenant(httptest.NewRequest(http.MethodGet, "/api/v1/ontology/object-types/123", nil), "tenant-a")
			recorder := httptest.NewRecorder()
			newRouter(stub).ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if code := decodeError(t, recorder)["code"]; code != test.wantCode {
				t.Fatalf("code = %v, want %q", code, test.wantCode)
			}
		})
	}
}

func TestMalformedBodiesAreRejected(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"malformed JSON", `{"name":`},
		{"unknown field", `{"name":"customer","unknown":true}`},
		{"trailing object", `{"name":"customer"}{"name":"order"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := withTenant(httptest.NewRequest(http.MethodPost, "/api/v1/ontology/object-types", strings.NewReader(test.body)), "tenant-a")
			recorder := httptest.NewRecorder()
			newRouter(&stubService{}).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			if code := decodeError(t, recorder)["code"]; code != "bad_request" {
				t.Fatalf("code = %v, want bad_request", code)
			}
		})
	}
}

func TestListObjectTypesPaginationContract(t *testing.T) {
	stub := &stubService{
		listObjectTypes: func(_ context.Context, tenantID string, page domain.Page) (*domain.PageResult[domain.ObjectType], error) {
			if tenantID != "tenant-a" || page.Page != 2 || page.PerPage != 5 {
				t.Fatalf("unexpected args: %q %+v", tenantID, page)
			}
			return &domain.PageResult[domain.ObjectType]{
				Items:   []domain.ObjectType{},
				Total:   6,
				Page:    2,
				PerPage: 5,
			}, nil
		},
	}
	request := withTenant(httptest.NewRequest(http.MethodGet, "/api/v1/ontology/object-types?page=2&per_page=5", nil), "tenant-a")
	recorder := httptest.NewRecorder()
	newRouter(stub).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["total"] != float64(6) || body["page"] != float64(2) || body["per_page"] != float64(5) {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestInvalidPaginationIsRejected(t *testing.T) {
	request := withTenant(httptest.NewRequest(http.MethodGet, "/api/v1/ontology/object-types?page=0", nil), "tenant-a")
	recorder := httptest.NewRecorder()
	newRouter(&stubService{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeError(t, recorder)["code"]; code != "invalid_argument" {
		t.Fatalf("code = %v, want invalid_argument", code)
	}
}

func TestNilServiceFailsClosed(t *testing.T) {
	router := newRouter(nil)
	request := withTenant(httptest.NewRequest(http.MethodGet, "/api/v1/ontology/object-types", nil), "tenant-a")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if code := decodeError(t, recorder)["code"]; code != "unavailable" {
		t.Fatalf("code = %v, want unavailable", code)
	}
}

func TestOntologyRouteTable(t *testing.T) {
	router := newRouter(nil)
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/ontology/object-types"},
		{http.MethodGet, "/api/v1/ontology/object-types"},
		{http.MethodGet, "/api/v1/ontology/object-types/123"},
		{http.MethodPut, "/api/v1/ontology/object-types/123"},
		{http.MethodDelete, "/api/v1/ontology/object-types/123"},
		{http.MethodPost, "/api/v1/ontology/properties"},
		{http.MethodGet, "/api/v1/ontology/object-types/123/properties"},
		{http.MethodPut, "/api/v1/ontology/properties/123"},
		{http.MethodDelete, "/api/v1/ontology/properties/123"},
		{http.MethodPost, "/api/v1/ontology/link-types"},
		{http.MethodGet, "/api/v1/ontology/link-types"},
		{http.MethodPut, "/api/v1/ontology/link-types/123"},
		{http.MethodDelete, "/api/v1/ontology/link-types/123"},
		{http.MethodPost, "/api/v1/ontology/object-types/123/dataset-binding"},
		{http.MethodDelete, "/api/v1/ontology/object-types/123/dataset-binding"},
		{http.MethodPost, "/api/v1/ontology/object-types/123/dataset-binding/materialize"},
	}
	for _, route := range routes {
		request := withTenant(httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)), "tenant-a")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code == http.StatusNotFound {
			t.Errorf("%s %s is not registered", route.method, route.path)
		}
	}

	unknown := withTenant(httptest.NewRequest(http.MethodGet, "/api/v1/ontology/projects", nil), "tenant-a")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, unknown)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, want 404", recorder.Code)
	}
}

type ontologyFixture struct {
	Request  json.RawMessage `json:"request"`
	Expected struct {
		Name               string `json:"name"`
		DisplayName        string `json:"display_name"`
		PrimaryKeyProperty string `json:"primary_key_property"`
		Status             string `json:"status"`
	} `json:"expected"`
}

func TestFixtureDrivenObjectTypeContract(t *testing.T) {
	data, err := os.ReadFile("testdata/ontology_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture ontologyFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	stub := &stubService{
		createObjectType: func(_ context.Context, tenantID string, in service.CreateObjectTypeInput) (*domain.ObjectType, error) {
			return &domain.ObjectType{
				ID:                 "018f0000-0000-7000-8000-000000000002",
				TenantID:           tenantID,
				Name:               in.Name,
				DisplayName:        in.DisplayName,
				PrimaryKeyProperty: in.PrimaryKeyProperty,
				Status:             domain.StatusActive,
				CreatedAt:          time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC),
				UpdatedAt:          time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	request := withTenant(httptest.NewRequest(http.MethodPost, "/api/v1/ontology/object-types", strings.NewReader(string(fixture.Request))), "tenant-a")
	recorder := httptest.NewRecorder()
	newRouter(stub).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["name"] != fixture.Expected.Name ||
		response["display_name"] != fixture.Expected.DisplayName ||
		response["primary_key_property"] != fixture.Expected.PrimaryKeyProperty ||
		response["status"] != fixture.Expected.Status {
		t.Fatalf("fixture response mismatch: %v", response)
	}
}
