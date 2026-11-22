package api

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// actionAPITracer emits request-level spans only. Attributes are deliberately
// absent: tenant, user, execution and object identifiers are never exported.
var actionAPITracer = otel.Tracer("ontoaxis/actions/api")

func actionAPISpan(ctx context.Context, operation string) (context.Context, trace.Span) {
	return actionAPITracer.Start(ctx, "action.api."+operation)
}
