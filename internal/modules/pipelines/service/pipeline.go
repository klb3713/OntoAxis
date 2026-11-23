package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/dbt"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
)

// CreatePipeline validates and stores a new draft Pipeline.
func (m *Manager) CreatePipeline(ctx context.Context, request CreatePipelineRequest) (domain.Pipeline, error) {
	if err := m.requireRepository(); err != nil {
		return domain.Pipeline{}, err
	}
	tenantID, err := normalizeTenant(request.TenantID)
	if err != nil {
		return domain.Pipeline{}, err
	}
	name, err := validatePipelineName(request.Name)
	if err != nil {
		return domain.Pipeline{}, err
	}
	pipelineType, err := domain.ParsePipelineType(string(request.Type))
	if err != nil {
		return domain.Pipeline{}, err
	}
	dag, err := normalizeDAG(request.DAG)
	if err != nil {
		return domain.Pipeline{}, err
	}
	id, err := newUUID()
	if err != nil {
		return domain.Pipeline{}, err
	}
	now := m.now().UTC()
	pipeline := domain.Pipeline{
		ID:          id,
		TenantID:    tenantID,
		Name:        name,
		DisplayName: strings.TrimSpace(request.DisplayName),
		Description: strings.TrimSpace(request.Description),
		Type:        pipelineType,
		DAG:         dag,
		Version:     0,
		Status:      domain.PipelineStatusDraft,
		CreatedBy:   strings.TrimSpace(request.CreatedBy),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return m.repo.CreatePipeline(ctx, pipeline)
}

// GetPipeline returns one non-deleted Pipeline in the caller's tenant.
func (m *Manager) GetPipeline(ctx context.Context, tenantID, pipelineID string) (domain.Pipeline, error) {
	if err := m.requireRepository(); err != nil {
		return domain.Pipeline{}, err
	}
	tenantID, err := normalizeTenant(tenantID)
	if err != nil {
		return domain.Pipeline{}, err
	}
	pipelineID, err = normalizeUUID(pipelineID, "pipeline_id")
	if err != nil {
		return domain.Pipeline{}, err
	}
	return m.repo.GetPipeline(ctx, tenantID, pipelineID)
}

// ListPipelines returns a filtered page of non-deleted Pipelines.
func (m *Manager) ListPipelines(ctx context.Context, request ListPipelinesRequest) (PipelinePage, error) {
	if err := m.requireRepository(); err != nil {
		return PipelinePage{}, err
	}
	tenantID, err := normalizeTenant(request.TenantID)
	if err != nil {
		return PipelinePage{}, err
	}
	page, perPage, err := normalizePage(request.Page, request.PerPage)
	if err != nil {
		return PipelinePage{}, err
	}
	if request.Status != "" && !request.Status.Valid() {
		return PipelinePage{}, fmt.Errorf("%w: unsupported pipeline status %q", domain.ErrInvalidArgument, request.Status)
	}
	if request.Type != "" && !request.Type.Valid() {
		return PipelinePage{}, fmt.Errorf("%w: unsupported pipeline type %q", domain.ErrInvalidArgument, request.Type)
	}
	items, total, err := m.repo.ListPipelines(ctx, PipelineFilter{
		TenantID: tenantID,
		Page:     page,
		PerPage:  perPage,
		Status:   request.Status,
		Type:     request.Type,
	})
	if err != nil {
		return PipelinePage{}, err
	}
	return PipelinePage{Items: items, Page: pageInfo(page, perPage, total)}, nil
}

// UpdatePipeline patches display name, description or DAG.
func (m *Manager) UpdatePipeline(ctx context.Context, request UpdatePipelineRequest) (domain.Pipeline, error) {
	if err := m.requireRepository(); err != nil {
		return domain.Pipeline{}, err
	}
	tenantID, err := normalizeTenant(request.TenantID)
	if err != nil {
		return domain.Pipeline{}, err
	}
	pipelineID, err := normalizeUUID(request.PipelineID, "pipeline_id")
	if err != nil {
		return domain.Pipeline{}, err
	}
	pipeline, err := m.repo.GetPipeline(ctx, tenantID, pipelineID)
	if err != nil {
		return domain.Pipeline{}, err
	}
	if pipeline.Status == domain.PipelineStatusArchived {
		return domain.Pipeline{}, fmt.Errorf("%w: archived pipeline cannot be updated", domain.ErrInvalidState)
	}
	if request.DisplayName != nil {
		pipeline.DisplayName = strings.TrimSpace(*request.DisplayName)
	}
	if request.Description != nil {
		pipeline.Description = strings.TrimSpace(*request.Description)
	}
	if len(request.DAG) > 0 {
		dag, dagErr := normalizeDAG(request.DAG)
		if dagErr != nil {
			return domain.Pipeline{}, dagErr
		}
		// Editing the executable definition of a published Pipeline turns it
		// back into a draft: the current published version snapshot stays
		// immutable and must be followed by an explicit new publish.
		if pipeline.Status == domain.PipelineStatusPublished && !bytes.Equal(pipeline.DAG, dag) {
			pipeline.Status = domain.PipelineStatusDraft
		}
		pipeline.DAG = dag
	}
	pipeline.UpdatedAt = m.now().UTC()
	return m.repo.UpdatePipeline(ctx, pipeline)
}

// DeletePipeline archives and soft-deletes a Pipeline.
func (m *Manager) DeletePipeline(ctx context.Context, tenantID, pipelineID string) error {
	if err := m.requireRepository(); err != nil {
		return err
	}
	tenantID, err := normalizeTenant(tenantID)
	if err != nil {
		return err
	}
	pipelineID, err = normalizeUUID(pipelineID, "pipeline_id")
	if err != nil {
		return err
	}
	if _, err := m.repo.GetPipeline(ctx, tenantID, pipelineID); err != nil {
		return err
	}
	return m.repo.DeletePipeline(ctx, tenantID, pipelineID, m.now().UTC())
}

// PublishPipeline renders the dbt project and Flow, uploads namespace files,
// registers the Flow, then commits the immutable version snapshot. Any engine
// failure prevents a published status; a database failure triggers best-effort
// file cleanup so the next attempt starts from a clean namespace.
func (m *Manager) PublishPipeline(ctx context.Context, request PublishPipelineRequest) (domain.Pipeline, domain.PipelineVersion, error) {
	if err := m.requireRepository(); err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, err
	}
	tenantID, err := normalizeTenant(request.TenantID)
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, err
	}
	pipelineID, err := normalizeUUID(request.PipelineID, "pipeline_id")
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, err
	}
	pipeline, err := m.repo.GetPipeline(ctx, tenantID, pipelineID)
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, err
	}
	if pipeline.Status == domain.PipelineStatusArchived {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: archived pipeline cannot be published", domain.ErrInvalidState)
	}
	if m.adapter == nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: kestra adapter is not configured", domain.ErrUnavailable)
	}
	dag, err := domain.ParseDAG(pipeline.DAG)
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, err
	}
	if err := domain.ValidateDAGDefinition(dag); err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, err
	}
	if err := m.validateDatasetReferences(ctx, tenantID, pipeline.DAG); err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, err
	}
	versionID, err := newUUID()
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, err
	}
	now := m.now().UTC()
	previousVersion := pipeline.Version
	nextVersion := previousVersion + 1

	flowIR, err := kestra.BuildFlowIR(tenantID, pipelineID, nextVersion, dag, m.environment)
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: render flow: %s", domain.ErrInvalidArgument, err.Error())
	}
	flowYAML, err := kestra.RenderFlowYAML(flowIR)
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: render flow: %s", domain.ErrInvalidArgument, err.Error())
	}
	project, err := dbt.RenderProject(tenantID, pipelineID, nextVersion, dag, m.dbtCatalog)
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, err
	}
	files := make([]kestra.File, 0, len(project.Files))
	paths := make([]string, 0, len(project.Files))
	for _, file := range project.Files {
		files = append(files, kestra.File{Path: file.Path, Content: file.Content})
		paths = append(paths, file.Path)
	}
	if err := m.adapter.PublishFiles(ctx, flowIR.Namespace, files); err != nil {
		original := fmt.Errorf("%w: publish dbt files failed", domain.ErrUnavailable)
		return domain.Pipeline{}, domain.PipelineVersion{}, m.failPublish(ctx, original, tenantID, pipelineID, previousVersion, nextVersion, flowIR, paths)
	}
	if _, err := m.adapter.RegisterFlow(ctx, kestra.Flow{
		TenantID:   tenantID,
		PipelineID: pipelineID,
		Name:       pipeline.Name,
		Version:    nextVersion,
		DAG:        pipeline.DAG,
		FlowYAML:   flowYAML,
	}); err != nil {
		original := fmt.Errorf("%w: kestra Flow registration failed", domain.ErrUnavailable)
		return domain.Pipeline{}, domain.PipelineVersion{}, m.failPublish(ctx, original, tenantID, pipelineID, previousVersion, nextVersion, flowIR, paths)
	}
	version := domain.PipelineVersion{
		ID:          versionID,
		TenantID:    tenantID,
		PipelineID:  pipelineID,
		Version:     nextVersion,
		DAG:         pipeline.DAG,
		PublishedAt: now,
		PublishedBy: strings.TrimSpace(request.PublishedBy),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	pipeline.Version = nextVersion
	pipeline.Status = domain.PipelineStatusPublished
	pipeline.UpdatedAt = now
	published, publishedVersion, err := m.repo.PublishPipeline(ctx, pipeline, version)
	if err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, m.failPublish(ctx, err, tenantID, pipelineID, previousVersion, nextVersion, flowIR, paths)
	}
	return published, publishedVersion, nil
}

