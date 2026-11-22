package domain

import "errors"

// Sentinel errors returned by the domain, service and repository layers.
// Handlers translate these to stable external error codes.
var (
	ErrNotFound           = errors.New("ontology: resource not found")
	ErrAlreadyExists      = errors.New("ontology: resource already exists")
	ErrInvalidArgument    = errors.New("ontology: invalid argument")
	ErrFailedPrecondition = errors.New("ontology: failed precondition")
)
