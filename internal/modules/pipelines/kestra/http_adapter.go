package kestra

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
	"io"
	mathrand "math/rand/v2"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// Defaults for the real adapter. They mirror the configuration defaults in
// design D12 and can be overridden through HTTPConfig.
const (
	// DefaultHTTPTimeout bounds one HTTP round trip.
	DefaultHTTPTimeout = 10 * time.Second
	// DefaultMaxAttempts is the attempt budget including the initial attempt.
	DefaultMaxAttempts = 3
	// DefaultBaseDelay is the first retry delay; later delays double.
	DefaultBaseDelay = 100 * time.Millisecond

	maxResponseBytes     = 4 << 20
	maxErrorMessageBytes = 300
	maxBackoffShift      = 5
	// searchPageSize is passed as the page size of the run-label lookup. The
	// upstream pagination parameter and label filter syntax are documented as
	// spike-pending in the design; this adapter uses the conventional size
	// parameter and a "key:value" label expression.
	searchPageSize = 100
)

// HTTPConfig configures the real Kestra adapter. An empty BaseURL is valid
// input for construction but every call then fails closed with
// ErrNotConfigured (design D12).
type HTTPConfig struct {
	// BaseURL is the Kestra API root, for example http://kestra:8080.
	BaseURL string
	// Timeout bounds one HTTP round trip. Defaults to DefaultHTTPTimeout.
	Timeout time.Duration
	// APIToken is sent as a bearer token. It is never logged or included in
	// returned errors (design D12).
	APIToken string
	// Environment is attached to flow and execution labels.
	Environment string
	// HTTPClient overrides the default client; tests inject one.
	HTTPClient *http.Client
	// Clock supplies the current time for retry jitter. Defaults to time.Now.
	Clock func() time.Time
	// MaxAttempts bounds total attempts. Defaults to DefaultMaxAttempts.
	MaxAttempts int
	// BaseDelay is the first retry delay. Defaults to DefaultBaseDelay.
	BaseDelay time.Duration
	// Metrics optionally records outbound request duration/result.
	Metrics *observability.Metrics
}

// HTTPAdapter talks to the Kestra REST API directly with net/http. Transient
// failures (connection errors, timeouts, 408, 429 and 5xx) are retried with
// bounded exponential backoff; other 4xx responses are surfaced immediately as
// APIError. It implements Adapter.
type HTTPAdapter struct {
	baseURL     string
	apiToken    string
	environment string
	client      *http.Client
	clock       func() time.Time
	maxAttempts int
	baseDelay   time.Duration
	// sleep performs the backoff wait. Tests replace it so retries do not wait
	// in real time.
	sleep   func(ctx context.Context, delay time.Duration) error
	metrics *observability.Metrics
}

// NewHTTPAdapter builds the real adapter. An empty (or whitespace-only) BaseURL
// yields ErrNotConfigured instead of a partially usable adapter.
func NewHTTPAdapter(config HTTPConfig) (*HTTPAdapter, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		return nil, ErrNotConfigured
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		// The URL is deliberately not echoed: it may embed credentials.
		return nil, fmt.Errorf("kestra: invalid base url")
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = DefaultHTTPTimeout
	}
	maxAttempts := config.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	baseDelay := config.BaseDelay
	if baseDelay <= 0 {
		baseDelay = DefaultBaseDelay
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &HTTPAdapter{
		baseURL:     baseURL,
		apiToken:    strings.TrimSpace(config.APIToken),
		environment: strings.TrimSpace(config.Environment),
		client:      client,
		clock:       clock,
		maxAttempts: maxAttempts,
		baseDelay:   baseDelay,
		sleep:       sleepContext,
		metrics:     config.Metrics,
	}, nil
}

