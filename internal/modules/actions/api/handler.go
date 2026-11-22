package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/service"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

const maxBody = 1 << 20

// Declared actions from the platform catalog. Handlers only name the action
// and the resource fact; they never inspect roles or policies.
const (
	actionTypeCreate   authz.Action = "action_type:create"
	actionTypeRead     authz.Action = "action_type:read"
	actionTypeUpdate   authz.Action = "action_type:update"
	actionTypeDelete   authz.Action = "action_type:delete"
	actionTypeValidate authz.Action = "action_type:validate"
	actionTypePreview  authz.Action = "action_type:preview"
	actionTypeRun      authz.Action = "action_type:run"

	actionExecutionRead    authz.Action = "action_execution:read"
	actionExecutionCancel  authz.Action = "action_execution:cancel"
	actionExecutionRetry   authz.Action = "action_execution:retry"
	actionExecutionApprove authz.Action = "action_execution:approve"
)

type Handler struct {
	svc   service.Service
	guard authz.DecisionGuard
	audit AuditSink
}

// Option customizes the action handler without changing the service contract.
type Option func(*Handler)

// WithDecisionGuard wires the single authorization decision point. The
// production app must set it; tests that predate authz may omit it.
func WithDecisionGuard(guard authz.DecisionGuard) Option {
	return func(h *Handler) { h.guard = guard }
}

func NewHandler(svc service.Service, opts ...Option) *Handler {
	h := &Handler{svc: svc}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// authorize fails closed on denial and writes the stable HTTP error. A nil
// guard means the handler was constructed without the app wiring (legacy unit
// tests); the production constructor must pass WithDecisionGuard.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, action authz.Action, resource authz.ResourceFact) bool {
	return authz.AuthorizeHTTP(w, r, h.guard, action, resource)
}

// tenantFact is the resource fact for tenant-level actions: the tenant itself.
func tenantFact(tenantID uuid.UUID) authz.ResourceFact {
	id := tenantID.String()
	return authz.ResourceFact{Type: "tenant", ID: id, TenantID: id}
}

// actionTypeFact builds the fact from the tenant-scoped row, so the status is
// never taken from the request body.
func actionTypeFact(item domain.ActionType) authz.ResourceFact {
	return authz.ResourceFact{
		Type:     "action_type",
		ID:       item.ID.String(),
		TenantID: item.TenantID.String(),
		Status:   item.Status,
	}
}

// executionFact carries the action-type parent edge so the relationship check
// can walk action_execution -> action_type -> tenant.
func executionFact(item domain.Execution) authz.ResourceFact {
	fact := authz.ResourceFact{
		Type:     "action_execution",
		ID:       item.ID.String(),
		TenantID: item.TenantID.String(),
		Status:   item.Status,
	}
	if item.ActionTypeID != uuid.Nil {
		fact.Ancestors = []authz.Ancestor{{Type: "action_type", ID: item.ActionTypeID.String()}}
	}
	return fact
}

// sameTenant enforces the row-tenant invariant before authorization: a row
// from another tenant is indistinguishable from a missing row (404).
func sameTenant(tenantID, rowTenant uuid.UUID) bool { return rowTenant == tenantID }

func writeNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, httperr.NotFound("action not found"))
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/types", func(r chi.Router) {
		r.Post("/", h.createActionType)
		r.Get("/", h.listActionTypes)
		r.Get("/{id}", h.getActionType)
		r.Patch("/{id}", h.updateActionType)
		r.Delete("/{id}", h.deleteActionType)
		r.Post("/{id}/validate", h.validateAction)
		r.Post("/{id}/preview", h.previewAction)
		r.Post("/{id}/run", h.runAction)
	})
	r.Route("/executions", func(r chi.Router) {
		r.Get("/", h.listExecutions)
		r.Get("/{id}", h.getExecution)
		r.Post("/{id}/cancel", h.cancelExecution)
		r.Post("/{id}/retry", h.retryExecution)
	})
	r.Route("/approvals", h.RegisterApprovalRoutes)
}

type actionTypeRequest struct {
	Name         string         `json:"name"`
	DisplayName  string         `json:"display_name"`
	Description  string         `json:"description"`
	ObjectTypeID *string        `json:"object_type_id"`
	Parameters   map[string]any `json:"parameters"`
	Status       string         `json:"status"`
}

