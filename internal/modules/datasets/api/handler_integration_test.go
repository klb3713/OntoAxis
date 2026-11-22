//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	fgaSdk "github.com/openfga/go-sdk"
	"github.com/openfga/language/pkg/go/transformer"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/repo"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/service"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/policies"
)

func TestDatasetHandlerWithRealOpenFGA(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	container, apiURL := startOpenFGA(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()
	storeID, modelID := uploadModel(t, ctx, apiURL)

	var audit bytes.Buffer
	a, err := authz.New(ctx, authz.Options{Config: authz.Config{
		OpenFGAApiURL:   apiURL,
		OpenFGAStoreID:  storeID,
		OpenFGAModelID:  modelID,
		OpenFGATimeout:  2 * time.Second,
		DecisionTimeout: 3 * time.Second,
		AuditSink:       authz.DefaultAuditSink,
		SourceCommit:    "integration-handler",
	}, AuditWriter: &audit})
	if err != nil {
		t.Fatalf("new authorizer: %v", err)
	}
	defer func() { _ = a.Close() }()

	tenantID, userID := uuid.New(), uuid.New()
	ds := service.New(repo.NewMemoryStore())
	created, err := ds.CreateDataset(ctx, tenantID.String(), service.CreateDatasetInput{Name: "sales", Format: "iceberg"})
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}

	h := NewHandler(ds, GatewayTenantResolver{}, WithDecisionGuard(authz.NewGuard(a)))
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			tc := authn.TenantContext{TenantID: tenantID, UserID: userID, Roles: []string{"viewer"}, RequestID: "handler-integration"}
			next.ServeHTTP(w, req.WithContext(authn.WithTenantContext(req.Context(), tc)))
		})
	})
	r.Use(authz.NewGuard(a).Middleware)
	h.RegisterRoutes(r)

	// 1 + 12: real datasets handler allow, decision id header, audit correlation.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+created.Dataset.ID, nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", tenantID.String())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	decisionID := rec.Header().Get(authz.DecisionHeader)
	if decisionID == "" {
		t.Fatal("missing decision id header")
	}
	if !auditContainsDecisionID(audit.Bytes(), decisionID) {
		t.Fatalf("audit does not contain decision id %q", decisionID)
	}

	// 1: viewer write is denied by the real Cedar role matrix.
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/datasets/"+created.Dataset.ID, bytes.NewBufferString(`{"display_name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ontoaxis-Tenant-Id", tenantID.String())
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer PATCH status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 2: cross-tenant lookup is 404 before authz and leaks no body.
	otherTenant := uuid.New()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/datasets/"+created.Dataset.ID, nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", otherTenant.String())
	rec = httptest.NewRecorder()
	// The middleware injects tenantID; use a second router with the other tenant context.
	r2 := chi.NewRouter()
	r2.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			tc := authn.TenantContext{TenantID: otherTenant, UserID: userID, Roles: []string{"member"}}
			next.ServeHTTP(w, req.WithContext(authn.WithTenantContext(req.Context(), tc)))
		})
	})
	r2.Use(authz.NewGuard(a).Middleware)
	h.RegisterRoutes(r2)
	r2.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("sales")) {
		t.Fatal("cross-tenant response leaked dataset content")
	}

	// Remaining rows use the same real OpenFGA + real cedar-go authorizer.
	tenantCtx := func(roles ...string) context.Context {
		return authn.WithTenantContext(ctx, authn.TenantContext{TenantID: tenantID, UserID: userID, Roles: roles, RequestID: "matrix"})
	}
	member, viewer, wsAdmin, tenantAdmin := tenantCtx("member"), tenantCtx("viewer"), tenantCtx("workspace-admin"), tenantCtx("tenant-admin")
	base := tenantID.String()
	res := func(typ, status string) authz.ResourceFact {
		return authz.ResourceFact{Type: typ, ID: uuid.NewString(), TenantID: base, Status: status}
	}
	mustAllow := func(name string, c context.Context, action authz.Action, r authz.ResourceFact) {
		t.Helper()
		if _, err := a.Authorize(c, authz.Request{Action: action, Resource: r}); err != nil {
			t.Fatalf("%s: expected allow, got %v", name, err)
		}
	}
	mustDeny := func(name string, c context.Context, action authz.Action, r authz.ResourceFact) {
		t.Helper()
		if _, err := a.Authorize(c, authz.Request{Action: action, Resource: r}); err == nil {
			t.Fatalf("%s: expected deny", name)
		}
	}

	// 3: SavedQuery owner relation.
	sqOwner := authz.ResourceFact{Type: "saved_query", ID: uuid.NewString(), TenantID: base, OwnerID: userID.String()}
	mustAllow("saved query owner", member, "saved_query:delete", sqOwner)
	mustDeny("saved query viewer owner", viewer, "saved_query:delete", sqOwner)
	mustDeny("saved query non-owner", member, "saved_query:delete", authz.ResourceFact{Type: "saved_query", ID: uuid.NewString(), TenantID: base, OwnerID: uuid.NewString()})

	// 4: Object -> ObjectType -> Tenant chain.
	otID := uuid.NewString()
	obj := authz.ResourceFact{Type: "object", ID: uuid.NewString(), TenantID: base, Ancestors: []authz.Ancestor{{Type: "object_type", ID: otID}}}
	mustAllow("object member read", member, "object:read", obj)
	mustDeny("object viewer write", viewer, "object:update", obj)

	// 5: Pipeline state.
	mustAllow("published pipeline invoke", member, "pipeline:invoke", res("pipeline", "published"))
	mustDeny("archived pipeline invoke", member, "pipeline:invoke", res("pipeline", "archived"))
	mustDeny("draft pipeline invoke", member, "pipeline:invoke", res("pipeline", "draft"))

	// 6: ActionType state.
	mustAllow("active action run", member, "action_type:run", res("action_type", "active"))
	mustDeny("disabled action run", member, "action_type:run", res("action_type", "disabled"))

	// 7: Connector state.
	mustAllow("active connector sync", member, "connector:sync", res("connector", "active"))
	mustDeny("paused connector sync", member, "connector:sync", res("connector", "paused"))

	// 8: Execution state.
	mustDeny("queued execution retry", member, "action_execution:retry", res("action_execution", "queued"))
	mustDeny("succeeded execution cancel", member, "action_execution:cancel", res("action_execution", "succeeded"))

	// 9: Admin scope.
	org := authz.ResourceFact{Type: "organization", ID: uuid.NewString(), TenantID: base}
	mustDeny("workspace-admin org update", wsAdmin, "tenancy:organization:update", org)
	mustAllow("tenant-admin org update", tenantAdmin, "tenancy:organization:update", org)
	mustAllow("tenant-admin audit read", tenantAdmin, "audit:read", authz.ResourceFact{Type: "tenant", ID: base, TenantID: base})
	mustDeny("workspace-admin audit read", wsAdmin, "audit:read", authz.ResourceFact{Type: "tenant", ID: base, TenantID: base})

	// 10: unknown action.
	mustDeny("unknown action", member, "dataset:unknown", authz.ResourceFact{Type: "dataset", ID: uuid.NewString(), TenantID: base})
}

