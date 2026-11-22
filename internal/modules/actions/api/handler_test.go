package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/service"
)

type fakeService struct {
	service.Service
	created domain.ActionType
}

func (f *fakeService) CreateActionType(_ context.Context, tenantID uuid.UUID, name, display, description string, objectTypeID *uuid.UUID, parameters map[string]any, status string) (domain.ActionType, error) {
	f.created = domain.ActionType{ID: uuid.New(), TenantID: tenantID, Name: name, DisplayName: display, Description: description, ObjectTypeID: objectTypeID, Parameters: parameters, Status: status}
	return f.created, nil
}

func (f *fakeService) ListActionTypes(context.Context, uuid.UUID, int, int) ([]domain.ActionType, int64, error) {
	return []domain.ActionType{f.created}, 1, nil
}

func TestCreateAndListActionTypeContract(t *testing.T) {
	svc := &fakeService{}
	r := chi.NewRouter()
	r.Route("/api/v1/actions", NewHandler(svc).Register)

	body := `{"name":"archive","display_name":"Archive","parameters":{"required":["reason"]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions/types", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-Id", uuid.New().String())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created["name"] != "archive" || created["display_name"] != "Archive" {
		t.Fatalf("created = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/actions/types", nil)
	req.Header.Set("X-Tenant-Id", uuid.New().String())
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("list = %#v", list)
	}
}

func TestActionTypeRequiresTenant(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1/actions", NewHandler(&fakeService{}).Register)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/actions/types", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
