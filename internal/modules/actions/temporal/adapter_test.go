package temporal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	"github.com/klb3713/ontoaxis/internal/modules/actions/service"
)

func envFromMap(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestLoadConfigFailsClosedWhenTemporalHostIsMissing(t *testing.T) {
	_, err := LoadConfig(envFromMap(nil))
	if !errors.Is(err, service.ErrEngineUnavailable) {
		t.Fatalf("err = %v, want service.ErrEngineUnavailable", err)
	}
}

func TestLoadConfigRejectsUnknownExecutionMode(t *testing.T) {
	_, err := LoadConfig(envFromMap(map[string]string{
		EnvExecutionMode: "kubernetes",
	}))
	if !errors.Is(err, service.ErrEngineUnavailable) {
		t.Fatalf("err = %v, want service.ErrEngineUnavailable", err)
	}
}

func TestLoadConfigAppliesReviewedDefaults(t *testing.T) {
	cfg, err := LoadConfig(envFromMap(map[string]string{
		EnvHostPort: "temporal:7233",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Mode != service.ExecutionModeTemporal {
		t.Errorf("mode = %q, want %q", cfg.Mode, service.ExecutionModeTemporal)
	}
	if cfg.HostPort != "temporal:7233" {
		t.Errorf("host port = %q, want temporal:7233", cfg.HostPort)
	}
	if cfg.Namespace != DefaultNamespace {
		t.Errorf("namespace = %q, want %q", cfg.Namespace, DefaultNamespace)
	}
	if cfg.TaskQueue != DefaultTaskQueue {
		t.Errorf("task queue = %q, want %q", cfg.TaskQueue, DefaultTaskQueue)
	}
	if cfg.ApprovalTimeout != DefaultApprovalTimeout {
		t.Errorf("approval timeout = %s, want %s", cfg.ApprovalTimeout, DefaultApprovalTimeout)
	}
	if cfg.WorkflowRunTimeout != DefaultWorkflowRunTimeout {
		t.Errorf("workflow run timeout = %s, want %s", cfg.WorkflowRunTimeout, DefaultWorkflowRunTimeout)
	}
}

func TestLoadConfigNoopModeWorksWithoutHost(t *testing.T) {
	cfg, err := LoadConfig(envFromMap(map[string]string{
		EnvExecutionMode: string(service.ExecutionModeNoop),
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Mode != service.ExecutionModeNoop {
		t.Errorf("mode = %q, want %q", cfg.Mode, service.ExecutionModeNoop)
	}
	if cfg.HostPort != "" {
		t.Errorf("host port = %q, want empty", cfg.HostPort)
	}
}

func TestLoadConfigReadsOverrides(t *testing.T) {
	cfg, err := LoadConfig(envFromMap(map[string]string{
		EnvExecutionMode:      string(service.ExecutionModeTemporal),
		EnvHostPort:           "temporal.internal:7233",
		EnvNamespace:          "custom-ns",
		EnvTaskQueue:          "custom-queue",
		EnvApprovalTimeout:    "90s",
		EnvWorkflowRunTimeout: "10m",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	want := Config{
		HostPort:           "temporal.internal:7233",
		Namespace:          "custom-ns",
		TaskQueue:          "custom-queue",
		Mode:               service.ExecutionModeTemporal,
		ApprovalTimeout:    90 * time.Second,
		WorkflowRunTimeout: 10 * time.Minute,
	}
	if cfg != want {
		t.Fatalf("config = %+v, want %+v", cfg, want)
	}
}

func TestLoadConfigRejectsInvalidDurations(t *testing.T) {
	cases := map[string]map[string]string{
		"approval timeout": {
			EnvHostPort:        "temporal:7233",
			EnvApprovalTimeout: "not-a-duration",
		},
		"negative approval timeout": {
			EnvHostPort:        "temporal:7233",
			EnvApprovalTimeout: "-1m",
		},
		"workflow run timeout": {
			EnvHostPort:           "temporal:7233",
			EnvWorkflowRunTimeout: "0s",
		},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(envFromMap(env)); err == nil {
				t.Fatal("LoadConfig() error = nil, want an error")
			}
		})
	}
}

func TestWorkflowIDIsDeterministic(t *testing.T) {
	executionID := uuid.MustParse("6f2a1f60-3a2a-4a4a-8f6a-1f2b3c4d5e6f")
	if got, want := WorkflowID(executionID), "action-execution-6f2a1f60-3a2a-4a4a-8f6a-1f2b3c4d5e6f"; got != want {
		t.Fatalf("WorkflowID() = %q, want %q", got, want)
	}
}

func TestMapWorkflowStatus(t *testing.T) {
	cases := map[enumspb.WorkflowExecutionStatus]string{
		enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:          domain.ExecutionStatusRunning,
		enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:        domain.ExecutionStatusSucceeded,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:         domain.ExecutionStatusCancelled,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:        domain.ExecutionStatusTimedOut,
		enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:           domain.ExecutionStatusFailed,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:       domain.ExecutionStatusFailed,
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW: domain.ExecutionStatusRunning,
		enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED:      domain.ExecutionStatusFailed,
	}
	for status, want := range cases {
		if got := mapWorkflowStatus(status); got != want {
			t.Errorf("mapWorkflowStatus(%s) = %q, want %q", status, got, want)
		}
	}
}

func TestDialNoopModeDoesNotRequireHost(t *testing.T) {
	client, err := Dial(Config{Mode: service.ExecutionModeNoop})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	if client != nil {
		t.Fatal("Dial() in noop mode returned a client, want nil")
	}
}

func TestDialTemporalModeWithoutHostFailsClosed(t *testing.T) {
	_, err := Dial(Config{Mode: service.ExecutionModeTemporal})
	if !errors.Is(err, service.ErrEngineUnavailable) {
		t.Fatalf("err = %v, want service.ErrEngineUnavailable", err)
	}
}

func TestAdapterWithoutClientFailsClosed(t *testing.T) {
	adapter := NewAdapterWithClient(Config{Mode: service.ExecutionModeTemporal}, nil)
	if err := adapter.Health(context.Background()); !errors.Is(err, service.ErrEngineUnavailable) {
		t.Errorf("Health() err = %v, want service.ErrEngineUnavailable", err)
	}
	_, err := adapter.Start(context.Background(), service.ExecutionInput{ExecutionID: uuid.New()})
	if !errors.Is(err, service.ErrEngineUnavailable) {
		t.Errorf("Start() err = %v, want service.ErrEngineUnavailable", err)
	}
	if err := adapter.Cancel(context.Background(), service.ExecutionRef{WorkflowID: "wf"}, "test"); !errors.Is(err, service.ErrEngineUnavailable) {
		t.Errorf("Cancel() err = %v, want service.ErrEngineUnavailable", err)
	}
	if adapter.Mode() != service.ExecutionModeTemporal {
		t.Errorf("Mode() = %q, want %q", adapter.Mode(), service.ExecutionModeTemporal)
	}
}

func TestNewAdapterRequiresTemporalMode(t *testing.T) {
	_, err := NewAdapter(Config{Mode: service.ExecutionModeNoop})
	if !errors.Is(err, service.ErrEngineUnavailable) {
		t.Fatalf("err = %v, want service.ErrEngineUnavailable", err)
	}
}
