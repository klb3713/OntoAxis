package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	objectsdomain "github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	"github.com/klb3713/ontoaxis/internal/modules/query/domain"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
)

type fakeService struct {
	executeResult domain.QueryResult
	executeErr    error
	plan          string
}

func (f *fakeService) Execute(context.Context, string, int) (domain.QueryResult, error) {
	return f.executeResult, f.executeErr
}
func (f *fakeService) Explain(context.Context, string) (string, error) { return f.plan, nil }
func (f *fakeService) SaveQuery(_ context.Context, tenantID, ownerID uuid.UUID, name, sql string) (domain.SavedQuery, error) {
	return domain.SavedQuery{ID: uuid.New(), TenantID: tenantID, Name: name, SQL: sql, OwnerID: ownerID}, nil
}
func (f *fakeService) ListSavedQueries(context.Context, uuid.UUID, int, int) ([]domain.SavedQuery, int64, error) {
	return []domain.SavedQuery{{Name: "q"}}, 1, nil
}
func (f *fakeService) DeleteSavedQuery(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeService) QueryObjects(context.Context, uuid.UUID, uuid.UUID, int, int) ([]objectsdomain.Object, int64, error) {
	return []objectsdomain.Object{{ID: uuid.New()}}, 1, nil
}

func testRouter(svc *fakeService, tc authn.TenantContext) chi.Router {
	r := chi.NewRouter()
	NewHandler(svc).Register(r)
	return r
}

func requestWithTenant(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := authn.WithTenantContext(req.Context(), authn.TenantContext{TenantID: uuid.New(), UserID: uuid.New()})
	return req.WithContext(ctx)
}

func TestExecuteRequiresTenant(t *testing.T) {
	r := chi.NewRouter()
	NewHandler(&fakeService{}).Register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(`{"sql":"SELECT 1"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestExecuteBadSQL(t *testing.T) {
	svc := &fakeService{executeErr: domain.ErrInvalidInput}
	r := testRouter(svc, authn.TenantContext{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, requestWithTenant(http.MethodPost, "/execute", `{"sql":"DELETE FROM x"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestExecuteSuccess(t *testing.T) {
	svc := &fakeService{executeResult: domain.QueryResult{Columns: []string{"x"}, Rows: [][]any{{1}}, RowCount: 1}}
	r := testRouter(svc, authn.TenantContext{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, requestWithTenant(http.MethodPost, "/execute", `{"sql":"SELECT 1"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"row_count":1`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestExplainSuccess(t *testing.T) {
	svc := &fakeService{plan: "plan"}
	r := testRouter(svc, authn.TenantContext{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, requestWithTenant(http.MethodPost, "/explain", `{"sql":"SELECT 1"}`))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "plan") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSaveQuerySuccess(t *testing.T) {
	r := testRouter(&fakeService{}, authn.TenantContext{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, requestWithTenant(http.MethodPost, "/saved-queries", `{"name":"q","sql":"SELECT 1"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeleteInvalidID(t *testing.T) {
	r := testRouter(&fakeService{}, authn.TenantContext{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, requestWithTenant(http.MethodDelete, "/saved-queries/nope", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

var _ = errors.New