type actionTypeResponse struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	Name         string         `json:"name"`
	DisplayName  string         `json:"display_name"`
	Description  string         `json:"description"`
	ObjectTypeID *string        `json:"object_type_id,omitempty"`
	Parameters   map[string]any `json:"parameters"`
	Status       string         `json:"status"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

type executionResponse struct {
	ID                string         `json:"id"`
	TenantID          string         `json:"tenant_id"`
	ActionTypeID      string         `json:"action_type_id"`
	ObjectID          *string        `json:"object_id,omitempty"`
	Status            string         `json:"status"`
	Parameters        map[string]any `json:"parameters"`
	Result            map[string]any `json:"result,omitempty"`
	Error             string         `json:"error,omitempty"`
	Attempt           int            `json:"attempt"`
	CurrentStepIndex  int            `json:"current_step_index"`
	WorkflowID        string         `json:"workflow_id,omitempty"`
	WorkflowRunID     string         `json:"workflow_run_id,omitempty"`
	ApprovalState     string         `json:"approval_state,omitempty"`
	FailureCode       string         `json:"failure_code,omitempty"`
	FailureMessage    string         `json:"failure_message,omitempty"`
	CancelRequestedAt *string        `json:"cancel_requested_at,omitempty"`
	CreatedAt         string         `json:"created_at"`
}

type pageResponse[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
}

func (h *Handler) createActionType(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	var req actionTypeRequest
	if !decode(w, r, &req) {
		return
	}
	var objectTypeID *uuid.UUID
	if req.ObjectTypeID != nil && *req.ObjectTypeID != "" {
		id, err := uuid.Parse(*req.ObjectTypeID)
		if err != nil {
			writeError(w, r, httperr.BadRequest("object_type_id must be a UUID"))
			return
		}
		objectTypeID = &id
	}
	if !h.authorize(w, r, actionTypeCreate, tenantFact(tenantID)) {
		return
	}
	item, err := h.svc.CreateActionType(r.Context(), tenantID, req.Name, req.DisplayName, req.Description, objectTypeID, req.Parameters, req.Status)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toActionType(item))
}

func (h *Handler) listActionTypes(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	limit, offset := page(r)
	if !h.authorize(w, r, actionTypeRead, tenantFact(tenantID)) {
		return
	}
	items, total, err := h.svc.ListActionTypes(r.Context(), tenantID, limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	out := make([]actionTypeResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toActionType(item))
	}
	writeJSON(w, http.StatusOK, pageResponse[actionTypeResponse]{Items: out, Total: total})
}

func (h *Handler) getActionType(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	item, err := h.svc.GetActionType(r.Context(), tenantID, id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if !sameTenant(tenantID, item.TenantID) {
		writeNotFound(w, r)
		return
	}
	if !h.authorize(w, r, actionTypeRead, actionTypeFact(item)) {
		return
	}
	writeJSON(w, http.StatusOK, toActionType(item))
}

func (h *Handler) updateActionType(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	var req actionTypeRequest
	if !decode(w, r, &req) {
		return
	}
	item, err := h.svc.GetActionType(r.Context(), tenantID, id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if !sameTenant(tenantID, item.TenantID) {
		writeNotFound(w, r)
		return
	}
	if !h.authorize(w, r, actionTypeUpdate, actionTypeFact(item)) {
		return
	}
	item, err = h.svc.UpdateActionType(r.Context(), tenantID, id, req.DisplayName, req.Parameters)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toActionType(item))
}

func (h *Handler) deleteActionType(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	item, err := h.svc.GetActionType(r.Context(), tenantID, id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if !sameTenant(tenantID, item.TenantID) {
		writeNotFound(w, r)
		return
	}
	if !h.authorize(w, r, actionTypeDelete, actionTypeFact(item)) {
		return
	}
	if err := h.svc.DeleteActionType(r.Context(), tenantID, id); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type parametersRequest struct {
	Parameters map[string]any `json:"parameters"`
}

func (h *Handler) validateAction(w http.ResponseWriter, r *http.Request) {
	h.parametersAction(w, r, actionTypeValidate, func(tenantID, actionTypeID uuid.UUID, params map[string]any) (any, error) {
		valid, problems, err := h.svc.Validate(r.Context(), tenantID, actionTypeID, params)
		if err != nil {
			return nil, err
		}
		return map[string]any{"valid": valid, "errors": problems}, nil
	})
}

func (h *Handler) previewAction(w http.ResponseWriter, r *http.Request) {
	h.parametersAction(w, r, actionTypePreview, func(tenantID, actionTypeID uuid.UUID, params map[string]any) (any, error) {
		return h.svc.Preview(r.Context(), tenantID, actionTypeID, params)
	})
}

func (h *Handler) runAction(w http.ResponseWriter, r *http.Request) {
	ctx, span := actionAPISpan(r.Context(), "run")
	defer span.End()
	if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
		if len(key) > 255 {
			writeError(w, r, httperr.BadRequest("Idempotency-Key must be at most 255 characters"))
			return
		}
		ctx = service.WithIdempotencyKey(ctx, key)
	}
	h.parametersAction(w, r, actionTypeRun, func(tenantID, actionTypeID uuid.UUID, params map[string]any) (any, error) {
		execution, err := h.svc.Run(ctx, tenantID, actionTypeID, params)
		if err != nil {
			return nil, err
		}
		return toExecution(execution), nil
	})
}

func (h *Handler) parametersAction(w http.ResponseWriter, r *http.Request, action authz.Action, fn func(uuid.UUID, uuid.UUID, map[string]any) (any, error)) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	actionTypeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	var req parametersRequest
	if !decode(w, r, &req) {
		return
	}
	item, err := h.svc.GetActionType(r.Context(), tenantID, actionTypeID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if !sameTenant(tenantID, item.TenantID) {
		writeNotFound(w, r)
		return
	}
	if !h.authorize(w, r, action, actionTypeFact(item)) {
		return
	}
	out, err := fn(tenantID, actionTypeID, req.Parameters)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) listExecutions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	var actionTypeID *uuid.UUID
	if raw := r.URL.Query().Get("action_type_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, r, httperr.BadRequest("action_type_id must be a UUID"))
			return
		}
		actionTypeID = &id
	}
	limit, offset := page(r)
	if actionTypeID != nil {
		parent, err := h.svc.GetActionType(r.Context(), tenantID, *actionTypeID)
		if err != nil {
			h.writeServiceError(w, r, err)
			return
		}
		if !sameTenant(tenantID, parent.TenantID) {
			writeNotFound(w, r)
			return
		}
		if !h.authorize(w, r, actionExecutionRead, actionTypeFact(parent)) {
			return
		}
	} else if !h.authorize(w, r, actionExecutionRead, tenantFact(tenantID)) {
		return
	}
	items, total, err := h.svc.ListExecutions(r.Context(), tenantID, actionTypeID, limit, offset)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	out := make([]executionResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toExecution(item))
	}
	writeJSON(w, http.StatusOK, pageResponse[executionResponse]{Items: out, Total: total})
}

func (h *Handler) getExecution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	item, err := h.svc.GetExecution(r.Context(), tenantID, id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if !sameTenant(tenantID, item.TenantID) {
		writeNotFound(w, r)
		return
	}
	if !h.authorize(w, r, actionExecutionRead, executionFact(item)) {
		return
	}
	writeJSON(w, http.StatusOK, toExecution(item))
}

func (h *Handler) cancelExecution(w http.ResponseWriter, r *http.Request) {
	h.executionAction(w, r, actionExecutionCancel, h.svc.CancelExecution)
}

func (h *Handler) retryExecution(w http.ResponseWriter, r *http.Request) {
	h.executionAction(w, r, actionExecutionRetry, h.svc.RetryExecution)
}

func (h *Handler) executionAction(w http.ResponseWriter, r *http.Request, action authz.Action, fn func(context.Context, uuid.UUID, uuid.UUID) (domain.Execution, error)) {
	ctx, span := actionAPISpan(r.Context(), string(action))
	defer span.End()
	r = r.WithContext(ctx)
	tenantID, ok := tenant(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, httperr.BadRequest("id must be a UUID"))
		return
	}
	loaded, err := h.svc.GetExecution(r.Context(), tenantID, id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if !sameTenant(tenantID, loaded.TenantID) {
		writeNotFound(w, r)
		return
	}
	if !h.authorize(w, r, action, executionFact(loaded)) {
		return
	}
	item, err := fn(r.Context(), tenantID, id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toExecution(item))
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, r, httperr.NotFound("action not found"))
	case errors.Is(err, domain.ErrConflict):
		writeError(w, r, httperr.Conflict("action already exists"))
	case errors.Is(err, domain.ErrInvalidInput):
		writeError(w, r, httperr.BadRequest("invalid action input"))
	default:
		writeError(w, r, httperr.Internal())
	}
}

func tenant(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if tc, ok := authn.FromContext(r.Context()); ok && tc.TenantID != uuid.Nil {
		return tc.TenantID, true
	}
	id, err := uuid.Parse(r.Header.Get("X-Tenant-Id"))
	if err != nil || id == uuid.Nil {
		writeError(w, r, httperr.Unauthorized("tenant context is required"))
		return uuid.Nil, false
	}
	return id, true
}

func page(r *http.Request) (int, int) {
	limit, offset := 50, 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, r, httperr.BadRequest("invalid JSON request body"))
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, r, httperr.BadRequest("request body must contain a single JSON object"))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, e httperr.Error) {
	httperr.Write(w, r, e)
}

func toActionType(a domain.ActionType) actionTypeResponse {
	var objectTypeID *string
	if a.ObjectTypeID != nil {
		raw := a.ObjectTypeID.String()
		objectTypeID = &raw
	}
	return actionTypeResponse{
		ID: a.ID.String(), TenantID: a.TenantID.String(), Name: a.Name, DisplayName: a.DisplayName,
		Description: a.Description, ObjectTypeID: objectTypeID, Parameters: a.Parameters, Status: a.Status,
		CreatedAt: a.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: a.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toExecution(e domain.Execution) executionResponse {
	out := executionResponse{
		ID: e.ID.String(), TenantID: e.TenantID.String(), ActionTypeID: e.ActionTypeID.String(),
		Status: e.Status, Parameters: e.Parameters, Result: e.Result, Error: e.Error,
		Attempt: e.Attempt, CurrentStepIndex: e.CurrentStepIndex,
		WorkflowID: e.WorkflowID, WorkflowRunID: e.WorkflowRunID,
		ApprovalState: e.ApprovalState, FailureCode: e.FailureCode, FailureMessage: e.FailureMessage,
		CreatedAt: e.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if e.ObjectID != nil {
		id := e.ObjectID.String()
		out.ObjectID = &id
	}
	if e.CancelRequestedAt != nil {
		ts := e.CancelRequestedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.CancelRequestedAt = &ts
	}
	return out
}
