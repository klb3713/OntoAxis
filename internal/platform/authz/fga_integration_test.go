//go:build integration

package authz

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	fgaSdk "github.com/openfga/go-sdk"
	"github.com/openfga/language/pkg/go/transformer"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/policies"
)

func TestAuthorizerWithRealOpenFGA(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	container, apiURL := startOpenFGA(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	storeID, modelID := uploadModel(t, ctx, apiURL)
	var audit bytes.Buffer
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	a, err := New(ctx, Options{Config: Config{
		OpenFGAApiURL:   apiURL,
		OpenFGAStoreID:  storeID,
		OpenFGAModelID:  modelID,
		OpenFGATimeout:  2 * time.Second,
		DecisionTimeout: 3 * time.Second,
		AuditSink:       DefaultAuditSink,
		SourceCommit:    "integration-test",
	}, AuditWriter: &audit, Tracer: provider.Tracer("integration")})
	if err != nil {
		t.Fatalf("new authorizer: %v", err)
	}
	defer func() { _ = a.Close() }()

	tenantID, userID := uuid.New(), uuid.New()
	requestCtx := authn.WithTenantContext(ctx, authn.TenantContext{TenantID: tenantID, UserID: userID, Roles: []string{"viewer"}, RequestID: "integration"})
	datasetID := uuid.NewString()
	resource := ResourceFact{Type: "dataset", ID: datasetID, TenantID: tenantID.String()}

	decision, err := a.Authorize(requestCtx, Request{Action: "dataset:read", Resource: resource})
	if err != nil {
		t.Fatalf("viewer read should be allowed: %v", err)
	}
	if !decision.Allow || decision.Policy.FGADecision != "allow" || decision.Policy.CedarDecision != "allow" {
		t.Fatalf("unexpected allow decision: %+v", decision)
	}
	// decision id must agree across the decision, the decision audit event, and
	// the authz.check span.
	var auditDecisionID string
	for _, line := range bytes.Split(bytes.TrimSpace(audit.Bytes()), []byte("\n")) {
		var envelope struct {
			Authz struct {
				EventType  string `json:"event_type"`
				DecisionID string `json:"decision_id"`
			} `json:"authz"`
		}
		if json.Unmarshal(line, &envelope) == nil && envelope.Authz.EventType == "authz.decision" {
			auditDecisionID = envelope.Authz.DecisionID
		}
	}
	if auditDecisionID != decision.DecisionID {
		t.Fatalf("audit decision id %q != decision id %q", auditDecisionID, decision.DecisionID)
	}
	var spanDecisionID string
	for _, span := range recorder.Ended() {
		if span.Name() != "authz.check" {
			continue
		}
		for _, attr := range span.Attributes() {
			if string(attr.Key) == "authz.decision_id" {
				spanDecisionID = attr.Value.AsString()
			}
		}
	}
	if spanDecisionID != decision.DecisionID {
		t.Fatalf("span decision id %q != decision id %q", spanDecisionID, decision.DecisionID)
	}

	if _, err := a.Authorize(requestCtx, Request{Action: "dataset:update", Resource: resource}); err == nil {
		t.Fatal("viewer update must be denied by the Cedar role matrix")
	}

	otherTenant := uuid.NewString()
	if _, err := a.Authorize(requestCtx, Request{Action: "dataset:read", Resource: ResourceFact{Type: "dataset", ID: datasetID, TenantID: otherTenant}}); err == nil {
		t.Fatal("cross-tenant request must be denied before OpenFGA")
	}
}

func startOpenFGA(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "openfga/openfga:v1.23.0",
		ExposedPorts: []string{"8080/tcp"},
		Cmd:          []string{"run"},
		Env: map[string]string{
			"OPENFGA_DATASTORE_ENGINE": "memory",
			"OPENFGA_LOG_FORMAT":       "json",
		},
		WaitingFor: wait.ForLog("starting HTTP server").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		t.Fatalf("start OpenFGA: %v", err)
	}
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("OpenFGA host: %v", err)
	}
	port, err := container.MappedPort(ctx, "8080/tcp")
	if err != nil {
		t.Fatalf("OpenFGA port: %v", err)
	}
	return container, "http://" + host + ":" + port.Port()
}

func uploadModel(t *testing.T, ctx context.Context, apiURL string) (string, string) {
	t.Helper()
	modelDSL, err := policies.FS.ReadFile(ModelPath)
	if err != nil {
		t.Fatalf("read model: %v", err)
	}
	protoModel, err := transformer.TransformDSLToProto(string(modelDSL))
	if err != nil {
		t.Fatalf("compile model: %v", err)
	}
	raw, err := protojson.Marshal(protoModel)
	if err != nil {
		t.Fatalf("marshal model: %v", err)
	}
	var writeRequest fgaSdk.WriteAuthorizationModelRequest
	if err := json.Unmarshal(raw, &writeRequest); err != nil {
		t.Fatalf("decode model request: %v", err)
	}

	cfg, err := fgaSdk.NewConfiguration(fgaSdk.Configuration{ApiUrl: apiURL})
	if err != nil {
		t.Fatalf("configure OpenFGA API client: %v", err)
	}
	api := fgaSdk.NewAPIClient(cfg)
	store, _, err := api.OpenFgaApi.CreateStore(ctx).Body(fgaSdk.CreateStoreRequest{Name: "ontoaxis-integration"}).Execute()
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	model, _, err := api.OpenFgaApi.WriteAuthorizationModel(ctx, store.GetId()).Body(writeRequest).Execute()
	if err != nil {
		t.Fatalf("write model: %v", err)
	}
	return store.GetId(), model.GetAuthorizationModelId()
}
