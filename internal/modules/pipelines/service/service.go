// Package service implements the pipeline authoring use cases. It depends only
// on domain types and on the Repository/Kestra/DatasetResolver seams, so tests
// can use in-memory implementations without Postgres or Kestra.
package service

import (
	"context"
	"encoding/json"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
)

// Service is the exported Pipeline-facing service interface for this module
// (ARCHITECTURE 3.4.1). HTTP and future in-process callers depend on it.
type Service interface {
	CreatePipeline(ctx context.Context, request CreatePipelineRequest) (domain.Pipeline, error)
	GetPipeline(ctx context.Context, tenantID, pipelineID string) (domain.Pipeline, error)
	ListPipelines(ctx context.Context, request ListPipelinesRequest) (PipelinePage, error)
	UpdatePipeline(ctx context.Context, request UpdatePipelineRequest) (domain.Pipeline, error)
	DeletePipeline(ctx context.Context, tenantID, pipelineID string) error
	PublishPipeline(ctx context.Context, request PublishPipelineRequest) (domain.Pipeline, domain.PipelineVersion, error)
	TriggerPipelineRun(ctx context.Context, request TriggerPipelineRunRequest) (domain.PipelineRun, error)
	GetPipelineRun(ctx context.Context, request GetPipelineRunRequest) (domain.PipelineRun, error)
	ListPipelineRuns(ctx context.Context, request ListPipelineRunsRequest) (PipelineRunPage, error)
	LookupIdempotency(ctx context.Context, tenantID, operation, keyHash string) (IdempotencyRecord, bool, error)
	SaveIdempotency(ctx context.Context, record IdempotencyRecord) (bool, error)
}

// IdempotencyRecord is a replayable POST response keyed by tenant/operation/key.
type IdempotencyRecord struct {
	TenantID       string
	Operation      string
	KeyHash        string
	RequestHash    string
	ResponseStatus int
	ResponseBody   []byte
	ExpiresAt      time.Time
}

// ScheduleService is the exported Schedule-facing service interface
// (ARCHITECTURE 3.4.1).
type ScheduleService interface {
	CreateSchedule(ctx context.Context, request CreateScheduleRequest) (domain.Schedule, error)
	GetSchedule(ctx context.Context, tenantID, scheduleID string) (domain.Schedule, error)
	ListSchedules(ctx context.Context, request ListSchedulesRequest) (SchedulePage, error)
	UpdateSchedule(ctx context.Context, request UpdateScheduleRequest) (domain.Schedule, error)
	DeleteSchedule(ctx context.Context, tenantID, scheduleID string) error
	PauseSchedule(ctx context.Context, tenantID, scheduleID string) (domain.Schedule, error)
	ResumeSchedule(ctx context.Context, tenantID, scheduleID string) (domain.Schedule, error)
}

// Repository is the persistence seam owned by this module. Implementations
// must filter every read by tenant_id and must never return soft-deleted
// pipelines.
type Repository interface {
	Ping(ctx context.Context) error
	CreatePipeline(ctx context.Context, pipeline domain.Pipeline) (domain.Pipeline, error)
	GetPipeline(ctx context.Context, tenantID, pipelineID string) (domain.Pipeline, error)
	GetPublishedVersion(ctx context.Context, tenantID, pipelineID string, version int) (domain.PipelineVersion, error)
	ListPipelines(ctx context.Context, filter PipelineFilter) ([]domain.Pipeline, int, error)
	UpdatePipeline(ctx context.Context, pipeline domain.Pipeline) (domain.Pipeline, error)
	DeletePipeline(ctx context.Context, tenantID, pipelineID string, deletedAt time.Time) error
	PublishPipeline(ctx context.Context, pipeline domain.Pipeline, version domain.PipelineVersion) (domain.Pipeline, domain.PipelineVersion, error)
	CreateRun(ctx context.Context, run domain.PipelineRun) (domain.PipelineRun, error)
	GetRun(ctx context.Context, tenantID, pipelineID, runID string) (domain.PipelineRun, error)
	ListRuns(ctx context.Context, filter RunFilter) ([]domain.PipelineRun, int, error)
	UpdateRunConditional(ctx context.Context, run domain.PipelineRun) (domain.PipelineRun, error)
	CreateRunInputs(ctx context.Context, inputs []domain.PipelineRunInput) error
	CreateRunOutputs(ctx context.Context, outputs []domain.PipelineRunOutput) error
	LookupIdempotency(ctx context.Context, tenantID, operation, keyHash string) (IdempotencyRecord, bool, error)
	SaveIdempotency(ctx context.Context, record IdempotencyRecord) (bool, error)
	MarkSucceededWithOutputs(ctx context.Context, run domain.PipelineRun, outputs []domain.PipelineRunOutput) (domain.PipelineRun, error)
	CreateSchedule(ctx context.Context, schedule domain.Schedule) (domain.Schedule, error)
	GetSchedule(ctx context.Context, tenantID, scheduleID string) (domain.Schedule, error)
	ListSchedules(ctx context.Context, filter ScheduleFilter) ([]domain.Schedule, int, error)
	UpdateSchedule(ctx context.Context, schedule domain.Schedule) (domain.Schedule, error)
	DeleteSchedule(ctx context.Context, tenantID, scheduleID string) error
}

