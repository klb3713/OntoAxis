// Package template is a compile-checked module scaffold. Copy this directory
// when adding a business module; do not register the template itself.
package template

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/platform/module"
)

var _ module.Module = (*Module)(nil)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string           { return "template" }
func (m *Module) Dependencies() []string { return nil }
func (m *Module) Migrate(context.Context, *pgxpool.Pool) error {
	return nil
}
func (m *Module) RegisterRoutes(chi.Router) {}
func (m *Module) Ready(context.Context) error {
	return nil
}
