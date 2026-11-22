package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

func errorf(err error, format string, args ...any) error {
	return fmt.Errorf("%w: %s", err, fmt.Sprintf(format, args...))
}

func (s *impl) CreateFile(ctx context.Context, tenantID, datasetID string, input CreateFileInput) (domain.File, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.File{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return domain.File{}, err
	}
	objectKey := strings.TrimSpace(input.ObjectKey)
	if objectKey == "" {
		return domain.File{}, errorf(domain.ErrInvalidArgument, "object_key must not be empty")
	}
	if input.SizeBytes < 0 {
		return domain.File{}, errorf(domain.ErrInvalidArgument, "size_bytes must not be negative")
	}
	if input.RowCount < 0 {
		return domain.File{}, errorf(domain.ErrInvalidArgument, "row_count must not be negative")
	}
	branchID := ""
	if input.BranchID != "" {
		branch, err := s.getBranch(ctx, dataset.TenantID, dataset.ID, input.BranchID)
		if err != nil {
			return domain.File{}, err
		}
		branchID = branch.ID
	}
	id, err := newID()
	if err != nil {
		return domain.File{}, err
	}
	now := time.Now().UTC()
	file := domain.File{
		ID:          id,
		TenantID:    dataset.TenantID,
		DatasetID:   dataset.ID,
		BranchID:    branchID,
		ObjectKey:   objectKey,
		SizeBytes:   input.SizeBytes,
		RowCount:    input.RowCount,
		ETag:        input.ETag,
		ContentType: input.ContentType,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := repo.CreateFile(ctx, file); err != nil {
		return domain.File{}, err
	}
	return file, nil
}

func (s *impl) GetFile(ctx context.Context, tenantID, datasetID, fileID string) (domain.File, error) {
	repo, err := s.repository()
	if err != nil {
		return domain.File{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return domain.File{}, err
	}
	id, err := normalizeUUID("file_id", fileID)
	if err != nil {
		return domain.File{}, err
	}
	file, err := repo.GetFile(ctx, dataset.TenantID, dataset.ID, id)
	if err != nil {
		return domain.File{}, err
	}
	if file.TenantID != dataset.TenantID || file.DatasetID != dataset.ID {
		return domain.File{}, domain.ErrNotFound
	}
	return file, nil
}

func (s *impl) ListFiles(ctx context.Context, tenantID, datasetID, branchID string, page, perPage int) (FilePage, error) {
	repo, err := s.repository()
	if err != nil {
		return FilePage{}, err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return FilePage{}, err
	}
	normalizedBranchID := ""
	if branchID != "" {
		branch, err := s.getBranch(ctx, dataset.TenantID, dataset.ID, branchID)
		if err != nil {
			return FilePage{}, err
		}
		normalizedBranchID = branch.ID
	}
	meta, offset, limit, err := normalizePage(page, perPage)
	if err != nil {
		return FilePage{}, err
	}
	items, total, err := repo.ListFiles(ctx, dataset.TenantID, dataset.ID, normalizedBranchID, offset, limit)
	if err != nil {
		return FilePage{}, err
	}
	if items == nil {
		items = []domain.File{}
	}
	return FilePage{Items: items, Page: finishPage(meta, total)}, nil
}

func (s *impl) DeleteFile(ctx context.Context, tenantID, datasetID, fileID string) error {
	repo, err := s.repository()
	if err != nil {
		return err
	}
	dataset, err := s.getDataset(ctx, tenantID, datasetID)
	if err != nil {
		return err
	}
	id, err := normalizeUUID("file_id", fileID)
	if err != nil {
		return err
	}
	return repo.DeleteFile(ctx, dataset.TenantID, dataset.ID, id, time.Now().UTC())
}
