// Package kestra defines the boundary between the pipelines module and the
// external Kestra workflow engine. Stage 1 deliberately ships only the
// interface and an in-memory adapter: no network calls, no Flow YAML
// conversion and no real execution are implemented here.
package kestra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// ErrNotConfigured is returned when a real adapter is expected but absent.
var ErrNotConfigured = errors.New("kestra adapter is not configured")

// Flow is the provider-neutral representation of one published Pipeline.
// A future adapter converts DAG and name/version into Kestra Flow YAML.
type Flow struct {
	TenantID   string
	PipelineID string
	Name       string
	Version    int
	DAG        json.RawMessage
}

// RunRequest asks the execution provider to start one Pipeline version.
type RunRequest struct {
	TenantID    string
	PipelineID  string
	Version     int
	TriggerType string
	DAG         json.RawMessage
}

// RunResult is the provider handle for an accepted execution.
type RunResult struct {
	ExecutionID string
	Status      string
}

// Adapter is the only contract the service layer knows about. Implementations
// must be safe for concurrent use.
type Adapter interface {
	RegisterFlow(ctx context.Context, flow Flow) error
	TriggerRun(ctx context.Context, request RunRequest) (RunResult, error)
}

// MemoryAdapter is a deterministic, process-local skeleton for tests and local
// development. It records registrations and run requests but does not execute
// anything. It is not a substitute for a real Kestra instance.
type MemoryAdapter struct {
	mu             sync.Mutex
	flows          map[string]Flow
	runs           []RunRequest
	nextExecution  int
	RegisterError  error
	TriggerError   error
	TriggeredCount int
}

// NewMemoryAdapter creates an empty in-memory adapter.
func NewMemoryAdapter() *MemoryAdapter {
	return &MemoryAdapter{flows: make(map[string]Flow)}
}

// RegisterFlow records one published flow keyed by tenant, pipeline and
// version. Re-registering the same key replaces the previous record.
func (a *MemoryAdapter) RegisterFlow(_ context.Context, flow Flow) error {
	if a == nil {
		return ErrNotConfigured
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.RegisterError != nil {
		return a.RegisterError
	}
	if flow.TenantID == "" || flow.PipelineID == "" {
		return fmt.Errorf("kestra: tenant and pipeline are required")
	}
	a.flows[flowKey(flow.TenantID, flow.PipelineID, flow.Version)] = flow
	return nil
}

// TriggerRun records a run request and returns a synthetic queued execution.
func (a *MemoryAdapter) TriggerRun(_ context.Context, request RunRequest) (RunResult, error) {
	if a == nil {
		return RunResult{}, ErrNotConfigured
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.TriggerError != nil {
		return RunResult{}, a.TriggerError
	}
	if request.TenantID == "" || request.PipelineID == "" {
		return RunResult{}, fmt.Errorf("kestra: tenant and pipeline are required")
	}
	a.runs = append(a.runs, request)
	a.nextExecution++
	a.TriggeredCount++
	return RunResult{
		ExecutionID: fmt.Sprintf("memory-execution-%d", a.nextExecution),
		Status:      "queued",
	}, nil
}

// Flows returns a copy of all registered flows, for assertions in tests.
func (a *MemoryAdapter) Flows() []Flow {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Flow, 0, len(a.flows))
	for _, flow := range a.flows {
		out = append(out, flow)
	}
	return out
}

// Runs returns a copy of all run requests, for assertions in tests.
func (a *MemoryAdapter) Runs() []RunRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]RunRequest(nil), a.runs...)
}

func flowKey(tenantID, pipelineID string, version int) string {
	return fmt.Sprintf("%s/%s/%d", tenantID, pipelineID, version)
}
