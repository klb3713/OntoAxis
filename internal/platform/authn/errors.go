package authn

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Stable authentication error codes required by ARCHITECTURE.md 6.3.5.
const (
	CodeUnauthenticated  = "unauthenticated"
	CodePermissionDenied = "permission_denied"
)

// ErrPermissionDenied marks an authenticated principal that is not allowed to
// access the requested tenant or resource.
var ErrPermissionDenied = errors.New("permission_denied")

// ErrorDetail is the external error envelope used by the identity edge. Its
// shape matches httperr.Error but keeps the identity-specific stable codes
// until httperr learns unauthenticated/permission_denied.
type ErrorDetail struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody is the payload of ErrorDetail.
type ErrorBody struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata"`
	Retryable bool              `json:"retryable"`
	RequestID string            `json:"request_id"`
}

func writeAuthError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	requestID := requestIDFromRequest(r)
	if requestID != "" {
		w.Header().Set("X-Request-Id", requestID)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorDetail{Error: ErrorBody{
		Code:      code,
		Message:   message,
		Metadata:  map[string]string{},
		Retryable: false,
		RequestID: requestID,
	}})
}
