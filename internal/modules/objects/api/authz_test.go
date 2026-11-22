package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	"github.com/klb3713/ontoaxis/internal/modules/objects/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/authz/authtest"
)

var (
	authzTenantID   = uuid.MustParse("22222222-2222-7222-8222-222222222222")
	otherTenantID   = uuid.MustParse("33333333-3333-7333-8333-333333333333")
	authzTypeID     = uuid.MustParse("44444444-4444-7444-8444-444444444444")
	authzObjectID   = uuid.MustParse("55555555-5555-7555-8555-555555555555")
	authzSourceID   = uuid.MustParse("66666666-6666-7666-8666-666666666666")
	authzTargetID   = uuid.MustParse("77777777-7777-7777-8777-777777777777")
	authzLinkID     = uuid.MustParse("88888888-8888-7888-8888-888888888888")
	authzLinkTypeID = uuid.MustParse("99999999-9999-7999-8999-999999999999")
)

type authzFakeService struct {
	createObject func(context.Context, uuid.UUID, uuid.UUID, map[string]any) (domain.Object, error)
	getObject    func(context.Context, uuid.UUID, uuid.UUID) (domain.Object, error)
	listObjects  func(context.Context, uuid.UUID, uuid.UUID, int, int) ([]domain.Object, int64, error)
	updateObject func(context.Context, uuid.UUID, uuid.UUID, map[string]any, bool) (domain.Object, error)
	deleteObject func(context.Context, uuid.UUID, uuid.UUID) error
	createLink   func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, map[string]any) (domain.Link, error)
	listLinks    func(context.Context, uuid.UUID, uuid.UUID, int, int) ([]domain.Link, int64, error)
	deleteLink   func(context.Context, uuid.UUID, uuid.UUID) error
}

func (f *authzFakeService) CreateObject(ctx context.Context, tenantID, objectTypeID uuid.UUID, properties map[string]any) (domain.Object, error) {
	if f.createObject != nil {
		return f.createObject(ctx, tenantID, objectTypeID, properties)
	}
	return domain.Object{}, domain.ErrNotFound
}

func (f *authzFakeService) GetObject(ctx context.Context, tenantID, id uuid.UUID) (domain.Object, error) {
	if f.getObject != nil {
		return f.getObject(ctx, tenantID, id)
	}
	return domain.Object{}, domain.ErrNotFound
}

func (f *authzFakeService) ListObjects(ctx context.Context, tenantID, objectTypeID uuid.UUID, limit, offset int) ([]domain.Object, int64, error) {
	if f.listObjects != nil {
		return f.listObjects(ctx, tenantID, objectTypeID, limit, offset)
	}
	return nil, 0, nil
}

func (f *authzFakeService) UpdateObject(ctx context.Context, tenantID, id uuid.UUID, properties map[string]any, replace bool) (domain.Object, error) {
	if f.updateObject != nil {
		return f.updateObject(ctx, tenantID, id, properties, replace)
	}
	return domain.Object{}, domain.ErrNotFound
}

func (f *authzFakeService) DeleteObject(ctx context.Context, tenantID, id uuid.UUID) error {
	if f.deleteObject != nil {
		return f.deleteObject(ctx, tenantID, id)
	}
	return domain.ErrNotFound
}

func (f *authzFakeService) CreateLink(ctx context.Context, tenantID, linkTypeID, sourceID, targetID uuid.UUID, properties map[string]any) (domain.Link, error) {
	if f.createLink != nil {
		return f.createLink(ctx, tenantID, linkTypeID, sourceID, targetID, properties)
	}
	return domain.Link{}, domain.ErrNotFound
}

func (f *authzFakeService) ListLinks(ctx context.Context, tenantID, objectID uuid.UUID, limit, offset int) ([]domain.Link, int64, error) {
	if f.listLinks != nil {
		return f.listLinks(ctx, tenantID, objectID, limit, offset)
	}
	return nil, 0, nil
}

func (f *authzFakeService) DeleteLink(ctx context.Context, tenantID, id uuid.UUID) error {
	if f.deleteLink != nil {
		return f.deleteLink(ctx, tenantID, id)
	}
	return domain.ErrNotFound
}

type fakeObjectTypeReader struct {
	fact  ObjectTypeFact
	err   error
	calls int
}

