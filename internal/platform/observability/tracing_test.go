package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHTTPMiddlewareUsesRouteTemplateAndNeverRawID(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = provider.Shutdown(context.Background()) }()

	r := chi.NewRouter()
	r.Use(HTTPMiddleware(provider.Tracer("test")))
	r.Get("/api/v1/objects/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/objects/01a20200-1111-7000-8000-000000000000?tenant=secret", nil))

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Name != "HTTP GET /api/v1/objects/{id}" {
		t.Fatalf("span name = %q", span.Name)
	}
	attrs := map[string]string{}
	for _, a := range span.Attributes {
		attrs[string(a.Key)] = a.Value.AsString()
	}
	if attrs["http.route"] != "/api/v1/objects/{id}" {
		t.Fatalf("http.route = %q", attrs["http.route"])
	}
	if attrs["url.path"] != "/api/v1/objects/{id}" {
		t.Fatalf("url.path = %q", attrs["url.path"])
	}
	if containsRawID(span.Name) || containsRawID(attrs["url.path"]) {
		t.Fatalf("raw object id leaked: %#v", attrs)
	}
	if span.Status.Code.String() != "Unset" {
		t.Fatalf("status = %s", span.Status.Code)
	}
}

func containsRawID(s string) bool {
	for i := 0; i+8 <= len(s); i++ {
		if s[i:i+8] == "01a20200" {
			return true
		}
	}
	return false
}

func TestJobSpanExportsType(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = provider.Shutdown(context.Background()) }()

	_, span := StartJobSpan(context.Background(), provider.Tracer("test"), "index")
	span.End()
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "job index" {
		t.Fatalf("unexpected job spans: %#v", spans)
	}
}

func TestHTTPMiddlewareCapturesErrorStatuses(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = provider.Shutdown(context.Background()) }()

	r := chi.NewRouter()
	r.Use(HTTPMiddleware(provider.Tracer("test")))
	r.Get("/api/v1/private", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	r.Get("/api/v1/denied", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/private", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/denied", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))

	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("spans = %d, want 3", len(spans))
	}
	wantStatuses := map[string]int{
		"HTTP GET /api/v1/private": 401,
		"HTTP GET /api/v1/denied":  403,
		"HTTP GET unmatched":       404,
	}
	for _, span := range spans {
		want, ok := wantStatuses[span.Name]
		if !ok {
			t.Fatalf("unexpected span %q", span.Name)
		}
		var got int64
		for _, attr := range span.Attributes {
			if string(attr.Key) == "http.response.status_code" {
				got = attr.Value.AsInt64()
			}
		}
		if int(got) != want {
			t.Fatalf("%s status = %d, want %d", span.Name, got, want)
		}
	}
}

func TestHTTPMiddlewareContinuesInboundTrace(t *testing.T) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = provider.Shutdown(context.Background()) }()

	r := chi.NewRouter()
	r.Use(HTTPMiddleware(provider.Tracer("test")))
	r.Get("/ok", func(w http.ResponseWriter, _ *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	r.ServeHTTP(httptest.NewRecorder(), req)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if got := spans[0].SpanContext.TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %s", got)
	}
}
