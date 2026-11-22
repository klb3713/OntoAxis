package api

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestAPISpanIsBounded(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	_, span := actionAPISpan(context.Background(), "run")
	span.End()
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if spans[0].Name() != "action.api.run" {
		t.Fatalf("span name = %q", spans[0].Name())
	}
	if len(spans[0].Attributes()) != 0 {
		t.Fatalf("span attributes = %v, want none", spans[0].Attributes())
	}
}
