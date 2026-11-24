package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

const (
	defaultClientTimeout = 1500 * time.Millisecond
	defaultWaitTimeout   = 30 * time.Second
	taskPollInterval     = 50 * time.Millisecond
	maxResponseBytes     = 4 << 20
)

// Client is a thin, dependency-free Meilisearch HTTP client. It performs no
// automatic retries: retry semantics belong to the consumer and rebuild
// callers. Request and trace identifiers from the context are propagated.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	timeout    time.Duration
	wait       time.Duration
	tracer     trace.Tracer
}

// ClientOption customizes a Client.
type ClientOption func(*Client)

// WithTimeout sets the per-request timeout. It must be positive.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		if timeout > 0 {
			c.timeout = timeout
		}
	}
}

// WithHTTPClient replaces the underlying HTTP client (useful in tests).
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithTracer sets the tracer used for meilisearch.* spans. A nil tracer keeps
// the no-op tracer.
func WithTracer(tracer trace.Tracer) ClientOption {
	return func(c *Client) {
		if tracer != nil {
			c.tracer = tracer
		}
	}
}

// WithWaitTimeout sets the maximum time WaitTask (and task-returning helpers)
// waits for a Meilisearch task.
func WithWaitTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		if timeout > 0 {
			c.wait = timeout
		}
	}
}

// NewClient constructs a Meilisearch client. baseURL must include a scheme and
// host; a trailing slash is ignored. apiKey is sent as a bearer token and is
// never logged.
func NewClient(baseURL, apiKey string, opts ...ClientOption) (*Client, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, Errorf(ErrInvalid, "new_client", "search url is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, Errorf(ErrInvalid, "new_client", "search url scheme must be http or https")
	}
	if parsed.Host == "" {
		return nil, Errorf(ErrInvalid, "new_client", "search url must include a host")
	}
	if parsed.User != nil {
		return nil, Errorf(ErrInvalid, "new_client", "search url must not include credentials")
	}
	client := &Client{
		baseURL: trimmed,
		apiKey:  strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: 0, // per-request context deadline governs timeouts
		},
		timeout: defaultClientTimeout,
		wait:    defaultWaitTimeout,
		tracer:  noop.NewTracerProvider().Tracer("ontoaxis-search"),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	if client.timeout <= 0 {
		client.timeout = defaultClientTimeout
	}
	return client, nil
}

// BaseURL returns the configured base URL (without credentials).
func (c *Client) BaseURL() string { return c.baseURL }

type apiErrorBody struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Type    string `json:"type"`
	Link    string `json:"link"`
}

type taskResponse struct {
	TaskUID int64  `json:"taskUid"`
	UID     string `json:"uid"`
	Status  string `json:"status"`
}

type taskStatus struct {
	UID    int64          `json:"uid"`
	Status string         `json:"status"`
	Error  *taskErrorInfo `json:"error"`
}

