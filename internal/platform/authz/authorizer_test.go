package authz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
	"github.com/klb3713/ontoaxis/policies"
)

type fakeFGA struct {
	allowed bool
	err     error
	startup error
	checks  []CheckInput
}

func (f *fakeFGA) Check(_ context.Context, input CheckInput) (CheckResult, error) {
	f.checks = append(f.checks, input)
	if f.err != nil {
		return CheckResult{}, f.err
	}
	return CheckResult{Allowed: f.allowed, ModelID: "model-test"}, nil
}
func (f *fakeFGA) Startup(context.Context) error { return f.startup }
func (f *fakeFGA) Close() error                  { return nil }

func testContext(t *testing.T, roles ...string) (context.Context, uuid.UUID, uuid.UUID) {
	t.Helper()
	tenantID, userID := uuid.New(), uuid.New()
	ctx := authn.WithTenantContext(context.Background(), authn.TenantContext{
		TenantID:  tenantID,
		UserID:    userID,
		Roles:     roles,
		RequestID: "req-test",
	})
	ctx = observability.WithRequestID(ctx, "req-test")
	return WithRequestMemo(ctx), tenantID, userID
}

func newTestAuthorizer(t *testing.T, fga FGAClient, writer *bytes.Buffer) *Authorizer {
	t.Helper()
	bundle, err := LoadBundle(policies.FS)
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatalf("default catalog: %v", err)
	}
	if err := ValidateCatalogAgainstSchema(catalog, bundle); err != nil {
		t.Fatalf("catalog/schema mismatch: %v", err)
	}
	engine, err := newCedarEngine(bundle, catalog)
	if err != nil {
		t.Fatalf("cedar engine: %v", err)
	}
	if writer == nil {
		writer = &bytes.Buffer{}
	}
	return &Authorizer{
		cfg: Config{
			OpenFGAApiURL:   "http://openfga.invalid",
			OpenFGAStoreID:  "store-test",
			OpenFGAModelID:  "model-test",
			OpenFGATimeout:  200 * time.Millisecond,
			DecisionTimeout: 300 * time.Millisecond,
			AuditSink:       DefaultAuditSink,
		},
		catalog: catalog,
		bundle:  bundle,
		cedar:   engine,
		fga:     fga,
		auditor: NewAuditor(writer, "model-test", bundle.ModelDigest, bundle.Digest, "test-commit"),
	}
}

func TestAuthorizeViewerReadAllowed(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "viewer")
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	decision, err := a.Authorize(ctx, Request{
		Action:   "dataset:read",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()},
	})
	if err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
	if !decision.Allow || decision.Reason != ReasonAllow {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	if decision.Policy.FGARelation != "can_read" || decision.Policy.CedarDecision != "allow" {
		t.Fatalf("unexpected policy facts: %+v", decision.Policy)
	}
}

func TestAuthorizeViewerWriteDenied(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "viewer")
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	decision, err := a.Authorize(ctx, Request{
		Action:   "dataset:update",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()},
	})
	if err == nil {
		t.Fatal("expected denial")
	}
	var authzErr *Error
	if !errors.As(err, &authzErr) || authzErr.HTTPStatus() != 403 {
		t.Fatalf("expected 403 authz error, got %v", err)
	}
	if decision.Allow || decision.Reason != ReasonRoleNotPermitted {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestAuthorizeMemberAdminDenied(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "member")
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	decision, err := a.Authorize(ctx, Request{
		Action:   "tenancy:organization:update",
		Resource: ResourceFact{Type: "organization", ID: "org-1", TenantID: tenantID.String()},
	})
	if err == nil || decision.Reason != ReasonAdminRequired {
		t.Fatalf("expected admin_required denial, got decision=%+v err=%v", decision, err)
	}
}

func TestAuthorizeTenantAdminAdminAllowed(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "tenant-admin")
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	decision, err := a.Authorize(ctx, Request{
		Action:   "tenancy:organization:update",
		Resource: ResourceFact{Type: "organization", ID: "org-1", TenantID: tenantID.String()},
	})
	if err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
	if !decision.Allow {
		t.Fatalf("expected allow decision: %+v", decision)
	}
}

