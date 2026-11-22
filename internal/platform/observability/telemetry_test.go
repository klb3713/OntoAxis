package observability

import (
	"context"
	"testing"
)

func TestSetupTelemetryDisabledWithoutEndpoint(t *testing.T) {
	tel, err := SetupTelemetry(context.Background(), TelemetryConfig{ServiceName: "ontoaxis-core"}, nil)
	if err != nil {
		t.Fatalf("SetupTelemetry: %v", err)
	}
	if tel.Enabled() {
		t.Fatal("tracing should be disabled without endpoint")
	}
	if tel.Tracer() == nil {
		t.Fatal("no-op tracer must be available")
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestSetupTelemetryRejectsInvalidSampleRatio(t *testing.T) {
	_, err := SetupTelemetry(context.Background(), TelemetryConfig{SampleRatio: 2}, nil)
	if err == nil {
		t.Fatal("expected invalid sample ratio error")
	}
}

func TestOTLPTraceURLNormalizesBaseEndpoint(t *testing.T) {
	got, err := otlpTraceURL("http://collector:4318")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://collector:4318/v1/traces" {
		t.Fatalf("unexpected trace URL %q", got)
	}
	got, err = otlpTraceURL("https://collector:4318/custom/traces")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://collector:4318/custom/traces" {
		t.Fatalf("unexpected custom trace URL %q", got)
	}
	if _, err := otlpTraceURL("collector:4318"); err == nil {
		t.Fatal("expected missing scheme error")
	}
}
