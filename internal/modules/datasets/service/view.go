package service

import (
	"context"
	"strings"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

func (s *impl) CreateView(ctx context.Context, tenantID, datasetID string, input CreateViewInput) (domain.View, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.View{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return domain.View{}, err
	}
	name, err := normalizeName("name", input.Name)
	if err != nil {
		return domain.View{}, err
	}
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return domain.View{}, errorf(domain.ErrInvalidArgument, "query must not be empty")
	}
	branchID := ""
	if input.BranchID != "" {
		branch, err := s.getBranch(ctx, dataset.TenantID, dataset.ID, input.BranchID)
		if err != nil {
			return domain.View{}, err
		}
		branchID = branch.ID
	}
	id, err := newID()
	if err != nil {
		return domain.View{}, err
	}
	now := time.Now().UTC()
	view := domain.View{
		ID:        id,
		TenantID:  dataset.TenantID,
		DatasetID: dataset.ID,
		BranchID:  branchID,
		Name:      name,
		Query:     query,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repo.CreateView(ctx, view); err != nil {
		return domain.View{}, err
	}
	return view, nil
}

func (s *impl) GetView(ctx context.Context, tenantID, datasetID, viewID string) (domain.View, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.View{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return domain.View{}, err
	}
	id, err := normalizeUUID("view_id", viewID)
	if err != nil {
		return domain.View{}, err
	}
	view, err := repo.GetView(ctx, dataset.TenantID, dataset.ID, id)
	if err != nil {
		return domain.View{}, err
	}
	if view.TenantID != dataset.TenantID || view.DatasetID != dataset.ID {
		return domain.View{}, domain.ErrNotFound
	}
	return view, nil
}

func (s *impl) ListViews(ctx context.Context, tenantID, datasetID string, page, perPage int) (ViewPage, error) {
	repo, err := s.repository()
	if err != nil {
		return ViewPage{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return ViewPage{}, err
	}
	meta, offset, limit, err := normalizePage(page, perPage)
	if err != nil {
		return ViewPage{}, err
	}
	items, total, err := repo.ListViews(ctx, dataset.TenantID, dataset.ID, offset, limit)
	if err != nil {
		return ViewPage{}, err
	}
	if items == nil {
		items = []domain.View{}
	}
	return ViewPage{Items: items, Page: finishPage(meta, total)}, nil
}

func (s *impl) UpdateView(ctx context.Context, tenantID, datasetID, viewID string, input UpdateViewInput) (domain.View, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.View{}, err
	}
	view, err := s.GetView(ctx, tenantID, datasetID, viewID)
	if err != nil {
		return domain.View{}, err
	}
	if input.Query != nil {
		query := strings.TrimSpace(*input.Query)
		if query == "" {
			return domain.View{}, errorf(domain.ErrInvalidArgument, "query must not be empty")
		}
		view.Query = query
		view.Version++
	}
	view.UpdatedAt = time.Now().UTC()
	if err := repo.UpdateView(ctx, view); err != nil {
		return domain.View{}, err
	}
	return view, nil
}

func (s *impl) DeleteView(ctx context.Context, tenantID, datasetID, viewID string) error {
	repo, err := s.repository()
	if err != nil {
		return err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return err
	}
	id, err := normalizeUUID("view_id", viewID)
	if err != nil {
		return err
	}
	return repo.DeleteView(ctx, dataset.TenantID, dataset.ID, id, time.Now().UTC())
}
