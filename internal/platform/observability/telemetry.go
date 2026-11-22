package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// TelemetryConfig describes the process-wide tracing setup. An empty OTLP
// endpoint keeps tracing disabled (no-op); it does not disable propagation or
// request-scoped spans.
type TelemetryConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string
	SampleRatio    float64
}

// Telemetry owns the OTel tracer provider when tracing is enabled.
type Telemetry struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	logger   *slog.Logger
	enabled  bool
}

// SetupTelemetry configures W3C propagation and, when an endpoint is supplied,
// an OTLP/HTTP trace pipeline. It is safe to call in tests with an empty
// endpoint; the returned Telemetry then uses the global no-op tracer.
func SetupTelemetry(ctx context.Context, cfg TelemetryConfig, logger *slog.Logger) (*Telemetry, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "ontoaxis-core"
	}
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = "dev"
	}
	if cfg.Environment == "" {
		cfg.Environment = "local"
	}
	if cfg.SampleRatio < 0 || cfg.SampleRatio > 1 {
		return nil, fmt.Errorf("telemetry sample ratio must be between 0 and 1")
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	endpoint := strings.TrimSpace(cfg.OTLPEndpoint)
	if endpoint == "" {
		return &Telemetry{tracer: otel.Tracer("ontoaxis"), logger: logger}, nil
	}
	endpoint, err := otlpTraceURL(endpoint)
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String("deployment.environment.name", cfg.Environment),
		attribute.String("service.instance.id", uuid.NewString()),
	))
	if err != nil {
		_ = exporter.Shutdown(ctx)
		return nil, fmt.Errorf("build telemetry resource: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	logger.Info("telemetry tracing enabled", "endpoint", endpoint, "environment", cfg.Environment, "sample_ratio", cfg.SampleRatio)
	return &Telemetry{provider: tp, tracer: tp.Tracer("ontoaxis"), logger: logger, enabled: true}, nil
}

// Tracer returns the process tracer.
func (t *Telemetry) Tracer() trace.Tracer {
	if t == nil || t.tracer == nil {
		return otel.Tracer("ontoaxis")
	}
	return t.tracer
}

// Enabled reports whether an OTLP exporter is configured.
func (t *Telemetry) Enabled() bool { return t != nil && t.enabled }

// Shutdown flushes and stops the tracer provider. It is a no-op when tracing
// is disabled.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil || t.provider == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return t.provider.Shutdown(shutdownCtx)
}

// otlpTraceURL accepts both base URLs (http://collector:4318) and full trace
// URLs. The OTLP/HTTP exporter requires an explicit /v1/traces path.
func otlpTraceURL(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse OTLP endpoint: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("OTLP endpoint must include scheme and host: %q", endpoint)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/v1/traces"
	}
	return u.String(), nil
}
