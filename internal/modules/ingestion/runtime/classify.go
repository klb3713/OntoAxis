package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
)

// ErrorClass tells the dispatcher whether a submission failure may be retried.
type ErrorClass string

const (
	// ClassRetryable marks transient failures: timeouts, connection errors and
	// connector runtime 5xx responses.
	ClassRetryable ErrorClass = "retryable"
	// ClassPermanent marks failures a retry cannot fix: connector configuration,
	// authentication, conflicts and other 4xx responses.
	ClassPermanent ErrorClass = "permanent"
)

var (
	// ErrPermanent marks a submission failure that must not be retried.
	ErrPermanent = errors.New("permanent connector runtime failure")
	// ErrRetryable marks a submission failure that may be retried.
	ErrRetryable = errors.New("retryable connector runtime failure")
)

// Permanent wraps err so DefaultClassify treats it as permanent.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrPermanent, err)
}

// Retryable wraps err so DefaultClassify treats it as retryable.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrRetryable, err)
}

// RetryableError is implemented by runtime errors that carry their own retry
// decision, for example an HTTP status error from the connector runtime client.
type RetryableError interface {
	Retryable() bool
}

// Classifier maps a submission error to its retry class.
type Classifier func(error) ErrorClass

// DefaultClassify implements the connector runtime failure classification:
// explicitly wrapped errors win, then typed retryability, timeouts and common
// transient network errors are retryable. Everything else is permanent so a
// configuration or authentication error is not retried five times.
func DefaultClassify(err error) ErrorClass {
	if err == nil {
		return ClassPermanent
	}
	if errors.Is(err, ErrPermanent) {
		return ClassPermanent
	}
	if errors.Is(err, ErrRetryable) {
		return ClassRetryable
	}
	var typed RetryableError
	if errors.As(err, &typed) {
		if typed.Retryable() {
			return ClassRetryable
		}
		return ClassPermanent
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ClassRetryable
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ClassRetryable
	}
	for _, errno := range []syscall.Errno{
		syscall.ECONNREFUSED,
		syscall.ECONNRESET,
		syscall.EPIPE,
		syscall.EHOSTUNREACH,
		syscall.ENETUNREACH,
	} {
		if errors.Is(err, errno) {
			return ClassRetryable
		}
	}
	return ClassPermanent
}
