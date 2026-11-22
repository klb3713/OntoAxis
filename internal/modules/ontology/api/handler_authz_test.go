package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ontology/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/authz/authtest"
)

const authorizationTestTenant = "tenant-a"

// authorizationStubService implements the handlers exercised by the seam tests.
// Methods not exercised by those routes stay on the embedded interface.
type authorizationStubService struct {
	service.Service
	status                domain.Status
	getErr                error
	createObjectTypeCalls int
	bindDatasetCalls      int
}

func (s *authorizationStubService) objectType(tenantID string) *domain.ObjectType {
	status := s.status
	if status == "" {
		status = domain.StatusActive
	}
	return &domain.ObjectType{
		ID:       "ot-1",
		TenantID: tenantID,
		Name:     "customer",
		Status:   status,
	}
}

func (s *authorizationStubService) CreateObjectType(_ context.Context, tenantID string, _ service.CreateObjectTypeInput) (*domain.ObjectType, error) {
	s.createObjectTypeCalls++
	return s.objectType(tenantID), nil
}

func (s *authorizationStubService) GetObjectType(_ context.Context, tenantID, id string) (*domain.ObjectType, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if id != "ot-1" {
		return nil, domain.ErrNotFound
	}
	return s.objectType(tenantID), nil
}

func (s *authorizationStubService) ListObjectTypes(_ context.Context, tenantID string, page domain.Page) (*domain.PageResult[domain.ObjectType], error) {
	return &domain.PageResult[domain.ObjectType]{
		Items:   []domain.ObjectType{*s.objectType(tenantID)},
		Total:   1,
		Page:    page.Page,
		PerPage: page.PerPage,
	}, nil
}

func (s *authorizationStubService) UpdateObjectType(_ context.Context, tenantID, _ string, _ service.UpdateObjectTypeInput) (*domain.ObjectType, error) {
	return s.objectType(tenantID), nil
}

func (s *authorizationStubService) DeleteObjectType(_ context.Context, _, _ string) error {
	return nil
}

func (s *authorizationStubService) CreateProperty(_ context.Context, tenantID string, in service.CreatePropertyInput) (*domain.Property, error) {
	return &domain.Property{
		ID:           "prop-1",
		TenantID:     tenantID,
		ObjectTypeID: in.ObjectTypeID,
		Name:         in.Name,
		PropertyType: in.PropertyType,
	}, nil
}

func (s *authorizationStubService) ListProperties(_ context.Context, tenantID, objectTypeID string) ([]domain.Property, error) {
	return []domain.Property{{
		ID:           "prop-1",
		TenantID:     tenantID,
		ObjectTypeID: objectTypeID,
		Name:         "email",
		PropertyType: "string",
	}}, nil
}

func (s *authorizationStubService) UpdateProperty(_ context.Context, tenantID, id string, _ service.UpdatePropertyInput) (*domain.Property, error) {
	return &domain.Property{
		ID:           id,
		TenantID:     tenantID,
		ObjectTypeID: "ot-1",
		Name:         "email",
		PropertyType: "string",
	}, nil
}

func (s *authorizationStubService) DeleteProperty(_ context.Context, _, _ string) error {
	return nil
}

func (s *authorizationStubService) CreateLinkType(_ context.Context, tenantID string, in service.CreateLinkTypeInput) (*domain.LinkType, error) {
	return &domain.LinkType{
		ID:           "lt-1",
		TenantID:     tenantID,
		Name:         in.Name,
		SourceTypeID: in.SourceTypeID,
		TargetTypeID: in.TargetTypeID,
		Cardinality:  in.Cardinality,
	}, nil
}

func (s *authorizationStubService) ListLinkTypes(_ context.Context, tenantID string, page domain.Page) (*domain.PageResult[domain.LinkType], error) {
	return &domain.PageResult[domain.LinkType]{
		Items: []domain.LinkType{{
			ID:           "lt-1",
			TenantID:     tenantID,
			Name:         "owns",
			SourceTypeID: "ot-1",
			TargetTypeID: "ot-2",
			Cardinality:  domain.CardinalityManyToMany,
		}},
		Total:   1,
		Page:    page.Page,
		PerPage: page.PerPage,
	}, nil
}

