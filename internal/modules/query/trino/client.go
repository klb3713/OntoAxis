package trino

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/klb3713/ontoaxis/internal/modules/query/domain"
)

// Client is a minimal Trino HTTP protocol client used by the query module.
type Client struct {
	baseURL string
	user    string
	http    *http.Client
}

func New(baseURL, user string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), user: user, http: &http.Client{Timeout: timeout}}
}

type statementResponse struct {
	ID      string          `json:"id"`
	NextURI string          `json:"nextUri"`
	Columns []column        `json:"columns"`
	Data    [][]any         `json:"data"`
	Error   *statementError `json:"error"`
	Stats   json.RawMessage `json:"stats"`
}

type column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type statementError struct {
	Message string `json:"message"`
	Code    int    `json:"errorCode"`
}

// Execute runs a read-only query and follows Trino's nextUri chain.
func (c *Client) Execute(ctx context.Context, sql string, limit int) (domain.QueryResult, error) {
	if c == nil || c.baseURL == "" {
		return domain.QueryResult{}, domain.ErrUnavailable
	}
	body, err := c.start(ctx, sql)
	if err != nil {
		return domain.QueryResult{}, err
	}
	result := domain.QueryResult{}
	seenColumns := false
	for {
		if body.Error != nil {
			return domain.QueryResult{}, fmt.Errorf("trino: %s", body.Error.Message)
		}
		if !seenColumns && len(body.Columns) > 0 {
			result.Columns = make([]string, 0, len(body.Columns))
			for _, col := range body.Columns {
				result.Columns = append(result.Columns, col.Name)
			}
			seenColumns = true
		}
		result.Rows = append(result.Rows, body.Data...)
		if limit > 0 && len(result.Rows) >= limit {
			result.Rows = result.Rows[:limit]
			result.RowCount = int64(len(result.Rows))
			return result, nil
		}
		if body.NextURI == "" {
			result.RowCount = int64(len(result.Rows))
			return result, nil
		}
		body, err = c.next(ctx, body.NextURI)
		if err != nil {
			return domain.QueryResult{}, err
		}
	}
}

// Explain returns Trino's textual plan for a SQL statement.
func (c *Client) Explain(ctx context.Context, sql string) (string, error) {
	result, err := c.Execute(ctx, "EXPLAIN "+sql, 1)
	if err != nil {
		return "", err
	}
	if len(result.Rows) == 0 || len(result.Rows[0]) == 0 {
		return "", fmt.Errorf("trino: empty explain result")
	}
	plan, ok := result.Rows[0][0].(string)
	if !ok {
		return "", fmt.Errorf("trino: unexpected explain result type %T", result.Rows[0][0])
	}
	return plan, nil
}

func (c *Client) start(ctx context.Context, sql string) (statementResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/statement", bytes.NewBufferString(sql))
	if err != nil {
		return statementResponse{}, err
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Trino-User", c.user)
	return c.do(req)
}

func (c *Client) next(ctx context.Context, nextURI string) (statementResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURI, nil)
	if err != nil {
		return statementResponse{}, err
	}
	return c.do(req)
}

func (c *Client) do(req *http.Request) (statementResponse, error) {
	ctx, span := otel.Tracer("ontoaxis/trino").Start(req.Context(), "trino query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "trino"),
			attribute.String("db.operation", "query"),
		),
	)
	defer span.End()
	req = req.WithContext(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	resp, err := c.http.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, "request failed")
		return statementResponse{}, fmt.Errorf("trino request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		span.SetStatus(codes.Error, "non-2xx response")
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return statementResponse{}, fmt.Errorf("trino HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out statementResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return statementResponse{}, fmt.Errorf("decode trino response: %w", err)
	}
	return out, nil
}
