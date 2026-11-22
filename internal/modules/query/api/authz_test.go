package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	objectsdomain "github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	"github.com/klb3713/ontoaxis/internal/modules/query/domain"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/authz/authtest"
)

// seamQueryService is a controllable query service for the authorization seam
// tests. It records side effects so denied decisions can be proven inert.
type seamQueryService struct {
	queryResult domain.QueryResult
	queryErr    error
	plan        string

	savedQuery domain.SavedQuery
	savedRows  []domain.SavedQuery
	listErr    error

	executed  int
	explained int
	saved     int
	deleted   int
	queried   int
}

func (s *seamQueryService) Execute(context.Context, string, int) (domain.QueryResult, error) {
	s.executed++
	return s.queryResult, s.queryErr
}

func (s *seamQueryService) Explain(context.Context, string) (string, error) {
	s.explained++
	return s.plan, nil
}

func (s *seamQueryService) SaveQuery(_ context.Context, tenantID, ownerID uuid.UUID, name, sql string) (domain.SavedQuery, error) {
	s.saved++
	if s.savedQuery.ID != uuid.Nil {
		return s.savedQuery, nil
	}
	return domain.SavedQuery{ID: uuid.New(), TenantID: tenantID, Name: name, SQL: sql, OwnerID: ownerID}, nil
}

func (s *seamQueryService) ListSavedQueries(_ context.Context, _ uuid.UUID, limit, offset int) ([]domain.SavedQuery, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	rows := s.savedRows
	if offset > len(rows) {
		offset = len(rows)
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[offset:end], int64(len(rows)), nil
}

func (s *seamQueryService) DeleteSavedQuery(context.Context, uuid.UUID, uuid.UUID) error {
	s.deleted++
	return nil
}

func (s *seamQueryService) QueryObjects(context.Context, uuid.UUID, uuid.UUID, int, int) ([]objectsdomain.Object, int64, error) {
	s.queried++
	return []objectsdomain.Object{{ID: uuid.New()}}, 1, nil
}

func seamQueryRouter(svc *seamQueryService, guard authz.DecisionGuard) chi.Router {
	r := chi.NewRouter()
	var opts []Option
	if guard != nil {
		opts = append(opts, WithDecisionGuard(guard))
	}
	NewHandler(svc, opts...).Register(r)
	return r
}

func seamQueryRequest(method, path, body string, tenantID, userID uuid.UUID) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := authn.WithTenantContext(req.Context(), authn.TenantContext{TenantID: tenantID, UserID: userID})
	return req.WithContext(ctx)
}

