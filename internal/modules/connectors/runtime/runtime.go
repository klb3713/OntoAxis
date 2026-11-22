package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
)

const (
	// defaultVerifyMaxRows bounds the preflight row count used by the dispatcher.
	defaultVerifyMaxRows = 1_000_000
	// runtimeTracerName scopes runtime orchestration spans.
	runtimeTracerName = "ontoaxis/connectors/runtime"
)

// Config wires the connector sync runtime.
type Config struct {
	// ConnectURL is the Kafka Connect REST base URL.
	ConnectURL string
	// ConnectTimeout bounds one Kafka Connect REST request.
	ConnectTimeout time.Duration
	// ConnectRetryMax is the number of retries after the first attempt.
	ConnectRetryMax int
	// VerifyMaxRows bounds count-based verification in the dispatcher.
	VerifyMaxRows int
	// Warehouse locates the Iceberg catalog and Dataset storage.
	Warehouse Warehouse
}

// RuntimeStatus is the normalized source and sink connector state.
type RuntimeStatus struct {
	Source State
	Sink   State
}

// Runtime orchestrates Debezium source and Iceberg sink connectors against Kafka
// Connect. It never persists credentials and never embeds them in errors.
type Runtime struct {
	naming         Naming
	resolver       SecretResolver
	client         Client
	warehouse      Warehouse
	metrics        *Metrics
	tracer         trace.Tracer
	verifyMaxRows  int
	connectTimeout time.Duration
	retryPolicy    RetryPolicy
}

// Option customizes the runtime.
type Option func(*Runtime)

// WithConnectClient injects the Kafka Connect client (tests, alternate adapters).
func WithConnectClient(client Client) Option {
	return func(r *Runtime) {
		if client != nil {
			r.client = client
		}
	}
}

// WithSecretResolver injects the secret resolver used for submit requests.
func WithSecretResolver(resolver SecretResolver) Option {
	return func(r *Runtime) {
		if resolver != nil {
			r.resolver = resolver
		}
	}
}

// WithNaming injects the deterministic naming strategy.
func WithNaming(naming Naming) Option {
	return func(r *Runtime) { r.naming = naming }
}

// WithWarehouse overrides the Iceberg warehouse wiring.
func WithWarehouse(warehouse Warehouse) Option {
	return func(r *Runtime) { r.warehouse = warehouse }
}

// WithMetrics attaches bounded-label runtime metrics.
func WithMetrics(metrics *Metrics) Option {
	return func(r *Runtime) {
		if metrics != nil {
			r.metrics = metrics
		}
	}
}

// WithTracer overrides the runtime trace provider.
func WithTracer(tracer trace.Tracer) Option {
	return func(r *Runtime) {
		if tracer != nil {
			r.tracer = tracer
		}
	}
}

// WithConnectTimeout overrides the Kafka Connect request timeout for the client
// built by NewRuntime.
func WithConnectTimeout(timeout time.Duration) Option {
	return func(r *Runtime) {
		if timeout > 0 {
			r.connectTimeout = timeout
		}
	}
}

// WithRetryPolicy overrides the Kafka Connect retry policy for the client built
// by NewRuntime.
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(r *Runtime) { r.retryPolicy = policy }
}

// NewRuntime wires the runtime. Missing Kafka Connect or warehouse wiring is a
// configuration error so the module can keep running without the runtime.
func NewRuntime(cfg Config, opts ...Option) (*Runtime, error) {
	verifyMaxRows := cfg.VerifyMaxRows
	if verifyMaxRows <= 0 {
		verifyMaxRows = defaultVerifyMaxRows
	}
	timeout := cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	policy := DefaultRetryPolicy()
	if cfg.ConnectRetryMax > 0 {
		policy.MaxRetries = cfg.ConnectRetryMax
	}
	runtime := &Runtime{
		naming:         NewNaming(),
		resolver:       NewEnvSecretResolver(),
		warehouse:      cfg.Warehouse,
		metrics:        NewMetrics(nil),
		tracer:         otel.Tracer(runtimeTracerName),
		verifyMaxRows:  verifyMaxRows,
		connectTimeout: timeout,
		retryPolicy:    policy,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(runtime)
		}
	}
	if runtime.client == nil {
		if strings.TrimSpace(cfg.ConnectURL) == "" {
			return nil, fmt.Errorf("%w: kafka connect url is not configured", ErrRuntimeNotConfigured)
		}
		runtime.client = NewHTTPClient(cfg.ConnectURL,
			WithHTTPTimeout(runtime.connectTimeout),
			WithHTTPRetryPolicy(runtime.retryPolicy),
			WithHTTPMetrics(runtime.metrics),
			WithHTTPTracer(runtime.tracer),
		)
	}
	if err := runtime.warehouse.Validate(); err != nil {
		return nil, err
	}
	return runtime, nil
}

