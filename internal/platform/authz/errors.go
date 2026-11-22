package authz

import (
	"fmt"
	"net/http"
)

// Reason is the stable, low-cardinality reason attached to a decision or
// error. It is safe to expose to clients; it never contains policy text,
// attribute values, or dependency details.
type Reason string

const (
	ReasonAllow               Reason = ""
	ReasonUnknownAction       Reason = "unknown_action"
	ReasonNoRelationship      Reason = "no_relationship"
	ReasonRoleNotPermitted    Reason = "role_not_permitted"
	ReasonAdminRequired       Reason = "admin_required"
	ReasonResourceStateDenied Reason = "resource_state_denied"
	ReasonAttributeMissing    Reason = "attribute_missing"
	ReasonNoPolicy            Reason = "no_policy"
	ReasonEngineTimeout       Reason = "engine_timeout"
	ReasonEngineError         Reason = "engine_error"
	ReasonAuditUnavailable    Reason = "audit_unavailable"
	ReasonInternalError       Reason = "internal_error"
)

// Error is a fail-closed authorization failure. A denial is represented by a
// Decision plus a *Error with a deny reason; dependency and audit failures use
// the 5xx reasons and always suppress the business result.
type Error struct {
	Reason   Reason
	Engine   string
	Decision Decision
	Err      error
}

func (e *Error) Error() string {
	if e == nil {
		return "authorization error"
	}
	if e.Err != nil {
		return fmt.Sprintf("authorization %s (%s): %v", e.Reason, e.Engine, e.Err)
	}
	return fmt.Sprintf("authorization %s (%s)", e.Reason, e.Engine)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// HTTPStatus maps a fail-closed reason to the response status. Denials are
// 403 and dependency/audit failures are 503. Authentication is handled before
// this package and is 401.
func (e *Error) HTTPStatus() int {
	if e == nil {
		return http.StatusForbidden
	}
	switch e.Reason {
	case ReasonEngineTimeout, ReasonEngineError, ReasonAuditUnavailable, ReasonInternalError:
		return http.StatusServiceUnavailable
	default:
		return http.StatusForbidden
	}
}

// IsDeny reports whether the reason is a policy denial rather than a
// dependency failure.
func IsDeny(reason Reason) bool {
	switch reason {
	case ReasonUnknownAction, ReasonNoRelationship, ReasonRoleNotPermitted,
		ReasonAdminRequired, ReasonResourceStateDenied, ReasonAttributeMissing, ReasonNoPolicy:
		return true
	default:
		return false
	}
}

// IsDependencyFailure reports whether the reason must map to 503.
func IsDependencyFailure(reason Reason) bool {
	return !IsDeny(reason) && reason != ReasonAllow
}