func serveSeam(t *testing.T, router chi.Router, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func singleSeamCall(t *testing.T, guard *authtest.FakeGuard) (authz.Action, authz.ResourceFact) {
	t.Helper()
	actions, facts := guard.Calls()
	if len(actions) != 1 || len(facts) != 1 {
		t.Fatalf("guard calls = %d actions %d facts, want 1 each", len(actions), len(facts))
	}
	return actions[0], facts[0]
}

func TestQueryAuthorizationExecuteUsesTenantFact(t *testing.T) {
	tenantID, userID := uuid.New(), uuid.New()
	guard := authtest.Guard()
	svc := &seamQueryService{queryResult: domain.QueryResult{Columns: []string{"x"}, Rows: [][]any{{1}}, RowCount: 1}}
	router := seamQueryRouter(svc, guard)

	rec := serveSeam(t, router, seamQueryRequest(http.MethodPost, "/execute", `{"sql":"SELECT 1"}`, tenantID, userID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(authz.DecisionHeader); got != "decision-test" {
		t.Fatalf("decision header = %q", got)
	}
	if svc.executed != 1 {
		t.Fatalf("execute side effect = %d, want 1", svc.executed)
	}
	action, fact := singleSeamCall(t, guard)
	if action != authz.Action("query:execute") {
		t.Fatalf("action = %q", action)
	}
	if fact.Type != "tenant" || fact.ID != tenantID.String() || fact.TenantID != tenantID.String() {
		t.Fatalf("fact = %#v", fact)
	}
}

func TestQueryAuthorizationExplainUsesTenantFact(t *testing.T) {
	tenantID, userID := uuid.New(), uuid.New()
	guard := authtest.Guard()
	svc := &seamQueryService{plan: "plan"}
	router := seamQueryRouter(svc, guard)

	rec := serveSeam(t, router, seamQueryRequest(http.MethodPost, "/explain", `{"sql":"SELECT 1"}`, tenantID, userID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	action, fact := singleSeamCall(t, guard)
	if action != authz.Action("query:explain") {
		t.Fatalf("action = %q", action)
	}
	if fact.Type != "tenant" || fact.ID != tenantID.String() {
		t.Fatalf("fact = %#v", fact)
	}
}

func TestQueryAuthorizationDenyBlocksExecute(t *testing.T) {
	tenantID, userID := uuid.New(), uuid.New()
	guard := authtest.Deny(authz.ReasonRoleNotPermitted)
	svc := &seamQueryService{}
	router := seamQueryRouter(svc, guard)

	rec := serveSeam(t, router, seamQueryRequest(http.MethodPost, "/execute", `{"sql":"SELECT 1"}`, tenantID, userID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "permission denied") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if svc.executed != 0 {
		t.Fatalf("execute side effect fired %d times after denial", svc.executed)
	}
	action, _ := singleSeamCall(t, guard)
	if action != authz.Action("query:execute") {
		t.Fatalf("action = %q", action)
	}
}

func TestQueryAuthorizationSavedQueryListTenantScope(t *testing.T) {
	tenantID, userID := uuid.New(), uuid.New()
	guard := authtest.Guard()
	svc := &seamQueryService{savedRows: []domain.SavedQuery{{ID: uuid.New(), TenantID: tenantID, Name: "q"}}}
	router := seamQueryRouter(svc, guard)

	rec := serveSeam(t, router, seamQueryRequest(http.MethodGet, "/saved-queries", "", tenantID, userID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	action, fact := singleSeamCall(t, guard)
	if action != authz.Action("saved_query:read") {
		t.Fatalf("action = %q", action)
	}
	if fact.Type != "tenant" || fact.ID != tenantID.String() {
		t.Fatalf("fact = %#v", fact)
	}
}

func TestQueryAuthorizationSavedQueryCreateTenantScope(t *testing.T) {
	tenantID, userID := uuid.New(), uuid.New()
	guard := authtest.Guard()
	svc := &seamQueryService{}
	router := seamQueryRouter(svc, guard)

	rec := serveSeam(t, router, seamQueryRequest(http.MethodPost, "/saved-queries", `{"name":"q","sql":"SELECT 1"}`, tenantID, userID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.saved != 1 {
		t.Fatalf("save side effect = %d, want 1", svc.saved)
	}
	action, fact := singleSeamCall(t, guard)
	if action != authz.Action("saved_query:create") {
		t.Fatalf("action = %q", action)
	}
	if fact.Type != "tenant" || fact.ID != tenantID.String() {
		t.Fatalf("fact = %#v", fact)
	}
}

func TestQueryAuthorizationSavedQueryDeleteCarriesOwner(t *testing.T) {
	tenantID, userID, ownerID := uuid.New(), uuid.New(), uuid.New()
	row := domain.SavedQuery{ID: uuid.New(), TenantID: tenantID, Name: "q", OwnerID: ownerID}
	guard := authtest.Guard()
	svc := &seamQueryService{savedRows: []domain.SavedQuery{row}}
	router := seamQueryRouter(svc, guard)

	rec := serveSeam(t, router, seamQueryRequest(http.MethodDelete, "/saved-queries/"+row.ID.String(), "", tenantID, userID))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.deleted != 1 {
		t.Fatalf("delete side effect = %d, want 1", svc.deleted)
	}
	action, fact := singleSeamCall(t, guard)
	if action != authz.Action("saved_query:delete") {
		t.Fatalf("action = %q", action)
	}
	if fact.Type != "saved_query" || fact.ID != row.ID.String() || fact.TenantID != tenantID.String() {
		t.Fatalf("fact = %#v", fact)
	}
	if fact.OwnerID != ownerID.String() {
		t.Fatalf("owner fact = %q, want %q", fact.OwnerID, ownerID.String())
	}
}

func TestQueryAuthorizationDenyBlocksSavedQueryDelete(t *testing.T) {
	tenantID, userID, ownerID := uuid.New(), uuid.New(), uuid.New()
	row := domain.SavedQuery{ID: uuid.New(), TenantID: tenantID, Name: "q", OwnerID: ownerID}
	guard := authtest.Deny(authz.ReasonRoleNotPermitted)
	svc := &seamQueryService{savedRows: []domain.SavedQuery{row}}
	router := seamQueryRouter(svc, guard)

	rec := serveSeam(t, router, seamQueryRequest(http.MethodDelete, "/saved-queries/"+row.ID.String(), "", tenantID, userID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.deleted != 0 {
		t.Fatalf("delete side effect fired %d times after denial", svc.deleted)
	}
	action, fact := singleSeamCall(t, guard)
	if action != authz.Action("saved_query:delete") || fact.OwnerID != ownerID.String() {
		t.Fatalf("action = %q fact = %#v", action, fact)
	}
}

func TestQueryAuthorizationSavedQueryDeleteMissingOrForeign404BeforeDecision(t *testing.T) {
	tenantID, userID := uuid.New(), uuid.New()
	t.Run("missing row", func(t *testing.T) {
		guard := authtest.Guard()
		svc := &seamQueryService{}
		router := seamQueryRouter(svc, guard)
		rec := serveSeam(t, router, seamQueryRequest(http.MethodDelete, "/saved-queries/"+uuid.New().String(), "", tenantID, userID))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if actions, facts := guard.Calls(); len(actions) != 0 || len(facts) != 0 {
			t.Fatalf("guard called before 404: %d actions %d facts", len(actions), len(facts))
		}
	})
	t.Run("row from another tenant", func(t *testing.T) {
		guard := authtest.Guard()
		other := uuid.New()
		row := domain.SavedQuery{ID: uuid.New(), TenantID: other, Name: "q", OwnerID: uuid.New()}
		svc := &seamQueryService{savedRows: []domain.SavedQuery{row}}
		router := seamQueryRouter(svc, guard)
		rec := serveSeam(t, router, seamQueryRequest(http.MethodDelete, "/saved-queries/"+row.ID.String(), "", tenantID, userID))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if actions, facts := guard.Calls(); len(actions) != 0 || len(facts) != 0 {
			t.Fatalf("guard called before 404: %d actions %d facts", len(actions), len(facts))
		}
	})
}

func TestQueryAuthorizationObjectQueryResource(t *testing.T) {
	tenantID, userID := uuid.New(), uuid.New()
	objectTypeID := uuid.New()

	t.Run("filtered authorizes tenant in v1", func(t *testing.T) {
		guard := authtest.Guard()
		svc := &seamQueryService{}
		router := seamQueryRouter(svc, guard)
		rec := serveSeam(t, router, seamQueryRequest(http.MethodGet, "/objects?object_type_id="+objectTypeID.String(), "", tenantID, userID))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if svc.queried != 1 {
			t.Fatalf("query side effect = %d, want 1", svc.queried)
		}
		action, fact := singleSeamCall(t, guard)
		if action != authz.Action("object:read") {
			t.Fatalf("action = %q", action)
		}
		// No tenant-scoped ObjectType reader exists yet, so the filtered list is
		// decided against the tenant resource rather than a synthesized fact.
		if fact.Type != "tenant" || fact.ID != tenantID.String() {
			t.Fatalf("fact = %#v", fact)
		}
	})

	t.Run("unfiltered uses tenant", func(t *testing.T) {
		guard := authtest.Guard()
		svc := &seamQueryService{}
		router := seamQueryRouter(svc, guard)
		rec := serveSeam(t, router, seamQueryRequest(http.MethodGet, "/objects", "", tenantID, userID))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		action, fact := singleSeamCall(t, guard)
		if action != authz.Action("object:read") {
			t.Fatalf("action = %q", action)
		}
		if fact.Type != "tenant" || fact.ID != tenantID.String() {
			t.Fatalf("fact = %#v", fact)
		}
	})
}

func TestQueryAuthorizationDenyBlocksObjectQuery(t *testing.T) {
	tenantID, userID := uuid.New(), uuid.New()
	guard := authtest.Deny(authz.ReasonNoRelationship)
	svc := &seamQueryService{}
	router := seamQueryRouter(svc, guard)

	rec := serveSeam(t, router, seamQueryRequest(http.MethodGet, "/objects", "", tenantID, userID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.queried != 0 {
		t.Fatalf("query side effect fired %d times after denial", svc.queried)
	}
}

func TestQueryAuthorizationLegacyHandlerWithoutGuard(t *testing.T) {
	tenantID, userID := uuid.New(), uuid.New()
	svc := &seamQueryService{}
	router := seamQueryRouter(svc, nil)
	rec := serveSeam(t, router, seamQueryRequest(http.MethodGet, "/saved-queries", "", tenantID, userID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(authz.DecisionHeader); got != "" {
		t.Fatalf("decision header set without guard: %q", got)
	}
}
