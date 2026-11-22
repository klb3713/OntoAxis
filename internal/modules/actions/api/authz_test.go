package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/authz/authtest"
)

// seamService is a controllable service for the authorization seam tests. It
// records side effects so a denied decision can be proven to write nothing.
type seamService struct {
	service.Service

	actionType    domain.ActionType
	actionErr     error
	execution     domain.Execution
	executionErr  error
	executions    []domain.Execution
	executionsTot int64
	listErr       error

	created   int
	updated   int
	deleted   int
	validated int
	previewed int
	ran       int
	cancelled int
	retried   int
}

func (s *seamService) GetActionType(context.Context, uuid.UUID, uuid.UUID) (domain.ActionType, error) {
	return s.actionType, s.actionErr
}

func (s *seamService) CreateActionType(_ context.Context, tenantID uuid.UUID, name, display, description string, objectTypeID *uuid.UUID, parameters map[string]any, status string) (domain.ActionType, error) {
	s.created++
	return domain.ActionType{
		ID: uuid.New(), TenantID: tenantID, Name: name, DisplayName: display, Description: description,
		ObjectTypeID: objectTypeID, Parameters: parameters, Status: status,
	}, nil
}

func (s *seamService) ListActionTypes(context.Context, uuid.UUID, int, int) ([]domain.ActionType, int64, error) {
	return []domain.ActionType{s.actionType}, 1, nil
}

func (s *seamService) UpdateActionType(context.Context, uuid.UUID, uuid.UUID, string, map[string]any) (domain.ActionType, error) {
	s.updated++
	return s.actionType, nil
}

func (s *seamService) DeleteActionType(context.Context, uuid.UUID, uuid.UUID) error {
	s.deleted++
	return nil
}

func (s *seamService) Validate(context.Context, uuid.UUID, uuid.UUID, map[string]any) (bool, []string, error) {
	s.validated++
	return true, nil, nil
}

func (s *seamService) Preview(context.Context, uuid.UUID, uuid.UUID, map[string]any) (map[string]any, error) {
	s.previewed++
	return map[string]any{}, nil
}

func (s *seamService) Run(context.Context, uuid.UUID, uuid.UUID, map[string]any) (domain.Execution, error) {
	s.ran++
	return s.execution, s.executionErr
}

func (s *seamService) GetExecution(context.Context, uuid.UUID, uuid.UUID) (domain.Execution, error) {
	return s.execution, s.executionErr
}

func (s *seamService) ListExecutions(context.Context, uuid.UUID, *uuid.UUID, int, int) ([]domain.Execution, int64, error) {
	return s.executions, s.executionsTot, s.listErr
}

func (s *seamService) CancelExecution(context.Context, uuid.UUID, uuid.UUID) (domain.Execution, error) {
	s.cancelled++
	return s.execution, nil
}

func (s *seamService) RetryExecution(context.Context, uuid.UUID, uuid.UUID) (domain.Execution, error) {
	s.retried++
	return s.execution, nil
}

func seamRouter(svc *seamService, guard authz.DecisionGuard) chi.Router {
	r := chi.NewRouter()
	var opts []Option
	if guard != nil {
		opts = append(opts, WithDecisionGuard(guard))
	}
	r.Route("/api/v1/actions", NewHandler(svc, opts...).Register)
	return r
}

