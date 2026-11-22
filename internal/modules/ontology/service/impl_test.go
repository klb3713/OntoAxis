package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
)

func newTestService() (*impl, *fakeStore) {
	store := newFakeStore()
	return New(store).(*impl), store
}

func mustCreateObjectType(t *testing.T, svc *impl, tenantID, name string) *domain.ObjectType {
	t.Helper()
	objectType, err := svc.CreateObjectType(context.Background(), tenantID, CreateObjectTypeInput{
		Name:        name,
		DisplayName: name,
	})
	if err != nil {
		t.Fatalf("create object type %q: %v", name, err)
	}
	return objectType
}

func TestCreateObjectTypeNormalizesAndGeneratesUUIDv7(t *testing.T) {
	svc, _ := newTestService()
	objectType, err := svc.CreateObjectType(context.Background(), "tenant-a", CreateObjectTypeInput{
		Name:               "  customer ",
		PrimaryKeyProperty: "customer_id",
	})
	if err != nil {
		t.Fatalf("CreateObjectType() error = %v", err)
	}
	if objectType.Name != "customer" {
		t.Fatalf("Name = %q, want customer", objectType.Name)
	}
	if objectType.DisplayName != "customer" {
		t.Fatalf("DisplayName = %q, want customer", objectType.DisplayName)
	}
	if objectType.Status != domain.StatusActive {
		t.Fatalf("Status = %q, want active", objectType.Status)
	}
	parsed, err := uuid.Parse(objectType.ID)
	if err != nil {
		t.Fatalf("ID %q is not a UUID: %v", objectType.ID, err)
	}
	if parsed.Version() != 7 {
		t.Fatalf("UUID version = %d, want 7", parsed.Version())
	}
	if objectType.CreatedAt.IsZero() || objectType.UpdatedAt.IsZero() {
		t.Fatal("timestamps must be set")
	}
}

func TestCreateObjectTypeRejectsDuplicateAndInvalidInput(t *testing.T) {
	svc, _ := newTestService()
	mustCreateObjectType(t, svc, "tenant-a", "customer")

	_, err := svc.CreateObjectType(context.Background(), "tenant-a", CreateObjectTypeInput{Name: "customer"})
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate error = %v, want ErrAlreadyExists", err)
	}

	_, err = svc.CreateObjectType(context.Background(), "tenant-a", CreateObjectTypeInput{Name: "Bad Name"})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("invalid name error = %v, want ErrInvalidArgument", err)
	}

	_, err = svc.CreateObjectType(context.Background(), " ", CreateObjectTypeInput{Name: "customer"})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("empty tenant error = %v, want ErrInvalidArgument", err)
	}
}

func TestSameNameIsAllowedForDifferentTenantsAndReadsAreScoped(t *testing.T) {
	svc, _ := newTestService()
	first := mustCreateObjectType(t, svc, "tenant-a", "customer")
	second := mustCreateObjectType(t, svc, "tenant-b", "customer")
	if first.ID == second.ID {
		t.Fatal("different tenants must receive independent object types")
	}
	if _, err := svc.GetObjectType(context.Background(), "tenant-b", first.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant GetObjectType error = %v, want ErrNotFound", err)
	}
}

