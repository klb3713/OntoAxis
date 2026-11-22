// Package kestra defines the boundary between the pipelines module and the
// external Kestra workflow engine. Adapter implementations either record calls
// in memory (tests and explicit local development) or issue bounded, retried
// REST requests against a Kestra instance (HTTPAdapter).
package kestra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ErrNotConfigured is returned when a real adapter is expected but absent.
var ErrNotConfigured = errors.New("kestra adapter is not configured")

// ErrNotFound reports that the engine could not find the referenced resource
// (namespace file, flow or execution). HTTPAdapter maps engine 404 responses to
// this sentinel; callers can use errors.Is to translate it into an
// execution_not_found style reason without copying response bodies around.
var ErrNotFound = errors.New("kestra: engine resource not found")

// Label keys shared by flow definitions and execution runs (design decision
// D4). They are the only attribution mechanism the adapter uses: namespaces are
// tenant-derived and every execution carries the core-side run identifier so a
// lost trigger response can be adopted instead of executed twice.
const (
	labelTenantID        = "ontoaxis.tenant_id"
	labelPipelineID      = "ontoaxis.pipeline_id"
	labelPipelineVersion = "ontoaxis.pipeline_version"
	labelDAGVersion      = "ontoaxis.dag_version"
	labelManagedBy       = "ontoaxis.managed_by"
	labelEnvironment     = "ontoaxis.environment"
	labelRunID           = "ontoaxis.run_id"
	managedByValue       = "ontoaxis-core"
	namespacePrefix      = "oa"
)

// Flow is the provider-neutral representation of one published Pipeline.
// A future adapter converts DAG and name/version into Kestra Flow YAML.
type Flow struct {
	TenantID   string
	PipelineID string
	Name       string
	Version    int
	DAG        json.RawMessage
	// FlowYAML is the deterministic engine Flow document rendered from the DAG.
	// When set, HTTPAdapter registers it verbatim; otherwise it falls back to
	// the JSON DAG payload (test/memory surface).
	FlowYAML []byte
}

// FlowRef identifies one Flow inside the execution engine. Revision is the
// Pipeline version used as the engine-side flow revision (design D4), not the
// engine's own monotonically increasing revision counter.
type FlowRef struct {
	Namespace string
	FlowID    string
	Revision  int
}

// File is one deterministic namespace file (for example a dbt project file).
// Path is slash-separated and relative to the namespace root.
type File struct {
	Path    string
	Content []byte
}

// RunRequest asks the execution provider to start one Pipeline version.
type RunRequest struct {
	TenantID    string
	PipelineID  string
	Version     int
	TriggerType string
	DAG         json.RawMessage
	// RunID is the core-side run identifier. Adapters label the execution with
	// it (labelRunID) so a trigger whose response was lost can be recovered
	// through FindExecutionByRun without creating a second execution.
	RunID string
}

// RunResult is the provider handle for an accepted execution.
type RunResult struct {
	ExecutionID string
	Status      string
}

// ExecutionRef references one engine execution.
type ExecutionRef struct {
	ID string
}

// Execution is the provider-neutral view of one engine execution used for
// read-time reconciliation.
type Execution struct {
	ID        string
	Status    string
	Namespace string
	FlowID    string
	Labels    map[string]string
	Outputs   map[string]string
}

// Adapter is the only contract the service layer knows about. Implementations
// must be safe for concurrent use.
type Adapter interface {
	// RegisterFlow idempotently upserts a Flow for a Pipeline version.
	// Registering the same (namespace, flow id, revision) again is a no-op
	// upsert and MUST NOT create a second Flow.
	RegisterFlow(ctx context.Context, flow Flow) (FlowRef, error)
	// PublishFiles idempotently uploads namespace files. Re-uploading the same
	// path replaces the previous content.
	PublishFiles(ctx context.Context, namespace string, files []File) error
	// DeleteFiles removes namespace files as publish compensation. Deleting a
	// path that no longer exists is not an error.
	DeleteFiles(ctx context.Context, namespace string, paths []string) error
	// TriggerRun starts one execution. Implementations MUST attach the run
	// identifier to the execution labels.
	TriggerRun(ctx context.Context, request RunRequest) (RunResult, error)
	// GetExecution returns the current state of one execution.
	GetExecution(ctx context.Context, ref ExecutionRef) (Execution, error)
	// FindExecutionByRun returns the execution labelled for one run. It returns
	// ErrNotFound (or an error that unwraps to it) when no execution exists.
	FindExecutionByRun(ctx context.Context, tenantID, runID string) (Execution, error)
	// CancelExecution asks the engine to kill one execution.
	CancelExecution(ctx context.Context, ref ExecutionRef) error
}

