package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/service"
)

type fakePreflight struct {
	mu     sync.Mutex
	calls  int
	err    error
	tenant string
	conn   *domain.Connector
	onCall func()
}

func (f *fakePreflight) Preflight(_ context.Context, tenantID string, connector *domain.Connector) error {
	f.mu.Lock()
	f.calls++
	f.tenant = tenantID
	f.conn = connector
	onCall := f.onCall
	err := f.err
	f.mu.Unlock()
	if onCall != nil {
		onCall()
	}
	return err
}

func (f *fakePreflight) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type recordingStarter struct {
	mu    sync.Mutex
	id    string
	order *[]string
	calls int
}

func (s *recordingStarter) StartSync(context.Context, string, string) (string, error) {
	s.mu.Lock()
	s.calls++
	order := s.order
	s.mu.Unlock()
	if order != nil {
		*order = append(*order, "start")
	}
	return s.id, nil
}

func (s *recordingStarter) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestStartSyncCallsPreflightBeforeStarter(t *testing.T) {
	t.Parallel()

	var order []string
	preflight := &fakePreflight{onCall: func() { order = append(order, "preflight") }}
	starter := &recordingStarter{id: "job-1", order: &order}
	svc, _ := newService(t, service.WithSyncPreflight(preflight), service.WithSyncStarter(starter))

	created, err := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
	if err != nil {
		t.Fatalf("CreateConnector() error = %v", err)
	}

	jobID, err := svc.StartSync(context.Background(), tenantA, created.ID)
	if err != nil {
		t.Fatalf("StartSync() error = %v", err)
	}
	if jobID != "job-1" {
		t.Fatalf("StartSync() jobID = %q, want job-1", jobID)
	}
	if len(order) != 2 || order[0] != "preflight" || order[1] != "start" {
		t.Fatalf("call order = %v, want [preflight start]", order)
	}
	if preflight.callCount() != 1 {
		t.Fatalf("preflight calls = %d, want 1", preflight.callCount())
	}
	if preflight.tenant != tenantA || preflight.conn == nil || preflight.conn.ID != created.ID {
		t.Fatalf("preflight received tenant=%q connector=%+v", preflight.tenant, preflight.conn)
	}
	if preflight.conn.Status != domain.StatusActive {
		t.Fatalf("preflight saw status %q, want active", preflight.conn.Status)
	}
}

func TestStartSyncPreflightFailureCreatesNoJob(t *testing.T) {
	t.Parallel()

	preflight := &fakePreflight{err: errors.New("kafka connect unreachable")}
	starter := &recordingStarter{id: "job-1"}
	svc, _ := newService(t, service.WithSyncPreflight(preflight), service.WithSyncStarter(starter))

	created, err := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
	if err != nil {
		t.Fatalf("CreateConnector() error = %v", err)
	}

	jobID, err := svc.StartSync(context.Background(), tenantA, created.ID)
	if !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("StartSync() error = %v, want %v", err, service.ErrUnavailable)
	}
	if jobID != "" {
		t.Fatalf("StartSync() jobID = %q, want empty on preflight failure", jobID)
	}
	if starter.callCount() != 0 {
		t.Fatalf("sync starter called %d times, want 0", starter.callCount())
	}
}

func TestStartSyncPreflightErrorClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		wantErr   error
		wantOther error
	}{
		{
			name:    "invalid input preserved",
			err:     fmt.Errorf("%w: invalid config", service.ErrInvalidInput),
			wantErr: service.ErrInvalidInput,
		},
		{
			name:    "conflict preserved",
			err:     fmt.Errorf("%w: runtime busy", domain.ErrConflict),
			wantErr: domain.ErrConflict,
		},
		{
			name:      "other errors become unavailable",
			err:       errors.New("boom"),
			wantErr:   service.ErrUnavailable,
			wantOther: service.ErrInvalidInput,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			preflight := &fakePreflight{err: tt.err}
			starter := &recordingStarter{id: "job-1"}
			svc, _ := newService(t, service.WithSyncPreflight(preflight), service.WithSyncStarter(starter))

			created, err := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
			if err != nil {
				t.Fatalf("CreateConnector() error = %v", err)
			}
			jobID, err := svc.StartSync(context.Background(), tenantA, created.ID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("StartSync() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantOther != nil && errors.Is(err, tt.wantOther) {
				t.Fatalf("StartSync() error = %v, must not be %v", err, tt.wantOther)
			}
			if jobID != "" {
				t.Fatalf("StartSync() jobID = %q, want empty", jobID)
			}
			if starter.callCount() != 0 {
				t.Fatalf("sync starter called %d times, want 0", starter.callCount())
			}
		})
	}
}

func TestStartSyncInactiveConnectorSkipsPreflight(t *testing.T) {
	t.Parallel()

	preflight := &fakePreflight{}
	starter := &recordingStarter{id: "job-1"}
	svc, _ := newService(t, service.WithSyncPreflight(preflight), service.WithSyncStarter(starter))

	created, err := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
	if err != nil {
		t.Fatalf("CreateConnector() error = %v", err)
	}
	paused := "paused"
	if _, err := svc.UpdateConnector(context.Background(), tenantA, created.ID, service.UpdateInput{Status: &paused}); err != nil {
		t.Fatalf("UpdateConnector() error = %v", err)
	}

	if _, err := svc.StartSync(context.Background(), tenantA, created.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("StartSync() error = %v, want %v", err, domain.ErrConflict)
	}
	if preflight.callCount() != 0 {
		t.Fatalf("preflight called %d times for inactive connector, want 0", preflight.callCount())
	}
	if starter.callCount() != 0 {
		t.Fatalf("sync starter called %d times for inactive connector, want 0", starter.callCount())
	}
}

func TestStartSyncMissingStarterSkipsPreflight(t *testing.T) {
	t.Parallel()

	preflight := &fakePreflight{}
	svc, _ := newService(t, service.WithSyncPreflight(preflight))

	created, err := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
	if err != nil {
		t.Fatalf("CreateConnector() error = %v", err)
	}
	if _, err := svc.StartSync(context.Background(), tenantA, created.ID); !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("StartSync() error = %v, want %v", err, service.ErrUnavailable)
	}
	if preflight.callCount() != 0 {
		t.Fatalf("preflight called %d times without a sync starter, want 0", preflight.callCount())
	}
}

func TestStartSyncWithoutPreflightKeepsBehavior(t *testing.T) {
	t.Parallel()

	starter := &recordingStarter{id: "job-1"}
	svc, _ := newService(t, service.WithSyncStarter(starter))

	created, err := svc.CreateConnector(context.Background(), tenantA, createInput("pg-main"))
	if err != nil {
		t.Fatalf("CreateConnector() error = %v", err)
	}
	jobID, err := svc.StartSync(context.Background(), tenantA, created.ID)
	if err != nil {
		t.Fatalf("StartSync() error = %v", err)
	}
	if jobID != "job-1" {
		t.Fatalf("StartSync() jobID = %q, want job-1", jobID)
	}
}
