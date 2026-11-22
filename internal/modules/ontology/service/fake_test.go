package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
)

// fakeStore is an in-memory Store used by service tests. It enforces the same
// tenant scoping and uniqueness rules that the PostgreSQL repository enforces.
type fakeStore struct {
	objectTypes map[string]map[string]*domain.ObjectType
	properties  map[string]map[string]*domain.Property
	linkTypes   map[string]map[string]*domain.LinkType
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		objectTypes: map[string]map[string]*domain.ObjectType{},
		properties:  map[string]map[string]*domain.Property{},
		linkTypes:   map[string]map[string]*domain.LinkType{},
	}
}

func tenantMap[T any](m map[string]map[string]*T, tenantID string) map[string]*T {
	items, ok := m[tenantID]
	if !ok {
		items = map[string]*T{}
		m[tenantID] = items
	}
	return items
}

func (f *fakeStore) CreateObjectType(_ context.Context, objectType *domain.ObjectType) error {
	items := tenantMap(f.objectTypes, objectType.TenantID)
	for _, existing := range items {
		if existing.Name == objectType.Name {
			return fmt.Errorf("%w: object type name already exists", domain.ErrAlreadyExists)
		}
	}
	items[objectType.ID] = cloneObjectType(objectType)
	return nil
}

func (f *fakeStore) GetObjectType(_ context.Context, tenantID, id string) (*domain.ObjectType, error) {
	items, ok := f.objectTypes[tenantID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	objectType, ok := items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return cloneObjectType(objectType), nil
}

func (f *fakeStore) ListObjectTypes(_ context.Context, tenantID string, offset, limit int) ([]domain.ObjectType, int, error) {
	items := make([]domain.ObjectType, 0)
	for _, objectType := range f.objectTypes[tenantID] {
		items = append(items, *cloneObjectType(objectType))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	total := len(items)
	if offset >= total {
		return []domain.ObjectType{}, total, nil
	}
	items = items[offset:]
	if len(items) > limit {
		items = items[:limit]
	}
	return items, total, nil
}

func (f *fakeStore) UpdateObjectType(_ context.Context, objectType *domain.ObjectType) error {
	items, ok := f.objectTypes[objectType.TenantID]
	if !ok {
		return domain.ErrNotFound
	}
	if _, ok := items[objectType.ID]; !ok {
		return domain.ErrNotFound
	}
	items[objectType.ID] = cloneObjectType(objectType)
	return nil
}

func (f *fakeStore) DeleteObjectType(_ context.Context, tenantID, id string, _ time.Time) error {
	items, ok := f.objectTypes[tenantID]
	if !ok {
		return domain.ErrNotFound
	}
	if _, ok := items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(items, id)
	return nil
}

func (f *fakeStore) HasLinkTypesReferencing(_ context.Context, tenantID, objectTypeID string) (bool, error) {
	for _, linkType := range f.linkTypes[tenantID] {
		if linkType.SourceTypeID == objectTypeID || linkType.TargetTypeID == objectTypeID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) CreateProperty(_ context.Context, property *domain.Property) error {
	items := tenantMap(f.properties, property.TenantID)
	for _, existing := range items {
		if existing.ObjectTypeID == property.ObjectTypeID && existing.Name == property.Name {
			return fmt.Errorf("%w: property name already exists", domain.ErrAlreadyExists)
		}
	}
	items[property.ID] = cloneProperty(property)
	return nil
}

func (f *fakeStore) GetProperty(_ context.Context, tenantID, id string) (*domain.Property, error) {
	property, ok := f.properties[tenantID][id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return cloneProperty(property), nil
}

func (f *fakeStore) ListProperties(_ context.Context, tenantID, objectTypeID string) ([]domain.Property, error) {
	items := make([]domain.Property, 0)
	for _, property := range f.properties[tenantID] {
		if property.ObjectTypeID == objectTypeID {
			items = append(items, *cloneProperty(property))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (f *fakeStore) UpdateProperty(_ context.Context, property *domain.Property) error {
	items, ok := f.properties[property.TenantID]
	if !ok {
		return domain.ErrNotFound
	}
	if _, ok := items[property.ID]; !ok {
		return domain.ErrNotFound
	}
	items[property.ID] = cloneProperty(property)
	return nil
}

func (f *fakeStore) DeleteProperty(_ context.Context, tenantID, id string, _ time.Time) error {
	items, ok := f.properties[tenantID]
	if !ok {
		return domain.ErrNotFound
	}
	if _, ok := items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(items, id)
	return nil
}

func (f *fakeStore) CreateLinkType(_ context.Context, linkType *domain.LinkType) error {
	items := tenantMap(f.linkTypes, linkType.TenantID)
	for _, existing := range items {
		if existing.Name == linkType.Name {
			return fmt.Errorf("%w: link type name already exists", domain.ErrAlreadyExists)
		}
	}
	items[linkType.ID] = cloneLinkType(linkType)
	return nil
}

func (f *fakeStore) GetLinkType(_ context.Context, tenantID, id string) (*domain.LinkType, error) {
	linkType, ok := f.linkTypes[tenantID][id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return cloneLinkType(linkType), nil
}

func (f *fakeStore) ListLinkTypes(_ context.Context, tenantID string, offset, limit int) ([]domain.LinkType, int, error) {
	items := make([]domain.LinkType, 0)
	for _, linkType := range f.linkTypes[tenantID] {
		items = append(items, *cloneLinkType(linkType))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	total := len(items)
	if offset >= total {
		return []domain.LinkType{}, total, nil
	}
	items = items[offset:]
	if len(items) > limit {
		items = items[:limit]
	}
	return items, total, nil
}

func (f *fakeStore) UpdateLinkType(_ context.Context, linkType *domain.LinkType) error {
	items, ok := f.linkTypes[linkType.TenantID]
	if !ok {
		return domain.ErrNotFound
	}
	if _, ok := items[linkType.ID]; !ok {
		return domain.ErrNotFound
	}
	items[linkType.ID] = cloneLinkType(linkType)
	return nil
}

func (f *fakeStore) DeleteLinkType(_ context.Context, tenantID, id string, _ time.Time) error {
	items, ok := f.linkTypes[tenantID]
	if !ok {
		return domain.ErrNotFound
	}
	if _, ok := items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(items, id)
	return nil
}

func (f *fakeStore) SetDatasetBinding(_ context.Context, tenantID, objectTypeID, datasetID, syncMode string, updatedAt time.Time) error {
	objectType, ok := f.objectTypes[tenantID][objectTypeID]
	if !ok {
		return domain.ErrNotFound
	}
	objectType.BackingDatasetID = datasetID
	objectType.DatasetSyncMode = syncMode
	objectType.UpdatedAt = updatedAt
	return nil
}

func (f *fakeStore) ClearDatasetBinding(_ context.Context, tenantID, objectTypeID string, updatedAt time.Time) error {
	objectType, ok := f.objectTypes[tenantID][objectTypeID]
	if !ok {
		return domain.ErrNotFound
	}
	objectType.BackingDatasetID = ""
	objectType.DatasetSyncMode = ""
	objectType.UpdatedAt = updatedAt
	return nil
}