// RegisterFlow idempotently upserts the flow definition. Kestra keys flows by
// (namespace, flow id) and the request body carries the Pipeline version as the
// revision, so repeating the call is the same PUT (design D14).
func (a *HTTPAdapter) RegisterFlow(ctx context.Context, flow Flow) (FlowRef, error) {
	if a == nil || a.client == nil {
		return FlowRef{}, ErrNotConfigured
	}
	if strings.TrimSpace(flow.TenantID) == "" || strings.TrimSpace(flow.PipelineID) == "" {
		return FlowRef{}, fmt.Errorf("kestra: tenant and pipeline are required")
	}
	ref := FlowRef{
		Namespace: deriveNamespace(flow.TenantID, flow.PipelineID),
		FlowID:    deriveFlowID(flow.PipelineID),
		Revision:  flow.Version,
	}
	body := flow.FlowYAML
	var err error
	contentType := "application/x-yaml"
	if len(body) == 0 {
		body, err = json.Marshal(flowUpsertBody{
			ID:        ref.FlowID,
			Namespace: ref.Namespace,
			Revision:  ref.Revision,
			Labels:    a.buildLabels(flow.TenantID, flow.PipelineID, flow.Version, flow.DAG, ""),
			Tasks:     dagTasks(flow.DAG),
		})
		if err != nil {
			return FlowRef{}, fmt.Errorf("kestra: encode flow: %s", a.redact(err.Error()))
		}
		contentType = "application/json"
	}
	apiPath := "/api/v1/flows/" + url.PathEscape(ref.Namespace) + "/" + url.PathEscape(ref.FlowID)
	err = a.withRetry(ctx, func(ctx context.Context) error {
		_, requestErr := a.doOnce(ctx, http.MethodPut, apiPath, nil, body, contentType)
		return requestErr
	})
	if err != nil {
		return FlowRef{}, err
	}
	return ref, nil
}

