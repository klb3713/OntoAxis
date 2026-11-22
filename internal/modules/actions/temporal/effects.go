package temporal

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
	objectsdomain "github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	objectservice "github.com/klb3713/ontoaxis/internal/modules/objects/service"
)

// ObjectEffectApplier applies action steps to business objects through the
// objects service. It is deliberately the only place in the worker that writes
// business data; the workflow itself stays deterministic and I/O free.
type ObjectEffectApplier struct {
	Objects objectservice.Service
	// TestHooks enables the deterministic failure-injection activities used by
	// the local E2E smoke. Production wiring leaves it false.
	TestHooks bool
}

var _ EffectApplier = ObjectEffectApplier{}

// Apply implements the supported object activity types and returns before/after
// snapshots suitable for the step ledger and compensation.
func (a ObjectEffectApplier) Apply(ctx context.Context, tenantID, _ uuid.UUID, step domain.StepDefinition, params map[string]any) (map[string]any, map[string]any, error) {
	switch step.ActivityType {
	case domain.ActivityTypeNoop:
		return nil, nil, nil
	case domain.ActivityTypeTestTransientFailure:
		if !a.TestHooks {
			return nil, nil, fmt.Errorf("test activity %q is disabled", step.ActivityType)
		}
		return nil, nil, fmt.Errorf("injected transient failure")
	case domain.ActivityTypeTestPermanentFailure:
		if !a.TestHooks {
			return nil, nil, fmt.Errorf("test activity %q is disabled", step.ActivityType)
		}
		return nil, nil, fmt.Errorf("%w: injected permanent failure", domain.ErrInvalidInput)
	case domain.ActivityTypeObjectUpdate:
		if a.Objects == nil {
			return nil, nil, fmt.Errorf("object effect applier is not configured")
		}
		objectID, err := objectIDFromParams(params)
		if err != nil {
			return nil, nil, err
		}
		before, err := a.Objects.GetObject(ctx, tenantID, objectID)
		if err != nil {
			return nil, nil, fmt.Errorf("load object: %w", err)
		}
		properties, err := propertiesFromParams(params)
		if err != nil {
			return nil, nil, err
		}
		replace, _ := params["replace"].(bool)
		after, err := a.Objects.UpdateObject(ctx, tenantID, objectID, properties, replace)
		if err != nil {
			return nil, nil, fmt.Errorf("update object: %w", err)
		}
		return objectSnapshot(before), objectSnapshot(after), nil
	case domain.ActivityTypeObjectCreate:
		if a.Objects == nil {
			return nil, nil, fmt.Errorf("object effect applier is not configured")
		}
		objectTypeID, err := objectTypeIDFromParams(params)
		if err != nil {
			return nil, nil, err
		}
		properties, err := propertiesFromParams(params)
		if err != nil {
			return nil, nil, err
		}
		after, err := a.Objects.CreateObject(ctx, tenantID, objectTypeID, properties)
		if err != nil {
			return nil, nil, fmt.Errorf("create object: %w", err)
		}
		return nil, objectSnapshot(after), nil
	case domain.ActivityTypeObjectDelete:
		if a.Objects == nil {
			return nil, nil, fmt.Errorf("object effect applier is not configured")
		}
		objectID, err := objectIDFromParams(params)
		if err != nil {
			return nil, nil, err
		}
		before, err := a.Objects.GetObject(ctx, tenantID, objectID)
		if err != nil {
			return nil, nil, fmt.Errorf("load object: %w", err)
		}
		if err := a.Objects.DeleteObject(ctx, tenantID, objectID); err != nil {
			return nil, nil, fmt.Errorf("delete object: %w", err)
		}
		return objectSnapshot(before), nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported activity type %q", step.ActivityType)
	}
}

func objectIDFromParams(params map[string]any) (uuid.UUID, error) {
	raw, _ := params["object_id"].(string)
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, fmt.Errorf("object_id is required")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("object_id must be a UUID: %w", err)
	}
	return id, nil
}

func objectTypeIDFromParams(params map[string]any) (uuid.UUID, error) {
	raw, _ := params["object_type_id"].(string)
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, fmt.Errorf("object_type_id is required")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("object_type_id must be a UUID: %w", err)
	}
	return id, nil
}

func propertiesFromParams(params map[string]any) (map[string]any, error) {
	raw, ok := params["properties"]
	if !ok || raw == nil {
		return map[string]any{}, nil
	}
	properties, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("properties must be an object")
	}
	return properties, nil
}

func objectSnapshot(object objectsdomain.Object) map[string]any {
	return map[string]any{
		"object_id":      object.ID.String(),
		"object_type_id": object.ObjectTypeID.String(),
		"properties":     object.Properties,
		"version":        object.Version,
	}
}
