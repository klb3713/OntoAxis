package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	// defaultHTTPTimeout bounds one Kafka Connect REST request.
	defaultHTTPTimeout = 10 * time.Second
	// maxResponseBody caps how much of a Kafka Connect response is read.
	maxResponseBody = 1 << 20
	// connectorNamePattern matches Kafka Connect connector names.
	connectorNamePattern = `^[a-zA-Z0-9._-]{1,250}$`
)

var connectorNameRegexp = regexp.MustCompile(connectorNamePattern)

// State is the normalized Kafka Connect connector state.
type State string

const (
	// StateRunning means the connector and its tasks run normally.
	StateRunning State = "running"
	// StateFailed means the connector or one of its tasks failed.
	StateFailed State = "failed"
	// StatePaused means the connector is paused.
	StatePaused State = "paused"
	// StateStopped means the connector is stopped.
	StateStopped State = "stopped"
	// StateUnassigned means the connector has no assigned worker yet.
	StateUnassigned State = "unassigned"
	// StateMissing means the connector does not exist in Kafka Connect.
	StateMissing State = "missing"
	// StateUnknown means the state could not be normalized.
	StateUnknown State = "unknown"
)

// Client is the narrow Kafka Connect REST surface used by the runtime. All
// methods are idempotent so retries and repeated submits are safe.
type Client interface {
	UpsertConnector(ctx context.Context, name string, config map[string]any) error
	ConnectorStatus(ctx context.Context, name string) (State, error)
	StopConnector(ctx context.Context, name string) error
	DeleteConnector(ctx context.Context, name string) error
}

// HealthChecker is an optional client capability used by preflight. It is kept
// out of Client so submit-only fakes stay small.
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// RetryPolicy bounds retries and exponential backoff for Kafka Connect requests.
type RetryPolicy struct {
	// MaxRetries is the number of retries after the first attempt.
	MaxRetries int
	// BaseDelay is the first backoff delay.
	BaseDelay time.Duration
	// MaxDelay caps the backoff delay.
	MaxDelay time.Duration
	// Jitter randomizes each wait by ±Jitter (0..1). Zero disables jitter.
	Jitter float64
}

// DefaultRetryPolicy returns the design baseline: 1s, 2s, 4s, 8s, 16s, at most
// five retries, and ±20% jitter.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries: 5,
		BaseDelay:  time.Second,
		MaxDelay:   16 * time.Second,
		Jitter:     0.2,
	}
}

