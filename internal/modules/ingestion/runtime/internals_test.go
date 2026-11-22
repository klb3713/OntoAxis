package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"
)

func TestParseRuntimeState(t *testing.T) {
	t.Parallel()
	cases := map[string]RuntimeState{
		"running":   StateRunning,
		" RUNNING ": StateRunning,
		"run":       StateRunning,
		"failed":    StateFailed,
		"error":     StateFailed,
		"unhealthy": StateFailed,
		"paused":    StatePaused,
		"stopped":   StateStopped,
		"stop":      StateStopped,
		"missing":   StateMissing,
		"not_found": StateMissing,
		"deleted":   StateMissing,
		"weird":     StateUnknown,
		"":          StateUnknown,
	}
	for input, want := range cases {
		if got := ParseRuntimeState(input); got != want {
			t.Fatalf("ParseRuntimeState(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRuntimeStatusOverall(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status RuntimeStatus
		want   RuntimeState
	}{
		{
			name:   "both running",
			status: RuntimeStatus{Source: StateRunning, Sink: StateRunning},
			want:   StateRunning,
		},
		{
			name:   "ready with unset states",
			status: RuntimeStatus{Ready: true},
			want:   StateRunning,
		},
		{
			name:   "failed wins over missing",
			status: RuntimeStatus{Source: StateFailed, Sink: StateMissing},
			want:   StateFailed,
		},
		{
			name:   "missing wins over stopped",
			status: RuntimeStatus{Source: StateMissing, Sink: StateStopped},
			want:   StateMissing,
		},
		{
			name:   "stopped wins over paused",
			status: RuntimeStatus{Source: StateStopped, Sink: StatePaused},
			want:   StateStopped,
		},
		{
			name:   "paused beats running",
			status: RuntimeStatus{Source: StateRunning, Sink: StatePaused},
			want:   StatePaused,
		},
		{
			name:   "unknown when not ready",
			status: RuntimeStatus{Source: StateRunning, Sink: StateUnknown},
			want:   StateUnknown,
		},
		{
			name:   "empty status is unknown",
			status: RuntimeStatus{},
			want:   StateUnknown,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.status.Overall(); got != tc.want {
				t.Fatalf("Overall() = %q, want %q", got, tc.want)
			}
		})
	}
}

type retryableTestError struct {
	retry bool
}

func (e retryableTestError) Error() string { return "connector runtime error" }

func (e retryableTestError) Retryable() bool { return e.retry }

type timeoutTestError struct{}

func (timeoutTestError) Error() string   { return "i/o timeout" }
func (timeoutTestError) Timeout() bool   { return true }
func (timeoutTestError) Temporary() bool { return true }

func TestDefaultClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{name: "nil", err: nil, want: ClassPermanent},
		{name: "permanent sentinel", err: Permanent(errors.New("bad config")), want: ClassPermanent},
		{name: "retryable sentinel", err: Retryable(errors.New("upstream timeout")), want: ClassRetryable},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: ClassRetryable},
		{name: "wrapped deadline exceeded", err: fmt.Errorf("submit: %w", context.DeadlineExceeded), want: ClassRetryable},
		{name: "net timeout", err: timeoutTestError{}, want: ClassRetryable},
		{name: "typed retryable", err: retryableTestError{retry: true}, want: ClassRetryable},
		{name: "typed permanent", err: retryableTestError{retry: false}, want: ClassPermanent},
		{name: "connection refused", err: fmt.Errorf("dial: %w", syscall.ECONNREFUSED), want: ClassRetryable},
		{name: "connection reset", err: fmt.Errorf("write: %w", syscall.ECONNRESET), want: ClassRetryable},
		{name: "other syscall", err: syscall.ENOENT, want: ClassPermanent},
		{name: "generic error", err: errors.New("HTTP 422 unprocessable entity"), want: ClassPermanent},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DefaultClassify(tc.err); got != tc.want {
				t.Fatalf("DefaultClassify(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestRedactStripsURLCredentials(t *testing.T) {
	t.Parallel()
	got := Redact("connect failed: postgres://user:secret@db:5432/ontoaxis")
	if strings.Contains(got, "user:secret") {
		t.Fatalf("Redact() leaks URL credentials: %q", got)
	}
	if !strings.Contains(got, "postgres://***@db:5432/ontoaxis") {
		t.Fatalf("Redact() = %q, want credentials replaced", got)
	}
}

func TestRedactStripsSecretAssignments(t *testing.T) {
	t.Parallel()
	got := Redact("auth failed password=hunter2 token=abc123 api_key=k-1")
	if strings.Contains(got, "hunter2") || strings.Contains(got, "abc123") || strings.Contains(got, "k-1") {
		t.Fatalf("Redact() leaks credentials: %q", got)
	}
	for _, want := range []string{"password=***", "token=***", "api_key=***"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Redact() = %q, want %q", got, want)
		}
	}
}

func TestRedactStripsJSONSecretValues(t *testing.T) {
	t.Parallel()
	got := Redact(`request rejected {"password": "hunter2"}`)
	if strings.Contains(got, "hunter2") {
		t.Fatalf("Redact() leaks JSON credential: %q", got)
	}
	if !strings.Contains(got, `"password": ***`) {
		t.Fatalf("Redact() = %q, want JSON password redacted", got)
	}
}

func TestRedactKeepsFirstLineAndCapsLength(t *testing.T) {
	t.Parallel()
	multiline := Redact("first line\npassword=hunter2 on the second line")
	if multiline != "first line" {
		t.Fatalf("Redact() = %q, want only first line", multiline)
	}
	long := Redact(strings.Repeat("a", 400))
	if !strings.HasSuffix(long, "…") {
		t.Fatalf("Redact() = %q, want truncation marker", long)
	}
	if count := utf8.RuneCountInString(long); count != maxRedactedLength+1 {
		t.Fatalf("Redact() rune count = %d, want %d", count, maxRedactedLength+1)
	}
	empty := Redact("   ")
	if empty != "" {
		t.Fatalf("Redact(blank) = %q, want empty", empty)
	}
}
