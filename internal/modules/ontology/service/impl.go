package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
)

type impl struct {
	store Store
	now   func() time.Time
	newID func() (string, error)
}

var _ Service = (*impl)(nil)

// New creates the Ontology application service backed by store.
func New(store Store) Service {
	return &impl{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		newID: func() (string, error) {
			id, err := uuid.NewV7()
			if err != nil {
				return "", fmt.Errorf("generate uuidv7: %w", err)
			}
			return id.String(), nil
		},
	}
}

func ensureTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: tenant_id is required", domain.ErrInvalidArgument)
	}
	return nil
}

func validateObjectTypeFields(name, displayName, description string) error {
	if err := domain.ValidateName(name); err != nil {
		return err
	}
	if err := domain.ValidateDisplayName(displayName); err != nil {
		return err
	}
	return domain.ValidateDescription(description)
}

func (s *impl) CreateObjectType(ctx context.Context, tenantID string, in CreateObjectTypeInput) (*domain.ObjectType, error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	name := domain.NormalizeName(in.Name)
	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		displayName = name
	}
	if err := validateObjectTypeFields(name, displayName, in.Description); err != nil {
		return nil, err
	}
	primaryKeyProperty := strings.TrimSpace(in.PrimaryKeyProperty)
	if primaryKeyProperty != "" {
		if err := domain.ValidateName(primaryKeyProperty); err != nil {
			return nil, fmt.Errorf("%w: primary_key_property is invalid", domain.ErrInvalidArgument)
		}
	}
	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	now := s.now()
	objectType := &domain.ObjectType{
		ID:                 id,
		TenantID:           tenantID,
		Name:               name,
		DisplayName:        displayName,
		Description:        in.Description,
		PrimaryKeyProperty: primaryKeyProperty,
		Status:             domain.StatusActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.store.CreateObjectType(ctx, objectType); err != nil {
		return nil, err
	}
	return cloneObjectType(objectType), nil
}

func (s *impl) GetObjectType(ctx context.Context, tenantID, id string) (*domain.ObjectType, error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: object type id is required", domain.ErrInvalidArgument)
	}
	if err := domain.ValidateID(id); err != nil {
		return nil, err
	}
	objectType, err := s.store.GetObjectType(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	properties, err := s.store.ListProperties(ctx, tenantID, objectType.ID)
	if err != nil {
		return nil, err
	}
	objectType.Properties = properties
	return cloneObjectType(objectType), nil
}

func (s *impl) ListObjectTypes(ctx context.Context, tenantID string, page domain.Page) (*domain.PageResult[domain.ObjectType], error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(page); err != nil {
		return nil, err
	}
	offset := (page.Page - 1) * page.PerPage
	items, total, err := s.store.ListObjectTypes(ctx, tenantID, offset, page.PerPage)
	if err != nil {
		return nil, err
	}
	for i := range items {
		properties, err := s.store.ListProperties(ctx, tenantID, items[i].ID)
		if err != nil {
			return nil, err
		}
		items[i].Properties = properties
	}
	return &domain.PageResult[domain.ObjectType]{
		Items:   items,
		Total:   total,
		Page:    page.Page,
		PerPage: page.PerPage,
	}, nil
}

