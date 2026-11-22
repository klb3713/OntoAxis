package service

import (
	"context"
	"fmt"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

func (s *impl) CreateDataset(ctx context.Context, tenantID string, input CreateDatasetInput) (CreateDatasetResult, error) {
	repo, err := s.repository()
	if err != nil {
		return CreateDatasetResult{}, err
	}
	tenant, err := normalizeTenant(tenantID)
	if err != nil {
		return CreateDatasetResult{}, err
	}
	name, err := normalizeName("name", input.Name)
	if err != nil {
		return CreateDatasetResult{}, err
	}
	displayName, err := normalizeDisplayName(input.DisplayName)
	if err != nil {
		return CreateDatasetResult{}, err
	}
	format, err := domain.ParseDatasetFormat(input.Format)
	if err != nil {
		return CreateDatasetResult{}, err
	}
	ownerID := ""
	if input.OwnerID != "" {
		ownerID, err = normalizeUUID("owner_id", input.OwnerID)
		if err != nil {
			return CreateDatasetResult{}, err
		}
	}

	datasetID, err := newID()
	if err != nil {
		return CreateDatasetResult{}, err
	}
	branchID, err := newID()
	if err != nil {
		return CreateDatasetResult{}, err
	}
	now := time.Now().UTC()
	dataset := domain.Dataset{
		ID:              datasetID,
		TenantID:        tenant,
		Name:            name,
		DisplayName:     displayName,
		Description:     input.Description,
		RID:             domain.DatasetRID(datasetID),
		Format:          format,
		StorageLocation: input.StorageLocation,
		OwnerID:         ownerID,
		Tags:            copyTags(input.Tags),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	mainBranch := domain.Branch{
		ID:        branchID,
		TenantID:  tenant,
		DatasetID: datasetID,
		Name:      "main",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repo.CreateDataset(ctx, dataset, mainBranch); err != nil {
		return CreateDatasetResult{}, err
	}
	return CreateDatasetResult{Dataset: dataset, MainBranch: mainBranch}, nil
}

func (s *impl) GetDataset(ctx context.Context, tenantID, datasetID string) (domain.Dataset, error) {
	return s.getDataset(ctx, tenantID, datasetID)
}

func (s *impl) ListDatasets(ctx context.Context, tenantID string, page, perPage int) (DatasetPage, error) {
	repo, err := s.repository()
	if err != nil {
		return DatasetPage{}, err
	}
	tenant, err := normalizeTenant(tenantID)
	if err != nil {
		return DatasetPage{}, err
	}
	meta, offset, limit, err := normalizePage(page, perPage)
	if err != nil {
		return DatasetPage{}, err
	}
	items, total, err := repo.ListDatasets(ctx, tenant, offset, limit)
	if err != nil {
		return DatasetPage{}, err
	}
	if items == nil {
		items = []domain.Dataset{}
	}
	return DatasetPage{Items: items, Page: finishPage(meta, total)}, nil
}

func (s *impl) UpdateDataset(ctx context.Context, tenantID, datasetID string, input UpdateDatasetInput) (domain.Dataset, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.Dataset{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return domain.Dataset{}, err
	}
	if input.DisplayName != nil {
		displayName, err := normalizeDisplayName(*input.DisplayName)
		if err != nil {
			return domain.Dataset{}, err
		}
		dataset.DisplayName = displayName
	}
	if input.Description != nil {
		dataset.Description = *input.Description
	}
	dataset.UpdatedAt = time.Now().UTC()
	if err := repo.UpdateDataset(ctx, dataset); err != nil {
		return domain.Dataset{}, fmt.Errorf("update dataset: %w", err)
	}
	return dataset, nil
}

func (s *impl) DeleteDataset(ctx context.Context, tenantID, datasetID string) error {
	repo, err := s.repository()
	if err != nil {
		return err
	}
	tenant, err := normalizeTenant(tenantID)
	if err != nil {
		return err
	}
	id, err := normalizeUUID("dataset_id", datasetID)
	if err != nil {
		return err
	}
	return repo.DeleteDataset(ctx, tenant, id, time.Now().UTC())
}