// DatasetResolver is the consumer-side view of the datasets module. The
// datasets owner supplies an adapter; this module never reads dataset tables.
type DatasetResolver interface {
	DatasetExists(ctx context.Context, tenantID, datasetID string) (bool, error)
}

// PipelineFilter is the repository-level list filter.
type PipelineFilter struct {
	TenantID string
	Page     int
	PerPage  int
	Status   domain.PipelineStatus
	Type     domain.PipelineType
}

// RunFilter is the repository-level run list filter.
type RunFilter struct {
	TenantID   string
	PipelineID string
	Page       int
	PerPage    int
	Status     domain.PipelineRunStatus
}

// ScheduleFilter is the repository-level schedule list filter.
type ScheduleFilter struct {
	TenantID   string
	PipelineID string
	Page       int
	PerPage    int
}

// PageInfo is the stable pagination envelope.
type PageInfo struct {
	Page       int
	PerPage    int
	Total      int
	TotalPages int
}

// PipelinePage is one page of Pipeline definitions.
type PipelinePage struct {
	Items []domain.Pipeline
	Page  PageInfo
}

// PipelineRunPage is one page of runs.
type PipelineRunPage struct {
	Items []domain.PipelineRun
	Page  PageInfo
}

// SchedulePage is one page of schedules.
type SchedulePage struct {
	Items []domain.Schedule
	Page  PageInfo
}

// CreatePipelineRequest is the service-level create command.
type CreatePipelineRequest struct {
	TenantID    string
	Name        string
	DisplayName string
	Description string
	Type        domain.PipelineType
	DAG         json.RawMessage
	CreatedBy   string
}

// UpdatePipelineRequest patches the mutable fields. A nil pointer or nil DAG
// leaves the corresponding field unchanged.
type UpdatePipelineRequest struct {
	TenantID    string
	PipelineID  string
	DisplayName *string
	Description *string
	DAG         json.RawMessage
}

// ListPipelinesRequest filters and paginates Pipeline definitions.
type ListPipelinesRequest struct {
	TenantID string
	Page     int
	PerPage  int
	Status   domain.PipelineStatus
	Type     domain.PipelineType
}

// PublishPipelineRequest publishes the current DAG as a new immutable version.
type PublishPipelineRequest struct {
	TenantID    string
	PipelineID  string
	PublishedBy string
}

// TriggerPipelineRunRequest starts one manual run.
type TriggerPipelineRunRequest struct {
	TenantID   string
	PipelineID string
}

// GetPipelineRunRequest identifies one run under a Pipeline.
type GetPipelineRunRequest struct {
	TenantID   string
	PipelineID string
	RunID      string
}

// ListPipelineRunsRequest lists runs of one Pipeline.
type ListPipelineRunsRequest struct {
	TenantID   string
	PipelineID string
	Page       int
	PerPage    int
	Status     domain.PipelineRunStatus
}

// CreateScheduleRequest creates a cron schedule for an existing Pipeline.
type CreateScheduleRequest struct {
	TenantID   string
	PipelineID string
	Cron       string
	Timezone   string
}