func (s *impl) UpdateObjectType(ctx context.Context, tenantID, id string, in UpdateObjectTypeInput) (*domain.ObjectType, error) {
	objectType, err := s.GetObjectType(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if in.DisplayName != nil {
		objectType.DisplayName = *in.DisplayName
	}
	if in.Description != nil {
		objectType.Description = *in.Description
	}
	if err := validateObjectTypeFields(objectType.Name, objectType.DisplayName, objectType.Description); err != nil {
		return nil, err
	}
	objectType.UpdatedAt = s.now()
	if err := s.store.UpdateObjectType(ctx, objectType); err != nil {
		return nil, err
	}
	return cloneObjectType(objectType), nil
}

func (s *impl) DeleteObjectType(ctx context.Context, tenantID, id string) error {
	if _, err := s.GetObjectType(ctx, tenantID, id); err != nil {
		return err
	}
	referenced, err := s.store.HasLinkTypesReferencing(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if referenced {
		return fmt.Errorf("%w: object type is referenced by a link type", domain.ErrFailedPrecondition)
	}
	return s.store.DeleteObjectType(ctx, tenantID, id, s.now())
}

func (s *impl) CreateProperty(ctx context.Context, tenantID string, in CreatePropertyInput) (*domain.Property, error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	name := domain.NormalizeName(in.Name)
	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		displayName = name
	}
	if err := domain.ValidateName(name); err != nil {
		return nil, err
	}
	if err := domain.ValidateDisplayName(displayName); err != nil {
		return nil, err
	}
	if err := domain.ValidateDescription(in.Description); err != nil {
		return nil, err
	}
	if err := domain.ValidatePropertyType(in.PropertyType); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ObjectTypeID) == "" {
		return nil, fmt.Errorf("%w: object_type_id is required", domain.ErrInvalidArgument)
	}
	if err := domain.ValidateID(in.ObjectTypeID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetObjectType(ctx, tenantID, in.ObjectTypeID); err != nil {
		return nil, err
	}
	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	now := s.now()
	property := &domain.Property{
		ID:               id,
		TenantID:         tenantID,
		ObjectTypeID:     in.ObjectTypeID,
		Name:             name,
		DisplayName:      displayName,
		Description:      in.Description,
		PropertyType:     strings.TrimSpace(in.PropertyType),
		Required:         in.Required,
		UniqueConstraint: in.UniqueConstraint,
		Config:           map[string]any{},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.store.CreateProperty(ctx, property); err != nil {
		return nil, err
	}
	return cloneProperty(property), nil
}

func (s *impl) ListProperties(ctx context.Context, tenantID, objectTypeID string) ([]domain.Property, error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	if err := domain.ValidateID(objectTypeID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetObjectType(ctx, tenantID, objectTypeID); err != nil {
		return nil, err
	}
	properties, err := s.store.ListProperties(ctx, tenantID, objectTypeID)
	if err != nil {
		return nil, err
	}
	if properties == nil {
		properties = []domain.Property{}
	}
	return properties, nil
}

func (s *impl) UpdateProperty(ctx context.Context, tenantID, id string, in UpdatePropertyInput) (*domain.Property, error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	if err := domain.ValidateID(id); err != nil {
		return nil, err
	}
	property, err := s.store.GetProperty(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if in.DisplayName != nil {
		property.DisplayName = *in.DisplayName
	}
	if in.Description != nil {
		property.Description = *in.Description
	}
	if in.Required != nil {
		property.Required = *in.Required
	}
	if in.UniqueConstraint != nil {
		property.UniqueConstraint = *in.UniqueConstraint
	}
	if err := domain.ValidateDisplayName(property.DisplayName); err != nil {
		return nil, err
	}
	if err := domain.ValidateDescription(property.Description); err != nil {
		return nil, err
	}
	property.UpdatedAt = s.now()
	if err := s.store.UpdateProperty(ctx, property); err != nil {
		return nil, err
	}
	return cloneProperty(property), nil
}

func (s *impl) DeleteProperty(ctx context.Context, tenantID, id string) error {
	if err := ensureTenant(tenantID); err != nil {
		return err
	}
	if err := domain.ValidateID(id); err != nil {
		return err
	}
	if _, err := s.store.GetProperty(ctx, tenantID, id); err != nil {
		return err
	}
	return s.store.DeleteProperty(ctx, tenantID, id, s.now())
}

func (s *impl) CreateLinkType(ctx context.Context, tenantID string, in CreateLinkTypeInput) (*domain.LinkType, error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	name := domain.NormalizeName(in.Name)
	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		displayName = name
	}
	if err := domain.ValidateName(name); err != nil {
		return nil, err
	}
	if err := domain.ValidateDisplayName(displayName); err != nil {
		return nil, err
	}
	if err := domain.ValidateDescription(in.Description); err != nil {
		return nil, err
	}
	if err := domain.ValidateCardinality(in.Cardinality); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.SourceTypeID) == "" || strings.TrimSpace(in.TargetTypeID) == "" {
		return nil, fmt.Errorf("%w: source_type_id and target_type_id are required", domain.ErrInvalidArgument)
	}
	if err := domain.ValidateID(in.SourceTypeID); err != nil {
		return nil, err
	}
	if err := domain.ValidateID(in.TargetTypeID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetObjectType(ctx, tenantID, in.SourceTypeID); err != nil {
		return nil, fmt.Errorf("source object type: %w", err)
	}
	if _, err := s.store.GetObjectType(ctx, tenantID, in.TargetTypeID); err != nil {
		return nil, fmt.Errorf("target object type: %w", err)
	}
	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	now := s.now()
	linkType := &domain.LinkType{
		ID:           id,
		TenantID:     tenantID,
		Name:         name,
		DisplayName:  displayName,
		Description:  in.Description,
		SourceTypeID: in.SourceTypeID,
		TargetTypeID: in.TargetTypeID,
		Cardinality:  in.Cardinality,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.CreateLinkType(ctx, linkType); err != nil {
		return nil, err
	}
	return cloneLinkType(linkType), nil
}

func (s *impl) ListLinkTypes(ctx context.Context, tenantID string, page domain.Page) (*domain.PageResult[domain.LinkType], error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(page); err != nil {
		return nil, err
	}
	offset := (page.Page - 1) * page.PerPage
	items, total, err := s.store.ListLinkTypes(ctx, tenantID, offset, page.PerPage)
	if err != nil {
		return nil, err
	}
	return &domain.PageResult[domain.LinkType]{
		Items:   items,
		Total:   total,
		Page:    page.Page,
		PerPage: page.PerPage,
	}, nil
}

func (s *impl) UpdateLinkType(ctx context.Context, tenantID, id string, in UpdateLinkTypeInput) (*domain.LinkType, error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	if err := domain.ValidateID(id); err != nil {
		return nil, err
	}
	linkType, err := s.store.GetLinkType(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if in.DisplayName != nil {
		linkType.DisplayName = *in.DisplayName
	}
	if in.Description != nil {
		linkType.Description = *in.Description
	}
	if err := domain.ValidateDisplayName(linkType.DisplayName); err != nil {
		return nil, err
	}
	if err := domain.ValidateDescription(linkType.Description); err != nil {
		return nil, err
	}
	linkType.UpdatedAt = s.now()
	if err := s.store.UpdateLinkType(ctx, linkType); err != nil {
		return nil, err
	}
	return cloneLinkType(linkType), nil
}

func (s *impl) DeleteLinkType(ctx context.Context, tenantID, id string) error {
	if err := ensureTenant(tenantID); err != nil {
		return err
	}
	if err := domain.ValidateID(id); err != nil {
		return err
	}
	if _, err := s.store.GetLinkType(ctx, tenantID, id); err != nil {
		return err
	}
	return s.store.DeleteLinkType(ctx, tenantID, id, s.now())
}

func (s *impl) BindDataset(ctx context.Context, tenantID string, in BindDatasetInput) (*domain.ObjectType, error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	if err := domain.ValidateID(in.ObjectTypeID); err != nil {
		return nil, err
	}
	datasetID := strings.TrimSpace(in.DatasetID)
	if datasetID == "" {
		return nil, fmt.Errorf("%w: dataset_id is required", domain.ErrInvalidArgument)
	}
	if _, err := uuid.Parse(datasetID); err != nil {
		return nil, fmt.Errorf("%w: dataset_id must be a UUID", domain.ErrInvalidArgument)
	}
	syncMode := strings.TrimSpace(in.SyncMode)
	if syncMode == "" {
		syncMode = "manual"
	}
	if err := domain.ValidateSyncMode(syncMode); err != nil {
		return nil, err
	}
	if _, err := s.store.GetObjectType(ctx, tenantID, in.ObjectTypeID); err != nil {
		return nil, err
	}
	if err := s.store.SetDatasetBinding(ctx, tenantID, in.ObjectTypeID, datasetID, syncMode, s.now()); err != nil {
		return nil, err
	}
	return s.GetObjectType(ctx, tenantID, in.ObjectTypeID)
}

func (s *impl) UnbindDataset(ctx context.Context, tenantID, objectTypeID string) (*domain.ObjectType, error) {
	if err := ensureTenant(tenantID); err != nil {
		return nil, err
	}
	if err := domain.ValidateID(objectTypeID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetObjectType(ctx, tenantID, objectTypeID); err != nil {
		return nil, err
	}
	if err := s.store.ClearDatasetBinding(ctx, tenantID, objectTypeID, s.now()); err != nil {
		return nil, err
	}
	return s.GetObjectType(ctx, tenantID, objectTypeID)
}

func (s *impl) MaterializeBinding(ctx context.Context, tenantID, objectTypeID string) (string, error) {
	if err := ensureTenant(tenantID); err != nil {
		return "", err
	}
	if err := domain.ValidateID(objectTypeID); err != nil {
		return "", err
	}
	objectType, err := s.store.GetObjectType(ctx, tenantID, objectTypeID)
	if err != nil {
		return "", err
	}
	if objectType.BackingDatasetID == "" {
		return "", fmt.Errorf("%w: object type has no dataset binding", domain.ErrFailedPrecondition)
	}
	return s.newID()
}

func cloneObjectType(in *domain.ObjectType) *domain.ObjectType {
	if in == nil {
		return nil
	}
	out := *in
	if in.Properties != nil {
		out.Properties = append([]domain.Property(nil), in.Properties...)
	}
	return &out
}

func cloneProperty(in *domain.Property) *domain.Property {
	if in == nil {
		return nil
	}
	out := *in
	if in.Config != nil {
		out.Config = make(map[string]any, len(in.Config))
		for k, v := range in.Config {
			out.Config[k] = v
		}
	}
	return &out
}

func cloneLinkType(in *domain.LinkType) *domain.LinkType {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
