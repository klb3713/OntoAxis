package search

import (
	"errors"
	"fmt"
)

// Error kinds returned by this package. Callers should use errors.Is against
// these sentinels instead of matching messages.
var (
	// ErrUnavailable reports a search backend that cannot serve the request
	// (transport failure, timeout, server error, or an unusable credentials
	// configuration). It is safe to retry.
	ErrUnavailable = errors.New("search unavailable")

	// ErrInvalid reports a request or document that the backend rejected as
	// invalid. Retrying without a change will not succeed.
	ErrInvalid = errors.New("search invalid")

	// ErrNotFound reports a missing resource such as an index.
	ErrNotFound = errors.New("search not found")
)

// Error is the typed error returned by the search client and consumer. Kind is
// one of ErrUnavailable, ErrInvalid or ErrNotFound; Op names the logical
// operation (for example "ensure_index"); Err carries the underlying cause.
//
// Error intentionally never includes request headers, the API key, request
// bodies, or response bodies: upstream payloads can contain object properties
// and credentials. Only the HTTP status and a short backend error code are
// retained in the message.
type Error struct {
	Kind error
	Op   string
	Err  error
}

// Error renders a bounded, non-sensitive message.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	kind := "error"
	switch {
	case errors.Is(e.Kind, ErrUnavailable):
		kind = "unavailable"
	case errors.Is(e.Kind, ErrInvalid):
		kind = "invalid"
	case errors.Is(e.Kind, ErrNotFound):
		kind = "not_found"
	}
	if e.Op == "" && e.Err == nil {
		return "search: " + kind
	}
	if e.Op == "" {
		return fmt.Sprintf("search: %s: %v", kind, e.Err)
	}
	if e.Err == nil {
		return fmt.Sprintf("search: %s: %s", kind, e.Op)
	}
	return fmt.Sprintf("search: %s: %s: %v", kind, e.Op, e.Err)
}

// Unwrap exposes the underlying cause to errors.Unwrap/errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is makes the typed error match both its classification sentinel (for
// example ErrUnavailable) and its underlying cause.
func (e *Error) Is(target error) bool {
	if e == nil {
		return false
	}
	return errors.Is(e.Kind, target) || errors.Is(e.Err, target)
}

// Errorf builds a typed search error.
func Errorf(kind error, op string, format string, args ...any) error {
	return &Error{Kind: kind, Op: op, Err: fmt.Errorf(format, args...)}
}

// Wrap builds a typed search error around err. A nil err yields a typed error
// with no cause (only the kind and operation are retained).
func Wrap(kind error, op string, err error) error {
	if err == nil {
		err = errors.New("unknown error")
	}
	return &Error{Kind: kind, Op: op, Err: err}
}

// IsUnavailable reports whether err represents a retryable backend failure.
func IsUnavailable(err error) bool { return errors.Is(err, ErrUnavailable) }

// IsInvalid reports whether err represents a non-retryable invalid request.
func IsInvalid(err error) bool { return errors.Is(err, ErrInvalid) }

// IsNotFound reports whether err represents a missing resource.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
