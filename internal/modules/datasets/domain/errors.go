package domain

import "errors"

// Sentinel errors returned by the dataset catalog domain. The API layer maps
// these to stable HTTP error codes; repository implementations must not leak
// driver-specific errors to callers.
var (
	ErrNotFound        = errors.New("dataset catalog resource not found")
	ErrInvalidArgument = errors.New("invalid dataset catalog argument")
	ErrAlreadyExists   = errors.New("dataset catalog resource already exists")
	ErrInvalidState    = errors.New("dataset catalog resource is not in a valid state")
	ErrUnavailable     = errors.New("dataset catalog storage is unavailable")
	ErrTenantRequired  = errors.New("tenant context is required")
)
