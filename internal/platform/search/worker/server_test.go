package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

type fakeHealth struct{ err error }

func (f fakeHealth) Health(context.Context) error { return f.err }

func TestServerHealthz(t *testing.T) {
	server := NewServer(nil, nil, NewMetrics())
	recorder := httptest.NewRecorder()
	server.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"ok"`) {
		t.Fatalf("healthz body = %q", recorder.Body.String())
	}
}

func TestServerReadyz(t *testing.T) {
	tests := []struct {
		name       string
		db         DatabasePinger
		search     SearchHealth
		wantStatus int
		wantBody   string
	}{
		{name: "ready", db: fakePinger{}, search: fakeHealth{}, wantStatus: http.StatusOK, wantBody: "ready"},
		{name: "postgres down", db: fakePinger{err: errors.New("down")}, search: fakeHealth{}, wantStatus: http.StatusServiceUnavailable, wantBody: "postgres"},
		{name: "search down", db: fakePinger{}, search: fakeHealth{err: errors.New("down")}, wantStatus: http.StatusServiceUnavailable, wantBody: "search"},
		{name: "missing postgres", db: nil, search: fakeHealth{}, wantStatus: http.StatusServiceUnavailable, wantBody: "postgres"},
		{name: "missing search", db: fakePinger{}, search: nil, wantStatus: http.StatusServiceUnavailable, wantBody: "search"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(tt.db, tt.search, NewMetrics())
			recorder := httptest.NewRecorder()
			server.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if recorder.Code != tt.wantStatus {
				t.Fatalf("readyz status = %d, want %d (body %s)", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tt.wantBody) {
				t.Fatalf("readyz body = %q, want %q", recorder.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestServerRejectsWrongMethod(t *testing.T) {
	server := NewServer(fakePinger{}, fakeHealth{}, NewMetrics())
	recorder := httptest.NewRecorder()
	server.HTTPHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz status = %d, want 405", recorder.Code)
	}
}

func TestParseMode(t *testing.T) {
	for _, want := range []Mode{ModeConsume, ModeRebuild, ModeReplay, ModeMigrate} {
		got, err := ParseMode(string(want))
		if err != nil || got != want {
			t.Fatalf("ParseMode(%q) = %q, %v", want, got, err)
		}
	}
	if _, err := ParseMode("nope"); err == nil {
		t.Fatal("ParseMode accepted unknown mode")
	}
}