// PublishFiles uploads namespace files. Every path is validated before the
// first request so a malformed project cannot be left half published; each file
// is an idempotent upsert on the engine side.
func (a *HTTPAdapter) PublishFiles(ctx context.Context, namespace string, files []File) error {
	if a == nil || a.client == nil {
		return ErrNotConfigured
	}
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("kestra: namespace is required")
	}
	for _, file := range files {
		if err := validateFilePath(file.Path); err != nil {
			return err
		}
	}
	apiPath := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/files"
	for _, file := range files {
		body, contentType, err := encodeFileUpload(file)
		if err != nil {
			return fmt.Errorf("kestra: encode namespace file: %w", err)
		}
		err = a.withRetry(ctx, func(ctx context.Context) error {
			_, requestErr := a.doOnce(ctx, http.MethodPost, apiPath, nil, body, contentType)
			return requestErr
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteFiles removes namespace files. A 404 is ignored because compensation may
// run after a previous delete already succeeded.
func (a *HTTPAdapter) DeleteFiles(ctx context.Context, namespace string, paths []string) error {
	if a == nil || a.client == nil {
		return ErrNotConfigured
	}
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("kestra: namespace is required")
	}
	for _, filePath := range paths {
		if err := validateFilePath(filePath); err != nil {
			return err
		}
	}
	for _, filePath := range paths {
		apiPath := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/files/" + escapeFilePath(filePath)
		err := a.withRetry(ctx, func(ctx context.Context) error {
			_, requestErr := a.doOnce(ctx, http.MethodDelete, apiPath, nil, nil, "")
			return requestErr
		})
		if err == nil {
			continue
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			continue
		}
		return err
	}
	return nil
}

// TriggerRun starts one execution labelled with the core-side run identifier.
// Before retrying a transiently failed POST it first looks for an execution with
// that label, so a lost response is adopted instead of executed twice.
func (a *HTTPAdapter) TriggerRun(ctx context.Context, request RunRequest) (RunResult, error) {
	if a == nil || a.client == nil {
		return RunResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.PipelineID) == "" {
		return RunResult{}, fmt.Errorf("kestra: tenant and pipeline are required")
	}
	namespace := deriveNamespace(request.TenantID, request.PipelineID)
	flowID := deriveFlowID(request.PipelineID)
	body, err := json.Marshal(triggerBody{
		Labels: a.buildLabels(request.TenantID, request.PipelineID, request.Version, request.DAG, request.RunID),
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("kestra: encode trigger: %s", a.redact(err.Error()))
	}
	apiPath := "/api/v1/executions/" + url.PathEscape(namespace) + "/" + url.PathEscape(flowID)
	maxAttempts := a.attemptBudget()
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		payload, requestErr := a.doOnce(ctx, http.MethodPost, apiPath, nil, body, "application/json")
		if requestErr == nil {
			if executionID := executionIDFromPayload(payload); executionID != "" {
				return RunResult{ExecutionID: executionID, Status: executionStatusFromPayload(payload, "queued")}, nil
			}
			// A success without an id is only usable when the run label can
			// find the execution that was just created.
			if adopted, adoptErr := a.adoptExecution(ctx, request); adoptErr == nil {
				return RunResult{ExecutionID: adopted.ID, Status: adopted.Status}, nil
			}
			return RunResult{}, &APIError{Code: "invalid_response", Message: "kestra: trigger response has no execution id"}
		}
		lastErr = requestErr
		if !IsRetryable(requestErr) || ctx.Err() != nil {
			return RunResult{}, requestErr
		}
		if adopted, adoptErr := a.adoptExecution(ctx, request); adoptErr == nil {
			return RunResult{ExecutionID: adopted.ID, Status: adopted.Status}, nil
		}
		if attempt >= maxAttempts {
			break
		}
		if sleepErr := a.sleep(ctx, a.backoff(attempt)); sleepErr != nil {
			return RunResult{}, sleepErr
		}
	}
	return RunResult{}, lastErr
}

// GetExecution reads one execution. 404 surfaces as an error that unwraps to
// ErrNotFound so callers can classify execution_not_found without parsing
// bodies.
func (a *HTTPAdapter) GetExecution(ctx context.Context, ref ExecutionRef) (Execution, error) {
	if a == nil || a.client == nil {
		return Execution{}, ErrNotConfigured
	}
	if strings.TrimSpace(ref.ID) == "" {
		return Execution{}, fmt.Errorf("kestra: execution id is required")
	}
	var execution Execution
	err := a.withRetry(ctx, func(ctx context.Context) error {
		found, requestErr := a.getExecutionOnce(ctx, ref.ID)
		if requestErr != nil {
			return requestErr
		}
		execution = found
		return nil
	})
	if err != nil {
		return Execution{}, err
	}
	return execution, nil
}

// FindExecutionByRun searches executions by the ontoaxis.run_id label and
// requires the returned labels to match the tenant as well.
func (a *HTTPAdapter) FindExecutionByRun(ctx context.Context, tenantID, runID string) (Execution, error) {
	if a == nil || a.client == nil {
		return Execution{}, ErrNotConfigured
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(runID) == "" {
		return Execution{}, fmt.Errorf("kestra: tenant and run id are required")
	}
	var execution Execution
	err := a.withRetry(ctx, func(ctx context.Context) error {
		found, requestErr := a.searchExecutionByRun(ctx, tenantID, runID)
		if requestErr != nil {
			return requestErr
		}
		execution = found
		return nil
	})
	if err != nil {
		return Execution{}, err
	}
	return execution, nil
}

// CancelExecution asks the engine to kill an execution. An execution that is
// already finished (404/409) needs no cancellation.
func (a *HTTPAdapter) CancelExecution(ctx context.Context, ref ExecutionRef) error {
	if a == nil || a.client == nil {
		return ErrNotConfigured
	}
	if strings.TrimSpace(ref.ID) == "" {
		return fmt.Errorf("kestra: execution id is required")
	}
	apiPath := "/api/v1/executions/" + url.PathEscape(ref.ID) + "/kill"
	err := a.withRetry(ctx, func(ctx context.Context) error {
		_, requestErr := a.doOnce(ctx, http.MethodPost, apiPath, nil, nil, "")
		return requestErr
	})
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusConflict) {
		return nil
	}
	return err
}

// APIError classifies one engine API failure. StatusCode is 0 for transport
// failures; Retryable reports whether another attempt could succeed; Message is
// already sanitized (no request/response bodies, tokens or DSNs).
type APIError struct {
	StatusCode int
	Retryable  bool
	Code       string
	Message    string
}

// Error implements error with a stable, sanitized rendering.
func (e *APIError) Error() string {
	if e == nil {
		return "kestra: <nil> api error"
	}
	builder := strings.Builder{}
	builder.WriteString("kestra: ")
	code := strings.TrimSpace(e.Code)
	if code == "" {
		code = "request_failed"
	}
	builder.WriteString(code)
	if e.StatusCode > 0 {
		fmt.Fprintf(&builder, " (HTTP %d)", e.StatusCode)
	}
	if message := sanitizeMessage(e.Message, ""); message != "" {
		builder.WriteString(": ")
		builder.WriteString(message)
	}
	return builder.String()
}

// Unwrap exposes ErrNotFound for HTTP 404 responses.
func (e *APIError) Unwrap() error {
	if e != nil && e.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	return nil
}

// IsRetryable reports whether err is an APIError classified as transient.
func IsRetryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable
	}
	return false
}

