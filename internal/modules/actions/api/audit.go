package api

import (
	"context"
	"net/http"

	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

// AuditSink receives API-side audit projections. Implementations must return
// an error when the event cannot be durably recorded; callers map that error
// to HTTP 503 before performing the business side effect.
type AuditSink interface {
	Emit(ctx context.Context, event string, fields map[string]any) error
}

// WithAuditSink injects the API audit sink. A nil sink preserves legacy
// behavior for tests; production assembly must provide one.
func WithAuditSink(sink AuditSink) Option {
	return func(h *Handler) { h.audit = sink }
}

func (h *Handler) auditOrFail(w http.ResponseWriter, r *http.Request, event string, fields map[string]any) bool {
	if h.audit == nil {
		return true
	}
	if err := h.audit.Emit(r.Context(), event, fields); err != nil {
		httperr.Write(w, r, httperr.Unavailable("audit sink unavailable"))
		return false
	}
	return true
}
