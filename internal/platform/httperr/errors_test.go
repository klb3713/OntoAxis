package httperr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteStableErrorShape(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/missing", nil)
	rec := httptest.NewRecorder()
	Write(rec, req, NotFound("resource not found"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body struct {
		Error Error `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != CodeNotFound || body.Error.Message != "resource not found" || body.Error.Retryable {
		t.Fatalf("unexpected error: %+v", body.Error)
	}
}

func TestInternalDoesNotExposeDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, httptest.NewRequest(http.MethodGet, "/", nil), Internal())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body struct {
		Error Error `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.Message != "internal server error" {
		t.Fatalf("leaked internal message: %q", body.Error.Message)
	}
}
