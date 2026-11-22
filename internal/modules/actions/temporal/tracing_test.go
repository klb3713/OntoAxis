package temporal

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestActionSpansAreBounded(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	_, activity := actionActivitySpan(context.Background(), "ApplyStep")
	activity.End()
	_, workflow := workflowSpan(context.Background())
	workflow.End()

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("ended spans = %d, want 2", len(spans))
	}
	names := map[string]bool{}
	for _, span := range spans {
		names[span.Name()] = true
		if len(span.Attributes()) != 0 {
			t.Fatalf("span %q attributes = %v, want none", span.Name(), span.Attributes())
		}
	}
	for _, want := range []string{"action.activity.ApplyStep", "action.workflow.execute"} {
		if !names[want] {
			t.Fatalf("missing span %q in %v", want, names)
		}
	}
}