func (f *fakeObjectTypeReader) GetObjectTypeFact(_ context.Context, _, _ uuid.UUID) (ObjectTypeFact, error) {
	f.calls++
	return f.fact, f.err
}

type fakeLinkReader struct {
	fact  LinkFact
	err   error
	calls int
}

func (f *fakeLinkReader) GetLinkFact(_ context.Context, _, _ uuid.UUID) (LinkFact, error) {
	f.calls++
	return f.fact, f.err
}

func newAuthzRouter(svc service.Service, opts ...Option) http.Handler {
	r := chi.NewRouter()
	NewHandler(svc, opts...).Register(r)
	return r
}

func authzRequest(t *testing.T, router http.Handler, method, path, body string, tenantID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	request.Header.Set("X-Tenant-Id", tenantID.String())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	return rec
}

func authzObject(id, tenantID uuid.UUID) domain.Object {
	return domain.Object{
		ID: id, TenantID: tenantID, ObjectTypeID: authzTypeID, Version: 1,
		Properties: map[string]any{"name": "trail"},
	}
}

func TestCreateObjectAuthorizesObjectType(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	reader := &fakeObjectTypeReader{fact: ObjectTypeFact{ID: authzTypeID.String(), TenantID: authzTenantID.String()}}
	created := false
	svc := &authzFakeService{createObject: func(context.Context, uuid.UUID, uuid.UUID, map[string]any) (domain.Object, error) {
		created = true
		return authzObject(authzObjectID, authzTenantID), nil
	}}
	router := newAuthzRouter(svc, WithDecisionGuard(guard), WithObjectTypeReader(reader))
	body := fmt.Sprintf(`{"object_type_id":%q,"properties":{"name":"trail"}}`, authzTypeID.String())
	rec := authzRequest(t, router, http.MethodPost, "/", body, authzTenantID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	if !created {
		t.Fatal("CreateObject did not run after the guard allowed the request")
	}
	if got := rec.Header().Get(authz.DecisionHeader); got != "decision-test" {
		t.Fatalf("decision header = %q, want decision-test", got)
	}
	actions, facts := guard.Calls()
	if !reflect.DeepEqual(actions, []authz.Action{"object:create"}) {
		t.Fatalf("actions = %v, want [object:create]", actions)
	}
	wantFact := authz.ResourceFact{Type: "object_type", ID: authzTypeID.String(), TenantID: authzTenantID.String()}
	if len(facts) != 1 || !reflect.DeepEqual(facts[0], wantFact) {
		t.Fatalf("facts = %+v, want [%+v]", facts, wantFact)
	}
}

func TestCreateObjectObjectTypeNotFoundBeforeAuthorization(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	reader := &fakeObjectTypeReader{err: domain.ErrNotFound}
	created := false
	svc := &authzFakeService{createObject: func(context.Context, uuid.UUID, uuid.UUID, map[string]any) (domain.Object, error) {
		created = true
		return authzObject(authzObjectID, authzTenantID), nil
	}}
	router := newAuthzRouter(svc, WithDecisionGuard(guard), WithObjectTypeReader(reader))
	body := fmt.Sprintf(`{"object_type_id":%q,"properties":{}}`, authzTypeID.String())
	rec := authzRequest(t, router, http.MethodPost, "/", body, authzTenantID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
	if created {
		t.Fatal("CreateObject ran despite the object type missing in the tenant")
	}
	if actions, _ := guard.Calls(); len(actions) != 0 {
		t.Fatalf("guard was called before the tenant-scoped object type load: actions = %v", actions)
	}
}

func TestCreateObjectMissingObjectTypeReaderIsUnavailable(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	router := newAuthzRouter(&authzFakeService{}, WithDecisionGuard(guard))
	body := fmt.Sprintf(`{"object_type_id":%q,"properties":{}}`, authzTypeID.String())
	rec := authzRequest(t, router, http.MethodPost, "/", body, authzTenantID)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	if actions, _ := guard.Calls(); len(actions) != 0 {
		t.Fatalf("guard must not be called without the tenant-scoped object type reader: actions = %v", actions)
	}
}

func TestObjectReadUpdateDeleteAuthorizeObjectFact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantAction authz.Action
		wantStatus int
	}{
		{"read", http.MethodGet, "/" + authzObjectID.String(), "", "object:read", http.StatusOK},
		{"update", http.MethodPatch, "/" + authzObjectID.String(), `{"properties":{"name":"ridge"}}`, "object:update", http.StatusOK},
		{"delete", http.MethodDelete, "/" + authzObjectID.String(), "", "object:delete", http.StatusNoContent},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			guard := authtest.Guard()
			svc := &authzFakeService{
				getObject: func(context.Context, uuid.UUID, uuid.UUID) (domain.Object, error) {
					return authzObject(authzObjectID, authzTenantID), nil
				},
				updateObject: func(context.Context, uuid.UUID, uuid.UUID, map[string]any, bool) (domain.Object, error) {
					return authzObject(authzObjectID, authzTenantID), nil
				},
				deleteObject: func(context.Context, uuid.UUID, uuid.UUID) error { return nil },
			}
			router := newAuthzRouter(svc, WithDecisionGuard(guard))
			rec := authzRequest(t, router, tc.method, tc.path, tc.body, authzTenantID)
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
				Type: "object", ID: authzObjectID.String(), TenantID: authzTenantID.String(),
				Ancestors: []authz.Ancestor{{Type: "object_type", ID: authzTypeID.String()}},
			}
			if len(facts) != 1 || !reflect.DeepEqual(facts[0], wantFact) {
				t.Fatalf("facts = %+v, want [%+v]", facts, wantFact)
			}
		})
	}
}