type taskErrorInfo struct {
	Code    string `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
	Link    string `json:"link"`
}

// EnsureIndex creates the index when missing and applies settings. It is
// idempotent: existing indexes are kept and their settings overwritten.
func (c *Client) EnsureIndex(ctx context.Context, uid string, settings json.RawMessage) error {
	if strings.TrimSpace(uid) == "" {
		return Errorf(ErrInvalid, "ensure_index", "index uid is empty")
	}
	var existing map[string]any
	err := c.do(ctx, "get_index", http.MethodGet, "/indexes/"+url.PathEscape(uid), nil, &existing)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		createBody := map[string]any{"uid": uid, "primaryKey": PrimaryKey}
		if createErr := c.do(ctx, "create_index", http.MethodPost, "/indexes", createBody, nil); createErr != nil {
			// A concurrent creator is harmless: re-read the index and treat it
			// as success when it now exists.
			if errors.Is(createErr, ErrInvalid) {
				var check map[string]any
				if checkErr := c.do(ctx, "get_index", http.MethodGet, "/indexes/"+url.PathEscape(uid), nil, &check); checkErr == nil {
					err = nil
				}
			}
			if err != nil {
				return createErr
			}
		}
	}
	if len(settings) == 0 {
		settings = SettingsJSON()
	}
	var task taskResponse
	if err := c.do(ctx, "update_settings", http.MethodPatch, "/indexes/"+url.PathEscape(uid)+"/settings", settings, &task); err != nil {
		return err
	}
	if task.TaskUID != 0 {
		if err := c.WaitTask(ctx, uid, strconv.FormatInt(task.TaskUID, 10), c.wait); err != nil {
			return err
		}
	}
	return nil
}

// DeleteIndex deletes an index. A missing index is not an error.
func (c *Client) DeleteIndex(ctx context.Context, uid string) error {
	if strings.TrimSpace(uid) == "" {
		return Errorf(ErrInvalid, "delete_index", "index uid is empty")
	}
	err := c.do(ctx, "delete_index", http.MethodDelete, "/indexes/"+url.PathEscape(uid), nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// UpsertDocuments adds or fully replaces documents and returns the Meilisearch
// task UID. Waiting for task completion is a separate WaitTask call.
func (c *Client) UpsertDocuments(ctx context.Context, uid string, docs []Document) (string, error) {
	if strings.TrimSpace(uid) == "" {
		return "", Errorf(ErrInvalid, "upsert_documents", "index uid is empty")
	}
	if len(docs) == 0 {
		return "", nil
	}
	var response taskResponse
	path := "/indexes/" + url.PathEscape(uid) + "/documents?primaryKey=" + url.QueryEscape(PrimaryKey)
	if err := c.do(ctx, "upsert_documents", http.MethodPost, path, docs, &response); err != nil {
		return "", err
	}
	return strconv.FormatInt(response.TaskUID, 10), nil
}

// DeleteDocuments deletes documents by ID and returns the Meilisearch task UID.
func (c *Client) DeleteDocuments(ctx context.Context, uid string, ids []string) (string, error) {
	if strings.TrimSpace(uid) == "" {
		return "", Errorf(ErrInvalid, "delete_documents", "index uid is empty")
	}
	if len(ids) == 0 {
		return "", nil
	}
	var response taskResponse
	path := "/indexes/" + url.PathEscape(uid) + "/documents/delete-batch"
	if err := c.do(ctx, "delete_documents", http.MethodPost, path, ids, &response); err != nil {
		return "", err
	}
	return strconv.FormatInt(response.TaskUID, 10), nil
}

// SearchRequest is the query contract sent to Meilisearch.
type SearchRequest struct {
	Q      string
	Filter string
	Sort   []string
	Limit  int
	Offset int
}

// SearchResult is the normalized search response. Truncated is true when the
// backend reports more matching documents than the configured total-hits
// window, meaning the returned list cannot be paged exhaustively.
type SearchResult struct {
	Hits               []Document
	EstimatedTotalHits int64
	Limit              int
	Offset             int
	Truncated          bool
}

type searchResponse struct {
	Hits               []Document `json:"hits"`
	EstimatedTotalHits int64      `json:"estimatedTotalHits"`
	TotalHits          int64      `json:"totalHits"`
	Limit              int        `json:"limit"`
	Offset             int        `json:"offset"`
}

// Search executes a search query. The caller must include the tenant filter;
// this method does not add one implicitly.
func (c *Client) Search(ctx context.Context, uid string, req SearchRequest) (SearchResult, error) {
	if strings.TrimSpace(uid) == "" {
		return SearchResult{}, Errorf(ErrInvalid, "search", "index uid is empty")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	body := map[string]any{
		"q":      req.Q,
		"limit":  limit,
		"offset": req.Offset,
	}
	if req.Filter != "" {
		body["filter"] = req.Filter
	}
	if len(req.Sort) > 0 {
		body["sort"] = req.Sort
	}
	var response searchResponse
	if err := c.do(ctx, "search", http.MethodPost, "/indexes/"+url.PathEscape(uid)+"/search", body, &response); err != nil {
		return SearchResult{}, err
	}
	total := response.EstimatedTotalHits
	resultLimit := response.Limit
	if resultLimit <= 0 {
		resultLimit = limit
	}
	resultOffset := response.Offset
	if resultOffset == 0 && req.Offset != 0 {
		resultOffset = req.Offset
	}
	if resultOffset < 0 {
		resultOffset = req.Offset
	}
	return SearchResult{
		Hits:               response.Hits,
		EstimatedTotalHits: total,
		Limit:              resultLimit,
		Offset:             resultOffset,
		Truncated:          total > MaxTotalHits,
	}, nil
}

// DocumentPage reports exact document pagination, independent of search hits.
type DocumentPage struct {
	Documents []Document
	Total     int
}

// ListDocuments reads a page of complete index documents, including fields
// hidden from search results.
func (c *Client) ListDocuments(ctx context.Context, uid string, offset, limit int) (DocumentPage, error) {
	const op = "list_documents"
	if strings.TrimSpace(uid) == "" || offset < 0 || limit <= 0 {
		return DocumentPage{}, Errorf(ErrInvalid, op, "invalid index or page bounds")
	}
	query := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}}
	var response struct {
		Results []json.RawMessage `json:"results"`
		Offset  *int              `json:"offset"`
		Limit   *int              `json:"limit"`
		Total   *int              `json:"total"`
	}
	path := "/indexes/" + url.PathEscape(uid) + "/documents?" + query.Encode()
	if err := c.do(ctx, op, http.MethodGet, path, nil, &response); err != nil {
		return DocumentPage{}, err
	}
	if response.Results == nil || response.Offset == nil || *response.Offset != offset ||
		response.Limit == nil || *response.Limit != limit || response.Total == nil || *response.Total < 0 ||
		len(response.Results) != min(limit, max(0, *response.Total-offset)) {
		return DocumentPage{}, Errorf(ErrUnavailable, op, "inconsistent document pagination")
	}
	page := DocumentPage{Total: *response.Total, Documents: make([]Document, 0, len(response.Results))}
	for _, raw := range response.Results {
		var fields map[string]json.RawMessage
		var doc Document
		if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 8 {
			return DocumentPage{}, Errorf(ErrUnavailable, op, "unexpected index document fields")
		}
		for _, name := range [...]string{"id", "tenant_id", "object_type_id", "display_name", "properties_text", "version", "created_at", "updated_at"} {
			if value := fields[name]; len(value) == 0 || bytes.Equal(value, []byte("null")) {
				return DocumentPage{}, Errorf(ErrUnavailable, op, "missing index document field")
			}
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return DocumentPage{}, Errorf(ErrUnavailable, op, "invalid index document field type")
		}
		page.Documents = append(page.Documents, doc)
	}
	return page, nil
}

// WaitTask polls a Meilisearch task until it succeeds, fails, or the timeout
// elapses. Failed tasks map to ErrInvalid for document/request validation
// errors and ErrUnavailable otherwise.
func (c *Client) WaitTask(ctx context.Context, uid, taskUID string, timeout time.Duration) error {
	if strings.TrimSpace(taskUID) == "" {
		return Errorf(ErrInvalid, "wait_task", "task uid is empty")
	}
	if timeout <= 0 {
		timeout = c.wait
	}
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(taskPollInterval)
	defer ticker.Stop()
	for {
		var task taskStatus
		err := c.do(waitCtx, "get_task", http.MethodGet, "/tasks/"+url.PathEscape(taskUID), nil, &task)
		if err != nil {
			if waitCtx.Err() != nil {
				return Errorf(ErrUnavailable, "wait_task", "task %s did not finish within %s", taskUID, timeout)
			}
			return err
		}
		switch strings.ToLower(task.Status) {
		case "succeeded":
			return nil
		case "failed":
			kind := ErrUnavailable
			code := ""
			if task.Error != nil {
				code = task.Error.Code
				if looksInvalidTaskCode(code) {
					kind = ErrInvalid
				}
			}
			return &Error{Kind: kind, Op: "wait_task", Err: fmt.Errorf("task %s failed (code %q)", taskUID, code)}
		case "canceled", "cancelled":
			return &Error{Kind: ErrUnavailable, Op: "wait_task", Err: fmt.Errorf("task %s was canceled", taskUID)}
		}
		select {
		case <-waitCtx.Done():
			return Errorf(ErrUnavailable, "wait_task", "task %s did not finish within %s", taskUID, timeout)
		case <-ticker.C:
		}
	}
}

func looksInvalidTaskCode(code string) bool {
	code = strings.ToLower(code)
	for _, prefix := range []string{"invalid", "missing", "not_found", "index_not_found", "document_not_found", "bad_request"} {
		if strings.HasPrefix(code, prefix) {
			return true
		}
	}
	return false
}

// SwapIndexes atomically swaps index pairs via /swap-indexes. When Meilisearch
// returns a task UID the call waits for the swap task to finish so callers can
// replay events against the post-swap index.
func (c *Client) SwapIndexes(ctx context.Context, pairs [][2]string) error {
	if len(pairs) == 0 {
		return nil
	}
	payload := make([]map[string][]string, 0, len(pairs))
	for _, pair := range pairs {
		if strings.TrimSpace(pair[0]) == "" || strings.TrimSpace(pair[1]) == "" {
			return Errorf(ErrInvalid, "swap_indexes", "swap pair contains an empty index uid")
		}
		payload = append(payload, map[string][]string{"indexes": {pair[0], pair[1]}})
	}
	var response taskResponse
	if err := c.do(ctx, "swap_indexes", http.MethodPost, "/swap-indexes", payload, &response); err != nil {
		return err
	}
	if response.TaskUID == 0 {
		return nil
	}
	return c.WaitTask(ctx, "", strconv.FormatInt(response.TaskUID, 10), c.wait)
}

// Health reports whether Meilisearch responds to /health.
func (c *Client) Health(ctx context.Context) error {
	var response struct {
		Status string `json:"status"`
	}
	if err := c.do(ctx, "health", http.MethodGet, "/health", nil, &response); err != nil {
		return err
	}
	if !strings.EqualFold(response.Status, "available") && response.Status != "" {
		return Errorf(ErrUnavailable, "health", "search backend status %q", response.Status)
	}
	return nil
}

// do executes one HTTP request and maps failures onto the typed error kinds.
// Response bodies are never copied into errors to avoid leaking document data.
func (c *Client) do(ctx context.Context, op, method, path string, body any, out any) error {
	ctx, span := c.tracer.Start(ctx, "meilisearch."+op,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("meilisearch.operation", op)),
	)
	defer span.End()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			span.SetStatus(codes.Error, "encode request")
			return (&Error{Kind: ErrInvalid, Op: op, Err: errors.New("encode request body")})
		}
		reader = bytes.NewReader(encoded)
	}
	requestCtx := ctx
	cancel := func() {}
	if c.timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, method, c.baseURL+path, reader)
	if err != nil {
		span.SetStatus(codes.Error, "build request")
		return Wrap(ErrInvalid, op, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("User-Agent", "ontoaxis-search/1")
	if requestID := observability.RequestID(ctx); requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	// W3C traceparent (and baggage) propagation from the active context.
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	response, err := c.httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "request failed")
		return Wrap(ErrUnavailable, op, fmt.Errorf("request failed: %w", err))
	}
	defer func() { _ = response.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if readErr != nil {
		span.RecordError(readErr)
		span.SetStatus(codes.Error, "read response failed")
		return Wrap(ErrUnavailable, op, fmt.Errorf("read response failed: %w", readErr))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return c.statusError(op, response.StatusCode, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "decode response failed")
			return Wrap(ErrUnavailable, op, fmt.Errorf("decode response failed: %w", err))
		}
	}
	return nil
}

func (c *Client) statusError(op string, status int, raw []byte) error {
	var apiErr apiErrorBody
	_ = json.Unmarshal(raw, &apiErr)
	detail := fmt.Sprintf("status %d", status)
	if apiErr.Code != "" {
		detail += " code " + apiErr.Code
	}
	switch status {
	case http.StatusNotFound:
		return &Error{Kind: ErrNotFound, Op: op, Err: errors.New(detail)}
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return &Error{Kind: ErrInvalid, Op: op, Err: errors.New(detail)}
	default:
		return &Error{Kind: ErrUnavailable, Op: op, Err: errors.New(detail)}
	}
}