// NewFromEnv builds the runtime from ONTOAXIS_CONNECT_* and ONTOAXIS_ICEBERG_*
// environment variables. It returns (nil, nil) when Kafka Connect is unconfigured,
// so the module can start with sync marked unavailable.
func NewFromEnv(opts ...Option) (*Runtime, error) {
	cfg, configured, err := LoadConfigFromEnv()
	if err != nil {
		return nil, err
	}
	if !configured {
		return nil, nil
	}
	return NewRuntime(cfg, opts...)
}

// VerifyMaxRows is the count-verification bound for the sync dispatcher.
func (r *Runtime) VerifyMaxRows() int {
	if r == nil {
		return defaultVerifyMaxRows
	}
	return r.verifyMaxRows
}

// preparedSync is the validated runtime definition for one submit attempt.
type preparedSync struct {
	names   Names
	config  SyncConfig
	source  map[string]any
	sink    map[string]any
	secrets map[string]string
}

// Preflight validates the connector, builds both runtime definitions in memory,
// and verifies Kafka Connect reachability. It never submits anything.
func (r *Runtime) Preflight(ctx context.Context, tenantID string, connector *domain.Connector) error {
	started := time.Now()
	operation := "preflight"
	ctx, span := r.startSpan(ctx, operation)
	defer span.End()
	prepared, err := r.prepare(ctx, tenantID, connector)
	if err == nil {
		err = r.checkReachable(ctx, prepared.names)
	}
	span.SetAttributes(attribute.String("result", RequestResult(err)))
	if err != nil {
		span.SetStatus(codes.Error, "preflight failed")
		r.observeSubmit(operation, ResultError, started)
		return err
	}
	r.observeSubmit(operation, ResultOK, started)
	return nil
}

// Submit preflights the connector and upserts the source then the sink connector.
// The returned runtime reference is the deterministic sink connector name; a sink
// failure deletes the just-created source on a best-effort basis.
func (r *Runtime) Submit(ctx context.Context, tenantID string, connector *domain.Connector) (string, error) {
	started := time.Now()
	operation := "submit"
	ctx, span := r.startSpan(ctx, operation)
	defer span.End()
	prepared, err := r.prepare(ctx, tenantID, connector)
	if err == nil {
		err = r.submitPrepared(ctx, prepared)
	}
	span.SetAttributes(attribute.String("result", RequestResult(err)))
	if err != nil {
		span.SetStatus(codes.Error, "submit failed")
		r.observeSubmit(operation, ResultError, started)
		return "", err
	}
	r.observeSubmit(operation, ResultOK, started)
	return prepared.names.Sink, nil
}

// Stop stops and deletes the source and sink connectors for a runtime reference.
// Missing connectors are treated as already stopped, and all steps are attempted
// even if one fails.
func (r *Runtime) Stop(ctx context.Context, tenantID, runtimeRef string) error {
	names, err := r.naming.NamesFromRuntimeRef(tenantID, runtimeRef)
	if err != nil {
		return err
	}
	var failures []error
	stop := func(name string) {
		if err := r.client.StopConnector(ctx, name); err != nil {
			failures = append(failures, fmt.Errorf("stop runtime connector: %w", err))
		}
	}
	remove := func(name string) {
		if err := r.client.DeleteConnector(ctx, name); err != nil {
			failures = append(failures, fmt.Errorf("delete runtime connector: %w", err))
		}
	}
	stop(names.Source)
	stop(names.Sink)
	remove(names.Source)
	remove(names.Sink)
	return errors.Join(failures...)
}

