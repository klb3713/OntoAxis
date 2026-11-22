package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/api"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/authz/authtest"
)

const authorizationConnectorID = "019c0000-0000-7000-8000-0000000000aa"

func newAuthorizedRouter(svc api.Service, opts ...api.Option) http.Handler {
	r := chi.NewRouter()
	api.NewHandler(svc, opts...).Register(r)
	return r
}

func newConnectorRequest(method, path, body string) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	request.Header.Set(tenantHeaderForTest, tenantID)
	return request
}

func activeConnector(id string) *domain.Connector {
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	return &domain.Connector{
		ID: idsOr(id), TenantID: tenantID, Name: "pg-main", Type: domain.TypePostgres,
		Config: map[string]any{"host": "db"}, Status: domain.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
}

func idsOr(id string) string {
	if id == "" {
		return authorizationConnectorID
	}
	return id
}

func TestConnectorAuthorizationTenantLevelActions(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	svc := &fakeService{
		create: func(context.Context, string, service.CreateInput) (*domain.Connector, error) {
			return activeConnector(""), nil
		},
		list: func(context.Context, string, int32, int32) (*service.ListResult, error) {
			return &service.ListResult{}, nil
		},
	}
	router := newAuthorizedRouter(svc, api.WithDecisionGuard(guard))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, newConnectorRequest(http.MethodPost, "/api/v1/connectors", `{"name":"pg-main","type":"postgres"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(authz.DecisionHeader); got != "decision-test" {
		t.Fatalf("create decision header = %q, want decision-test", got)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, newConnectorRequest(http.MethodGet, "/api/v1/connectors", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(authz.DecisionHeader); got != "decision-test" {
		t.Fatalf("list decision header = %q, want decision-test", got)
	}

	actions, facts := guard.Calls()
	wantActions := []authz.Action{"connector:create", "connector:read"}
	if !reflect.DeepEqual(actions, wantActions) {
		t.Fatalf("actions = %v, want %v", actions, wantActions)
	}
	wantFact := authz.ResourceFact{Type: "tenant", ID: tenantID, TenantID: tenantID}
	for i := range facts {
		if !reflect.DeepEqual(facts[i], wantFact) {
			t.Fatalf("facts[%d] = %+v, want %+v", i, facts[i], wantFact)
		}
	}
}

func TestConnectorAuthorizationResourceActions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantAction authz.Action
		wantStatus int
	}{
		{"get", http.MethodGet, "/api/v1/connectors/" + authorizationConnectorID, "", "connector:read", http.StatusOK},
		{"update", http.MethodPatch, "/api/v1/connectors/" + authorizationConnectorID, `{}`, "connector:update", http.StatusOK},
		{"delete", http.MethodDelete, "/api/v1/connectors/" + authorizationConnectorID, "", "connector:delete", http.StatusNoContent},
		{"test", http.MethodPost, "/api/v1/connectors/" + authorizationConnectorID + "/test", "", "connector:test", http.StatusOK},
		{"sync", http.MethodPost, "/api/v1/connectors/" + authorizationConnectorID + "/sync", "", "connector:sync", http.StatusCreated},
		{"infer_schema", http.MethodPost, "/api/v1/connectors/" + authorizationConnectorID + "/infer-schema", "", "connector:infer_schema", http.StatusOK},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			guard := authtest.Guard()
			svc := &fakeService{
				get: func(context.Context, string, string) (*domain.Connector, error) {
					return activeConnector(""), nil
				},
				update: func(context.Context, string, string, service.UpdateInput) (*domain.Connector, error) {
					return activeConnector(""), nil
				},
			}
			router := newAuthorizedRouter(svc, api.WithDecisionGuard(guard))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, newConnectorRequest(tc.method, tc.path, tc.body))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := rec.Header().Get(authz.DecisionHeader); got != "decision-test" {
				t.Fatalf("decision header = %q, want decision-test", got)
			}
			actions, facts := guard.Calls()
			if !reflect.DeepEqual(actions, []authz.Action{tc.wantAction}) {
				t.Fatalf("actions = %v, want [%s]", actions, tc.wantAction)
			}
			wantFact := authz.ResourceFact{
				Type: "connector", ID: authorizationConnectorID, TenantID: tenantID, Status: string(domain.StatusActive),
			}
			if len(facts) != 1 || !reflect.DeepEqual(facts[0], wantFact) {
				t.Fatalf("facts = %+v, want [%+v]", facts, wantFact)
			}
		})
	}
}

func TestConnectorAuthorizationDeny(t *testing.T) {
	t.Parallel()
	guard := authtest.Deny(authz.ReasonRoleNotPermitted)
	svc := &fakeService{get: func(context.Context, string, string) (*domain.Connector, error) {
		return activeConnector(""), nil
	}}
	router := newAuthorizedRouter(svc, api.WithDecisionGuard(guard))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, newConnectorRequest(http.MethodGet, "/api/v1/connectors/"+authorizationConnectorID, ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	actions, _ := guard.Calls()
	if !reflect.DeepEqual(actions, []authz.Action{"connector:read"}) {
		t.Fatalf("actions = %v, want [connector:read]", actions)
	}
}

func TestConnectorCrossTenantIsNotFoundBeforeAuthorization(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	svc := &fakeService{get: func(context.Context, string, string) (*domain.Connector, error) {
		return nil, domain.ErrNotFound
	}}
	router := newAuthorizedRouter(svc, api.WithDecisionGuard(guard))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, newConnectorRequest(http.MethodGet, "/api/v1/connectors/"+authorizationConnectorID, ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
	actions, facts := guard.Calls()
	if len(actions) != 0 || len(facts) != 0 {
		t.Fatalf("guard was called before the tenant-scoped load: actions = %v, facts = %+v", actions, facts)
	}
}

func TestConnectorSyncRejectsNonActiveConnector(t *testing.T) {
	t.Parallel()
	guard := authtest.Deny(authz.ReasonResourceStateDenied)
	syncStarted := false
	paused := activeConnector("")
	paused.Status = domain.StatusPaused
	svc := &fakeService{
		get: func(context.Context, string, string) (*domain.Connector, error) {
			return paused, nil
		},
		startSync: func(context.Context, string, string) (string, error) {
			syncStarted = true
			return "job-1", nil
		},
	}
	router := newAuthorizedRouter(svc, api.WithDecisionGuard(guard))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, newConnectorRequest(http.MethodPost, "/api/v1/connectors/"+authorizationConnectorID+"/sync", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	if syncStarted {
		t.Fatal("sync side effect ran despite a resource-state denial")
	}
	actions, facts := guard.Calls()
	if !reflect.DeepEqual(actions, []authz.Action{"connector:sync"}) {
		t.Fatalf("actions = %v, want [connector:sync]", actions)
	}
	if len(facts) != 1 || facts[0].Status != string(domain.StatusPaused) {
		t.Fatalf("facts = %+v, want status paused", facts)
	}
}