func TestObjectDenySkipsSideEffect(t *testing.T) {
	t.Parallel()
	guard := authtest.Deny(authz.ReasonRoleNotPermitted)
	deleted := false
	svc := &authzFakeService{
		getObject: func(context.Context, uuid.UUID, uuid.UUID) (domain.Object, error) {
			return authzObject(authzObjectID, authzTenantID), nil
		},
		deleteObject: func(context.Context, uuid.UUID, uuid.UUID) error {
			deleted = true
			return nil
		},
	}
	router := newAuthzRouter(svc, WithDecisionGuard(guard))
	rec := authzRequest(t, router, http.MethodDelete, "/"+authzObjectID.String(), "", authzTenantID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	if deleted {
		t.Fatal("DeleteObject ran despite a deny decision")
	}
	actions, _ := guard.Calls()
	if !reflect.DeepEqual(actions, []authz.Action{"object:delete"}) {
		t.Fatalf("actions = %v, want [object:delete]", actions)
	}
}

func TestObjectCrossTenantIsNotFoundBeforeAuthorization(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		obj  domain.Object
		err  error
	}{
		{"missing", domain.Object{}, domain.ErrNotFound},
		{"foreign tenant", authzObject(authzObjectID, otherTenantID), nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			guard := authtest.Guard()
			svc := &authzFakeService{getObject: func(context.Context, uuid.UUID, uuid.UUID) (domain.Object, error) {
				return tc.obj, tc.err
			}}
			router := newAuthzRouter(svc, WithDecisionGuard(guard))
			rec := authzRequest(t, router, http.MethodGet, "/"+authzObjectID.String(), "", authzTenantID)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
			}
			if actions, facts := guard.Calls(); len(actions) != 0 || len(facts) != 0 {
				t.Fatalf("guard was called before the tenant-scoped load: actions = %v, facts = %+v", actions, facts)
			}
		})
	}
}

