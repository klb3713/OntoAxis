package module

import (
	"context"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fakeModule struct {
	name string
	deps []string
}

func (f *fakeModule) Name() string                                 { return f.name }
func (f *fakeModule) Dependencies() []string                       { return f.deps }
func (f *fakeModule) Migrate(context.Context, *pgxpool.Pool) error { return nil }
func (f *fakeModule) RegisterRoutes(chi.Router)                    {}
func (f *fakeModule) Ready(context.Context) error                  { return nil }

func TestRegistryDuplicate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeModule{name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&fakeModule{name: "a"}); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestRegistryMissingDependency(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&fakeModule{name: "a", deps: []string{"missing"}})
	if _, err := r.Ordered(); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestRegistryCycle(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&fakeModule{name: "a", deps: []string{"b"}})
	_ = r.Register(&fakeModule{name: "b", deps: []string{"a"}})
	if _, err := r.Ordered(); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestRegistryStableTopologicalOrder(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&fakeModule{name: "c"})
	_ = r.Register(&fakeModule{name: "a"})
	_ = r.Register(&fakeModule{name: "b", deps: []string{"a"}})
	ordered, err := r.Ordered()
	if err != nil {
		t.Fatal(err)
	}
	got := []string{ordered[0].Name(), ordered[1].Name(), ordered[2].Name()}
	want := []string{"c", "a", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
