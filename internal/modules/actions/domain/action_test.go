package domain_test

import (
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
)

func TestExecutionStatusTransitionTable(t *testing.T) {
	t.Parallel()
	legal := [][2]string{
		{domain.ExecutionStatusQueued, domain.ExecutionStatusRunning},
		{domain.ExecutionStatusQueued, domain.ExecutionStatusCancelled},
		{domain.ExecutionStatusQueued, domain.ExecutionStatusWaitingApproval},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusSucceeded},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusFailed},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusCancelled},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusTimedOut},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusWaitingApproval},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusCompensating},
		{domain.ExecutionStatusWaitingApproval, domain.ExecutionStatusRunning},
		{domain.ExecutionStatusWaitingApproval, domain.ExecutionStatusCancelled},
		{domain.ExecutionStatusWaitingApproval, domain.ExecutionStatusTimedOut},
		{domain.ExecutionStatusWaitingApproval, domain.ExecutionStatusCompensating},
		{domain.ExecutionStatusCompensating, domain.ExecutionStatusFailed},
		{domain.ExecutionStatusCompensating, domain.ExecutionStatusCancelled},
		{domain.ExecutionStatusCompensating, domain.ExecutionStatusTimedOut},
		{domain.ExecutionStatusCompensating, domain.ExecutionStatusCompensationFailed},
	}
	for _, pair := range legal {
		if !domain.CanTransition(pair[0], pair[1]) {
			t.Errorf("CanTransition(%q, %q) = false, want true", pair[0], pair[1])
		}
	}

	illegal := [][2]string{
		{domain.ExecutionStatusQueued, domain.ExecutionStatusSucceeded},
		{domain.ExecutionStatusQueued, domain.ExecutionStatusFailed},
		{domain.ExecutionStatusQueued, domain.ExecutionStatusTimedOut},
		{domain.ExecutionStatusQueued, domain.ExecutionStatusCompensating},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusQueued},
		{domain.ExecutionStatusWaitingApproval, domain.ExecutionStatusSucceeded},
		{domain.ExecutionStatusWaitingApproval, domain.ExecutionStatusFailed},
		{domain.ExecutionStatusWaitingApproval, domain.ExecutionStatusCompensationFailed},
		{domain.ExecutionStatusWaitingApproval, domain.ExecutionStatusWaitingApproval},
		{domain.ExecutionStatusCompensating, domain.ExecutionStatusRunning},
		{domain.ExecutionStatusCompensating, domain.ExecutionStatusSucceeded},
		{domain.ExecutionStatusCompensating, domain.ExecutionStatusCompensating},
		{domain.ExecutionStatusSucceeded, domain.ExecutionStatusRunning},
		{domain.ExecutionStatusSucceeded, domain.ExecutionStatusCancelled},
		{domain.ExecutionStatusFailed, domain.ExecutionStatusRunning},
		{domain.ExecutionStatusFailed, domain.ExecutionStatusCompensating},
		{domain.ExecutionStatusCancelled, domain.ExecutionStatusRunning},
		{domain.ExecutionStatusTimedOut, domain.ExecutionStatusCompensating},
		{domain.ExecutionStatusCompensationFailed, domain.ExecutionStatusFailed},
		{"", domain.ExecutionStatusRunning},
		{domain.ExecutionStatusRunning, ""},
		{"unknown", domain.ExecutionStatusFailed},
		{domain.ExecutionStatusRunning, "unknown"},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusRunning},
	}
	for _, pair := range illegal {
		if domain.CanTransition(pair[0], pair[1]) {
			t.Errorf("CanTransition(%q, %q) = true, want false", pair[0], pair[1])
		}
	}
}

