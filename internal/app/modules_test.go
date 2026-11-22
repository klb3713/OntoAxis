package app

import (
	"testing"

	"github.com/klb3713/ontoaxis/internal/platform/config"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

func TestBuildModulesResolvesDependencyOrder(t *testing.T) {
	mods := BuildModules(nil, config.Defaults())
	if len(mods) != 9 {
		t.Fatalf("expected 9 core modules, got %d", len(mods))
	}
	reg := module.NewRegistry()
	for _, m := range mods {
		if err := reg.Register(m); err != nil {
			t.Fatalf("register %s: %v", m.Name(), err)
		}
	}
	ordered, err := reg.Ordered()
	if err != nil {
		t.Fatalf("dependency order: %v", err)
	}
	got := make([]string, 0, len(ordered))
	for _, m := range ordered {
		got = append(got, m.Name())
	}
	want := []string{"tenancy", "connectors", "ingestion", "datasets", "pipelines", "ontology", "objects", "query", "actions"}
	if len(got) != len(want) {
		t.Fatalf("ordered module count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ordered modules: got %v want %v", got, want)
		}
	}
}
