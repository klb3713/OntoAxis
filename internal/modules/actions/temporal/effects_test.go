package temporal

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	objectsdomain "github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	objectservice "github.com/klb3713/ontoaxis/internal/modules/objects/service"
)

type fakeObjects struct {
	object objectsdomain.Object
}

var _ objectservice.Service = (*fakeObjects)(nil)

func (f *fakeObjects) CreateObject(_ context.Context, tenantID, objectTypeID uuid.UUID, properties map[string]any) (objectsdomain.Object, error) {
	return objectsdomain.Object{ID: uuid.New(), TenantID: tenantID, ObjectTypeID: objectTypeID, Properties: properties, Version: 1}, nil
}
func (f *fakeObjects) GetObject(_ context.Context, _, id uuid.UUID) (objectsdomain.Object, error) {
	return f.object, nil
}
func (f *fakeObjects) ListObjects(context.Context, uuid.UUID, uuid.UUID, int, int) ([]objectsdomain.Object, int64, error) {
	return nil, 0, nil
}
func (f *fakeObjects) UpdateObject(_ context.Context, tenantID, id uuid.UUID, properties map[string]any, _ bool) (objectsdomain.Object, error) {
	updated := f.object
	updated.ID = id
	updated.TenantID = tenantID
	updated.Properties = properties
	updated.Version++
	return updated, nil
}
func (f *fakeObjects) DeleteObject(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeObjects) CreateLink(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, map[string]any) (objectsdomain.Link, error) {
	return objectsdomain.Link{}, nil
}
func (f *fakeObjects) ListLinks(context.Context, uuid.UUID, uuid.UUID, int, int) ([]objectsdomain.Link, int64, error) {
	return nil, 0, nil
}
func (f *fakeObjects) DeleteLink(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func TestObjectEffectApplierUpdate(t *testing.T) {
	objID := uuid.New()
	applier := ObjectEffectApplier{Objects: &fakeObjects{object: objectsdomain.Object{ID: objID, Properties: map[string]any{"name": "before"}, Version: 1}}}
	step := domain.StepDefinition{
		ID: "s1", ActivityType: domain.ActivityTypeObjectUpdate,
		Parameters: map[string]any{"object_id": objID.String(), "properties": map[string]any{"name": "after"}},
	}
	before, after, err := applier.Apply(context.Background(), uuid.New(), uuid.New(), step, step.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if before["object_id"] != after["object_id"] {
		t.Fatalf("object id changed: %#v %#v", before, after)
	}
	if fmt.Sprint(before["version"]) == fmt.Sprint(after["version"]) {
		t.Fatalf("version did not advance: %#v %#v", before, after)
	}
}

func TestObjectEffectApplierNoop(t *testing.T) {
	before, after, err := ObjectEffectApplier{}.Apply(context.Background(), uuid.New(), uuid.New(), domain.StepDefinition{ActivityType: domain.ActivityTypeNoop}, nil)
	if err != nil || before != nil || after != nil {
		t.Fatalf("unexpected noop: %v %v %v", before, after, err)
	}
}

func TestObjectEffectApplierTestHooks(t *testing.T) {
	_, _, err := ObjectEffectApplier{}.Apply(context.Background(), uuid.New(), uuid.New(), domain.StepDefinition{ActivityType: domain.ActivityTypeTestPermanentFailure}, nil)
	if err == nil {
		t.Fatal("expected disabled test hook to fail")
	}
	_, _, err = ObjectEffectApplier{TestHooks: true}.Apply(context.Background(), uuid.New(), uuid.New(), domain.StepDefinition{ActivityType: domain.ActivityTypeTestPermanentFailure}, nil)
	if err == nil {
		t.Fatal("expected injected permanent failure")
	}
}
