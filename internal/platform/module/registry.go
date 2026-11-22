package module

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Module is the stable contract implemented by every business capability.
// Shared entrypoints are registered once by the app; modules must not modify
// cmd/ontoaxis-core, internal/app or the global router.
type Module interface {
	Name() string
	Dependencies() []string
	Migrate(ctx context.Context, pool *pgxpool.Pool) error
	RegisterRoutes(r chi.Router)
	Ready(ctx context.Context) error
}

// Registry validates names and dependencies and returns a stable topological
// order (ties are ordered by registration order).
type Registry struct {
	order []string
	mods  map[string]Module
}

func NewRegistry() *Registry {
	return &Registry{mods: make(map[string]Module)}
}

func (r *Registry) Register(m Module) error {
	if m == nil {
		return fmt.Errorf("module is nil")
	}
	name := m.Name()
	if name == "" {
		return fmt.Errorf("module name is empty")
	}
	if _, exists := r.mods[name]; exists {
		return fmt.Errorf("duplicate module %q", name)
	}
	r.mods[name] = m
	r.order = append(r.order, name)
	return nil
}

// Ordered validates all dependencies and returns a deterministic topological
// order.
func (r *Registry) Ordered() ([]Module, error) {
	deps := make(map[string][]string, len(r.mods))
	for name, m := range r.mods {
		for _, dep := range m.Dependencies() {
			if dep == name {
				return nil, fmt.Errorf("module %q depends on itself", name)
			}
			if _, ok := r.mods[dep]; !ok {
				return nil, fmt.Errorf("module %q depends on missing module %q", name, dep)
			}
		}
		deps[name] = append([]string(nil), m.Dependencies()...)
	}

	// Kahn's algorithm with registration order as the tie-breaker.
	indegree := make(map[string]int, len(r.mods))
	for name, d := range deps {
		indegree[name] = len(d)
	}
	var ready []string
	for _, name := range r.order {
		if indegree[name] == 0 {
			ready = append(ready, name)
		}
	}
	out := make([]Module, 0, len(r.mods))
	for len(ready) > 0 {
		sort.SliceStable(ready, func(i, j int) bool { return indexOf(r.order, ready[i]) < indexOf(r.order, ready[j]) })
		name := ready[0]
		ready = ready[1:]
		out = append(out, r.mods[name])
		for _, other := range r.order {
			for _, dep := range deps[other] {
				if dep == name {
					indegree[other]--
					if indegree[other] == 0 {
						ready = append(ready, other)
					}
				}
			}
		}
	}
	if len(out) != len(r.mods) {
		return nil, fmt.Errorf("module dependency cycle detected")
	}
	return out, nil
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return len(s)
}
