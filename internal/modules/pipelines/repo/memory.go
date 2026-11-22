// Package repo contains persistence implementations for the pipelines module.
package repo

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
)

// MemoryStore is a deterministic fake Repository for unit and HTTP tests. It
// enforces the same tenant, uniqueness and soft-delete rules as PostgresStore
// but keeps everything in process memory.
type MemoryStore struct {
	mu        sync.RWMutex
	pipelines map[string]domain.Pipeline
	versions  map[string][]domain.PipelineVersion
	runs      map[string]domain.PipelineRun
	schedules map[string]domain.Schedule
}

// NewMemoryStore creates an empty in-memory repository.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		pipelines: make(map[string]domain.Pipeline),
		versions:  make(map[string][]domain.PipelineVersion),
		runs:      make(map[string]domain.PipelineRun),
		schedules: make(map[string]domain.Schedule),
	}
}

// Ping always succeeds for the in-memory store.
func (s *MemoryStore) Ping(context.Context) error { return nil }

// Versions returns the immutable published snapshots for one Pipeline. It is a
// test and local-integration inspection helper, not part of the Repository
// contract.
func (s *MemoryStore) Versions(pipelineID string) []domain.PipelineVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stored := s.versions[pipelineID]
	out := make([]domain.PipelineVersion, 0, len(stored))
	for _, version := range stored {
		out = append(out, cloneVersion(version))
	}
	return out
}

// CreatePipeline stores a new Pipeline, rejecting duplicate tenant/name pairs.
func (s *MemoryStore) CreatePipeline(_ context.Context, pipeline domain.Pipeline) (domain.Pipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.pipelines {
		if existing.TenantID == pipeline.TenantID && existing.Name == pipeline.Name {
			return domain.Pipeline{}, fmt.Errorf("%w: pipeline name %q already exists", domain.ErrAlreadyExists, pipeline.Name)
		}
	}
	s.pipelines[pipeline.ID] = clonePipeline(pipeline)
	return clonePipeline(pipeline), nil
}

// GetPipeline returns a non-deleted Pipeline scoped to tenant.
func (s *MemoryStore) GetPipeline(_ context.Context, tenantID, pipelineID string) (domain.Pipeline, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pipeline, ok := s.pipelines[pipelineID]
	if !ok || pipeline.TenantID != tenantID || pipeline.DeletedAt != nil {
		return domain.Pipeline{}, fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, pipelineID)
	}
	return clonePipeline(pipeline), nil
}

// ListPipelines returns a page sorted by created_at DESC.
func (s *MemoryStore) ListPipelines(_ context.Context, filter service.PipelineFilter) ([]domain.Pipeline, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var matched []domain.Pipeline
	for _, pipeline := range s.pipelines {
		if pipeline.TenantID != filter.TenantID || pipeline.DeletedAt != nil {
			continue
		}
		if filter.Status != "" && pipeline.Status != filter.Status {
			continue
		}
		if filter.Type != "" && pipeline.Type != filter.Type {
			continue
		}
		matched = append(matched, clonePipeline(pipeline))
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
			return matched[i].ID < matched[j].ID
		}
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	return paginate(matched, filter.Page, filter.PerPage), len(matched), nil
}

// UpdatePipeline replaces a Pipeline definition.
func (s *MemoryStore) UpdatePipeline(_ context.Context, pipeline domain.Pipeline) (domain.Pipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.pipelines[pipeline.ID]
	if !ok || existing.TenantID != pipeline.TenantID || existing.DeletedAt != nil {
		return domain.Pipeline{}, fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, pipeline.ID)
	}
	s.pipelines[pipeline.ID] = clonePipeline(pipeline)
	return clonePipeline(pipeline), nil
}

// DeletePipeline sets deleted_at and archives the Pipeline.
func (s *MemoryStore) DeletePipeline(_ context.Context, tenantID, pipelineID string, deletedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pipeline, ok := s.pipelines[pipelineID]
	if !ok || pipeline.TenantID != tenantID || pipeline.DeletedAt != nil {
		return fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, pipelineID)
	}
	pipeline.DeletedAt = timePointer(deletedAt)
	pipeline.Status = domain.PipelineStatusArchived
	pipeline.UpdatedAt = deletedAt
	s.pipelines[pipelineID] = pipeline
	return nil
}

// PublishPipeline optimistically replaces the Pipeline and appends an
// immutable version snapshot.
func (s *MemoryStore) PublishPipeline(_ context.Context, pipeline domain.Pipeline, version domain.PipelineVersion) (domain.Pipeline, domain.PipelineVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.pipelines[pipeline.ID]
	if !ok || existing.TenantID != pipeline.TenantID || existing.DeletedAt != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, pipeline.ID)
	}
	if existing.Status == domain.PipelineStatusArchived {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: archived pipeline cannot be published", domain.ErrInvalidState)
	}
	if pipeline.Version != existing.Version+1 {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: pipeline version changed concurrently", domain.ErrAlreadyExists)
	}
	for _, previous := range s.versions[pipeline.ID] {
		if previous.Version == version.Version {
			return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: pipeline version %d already exists", domain.ErrAlreadyExists, version.Version)
		}
	}
	s.pipelines[pipeline.ID] = clonePipeline(pipeline)
	s.versions[pipeline.ID] = append(s.versions[pipeline.ID], cloneVersion(version))
	return clonePipeline(pipeline), cloneVersion(version), nil
}

// CreateRun stores one run.
func (s *MemoryStore) CreateRun(_ context.Context, run domain.PipelineRun) (domain.PipelineRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runs[run.ID]; exists {
		return domain.PipelineRun{}, fmt.Errorf("%w: run %s", domain.ErrAlreadyExists, run.ID)
	}
	s.runs[run.ID] = cloneRun(run)
	return cloneRun(run), nil
}