func TestTerminalStatusesAreImmutable(t *testing.T) {
	t.Parallel()
	terminal := []string{
		domain.ExecutionStatusSucceeded,
		domain.ExecutionStatusFailed,
		domain.ExecutionStatusCancelled,
		domain.ExecutionStatusTimedOut,
		domain.ExecutionStatusCompensationFailed,
	}
	allStatuses := []string{
		domain.ExecutionStatusQueued,
		domain.ExecutionStatusRunning,
		domain.ExecutionStatusWaitingApproval,
		domain.ExecutionStatusCompensating,
		domain.ExecutionStatusSucceeded,
		domain.ExecutionStatusFailed,
		domain.ExecutionStatusCancelled,
		domain.ExecutionStatusTimedOut,
		domain.ExecutionStatusCompensationFailed,
		"",
		"unknown",
	}
	for _, status := range terminal {
		if !domain.IsTerminal(status) {
			t.Errorf("IsTerminal(%q) = false, want true", status)
		}
		for _, next := range allStatuses {
			if domain.CanTransition(status, next) {
				t.Errorf("terminal status %q must not transition to %q", status, next)
			}
		}
	}
	for _, status := range []string{
		domain.ExecutionStatusQueued,
		domain.ExecutionStatusRunning,
		domain.ExecutionStatusWaitingApproval,
		domain.ExecutionStatusCompensating,
		"",
		"unknown",
	} {
		if domain.IsTerminal(status) {
			t.Errorf("IsTerminal(%q) = true, want false", status)
		}
	}
}

func TestExecutionRuntimeStatusConstants(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ got, want string }{
		"waiting_approval":    {domain.ExecutionStatusWaitingApproval, "waiting_approval"},
		"compensating":        {domain.ExecutionStatusCompensating, "compensating"},
		"timed_out":           {domain.ExecutionStatusTimedOut, "timed_out"},
		"compensation_failed": {domain.ExecutionStatusCompensationFailed, "compensation_failed"},
		"approval_none":       {domain.ApprovalStateNone, "none"},
		"approval_pending":    {domain.ApprovalStatePending, "pending"},
		"approval_approved":   {domain.ApprovalStateApproved, "approved"},
		"approval_rejected":   {domain.ApprovalStateRejected, "rejected"},
		"approval_expired":    {domain.ApprovalStateExpired, "expired"},
		"step_pending":        {domain.ExecutionStepStatusPending, "pending"},
		"step_running":        {domain.ExecutionStepStatusRunning, "running"},
		"step_succeeded":      {domain.ExecutionStepStatusSucceeded, "succeeded"},
		"step_failed":         {domain.ExecutionStepStatusFailed, "failed"},
		"step_compensated":    {domain.ExecutionStepStatusCompensated, "compensated"},
		"step_comp_failed":    {domain.ExecutionStepStatusCompensationFailed, "compensation_failed"},
		"task_pending":        {domain.ApprovalTaskStatusPending, "pending"},
		"task_approved":       {domain.ApprovalTaskStatusApproved, "approved"},
		"task_rejected":       {domain.ApprovalTaskStatusRejected, "rejected"},
		"task_expired":        {domain.ApprovalTaskStatusExpired, "expired"},
	}
	for name, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %q, want %q", name, tc.got, tc.want)
		}
	}
}

func TestLegacyExecutionStatusesStillTransition(t *testing.T) {
	t.Parallel()
	// Behavior that existed before the temporal extension must keep working.
	for _, pair := range [][2]string{
		{domain.ExecutionStatusQueued, domain.ExecutionStatusRunning},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusSucceeded},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusFailed},
		{domain.ExecutionStatusRunning, domain.ExecutionStatusCancelled},
		{domain.ExecutionStatusQueued, domain.ExecutionStatusCancelled},
	} {
		if !domain.CanTransition(pair[0], pair[1]) {
			t.Errorf("legacy transition %q -> %q no longer legal", pair[0], pair[1])
		}
	}
	if !domain.IsTerminal(domain.ExecutionStatusSucceeded) ||
		!domain.IsTerminal(domain.ExecutionStatusFailed) ||
		!domain.IsTerminal(domain.ExecutionStatusCancelled) {
		t.Error("legacy terminal statuses must stay terminal")
	}
}

func TestExecutionRuntimeFieldsAreAdditive(t *testing.T) {
	t.Parallel()
	var execution domain.Execution
	if execution.Attempt != 0 || execution.CurrentStepIndex != 0 || execution.ApprovalState != "" ||
		execution.WorkflowID != "" || execution.WorkflowRunID != "" || execution.FailureCode != "" ||
		execution.FailureMessage != "" || execution.ObjectID != nil || execution.CancelRequestedAt != nil ||
		execution.DefinitionSnapshot != nil {
		t.Fatalf("unexpected non-zero execution runtime fields: %+v", execution)
	}
	if execution.Status != "" || execution.StartedAt != nil || execution.FinishedAt != nil {
		t.Fatalf("legacy execution fields changed zero value: %+v", execution)
	}
}
