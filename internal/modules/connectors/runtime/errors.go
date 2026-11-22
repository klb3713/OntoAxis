package runtime

import (
	"errors"
	"fmt"
)

var (
	// ErrRuntimeNotConfigured reports missing Kafka Connect or warehouse wiring.
	ErrRuntimeNotConfigured = errors.New("connector sync runtime is not configured")
	// ErrUnsupportedType reports a connector type without a CDC runtime mapping.
	ErrUnsupportedType = errors.New("connector type does not support sync runtime")
	// ErrInvalidSyncConfig reports sync configuration that is missing or unsafe.
	ErrInvalidSyncConfig = errors.New("invalid connector sync config")
	// ErrInvalidTenant reports a missing or malformed tenant identifier.
	ErrInvalidTenant = errors.New("invalid runtime tenant id")
	// ErrInvalidConnector reports a missing connector or connector identifier.
	ErrInvalidConnector = errors.New("invalid runtime connector")
	// ErrInvalidRuntimeRef reports a runtime reference outside the tenant namespace.
	ErrInvalidRuntimeRef = errors.New("invalid runtime reference")
	// ErrSecretUnavailable reports a secret reference that cannot be resolved. The
	// wrapped variants below stay distinguishable through errors.Is.
	ErrSecretUnavailable = errors.New("connector secret unavailable")
	// ErrSecretNotFound reports a secret reference without stored values.
	ErrSecretNotFound = fmt.Errorf("%w: secret is not configured", ErrSecretUnavailable)
	// ErrSecretInvalid reports malformed secret values.
	ErrSecretInvalid = fmt.Errorf("%w: secret is malformed", ErrSecretUnavailable)
	// ErrConnectUnavailable reports Kafka Connect transport failures, 5xx
	// responses, timeouts, and exhausted retries.
	ErrConnectUnavailable = errors.New("kafka connect unavailable")
	// ErrConnectRejected reports a permanent 4xx Kafka Connect rejection.
	ErrConnectRejected = errors.New("kafka connect rejected request")
	// ErrConnectConflict reports a Kafka Connect 409 conflict.
	ErrConnectConflict = errors.New("kafka connect conflict")
	// ErrInvalidConnectorName reports an unsafe Kafka Connect connector name.
	ErrInvalidConnectorName = errors.New("invalid kafka connect connector name")
)

// IsInvalidInput reports preflight failures caused by caller configuration or
// identifiers, so the service can surface them as parameter errors.
func IsInvalidInput(err error) bool {
	return errors.Is(err, ErrInvalidSyncConfig) ||
		errors.Is(err, ErrUnsupportedType) ||
		errors.Is(err, ErrInvalidTenant) ||
		errors.Is(err, ErrInvalidConnector) ||
		errors.Is(err, ErrInvalidRuntimeRef)
}

// IsConflict reports a runtime conflict that the connector service must surface
// as its conflict error.
func IsConflict(err error) bool { return errors.Is(err, ErrConnectConflict) }

// IsRetryable reports runtime failures that may succeed on a later attempt.
func IsRetryable(err error) bool { return errors.Is(err, ErrConnectUnavailable) }