type flowUpsertBody struct {
	ID        string            `json:"id"`
	Namespace string            `json:"namespace"`
	Revision  int               `json:"revision"`
	Labels    map[string]string `json:"labels"`
	Tasks     json.RawMessage   `json:"tasks"`
}

type triggerBody struct {
	Labels map[string]string `json:"labels"`
}

func (a *HTTPAdapter) buildLabels(tenantID, pipelineID string, version int, dag json.RawMessage, runID string) map[string]string {
	labels := map[string]string{
		labelTenantID:        tenantID,
		labelPipelineID:      pipelineID,
		labelPipelineVersion: strconv.Itoa(version),
		labelDAGVersion:      dagVersion(dag),
		labelManagedBy:       managedByValue,
	}
	if a.environment != "" {
		labels[labelEnvironment] = a.environment
	}
	if runID != "" {
		labels[labelRunID] = runID
	}
	return labels
}

// dagTasks forwards the authoring DAG as the flow task payload. The transport
// seam deliberately does not depend on the flow renderer: a later integration
// can substitute rendered Flow YAML/JSON without changing the interface.
func dagTasks(dag json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(dag)) == 0 {
		return json.RawMessage("null")
	}
	return dag
}

// withRetry runs attempt until it succeeds, fails non-retryably, exhausts the
// attempt budget or the context is done.
func (a *HTTPAdapter) withRetry(ctx context.Context, attempt func(context.Context) error) error {
	maxAttempts := a.attemptBudget()
	var lastErr error
	for number := 1; number <= maxAttempts; number++ {
		lastErr = attempt(ctx)
		if lastErr == nil {
			return nil
		}
		if !IsRetryable(lastErr) || ctx.Err() != nil || number >= maxAttempts {
			return lastErr
		}
		if err := a.sleep(ctx, a.backoff(number)); err != nil {
			return err
		}
	}
	return lastErr
}

// attemptBudget returns the configured attempt count with a safe default for
// adapters created as a bare struct literal instead of through the constructor.
func (a *HTTPAdapter) attemptBudget() int {
	if a != nil && a.maxAttempts > 0 {
		return a.maxAttempts
	}
	return DefaultMaxAttempts
}

// backoff returns the wait before the next attempt: BaseDelay*2^(attempt-1)
// with +/-20% jitter derived deterministically from the injected clock.
func (a *HTTPAdapter) backoff(attempt int) time.Duration {
	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	if shift > maxBackoffShift {
		shift = maxBackoffShift
	}
	baseDelay := a.baseDelay
	if baseDelay <= 0 {
		baseDelay = DefaultBaseDelay
	}
	delay := baseDelay << uint(shift)
	clock := a.clock
	if clock == nil {
		clock = time.Now
	}
	source := mathrand.New(mathrand.NewPCG(uint64(clock().UnixNano()), uint64(attempt)))
	factor := 0.8 + 0.4*source.Float64()
	return time.Duration(float64(delay) * factor)
}

func (a *HTTPAdapter) adoptExecution(ctx context.Context, request RunRequest) (Execution, error) {
	if request.RunID == "" {
		return Execution{}, fmt.Errorf("%w: run id is required for adoption", ErrNotFound)
	}
	return a.searchExecutionByRun(ctx, request.TenantID, request.RunID)
}

func (a *HTTPAdapter) searchExecutionByRun(ctx context.Context, tenantID, runID string) (Execution, error) {
	query := url.Values{}
	query.Set("labels", labelRunID+":"+runID)
	query.Set("size", strconv.Itoa(searchPageSize))
	payload, err := a.doOnce(ctx, http.MethodGet, "/api/v1/executions", query, nil, "")
	if err != nil {
		return Execution{}, err
	}
	executions, err := decodeExecutionList(payload)
	if err != nil {
		return Execution{}, err
	}
	for _, execution := range executions {
		if execution.Labels[labelTenantID] != tenantID || execution.Labels[labelRunID] != runID {
			continue
		}
		return execution, nil
	}
	return Execution{}, fmt.Errorf("%w: no execution for run %s", ErrNotFound, runID)
}