// failPublish restores the engine to the last published version after any
// failure that happened after the Flow/files may have been written. The
// database is authoritative: if the new version was not committed, the old
// Flow revision and its namespace files must remain what callers see.
func (m *Manager) failPublish(ctx context.Context, original error, tenantID, pipelineID string, previousVersion, nextVersion int, nextFlow kestra.FlowIR, nextPaths []string) error {
	if compErr := m.rollbackPublish(ctx, tenantID, pipelineID, previousVersion, nextVersion, nextFlow, nextPaths); compErr != nil {
		slog.Default().Warn("pipeline publish compensation failed",
			"tenant_id", tenantID,
			"pipeline_id", pipelineID,
			"previous_version", previousVersion,
			"next_version", nextVersion,
			"error", compErr.Error())
		return fmt.Errorf("%w; publish compensation failed: %v", original, compErr)
	}
	return original
}

func (m *Manager) rollbackPublish(ctx context.Context, tenantID, pipelineID string, previousVersion, nextVersion int, nextFlow kestra.FlowIR, nextPaths []string) error {
	namespace := nextFlow.Namespace
	if previousVersion <= 0 {
		var firstErr error
		if err := m.adapter.DeleteFlow(ctx, kestra.FlowRef{Namespace: namespace, FlowID: nextFlow.FlowID, Revision: nextVersion}); err != nil {
			firstErr = err
		}
		if err := m.adapter.DeleteFiles(ctx, namespace, nextPaths); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	}
	previous, err := m.repo.GetPublishedVersion(ctx, tenantID, pipelineID, previousVersion)
	if err != nil {
		return fmt.Errorf("load previous published version: %w", err)
	}
	previousDAG, err := domain.ParseDAG(previous.DAG)
	if err != nil {
		return fmt.Errorf("parse previous DAG: %w", err)
	}
	previousFlow, err := kestra.BuildFlowIR(tenantID, pipelineID, previous.Version, previousDAG, m.environment)
	if err != nil {
		return fmt.Errorf("render previous flow: %w", err)
	}
	previousYAML, err := kestra.RenderFlowYAML(previousFlow)
	if err != nil {
		return fmt.Errorf("render previous flow YAML: %w", err)
	}
	previousProject, err := dbt.RenderProject(tenantID, pipelineID, previous.Version, previousDAG, m.dbtCatalog)
	if err != nil {
		return fmt.Errorf("render previous dbt project: %w", err)
	}
	previousPaths := make(map[string]struct{}, len(previousProject.Files))
	previousFiles := make([]kestra.File, 0, len(previousProject.Files))
	for _, file := range previousProject.Files {
		previousPaths[file.Path] = struct{}{}
		previousFiles = append(previousFiles, kestra.File{Path: file.Path, Content: file.Content})
	}
	removed := make([]string, 0)
	for _, filePath := range nextPaths {
		if _, ok := previousPaths[filePath]; !ok {
			removed = append(removed, filePath)
		}
	}
	var firstErr error
	if len(removed) > 0 {
		if err := m.adapter.DeleteFiles(ctx, namespace, removed); err != nil {
			firstErr = err
		}
	}
	if err := m.adapter.PublishFiles(ctx, namespace, previousFiles); err != nil && firstErr == nil {
		firstErr = err
	}
	if _, err := m.adapter.RegisterFlow(ctx, kestra.Flow{
		TenantID:   tenantID,
		PipelineID: pipelineID,
		Version:    previous.Version,
		DAG:        previous.DAG,
		FlowYAML:   previousYAML,
	}); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// TriggerPipelineRun creates a queued run, records its input lineage, then asks
// the engine to execute the published version. A lost trigger response is
// adopted through the run label instead of creating a second execution; a
// permanent failure marks the run failed without leaking engine text.
func (m *Manager) TriggerPipelineRun(ctx context.Context, request TriggerPipelineRunRequest) (domain.PipelineRun, error) {
	if err := m.requireRepository(); err != nil {
		return domain.PipelineRun{}, err
	}
	tenantID, err := normalizeTenant(request.TenantID)
	if err != nil {
		return domain.PipelineRun{}, err
	}
	pipelineID, err := normalizeUUID(request.PipelineID, "pipeline_id")
	if err != nil {
		return domain.PipelineRun{}, err
	}
	pipeline, err := m.repo.GetPipeline(ctx, tenantID, pipelineID)
	if err != nil {
		return domain.PipelineRun{}, err
	}
	if pipeline.Status != domain.PipelineStatusPublished {
		return domain.PipelineRun{}, fmt.Errorf("%w: only published pipelines can be triggered", domain.ErrInvalidState)
	}
	if m.adapter == nil {
		return domain.PipelineRun{}, fmt.Errorf("%w: kestra adapter is not configured", domain.ErrUnavailable)
	}
	dag, err := domain.ParseDAG(pipeline.DAG)
	if err != nil {
		return domain.PipelineRun{}, err
	}
	if err := domain.ValidateDAGDefinition(dag); err != nil {
		return domain.PipelineRun{}, err
	}
	if err := m.validateDatasetReferences(ctx, tenantID, pipeline.DAG); err != nil {
		return domain.PipelineRun{}, err
	}
	runID, err := newUUID()
	if err != nil {
		return domain.PipelineRun{}, err
	}
	now := m.now().UTC()
	run := domain.PipelineRun{
		ID:          runID,
		TenantID:    tenantID,
		PipelineID:  pipelineID,
		Version:     pipeline.Version,
		Status:      domain.PipelineRunStatusQueued,
		TriggerType: domain.TriggerTypeManual,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	run, err = m.repo.CreateRun(ctx, run)
	if err != nil {
		return domain.PipelineRun{}, err
	}
	if inputs := runInputs(run, dag); len(inputs) > 0 {
		if err := m.repo.CreateRunInputs(ctx, inputs); err != nil {
			return domain.PipelineRun{}, err
		}
	}
	result, triggerErr := m.adapter.TriggerRun(ctx, kestra.RunRequest{
		TenantID:    tenantID,
		PipelineID:  pipelineID,
		Version:     pipeline.Version,
		TriggerType: string(domain.TriggerTypeManual),
		DAG:         pipeline.DAG,
		RunID:       runID,
	})
	if triggerErr != nil {
		if execution, findErr := m.adapter.FindExecutionByRun(ctx, tenantID, runID); findErr == nil {
			return m.markRunning(ctx, run, execution.ID)
		}
		finished := m.now().UTC()
		run.Status = domain.PipelineRunStatusFailed
		run.FailureCode = "execution_not_accepted"
		run.Error = "kestra trigger failed"
		run.FinishedAt = &finished
		run.UpdatedAt = finished
		updated, storeErr := m.repo.UpdateRunConditional(ctx, run)
		if storeErr != nil {
			return domain.PipelineRun{}, errors.Join(fmt.Errorf("%w: kestra trigger failed", domain.ErrUnavailable), storeErr)
		}
		return updated, fmt.Errorf("%w: kestra trigger failed", domain.ErrUnavailable)
	}
	return m.markRunning(ctx, run, result.ExecutionID)
}

func (m *Manager) markRunning(ctx context.Context, run domain.PipelineRun, executionID string) (domain.PipelineRun, error) {
	now := m.now().UTC()
	run.Status = domain.PipelineRunStatusRunning
	run.ExecutionID = executionID
	run.StartedAt = &now
	run.FailureCode = ""
	run.UpdatedAt = now
	return m.repo.UpdateRunConditional(ctx, run)
}

func runInputs(run domain.PipelineRun, dag domain.DAG) []domain.PipelineRunInput {
	seen := map[string]struct{}{}
	inputs := make([]domain.PipelineRunInput, 0, len(dag.DatasetIDs)+len(dag.Inputs))
	add := func(datasetID string) {
		if datasetID == "" {
			return
		}
		if _, ok := seen[datasetID]; ok {
			return
		}
		seen[datasetID] = struct{}{}
		inputs = append(inputs, domain.PipelineRunInput{
			PipelineRunID: run.ID,
			TenantID:      run.TenantID,
			DatasetID:     datasetID,
			CreatedAt:     run.CreatedAt,
		})
	}
	for _, id := range dag.DatasetIDs {
		add(id)
	}
	for _, input := range dag.Inputs {
		add(input.DatasetID)
	}
	return inputs
}

func terminalRunStatus(status domain.PipelineRunStatus) bool {
	switch status {
	case domain.PipelineRunStatusSucceeded, domain.PipelineRunStatusFailed, domain.PipelineRunStatusCancelled:
		return true
	default:
		return false
	}
}

// mapExecutionStatus implements design D7. Unknown states stay non-terminal so
// a transient engine response cannot terminalize a run; the failure code is
// retained for diagnostics.
func mapExecutionStatus(raw string) (domain.PipelineRunStatus, string) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "CREATED", "RESTARTED", "RETRYING", "RUNNING", "PAUSED", "KILLING":
		return domain.PipelineRunStatusRunning, ""
	case "SUCCESS", "WARNING":
		return domain.PipelineRunStatusSucceeded, ""
	case "FAILED", "RETRY_FAILED":
		return domain.PipelineRunStatusFailed, "execution_failed"
	case "KILLED", "CANCELLED":
		return domain.PipelineRunStatusCancelled, "execution_cancelled"
	default:
		return domain.PipelineRunStatusRunning, "unknown_execution_state"
	}
}

