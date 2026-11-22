// Package authtest provides deterministic authorization seams for module tests.
package authtest

import (
	"context"
	"net/http"
	"sync"

	"github.com/klb3713/ontoaxis/internal/platform/authz"
)

// FakeGuard records requested decisions and returns a fixed verdict.
type FakeGuard struct {
	Allow   bool
	Reason  authz.Reason
	mu      sync.Mutex
	actions []authz.Action
	facts   []authz.ResourceFact
}

// Guard returns a permissive fake.
func Guard() *FakeGuard { return &FakeGuard{Allow: true} }

// Deny returns a denying fake with the given reason.
func Deny(reason authz.Reason) *FakeGuard { return &FakeGuard{Reason: reason} }

// Require implements authz.DecisionGuard.
func (f *FakeGuard) Require(_ context.Context, action authz.Action, resource authz.ResourceFact) (authz.Decision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	f.facts = append(f.facts, resource)
	if f.Allow {
		return authz.Decision{Allow: true, DecisionID: "decision-test", Action: action, Resource: resource}, nil
	}
	reason := f.Reason
	if reason == "" {
		reason = authz.ReasonRoleNotPermitted
	}
	return authz.Decision{DecisionID: "decision-test", Reason: reason}, &authz.Error{Reason: reason}
}

// WriteHTTP implements authz.DecisionGuard.
func (f *FakeGuard) WriteHTTP(w http.ResponseWriter, r *http.Request, err error) {
	authz.WriteHTTPError(w, r, err)
}

// SetDecisionHeader implements authz.DecisionGuard.
func (f *FakeGuard) SetDecisionHeader(w http.ResponseWriter, decision authz.Decision) {
	if decision.DecisionID != "" {
		w.Header().Set(authz.DecisionHeader, decision.DecisionID)
	}
}

// Calls returns a copy of recorded actions and facts.
func (f *FakeGuard) Calls() ([]authz.Action, []authz.ResourceFact) {
	f.mu.Lock()
	defer f.mu.Unlock()
	actions := append([]authz.Action(nil), f.actions...)
	facts := append([]authz.ResourceFact(nil), f.facts...)
	return actions, facts
}