// MemoryAdapter is a deterministic, process-local skeleton for tests and local
// development. It records registrations, files and run requests but does not
// execute anything. It is not a substitute for a real Kestra instance.
type MemoryAdapter struct {
	mu             sync.Mutex
	flows          map[string]Flow
	files          map[string]map[string][]byte
	executions     map[string]Execution
	executionOrder []string
	runs           []RunRequest
	nextExecution  int

	RegisterError        error
	PublishFilesError    error
	DeleteFilesError     error
	TriggerError         error
	GetExecutionError    error
	FindExecutionError   error
	CancelExecutionError error

	TriggeredCount int
}

// NewMemoryAdapter creates an empty in-memory adapter.
func NewMemoryAdapter() *MemoryAdapter {
	return &MemoryAdapter{
		flows:      make(map[string]Flow),
		files:      make(map[string]map[string][]byte),
		executions: make(map[string]Execution),
	}
}

// RegisterFlow records one published flow keyed by tenant, pipeline and
// version. Re-registering the same key replaces the previous record, matching
// the idempotent upsert semantics of the real adapter.
func (a *MemoryAdapter) RegisterFlow(_ context.Context, flow Flow) (FlowRef, error) {
	if a == nil {
		return FlowRef{}, ErrNotConfigured
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.RegisterError != nil {
		return FlowRef{}, a.RegisterError
	}
	if strings.TrimSpace(flow.TenantID) == "" || strings.TrimSpace(flow.PipelineID) == "" {
		return FlowRef{}, fmt.Errorf("kestra: tenant and pipeline are required")
	}
	a.flows[flowKey(flow.TenantID, flow.PipelineID, flow.Version)] = flow
	return FlowRef{
		Namespace: deriveNamespace(flow.TenantID, flow.PipelineID),
		FlowID:    deriveFlowID(flow.PipelineID),
		Revision:  flow.Version,
	}, nil
}

// PublishFiles records namespace files, replacing matching paths.
func (a *MemoryAdapter) PublishFiles(_ context.Context, namespace string, files []File) error {
	if a == nil {
		return ErrNotConfigured
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.PublishFilesError != nil {
		return a.PublishFilesError
	}
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("kestra: namespace is required")
	}
	stored := a.files[namespace]
	if stored == nil {
		stored = make(map[string][]byte)
		a.files[namespace] = stored
	}
	for _, file := range files {
		if strings.TrimSpace(file.Path) == "" {
			return fmt.Errorf("kestra: file path is required")
		}
		stored[file.Path] = append([]byte(nil), file.Content...)
	}
	return nil
}

// DeleteFiles removes namespace files. Missing paths are ignored.
func (a *MemoryAdapter) DeleteFiles(_ context.Context, namespace string, paths []string) error {
	if a == nil {
		return ErrNotConfigured
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.DeleteFilesError != nil {
		return a.DeleteFilesError
	}
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("kestra: namespace is required")
	}
	for _, filePath := range paths {
		delete(a.files[namespace], filePath)
	}
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
	if strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.PipelineID) == "" {
		return RunResult{}, fmt.Errorf("kestra: tenant and pipeline are required")
	}
	a.runs = append(a.runs, request)
	a.nextExecution++
	a.TriggeredCount++
	executionID := fmt.Sprintf("memory-execution-%d", a.nextExecution)
	execution := Execution{
		ID:        executionID,
		Status:    "queued",
		Namespace: deriveNamespace(request.TenantID, request.PipelineID),
		FlowID:    deriveFlowID(request.PipelineID),
		Labels:    executionLabels(request),
		Outputs:   map[string]string{},
	}
	a.executions[executionID] = execution
	a.executionOrder = append(a.executionOrder, executionID)
	return RunResult{ExecutionID: executionID, Status: execution.Status}, nil
}