func TestAuthorizeCrossTenantDeniedWithoutFGA(t *testing.T) {
	ctx, _, _ := testContext(t, "member")
	fga := &fakeFGA{allowed: true}
	a := newTestAuthorizer(t, fga, nil)
	decision, err := a.Authorize(ctx, Request{
		Action:   "dataset:read",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: uuid.New().String()},
	})
	if err == nil || decision.Reason != ReasonNoRelationship {
		t.Fatalf("expected cross-tenant denial, got decision=%+v err=%v", decision, err)
	}
	if len(fga.checks) != 0 {
		t.Fatal("cross-tenant request must not reach OpenFGA")
	}
}

func TestAuthorizeUnknownActionDenied(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "member")
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	decision, err := a.Authorize(ctx, Request{
		Action:   "unknown:action",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()},
	})
	if err == nil || decision.Reason != ReasonUnknownAction {
		t.Fatalf("expected unknown action denial, got decision=%+v err=%v", decision, err)
	}
}

func TestAuthorizeFGATimeoutFailsClosed(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "member")
	a := newTestAuthorizer(t, &fakeFGA{err: context.DeadlineExceeded}, nil)
	decision, err := a.Authorize(ctx, Request{
		Action:   "dataset:read",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()},
	})
	var authzErr *Error
	if !errors.As(err, &authzErr) || authzErr.Reason != ReasonEngineTimeout || authzErr.HTTPStatus() != 503 {
		t.Fatalf("expected engine_timeout 503, got %v", err)
	}
	if decision.Allow {
		t.Fatal("timeout must not allow")
	}
}

func TestAuthorizeAuditFailureFailsClosed(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "viewer")
	writer := &failingWriter{}
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	a.auditor = NewAuditor(writer, "model-test", a.bundle.ModelDigest, a.bundle.Digest, "test-commit")
	_, err := a.Authorize(ctx, Request{
		Action:   "dataset:read",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()},
	})
	var authzErr *Error
	if !errors.As(err, &authzErr) || authzErr.Reason != ReasonAuditUnavailable || authzErr.HTTPStatus() != 503 {
		t.Fatalf("expected audit_unavailable 503, got %v", err)
	}
}

func TestAuthorizeResourceStateDenied(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "member")
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	decision, err := a.Authorize(ctx, Request{
		Action: "pipeline:invoke",
		Resource: ResourceFact{
			Type: "pipeline", ID: "pl-1", TenantID: tenantID.String(),
			Status: "archived",
		},
	})
	if err == nil || decision.Reason != ReasonResourceStateDenied {
		t.Fatalf("expected resource_state_denied, got decision=%+v err=%v", decision, err)
	}
}

func TestAuthorizeAllowIsMemoizedPerRequest(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "viewer")
	fga := &fakeFGA{allowed: true}
	a := newTestAuthorizer(t, fga, nil)
	req := Request{Action: "dataset:read", Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()}}
	if _, err := a.Authorize(ctx, req); err != nil {
		t.Fatalf("first authorize: %v", err)
	}
	if _, err := a.Authorize(ctx, req); err != nil {
		t.Fatalf("second authorize: %v", err)
	}
	if len(fga.checks) != 1 {
		t.Fatalf("expected one OpenFGA check, got %d", len(fga.checks))
	}
}

type failingWriter struct{}

func (f *failingWriter) Write([]byte) (int, error) { return 0, errors.New("sink down") }