func TestListObjectsAuthorizesByFilter(t *testing.T) {
	t.Parallel()
	t.Run("object_type filter authorizes object type", func(t *testing.T) {
		t.Parallel()
		guard := authtest.Guard()
		reader := &fakeObjectTypeReader{fact: ObjectTypeFact{ID: authzTypeID.String(), TenantID: authzTenantID.String()}}
		svc := &authzFakeService{listObjects: func(context.Context, uuid.UUID, uuid.UUID, int, int) ([]domain.Object, int64, error) {
			return nil, 0, nil
		}}
		router := newAuthzRouter(svc, WithDecisionGuard(guard), WithObjectTypeReader(reader))
		rec := authzRequest(t, router, http.MethodGet, "/?object_type_id="+authzTypeID.String(), "", authzTenantID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		actions, facts := guard.Calls()
		if !reflect.DeepEqual(actions, []authz.Action{"object:read"}) {
			t.Fatalf("actions = %v, want [object:read]", actions)
		}
		wantFact := authz.ResourceFact{Type: "object_type", ID: authzTypeID.String(), TenantID: authzTenantID.String()}
		if len(facts) != 1 || !reflect.DeepEqual(facts[0], wantFact) {
			t.Fatalf("facts = %+v, want [%+v]", facts, wantFact)
		}
	})
	t.Run("tenant list authorizes tenant", func(t *testing.T) {
		t.Parallel()
		guard := authtest.Guard()
		svc := &authzFakeService{listObjects: func(context.Context, uuid.UUID, uuid.UUID, int, int) ([]domain.Object, int64, error) {
			return nil, 0, nil
		}}
		router := newAuthzRouter(svc, WithDecisionGuard(guard))
		rec := authzRequest(t, router, http.MethodGet, "/", "", authzTenantID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		actions, facts := guard.Calls()
		if !reflect.DeepEqual(actions, []authz.Action{"object:read"}) {
			t.Fatalf("actions = %v, want [object:read]", actions)
		}
		wantFact := authz.ResourceFact{Type: "tenant", ID: authzTenantID.String(), TenantID: authzTenantID.String()}
		if len(facts) != 1 || !reflect.DeepEqual(facts[0], wantFact) {
			t.Fatalf("facts = %+v, want [%+v]", facts, wantFact)
		}
	})
}

func TestCreateLinkAuthorizesSourceObjectAndRequiresSameTenantTarget(t *testing.T) {
	t.Parallel()
	t.Run("allowed", func(t *testing.T) {
		t.Parallel()
		guard := authtest.Guard()
		created := false
		svc := &authzFakeService{
			getObject: func(_ context.Context, _ uuid.UUID, id uuid.UUID) (domain.Object, error) {
				switch id {
				case authzSourceID:
					return authzObject(authzSourceID, authzTenantID), nil
				case authzTargetID:
					return authzObject(authzTargetID, authzTenantID), nil
				default:
					return domain.Object{}, domain.ErrNotFound
				}
			},
			createLink: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, map[string]any) (domain.Link, error) {
				created = true
				return domain.Link{ID: authzLinkID, TenantID: authzTenantID, LinkTypeID: authzLinkTypeID, SourceID: authzSourceID, TargetID: authzTargetID}, nil
			},
		}
		router := newAuthzRouter(svc, WithDecisionGuard(guard))
		body := fmt.Sprintf(`{"link_type_id":%q,"source_id":%q,"target_id":%q}`, authzLinkTypeID.String(), authzSourceID.String(), authzTargetID.String())
		rec := authzRequest(t, router, http.MethodPost, "/links", body, authzTenantID)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
		}
		if !created {
			t.Fatal("CreateLink did not run after the guard allowed the request")
		}
		actions, facts := guard.Calls()
		if !reflect.DeepEqual(actions, []authz.Action{"object_link:create"}) {
			t.Fatalf("actions = %v, want [object_link:create]", actions)
		}
		wantFact := authz.ResourceFact{
			Type: "object", ID: authzSourceID.String(), TenantID: authzTenantID.String(),
			Ancestors: []authz.Ancestor{{Type: "object_type", ID: authzTypeID.String()}},
		}
		if len(facts) != 1 || !reflect.DeepEqual(facts[0], wantFact) {
			t.Fatalf("facts = %+v, want [%+v]", facts, wantFact)
		}
	})
	t.Run("target in another tenant is not found", func(t *testing.T) {
		t.Parallel()
		guard := authtest.Guard()
		created := false
		svc := &authzFakeService{
			getObject: func(_ context.Context, _ uuid.UUID, id uuid.UUID) (domain.Object, error) {
				switch id {
				case authzSourceID:
					return authzObject(authzSourceID, authzTenantID), nil
				case authzTargetID:
					return domain.Object{}, domain.ErrNotFound
				default:
					return domain.Object{}, domain.ErrNotFound
				}
			},
			createLink: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, map[string]any) (domain.Link, error) {
				created = true
				return domain.Link{}, nil
			},
		}
		router := newAuthzRouter(svc, WithDecisionGuard(guard))
		body := fmt.Sprintf(`{"link_type_id":%q,"source_id":%q,"target_id":%q}`, authzLinkTypeID.String(), authzSourceID.String(), authzTargetID.String())
		rec := authzRequest(t, router, http.MethodPost, "/links", body, authzTenantID)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
		}
		if created {
			t.Fatal("CreateLink ran despite the cross-tenant target")
		}
		if actions, _ := guard.Calls(); len(actions) != 0 {
			t.Fatalf("guard was called before both link endpoints were loaded: actions = %v", actions)
		}
	})
}

