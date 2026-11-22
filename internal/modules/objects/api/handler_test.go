package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
)

type fakeService struct{}

func (fakeService) CreateObject(_ context.Context, tenantID, objectTypeID uuid.UUID, properties map[string]any) (domain.Object, error) {
	return domain.Object{ID: uuid.New(), TenantID: tenantID, ObjectTypeID: objectTypeID, Version: 1, Properties: properties}, nil
}
func (fakeService) GetObject(context.Context, uuid.UUID, uuid.UUID) (domain.Object, error) {
	return domain.Object{}, domain.ErrNotFound
}
func (fakeService) ListObjects(context.Context, uuid.UUID, uuid.UUID, int, int) ([]domain.Object, int64, error) {
	return nil, 0, nil
}
func (fakeService) UpdateObject(context.Context, uuid.UUID, uuid.UUID, map[string]any, bool) (domain.Object, error) {
	return domain.Object{}, domain.ErrNotFound
}
func (fakeService) DeleteObject(context.Context, uuid.UUID, uuid.UUID) error {
	return domain.ErrNotFound
}
func (fakeService) CreateLink(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, map[string]any) (domain.Link, error) {
	return domain.Link{}, domain.ErrInvalidInput
}
func (fakeService) ListLinks(context.Context, uuid.UUID, uuid.UUID, int, int) ([]domain.Link, int64, error) {
	return nil, 0, nil
}
func (fakeService) DeleteLink(context.Context, uuid.UUID, uuid.UUID) error {
	return domain.ErrNotFound
}

func newRouter() chi.Router {
	r := chi.NewRouter()
	NewHandler(fakeService{}).Register(r)
	return r
}

func TestRequiresTenant(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	newRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateObjectWithTenantHeader(t *testing.T) {
	typeID := uuid.New().String()
	body := map[string]any{"object_type_id": typeID, "properties": map[string]any{"name": "trail"}}
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	req.Header.Set("X-Tenant-Id", uuid.New().String())
	req.Header.Set("Content-Type", "application/json")
	newRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListRejectsInvalidObjectType(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?object_type_id=not-a-uuid", nil)
	req.Header.Set("X-Tenant-Id", uuid.New().String())
	newRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
