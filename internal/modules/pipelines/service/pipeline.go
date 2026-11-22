package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

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

// PublishPipeline snapshots the current DAG, registers a Flow and increments
// the published version.
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
	if err := domain.ValidateDAG(pipeline.DAG); err != nil {
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
	nextVersion := pipeline.Version + 1
	if err := m.adapter.RegisterFlow(ctx, kestra.Flow{
		TenantID:   tenantID,
		PipelineID: pipelineID,
		Name:       pipeline.Name,
		Version:    nextVersion,
		DAG:        pipeline.DAG,
	}); err != nil {
		return domain.Pipeline{}, domain.PipelineVersion{}, fmt.Errorf("%w: kestra Flow registration failed", domain.ErrUnavailable)
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
	return m.repo.PublishPipeline(ctx, pipeline, version)
}

// TriggerPipelineRun starts one manual execution through the adapter. On
// adapter failure the failed run record is persisted without leaking provider
// credentials or stack traces.
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
		Status:      domain.PipelineRunStatusRunning,
		TriggerType: domain.TriggerTypeManual,
		StartedAt:   &now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, triggerErr := m.adapter.TriggerRun(ctx, kestra.RunRequest{
		TenantID:    tenantID,
		PipelineID:  pipelineID,
		Version:     pipeline.Version,
		TriggerType: string(domain.TriggerTypeManual),
		DAG:         pipeline.DAG,
	})
	if triggerErr != nil {
		finished := m.now().UTC()
		run.Status = domain.PipelineRunStatusFailed
		run.FinishedAt = &finished
		run.Error = "kestra trigger failed"
		run.UpdatedAt = finished
		if _, storeErr := m.repo.CreateRun(ctx, run); storeErr != nil {
			return domain.PipelineRun{}, errors.Join(fmt.Errorf("%w: kestra trigger failed", domain.ErrUnavailable), storeErr)
		}
		return domain.PipelineRun{}, fmt.Errorf("%w: kestra trigger failed", domain.ErrUnavailable)
	}
	return m.repo.CreateRun(ctx, run)
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
	return m.repo.GetRun(ctx, tenantID, pipelineID, runID)
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
	return PipelineRunPage{Items: items, Page: pageInfo(page, perPage, total)}, nil
}
