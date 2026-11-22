// Package service defines the Ontology application service and its storage
// port. The service layer owns validation, identifiers, timestamps and
// cross-entity business rules; it never imports HTTP or SQL packages.
package service

import (
	"context"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
)

// Store is the persistence port used by the Ontology service. Every method
// receives a tenant ID; implementations MUST scope all SQL by tenant_id.
type Store interface {
	CreateObjectType(ctx context.Context, objectType *domain.ObjectType) error
	GetObjectType(ctx context.Context, tenantID, id string) (*domain.ObjectType, error)
	ListObjectTypes(ctx context.Context, tenantID string, offset, limit int) ([]domain.ObjectType, int, error)
	UpdateObjectType(ctx context.Context, objectType *domain.ObjectType) error
	DeleteObjectType(ctx context.Context, tenantID, id string, deletedAt time.Time) error
	HasLinkTypesReferencing(ctx context.Context, tenantID, objectTypeID string) (bool, error)

	CreateProperty(ctx context.Context, property *domain.Property) error
	GetProperty(ctx context.Context, tenantID, id string) (*domain.Property, error)
	ListProperties(ctx context.Context, tenantID, objectTypeID string) ([]domain.Property, error)
	UpdateProperty(ctx context.Context, property *domain.Property) error
	DeleteProperty(ctx context.Context, tenantID, id string, deletedAt time.Time) error

	CreateLinkType(ctx context.Context, linkType *domain.LinkType) error
	GetLinkType(ctx context.Context, tenantID, id string) (*domain.LinkType, error)
	ListLinkTypes(ctx context.Context, tenantID string, offset, limit int) ([]domain.LinkType, int, error)
	UpdateLinkType(ctx context.Context, linkType *domain.LinkType) error
	DeleteLinkType(ctx context.Context, tenantID, id string, deletedAt time.Time) error

	SetDatasetBinding(ctx context.Context, tenantID, objectTypeID, datasetID, syncMode string, updatedAt time.Time) error
	ClearDatasetBinding(ctx context.Context, tenantID, objectTypeID string, updatedAt time.Time) error
}

// Service is the public application interface of the Ontology module.
type Service interface {
	CreateObjectType(ctx context.Context, tenantID string, in CreateObjectTypeInput) (*domain.ObjectType, error)
	GetObjectType(ctx context.Context, tenantID, id string) (*domain.ObjectType, error)
	ListObjectTypes(ctx context.Context, tenantID string, page domain.Page) (*domain.PageResult[domain.ObjectType], error)
	UpdateObjectType(ctx context.Context, tenantID, id string, in UpdateObjectTypeInput) (*domain.ObjectType, error)
	DeleteObjectType(ctx context.Context, tenantID, id string) error

	CreateProperty(ctx context.Context, tenantID string, in CreatePropertyInput) (*domain.Property, error)
	ListProperties(ctx context.Context, tenantID, objectTypeID string) ([]domain.Property, error)
	UpdateProperty(ctx context.Context, tenantID, id string, in UpdatePropertyInput) (*domain.Property, error)
	DeleteProperty(ctx context.Context, tenantID, id string) error

	CreateLinkType(ctx context.Context, tenantID string, in CreateLinkTypeInput) (*domain.LinkType, error)
	ListLinkTypes(ctx context.Context, tenantID string, page domain.Page) (*domain.PageResult[domain.LinkType], error)
	UpdateLinkType(ctx context.Context, tenantID, id string, in UpdateLinkTypeInput) (*domain.LinkType, error)
	DeleteLinkType(ctx context.Context, tenantID, id string) error

	BindDataset(ctx context.Context, tenantID string, in BindDatasetInput) (*domain.ObjectType, error)
	UnbindDataset(ctx context.Context, tenantID, objectTypeID string) (*domain.ObjectType, error)
	MaterializeBinding(ctx context.Context, tenantID, objectTypeID string) (string, error)
}

// CreateObjectTypeInput is the create payload for an object type.
type CreateObjectTypeInput struct {
	Name               string
	DisplayName        string
	Description        string
	PrimaryKeyProperty string
}

// UpdateObjectTypeInput contains mutable object type fields. Nil fields are
// left unchanged.
type UpdateObjectTypeInput struct {
	DisplayName *string
	Description *string
}

// CreatePropertyInput is the create payload for a property.
type CreatePropertyInput struct {
	ObjectTypeID     string
	Name             string
	DisplayName      string
	Description      string
	PropertyType     string
	Required         bool
	UniqueConstraint bool
}

// UpdatePropertyInput contains mutable property fields. Nil fields are left
// unchanged.
type UpdatePropertyInput struct {
	DisplayName      *string
	Description      *string
	Required         *bool
	UniqueConstraint *bool
}

// CreateLinkTypeInput is the create payload for a link type.
type CreateLinkTypeInput struct {
	Name         string
	DisplayName  string
	Description  string
	SourceTypeID string
	TargetTypeID string
	Cardinality  domain.Cardinality
}

// UpdateLinkTypeInput contains mutable link type fields. Nil fields are left
// unchanged.
type UpdateLinkTypeInput struct {
	DisplayName *string
	Description *string
}

// BindDatasetInput is the dataset binding payload.
type BindDatasetInput struct {
	ObjectTypeID string
	DatasetID    string
	SyncMode     string
}