// GetExecution returns one recorded execution.
func (a *MemoryAdapter) GetExecution(_ context.Context, ref ExecutionRef) (Execution, error) {
	if a == nil {
		return Execution{}, ErrNotConfigured
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.GetExecutionError != nil {
		return Execution{}, a.GetExecutionError
	}
	if strings.TrimSpace(ref.ID) == "" {
		return Execution{}, fmt.Errorf("kestra: execution id is required")
	}
	execution, ok := a.executions[ref.ID]
	if !ok {
		return Execution{}, fmt.Errorf("%w: execution %s", ErrNotFound, ref.ID)
	}
	return cloneExecution(execution), nil
}

// FindExecutionByRun finds a recorded execution by tenant and run label.
func (a *MemoryAdapter) FindExecutionByRun(_ context.Context, tenantID, runID string) (Execution, error) {
	if a == nil {
		return Execution{}, ErrNotConfigured
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.FindExecutionError != nil {
		return Execution{}, a.FindExecutionError
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(runID) == "" {
		return Execution{}, fmt.Errorf("kestra: tenant and run id are required")
	}
	for _, executionID := range a.executionOrder {
		execution, ok := a.executions[executionID]
		if !ok {
			continue
		}
		if execution.Labels[labelTenantID] != tenantID {
			continue
		}
		if execution.Labels[labelRunID] != runID {
			continue
		}
		return cloneExecution(execution), nil
	}
	return Execution{}, fmt.Errorf("%w: no execution for run %s", ErrNotFound, runID)
}

// CancelExecution records a kill request by moving the execution to the engine's
// KILLED state.
func (a *MemoryAdapter) CancelExecution(_ context.Context, ref ExecutionRef) error {
	if a == nil {
		return ErrNotConfigured
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.CancelExecutionError != nil {
		return a.CancelExecutionError
	}
	if strings.TrimSpace(ref.ID) == "" {
		return fmt.Errorf("kestra: execution id is required")
	}
	execution, ok := a.executions[ref.ID]
	if !ok {
		return fmt.Errorf("%w: execution %s", ErrNotFound, ref.ID)
	}
	execution.Status = "KILLED"
	a.executions[ref.ID] = execution
	return nil
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

// Files returns a copy of the namespace files, sorted by path, for assertions
// in tests. A missing namespace yields no files.
func (a *MemoryAdapter) Files(namespace string) []File {
	a.mu.Lock()
	defer a.mu.Unlock()
	stored := a.files[namespace]
	out := make([]File, 0, len(stored))
	for filePath, content := range stored {
		out = append(out, File{Path: filePath, Content: append([]byte(nil), content...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Executions returns a copy of all recorded executions in creation order.
func (a *MemoryAdapter) Executions() []Execution {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Execution, 0, len(a.executionOrder))
	for _, executionID := range a.executionOrder {
		if execution, ok := a.executions[executionID]; ok {
			out = append(out, cloneExecution(execution))
		}
	}
	return out
}

// Runs returns a copy of all run requests, for assertions in tests.
func (a *MemoryAdapter) Runs() []RunRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]RunRequest(nil), a.runs...)
}

func cloneExecution(execution Execution) Execution {
	clone := execution
	clone.Labels = make(map[string]string, len(execution.Labels))
	for key, value := range execution.Labels {
		clone.Labels[key] = value
	}
	clone.Outputs = make(map[string]string, len(execution.Outputs))
	for key, value := range execution.Outputs {
		clone.Outputs[key] = value
	}
	return clone
}

func executionLabels(request RunRequest) map[string]string {
	labels := map[string]string{
		labelTenantID:        request.TenantID,
		labelPipelineID:      request.PipelineID,
		labelPipelineVersion: strconv.Itoa(request.Version),
		labelDAGVersion:      dagVersion(request.DAG),
		labelManagedBy:       managedByValue,
	}
	if request.RunID != "" {
		labels[labelRunID] = request.RunID
	}
	return labels
}

// dagVersion reads the optional dag_version field from an authoring DAG. The
// adapter is a transport seam: it only labels the definition and leaves strict
// validation to the publish-time DAG validator (design D2).
func dagVersion(dag json.RawMessage) string {
	if len(dag) == 0 {
		return "1"
	}
	var envelope struct {
		DAGVersion json.Number `json:"dag_version"`
	}
	if err := json.Unmarshal(dag, &envelope); err != nil || envelope.DAGVersion == "" {
		return "1"
	}
	return envelope.DAGVersion.String()
}

func flowKey(tenantID, pipelineID string, version int) string {
	return fmt.Sprintf("%s/%s/%d", tenantID, pipelineID, version)
}

// deriveNamespace returns the deterministic, non-guessable Kestra namespace
// oa_<tenant_hex32>.<pipeline_hex32> (design D4).
func deriveNamespace(tenantID, pipelineID string) string {
	return namespacePrefix + "_" + compactID(tenantID) + "." + compactID(pipelineID)
}

// deriveFlowID returns the deterministic Kestra flow id (<pipeline_hex32>).
func deriveFlowID(pipelineID string) string {
	return compactID(pipelineID)
}

func compactID(id string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(id), "-", ""))
}

// SetExecutionStatus mutates a recorded execution for tests and local
// reconciliation exercises. It reports false when the execution is unknown.
func (a *MemoryAdapter) SetExecutionStatus(executionID, status string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	execution, ok := a.executions[executionID]
	if !ok {
		return false
	}
	execution.Status = status
	a.executions[executionID] = execution
	return true
}