func TestListLinksAuthorizesSourceObject(t *testing.T) {
	t.Parallel()
	guard := authtest.Guard()
	svc := &authzFakeService{
		getObject: func(context.Context, uuid.UUID, uuid.UUID) (domain.Object, error) {
			return authzObject(authzSourceID, authzTenantID), nil
		},
		listLinks: func(context.Context, uuid.UUID, uuid.UUID, int, int) ([]domain.Link, int64, error) {
			return nil, 0, nil
		},
	}
	router := newAuthzRouter(svc, WithDecisionGuard(guard))
	rec := authzRequest(t, router, http.MethodGet, "/"+authzSourceID.String()+"/links", "", authzTenantID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	actions, facts := guard.Calls()
	if !reflect.DeepEqual(actions, []authz.Action{"object_link:read"}) {
		t.Fatalf("actions = %v, want [object_link:read]", actions)
	}
	if len(facts) != 1 || facts[0].Type != "object" || facts[0].ID != authzSourceID.String() {
		t.Fatalf("facts = %+v, want source object %s", facts, authzSourceID)
	}
}

func TestDeleteLinkAuthorizesSourceObjectThroughLinkReader(t *testing.T) {
	t.Parallel()
	t.Run("allowed", func(t *testing.T) {
		t.Parallel()
		guard := authtest.Guard()
		reader := &fakeLinkReader{fact: LinkFact{
			ID: authzLinkID.String(), TenantID: authzTenantID.String(),
			SourceID: authzSourceID.String(), TargetID: authzTargetID.String(),
		}}
		deleted := false
		svc := &authzFakeService{
			getObject: func(_ context.Context, _ uuid.UUID, id uuid.UUID) (domain.Object, error) {
				switch id {
				case authzSourceID, authzTargetID:
					return authzObject(id, authzTenantID), nil
				default:
					return domain.Object{}, domain.ErrNotFound
				}
			},
			deleteLink: func(context.Context, uuid.UUID, uuid.UUID) error {
				deleted = true
				return nil
			},
		}
		router := newAuthzRouter(svc, WithDecisionGuard(guard), WithLinkReader(reader))
		rec := authzRequest(t, router, http.MethodDelete, "/links/"+authzLinkID.String(), "", authzTenantID)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204; body = %s", rec.Code, rec.Body.String())
		}
		if !deleted {
			t.Fatal("DeleteLink did not run after the guard allowed the request")
		}
		actions, facts := guard.Calls()
		if !reflect.DeepEqual(actions, []authz.Action{"object_link:delete"}) {
			t.Fatalf("actions = %v, want [object_link:delete]", actions)
		}
		if len(facts) != 1 || facts[0].Type != "object" || facts[0].ID != authzSourceID.String() {
			t.Fatalf("facts = %+v, want source object %s", facts, authzSourceID)
		}
	})
	t.Run("missing link reader falls back to tenant scope", func(t *testing.T) {
		t.Parallel()
		guard := authtest.Guard()
		svc := &authzFakeService{deleteLink: func(context.Context, uuid.UUID, uuid.UUID) error { return nil }}
		router := newAuthzRouter(svc, WithDecisionGuard(guard))
		rec := authzRequest(t, router, http.MethodDelete, "/links/"+authzLinkID.String(), "", authzTenantID)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204; body = %s", rec.Code, rec.Body.String())
		}
		actions, facts := guard.Calls()
		if len(actions) != 1 || actions[0] != "object_link:delete" {
			t.Fatalf("actions = %v, want [object_link:delete]", actions)
		}
		if len(facts) != 1 || facts[0].Type != "tenant" || facts[0].ID != authzTenantID.String() {
			t.Fatalf("facts = %+v, want tenant %s", facts, authzTenantID)
		}
	})
}
