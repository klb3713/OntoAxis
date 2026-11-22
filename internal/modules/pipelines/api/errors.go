package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal","message":"internal server error","retryable":true}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeFailure(w http.ResponseWriter, r *http.Request, code, message string) {
	httperr.Write(w, r, httperr.Error{
		Code:      code,
		Message:   message,
		Retryable: code == httperr.CodeUnavailable || code == httperr.CodeInternal,
	})
}

// writeServiceError maps domain sentinels to the stable external error codes.
// Internal details are never copied into 5xx responses.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrTenantRequired), errors.Is(err, domain.ErrUnauthorized):
		writeFailure(w, r, httperr.CodeUnauthorized, "authenticated tenant is required")
	case errors.Is(err, domain.ErrInvalidArgument):
		writeFailure(w, r, httperr.CodeInvalidArgument, "invalid argument")
	case errors.Is(err, domain.ErrNotFound):
		writeFailure(w, r, httperr.CodeNotFound, "resource not found")
	case errors.Is(err, domain.ErrAlreadyExists):
		writeFailure(w, r, httperr.CodeAlreadyExists, "resource already exists")
	case errors.Is(err, domain.ErrInvalidState):
		writeFailure(w, r, httperr.CodeConflict, "operation conflicts with the current resource state")
	case errors.Is(err, domain.ErrUnavailable):
		writeFailure(w, r, httperr.CodeUnavailable, "service unavailable")
	default:
		httperr.Write(w, r, httperr.Internal())
	}
}