func (a *HTTPAdapter) getExecutionOnce(ctx context.Context, executionID string) (Execution, error) {
	payload, err := a.doOnce(ctx, http.MethodGet, "/api/v1/executions/"+url.PathEscape(executionID), nil, nil, "")
	if err != nil {
		return Execution{}, err
	}
	return decodeExecution(payload)
}

// doOnce performs exactly one HTTP round trip and maps transport and status
// failures to APIError. Bodies are never included in returned errors.
func (a *HTTPAdapter) doOnce(ctx context.Context, method, apiPath string, query url.Values, body []byte, contentType string) (payload []byte, err error) {
	started := time.Now()
	defer func() {
		if a.metrics != nil {
			result := "success"
			if err != nil {
				result = "error"
			}
			a.metrics.OutboundDuration.WithLabelValues("kestra", outboundOperation(method, apiPath), result).Observe(time.Since(started).Seconds())
		}
	}()
	target := a.baseURL + apiPath
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, &APIError{Code: "invalid_request", Message: "kestra: could not build request"}
	}
	request.Header.Set("Accept", "application/json")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if strings.HasPrefix(a.apiToken, "basic:") {
		encoded := base64.StdEncoding.EncodeToString([]byte(strings.TrimPrefix(a.apiToken, "basic:")))
		request.Header.Set("Authorization", "Basic "+encoded)
	} else if a.apiToken != "" {
		request.Header.Set("Authorization", "Bearer "+a.apiToken)
	}
	injectTraceparent(ctx, request.Header)

	response, err := a.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, a.transportError(err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if readErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, &APIError{Retryable: true, Code: "unavailable", Message: "kestra: failed to read engine response"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, a.apiError(response.StatusCode, payload)
	}
	return payload, nil
}

func outboundOperation(method, path string) string {
	switch {
	case strings.Contains(path, "/flows/"):
		return "flows.upsert"
	case strings.Contains(path, "/files"):
		if method == http.MethodDelete {
			return "files.delete"
		}
		return "files.publish"
	case strings.Contains(path, "/executions/") && strings.HasSuffix(path, "/kill"):
		return "executions.cancel"
	case strings.Contains(path, "/executions/"):
		return "executions.get"
	case strings.Contains(path, "/executions"):
		return "executions.trigger"
	default:
		return "request"
	}
}

func (a *HTTPAdapter) transportError(err error) *APIError {
	code := "unavailable"
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		code = "timeout"
	}
	return &APIError{Retryable: true, Code: code, Message: a.redact("request failed: " + err.Error())}
}

func (a *HTTPAdapter) apiError(statusCode int, payload []byte) *APIError {
	code, retryable := classifyStatus(statusCode)
	message := a.redact(engineMessage(payload))
	if message == "" {
		message = http.StatusText(statusCode)
	}
	return &APIError{StatusCode: statusCode, Retryable: retryable, Code: code, Message: message}
}

// classifyStatus maps an HTTP status to the adapter error taxonomy. 4xx
// responses are invalid requests and are not retried; timeouts, rate limits and
// 5xx responses are transient. 409 conflict is reported as non-retryable because
// replaying the same upsert cannot resolve it.
func classifyStatus(statusCode int) (code string, retryable bool) {
	switch {
	case statusCode == http.StatusRequestTimeout:
		return "timeout", true
	case statusCode == http.StatusTooManyRequests:
		return "rate_limited", true
	case statusCode >= 500:
		return "unavailable", true
	case statusCode == http.StatusConflict:
		return "conflict", false
	case statusCode == http.StatusNotFound:
		return "not_found", false
	case statusCode >= 400:
		return "invalid_argument", false
	default:
		return "unexpected_status", false
	}
}