func (s *authorizationStubService) UpdateLinkType(_ context.Context, tenantID, id string, _ service.UpdateLinkTypeInput) (*domain.LinkType, error) {
	return &domain.LinkType{
		ID:           id,
		TenantID:     tenantID,
		Name:         "owns",
		SourceTypeID: "ot-1",
		TargetTypeID: "ot-2",
	}, nil
}

func (s *authorizationStubService) DeleteLinkType(_ context.Context, _, _ string) error {
	return nil
}

func (s *authorizationStubService) BindDataset(_ context.Context, tenantID string, _ service.BindDatasetInput) (*domain.ObjectType, error) {
	s.bindDatasetCalls++
	return s.objectType(tenantID), nil
}

func (s *authorizationStubService) UnbindDataset(_ context.Context, tenantID, _ string) (*domain.ObjectType, error) {
	return s.objectType(tenantID), nil
}

func (s *authorizationStubService) MaterializeBinding(_ context.Context, _, _ string) (string, error) {
	return "op-1", nil
}

type fakeDatasetReader struct{}

func (fakeDatasetReader) GetDatasetFact(_ context.Context, tenantID, datasetID string) (DatasetFact, error) {
	return DatasetFact{ID: datasetID, TenantID: tenantID}, nil
}

func newAuthorizedRouter(svc service.Service, guard authz.DecisionGuard) http.Handler {
	router := chi.NewRouter()
	NewHandler(svc, WithDecisionGuard(guard), WithDatasetReader(fakeDatasetReader{})).Register(router)
	return router
}

func doAuthorizedRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request = withTenant(request, authorizationTestTenant)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func requireAuthorizationCalls(t *testing.T, guard *authtest.FakeGuard) ([]authz.Action, []authz.ResourceFact) {
	t.Helper()
	actions, facts := guard.Calls()
	if len(actions) != len(facts) {
		t.Fatalf("guard recorded %d actions and %d facts", len(actions), len(facts))
	}
	return actions, facts
}

func requireAuthorizationFact(t *testing.T, fact authz.ResourceFact, wantType, wantID, wantTenant, wantStatus string) {
	t.Helper()
	if fact.Type != wantType || fact.ID != wantID || fact.TenantID != wantTenant {
		t.Fatalf("resource fact = %#v, want type=%q id=%q tenant=%q", fact, wantType, wantID, wantTenant)
	}
	if fact.Status != wantStatus {
		t.Fatalf("resource fact status = %q, want %q", fact.Status, wantStatus)
	}
}

