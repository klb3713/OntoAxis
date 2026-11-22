package temporal

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// actionTracer names the worker-side spans. Attributes are limited to activity
// and step status by callers; tenant/user/execution IDs are never exported.
var actionTracer = otel.Tracer("ontoaxis/actions/worker")

func actionActivitySpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return actionTracer.Start(ctx, "action.activity."+name)
}

// workflowSpan is opened at the first activity boundary of an execution. The
// Temporal workflow itself stays deterministic and does not create spans.
func workflowSpan(ctx context.Context) (context.Context, trace.Span) {
	return actionTracer.Start(ctx, "action.workflow.execute")
}