// UpdateScheduleRequest patches mutable schedule fields.
type UpdateScheduleRequest struct {
	TenantID   string
	ScheduleID string
	Cron       *string
	Timezone   *string
	Enabled    *bool
}

// ListSchedulesRequest lists schedules, optionally scoped to one Pipeline.
type ListSchedulesRequest struct {
	TenantID   string
	PipelineID string
	Page       int
	PerPage    int
}

type options struct {
	adapter           kestra.Adapter
	datasetResolver   DatasetResolver
	dbtCatalog        string
	environment       string
	reconcileThrottle time.Duration
	recoveryTimeout   time.Duration
	metrics           *observability.Metrics
}

// Option configures the service implementation.
type Option func(*options)

// WithKestraAdapter injects the execution adapter. Without one, publish and
// trigger fail closed with domain.ErrUnavailable.
func WithKestraAdapter(adapter kestra.Adapter) Option {
	return func(o *options) { o.adapter = adapter }
}

// WithDatasetResolver injects the datasets module adapter.
func WithDatasetResolver(resolver DatasetResolver) Option {
	return func(o *options) { o.datasetResolver = resolver }
}

// WithDBTCatalog sets the dbt target catalog used when rendering projects.
func WithDBTCatalog(catalog string) Option {
	return func(o *options) { o.dbtCatalog = catalog }
}

// WithEnvironment sets the deployment environment label attached to flows.
func WithEnvironment(environment string) Option {
	return func(o *options) { o.environment = environment }
}

// WithReconcileThrottle bounds engine polling for run reads (default 2s).
func WithReconcileThrottle(throttle time.Duration) Option {
	return func(o *options) { o.reconcileThrottle = throttle }
}

// WithRecoveryTimeout bounds lost-trigger recovery (default 2m).
func WithRecoveryTimeout(timeout time.Duration) Option {
	return func(o *options) { o.recoveryTimeout = timeout }
}

// WithMetrics injects the platform metrics registry for reconcile counters.
func WithMetrics(metrics *observability.Metrics) Option {
	return func(o *options) { o.metrics = metrics }
}

// Manager implements Service and ScheduleService.
type Manager struct {
	repo              Repository
	adapter           kestra.Adapter
	datasetResolver   DatasetResolver
	dbtCatalog        string
	environment       string
	reconcileThrottle time.Duration
	recoveryTimeout   time.Duration
	metrics           *observability.Metrics
	now               func() time.Time
}

// New constructs the service manager. A nil repository is a programming error
// and is rejected by every method through ErrUnavailable.
func New(repo Repository, opts ...Option) *Manager {
	o := options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	throttle := o.reconcileThrottle
	if throttle <= 0 {
		throttle = 2 * time.Second
	}
	recoveryTimeout := o.recoveryTimeout
	if recoveryTimeout <= 0 {
		recoveryTimeout = 2 * time.Minute
	}
	return &Manager{
		repo:              repo,
		adapter:           o.adapter,
		datasetResolver:   o.datasetResolver,
		dbtCatalog:        o.dbtCatalog,
		environment:       o.environment,
		reconcileThrottle: throttle,
		recoveryTimeout:   recoveryTimeout,
		metrics:           o.metrics,
		now:               time.Now,
	}
}

// WithNow is an internal test seam; production callers should not need it.
func (m *Manager) WithNow(now func() time.Time) *Manager {
	if now != nil {
		m.now = now
	}
	return m
}

// LookupIdempotency returns a replayable response for the tenant/operation/key.
func (m *Manager) LookupIdempotency(ctx context.Context, tenantID, operation, keyHash string) (IdempotencyRecord, bool, error) {
	if m.repo == nil {
		return IdempotencyRecord{}, false, domain.ErrUnavailable
	}
	return m.repo.LookupIdempotency(ctx, tenantID, operation, keyHash)
}

// SaveIdempotency stores a response; false means a concurrent request won.
func (m *Manager) SaveIdempotency(ctx context.Context, record IdempotencyRecord) (bool, error) {
	if m.repo == nil {
		return false, domain.ErrUnavailable
	}
	return m.repo.SaveIdempotency(ctx, record)
}
