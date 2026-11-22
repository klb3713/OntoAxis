package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

// approvalService is the narrow approval surface the API needs. It is kept
// separate from service.Service so the HTTP layer can be built and tested
// before the execution engine lands.
type approvalService interface {
	ListApprovalTasks(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]domain.ApprovalTask, int64, error)
	GetApprovalTask(ctx context.Context, tenantID, id uuid.UUID) (domain.ApprovalTask, error)
	DecideApprovalTask(ctx context.Context, tenantID, id uuid.UUID, approved bool, actorID uuid.UUID, comment string) (domain.ApprovalTask, error)
}

type approvalResponse struct {
	ID          string  `json:"id"`
	TenantID    string  `json:"tenant_id"`
	ExecutionID string  `json:"execution_id"`
	StepIndex   int     `json:"step_index"`
	Status      string  `json:"status"`
	RequestedAt string  `json:"requested_at"`
	DeadlineAt  string  `json:"deadline_at"`
	DecidedAt   *string `json:"decided_at,omitempty"`
	DecidedBy   *string `json:"decided_by,omitempty"`
	Comment     string  `json:"comment,omitempty"`
	ReasonCode  string  `json:"reason_code,omitempty"`
}

func (h *Handler) RegisterApprovalRoutes(r chi.Router) {
	r.Get("/", h.listApprovals)
	r.Get("/{id}", h.getApproval)
	r.Post("/{id}/approve", h.decideApproval(true))
	r.Post("/{id}/reject", h.decideApproval(false))
}

func (h *Handler) approvalSvc(w http.ResponseWriter, r *http.Request) (approvalService, bool) {
	svc, ok := h.svc.(approvalService)
	if !ok {
		writeError(w, r, httperr.NotImplemented("approvals are not available"))
		return nil, false
	}
	return svc, true
}

func (h *Handler) listApprovals(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	svc, ok := h.approvalSvc(w, r)
	if !ok {
		return
	}
	if !h.authorize(w, r, actionExecutionRead, tenantFact(tenantID)) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch status {
	case "", "all", domain.ApprovalStatePending, domain.ApprovalStateApproved, domain.ApprovalStateRejected, domain.ApprovalStateExpired:
	default:
		writeError(w, r, httperr.BadRequest("invalid approval status"))
		return
	}
	limit, offset := page(r)
	items, total, err := svc.ListApprovalTasks(r.Context(), tenantID, status, limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	out := make([]approvalResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toApproval(item))
	}
	writeJSON(w, http.StatusOK, pageResponse[approvalResponse]{Items: out, Total: total})
}

func (h *Handler) getApproval(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	svc, ok := h.approvalSvc(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	task, err := svc.GetApprovalTask(r.Context(), tenantID, id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if !sameTenant(tenantID, task.TenantID) {
		writeNotFound(w, r)
		return
	}
	if !h.authorize(w, r, actionExecutionRead, h.approvalFact(r, tenantID, task)) {
		return
	}
	writeJSON(w, http.StatusOK, toApproval(task))
}

func (h *Handler) decideApproval(approved bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := "reject"
		if approved {
			op = "approve"
		}
		ctx, span := actionAPISpan(r.Context(), op)
		defer span.End()
		r = r.WithContext(ctx)
		tenantID, ok := tenant(w, r)
		if !ok {
			return
		}
		svc, ok := h.approvalSvc(w, r)
		if !ok {
			return
		}
		if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
			writeError(w, r, httperr.BadRequest("Idempotency-Key header is required"))
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, httperr.BadRequest("id must be a UUID"))
			return
		}
		var req struct {
			Comment string `json:"comment"`
		}
		if r.ContentLength > 0 && !decode(w, r, &req) {
			return
		}
		task, err := svc.GetApprovalTask(r.Context(), tenantID, id)
		if err != nil {
			h.writeServiceError(w, r, err)
			return
		}
		if !sameTenant(tenantID, task.TenantID) {
			writeNotFound(w, r)
			return
		}
		if !h.authorize(w, r, actionExecutionApprove, h.approvalFact(r, tenantID, task)) {
			return
		}
		actorID := uuid.Nil
		if tc, ok := authn.FromContext(r.Context()); ok {
			actorID = tc.UserID
		}
		decision := "rejected"
		if approved {
			decision = "approved"
		}
		if !h.auditOrFail(w, r, "action.approval.decided", map[string]any{
			"tenant_id": tenantID.String(), "approval_id": task.ID.String(),
			"execution_id": task.ExecutionID.String(), "decision": decision, "actor_id": actorID.String(),
		}) {
			return
		}
		updated, err := svc.DecideApprovalTask(r.Context(), tenantID, id, approved, actorID, req.Comment)
		if err != nil {
			h.writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toApproval(updated))
	}
}

func (h *Handler) approvalFact(r *http.Request, tenantID uuid.UUID, task domain.ApprovalTask) authz.ResourceFact {
	// The approval inherits the execution's action-type ancestor so the engine
	// sees the same relationship chain as execution reads.
	if exec, err := h.svc.GetExecution(r.Context(), tenantID, task.ExecutionID); err == nil && sameTenant(tenantID, exec.TenantID) {
		return executionFact(exec)
	}
	return authz.ResourceFact{Type: "action_execution", ID: task.ExecutionID.String(), TenantID: tenantID.String()}
}

func toApproval(t domain.ApprovalTask) approvalResponse {
	out := approvalResponse{
		ID: t.ID.String(), TenantID: t.TenantID.String(), ExecutionID: t.ExecutionID.String(),
		StepIndex: t.StepIndex, Status: t.Status,
		RequestedAt: t.RequestedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		DeadlineAt:  t.DeadlineAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Comment:     t.Comment, ReasonCode: t.ReasonCode,
	}
	if t.DecidedAt != nil {
		ts := t.DecidedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.DecidedAt = &ts
	}
	if t.DecidedBy != nil {
		id := t.DecidedBy.String()
		out.DecidedBy = &id
	}
	return out
}
