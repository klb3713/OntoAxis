package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

const (
	defaultPerPage = 50
	maxPerPage     = 200
)

type impl struct {
	repo Repository
}

// New returns a Service backed by repo. A nil repository produces a service
// that fails closed with domain.ErrUnavailable instead of panicking.
func New(repo Repository) Service {
	return &impl{repo: repo}
}

func (s *impl) repository() (Repository, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("%w: dataset repository is not configured", domain.ErrUnavailable)
	}
	return s.repo, nil
}

func normalizeTenant(tenantID string) (string, error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", domain.ErrTenantRequired
	}
	return normalizeUUID("tenant_id", tenantID)
}

func normalizeUUID(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %s must be a valid UUID", domain.ErrInvalidArgument, field)
	}
	return parsed.String(), nil
}

func normalizeName(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%w: %s must not be empty", domain.ErrInvalidArgument, field)
	}
	if utf8.RuneCountInString(trimmed) > 128 {
		return "", fmt.Errorf("%w: %s must be at most 128 characters", domain.ErrInvalidArgument, field)
	}
	return trimmed, nil
}

func normalizeDisplayName(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if utf8.RuneCountInString(trimmed) > 255 {
		return "", fmt.Errorf("%w: display_name must be at most 255 characters", domain.ErrInvalidArgument)
	}
	return trimmed, nil
}

func normalizePage(page, perPage int) (Page, int, int, error) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = defaultPerPage
	}
	if perPage > maxPerPage {
		return Page{}, 0, 0, fmt.Errorf("%w: per_page must be at most %d", domain.ErrInvalidArgument, maxPerPage)
	}
	return Page{Page: page, PerPage: perPage}, (page - 1) * perPage, perPage, nil
}

func finishPage(page Page, total int64) Page {
	page.Total = total
	if total == 0 {
		page.TotalPages = 0
		return page
	}
	page.TotalPages = int((total + int64(page.PerPage) - 1) / int64(page.PerPage))
	return page
}

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate uuidv7: %w", err)
	}
	return id.String(), nil
}

func copyTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		out[key] = value
	}
	return out
}

func (s *impl) getDataset(ctx context.Context, tenantID, datasetID string) (domain.Dataset, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.Dataset{}, err
	}
	tenant, err := normalizeTenant(tenantID)
	if err != nil {
		return domain.Dataset{}, err
	}
	id, err := normalizeUUID("dataset_id", datasetID)
	if err != nil {
		return domain.Dataset{}, err
	}
	dataset, err := repo.GetDataset(ctx, tenant, id)
	if err != nil {
		return domain.Dataset{}, err
	}
	if dataset.TenantID != tenant {
		return domain.Dataset{}, domain.ErrNotFound
	}
	return dataset, nil
}

func (s *impl) getBranch(ctx context.Context, tenantID, datasetID, branchID string) (domain.Branch, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.Branch{}, err
	}
	tenant, err := normalizeTenant(tenantID)
	if err != nil {
		return domain.Branch{}, err
	}
	dataset, err := normalizeUUID("dataset_id", datasetID)
	if err != nil {
		return domain.Branch{}, err
	}
	id, err := normalizeUUID("branch_id", branchID)
	if err != nil {
		return domain.Branch{}, err
	}
	branch, err := repo.GetBranch(ctx, tenant, dataset, id)
	if err != nil {
		return domain.Branch{}, err
	}
	if branch.TenantID != tenant || branch.DatasetID != dataset {
		return domain.Branch{}, domain.ErrNotFound
	}
	return branch, nil
}