func TestObjectTypePaginationAndUpdate(t *testing.T) {
	svc, _ := newTestService()
	first := mustCreateObjectType(t, svc, "tenant-a", "customer")
	mustCreateObjectType(t, svc, "tenant-a", "order")

	page, err := svc.ListObjectTypes(context.Background(), "tenant-a", domain.Page{Page: 1, PerPage: 1})
	if err != nil {
		t.Fatalf("ListObjectTypes() error = %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Page != 1 || page.PerPage != 1 {
		t.Fatalf("unexpected page result: %+v", page)
	}
	if _, err := svc.ListObjectTypes(context.Background(), "tenant-a", domain.Page{Page: 0, PerPage: 10}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("invalid pagination error = %v, want ErrInvalidArgument", err)
	}

	displayName := "Customer"
	updated, err := svc.UpdateObjectType(context.Background(), "tenant-a", first.ID, UpdateObjectTypeInput{DisplayName: &displayName})
	if err != nil {
		t.Fatalf("UpdateObjectType() error = %v", err)
	}
	if updated.DisplayName != "Customer" {
		t.Fatalf("DisplayName = %q, want Customer", updated.DisplayName)
	}
	if updated.UpdatedAt.Before(updated.CreatedAt) {
		t.Fatal("UpdatedAt must not precede CreatedAt")
	}
}

func TestPropertyLifecycle(t *testing.T) {
	svc, _ := newTestService()
	objectType := mustCreateObjectType(t, svc, "tenant-a", "customer")

	property, err := svc.CreateProperty(context.Background(), "tenant-a", CreatePropertyInput{
		ObjectTypeID:     objectType.ID,
		Name:             "email",
		DisplayName:      "Email",
		PropertyType:     "string",
		Required:         true,
		UniqueConstraint: true,
	})
	if err != nil {
		t.Fatalf("CreateProperty() error = %v", err)
	}
	if property.ObjectTypeID != objectType.ID || !property.Required || !property.UniqueConstraint {
		t.Fatalf("unexpected property: %+v", property)
	}

	properties, err := svc.ListProperties(context.Background(), "tenant-a", objectType.ID)
	if err != nil || len(properties) != 1 {
		t.Fatalf("ListProperties() = %v, %v; want one property", properties, err)
	}

	_, err = svc.CreateProperty(context.Background(), "tenant-a", CreatePropertyInput{
		ObjectTypeID: objectType.ID,
		Name:         "email",
		PropertyType: "string",
	})
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate property error = %v, want ErrAlreadyExists", err)
	}

	_, err = svc.CreateProperty(context.Background(), "tenant-a", CreatePropertyInput{
		ObjectTypeID: uuid.NewString(),
		Name:         "phone",
		PropertyType: "string",
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing object type error = %v, want ErrNotFound", err)
	}

	_, err = svc.CreateProperty(context.Background(), "tenant-a", CreatePropertyInput{
		ObjectTypeID: objectType.ID,
		Name:         "phone",
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("missing property type error = %v, want ErrInvalidArgument", err)
	}

	required := false
	updated, err := svc.UpdateProperty(context.Background(), "tenant-a", property.ID, UpdatePropertyInput{Required: &required})
	if err != nil {
		t.Fatalf("UpdateProperty() error = %v", err)
	}
	if updated.Required {
		t.Fatal("Required should have been updated to false")
	}

	if err := svc.DeleteProperty(context.Background(), "tenant-a", property.ID); err != nil {
		t.Fatalf("DeleteProperty() error = %v", err)
	}
	properties, err = svc.ListProperties(context.Background(), "tenant-a", objectType.ID)
	if err != nil || len(properties) != 0 {
		t.Fatalf("properties after delete = %v, %v; want empty", properties, err)
	}
}

func TestLinkTypeLifecycleAndReferencedObjectTypeDeletion(t *testing.T) {
	svc, _ := newTestService()
	source := mustCreateObjectType(t, svc, "tenant-a", "customer")
	target := mustCreateObjectType(t, svc, "tenant-a", "order")

	linkType, err := svc.CreateLinkType(context.Background(), "tenant-a", CreateLinkTypeInput{
		Name:         "places",
		DisplayName:  "Places",
		SourceTypeID: source.ID,
		TargetTypeID: target.ID,
		Cardinality:  domain.CardinalityOneToMany,
	})
	if err != nil {
		t.Fatalf("CreateLinkType() error = %v", err)
	}
	if linkType.SourceTypeID != source.ID || linkType.TargetTypeID != target.ID || linkType.Cardinality != domain.CardinalityOneToMany {
		t.Fatalf("unexpected link type: %+v", linkType)
	}

	if _, err := svc.CreateLinkType(context.Background(), "tenant-a", CreateLinkTypeInput{
		Name:         "missing_endpoint",
		SourceTypeID: source.ID,
		TargetTypeID: uuid.NewString(),
		Cardinality:  domain.CardinalityOneToOne,
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing endpoint error = %v, want ErrNotFound", err)
	}

	if _, err := svc.CreateLinkType(context.Background(), "tenant-a", CreateLinkTypeInput{
		Name:         "bad_cardinality",
		SourceTypeID: source.ID,
		TargetTypeID: target.ID,
		Cardinality:  "many",
	}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("bad cardinality error = %v, want ErrInvalidArgument", err)
	}

	if err := svc.DeleteObjectType(context.Background(), "tenant-a", source.ID); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Fatalf("referenced deletion error = %v, want ErrFailedPrecondition", err)
	}

	displayName := "Customer Places"
	updated, err := svc.UpdateLinkType(context.Background(), "tenant-a", linkType.ID, UpdateLinkTypeInput{DisplayName: &displayName})
	if err != nil || updated.DisplayName != "Customer Places" {
		t.Fatalf("UpdateLinkType() = %+v, %v", updated, err)
	}

	if err := svc.DeleteLinkType(context.Background(), "tenant-a", linkType.ID); err != nil {
		t.Fatalf("DeleteLinkType() error = %v", err)
	}
	if err := svc.DeleteObjectType(context.Background(), "tenant-a", source.ID); err != nil {
		t.Fatalf("DeleteObjectType() after link removal error = %v", err)
	}
	if _, err := svc.GetObjectType(context.Background(), "tenant-a", source.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted object type Get error = %v, want ErrNotFound", err)
	}
}

func TestDatasetBindingLifecycleAndMaterialization(t *testing.T) {
	svc, _ := newTestService()
	objectType := mustCreateObjectType(t, svc, "tenant-a", "customer")
	datasetID := uuid.NewString()

	bound, err := svc.BindDataset(context.Background(), "tenant-a", BindDatasetInput{
		ObjectTypeID: objectType.ID,
		DatasetID:    datasetID,
		SyncMode:     "manual",
	})
	if err != nil {
		t.Fatalf("BindDataset() error = %v", err)
	}
	if bound.BackingDatasetID != datasetID || bound.DatasetSyncMode != "manual" {
		t.Fatalf("unexpected binding: %+v", bound)
	}

	operationID, err := svc.MaterializeBinding(context.Background(), "tenant-a", objectType.ID)
	if err != nil {
		t.Fatalf("MaterializeBinding() error = %v", err)
	}
	if parsed, err := uuid.Parse(operationID); err != nil || parsed.Version() != 7 {
		t.Fatalf("operation ID = %q, want UUIDv7 (err=%v)", operationID, err)
	}

	if _, err := svc.BindDataset(context.Background(), "tenant-a", BindDatasetInput{
		ObjectTypeID: objectType.ID,
		DatasetID:    "not-a-uuid",
	}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("invalid dataset id error = %v, want ErrInvalidArgument", err)
	}

	unbound, err := svc.UnbindDataset(context.Background(), "tenant-a", objectType.ID)
	if err != nil {
		t.Fatalf("UnbindDataset() error = %v", err)
	}
	if unbound.BackingDatasetID != "" || unbound.DatasetSyncMode != "" {
		t.Fatalf("binding was not cleared: %+v", unbound)
	}
	if _, err := svc.MaterializeBinding(context.Background(), "tenant-a", objectType.ID); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Fatalf("materialize without binding error = %v, want ErrFailedPrecondition", err)
	}
}

func TestCrossTenantMutationsAreNotFound(t *testing.T) {
	svc, _ := newTestService()
	objectType := mustCreateObjectType(t, svc, "tenant-b", "customer")
	displayName := "Hijacked"

	if _, err := svc.UpdateObjectType(context.Background(), "tenant-a", objectType.ID, UpdateObjectTypeInput{DisplayName: &displayName}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant update error = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteObjectType(context.Background(), "tenant-a", objectType.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant delete error = %v, want ErrNotFound", err)
	}
	stored, err := svc.GetObjectType(context.Background(), "tenant-b", objectType.ID)
	if err != nil {
		t.Fatalf("tenant B GetObjectType() error = %v", err)
	}
	if stored.DisplayName != "customer" {
		t.Fatalf("tenant B object type was modified: %+v", stored)
	}
}

func TestFixedClockAndDeterministicIDs(t *testing.T) {
	svc, _ := newTestService()
	fixed := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	sequence := 0
	svc.newID = func() (string, error) {
		sequence++
		return fmt.Sprintf("00000000-0000-7000-8000-%012d", sequence), nil
	}
	objectType := mustCreateObjectType(t, svc, "tenant-a", "customer")
	if !objectType.CreatedAt.Equal(fixed) || !objectType.UpdatedAt.Equal(fixed) {
		t.Fatalf("timestamps = %v / %v, want %v", objectType.CreatedAt, objectType.UpdatedAt, fixed)
	}
}
