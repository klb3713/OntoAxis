package observability

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// HTTPMiddleware starts a server span around every request. The span name and
// route attribute are finalized after chi has selected the route template, so
// raw object IDs and query strings never become metric/trace labels.
func HTTPMiddleware(tracer trace.Tracer) func(http.Handler) http.Handler {
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("ontoaxis")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parent := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer.Start(parent, "HTTP "+r.Method+" "+r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.request.method", r.Method),
					attribute.String("url.path", r.URL.Path),
					attribute.String("http.route", "pending"),
				),
			)
			defer span.End()

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r.WithContext(ctx))

			route := "unmatched"
			if rc := chi.RouteContext(ctx); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			span.SetName(fmt.Sprintf("HTTP %s %s", r.Method, route))
			span.SetAttributes(
				attribute.String("http.route", route),
				attribute.String("url.path", route),
				attribute.Int("http.response.status_code", sw.status),
			)
			if sw.status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(sw.status))
			}
		})
	}
}

// StartJobSpan starts a background job span. Callers must end the returned span.
func StartJobSpan(ctx context.Context, tracer trace.Tracer, jobType string) (context.Context, trace.Span) {
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("ontoaxis")
	}
	return tracer.Start(ctx, "job "+jobType,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attribute.String("job.type", jobType)),
	)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush preserves streaming support for SSE/WebSocket-adjacent handlers.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
