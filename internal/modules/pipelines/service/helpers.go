package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
)

var pipelineNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func normalizeTenant(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", domain.ErrTenantRequired
	}
	return normalizeUUID(trimmed, "tenant_id")
}

func normalizeUUID(value, field string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("%w: %s must be a valid UUID", domain.ErrInvalidArgument, field)
	}
	return parsed.String(), nil
}

func normalizePage(page, perPage int) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if perPage == 0 {
		perPage = 50
	}
	if page < 1 {
		return 0, 0, fmt.Errorf("%w: page must be at least 1", domain.ErrInvalidArgument)
	}
	if perPage < 1 || perPage > 200 {
		return 0, 0, fmt.Errorf("%w: per_page must be between 1 and 200", domain.ErrInvalidArgument)
	}
	return page, perPage, nil
}

func pageInfo(page, perPage, total int) PageInfo {
	totalPages := 0
	if total > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	return PageInfo{Page: page, PerPage: perPage, Total: total, TotalPages: totalPages}
}

func newUUID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("%w: cannot allocate id", domain.ErrUnavailable)
	}
	return id.String(), nil
}

func validatePipelineName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if !pipelineNamePattern.MatchString(trimmed) {
		return "", fmt.Errorf("%w: name must be 1-128 characters and contain only letters, digits, '_', '-' or '.'", domain.ErrInvalidArgument)
	}
	return trimmed, nil
}

func normalizeDAG(dag json.RawMessage) (json.RawMessage, error) {
	if len(dag) == 0 {
		dag = json.RawMessage(`{}`)
	}
	if err := domain.ValidateDAG(dag); err != nil {
		return nil, err
	}
	return dag, nil
}

func (m *Manager) requireRepository() error {
	if m == nil || m.repo == nil {
		return fmt.Errorf("%w: repository is not configured", domain.ErrUnavailable)
	}
	return nil
}

// validateDatasetReferences fails closed when the DAG references datasets but
// the datasets module adapter is missing. It never reads dataset tables.
func (m *Manager) validateDatasetReferences(ctx context.Context, tenantID string, dag json.RawMessage) error {
	references := append(domain.DatasetIDsFromDAG(dag), domain.OutputDatasetIDsFromDAG(dag)...)
	if len(references) == 0 {
		return nil
	}
	if m.datasetResolver == nil {
		return fmt.Errorf("%w: dataset resolver is not configured", domain.ErrUnavailable)
	}
	for _, reference := range references {
		datasetID, err := normalizeUUID(reference, "dataset_id")
		if err != nil {
			return err
		}
		exists, err := m.datasetResolver.DatasetExists(ctx, tenantID, datasetID)
		if err != nil {
			return fmt.Errorf("%w: dataset lookup failed", domain.ErrUnavailable)
		}
		if !exists {
			return fmt.Errorf("%w: dataset %q does not exist", domain.ErrInvalidArgument, reference)
		}
	}
	return nil
}
