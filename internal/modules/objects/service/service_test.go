package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
)

type fakeStore struct {
	listLimit int
	listType  uuid.UUID
}

func (f *fakeStore) CreateObject(context.Context, uuid.UUID, uuid.UUID, map[string]any) (domain.Object, error) {
	return domain.Object{}, nil
}
func (f *fakeStore) GetObject(context.Context, uuid.UUID, uuid.UUID) (domain.Object, error) {
	return domain.Object{}, nil
}
func (f *fakeStore) ListObjects(_ context.Context, _ uuid.UUID, objectTypeID uuid.UUID, limit, _ int) ([]domain.Object, int64, error) {
	f.listLimit = limit
	f.listType = objectTypeID
	return nil, 0, nil
}
func (f *fakeStore) UpdateObject(context.Context, uuid.UUID, uuid.UUID, map[string]any, bool) (domain.Object, error) {
	return domain.Object{}, nil
}
func (f *fakeStore) DeleteObject(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeStore) CreateLink(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, map[string]any) (domain.Link, error) {
	return domain.Link{}, nil
}
func (f *fakeStore) ListLinks(context.Context, uuid.UUID, uuid.UUID, int, int) ([]domain.Link, int64, error) {
	return nil, 0, nil
}
func (f *fakeStore) DeleteLink(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func TestCreateObjectRejectsMissingProperties(t *testing.T) {
	svc := New(&fakeStore{})
	_, err := svc.CreateObject(context.Background(), uuid.New(), uuid.New(), nil)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestListObjectsNormalizesPagination(t *testing.T) {
	store := &fakeStore{}
	svc := New(store)
	typeID := uuid.New()
	if _, _, err := svc.ListObjects(context.Background(), uuid.New(), typeID, 0, -10); err != nil {
		t.Fatalf("list objects: %v", err)
	}
	if store.listLimit != 50 {
		t.Fatalf("limit = %d, want 50", store.listLimit)
	}
	if store.listType != typeID {
		t.Fatalf("type = %s, want %s", store.listType, typeID)
	}
}

func TestUpdateObjectRejectsEmptyPatch(t *testing.T) {
	svc := New(&fakeStore{})
	if _, err := svc.UpdateObject(context.Background(), uuid.New(), uuid.New(), nil, false); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCreateLinkRejectsNilIDs(t *testing.T) {
	svc := New(&fakeStore{})
	if _, err := svc.CreateLink(context.Background(), uuid.New(), uuid.New(), uuid.Nil, uuid.New(), nil); err == nil {
		t.Fatal("expected validation error")
	}
}

type fakeTypeValidator struct {
	exists bool
}

func (f fakeTypeValidator) ObjectTypeExists(context.Context, string, string) (bool, error) {
	return f.exists, nil
}

func TestCreateObjectRejectsUnknownObjectType(t *testing.T) {
	svc := New(&fakeStore{}, WithObjectTypeValidator(fakeTypeValidator{exists: false}))
	_, err := svc.CreateObject(context.Background(), uuid.New(), uuid.New(), map[string]any{"name": "trail"})
	if err == nil {
		t.Fatal("expected unknown object type error")
	}
}
