package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
)

type pipelineResponse struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Definition  json.RawMessage `json:"definition"`
	Version     int             `json:"version"`
	Status      string          `json:"status"`
	CreatedBy   string          `json:"created_by,omitempty"`
	DeletedAt   *time.Time      `json:"deleted_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type pipelineRunResponse struct {
	ID          string     `json:"id"`
	PipelineID  string     `json:"pipeline_id"`
	Version     int        `json:"version"`
	Status      string     `json:"status"`
	TriggerType string     `json:"trigger_type"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type scheduleResponse struct {
	ID         string     `json:"id"`
	PipelineID string     `json:"pipeline_id"`
	Cron       string     `json:"cron"`
	Timezone   string     `json:"timezone"`
	Enabled    bool       `json:"enabled"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type pageResponse struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type pipelineListResponse struct {
	Pipelines []pipelineResponse `json:"pipelines"`
	Page      pageResponse       `json:"page"`
}

type pipelineRunListResponse struct {
	Runs []pipelineRunResponse `json:"runs"`
	Page pageResponse          `json:"page"`
}

type scheduleListResponse struct {
	Schedules []scheduleResponse `json:"schedules"`
	Page      pageResponse       `json:"page"`
}

type createPipelineRequest struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Definition  json.RawMessage `json:"definition"`
	DAG         json.RawMessage `json:"dag"`
}

func (r createPipelineRequest) definition() json.RawMessage {
	if len(r.Definition) > 0 {
		return r.Definition
	}
	return r.DAG
}

type updatePipelineRequest struct {
	DisplayName *string         `json:"display_name"`
	Description *string         `json:"description"`
	Definition  json.RawMessage `json:"definition"`
	DAG         json.RawMessage `json:"dag"`
}

func (r updatePipelineRequest) definition() json.RawMessage {
	if len(r.Definition) > 0 {
		return r.Definition
	}
	return r.DAG
}

type publishPipelineRequest struct {
	PublishedBy string `json:"published_by"`
}

type createScheduleRequest struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
}

type updateScheduleRequest struct {
	Cron     *string `json:"cron"`
	Timezone *string `json:"timezone"`
	Enabled  *bool   `json:"enabled"`
}

func toPipelineResponse(pipeline domain.Pipeline) pipelineResponse {
	definition := pipeline.DAG
	if len(definition) == 0 {
		definition = json.RawMessage(`{}`)
	}
	return pipelineResponse{
		ID:          pipeline.ID,
		Name:        pipeline.Name,
		DisplayName: pipeline.DisplayName,
		Description: pipeline.Description,
		Type:        string(pipeline.Type),
		Definition:  definition,
		Version:     pipeline.Version,
		Status:      string(pipeline.Status),
		CreatedBy:   pipeline.CreatedBy,
		DeletedAt:   pipeline.DeletedAt,
		CreatedAt:   pipeline.CreatedAt,
		UpdatedAt:   pipeline.UpdatedAt,
	}
}

func toRunResponse(run domain.PipelineRun) pipelineRunResponse {
	return pipelineRunResponse{
		ID:          run.ID,
		PipelineID:  run.PipelineID,
		Version:     run.Version,
		Status:      string(run.Status),
		TriggerType: string(run.TriggerType),
		StartedAt:   run.StartedAt,
		FinishedAt:  run.FinishedAt,
		Error:       run.Error,
		CreatedAt:   run.CreatedAt,
		UpdatedAt:   run.UpdatedAt,
	}
}

func toScheduleResponse(schedule domain.Schedule) scheduleResponse {
	return scheduleResponse{
		ID:         schedule.ID,
		PipelineID: schedule.PipelineID,
		Cron:       schedule.Cron,
		Timezone:   schedule.Timezone,
		Enabled:    schedule.Enabled,
		NextRunAt:  schedule.NextRunAt,
		CreatedAt:  schedule.CreatedAt,
		UpdatedAt:  schedule.UpdatedAt,
	}
}

func toPageResponse(page service.PageInfo) pageResponse {
	return pageResponse{
		Page:       page.Page,
		PerPage:    page.PerPage,
		Total:      page.Total,
		TotalPages: page.TotalPages,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("invalid JSON body: multiple values")
	}
	return nil
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, target any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	if err := decodeJSON(w, r, target); err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}
	return nil
}