// engineMessage extracts an error message from a JSON error envelope. Non-JSON
// bodies (HTML error pages, stack traces) are ignored instead of being copied
// into errors.
func engineMessage(payload []byte) string {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ""
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return ""
	}
	for _, key := range []string{"message", "error", "detail"} {
		raw, ok := envelope[key]
		if !ok {
			continue
		}
		if text := rawText(raw); text != "" {
			return text
		}
	}
	return ""
}

func rawText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		return strings.TrimSpace(text)
	}
	var nested struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(trimmed, &nested) == nil {
		return strings.TrimSpace(nested.Message)
	}
	return ""
}

var (
	bearerTokenPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
	userinfoURLPattern = regexp.MustCompile(`(://)[^/@\s]+@`)
	secretValuePattern = regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token|api[_-]?key|authorization)["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;&}]+)`)
)

func (a *HTTPAdapter) redact(message string) string {
	if a == nil {
		return sanitizeMessage(message, "")
	}
	return sanitizeMessage(message, a.apiToken)
}

// sanitizeMessage removes credentials and DSN user info from an upstream
// message, collapses whitespace and bounds the length. It never returns a raw
// request or response body.
func sanitizeMessage(message, apiToken string) string {
	if apiToken != "" {
		message = strings.ReplaceAll(message, apiToken, "[REDACTED]")
	}
	message = bearerTokenPattern.ReplaceAllString(message, "Bearer [REDACTED]")
	message = userinfoURLPattern.ReplaceAllString(message, "${1}[REDACTED]@")
	message = secretValuePattern.ReplaceAllString(message, "${1}[REDACTED]")
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > maxErrorMessageBytes {
		message = message[:maxErrorMessageBytes] + "..."
	}
	return message
}

// injectTraceparent adds a W3C traceparent header for every engine request. It
// reuses the active span when the context carries one and otherwise generates a
// fresh trace id so callers can correlate logs even without a tracer provider.
func injectTraceparent(ctx context.Context, header http.Header) {
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		flags := "00"
		if spanContext.IsSampled() {
			flags = "01"
		}
		header.Set("traceparent", "00-"+spanContext.TraceID().String()+"-"+spanContext.SpanID().String()+"-"+flags)
		return
	}
	var traceID [16]byte
	var spanID [8]byte
	if _, err := cryptorand.Read(traceID[:]); err != nil {
		return
	}
	if _, err := cryptorand.Read(spanID[:]); err != nil {
		return
	}
	// All-zero values are invalid per the W3C spec.
	traceID[0] |= 1
	spanID[0] |= 1
	header.Set("traceparent", "00-"+hex.EncodeToString(traceID[:])+"-"+hex.EncodeToString(spanID[:])+"-01")
}

