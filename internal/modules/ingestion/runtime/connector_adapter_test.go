package runtime_test

import (
	"context"
	"errors"
	"testing"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	connectorsruntime "github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
	ingestionruntime "github.com/klb3713/ontoaxis/internal/modules/ingestion/runtime"
)

type fakeConnectorControl struct {
	submitRef string
	submitErr error
	stopErr   error
	status    connectorsruntime.RuntimeStatus
	statusErr error
	gotTenant string
	gotRef    string
}

func (f *fakeConnectorControl) Submit(_ context.Context, tenantID string, _ *connectordomain.Connector) (string, error) {
	f.gotTenant = tenantID
	return f.submitRef, f.submitErr
}

func (f *fakeConnectorControl) Stop(_ context.Context, tenantID, runtimeRef string) error {
	f.gotTenant, f.gotRef = tenantID, runtimeRef
	return f.stopErr
}

func (f *fakeConnectorControl) Status(_ context.Context, tenantID, runtimeRef string) (connectorsruntime.RuntimeStatus, error) {
	f.gotTenant, f.gotRef = tenantID, runtimeRef
	return f.status, f.statusErr
}

func TestConnectorJobRuntimeMapsStates(t *testing.T) {
	t.Parallel()
	control := &fakeConnectorControl{status: connectorsruntime.RuntimeStatus{
		Source: connectorsruntime.StateRunning,
		Sink:   connectorsruntime.StatePaused,
	}}
	adapter := ingestionruntime.NewConnectorJobRuntime(control)
	status, err := adapter.Status(context.Background(), "t1", "oa-x-y-sink")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Source != ingestionruntime.StateRunning || status.Sink != ingestionruntime.StatePaused {
		t.Fatalf("unexpected mapped status: %+v", status)
	}
	if control.gotTenant != "t1" || control.gotRef != "oa-x-y-sink" {
		t.Fatalf("unexpected scope passed to control: tenant=%q ref=%q", control.gotTenant, control.gotRef)
	}
}

func TestConnectorJobRuntimeMapsUnassignedToUnknown(t *testing.T) {
	t.Parallel()
	adapter := ingestionruntime.NewConnectorJobRuntime(&fakeConnectorControl{status: connectorsruntime.RuntimeStatus{
		Source: connectorsruntime.StateUnassigned,
		Sink:   connectorsruntime.StateRunning,
	}})
	status, err := adapter.Status(context.Background(), "t1", "oa-x-y-sink")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Source != ingestionruntime.StateUnknown {
		t.Fatalf("unassigned state = %q, want unknown", status.Source)
	}
}

func TestConnectorJobRuntimePropagatesErrors(t *testing.T) {
	t.Parallel()
	want := errors.New("connect unavailable")
	adapter := ingestionruntime.NewConnectorJobRuntime(&fakeConnectorControl{submitErr: want, stopErr: want})
	if _, err := adapter.Submit(context.Background(), "t1", &connectordomain.Connector{}); !errors.Is(err, want) {
		t.Fatalf("Submit() error = %v, want %v", err, want)
	}
	if err := adapter.Stop(context.Background(), "t1", "oa-x-y-sink"); !errors.Is(err, want) {
		t.Fatalf("Stop() error = %v, want %v", err, want)
	}
}

func TestConnectorJobRuntimeNilControlFailsClosed(t *testing.T) {
	t.Parallel()
	adapter := ingestionruntime.NewConnectorJobRuntime(nil)
	if _, err := adapter.Submit(context.Background(), "t1", &connectordomain.Connector{}); err == nil {
		t.Fatal("Submit() with nil control succeeded")
	}
	if err := adapter.Stop(context.Background(), "t1", "oa-x-y-sink"); err == nil {
		t.Fatal("Stop() with nil control succeeded")
	}
}