const legacyRunTimeout = 24 * time.Hour

func (m *Manager) reconcileRun(ctx context.Context, run domain.PipelineRun) (out domain.PipelineRun) {
	if m.adapter == nil || terminalRunStatus(run.Status) {
		return run
	}
	now := m.now().UTC()
	if run.LastReconciledAt != nil && now.Sub(*run.LastReconciledAt) < m.reconcileThrottle {
		return run
	}
	defer func() {
		if m.metrics != nil {
			result := "success"
			if out.FailureCode != "" {
				result = "failed"
			}
			m.metrics.JobsTotal.WithLabelValues("pipeline_run_reconcile", result).Inc()
		}
	}()
	var (
		execution kestra.Execution
		err       error
	)
	if run.ExecutionID == "" && now.Sub(run.CreatedAt) > legacyRunTimeout {
		update := run
		update.Status = domain.PipelineRunStatusFailed
		update.FailureCode = "legacy_run_without_execution"
		update.Error = "pipeline run has no execution reference after legacy timeout"
		finished := now
		update.FinishedAt = &finished
		update.UpdatedAt = now
		updated, updateErr := m.repo.UpdateRunConditional(ctx, update)
		if updateErr != nil {
			return run
		}
		return updated
	}
	if run.ExecutionID != "" {
		execution, err = m.adapter.GetExecution(ctx, kestra.ExecutionRef{ID: run.ExecutionID})
	} else {
		execution, err = m.adapter.FindExecutionByRun(ctx, run.TenantID, run.ID)
	}
	if err != nil {
		if run.Status == domain.PipelineRunStatusQueued && errors.Is(err, kestra.ErrNotFound) {
			if now.Sub(run.CreatedAt) > m.recoveryTimeout {
				update := run
				update.Status = domain.PipelineRunStatusFailed
				update.FailureCode = "execution_not_accepted"
				update.Error = "pipeline execution was not accepted before recovery timeout"
				finished := now
				update.FinishedAt = &finished
				update.UpdatedAt = now
				update.LastReconciledAt = &now
				updated, updateErr := m.repo.UpdateRunConditional(ctx, update)
				if updateErr != nil {
					return run
				}
				return updated
			}
			return run
		}
		if errors.Is(err, kestra.ErrNotFound) && run.AttemptCount+1 >= 3 {
			update := run
			update.AttemptCount++
			update.Status = domain.PipelineRunStatusFailed
			update.FailureCode = "execution_not_found"
			update.Error = "pipeline execution was not found after retries"
			finished := now
			update.FinishedAt = &finished
			update.UpdatedAt = now
			update.LastReconciledAt = &now
			updated, updateErr := m.repo.UpdateRunConditional(ctx, update)
			if updateErr != nil {
				return run
			}
			return updated
		}
		if errors.Is(err, kestra.ErrNotFound) {
			update := run
			update.AttemptCount++
			update.UpdatedAt = now
			update.LastReconciledAt = &now
			updated, updateErr := m.repo.UpdateRunConditional(ctx, update)
			if updateErr != nil {
				return run
			}
			return updated
		}
		// Fail closed without terminalizing: the engine may recover.
		return run
	}
	status, failureCode := mapExecutionStatus(execution.Status)
	update := run
	update.ExecutionID = execution.ID
	update.LastReconciledAt = &now
	update.UpdatedAt = now
	update.Status = status
	if failureCode != "" {
		update.FailureCode = failureCode
	}
	if status == domain.PipelineRunStatusSucceeded {
		required, artifactErr := m.dbtArtifactRequired(ctx, update)
		if artifactErr != nil {
			return m.failRunBeforeOutputs(ctx, update, "run_results_unavailable", "pipeline dbt artifact could not be checked")
		}
		if required && m.supportsRunResults() {
			raw, readerErr := m.readRunResults(ctx, execution.ID)
			if readerErr != nil {
				slog.Default().Warn(
					"pipeline dbt artifact read failed",
					"tenant_id", update.TenantID,
					"pipeline_id", update.PipelineID,
					"run_id", update.ID,
					"execution_id", execution.ID,
					"error", readerErr,
				)
				return m.failRunBeforeOutputs(ctx, update, "run_results_unavailable", "pipeline dbt artifact is unavailable")
			}
			summary, parseErr := dbt.ParseRunResults(raw)
			if parseErr != nil {
				return m.failRunBeforeOutputs(ctx, update, "run_results_invalid", "pipeline dbt artifact is invalid")
			}
			if summary.Failed > 0 || summary.Total == 0 {
				return m.failRunBeforeOutputs(ctx, update, "dbt_results_failed", "pipeline dbt results did not all succeed")
			}
			slog.Default().Info(
				"pipeline dbt run results",
				"tenant_id", update.TenantID,
				"pipeline_id", update.PipelineID,
				"run_id", update.ID,
				"total", summary.Total,
				"success", summary.Success,
				"failed", summary.Failed,
				"skipped", summary.Skipped,
			)
		}
		outputs, outputErr := m.runOutputs(ctx, update)
		if outputErr != nil {
			update.Status = domain.PipelineRunStatusRunning
			update.FailureCode = "output_writeback_failed"
		} else if terminalRunStatus(update.Status) {
			finished := now
			update.FinishedAt = &finished
			update.Error = ""
			updated, writeErr := m.repo.MarkSucceededWithOutputs(ctx, update, outputs)
			if writeErr != nil {
				update.Status = domain.PipelineRunStatusRunning
				update.FailureCode = "output_writeback_failed"
				updated, writeErr = m.repo.UpdateRunConditional(ctx, update)
				if writeErr != nil {
					return run
				}
			}
			return updated
		}
	}
	if terminalRunStatus(update.Status) {
		finished := now
		update.FinishedAt = &finished
		if update.Status == domain.PipelineRunStatusSucceeded {
			update.Error = ""
		} else if update.Error == "" {
			update.Error = "pipeline execution failed"
		}
	}
	updated, updateErr := m.repo.UpdateRunConditional(ctx, update)
	if updateErr != nil {
		return run
	}
	return updated
}

