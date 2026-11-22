package authz

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Module adapts the authorizer to the platform module contract so the app can
// mount Explain and include authorization readiness in /readyz. The app owns
// wiring (stage-2-integration); this package exposes only the seam.
type Module struct {
	authorizer *Authorizer
	guard      DecisionGuard
}

// NewModule returns a module backed by authorizer. A nil authorizer is
// rejected by Ready and by route registration is left to the app.
func NewModule(authorizer *Authorizer) *Module {
	return &Module{authorizer: authorizer, guard: NewGuard(authorizer)}
}

// Name implements module.Module.
func (*Module) Name() string { return "authorization" }

// Dependencies implements module.Module.
func (*Module) Dependencies() []string { return nil }

// Migrate implements module.Module; authorization owns no schema.
func (*Module) Migrate(context.Context, *pgxpool.Pool) error { return nil }

// RegisterRoutes implements module.Module.
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Post("/api/v1/authorization/explain", ExplainHandler(m.authorizer))
}

// Ready implements module.Module; it verifies the OpenFGA store, model and
// emptiness before the process reports ready.
func (m *Module) Ready(ctx context.Context) error {
	if m == nil || m.authorizer == nil {
		return &Error{Reason: ReasonInternalError, Engine: "authorization"}
	}
	return m.authorizer.Ready(ctx)
}

// Guard returns the decision guard business modules receive.
func (m *Module) Guard() DecisionGuard {
	if m == nil {
		return nil
	}
	return m.guard
}
