package runtime

import (
	"context"
	"fmt"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	connectorsruntime "github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
)

// ConnectorControl is the connector-runtime control surface used by the
// ingestion runtime. *connectorsruntime.Runtime implements it.
type ConnectorControl interface {
	Submit(ctx context.Context, tenantID string, connector *connectordomain.Connector) (runtimeRef string, err error)
	Stop(ctx context.Context, tenantID, runtimeRef string) error
	Status(ctx context.Context, tenantID, runtimeRef string) (connectorsruntime.RuntimeStatus, error)
}

// ConnectorJobRuntime adapts the connector runtime to the ingestion JobRuntime
// port. It also implements the ingestion service RuntimeStopper port.
type ConnectorJobRuntime struct {
	control ConnectorControl
}

// NewConnectorJobRuntime wraps a connector runtime control surface. A nil
// control is a configuration error surfaced on first use, never a silent allow.
func NewConnectorJobRuntime(control ConnectorControl) *ConnectorJobRuntime {
	return &ConnectorJobRuntime{control: control}
}

// Submit creates or updates the source and sink connectors for one sync job.
func (a *ConnectorJobRuntime) Submit(ctx context.Context, tenantID string, connector *connectordomain.Connector) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	return a.control.Submit(ctx, tenantID, connector)
}

// Stop stops both runtime connectors for one runtime reference.
func (a *ConnectorJobRuntime) Stop(ctx context.Context, tenantID, runtimeRef string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.control.Stop(ctx, tenantID, runtimeRef)
}

// Status returns the normalized source/sink state for one runtime reference.
func (a *ConnectorJobRuntime) Status(ctx context.Context, tenantID, runtimeRef string) (RuntimeStatus, error) {
	if err := a.ready(); err != nil {
		return RuntimeStatus{}, err
	}
	status, err := a.control.Status(ctx, tenantID, runtimeRef)
	if err != nil {
		return RuntimeStatus{}, err
	}
	return RuntimeStatus{
		Source: mapConnectorState(status.Source),
		Sink:   mapConnectorState(status.Sink),
	}, nil
}

func (a *ConnectorJobRuntime) ready() error {
	if a == nil || a.control == nil {
		return fmt.Errorf("connector runtime is not configured")
	}
	return nil
}

func mapConnectorState(state connectorsruntime.State) RuntimeState {
	switch state {
	case connectorsruntime.StateRunning:
		return StateRunning
	case connectorsruntime.StateFailed:
		return StateFailed
	case connectorsruntime.StatePaused:
		return StatePaused
	case connectorsruntime.StateStopped:
		return StateStopped
	case connectorsruntime.StateMissing:
		return StateMissing
	default:
		// unassigned/unknown states must keep the job running rather than guess.
		return StateUnknown
	}
}
