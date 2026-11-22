package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/authz/authtest"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
	"github.com/klb3713/ontoaxis/internal/platform/search"
)

// fakeSearchBackend records the request it received and returns a fixed
// response or error.
type fakeSearchBackend struct {
	response SearchResponse
	err      error

	calls   int
	lastReq SearchRequest
}

func (f *fakeSearchBackend) Search(_ context.Context, req SearchRequest) (SearchResponse, error) {
	f.calls++
	f.lastReq = req
	return f.response, f.err
}

func (f *fakeSearchBackend) Healthy(context.Context) error { return nil }

// denyAtGuard denies every decision after the first allow, proving that every
// requested object type must be allowed.
type denyAtGuard struct {
	calls int
}

func (g *denyAtGuard) Require(_ context.Context, action authz.Action, resource authz.ResourceFact) (authz.Decision, error) {
	g.calls++
	if g.calls > 1 {
		return authz.Decision{}, &authz.Error{Reason: authz.ReasonRoleNotPermitted}
	}
	return authz.Decision{Allow: true, DecisionID: "decision-test", Action: action, Resource: resource}, nil
}

func (g *denyAtGuard) WriteHTTP(w http.ResponseWriter, r *http.Request, _ error) {
	w.WriteHeader(http.StatusForbidden)
}

func (g *denyAtGuard) SetDecisionHeader(http.ResponseWriter, authz.Decision) {}

func searchTenantContext(tenantID uuid.UUID) context.Context {
	return authn.WithTenantContext(context.Background(), authn.TenantContext{
		TenantID: tenantID,
		UserID:   uuid.New(),
	})
}

func newSearchRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	h.Register(r)
	return r
}

func doSearchRequest(t *testing.T, router chi.Router, ctx context.Context, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type searchErrorEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

func decodeSearchError(t *testing.T, rec *httptest.ResponseRecorder) searchErrorEnvelope {
	t.Helper()
	var envelope searchErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error body: %v; body = %s", err, rec.Body.String())
	}
	return envelope
}

func TestSearchRouteNotRegisteredWhenDisabled(t *testing.T) {
	backend := &fakeSearchBackend{}
	h := NewHandler(fakeService{}, WithSearch(backend, authtest.Guard(), false))
	if err := h.SearchStartupError(); err != nil {
		t.Fatalf("SearchStartupError() = %v, want nil when disabled", err)
	}
	rec := doSearchRequest(t, newSearchRouter(h), searchTenantContext(uuid.New()), "/search?q=trail", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a disabled search route; body = %s", rec.Code, rec.Body.String())
	}
	if backend.calls != 0 {
		t.Fatalf("backend calls = %d, want 0", backend.calls)
	}
}

func TestSearchFailsClosedWhenEnabledWithoutDependencies(t *testing.T) {
	tenantID := uuid.New()
	for _, tc := range []struct {
		name string
		h    *Handler
	}{
		{"nil backend", NewHandler(fakeService{}, WithSearch(nil, authtest.Guard(), true))},
		{"nil guard", NewHandler(fakeService{}, WithSearch(&fakeSearchBackend{}, nil, true))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.h.SearchStartupError(); err == nil {
				t.Fatal("SearchStartupError() = nil, want misconfiguration error")
			}
			rec := doSearchRequest(t, newSearchRouter(tc.h), searchTenantContext(tenantID), "/search?q=trail", nil)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
			}
			envelope := decodeSearchError(t, rec)
			if envelope.Error.Code != "unavailable" {
				t.Fatalf("error code = %q, want unavailable", envelope.Error.Code)
			}
		})
	}
}

func TestSearchRequiresAuthenticatedTenant(t *testing.T) {
	backend := &fakeSearchBackend{}
	h := NewHandler(fakeService{}, WithSearch(backend, authtest.Guard(), true))
	router := newSearchRouter(h)

	t.Run("no tenant context and no header", func(t *testing.T) {
		rec := doSearchRequest(t, router, nil, "/search?q=trail", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
		}
		if envelope := decodeSearchError(t, rec); envelope.Error.Code != "unauthorized" {
			t.Fatalf("error code = %q, want unauthorized", envelope.Error.Code)
		}
	})

	t.Run("X-Tenant-Id header is not an identity fallback", func(t *testing.T) {
		rec := doSearchRequest(t, router, nil, "/search?q=trail", map[string]string{"X-Tenant-Id": uuid.New().String()})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 even with X-Tenant-Id; body = %s", rec.Code, rec.Body.String())
		}
	})
	if backend.calls != 0 {
		t.Fatalf("backend calls = %d, want 0", backend.calls)
	}
}