func executionIDFromPayload(payload []byte) string {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return ""
	}
	var envelope struct {
		ID          string `json:"id"`
		ExecutionID string `json:"executionId"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return ""
	}
	if id := strings.TrimSpace(envelope.ID); id != "" {
		return id
	}
	return strings.TrimSpace(envelope.ExecutionID)
}

func executionStatusFromPayload(payload []byte, fallback string) string {
	execution, err := decodeExecution(payload)
	if err != nil || execution.Status == "" {
		return fallback
	}
	return execution.Status
}

type executionPayload struct {
	ID          string                     `json:"id"`
	ExecutionID string                     `json:"executionId"`
	Namespace   string                     `json:"namespace"`
	FlowID      string                     `json:"flowId"`
	Status      json.RawMessage            `json:"status"`
	State       json.RawMessage            `json:"state"`
	Labels      json.RawMessage            `json:"labels"`
	Outputs     map[string]json.RawMessage `json:"outputs"`
}

// decodeExecution maps an engine execution document onto the provider-neutral
// Execution. Kestra versions differ slightly in their JSON shape, so the decoder
// accepts both the flat status string and the nested state/status object as well
// as both label maps and [{key,value}] lists.
func decodeExecution(payload []byte) (Execution, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return Execution{}, invalidResponse("malformed execution response")
	}
	var raw executionPayload
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return Execution{}, invalidResponse("malformed execution response")
	}
	executionID := strings.TrimSpace(raw.ID)
	if executionID == "" {
		executionID = strings.TrimSpace(raw.ExecutionID)
	}
	if executionID == "" {
		return Execution{}, invalidResponse("execution response has no id")
	}
	status := rawExecutionStatus(raw.Status)
	if status == "" {
		status = rawExecutionStatus(raw.State)
	}
	return Execution{
		ID:        executionID,
		Status:    status,
		Namespace: strings.TrimSpace(raw.Namespace),
		FlowID:    strings.TrimSpace(raw.FlowID),
		Labels:    decodeExecutionLabels(raw.Labels),
		Outputs:   decodeExecutionOutputs(raw.Outputs),
	}, nil
}

func decodeExecutionList(payload []byte) ([]Execution, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil, invalidResponse("empty execution search response")
	}
	switch trimmed[0] {
	case '[':
		return decodeExecutionArray(trimmed)
	case '{':
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &envelope); err != nil {
			return nil, invalidResponse("malformed execution search response")
		}
		for _, key := range []string{"results", "data", "items"} {
			raw, ok := envelope[key]
			if !ok {
				continue
			}
			trimmedRaw := bytes.TrimSpace(raw)
			if len(trimmedRaw) > 0 && trimmedRaw[0] == '[' {
				return decodeExecutionArray(trimmedRaw)
			}
		}
		execution, err := decodeExecution(trimmed)
		if err != nil {
			return nil, err
		}
		return []Execution{execution}, nil
	default:
		return nil, invalidResponse("malformed execution search response")
	}
}

func decodeExecutionArray(raw []byte) ([]Execution, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, invalidResponse("malformed execution search response")
	}
	executions := make([]Execution, 0, len(items))
	for _, item := range items {
		execution, err := decodeExecution(item)
		if err != nil {
			return nil, err
		}
		executions = append(executions, execution)
	}
	return executions, nil
}

func rawExecutionStatus(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		return strings.TrimSpace(text)
	}
	var envelope struct {
		Current string `json:"current"`
	}
	if json.Unmarshal(trimmed, &envelope) == nil {
		return strings.TrimSpace(envelope.Current)
	}
	return ""
}

func decodeExecutionLabels(raw json.RawMessage) map[string]string {
	labels := map[string]string{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return labels
	}
	switch trimmed[0] {
	case '{':
		var object map[string]json.RawMessage
		if json.Unmarshal(trimmed, &object) != nil {
			return labels
		}
		for key, value := range object {
			labels[key] = rawScalarString(value)
		}
	case '[':
		var entries []struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(trimmed, &entries) != nil {
			return labels
		}
		for _, entry := range entries {
			if strings.TrimSpace(entry.Key) == "" {
				continue
			}
			labels[entry.Key] = rawScalarString(entry.Value)
		}
	}
	return labels
}

func rawScalarString(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		return text
	}
	return string(trimmed)
}

func decodeExecutionOutputs(raw map[string]json.RawMessage) map[string]string {
	outputs := make(map[string]string, len(raw))
	for key, value := range raw {
		trimmed := bytes.TrimSpace(value)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		var text string
		if json.Unmarshal(trimmed, &text) == nil {
			outputs[key] = text
			continue
		}
		outputs[key] = string(trimmed)
	}
	return outputs
}

func invalidResponse(message string) *APIError {
	return &APIError{Code: "invalid_response", Message: message}
}

func validateFilePath(filePath string) error {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return fmt.Errorf("kestra: file path is required")
	}
	if strings.HasPrefix(trimmed, "/") || path.IsAbs(trimmed) {
		return fmt.Errorf("kestra: file path must be relative")
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("kestra: file path escapes the namespace")
	}
	return nil
}

func escapeFilePath(filePath string) string {
	segments := strings.Split(filePath, "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	return strings.Join(segments, "/")
}

// encodeFileUpload builds the multipart body for one namespace file. The file
// part carries the base name and the "path" field carries the directory so the
// engine can place the content at its project-relative location. This mirrors
// the Kestra UI upload shape; the exact upstream field names are part of the
// spike checklist documented in the change design.
func encodeFileUpload(file File) ([]byte, string, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	directory := path.Dir(file.Path)
	if directory == "." {
		directory = ""
	}
	if err := writer.WriteField("path", directory); err != nil {
		return nil, "", err
	}
	part, err := writer.CreateFormFile("file", path.Base(file.Path))
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(file.Content); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
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
