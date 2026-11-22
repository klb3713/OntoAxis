package authz

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

// DecisionGuard is the module-facing authorization seam. Modules depend on
// this interface so their handler tests can use a deterministic fake; the
// production implementation is Guard over the single Authorizer.
type DecisionGuard interface {
	Require(ctx context.Context, action Action, resource ResourceFact) (Decision, error)
	WriteHTTP(w http.ResponseWriter, r *http.Request, err error)
	SetDecisionHeader(w http.ResponseWriter, decision Decision)
}

// Guard is the module-facing seam. Modules call Require with a declared action
// and a tenant-scoped resource fact; they never inspect roles or policies.
type Guard struct {
	authorizer *Authorizer
}

// NewGuard wraps the decision point for HTTP modules.
func NewGuard(authorizer *Authorizer) *Guard {
	return &Guard{authorizer: authorizer}
}

// Authorizer returns the underlying decision point for startup wiring and
// tests. Production modules should use Require.
func (g *Guard) Authorizer() *Authorizer { return g.authorizer }

// Middleware makes per-request memoization and route metadata available. It
// does not authenticate; authn runs earlier in the chain.
func (g *Guard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithRequestMemo(r.Context())
		route := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			route = rc.RoutePattern()
		}
		ctx = WithAuditMeta(ctx, r.Method, route)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Require evaluates action against the trusted resource fact. On denial it
// returns the decision plus *Error; the caller must write the error and must
// not perform the side effect.
func (g *Guard) Require(ctx context.Context, action Action, resource ResourceFact) (Decision, error) {
	if g == nil || g.authorizer == nil {
		return Decision{}, &Error{Reason: ReasonInternalError, Engine: "guard"}
	}
	return g.authorizer.Authorize(ctx, Request{Action: action, Resource: resource})
}

// SetDecisionHeader exposes the decision id on an allow response. Modules
// call it after Require succeeds; it is never user-supplied.
func (g *Guard) SetDecisionHeader(w http.ResponseWriter, decision Decision) {
	if decision.DecisionID != "" {
		w.Header().Set(DecisionHeader, decision.DecisionID)
	}
}

// WriteHTTP maps a fail-closed error to the stable HTTP contract. It never
// includes policy text, attributes, or dependency details in the response.
func (g *Guard) WriteHTTP(w http.ResponseWriter, r *http.Request, err error) {
	WriteHTTPError(w, r, err)
}

// WriteHTTPError maps a fail-closed error to the stable HTTP contract for
// callers that hold an Authorizer rather than a Guard (for example Explain).
func WriteHTTPError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, authn.ErrUnauthenticated) {
		httperr.Write(w, r, httperr.Unauthorized("authentication required"))
		return
	}
	var authzErr *Error
	if errors.As(err, &authzErr) {
		if authzErr.Decision.DecisionID != "" {
			w.Header().Set(DecisionHeader, authzErr.Decision.DecisionID)
		}
		if authzErr.HTTPStatus() == http.StatusServiceUnavailable {
			httperr.Write(w, r, httperr.Unavailable("authorization temporarily unavailable"))
			return
		}
		httperr.Write(w, r, httperr.Forbidden("permission denied"))
		return
	}
	httperr.Write(w, r, httperr.Internal())
}

// DecisionHeader returns the response header that carries the decision ID.
const DecisionHeader = "X-Ontoaxis-Decision-Id"

// AuthorizeHTTP is the shared module seam: it evaluates one declared action
// against a tenant-scoped resource fact, writes the stable HTTP error on
// denial/failure, and stamps the decision id on allow. A nil guard is allowed
// only for legacy unit tests; production wiring must set a guard.
func AuthorizeHTTP(w http.ResponseWriter, r *http.Request, guard DecisionGuard, action Action, resource ResourceFact) bool {
	if guard == nil {
		return true
	}
	decision, err := guard.Require(r.Context(), action, resource)
	if err != nil {
		guard.WriteHTTP(w, r, err)
		return false
	}
	guard.SetDecisionHeader(w, decision)
	return true
}