// GetRun returns a run scoped to tenant and Pipeline.
func (s *MemoryStore) GetRun(_ context.Context, tenantID, pipelineID, runID string) (domain.PipelineRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[runID]
	if !ok || run.TenantID != tenantID || run.PipelineID != pipelineID {
		return domain.PipelineRun{}, fmt.Errorf("%w: run %s", domain.ErrNotFound, runID)
	}
	return cloneRun(run), nil
}

// ListRuns returns a page sorted by started_at DESC, falling back to created_at.
func (s *MemoryStore) ListRuns(_ context.Context, filter service.RunFilter) ([]domain.PipelineRun, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var matched []domain.PipelineRun
	for _, run := range s.runs {
		if run.TenantID != filter.TenantID || run.PipelineID != filter.PipelineID {
			continue
		}
		if filter.Status != "" && run.Status != filter.Status {
			continue
		}
		matched = append(matched, cloneRun(run))
	}
	sort.SliceStable(matched, func(i, j int) bool {
		left := runSortTime(matched[i])
		right := runSortTime(matched[j])
		if left.Equal(right) {
			return matched[i].ID < matched[j].ID
		}
		return left.After(right)
	})
	return paginate(matched, filter.Page, filter.PerPage), len(matched), nil
}

// CreateSchedule stores a schedule, enforcing tenant/pipeline/cron uniqueness.
func (s *MemoryStore) CreateSchedule(_ context.Context, schedule domain.Schedule) (domain.Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.schedules {
		if existing.TenantID == schedule.TenantID && existing.PipelineID == schedule.PipelineID && existing.Cron == schedule.Cron {
			return domain.Schedule{}, fmt.Errorf("%w: schedule cron %q already exists for this pipeline", domain.ErrAlreadyExists, schedule.Cron)
		}
	}
	s.schedules[schedule.ID] = cloneSchedule(schedule)
	return cloneSchedule(schedule), nil
}

// GetSchedule returns one schedule scoped to tenant.
func (s *MemoryStore) GetSchedule(_ context.Context, tenantID, scheduleID string) (domain.Schedule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	schedule, ok := s.schedules[scheduleID]
	if !ok || schedule.TenantID != tenantID {
		return domain.Schedule{}, fmt.Errorf("%w: schedule %s", domain.ErrNotFound, scheduleID)
	}
	return cloneSchedule(schedule), nil
}

// ListSchedules returns a page sorted by created_at DESC.
func (s *MemoryStore) ListSchedules(_ context.Context, filter service.ScheduleFilter) ([]domain.Schedule, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var matched []domain.Schedule
	for _, schedule := range s.schedules {
		if schedule.TenantID != filter.TenantID {
			continue
		}
		if filter.PipelineID != "" && schedule.PipelineID != filter.PipelineID {
			continue
		}
		matched = append(matched, cloneSchedule(schedule))
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
			return matched[i].ID < matched[j].ID
		}
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	return paginate(matched, filter.Page, filter.PerPage), len(matched), nil
}

// UpdateSchedule replaces a schedule after checking cron uniqueness.
func (s *MemoryStore) UpdateSchedule(_ context.Context, schedule domain.Schedule) (domain.Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.schedules[schedule.ID]
	if !ok || existing.TenantID != schedule.TenantID {
		return domain.Schedule{}, fmt.Errorf("%w: schedule %s", domain.ErrNotFound, schedule.ID)
	}
	for _, other := range s.schedules {
		if other.ID == schedule.ID {
			continue
		}
		if other.TenantID == schedule.TenantID && other.PipelineID == schedule.PipelineID && other.Cron == schedule.Cron {
			return domain.Schedule{}, fmt.Errorf("%w: schedule cron %q already exists for this pipeline", domain.ErrAlreadyExists, schedule.Cron)
		}
	}
	s.schedules[schedule.ID] = cloneSchedule(schedule)
	return cloneSchedule(schedule), nil
}

// DeleteSchedule removes a schedule.
func (s *MemoryStore) DeleteSchedule(_ context.Context, tenantID, scheduleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	schedule, ok := s.schedules[scheduleID]
	if !ok || schedule.TenantID != tenantID {
		return fmt.Errorf("%w: schedule %s", domain.ErrNotFound, scheduleID)
	}
	delete(s.schedules, scheduleID)
	return nil
}

func paginate[T any](items []T, page, perPage int) []T {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	start := (page - 1) * perPage
	if start >= len(items) {
		return []T{}
	}
	end := start + perPage
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func runSortTime(run domain.PipelineRun) time.Time {
	if run.StartedAt != nil {
		return *run.StartedAt
	}
	return run.CreatedAt
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func clonePipeline(pipeline domain.Pipeline) domain.Pipeline {
	pipeline.DAG = append([]byte(nil), pipeline.DAG...)
	if pipeline.DeletedAt != nil {
		pipeline.DeletedAt = timePointer(*pipeline.DeletedAt)
	}
	return pipeline
}

func cloneVersion(version domain.PipelineVersion) domain.PipelineVersion {
	version.DAG = append([]byte(nil), version.DAG...)
	return version
}

func cloneRun(run domain.PipelineRun) domain.PipelineRun {
	if run.StartedAt != nil {
		run.StartedAt = timePointer(*run.StartedAt)
	}
	if run.FinishedAt != nil {
		run.FinishedAt = timePointer(*run.FinishedAt)
	}
	return run
}

func cloneSchedule(schedule domain.Schedule) domain.Schedule {
	if schedule.NextRunAt != nil {
		schedule.NextRunAt = timePointer(*schedule.NextRunAt)
	}
	return schedule
}
