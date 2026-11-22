package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	"github.com/klb3713/ontoaxis/internal/modules/objects/repo"
)

// Service owns object/link use cases. Handlers depend on this interface, not
// on the database implementation.
type Service interface {
	CreateObject(ctx context.Context, tenantID, objectTypeID uuid.UUID, properties map[string]any) (domain.Object, error)
	GetObject(ctx context.Context, tenantID, id uuid.UUID) (domain.Object, error)
	ListObjects(ctx context.Context, tenantID, objectTypeID uuid.UUID, limit, offset int) ([]domain.Object, int64, error)
	UpdateObject(ctx context.Context, tenantID, id uuid.UUID, properties map[string]any, replace bool) (domain.Object, error)
	DeleteObject(ctx context.Context, tenantID, id uuid.UUID) error
	CreateLink(ctx context.Context, tenantID, linkTypeID, sourceID, targetID uuid.UUID, properties map[string]any) (domain.Link, error)
	ListLinks(ctx context.Context, tenantID, objectID uuid.UUID, limit, offset int) ([]domain.Link, int64, error)
	DeleteLink(ctx context.Context, tenantID, id uuid.UUID) error
}

// ObjectTypeValidator checks that an object type exists for a tenant.
type ObjectTypeValidator interface {
	ObjectTypeExists(ctx context.Context, tenantID, objectTypeID string) (bool, error)
}

// Option customizes the service graph.
type Option func(*impl)

// WithObjectTypeValidator injects the ontology boundary validator.
func WithObjectTypeValidator(v ObjectTypeValidator) Option {
	return func(s *impl) { s.objectTypeValidator = v }
}

type impl struct {
	store               repo.Store
	objectTypeValidator ObjectTypeValidator
}

// New returns the default service backed by a repository.
func New(store repo.Store, opts ...Option) Service {
	s := &impl{store: store}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *impl) CreateObject(ctx context.Context, tenantID, objectTypeID uuid.UUID, properties map[string]any) (domain.Object, error) {
	if err := domain.ValidateObject(objectTypeID, properties); err != nil {
		return domain.Object{}, err
	}
	if s.objectTypeValidator != nil {
		exists, err := s.objectTypeValidator.ObjectTypeExists(ctx, tenantID.String(), objectTypeID.String())
		if err != nil {
			return domain.Object{}, err
		}
		if !exists {
			return domain.Object{}, domain.ErrInvalidInput
		}
	}
	return s.store.CreateObject(ctx, tenantID, objectTypeID, properties)
}

func (s *impl) GetObject(ctx context.Context, tenantID, id uuid.UUID) (domain.Object, error) {
	return s.store.GetObject(ctx, tenantID, id)
}

func (s *impl) ListObjects(ctx context.Context, tenantID, objectTypeID uuid.UUID, limit, offset int) ([]domain.Object, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListObjects(ctx, tenantID, objectTypeID, limit, offset)
}

func (s *impl) UpdateObject(ctx context.Context, tenantID, id uuid.UUID, properties map[string]any, replace bool) (domain.Object, error) {
	if len(properties) == 0 {
		return domain.Object{}, domain.ErrInvalidInput
	}
	return s.store.UpdateObject(ctx, tenantID, id, properties, replace)
}

func (s *impl) DeleteObject(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.store.DeleteObject(ctx, tenantID, id)
}

func (s *impl) CreateLink(ctx context.Context, tenantID, linkTypeID, sourceID, targetID uuid.UUID, properties map[string]any) (domain.Link, error) {
	if err := domain.ValidateLink(linkTypeID, sourceID, targetID); err != nil {
		return domain.Link{}, err
	}
	return s.store.CreateLink(ctx, tenantID, linkTypeID, sourceID, targetID, properties)
}

func (s *impl) ListLinks(ctx context.Context, tenantID, objectID uuid.UUID, limit, offset int) ([]domain.Link, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListLinks(ctx, tenantID, objectID, limit, offset)
}

func (s *impl) DeleteLink(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.store.DeleteLink(ctx, tenantID, id)
}
