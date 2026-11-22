package httperr

import (
	"encoding/json"
	"net/http"

	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

// Error is the external error envelope. Internal errors must be translated to
// one of the stable codes below before they reach this package.
type Error struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Retryable bool              `json:"retryable"`
	RequestID string            `json:"request_id,omitempty"`
}

const (
	CodeBadRequest      = "bad_request"
	CodeUnauthorized    = "unauthorized"
	CodeForbidden       = "forbidden"
	CodeNotFound        = "not_found"
	CodeConflict        = "conflict"
	CodeUnavailable     = "unavailable"
	CodeInternal        = "internal"
	CodeNotImplemented  = "not_implemented"
	CodeAlreadyExists   = "already_exists"
	CodeInvalidArgument = "invalid_argument"
)

func statusFor(code string) int {
	switch code {
	case CodeBadRequest, CodeInvalidArgument:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict, CodeAlreadyExists:
		return http.StatusConflict
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	case CodeNotImplemented:
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

// Write writes a stable JSON error response and never includes internal error
// text for 5xx responses.
func Write(w http.ResponseWriter, r *http.Request, e Error) {
	if e.Code == "" {
		e.Code = CodeInternal
	}
	if e.Message == "" {
		e.Message = http.StatusText(statusFor(e.Code))
	}
	if e.RequestID == "" {
		e.RequestID = observability.RequestID(r.Context())
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusFor(e.Code))
	_ = json.NewEncoder(w).Encode(map[string]Error{"error": e})
}

func BadRequest(message string) Error     { return Error{Code: CodeBadRequest, Message: message} }
func Unauthorized(message string) Error   { return Error{Code: CodeUnauthorized, Message: message} }
func Forbidden(message string) Error      { return Error{Code: CodeForbidden, Message: message} }
func NotFound(message string) Error       { return Error{Code: CodeNotFound, Message: message} }
func Conflict(message string) Error       { return Error{Code: CodeConflict, Message: message} }
func Internal() Error                     { return Error{Code: CodeInternal, Message: "internal server error"} }
func Unavailable(message string) Error    { return Error{Code: CodeUnavailable, Message: message} }
func NotImplemented(message string) Error { return Error{Code: CodeNotImplemented, Message: message} }
