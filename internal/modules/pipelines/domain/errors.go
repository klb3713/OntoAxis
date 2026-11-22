package domain

import "errors"

// Sentinel errors are mapped to HTTP status codes by the api package. They are
// deliberately domain-level so service code never depends on net/http.
var (
	ErrNotFound        = errors.New("resource not found")
	ErrInvalidArgument = errors.New("invalid argument")
	ErrAlreadyExists   = errors.New("resource already exists")
	ErrInvalidState    = errors.New("invalid state")
	ErrUnavailable     = errors.New("service unavailable")
	ErrTenantRequired  = errors.New("tenant context required")
	ErrUnauthorized    = errors.New("unauthorized")
)