func TestHandlerAuthorizationSeamAllows(t *testing.T) {
	stub := &authorizationStubService{}
	guard := authtest.Guard()
	handler := newAuthorizedRouter(stub, guard)

	steps := []struct {
		method string
		path   string
		body   string
		status int
		action authz.Action
		fact   authz.ResourceFact
	}{
		{http.MethodPost, "/api/v1/ontology/object-types", `{"name":"customer"}`, http.StatusCreated,
			"ontology:object_type:create", authz.ResourceFact{Type: "tenant", ID: authorizationTestTenant, TenantID: authorizationTestTenant}},
		{http.MethodGet, "/api/v1/ontology/object-types/ot-1", "", http.StatusOK,
			"ontology:object_type:read", authz.ResourceFact{Type: "object_type", ID: "ot-1", TenantID: authorizationTestTenant, Status: "active"}},
		{http.MethodPut, "/api/v1/ontology/object-types/ot-1", `{"display_name":"Customer"}`, http.StatusOK,
			"ontology:object_type:update", authz.ResourceFact{Type: "object_type", ID: "ot-1", TenantID: authorizationTestTenant, Status: "active"}},
		{http.MethodGet, "/api/v1/ontology/object-types", "", http.StatusOK,
			"ontology:object_type:read", authz.ResourceFact{Type: "tenant", ID: authorizationTestTenant, TenantID: authorizationTestTenant}},
		{http.MethodPost, "/api/v1/ontology/properties", `{"object_type_id":"ot-1","name":"email","property_type":"string"}`, http.StatusCreated,
			"ontology:property:create", authz.ResourceFact{Type: "object_type", ID: "ot-1", TenantID: authorizationTestTenant, Status: "active"}},
		{http.MethodGet, "/api/v1/ontology/object-types/ot-1/properties", "", http.StatusOK,
			"ontology:property:read", authz.ResourceFact{Type: "object_type", ID: "ot-1", TenantID: authorizationTestTenant, Status: "active"}},
		{http.MethodPut, "/api/v1/ontology/properties/prop-1", `{"display_name":"Email"}`, http.StatusOK,
			"ontology:property:update", authz.ResourceFact{Type: "tenant", ID: authorizationTestTenant, TenantID: authorizationTestTenant}},
		{http.MethodDelete, "/api/v1/ontology/properties/prop-1", "", http.StatusNoContent,
			"ontology:property:delete", authz.ResourceFact{Type: "tenant", ID: authorizationTestTenant, TenantID: authorizationTestTenant}},
		{http.MethodPost, "/api/v1/ontology/link-types", `{"name":"owns","source_type_id":"ot-1","target_type_id":"ot-2"}`, http.StatusCreated,
			"ontology:link_type:create", authz.ResourceFact{Type: "object_type", ID: "ot-1", TenantID: authorizationTestTenant, Status: "active"}},
		{http.MethodGet, "/api/v1/ontology/link-types", "", http.StatusOK,
			"ontology:link_type:read", authz.ResourceFact{Type: "tenant", ID: authorizationTestTenant, TenantID: authorizationTestTenant}},
		{http.MethodPut, "/api/v1/ontology/link-types/lt-1", `{"display_name":"Owns"}`, http.StatusOK,
			"ontology:link_type:update", authz.ResourceFact{Type: "tenant", ID: authorizationTestTenant, TenantID: authorizationTestTenant}},
		{http.MethodDelete, "/api/v1/ontology/link-types/lt-1", "", http.StatusNoContent,
			"ontology:link_type:delete", authz.ResourceFact{Type: "tenant", ID: authorizationTestTenant, TenantID: authorizationTestTenant}},
		{http.MethodPost, "/api/v1/ontology/object-types/ot-1/dataset-binding", `{"dataset_id":"ds-1"}`, http.StatusOK,
			"ontology:binding:write", authz.ResourceFact{Type: "object_type", ID: "ot-1", TenantID: authorizationTestTenant, Status: "active"}},
		{http.MethodDelete, "/api/v1/ontology/object-types/ot-1/dataset-binding", "", http.StatusOK,
			"ontology:binding:write", authz.ResourceFact{Type: "object_type", ID: "ot-1", TenantID: authorizationTestTenant, Status: "active"}},
		{http.MethodPost, "/api/v1/ontology/object-types/ot-1/dataset-binding/materialize", "", http.StatusOK,
			"ontology:binding:materialize", authz.ResourceFact{Type: "object_type", ID: "ot-1", TenantID: authorizationTestTenant, Status: "active"}},
		{http.MethodDelete, "/api/v1/ontology/object-types/ot-1", "", http.StatusNoContent,
			"ontology:object_type:delete", authz.ResourceFact{Type: "object_type", ID: "ot-1", TenantID: authorizationTestTenant, Status: "active"}},
	}

	for i, step := range steps {
		recorder := doAuthorizedRequest(t, handler, step.method, step.path, step.body)
		if recorder.Code != step.status {
			t.Fatalf("step %d %s %s status = %d, want %d; body=%s", i, step.method, step.path, recorder.Code, step.status, recorder.Body.String())
		}
	}

	actions, facts := requireAuthorizationCalls(t, guard)
	if len(actions) != len(steps) {
		t.Fatalf("guard calls = %d, want %d", len(actions), len(steps))
	}
	for i, step := range steps {
		if actions[i] != step.action {
			t.Fatalf("call %d action = %q, want %q", i, actions[i], step.action)
		}
		requireAuthorizationFact(t, facts[i], step.fact.Type, step.fact.ID, step.fact.TenantID, step.fact.Status)
	}
}