func TestAuditEventShape(t *testing.T) {
	var buf bytes.Buffer
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, &buf)
	ctx, tenantID, _ := testContext(t, "viewer")
	ctx = WithAuditMeta(ctx, "GET", "/v1/datasets/{id}")
	if _, err := a.Authorize(ctx, Request{
		Action:   "dataset:read",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()},
	}); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	line := strings.TrimSpace(buf.String())
	if strings.Contains(line, "token") || strings.Contains(line, "Authorization") {
		t.Fatalf("audit event leaked credentials: %s", line)
	}
	var envelope struct {
		Authz struct {
			EventType  string `json:"event_type"`
			Decision   string `json:"decision"`
			DecisionID string `json:"decision_id"`
			RequestID  string `json:"request_id"`
			HTTP       struct {
				Method string `json:"method"`
				Route  string `json:"route"`
			} `json:"http"`
			Policy struct {
				FGARelation string `json:"fga_relation"`
			} `json:"policy"`
		} `json:"authz"`
	}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Fatalf("decode audit event %q: %v", line, err)
	}
	if envelope.Authz.EventType != "authz.decision" || envelope.Authz.Decision != "allow" {
		t.Fatalf("unexpected audit event: %+v", envelope.Authz)
	}
	if envelope.Authz.DecisionID == "" || envelope.Authz.RequestID != "req-test" {
		t.Fatalf("missing decision/request id: %+v", envelope.Authz)
	}
	if envelope.Authz.HTTP.Method != "GET" || envelope.Authz.HTTP.Route != "/v1/datasets/{id}" {
		t.Fatalf("missing http metadata: %+v", envelope.Authz.HTTP)
	}
	if envelope.Authz.Policy.FGARelation != "can_read" {
		t.Fatalf("missing fga relation: %+v", envelope.Authz.Policy)
	}
}

func TestBuildContextualTuplesTenantChain(t *testing.T) {
	tenantID, userID := uuid.New(), uuid.New()
	tc := authn.TenantContext{TenantID: tenantID, UserID: userID}
	tuples, err := BuildContextualTuples(tc, ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()})
	if err != nil {
		t.Fatalf("tuples: %v", err)
	}
	if len(tuples) != 2 {
		t.Fatalf("expected tenant member and dataset-tenant tuples, got %+v", tuples)
	}
	found := false
	for _, tuple := range tuples {
		if tuple.Object == "tenant:"+tenantID.String() && tuple.Relation == "member" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tenant member tuple missing: %+v", tuples)
	}
}

func TestBuildContextualTuplesRejectsOtherTenant(t *testing.T) {
	tc := authn.TenantContext{TenantID: uuid.New(), UserID: uuid.New()}
	if _, err := BuildContextualTuples(tc, ResourceFact{Type: "dataset", ID: "ds-1", TenantID: uuid.New().String()}); err == nil {
		t.Fatal("expected cross-tenant tuple error")
	}
}

func TestConfigRejectsDisabledAndFailOpen(t *testing.T) {
	t.Setenv("ONTOAXIS_AUTHZ_DISABLED", "true")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected disabled authorization to be rejected")
	}
}

func TestAuthorizeMissingResourceAttribute(t *testing.T) {
	ctx, _, _ := testContext(t, "member")
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	_, err := a.Authorize(ctx, Request{Action: "dataset:read", Resource: ResourceFact{Type: "dataset", ID: "", TenantID: ""}})
	var authzErr *Error
	if !errors.As(err, &authzErr) || authzErr.Reason != ReasonAttributeMissing {
		t.Fatalf("expected attribute_missing, got %v", err)
	}
}

func TestAuditDoesNotLeakEngineErrorText(t *testing.T) {
	var buf bytes.Buffer
	a := newTestAuthorizer(t, &fakeFGA{err: errors.New("Bearer super-secret-token")}, &buf)
	ctx, tenantID, _ := testContext(t, "member")
	_, _ = a.Authorize(ctx, Request{
		Action:   "dataset:read",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()},
	})
	if strings.Contains(buf.String(), "super-secret-token") {
		t.Fatalf("audit leaked engine error text: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"decision":"error"`) {
		t.Fatalf("expected error decision audit event: %s", buf.String())
	}
}

func TestBuildContextualTuplesMaxChainStaysUnderLimit(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "member")
	tc, _ := authn.FromContext(ctx)
	resource := ResourceFact{
		Type: "object", ID: "obj-1", TenantID: tenantID.String(),
		Ancestors: []Ancestor{
			{Type: "object_type", ID: "ot-1"},
			{Type: "project", ID: "p-1"},
			{Type: "workspace", ID: "w-1"},
			{Type: "organization", ID: "o-1"},
			{Type: "tenant", ID: tenantID.String()},
		},
	}
	tuples, err := BuildContextualTuples(tc, resource)
	if err != nil {
		t.Fatalf("BuildContextualTuples: %v", err)
	}
	if len(tuples) > maxContextualTuples {
		t.Fatalf("tuples = %d, want <= %d", len(tuples), maxContextualTuples)
	}
}