// dbtArtifactRequired reports whether the published Pipeline has any dbt task.
// Non-dbt pipelines have no run_results artifact to reconcile.
func (m *Manager) dbtArtifactRequired(ctx context.Context, run domain.PipelineRun) (bool, error) {
	pipeline, err := m.repo.GetPipeline(ctx, run.TenantID, run.PipelineID)
	if err != nil {
		return false, err
	}
	dag, err := domain.ParseDAG(pipeline.DAG)
	if err != nil {
		return false, err
	}
	for _, task := range dag.Tasks {
		if task.Type == domain.TaskTypeDBT {
			return true, nil
		}
	}
	return false, nil
}

// readRunResults uses the optional artifact reader implemented by the real
// engine adapter. The in-memory adapter deliberately does not implement it, so
// local tests keep their deterministic execution model.
func (m *Manager) supportsRunResults() bool {
	_, ok := m.adapter.(interface {
		GetRunResults(context.Context, string) ([]byte, error)
	})
	return ok
}

func (m *Manager) readRunResults(ctx context.Context, executionID string) ([]byte, error) {
	reader, ok := m.adapter.(interface {
		GetRunResults(context.Context, string) ([]byte, error)
	})
	if !ok {
		return nil, kestra.ErrNotConfigured
	}
	return reader.GetRunResults(ctx, executionID)
}

