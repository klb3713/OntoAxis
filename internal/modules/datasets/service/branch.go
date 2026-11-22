package service

import (
	"context"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

func (s *impl) CreateBranch(ctx context.Context, tenantID, datasetID string, input CreateBranchInput) (domain.Branch, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.Branch{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return domain.Branch{}, err
	}
	name, err := normalizeName("name", input.Name)
	if err != nil {
		return domain.Branch{}, err
	}
	parentBranchID := ""
	createdFrom := ""
	if input.ParentBranchID != "" {
		parent, err := s.getBranch(ctx, dataset.TenantID, dataset.ID, input.ParentBranchID)
		if err != nil {
			return domain.Branch{}, err
		}
		parentBranchID = parent.ID
		createdFrom = parent.HeadTransactionID
	}
	id, err := newID()
	if err != nil {
		return domain.Branch{}, err
	}
	now := time.Now().UTC()
	branch := domain.Branch{
		ID:                       id,
		TenantID:                 dataset.TenantID,
		DatasetID:                dataset.ID,
		Name:                     name,
		ParentBranchID:           parentBranchID,
		CreatedFromTransactionID: createdFrom,
		FallbackChain:            []string{},
		Labels:                   map[string]string{},
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	if err := repo.CreateBranch(ctx, branch); err != nil {
		return domain.Branch{}, err
	}
	return branch, nil
}

func (s *impl) GetBranch(ctx context.Context, tenantID, datasetID, branchID string) (domain.Branch, error) {
	if _, err := s.getDataset(ctx, tenantID, datasetID); err != nil {
		return domain.Branch{}, err
	}
	return s.getBranch(ctx, tenantID, datasetID, branchID)
}

func (s *impl) ListBranches(ctx context.Context, tenantID, datasetID string) ([]domain.Branch, error) {
	repo, err := s.repository()
	if err != nil {
		return nil, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return nil, err
	}
	branches, err := repo.ListBranches(ctx, dataset.TenantID, dataset.ID)
	if err != nil {
		return nil, err
	}
	if branches == nil {
		branches = []domain.Branch{}
	}
	return branches, nil
}
