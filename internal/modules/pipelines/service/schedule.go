package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
)

// CreateSchedule stores a cron schedule for an existing Pipeline.
func (m *Manager) CreateSchedule(ctx context.Context, request CreateScheduleRequest) (domain.Schedule, error) {
	if err := m.requireRepository(); err != nil {
		return domain.Schedule{}, err
	}
	tenantID, err := normalizeTenant(request.TenantID)
	if err != nil {
		return domain.Schedule{}, err
	}
	pipelineID, err := normalizeUUID(request.PipelineID, "pipeline_id")
	if err != nil {
		return domain.Schedule{}, err
	}
	if _, err := m.repo.GetPipeline(ctx, tenantID, pipelineID); err != nil {
		return domain.Schedule{}, err
	}
	cron := strings.TrimSpace(request.Cron)
	timezone, location, err := normalizeScheduleTime(cron, request.Timezone)
	if err != nil {
		return domain.Schedule{}, err
	}
	now := m.now().UTC()
	next, err := domain.ComputeNextRun(cron, location.String(), now)
	if err != nil {
		return domain.Schedule{}, err
	}
	id, err := newUUID()
	if err != nil {
		return domain.Schedule{}, err
	}
	schedule := domain.Schedule{
		ID:         id,
		TenantID:   tenantID,
		PipelineID: pipelineID,
		Cron:       cron,
		Timezone:   timezone,
		Enabled:    true,
		NextRunAt:  &next,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return m.repo.CreateSchedule(ctx, schedule)
}

// GetSchedule returns one schedule in the caller's tenant.
func (m *Manager) GetSchedule(ctx context.Context, tenantID, scheduleID string) (domain.Schedule, error) {
	if err := m.requireRepository(); err != nil {
		return domain.Schedule{}, err
	}
	tenantID, err := normalizeTenant(tenantID)
	if err != nil {
		return domain.Schedule{}, err
	}
	scheduleID, err = normalizeUUID(scheduleID, "schedule_id")
	if err != nil {
		return domain.Schedule{}, err
	}
	return m.repo.GetSchedule(ctx, tenantID, scheduleID)
}

// ListSchedules returns a page of schedules, optionally for one Pipeline.
func (m *Manager) ListSchedules(ctx context.Context, request ListSchedulesRequest) (SchedulePage, error) {
	if err := m.requireRepository(); err != nil {
		return SchedulePage{}, err
	}
	tenantID, err := normalizeTenant(request.TenantID)
	if err != nil {
		return SchedulePage{}, err
	}
	page, perPage, err := normalizePage(request.Page, request.PerPage)
	if err != nil {
		return SchedulePage{}, err
	}
	pipelineID := ""
	if strings.TrimSpace(request.PipelineID) != "" {
		pipelineID, err = normalizeUUID(request.PipelineID, "pipeline_id")
		if err != nil {
			return SchedulePage{}, err
		}
		if _, err := m.repo.GetPipeline(ctx, tenantID, pipelineID); err != nil {
			return SchedulePage{}, err
		}
	}
	items, total, err := m.repo.ListSchedules(ctx, ScheduleFilter{
		TenantID:   tenantID,
		PipelineID: pipelineID,
		Page:       page,
		PerPage:    perPage,
	})
	if err != nil {
		return SchedulePage{}, err
	}
	return SchedulePage{Items: items, Page: pageInfo(page, perPage, total)}, nil
}

// UpdateSchedule patches cron, timezone or enabled state.
func (m *Manager) UpdateSchedule(ctx context.Context, request UpdateScheduleRequest) (domain.Schedule, error) {
	if err := m.requireRepository(); err != nil {
		return domain.Schedule{}, err
	}
	tenantID, err := normalizeTenant(request.TenantID)
	if err != nil {
		return domain.Schedule{}, err
	}
	scheduleID, err := normalizeUUID(request.ScheduleID, "schedule_id")
	if err != nil {
		return domain.Schedule{}, err
	}
	schedule, err := m.repo.GetSchedule(ctx, tenantID, scheduleID)
	if err != nil {
		return domain.Schedule{}, err
	}
	cron := schedule.Cron
	timezone := schedule.Timezone
	cronChanged := false
	if request.Cron != nil {
		cron = strings.TrimSpace(*request.Cron)
		cronChanged = true
	}
	if request.Timezone != nil {
		timezone = strings.TrimSpace(*request.Timezone)
	}
	if cronChanged || request.Timezone != nil {
		normalizedTimezone, location, validateErr := normalizeScheduleTime(cron, timezone)
		if validateErr != nil {
			return domain.Schedule{}, validateErr
		}
		timezone = normalizedTimezone
		schedule.Cron = cron
		schedule.Timezone = timezone
		if schedule.Enabled {
			next, nextErr := domain.ComputeNextRun(cron, location.String(), m.now().UTC())
			if nextErr != nil {
				return domain.Schedule{}, nextErr
			}
			schedule.NextRunAt = &next
		}
	}
	if request.Enabled != nil {
		schedule.Enabled = *request.Enabled
		if schedule.Enabled {
			next, nextErr := domain.ComputeNextRun(schedule.Cron, schedule.Timezone, m.now().UTC())
			if nextErr != nil {
				return domain.Schedule{}, nextErr
			}
			schedule.NextRunAt = &next
		} else {
			schedule.NextRunAt = nil
		}
	}
	schedule.UpdatedAt = m.now().UTC()
	return m.repo.UpdateSchedule(ctx, schedule)
}

// DeleteSchedule removes a schedule from the tenant.
func (m *Manager) DeleteSchedule(ctx context.Context, tenantID, scheduleID string) error {
	if err := m.requireRepository(); err != nil {
		return err
	}
	tenantID, err := normalizeTenant(tenantID)
	if err != nil {
		return err
	}
	scheduleID, err = normalizeUUID(scheduleID, "schedule_id")
	if err != nil {
		return err
	}
	if _, err := m.repo.GetSchedule(ctx, tenantID, scheduleID); err != nil {
		return err
	}
	return m.repo.DeleteSchedule(ctx, tenantID, scheduleID)
}

// PauseSchedule disables the schedule and clears next_run_at.
func (m *Manager) PauseSchedule(ctx context.Context, tenantID, scheduleID string) (domain.Schedule, error) {
	disabled := false
	return m.UpdateSchedule(ctx, UpdateScheduleRequest{
		TenantID:   tenantID,
		ScheduleID: scheduleID,
		Enabled:    &disabled,
	})
}

// ResumeSchedule enables the schedule and recomputes next_run_at.
func (m *Manager) ResumeSchedule(ctx context.Context, tenantID, scheduleID string) (domain.Schedule, error) {
	enabled := true
	return m.UpdateSchedule(ctx, UpdateScheduleRequest{
		TenantID:   tenantID,
		ScheduleID: scheduleID,
		Enabled:    &enabled,
	})
}

func normalizeScheduleTime(cron, timezone string) (string, *time.Location, error) {
	cron = strings.TrimSpace(cron)
	if cron == "" {
		return "", nil, fmt.Errorf("%w: cron must not be empty", domain.ErrInvalidArgument)
	}
	if _, err := domain.ParseCron(cron); err != nil {
		return "", nil, err
	}
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", nil, fmt.Errorf("%w: invalid timezone %q", domain.ErrInvalidArgument, timezone)
	}
	return timezone, location, nil
}