// Status reads the normalized source and sink state for a runtime reference.
func (r *Runtime) Status(ctx context.Context, tenantID, runtimeRef string) (RuntimeStatus, error) {
	names, err := r.naming.NamesFromRuntimeRef(tenantID, runtimeRef)
	if err != nil {
		return RuntimeStatus{}, err
	}
	source, err := r.client.ConnectorStatus(ctx, names.Source)
	if err != nil {
		return RuntimeStatus{}, fmt.Errorf("read source runtime status: %w", err)
	}
	sink, err := r.client.ConnectorStatus(ctx, names.Sink)
	if err != nil {
		return RuntimeStatus{}, fmt.Errorf("read sink runtime status: %w", err)
	}
	return RuntimeStatus{Source: source, Sink: sink}, nil
}

// prepare validates the connector, resolves the secret once, and builds both
// runtime definitions. Validation errors are classified for the service layer.
func (r *Runtime) prepare(ctx context.Context, tenantID string, connector *domain.Connector) (*preparedSync, error) {
	tenant, err := uuid.Parse(strings.TrimSpace(tenantID))
	if err != nil {
		return nil, fmt.Errorf("%w: tenant id must be a uuid", ErrInvalidTenant)
	}
	if connector == nil {
		return nil, fmt.Errorf("%w: connector is required", ErrInvalidConnector)
	}
	connectorTenant, err := uuid.Parse(strings.TrimSpace(connector.TenantID))
	if err != nil || connectorTenant != tenant {
		return nil, fmt.Errorf("%w: connector does not belong to the tenant", domain.ErrNotFound)
	}
	if connector.Type != domain.TypePostgres {
		return nil, fmt.Errorf("%w: connector type %q has no CDC runtime mapping", ErrUnsupportedType, connector.Type)
	}
	if connector.Status != domain.StatusActive {
		return nil, fmt.Errorf("%w: connector must be active before starting sync", domain.ErrConflict)
	}
	syncConfig, err := ParseSyncConfig(connector.Config)
	if err != nil {
		return nil, err
	}
	if err := r.warehouse.Validate(); err != nil {
		return nil, err
	}
	names, err := r.naming.NamesFor(tenant.String(), connector.ID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(connector.SecretRef) == "" {
		return nil, fmt.Errorf("%w: connector has no secret reference", ErrSecretUnavailable)
	}
	secrets, err := r.resolver.Resolve(ctx, tenant.String(), connector.SecretRef)
	if err != nil {
		return nil, err
	}
	source, err := BuildSourceConfig(syncConfig, names, secrets)
	if err != nil {
		return nil, err
	}
	sink, err := BuildSinkConfig(syncConfig, names, r.warehouse, secrets)
	if err != nil {
		return nil, err
	}
	return &preparedSync{names: names, config: syncConfig, source: source, sink: sink, secrets: secrets}, nil
}

func (r *Runtime) submitPrepared(ctx context.Context, prepared *preparedSync) error {
	if err := r.client.UpsertConnector(ctx, prepared.names.Source, prepared.source); err != nil {
		return fmt.Errorf("submit source connector: %w", err)
	}
	if err := r.client.UpsertConnector(ctx, prepared.names.Sink, prepared.sink); err != nil {
		// Best-effort rollback so a half-submitted pair does not keep replicating.
		_ = r.client.DeleteConnector(ctx, prepared.names.Source)
		return fmt.Errorf("submit sink connector: %w", err)
	}
	return nil
}

// checkReachable prefers the optional health check and falls back to a status
// probe, which also proves the namespace is addressable.
func (r *Runtime) checkReachable(ctx context.Context, names Names) error {
	if checker, ok := r.client.(HealthChecker); ok {
		if err := checker.Ping(ctx); err != nil {
			return fmt.Errorf("kafka connect health check failed: %w", err)
		}
		return nil
	}
	if _, err := r.client.ConnectorStatus(ctx, names.Source); err != nil {
		return fmt.Errorf("kafka connect status probe failed: %w", err)
	}
	return nil
}

func (r *Runtime) startSpan(ctx context.Context, operation string) (context.Context, trace.Span) {
	tracer := r.tracer
	if tracer == nil {
		tracer = otel.Tracer(runtimeTracerName)
	}
	return tracer.Start(ctx, "connectors.runtime."+operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("dependency", DependencyKafkaConnect),
			attribute.String("operation", operation),
		),
	)
}

func (r *Runtime) observeSubmit(operation, result string, started time.Time) {
	if r.metrics == nil {
		return
	}
	r.metrics.ObserveSubmit(operation, result, time.Since(started))
}
