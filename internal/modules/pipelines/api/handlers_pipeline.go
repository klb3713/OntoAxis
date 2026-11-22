package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
)

// Register mounts the pipelines REST API on the module router.
func (a *API) Register(r chi.Router) {
	r.Route("/api/v1/pipelines", func(r chi.Router) {
		r.Post("/", a.handleCreatePipeline)
		r.Get("/", a.handleListPipelines)
		r.Get("/{pipelineID}", a.handleGetPipeline)
		r.Patch("/{pipelineID}", a.handleUpdatePipeline)
		r.Delete("/{pipelineID}", a.handleDeletePipeline)
		r.Post("/{pipelineID}/publish", a.handlePublishPipeline)
		r.Post("/{pipelineID}/runs", a.handleTriggerRun)
		r.Get("/{pipelineID}/runs", a.handleListRuns)
		r.Get("/{pipelineID}/runs/{runID}", a.handleGetRun)
		r.Get("/{pipelineID}/schedules", a.handleListSchedules)
		r.Post("/{pipelineID}/schedules", a.handleCreateSchedule)
		r.Get("/schedules/{scheduleID}", a.handleGetSchedule)
		r.Patch("/schedules/{scheduleID}", a.handleUpdateSchedule)
		r.Delete("/schedules/{scheduleID}", a.handleDeleteSchedule)
		r.Post("/schedules/{scheduleID}/pause", a.handlePauseSchedule)
		r.Post("/schedules/{scheduleID}/resume", a.handleResumeSchedule)
	})
}

func (a *API) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	var request createPipelineRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeFailure(w, r, "invalid_argument", "invalid request body")
		return
	}
	pipeline, err := a.pipelines.CreatePipeline(r.Context(), service.CreatePipelineRequest{
		TenantID:    tenantID,
		Name:        request.Name,
		DisplayName: request.DisplayName,
		Description: request.Description,
		Type:        domain.PipelineType(request.Type),
		DAG:         request.definition(),
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toPipelineResponse(pipeline))
}

func (a *API) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	pipeline, err := a.pipelines.GetPipeline(r.Context(), tenantID, chi.URLParam(r, "pipelineID"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPipelineResponse(pipeline))
}

func (a *API) handleListPipelines(w http.ResponseWriter, r *http.Request) {
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
	result, err := a.pipelines.ListPipelines(r.Context(), service.ListPipelinesRequest{
		TenantID: tenantID,
		Page:     page,
		PerPage:  perPage,
		Status:   domain.PipelineStatus(r.URL.Query().Get("status")),
		Type:     domain.PipelineType(r.URL.Query().Get("type")),
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]pipelineResponse, 0, len(result.Items))
	for _, pipeline := range result.Items {
		items = append(items, toPipelineResponse(pipeline))
	}
	writeJSON(w, http.StatusOK, pipelineListResponse{Pipelines: items, Page: toPageResponse(result.Page)})
}

func (a *API) handleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	var request updatePipelineRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeFailure(w, r, "invalid_argument", "invalid request body")
		return
	}
	pipeline, err := a.pipelines.UpdatePipeline(r.Context(), service.UpdatePipelineRequest{
		TenantID:    tenantID,
		PipelineID:  chi.URLParam(r, "pipelineID"),
		DisplayName: request.DisplayName,
		Description: request.Description,
		DAG:         request.definition(),
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPipelineResponse(pipeline))
}

func (a *API) handleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	if err := a.pipelines.DeletePipeline(r.Context(), tenantID, chi.URLParam(r, "pipelineID")); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type publishPipelineResponse struct {
	pipelineResponse
	PublishedVersionID string `json:"published_version_id"`
}

func (a *API) handlePublishPipeline(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	var request publishPipelineRequest
	if err := decodeOptionalJSON(w, r, &request); err != nil {
		writeFailure(w, r, "invalid_argument", "invalid request body")
		return
	}
	pipeline, version, err := a.pipelines.PublishPipeline(r.Context(), service.PublishPipelineRequest{
		TenantID:    tenantID,
		PipelineID:  chi.URLParam(r, "pipelineID"),
		PublishedBy: request.PublishedBy,
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, publishPipelineResponse{
		pipelineResponse:   toPipelineResponse(pipeline),
		PublishedVersionID: version.ID,
	})
}

func (a *API) handleTriggerRun(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	run, err := a.pipelines.TriggerPipelineRun(r.Context(), service.TriggerPipelineRunRequest{
		TenantID:   tenantID,
		PipelineID: chi.URLParam(r, "pipelineID"),
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toRunResponse(run))
}

func (a *API) handleGetRun(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := a.requireTenant(w, r)
	if !ok {
		return
	}
	run, err := a.pipelines.GetPipelineRun(r.Context(), service.GetPipelineRunRequest{
		TenantID:   tenantID,
		PipelineID: chi.URLParam(r, "pipelineID"),
		RunID:      chi.URLParam(r, "runID"),
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toRunResponse(run))
}

func (a *API) handleListRuns(w http.ResponseWriter, r *http.Request) {
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
	result, err := a.pipelines.ListPipelineRuns(r.Context(), service.ListPipelineRunsRequest{
		TenantID:   tenantID,
		PipelineID: chi.URLParam(r, "pipelineID"),
		Page:       page,
		PerPage:    perPage,
		Status:     domain.PipelineRunStatus(r.URL.Query().Get("status")),
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]pipelineRunResponse, 0, len(result.Items))
	for _, run := range result.Items {
		items = append(items, toRunResponse(run))
	}
	writeJSON(w, http.StatusOK, pipelineRunListResponse{Runs: items, Page: toPageResponse(result.Page)})
}

func queryInt(r *http.Request, name string) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return value, nil
}
