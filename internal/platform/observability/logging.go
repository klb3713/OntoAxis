package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type requestIDKey struct{}

// NewLogger creates a JSON slog logger. Log output intentionally contains no
// request bodies, headers, credentials, or tokens.
func NewLogger(level string) (*slog.Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.ToLower(level))); err != nil {
		return nil, err
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler), nil
}

// WithRequestID stores a request correlation ID in the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the correlation ID from ctx, or an empty string.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