// Delay returns the deterministic backoff delay for a zero-based attempt.
func (p RetryPolicy) Delay(attempt int) time.Duration {
	base := p.BaseDelay
	if base <= 0 {
		base = time.Second
	}
	maxDelay := p.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 16 * time.Second
	}
	if attempt < 0 {
		attempt = 0
	}
	delay := base
	for i := 0; i < attempt && delay < maxDelay; i++ {
		if delay > maxDelay/2 {
			delay = maxDelay
			break
		}
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

func (p RetryPolicy) waitDelay(attempt int) time.Duration {
	delay := p.Delay(attempt)
	if p.Jitter <= 0 || delay <= 0 {
		return delay
	}
	jitter := p.Jitter
	if jitter > 1 {
		jitter = 1
	}
	factor := 1 + jitter*(2*rand.Float64()-1)
	return time.Duration(float64(delay) * factor)
}

// HTTPDoer is the injectable transport used by HTTPClient.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPClient is the net/http Kafka Connect REST client.
type HTTPClient struct {
	baseURL string
	doer    HTTPDoer
	policy  RetryPolicy
	metrics *Metrics
	tracer  trace.Tracer
}

var (
	_ Client        = (*HTTPClient)(nil)
	_ HealthChecker = (*HTTPClient)(nil)
)

// HTTPOption customizes the Kafka Connect REST client.
type HTTPOption func(*HTTPClient)

// WithHTTPTimeout bounds each HTTP round trip.
func WithHTTPTimeout(timeout time.Duration) HTTPOption {
	return func(c *HTTPClient) {
		if timeout > 0 {
			c.doer = &http.Client{Timeout: timeout}
		}
	}
}

// WithHTTPRetryPolicy overrides retry and backoff behavior.
func WithHTTPRetryPolicy(policy RetryPolicy) HTTPOption {
	return func(c *HTTPClient) { c.policy = policy }
}

// WithHTTPMetrics attaches bounded-label runtime metrics.
func WithHTTPMetrics(metrics *Metrics) HTTPOption {
	return func(c *HTTPClient) { c.metrics = metrics }
}

// WithHTTPTracer overrides the client trace provider.
func WithHTTPTracer(tracer trace.Tracer) HTTPOption {
	return func(c *HTTPClient) {
		if tracer != nil {
			c.tracer = tracer
		}
	}
}

// WithHTTPDoer injects the HTTP transport; it replaces the default client and
// ignores WithHTTPTimeout when applied after it.
func WithHTTPDoer(doer HTTPDoer) HTTPOption {
	return func(c *HTTPClient) {
		if doer != nil {
			c.doer = doer
		}
	}
}

// NewHTTPClient builds a Kafka Connect REST client. An empty base URL yields a
// client whose calls report ErrRuntimeNotConfigured; callers may create it before
// wiring is complete.
func NewHTTPClient(baseURL string, opts ...HTTPOption) *HTTPClient {
	c := &HTTPClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		doer:    &http.Client{Timeout: defaultHTTPTimeout},
		policy:  DefaultRetryPolicy(),
		tracer:  otel.Tracer("ontoaxis/connectors/runtime"),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// ClientError is a classified Kafka Connect HTTP failure. It never contains the
// response body because bodies may echo submitted credentials.
type ClientError struct {
	Operation  string
	StatusCode int
	err        error
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("kafka connect %s failed with HTTP %d", e.Operation, e.StatusCode)
}

// Unwrap exposes the classification sentinel.
func (e *ClientError) Unwrap() error { return e.err }

// UpsertConnector idempotently creates or updates a connector config.
func (c *HTTPClient) UpsertConnector(ctx context.Context, name string, config map[string]any) error {
	if err := c.validateName(name); err != nil {
		return err
	}
	body, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("%w: connector config is not JSON-serializable", ErrInvalidSyncConfig)
	}
	_, err = c.do(ctx, "upsert", http.MethodPut, "/connectors/"+name+"/config", body)
	return err
}

// ConnectorStatus reads and normalizes the connector state. A missing connector
// is reported as StateMissing without an error.
func (c *HTTPClient) ConnectorStatus(ctx context.Context, name string) (State, error) {
	if err := c.validateName(name); err != nil {
		return StateUnknown, err
	}
	body, err := c.do(ctx, "status", http.MethodGet, "/connectors/"+name+"/status", nil)
	if err != nil {
		var clientErr *ClientError
		if errors.As(err, &clientErr) && clientErr.StatusCode == http.StatusNotFound {
			return StateMissing, nil
		}
		return StateUnknown, err
	}
	var payload struct {
		Connector struct {
			State string `json:"state"`
		} `json:"connector"`
		Tasks []struct {
			State string `json:"state"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return StateUnknown, fmt.Errorf("%w: kafka connect status response is malformed", ErrConnectUnavailable)
	}
	state := NormalizeState(payload.Connector.State)
	if state == StateRunning {
		for _, task := range payload.Tasks {
			if NormalizeState(task.State) == StateFailed {
				return StateFailed, nil
			}
		}
	}
	return state, nil
}

// StopConnector stops a connector without deleting it. A missing connector is
// treated as already stopped.
func (c *HTTPClient) StopConnector(ctx context.Context, name string) error {
	if err := c.validateName(name); err != nil {
		return err
	}
	_, err := c.do(ctx, "stop", http.MethodPut, "/connectors/"+name+"/stop", nil)
	if err != nil && isNotFound(err) {
		return nil
	}
	return err
}

// DeleteConnector deletes a connector. A missing connector is treated as already
// deleted.
func (c *HTTPClient) DeleteConnector(ctx context.Context, name string) error {
	if err := c.validateName(name); err != nil {
		return err
	}
	_, err := c.do(ctx, "delete", http.MethodDelete, "/connectors/"+name, nil)
	if err != nil && isNotFound(err) {
		return nil
	}
	return err
}

// Ping verifies Kafka Connect reachability for preflight.
func (c *HTTPClient) Ping(ctx context.Context) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("%w: kafka connect url is not configured", ErrRuntimeNotConfigured)
	}
	_, err := c.do(ctx, "ping", http.MethodGet, "/", nil)
	return err
}

func (c *HTTPClient) validateName(name string) error {
	if name == "" || !connectorNameRegexp.MatchString(name) {
		return fmt.Errorf("%w: connector name must match %s", ErrInvalidConnectorName, connectorNamePattern)
	}
	return nil
}

func (c *HTTPClient) do(ctx context.Context, operation, method, path string, body []byte) ([]byte, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("%w: kafka connect url is not configured", ErrRuntimeNotConfigured)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	attempts := c.policy.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		payload, err := c.attempt(ctx, operation, method, path, body)
		if err == nil {
			c.observe(operation, nil)
			return payload, nil
		}
		lastErr = err
		if !errors.Is(err, ErrConnectUnavailable) || ctx.Err() != nil || attempt == attempts-1 {
			break
		}
		if waitErr := sleepContext(ctx, c.policy.waitDelay(attempt)); waitErr != nil {
			lastErr = fmt.Errorf("%w: kafka connect %s request cancelled", ErrConnectUnavailable, operation)
			break
		}
	}
	c.observe(operation, lastErr)
	return nil, lastErr
}

func (c *HTTPClient) attempt(ctx context.Context, operation, method, path string, body []byte) ([]byte, error) {
	ctx, span := c.tracer.Start(ctx, "kafka connect "+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("dependency", DependencyKafkaConnect),
			attribute.String("operation", operation),
			attribute.String("http.request.method", method),
		),
	)
	defer span.End()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		span.SetStatus(codes.Error, "invalid request")
		return nil, fmt.Errorf("%w: kafka connect %s request is invalid", ErrConnectRejected, operation)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := c.doer.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, "request failed")
		span.SetAttributes(attribute.String("result", ResultRetryableError))
		return nil, fmt.Errorf("%w: kafka connect %s request failed", ErrConnectUnavailable, operation)
	}
	defer func() { _ = resp.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if readErr != nil {
		span.SetStatus(codes.Error, "response read failed")
		span.SetAttributes(attribute.String("result", ResultRetryableError))
		return nil, fmt.Errorf("%w: kafka connect %s response read failed", ErrConnectUnavailable, operation)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		span.SetAttributes(attribute.String("result", ResultOK))
		return payload, nil
	}
	err = classifyHTTPError(operation, resp.StatusCode)
	span.SetStatus(codes.Error, "non-2xx response")
	span.SetAttributes(attribute.String("result", RequestResult(err)))
	return nil, err
}

func classifyHTTPError(operation string, statusCode int) error {
	switch {
	case statusCode == http.StatusConflict:
		return &ClientError{Operation: operation, StatusCode: statusCode, err: ErrConnectConflict}
	case statusCode == http.StatusTooManyRequests || statusCode >= 500:
		return &ClientError{Operation: operation, StatusCode: statusCode, err: ErrConnectUnavailable}
	default:
		return &ClientError{Operation: operation, StatusCode: statusCode, err: ErrConnectRejected}
	}
}

func isNotFound(err error) bool {
	var clientErr *ClientError
	return errors.As(err, &clientErr) && clientErr.StatusCode == http.StatusNotFound
}

func (c *HTTPClient) observe(operation string, err error) {
	if c.metrics == nil {
		return
	}
	c.metrics.ObserveRequest(DependencyKafkaConnect, operation, RequestResult(err))
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// NormalizeState maps Kafka Connect state strings to the audit surface.
func NormalizeState(raw string) State {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "RUNNING":
		return StateRunning
	case "FAILED":
		return StateFailed
	case "PAUSED":
		return StatePaused
	case "STOPPED":
		return StateStopped
	case "UNASSIGNED":
		return StateUnassigned
	case "MISSING":
		return StateMissing
	default:
		return StateUnknown
	}
}
