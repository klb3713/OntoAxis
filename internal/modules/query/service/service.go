package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	objectsdomain "github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	objectsservice "github.com/klb3713/ontoaxis/internal/modules/objects/service"
	"github.com/klb3713/ontoaxis/internal/modules/query/domain"
	"github.com/klb3713/ontoaxis/internal/modules/query/repo"
)

// Executor is the SQL engine boundary; a Trino client or a test fake can
// implement it without the HTTP layer depending on a concrete engine.
type Executor interface {
	Execute(ctx context.Context, sql string, limit int) (domain.QueryResult, error)
	Explain(ctx context.Context, sql string) (string, error)
}

// Service owns saved-query CRUD and SQL execution use cases.
type Service interface {
	Execute(ctx context.Context, sql string, limit int) (domain.QueryResult, error)
	Explain(ctx context.Context, sql string) (string, error)
	SaveQuery(ctx context.Context, tenantID, ownerID uuid.UUID, name, sql string) (domain.SavedQuery, error)
	ListSavedQueries(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]domain.SavedQuery, int64, error)
	DeleteSavedQuery(ctx context.Context, tenantID, id uuid.UUID) error
	QueryObjects(ctx context.Context, tenantID, objectTypeID uuid.UUID, limit, offset int) ([]objectsdomain.Object, int64, error)
}

type impl struct {
	store    repo.Store
	executor Executor
	objects  objectsservice.Service
}

func New(store repo.Store, executor Executor, objects objectsservice.Service) Service {
	return &impl{store: store, executor: executor, objects: objects}
}

func (s *impl) Execute(ctx context.Context, sql string, limit int) (domain.QueryResult, error) {
	if err := domain.ValidateSQL(sql); err != nil {
		return domain.QueryResult{}, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if s.executor == nil {
		return domain.QueryResult{}, domain.ErrUnavailable
	}
	return s.executor.Execute(ctx, sql, limit)
}

func (s *impl) Explain(ctx context.Context, sql string) (string, error) {
	if err := domain.ValidateSQL(sql); err != nil {
		return "", err
	}
	if s.executor == nil {
		return "", domain.ErrUnavailable
	}
	return s.executor.Explain(ctx, sql)
}

func (s *impl) SaveQuery(ctx context.Context, tenantID, ownerID uuid.UUID, name, sql string) (domain.SavedQuery, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || strings.TrimSpace(sql) == "" {
		return domain.SavedQuery{}, domain.ErrInvalidInput
	}
	if err := domain.ValidateSQL(sql); err != nil {
		return domain.SavedQuery{}, err
	}
	if tenantID == uuid.Nil {
		return domain.SavedQuery{}, domain.ErrInvalidInput
	}
	return s.store.CreateSavedQuery(ctx, tenantID, name, sql, ownerID)
}

func (s *impl) ListSavedQueries(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]domain.SavedQuery, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListSavedQueries(ctx, tenantID, limit, offset)
}

func (s *impl) DeleteSavedQuery(ctx context.Context, tenantID, id uuid.UUID) error {
	if tenantID == uuid.Nil || id == uuid.Nil {
		return domain.ErrInvalidInput
	}
	return s.store.DeleteSavedQuery(ctx, tenantID, id)
}

func (s *impl) QueryObjects(ctx context.Context, tenantID, objectTypeID uuid.UUID, limit, offset int) ([]objectsdomain.Object, int64, error) {
	if tenantID == uuid.Nil {
		return nil, 0, domain.ErrInvalidInput
	}
	if s.objects == nil {
		return nil, 0, domain.ErrUnavailable
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.objects.ListObjects(ctx, tenantID, objectTypeID, limit, offset)
}