func auditContainsDecisionID(raw []byte, id string) bool {
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var envelope struct {
			Authz struct {
				EventType  string `json:"event_type"`
				DecisionID string `json:"decision_id"`
			} `json:"authz"`
		}
		if json.Unmarshal(line, &envelope) == nil && envelope.Authz.EventType == "authz.decision" && envelope.Authz.DecisionID == id {
			return true
		}
	}
	return false
}

func startOpenFGA(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "openfga/openfga:v1.23.0",
		ExposedPorts: []string{"8080/tcp"},
		Cmd:          []string{"run"},
		Env:          map[string]string{"OPENFGA_DATASTORE_ENGINE": "memory", "OPENFGA_LOG_FORMAT": "json"},
		WaitingFor:   wait.ForLog("starting HTTP server").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		t.Fatalf("start OpenFGA: %v", err)
	}
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "8080/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return container, "http://" + host + ":" + port.Port()
}

func uploadModel(t *testing.T, ctx context.Context, apiURL string) (string, string) {
	t.Helper()
	modelDSL, err := policies.FS.ReadFile("openfga/model.fga")
	if err != nil {
		t.Fatal(err)
	}
	protoModel, err := transformer.TransformDSLToProto(string(modelDSL))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := protojson.Marshal(protoModel)
	if err != nil {
		t.Fatal(err)
	}
	var writeRequest fgaSdk.WriteAuthorizationModelRequest
	if err := json.Unmarshal(raw, &writeRequest); err != nil {
		t.Fatal(err)
	}
	cfg, err := fgaSdk.NewConfiguration(fgaSdk.Configuration{ApiUrl: apiURL})
	if err != nil {
		t.Fatal(err)
	}
	api := fgaSdk.NewAPIClient(cfg)
	store, _, err := api.OpenFgaApi.CreateStore(ctx).Body(fgaSdk.CreateStoreRequest{Name: "ontoaxis-handler-integration"}).Execute()
	if err != nil {
		t.Fatal(err)
	}
	model, _, err := api.OpenFgaApi.WriteAuthorizationModel(ctx, store.GetId()).Body(writeRequest).Execute()
	if err != nil {
		t.Fatal(err)
	}
	return store.GetId(), model.GetAuthorizationModelId()
}
