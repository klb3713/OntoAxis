package authz

import (
	"testing"
)

func TestDefaultCatalogCoverage(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatalf("default catalog: %v", err)
	}
	actions := catalog.Actions()
	if len(actions) != 89 {
		t.Fatalf("expected 89 leaf actions, got %d", len(actions))
	}
	wantCounts := map[Group]int{
		GroupRead: 27, GroupWrite: 21, GroupDelete: 11,
		GroupInvoke: 10, GroupAdmin: 18, GroupAudit: 2,
	}
	for group, want := range wantCounts {
		if got := len(catalog.GroupActions(group)); got != want {
			t.Fatalf("group %s: expected %d actions, got %d", group, want, got)
		}
	}
	for _, spec := range actions {
		if spec.Relation != groupRelation[spec.Group] {
			t.Fatalf("action %s relation %s does not match group %s", spec.Action, spec.Relation, spec.Group)
		}
	}
}

func TestCatalogRejectsUnknownAction(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.Lookup("unknown:action"); ok {
		t.Fatal("unknown action must not be registered")
	}
	if _, ok := catalog.Lookup("dataset:read"); !ok {
		t.Fatal("dataset:read must be registered")
	}
}

func TestCatalogRejectsDuplicateAndBadGroup(t *testing.T) {
	if _, err := NewCatalog([]ActionSpec{
		{Action: "dataset:read", Group: GroupRead, ResourceType: "dataset", Relation: "can_read"},
		{Action: "dataset:read", Group: GroupRead, ResourceType: "dataset", Relation: "can_read"},
	}); err == nil {
		t.Fatal("expected duplicate action rejection")
	}
	if _, err := NewCatalog([]ActionSpec{
		{Action: "dataset:read", Group: "owner", ResourceType: "dataset", Relation: "can_read"},
	}); err == nil {
		t.Fatal("expected unknown group rejection")
	}
}