func TestSearchRejectsInvalidParameters(t *testing.T) {
	tenantID := uuid.New()

	tooManyTypes := url.Values{"q": []string{"trail"}}
	for i := 0; i < maxSearchObjectTypes+1; i++ {
		tooManyTypes.Add("object_type_id", uuid.New().String())
	}
	validTwoTypes := url.Values{"q": []string{"trail"}}
	validTwoTypes.Add("object_type_id", uuid.New().String())
	validTwoTypes.Add("object_type_id", uuid.New().String())

	cases := []struct {
		name   string
		target string
	}{
		{"missing q and object_type_id", "/search"},
		{"blank q", "/search?q=%20%20"},
		{"invalid object type uuid", "/search?object_type_id=not-a-uuid"},
		{"nil object type uuid", "/search?object_type_id=00000000-0000-0000-0000-000000000000"},
		{"too many object types", "/search?" + tooManyTypes.Encode()},
		{"query too long", "/search?" + url.Values{"q": []string{strings.Repeat("a", maxSearchQueryLength+1)}}.Encode()},
		{"limit zero", "/search?q=trail&limit=0"},
		{"limit too high", "/search?q=trail&limit=101"},
		{"limit not an integer", "/search?q=trail&limit=abc"},
		{"offset negative", "/search?q=trail&offset=-1"},
		{"offset too high", "/search?q=trail&offset=10000"},
		{"offset not an integer", "/search?q=trail&offset=abc"},
		{"invalid updated_after", "/search?q=trail&updated_after=yesterday"},
		{"invalid updated_before", "/search?q=trail&updated_before=2026-01-01"},
		{"invalid sort field", "/search?q=trail&sort=name"},
		{"invalid sort direction", "/search?q=trail&sort=updated_at:sideways"},
		{"empty sort direction", "/search?q=trail&sort=updated_at:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeSearchBackend{}
			h := NewHandler(fakeService{}, WithSearch(backend, authtest.Guard(), true))
			rec := doSearchRequest(t, newSearchRouter(h), searchTenantContext(tenantID), tc.target, nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
			}
			if envelope := decodeSearchError(t, rec); envelope.Error.Code != "bad_request" {
				t.Fatalf("error code = %q, want bad_request", envelope.Error.Code)
			}
			if backend.calls != 0 {
				t.Fatalf("backend calls = %d, want 0", backend.calls)
			}
		})
	}

	// Sanity check that the helper used for the "too many" case is otherwise a
	// valid request: two object types are accepted by the parser.
	backend := &fakeSearchBackend{}
	h := NewHandler(fakeService{}, WithSearch(backend, authtest.Guard(), true))
	rec := doSearchRequest(t, newSearchRouter(h), searchTenantContext(tenantID), "/search?"+validTwoTypes.Encode(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for two valid object types; body = %s", rec.Code, rec.Body.String())
	}
}

func TestSearchDeniedGuardReturnsForbiddenWithoutItems(t *testing.T) {
	backend := &fakeSearchBackend{}
	h := NewHandler(fakeService{}, WithSearch(backend, authtest.Deny(authz.ReasonRoleNotPermitted), true))
	rec := doSearchRequest(t, newSearchRouter(h), searchTenantContext(uuid.New()), "/search?q=trail", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, `"items"`) || strings.Contains(body, `"total"`) {
		t.Fatalf("deny body must not contain items/total: %s", body)
	}
	if envelope := decodeSearchError(t, rec); envelope.Error.Code != "forbidden" {
		t.Fatalf("error code = %q, want forbidden", envelope.Error.Code)
	}
	if backend.calls != 0 {
		t.Fatalf("backend calls = %d, want 0", backend.calls)
	}
}