// failRunBeforeOutputs terminalises a run that declared dbt work but whose
// artifact could not be reconciled. It never writes dataset outputs.
func (m *Manager) failRunBeforeOutputs(
	ctx context.Context,
	run domain.PipelineRun,
	code, message string,
) domain.PipelineRun {
	now := m.now().UTC()
	update := run
	update.Status = domain.PipelineRunStatusFailed
	update.FailureCode = code
	update.Error = message
	update.LastReconciledAt = &now
	update.UpdatedAt = now
	finished := now
	update.FinishedAt = &finished
	updated, err := m.repo.UpdateRunConditional(ctx, update)
	if err != nil {
		return run
	}
	return updated
}

func (m *Manager) runOutputs(ctx context.Context, run domain.PipelineRun) ([]domain.PipelineRunOutput, error) {
	pipeline, err := m.repo.GetPipeline(ctx, run.TenantID, run.PipelineID)
	if err != nil {
		return nil, err
	}
	dag, err := domain.ParseDAG(pipeline.DAG)
	if err != nil {
		return nil, err
	}
	catalog := m.dbtCatalog
	if catalog == "" {
		catalog = dbt.DefaultCatalog
	}
	project, err := dbt.RenderProject(run.TenantID, run.PipelineID, run.Version, dag, catalog)
	if err != nil {
		return nil, err
	}
	outputs := make([]domain.PipelineRunOutput, 0, len(dag.Outputs))
	now := m.now().UTC()
	for _, output := range dag.Outputs {
		if output.DatasetID == "" || output.Model == "" {
			continue
		}
		outputs = append(outputs, domain.PipelineRunOutput{
			PipelineRunID: run.ID,
			TenantID:      run.TenantID,
			DatasetID:     output.DatasetID,
			Model:         output.Model,
			Catalog:       catalog,
			Schema:        project.Schema,
			Table:         output.Model,
			CreatedAt:     now,
		})
	}
	return outputs, nil
}