func seamRequest(method, path, body string, tenantID uuid.UUID) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Tenant-Id", tenantID.String())
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func serve(t *testing.T, router chi.Router, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func singleCall(t *testing.T, guard *authtest.FakeGuard) (authz.Action, authz.ResourceFact) {
	t.Helper()
	actions, facts := guard.Calls()
	if len(actions) != 1 || len(facts) != 1 {
		t.Fatalf("guard calls = %d actions %d facts, want 1 each", len(actions), len(facts))
	}
	return actions[0], facts[0]
}

func TestAuthorizationAllowCarriesDecisionAndActionTypeFact(t *testing.T) {
	tenantID := uuid.New()
	actionType := domain.ActionType{ID: uuid.New(), TenantID: tenantID, Name: "archive", Status: domain.ActionStatusDraft}
	guard := authtest.Guard()
	svc := &seamService{actionType: actionType}
	router := seamRouter(svc, guard)

	rec := serve(t, router, seamRequest(http.MethodGet, "/api/v1/actions/types/"+actionType.ID.String(), "", tenantID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(authz.DecisionHeader); got != "decision-test" {
		t.Fatalf("decision header = %q, want decision-test", got)
	}
	action, fact := singleCall(t, guard)
	if action != authz.Action("action_type:read") {
		t.Fatalf("action = %q", action)
	}
	if fact.Type != "action_type" || fact.ID != actionType.ID.String() || fact.TenantID != tenantID.String() || fact.Status != domain.ActionStatusDraft {
		t.Fatalf("fact = %#v", fact)
	}
}

func TestAuthorizationDenyBlocksRunSideEffect(t *testing.T) {
	tenantID := uuid.New()
	actionType := domain.ActionType{ID: uuid.New(), TenantID: tenantID, Name: "archive", Status: domain.ActionStatusActive}
	guard := authtest.Deny(authz.ReasonRoleNotPermitted)
	svc := &seamService{actionType: actionType, execution: domain.Execution{ID: uuid.New(), TenantID: tenantID, ActionTypeID: actionType.ID}}
	router := seamRouter(svc, guard)

	rec := serve(t, router, seamRequest(http.MethodPost, "/api/v1/actions/types/"+actionType.ID.String()+"/run", `{"parameters":{}}`, tenantID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "permission denied") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if svc.ran != 0 {
		t.Fatalf("run side effect fired %d times after denial", svc.ran)
	}
	action, fact := singleCall(t, guard)
	if action != authz.Action("action_type:run") {
		t.Fatalf("action = %q", action)
	}
	if fact.Type != "action_type" || fact.Status != domain.ActionStatusActive {
		t.Fatalf("fact = %#v", fact)
	}
}

func TestAuthorizationDenyBlocksCancelSideEffect(t *testing.T) {
	tenantID := uuid.New()
	execution := domain.Execution{ID: uuid.New(), TenantID: tenantID, ActionTypeID: uuid.New(), Status: domain.ExecutionStatusSucceeded}
	guard := authtest.Deny(authz.ReasonResourceStateDenied)
	svc := &seamService{execution: execution}
	router := seamRouter(svc, guard)

	rec := serve(t, router, seamRequest(http.MethodPost, "/api/v1/actions/executions/"+execution.ID.String()+"/cancel", "", tenantID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.cancelled != 0 {
		t.Fatalf("cancel side effect fired %d times after denial", svc.cancelled)
	}
	action, fact := singleCall(t, guard)
	if action != authz.Action("action_execution:cancel") {
		t.Fatalf("action = %q", action)
	}
	if fact.Type != "action_execution" || fact.Status != domain.ExecutionStatusSucceeded {
		t.Fatalf("fact = %#v", fact)
	}
}

func TestAuthorizationRunStateStillDecidedFromRow(t *testing.T) {
	tenantID := uuid.New()
	actionType := domain.ActionType{ID: uuid.New(), TenantID: tenantID, Status: domain.ActionStatusDisabled}
	guard := authtest.Deny(authz.ReasonResourceStateDenied)
	svc := &seamService{actionType: actionType}
	router := seamRouter(svc, guard)

	rec := serve(t, router, seamRequest(http.MethodPost, "/api/v1/actions/types/"+actionType.ID.String()+"/run", `{"parameters":{}}`, tenantID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.ran != 0 {
		t.Fatalf("run side effect fired %d times", svc.ran)
	}
	_, fact := singleCall(t, guard)
	if fact.Status != domain.ActionStatusDisabled {
		t.Fatalf("status fact = %q, want %q", fact.Status, domain.ActionStatusDisabled)
	}
}

func TestAuthorizationExecutionFactCarriesParentEdge(t *testing.T) {
	tenantID := uuid.New()
	actionTypeID := uuid.New()
	execution := domain.Execution{ID: uuid.New(), TenantID: tenantID, ActionTypeID: actionTypeID, Status: domain.ExecutionStatusQueued}
	guard := authtest.Guard()
	svc := &seamService{execution: execution}
	router := seamRouter(svc, guard)

	rec := serve(t, router, seamRequest(http.MethodGet, "/api/v1/actions/executions/"+execution.ID.String(), "", tenantID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	action, fact := singleCall(t, guard)
	if action != authz.Action("action_execution:read") {
		t.Fatalf("action = %q", action)
	}
	if fact.Type != "action_execution" || fact.ID != execution.ID.String() || fact.TenantID != tenantID.String() || fact.Status != domain.ExecutionStatusQueued {
		t.Fatalf("fact = %#v", fact)
	}
	want := authz.Ancestor{Type: "action_type", ID: actionTypeID.String()}
	if len(fact.Ancestors) != 1 || fact.Ancestors[0] != want {
		t.Fatalf("ancestors = %#v, want %#v", fact.Ancestors, want)
	}
}

func TestAuthorizationCrossTenant404BeforeDecision(t *testing.T) {
	tenantID := uuid.New()
	t.Run("missing row", func(t *testing.T) {
		guard := authtest.Guard()
		svc := &seamService{actionErr: domain.ErrNotFound}
		router := seamRouter(svc, guard)
		rec := serve(t, router, seamRequest(http.MethodGet, "/api/v1/actions/types/"+uuid.New().String(), "", tenantID))
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
		actionType := domain.ActionType{ID: uuid.New(), TenantID: other, Status: domain.ActionStatusActive}
		svc := &seamService{actionType: actionType}
		router := seamRouter(svc, guard)
		rec := serve(t, router, seamRequest(http.MethodGet, "/api/v1/actions/types/"+actionType.ID.String(), "", tenantID))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if actions, facts := guard.Calls(); len(actions) != 0 || len(facts) != 0 {
			t.Fatalf("guard called before 404: %d actions %d facts", len(actions), len(facts))
		}
	})
}

func TestAuthorizationCreateIsTenantScoped(t *testing.T) {
	tenantID := uuid.New()
	guard := authtest.Guard()
	svc := &seamService{}
	router := seamRouter(svc, guard)

	body := `{"name":"archive","display_name":"Archive","parameters":{"required":["reason"]}}`
	rec := serve(t, router, seamRequest(http.MethodPost, "/api/v1/actions/types", body, tenantID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.created != 1 {
		t.Fatalf("create side effect = %d, want 1", svc.created)
	}
	action, fact := singleCall(t, guard)
	if action != authz.Action("action_type:create") {
		t.Fatalf("action = %q", action)
	}
	if fact.Type != "tenant" || fact.ID != tenantID.String() || fact.TenantID != tenantID.String() {
		t.Fatalf("fact = %#v", fact)
	}
}

func TestAuthorizationExecutionListUsesParentScope(t *testing.T) {
	tenantID := uuid.New()
	actionTypeID := uuid.New()
	actionType := domain.ActionType{ID: actionTypeID, TenantID: tenantID, Status: domain.ActionStatusActive}

	t.Run("filtered by action type", func(t *testing.T) {
		guard := authtest.Guard()
		svc := &seamService{actionType: actionType, executions: []domain.Execution{{ID: uuid.New(), TenantID: tenantID, ActionTypeID: actionTypeID}}, executionsTot: 1}
		router := seamRouter(svc, guard)
		rec := serve(t, router, seamRequest(http.MethodGet, "/api/v1/actions/executions?action_type_id="+actionTypeID.String(), "", tenantID))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		action, fact := singleCall(t, guard)
		if action != authz.Action("action_execution:read") {
			t.Fatalf("action = %q", action)
		}
		if fact.Type != "action_type" || fact.ID != actionTypeID.String() || fact.TenantID != tenantID.String() {
			t.Fatalf("fact = %#v", fact)
		}
	})

	t.Run("unfiltered", func(t *testing.T) {
		guard := authtest.Guard()
		svc := &seamService{executions: []domain.Execution{{ID: uuid.New(), TenantID: tenantID, ActionTypeID: actionTypeID}}, executionsTot: 1}
		router := seamRouter(svc, guard)
		rec := serve(t, router, seamRequest(http.MethodGet, "/api/v1/actions/executions", "", tenantID))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		action, fact := singleCall(t, guard)
		if action != authz.Action("action_execution:read") {
			t.Fatalf("action = %q", action)
		}
		if fact.Type != "tenant" || fact.ID != tenantID.String() {
			t.Fatalf("fact = %#v", fact)
		}
	})
}

func TestAuthorizationLegacyHandlerWithoutGuard(t *testing.T) {
	tenantID := uuid.New()
	svc := &seamService{}
	router := seamRouter(svc, nil)
	rec := serve(t, router, seamRequest(http.MethodGet, "/api/v1/actions/types", "", tenantID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

type unavailableGuard struct{}

func (unavailableGuard) Require(context.Context, authz.Action, authz.ResourceFact) (authz.Decision, error) {
	return authz.Decision{}, &authz.Error{Reason: authz.ReasonEngineTimeout, Engine: "openfga"}
}

func (unavailableGuard) WriteHTTP(w http.ResponseWriter, r *http.Request, err error) {
	authz.WriteHTTPError(w, r, err)
}

func (unavailableGuard) SetDecisionHeader(http.ResponseWriter, authz.Decision) {}

func TestAuthorizationUnavailableFailsClosed(t *testing.T) {
	tenantID := uuid.New()
	actionType := domain.ActionType{ID: uuid.New(), TenantID: tenantID, Name: "archive", Status: domain.ActionStatusActive}
	svc := &seamService{actionType: actionType}
	router := seamRouter(svc, unavailableGuard{})

	rec := serve(t, router, seamRequest(http.MethodPost, "/api/v1/actions/types/"+actionType.ID.String()+"/run", `{"parameters":{}}`, tenantID))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.ran != 0 {
		t.Fatalf("run side effect fired %d times after authorization failure", svc.ran)
	}
}

func TestAuthorizationRequiresTenantContext(t *testing.T) {
	svc := &seamService{}
	router := seamRouter(svc, authtest.Guard())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/actions/types", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