func TestSearchBackendUnavailable(t *testing.T) {
	backend := &fakeSearchBackend{err: fmt.Errorf("upstream down: %w", search.ErrUnavailable)}
	h := NewHandler(fakeService{}, WithSearch(backend, authtest.Guard(), true))
	rec := doSearchRequest(t, newSearchRouter(h), searchTenantContext(uuid.New()), "/search?q=trail", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	envelope := decodeSearchError(t, rec)
	if envelope.Error.Code != "unavailable" {
		t.Fatalf("error code = %q, want unavailable", envelope.Error.Code)
	}
	if !envelope.Error.Retryable {
		t.Fatal("retryable = false, want true for search.ErrUnavailable")
	}
}

func TestSearchBackendInvalidQueryIsBadRequest(t *testing.T) {
	backend := &fakeSearchBackend{err: fmt.Errorf("backend rejected: %w", search.ErrInvalid)}
	h := NewHandler(fakeService{}, WithSearch(backend, authtest.Guard(), true))
	rec := doSearchRequest(t, newSearchRouter(h), searchTenantContext(uuid.New()), "/search?q=trail", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if envelope := decodeSearchError(t, rec); envelope.Error.Code != "bad_request" {
		t.Fatalf("error code = %q, want bad_request", envelope.Error.Code)
	}
}

func TestSearchSuccessResponseShape(t *testing.T) {
	tenantID := uuid.New()
	typeID := uuid.New()
	updatedAt := time.Date(2026, 11, 22, 10, 0, 0, 123456789, time.UTC)
	backend := &fakeSearchBackend{response: SearchResponse{
		Items: []SearchHit{{
			ID:           uuid.New().String(),
			ObjectTypeID: typeID.String(),
			DisplayName:  "Trail Alpha",
			Version:      3,
			UpdatedAt:    updatedAt,
		}},
		Total:     1,
		Truncated: false,
	}}
	guard := authtest.Guard()
	h := NewHandler(fakeService{}, WithSearch(backend, guard, true))
	ctx := observability.WithRequestID(searchTenantContext(tenantID), "req-ctx-1")
	rec := doSearchRequest(t, newSearchRouter(h), ctx, "/search?q=trail", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get(authz.DecisionHeader) != "decision-test" {
		t.Fatalf("decision header = %q, want decision-test", rec.Header().Get(authz.DecisionHeader))
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
	}
	for _, key := range []string{"items", "total", "limit", "offset", "truncated", "request_id"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("response is missing %q: %s", key, rec.Body.String())
		}
	}
	if body["request_id"] != "req-ctx-1" {
		t.Fatalf("request_id = %v, want req-ctx-1", body["request_id"])
	}
	if body["total"] != float64(1) || body["limit"] != float64(defaultSearchLimit) || body["offset"] != float64(0) {
		t.Fatalf("unexpected pagination fields: %v", body)
	}
	rawItems, ok := body["items"].([]any)
	if !ok || len(rawItems) != 1 {
		t.Fatalf("items = %v, want one item", body["items"])
	}
	item := rawItems[0].(map[string]any)
	for _, key := range []string{"id", "object_type_id", "display_name", "version", "updated_at"} {
		if _, ok := item[key]; !ok {
			t.Fatalf("item is missing %q: %v", key, item)
		}
	}
	if _, exists := item["properties"]; exists {
		t.Fatalf("item must not contain properties: %v", item)
	}
	if _, exists := item["tenant_id"]; exists {
		t.Fatalf("item must not contain tenant_id: %v", item)
	}
	if item["display_name"] != "Trail Alpha" || item["version"] != float64(3) {
		t.Fatalf("unexpected item: %v", item)
	}
	if item["updated_at"] != "2026-11-22T10:00:00.123456789Z" {
		t.Fatalf("updated_at = %v", item["updated_at"])
	}
	if body["truncated"] != false {
		t.Fatalf("truncated = %v, want false", body["truncated"])
	}

	if backend.lastReq.TenantID != tenantID {
		t.Fatalf("backend tenant = %s, want %s", backend.lastReq.TenantID, tenantID)
	}
	if backend.lastReq.Query != "trail" || backend.lastReq.Limit != defaultSearchLimit || backend.lastReq.Offset != 0 || backend.lastReq.Sort != "" {
		t.Fatalf("unexpected backend request: %+v", backend.lastReq)
	}
}

func TestSearchRequestIDHeaderFallback(t *testing.T) {
	backend := &fakeSearchBackend{response: SearchResponse{Items: nil, Total: 0}}
	h := NewHandler(fakeService{}, WithSearch(backend, authtest.Guard(), true))
	rec := doSearchRequest(t, newSearchRouter(h), searchTenantContext(uuid.New()), "/search?q=trail",
		map[string]string{"X-Request-ID": "req-header-9"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items     []any  `json:"items"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.RequestID != "req-header-9" {
		t.Fatalf("request_id = %q, want req-header-9", body.RequestID)
	}
	if body.Items == nil {
		t.Fatalf("items must be an empty array, got null: %s", rec.Body.String())
	}
}

func TestSearchParsesFiltersAndSort(t *testing.T) {
	tenantID := uuid.New()
	typeOne := uuid.New()
	typeTwo := uuid.New()
	after := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	before := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	params := url.Values{
		"q":              []string{"  trail  "},
		"object_type_id": []string{typeOne.String(), typeTwo.String()},
		"updated_after":  []string{after.Format(time.RFC3339)},
		"updated_before": []string{before.Format(time.RFC3339)},
		"limit":          []string{"50"},
		"offset":         []string{"10"},
		"sort":           []string{"-updated_at"},
	}
	backend := &fakeSearchBackend{response: SearchResponse{Items: []SearchHit{}, Total: 0, Truncated: true}}
	guard := authtest.Guard()
	h := NewHandler(fakeService{}, WithSearch(backend, guard, true))
	rec := doSearchRequest(t, newSearchRouter(h), searchTenantContext(tenantID), "/search?"+params.Encode(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	req := backend.lastReq
	if req.Query != "trail" {
		t.Fatalf("query = %q, want trimmed trail", req.Query)
	}
	if len(req.ObjectTypeIDs) != 2 || req.ObjectTypeIDs[0] != typeOne || req.ObjectTypeIDs[1] != typeTwo {
		t.Fatalf("object type ids = %v", req.ObjectTypeIDs)
	}
	if req.UpdatedAfter == nil || !req.UpdatedAfter.Equal(after) {
		t.Fatalf("updated_after = %v, want %v", req.UpdatedAfter, after)
	}
	if req.UpdatedBefore == nil || !req.UpdatedBefore.Equal(before) {
		t.Fatalf("updated_before = %v, want %v", req.UpdatedBefore, before)
	}
	if req.Limit != 50 || req.Offset != 10 || req.Sort != "updated_at:desc" {
		t.Fatalf("unexpected pagination/sort: %+v", req)
	}

	var body searchHTTPResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Limit != 50 || body.Offset != 10 || !body.Truncated {
		t.Fatalf("unexpected response pagination: %+v", body)
	}

	actions, facts := guard.Calls()
	if len(actions) != 2 || len(facts) != 2 {
		t.Fatalf("guard calls = %d actions, %d facts; want 2 and 2", len(actions), len(facts))
	}
	for i, fact := range facts {
		if actions[i] != "object:read" {
			t.Fatalf("action[%d] = %q, want object:read", i, actions[i])
		}
		if fact.Type != "object_type" || fact.TenantID != tenantID.String() {
			t.Fatalf("fact[%d] = %+v", i, fact)
		}
	}
	if facts[0].ID != typeOne.String() || facts[1].ID != typeTwo.String() {
		t.Fatalf("fact ids = %q, %q", facts[0].ID, facts[1].ID)
	}
}

func TestSearchTenantScopeAuthorizationFact(t *testing.T) {
	tenantID := uuid.New()
	backend := &fakeSearchBackend{response: SearchResponse{Items: []SearchHit{}}}
	guard := authtest.Guard()
	h := NewHandler(fakeService{}, WithSearch(backend, guard, true))
	rec := doSearchRequest(t, newSearchRouter(h), searchTenantContext(tenantID), "/search?q=trail", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	actions, facts := guard.Calls()
	if len(actions) != 1 || len(facts) != 1 {
		t.Fatalf("guard calls = %d actions, %d facts; want 1 and 1", len(actions), len(facts))
	}
	if actions[0] != "object:read" {
		t.Fatalf("action = %q, want object:read", actions[0])
	}
	if facts[0].Type != "tenant" || facts[0].ID != tenantID.String() || facts[0].TenantID != tenantID.String() {
		t.Fatalf("fact = %+v", facts[0])
	}
}

func TestSearchMultipleObjectTypesAllMustAllow(t *testing.T) {
	params := url.Values{"q": []string{"trail"}}
	params.Add("object_type_id", uuid.New().String())
	params.Add("object_type_id", uuid.New().String())
	backend := &fakeSearchBackend{response: SearchResponse{Items: []SearchHit{}}}
	guard := &denyAtGuard{}
	h := NewHandler(fakeService{}, WithSearch(backend, guard, true))
	rec := doSearchRequest(t, newSearchRouter(h), searchTenantContext(uuid.New()), "/search?"+params.Encode(), nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	if guard.calls != 2 {
		t.Fatalf("guard calls = %d, want 2 (one per object type)", guard.calls)
	}
	if backend.calls != 0 {
		t.Fatalf("backend calls = %d, want 0", backend.calls)
	}
}

func TestParseSearchSort(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"", "", true},
		{"updated_at", "updated_at:asc", true},
		{"updated_at:asc", "updated_at:asc", true},
		{"updated_at:desc", "updated_at:desc", true},
		{"-updated_at", "updated_at:desc", true},
		{"created_at", "created_at:asc", true},
		{"created_at:asc", "created_at:asc", true},
		{"created_at:desc", "created_at:desc", true},
		{"-created_at", "created_at:desc", true},
		{"name", "", false},
		{"updated_at:sideways", "", false},
		{"updated_at:", "", false},
		{"updated_at:desc:extra", "", false},
		{"-", "", false},
	}
	for _, tc := range cases {
		got, ok := parseSearchSort(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("parseSearchSort(%q) = %q, %v; want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSearchOversizedBodyIsNotRelevant(t *testing.T) {
	// Search is a GET endpoint: it must not read a request body. This guards
	// against accidentally wiring the search handler through decode().
	backend := &fakeSearchBackend{response: SearchResponse{Items: []SearchHit{}}}
	h := NewHandler(fakeService{}, WithSearch(backend, authtest.Guard(), true))
	req := httptest.NewRequest(http.MethodGet, "/search?q=trail", strings.NewReader(`{"unexpected":true}`))
	req = req.WithContext(searchTenantContext(uuid.New()))
	rec := httptest.NewRecorder()
	newSearchRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
}
