// Package temporal implements the Temporal workflow engine adapter, the
// deterministic ActionExecutionWorkflow and the activities that persist
// execution state through the RuntimeStore port.
package temporal

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/actions/service"
)

// Environment variables read by LoadConfigFromEnv.
const (
	EnvExecutionMode      = "ONTOAXIS_ACTIONS_EXECUTION_MODE"
	EnvHostPort           = "ONTOAXIS_TEMPORAL_HOST_PORT"
	EnvNamespace          = "ONTOAXIS_TEMPORAL_NAMESPACE"
	EnvTaskQueue          = "ONTOAXIS_TEMPORAL_TASK_QUEUE"
	EnvApprovalTimeout    = "ONTOAXIS_ACTIONS_APPROVAL_TIMEOUT"
	EnvWorkflowRunTimeout = "ONTOAXIS_ACTIONS_WORKFLOW_RUN_TIMEOUT"
)

// Reviewed defaults for the Temporal execution engine.
const (
	DefaultNamespace          = "ontoaxis"
	DefaultTaskQueue          = "ontoaxis-actions"
	DefaultApprovalTimeout    = 30 * time.Minute
	DefaultWorkflowRunTimeout = 5 * time.Minute
)

// Config is the reviewed Temporal execution engine configuration.
type Config struct {
	HostPort  string
	Namespace string
	TaskQueue string
	Mode      service.ExecutionMode
	// ApprovalTimeout bounds how long a workflow waits for a human approval
	// signal when the execution definition does not declare one.
	ApprovalTimeout time.Duration
	// WorkflowRunTimeout bounds one engine-side workflow run.
	WorkflowRunTimeout time.Duration
}

// LoadConfigFromEnv reads the execution engine configuration from the process
// environment.
func LoadConfigFromEnv() (Config, error) {
	return LoadConfig(os.Getenv)
}

// LoadConfig builds the configuration from a lookup function. It fails closed:
// the temporal mode requires a non-empty host and port, and unknown modes are
// rejected so a missing deployment value never degrades into silent noop.
func LoadConfig(getenv func(string) string) (Config, error) {
	rawMode := strings.TrimSpace(getenv(EnvExecutionMode))
	if rawMode == "" {
		rawMode = string(service.ExecutionModeTemporal)
	}
	mode, err := service.ParseExecutionMode(rawMode)
	if err != nil {
		return Config{}, fmt.Errorf("%w: %v", service.ErrEngineUnavailable, err)
	}
	cfg := Config{
		HostPort:           strings.TrimSpace(getenv(EnvHostPort)),
		Namespace:          envOrDefault(getenv, EnvNamespace, DefaultNamespace),
		TaskQueue:          envOrDefault(getenv, EnvTaskQueue, DefaultTaskQueue),
		Mode:               mode,
		ApprovalTimeout:    DefaultApprovalTimeout,
		WorkflowRunTimeout: DefaultWorkflowRunTimeout,
	}
	if raw := strings.TrimSpace(getenv(EnvApprovalTimeout)); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s %q: %w", EnvApprovalTimeout, raw, err)
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("invalid %s %q: must be positive", EnvApprovalTimeout, raw)
		}
		cfg.ApprovalTimeout = timeout
	}
	if raw := strings.TrimSpace(getenv(EnvWorkflowRunTimeout)); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s %q: %w", EnvWorkflowRunTimeout, raw, err)
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("invalid %s %q: must be positive", EnvWorkflowRunTimeout, raw)
		}
		cfg.WorkflowRunTimeout = timeout
	}
	if mode == service.ExecutionModeTemporal {
		if cfg.HostPort == "" {
			return Config{}, fmt.Errorf("%w: %s is required when %s=%s", service.ErrEngineUnavailable, EnvHostPort, EnvExecutionMode, service.ExecutionModeTemporal)
		}
		if cfg.Namespace == "" || cfg.TaskQueue == "" {
			return Config{}, fmt.Errorf("%w: namespace and task queue are required when %s=%s", service.ErrEngineUnavailable, EnvExecutionMode, service.ExecutionModeTemporal)
		}
	}
	return cfg, nil
}

func envOrDefault(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}
