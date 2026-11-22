package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/platform/authz/authtest"
)

type fakeApprovalService struct {
	fakeService
	task     domain.ApprovalTask
	exec     domain.Execution
	decided  bool
	decision bool
	actor    uuid.UUID
	comment  string
}

func (f *fakeApprovalService) ListApprovalTasks(context.Context, uuid.UUID, string, int, int) ([]domain.ApprovalTask, int64, error) {
	return []domain.ApprovalTask{f.task}, 1, nil
}

func (f *fakeApprovalService) GetApprovalTask(context.Context, uuid.UUID, uuid.UUID) (domain.ApprovalTask, error) {
	return f.task, nil
}

func (f *fakeApprovalService) GetExecution(context.Context, uuid.UUID, uuid.UUID) (domain.Execution, error) {
	return f.exec, nil
}

func (f *fakeApprovalService) DecideApprovalTask(_ context.Context, _ uuid.UUID, _ uuid.UUID, approved bool, actorID uuid.UUID, comment string) (domain.ApprovalTask, error) {
	f.decided = true
	f.decision = approved
	f.actor = actorID
	f.comment = comment
	now := time.Now().UTC()
	status := domain.ApprovalStateRejected
	if approved {
		status = domain.ApprovalStateApproved
	}
	f.task.Status = status
	f.task.DecidedAt = &now
	f.task.DecidedBy = &actorID
	f.task.Comment = comment
	return f.task, nil
}

func newApprovalFixture() (*fakeApprovalService, *chi.Mux) {
	tenantID := uuid.New()
	execID := uuid.New()
	task := domain.ApprovalTask{
		ID: uuid.New(), TenantID: tenantID, ExecutionID: execID, StepIndex: 1,
		Status: domain.ApprovalStatePending, RequestedAt: time.Now().UTC(), DeadlineAt: time.Now().UTC().Add(30 * time.Minute),
	}
	svc := &fakeApprovalService{
		task: task,
		exec: domain.Execution{ID: execID, TenantID: tenantID, ActionTypeID: uuid.New(), Status: domain.ExecutionStatusWaitingApproval},
	}
	r := chi.NewRouter()
	r.Route("/api/v1/actions", NewHandler(svc).Register)
	return svc, r
}

func TestApprovalListAndDecisionContract(t *testing.T) {
	svc, r := newApprovalFixture()
	tenant := svc.task.TenantID.String()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/actions/approvals?status=pending", nil)
	req.Header.Set("X-Tenant-Id", tenant)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 1 || list.Items[0]["status"] != domain.ApprovalStatePending {
		t.Fatalf("list = %#v", list)
	}

	body := `{"comment":"looks good"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/actions/approvals/"+svc.task.ID.String()+"/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-Id", tenant)
	req.Header.Set("Idempotency-Key", "idem-1")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !svc.decided || !svc.decision || svc.comment != "looks good" {
		t.Fatalf("decision not recorded: %#v", svc)
	}
}

func TestApprovalDecisionRequiresIdempotencyKey(t *testing.T) {
	svc, r := newApprovalFixture()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions/approvals/"+svc.task.ID.String()+"/reject", strings.NewReader(`{}`))
	req.Header.Set("X-Tenant-Id", svc.task.TenantID.String())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if svc.decided {
		t.Fatal("decision must not be recorded without idempotency key")
	}
}

func TestApprovalListRejectsUnknownStatus(t *testing.T) {
	svc, r := newApprovalFixture()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/actions/approvals?status=bogus", nil)
	req.Header.Set("X-Tenant-Id", svc.task.TenantID.String())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

type failingAuditSink struct{}

func (failingAuditSink) Emit(context.Context, string, map[string]any) error {
	return errors.New("audit unavailable")
}

func TestApprovalDecisionFailsClosedWhenAuditFails(t *testing.T) {
	tenantID := uuid.New()
	execID := uuid.New()
	task := domain.ApprovalTask{
		ID: uuid.New(), TenantID: tenantID, ExecutionID: execID, StepIndex: 1,
		Status: domain.ApprovalStatePending, RequestedAt: time.Now().UTC(), DeadlineAt: time.Now().UTC().Add(30 * time.Minute),
	}
	svc := &fakeApprovalService{
		task: task,
		exec: domain.Execution{ID: execID, TenantID: tenantID, ActionTypeID: uuid.New(), Status: domain.ExecutionStatusWaitingApproval},
	}
	r := chi.NewRouter()
	r.Route("/api/v1/actions", NewHandler(svc, WithDecisionGuard(authtest.Guard()), WithAuditSink(failingAuditSink{})).Register)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions/approvals/"+task.ID.String()+"/approve", strings.NewReader(`{}`))
	req.Header.Set("X-Tenant-Id", tenantID.String())
	req.Header.Set("Idempotency-Key", "idem-audit")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if svc.decided {
		t.Fatal("approval decision side effect fired despite audit failure")
	}
}