func TestHandlerAuthorizationDeniesSideEffects(t *testing.T) {
	stub := &authorizationStubService{}
	guard := authtest.Deny(authz.ReasonRoleNotPermitted)
	handler := newAuthorizedRouter(stub, guard)

	recorder := doAuthorizedRequest(t, handler, http.MethodPost, "/api/v1/ontology/object-types", `{"name":"customer"}`)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("denied create status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeError(t, recorder)["code"]; code != "forbidden" {
		t.Fatalf("error code = %v, want forbidden", code)
	}
	if stub.createObjectTypeCalls != 0 {
		t.Fatalf("denied create called the service %d times", stub.createObjectTypeCalls)
	}

	recorder = doAuthorizedRequest(t, handler, http.MethodPost, "/api/v1/ontology/object-types/ot-1/dataset-binding", `{"dataset_id":"ds-1"}`)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("denied binding status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if stub.bindDatasetCalls != 0 {
		t.Fatalf("denied binding called the service %d times", stub.bindDatasetCalls)
	}

	actions, facts := requireAuthorizationCalls(t, guard)
	if len(actions) != 2 {
		t.Fatalf("guard calls = %v, want two", actions)
	}
	if actions[0] != "ontology:object_type:create" || actions[1] != "ontology:binding:write" {
		t.Fatalf("actions = %v, want create then binding write", actions)
	}
	requireAuthorizationFact(t, facts[0], "tenant", authorizationTestTenant, authorizationTestTenant, "")
	requireAuthorizationFact(t, facts[1], "object_type", "ot-1", authorizationTestTenant, "active")
}

func TestHandlerSkipsGuardForMissingObjectType(t *testing.T) {
	stub := &authorizationStubService{getErr: domain.ErrNotFound}
	guard := authtest.Guard()
	handler := newAuthorizedRouter(stub, guard)

	recorder := doAuthorizedRequest(t, handler, http.MethodGet, "/api/v1/ontology/object-types/missing", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing object type status = %d, want 404; body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeError(t, recorder)["code"]; code != "not_found" {
		t.Fatalf("error code = %v, want not_found", code)
	}

	recorder = doAuthorizedRequest(t, handler, http.MethodPost, "/api/v1/ontology/properties", `{"object_type_id":"missing","name":"email","property_type":"string"}`)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("property on missing parent status = %d, want 404; body=%s", recorder.Code, recorder.Body.String())
	}

	actions, _ := requireAuthorizationCalls(t, guard)
	if len(actions) != 0 {
		t.Fatalf("guard was called before the parent load resolved: %v", actions)
	}
}

func TestHandlerObjectTypeFactCarriesState(t *testing.T) {
	stub := &authorizationStubService{status: domain.StatusDeprecated}
	guard := authtest.Guard()
	handler := newAuthorizedRouter(stub, guard)

	recorder := doAuthorizedRequest(t, handler, http.MethodGet, "/api/v1/ontology/object-types/ot-1", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("get deprecated object type status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}

	actions, facts := requireAuthorizationCalls(t, guard)
	if len(actions) != 1 || actions[0] != "ontology:object_type:read" {
		t.Fatalf("guard calls = %v, want one ontology:object_type:read", actions)
	}
	requireAuthorizationFact(t, facts[0], "object_type", "ot-1", authorizationTestTenant, "deprecated")
}

type missingDatasetReader struct{}

func (missingDatasetReader) GetDatasetFact(context.Context, string, string) (DatasetFact, error) {
	return DatasetFact{}, ErrDatasetNotFound
}

func TestBindingRejectsCrossTenantDatasetBeforeAuthorization(t *testing.T) {
	svc := &authorizationStubService{}
	guard := authtest.Guard()
	router := chi.NewRouter()
	NewHandler(svc, WithDecisionGuard(guard), WithDatasetReader(missingDatasetReader{})).Register(router)
	recorder := doAuthorizedRequest(t, router, http.MethodPost, "/api/v1/ontology/object-types/ot-1/dataset-binding", `{"dataset_id":"other-tenant"}`)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", recorder.Code, recorder.Body.String())
	}
	if svc.bindDatasetCalls != 0 {
		t.Fatalf("BindDataset called %d times after cross-tenant rejection", svc.bindDatasetCalls)
	}
	if actions, _ := guard.Calls(); len(actions) != 0 {
		t.Fatalf("authorization must not run before the dataset tenant check: %v", actions)
	}
}

func TestBindingFailsClosedWithoutDatasetReader(t *testing.T) {
	svc := &authorizationStubService{}
	router := chi.NewRouter()
	NewHandler(svc, WithDecisionGuard(authtest.Guard())).Register(router)
	recorder := doAuthorizedRequest(t, router, http.MethodPost, "/api/v1/ontology/object-types/ot-1/dataset-binding", `{"dataset_id":"ds-1"}`)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	if svc.bindDatasetCalls != 0 {
		t.Fatalf("BindDataset called %d times without dataset reader", svc.bindDatasetCalls)
	}
}
