package authz

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestAuthorizeEmitsBoundedTrace(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "viewer")
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	a.tracer = provider.Tracer("test")
	decision, err := a.Authorize(ctx, Request{
		Action:   "dataset:read",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()},
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	var check sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == "authz.check" {
			check = span
		}
	}
	if check == nil {
		t.Fatalf("authz.check span not recorded")
	}
	attrs := map[attribute.Key]string{}
	for _, attr := range check.Attributes() {
		attrs[attr.Key] = attr.Value.AsString()
	}
	if attrs["authz.action"] != "dataset:read" || attrs["authz.decision"] != "allow" || attrs["authz.decision_id"] != decision.DecisionID {
		t.Fatalf("unexpected span attributes: %v", attrs)
	}
	for _, forbidden := range []attribute.Key{"tenant_id", "user_id", "resource_id", "authz.resource"} {
		if _, ok := attrs[forbidden]; ok {
			t.Fatalf("high-cardinality attribute %q present", forbidden)
		}
	}
}

func TestReadyFailsClosedOnDependencyError(t *testing.T) {
	a := newTestAuthorizer(t, &fakeFGA{startup: errors.New("unavailable")}, nil)
	if err := a.Ready(context.Background()); err == nil {
		t.Fatalf("expected ready failure")
	}
	b := newTestAuthorizer(t, &fakeFGA{}, nil)
	if err := b.Ready(context.Background()); err != nil {
		t.Fatalf("expected ready success, got %v", err)
	}
}

func TestResolveSourceCommitRequiresValue(t *testing.T) {
	if _, err := resolveSourceCommit(""); err == nil {
		t.Fatalf("expected missing source commit error")
	}
	got, err := resolveSourceCommit("abc123")
	if err != nil || got != "abc123" {
		t.Fatalf("resolve explicit source commit = %q, %v", got, err)
	}
}