// GetPipelineRun returns one run scoped to tenant and Pipeline.
func (m *Manager) GetPipelineRun(ctx context.Context, request GetPipelineRunRequest) (domain.PipelineRun, error) {
	if err := m.requireRepository(); err != nil {
		return domain.PipelineRun{}, err
	}
	tenantID, err := normalizeTenant(request.TenantID)
	if err != nil {
		return domain.PipelineRun{}, err
	}
	pipelineID, err := normalizeUUID(request.PipelineID, "pipeline_id")
	if err != nil {
		return domain.PipelineRun{}, err
	}
	runID, err := normalizeUUID(request.RunID, "run_id")
	if err != nil {
		return domain.PipelineRun{}, err
	}
	run, err := m.repo.GetRun(ctx, tenantID, pipelineID, runID)
	if err != nil {
		return domain.PipelineRun{}, err
	}
	return m.reconcileRun(ctx, run), nil
}

// ListPipelineRuns returns a page of runs for one Pipeline.
func (m *Manager) ListPipelineRuns(ctx context.Context, request ListPipelineRunsRequest) (PipelineRunPage, error) {
	if err := m.requireRepository(); err != nil {
		return PipelineRunPage{}, err
	}
	tenantID, err := normalizeTenant(request.TenantID)
	if err != nil {
		return PipelineRunPage{}, err
	}
	pipelineID, err := normalizeUUID(request.PipelineID, "pipeline_id")
	if err != nil {
		return PipelineRunPage{}, err
	}
	if _, err := m.repo.GetPipeline(ctx, tenantID, pipelineID); err != nil {
		return PipelineRunPage{}, err
	}
	page, perPage, err := normalizePage(request.Page, request.PerPage)
	if err != nil {
		return PipelineRunPage{}, err
	}
	if request.Status != "" && !request.Status.Valid() {
		return PipelineRunPage{}, fmt.Errorf("%w: unsupported run status %q", domain.ErrInvalidArgument, request.Status)
	}
	items, total, err := m.repo.ListRuns(ctx, RunFilter{
		TenantID:   tenantID,
		PipelineID: pipelineID,
		Page:       page,
		PerPage:    perPage,
		Status:     request.Status,
	})
	if err != nil {
		return PipelineRunPage{}, err
	}
	reconciled := 0
	for index := range items {
		if reconciled >= 10 {
			break
		}
		if terminalRunStatus(items[index].Status) {
			continue
		}
		items[index] = m.reconcileRun(ctx, items[index])
		reconciled++
	}
	return PipelineRunPage{Items: items, Page: pageInfo(page, perPage, total)}, nil
}
