package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
)

func (a *API) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	var request createScheduleRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeFailure(w, r, "invalid_argument", "invalid request body")
		return
	}
	pipeline, ok := a.loadPipelineForAuth(w, r, tenantID)
	if !ok {
		return
	}
	if !a.authorize(w, r, "pipeline_schedule:write", pipelineFact(pipeline)) {
		return
	}
	schedule, err := a.schedules.CreateSchedule(r.Context(), service.CreateScheduleRequest{
		TenantID:   tenantID,
		PipelineID: chi.URLParam(r, "pipelineID"),
		Cron:       request.Cron,
		Timezone:   request.Timezone,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toScheduleResponse(schedule))
}

func (a *API) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	schedule, ok := a.loadScheduleForAuth(w, r, tenantID)
	if !ok {
		return
	}
	if !a.authorize(w, r, "pipeline_schedule:read", pipelineScheduleFact(schedule)) {
		return
	}
	writeJSON(w, http.StatusOK, toScheduleResponse(schedule))
}

func (a *API) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	page, err := queryInt(r, "page")
	if err != nil {
		writeFailure(w, r, "invalid_argument", "invalid page")
		return
	}
	perPage, err := queryInt(r, "per_page")
	if err != nil {
		writeFailure(w, r, "invalid_argument", "invalid per_page")
		return
	}
	pipeline, ok := a.loadPipelineForAuth(w, r, tenantID)
	if !ok {
		return
	}
	if !a.authorize(w, r, "pipeline_schedule:read", pipelineFact(pipeline)) {
		return
	}
	result, err := a.schedules.ListSchedules(r.Context(), service.ListSchedulesRequest{
		TenantID:   tenantID,
		PipelineID: chi.URLParam(r, "pipelineID"),
		Page:       page,
		PerPage:    perPage,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]scheduleResponse, 0, len(result.Items))
	for _, schedule := range result.Items {
		items = append(items, toScheduleResponse(schedule))
	}
	writeJSON(w, http.StatusOK, scheduleListResponse{Schedules: items, Page: toPageResponse(result.Page)})
}

func (a *API) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	var request updateScheduleRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeFailure(w, r, "invalid_argument", "invalid request body")
		return
	}
	current, ok := a.loadScheduleForAuth(w, r, tenantID)
	if !ok {
		return
	}
	if !a.authorize(w, r, "pipeline_schedule:write", pipelineScheduleFact(current)) {
		return
	}
	schedule, err := a.schedules.UpdateSchedule(r.Context(), service.UpdateScheduleRequest{
		TenantID:   tenantID,
		ScheduleID: chi.URLParam(r, "scheduleID"),
		Cron:       request.Cron,
		Timezone:   request.Timezone,
		Enabled:    request.Enabled,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toScheduleResponse(schedule))
}

func (a *API) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	current, ok := a.loadScheduleForAuth(w, r, tenantID)
	if !ok {
		return
	}
	if !a.authorize(w, r, "pipeline_schedule:delete", pipelineScheduleFact(current)) {
		return
	}
	if err := a.schedules.DeleteSchedule(r.Context(), tenantID, chi.URLParam(r, "scheduleID")); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handlePauseSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	current, ok := a.loadScheduleForAuth(w, r, tenantID)
	if !ok {
		return
	}
	if !a.authorize(w, r, "pipeline_schedule:write", pipelineScheduleFact(current)) {
		return
	}
	schedule, err := a.schedules.PauseSchedule(r.Context(), tenantID, chi.URLParam(r, "scheduleID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toScheduleResponse(schedule))
}

func (a *API) handleResumeSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	current, ok := a.loadScheduleForAuth(w, r, tenantID)
	if !ok {
		return
	}
	if !a.authorize(w, r, "pipeline_schedule:write", pipelineScheduleFact(current)) {
		return
	}
	schedule, err := a.schedules.ResumeSchedule(r.Context(), tenantID, chi.URLParam(r, "scheduleID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toScheduleResponse(schedule))
}
