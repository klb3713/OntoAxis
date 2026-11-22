package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	datasetdomain "github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
	objectsapi "github.com/klb3713/ontoaxis/internal/modules/objects/api"
	objectsdomain "github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	ontologyapi "github.com/klb3713/ontoaxis/internal/modules/ontology/api"
	ontologydomain "github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
)

// ontologyObjectType is the narrow ontology lookup used to build authoritative
// object-type authorization facts. *ontology.Module satisfies it lazily via
// Service(), so the adapter is safe to build before route registration.
type ontologyObjectType interface {
	GetObjectType(ctx context.Context, tenantID, id string) (*ontologydomain.ObjectType, error)
}

// objectTypeFactReader adapts the ontology service to objects api.ObjectTypeReader.
// It enforces tenant scoping by delegating to the ontology service (which scopes
// every read by tenant) and maps a missing object type to objects domain.ErrNotFound.
type objectTypeFactReader struct {
	service func() ontologyObjectType
}

func (r objectTypeFactReader) GetObjectTypeFact(ctx context.Context, tenantID, objectTypeID uuid.UUID) (objectsapi.ObjectTypeFact, error) {
	svc := r.service()
	if svc == nil {
		return objectsapi.ObjectTypeFact{}, fmt.Errorf("ontology service is not available")
	}
	objectType, err := svc.GetObjectType(ctx, tenantID.String(), objectTypeID.String())
	if err != nil {
		if errors.Is(err, ontologydomain.ErrNotFound) {
			return objectsapi.ObjectTypeFact{}, objectsdomain.ErrNotFound
		}
		return objectsapi.ObjectTypeFact{}, err
	}
	return objectsapi.ObjectTypeFact{ID: objectType.ID, TenantID: objectType.TenantID}, nil
}

// datasetGetter is the narrow datasets lookup used to validate dataset bindings.
type datasetGetter interface {
	GetDataset(ctx context.Context, tenantID, datasetID string) (datasetdomain.Dataset, error)
}

// datasetFactReader adapts the datasets service to ontology api.DatasetReader. It
// maps a missing dataset to ontology api.ErrDatasetNotFound so the ontology
// handler can return 404 instead of a fail-open binding.
type datasetFactReader struct {
	service func() datasetGetter
}

func (r datasetFactReader) GetDatasetFact(ctx context.Context, tenantID, datasetID string) (ontologyapi.DatasetFact, error) {
	svc := r.service()
	if svc == nil {
		return ontologyapi.DatasetFact{}, fmt.Errorf("datasets service is not available")
	}
	dataset, err := svc.GetDataset(ctx, tenantID, datasetID)
	if err != nil {
		if errors.Is(err, datasetdomain.ErrNotFound) {
			return ontologyapi.DatasetFact{}, ontologyapi.ErrDatasetNotFound
		}
		return ontologyapi.DatasetFact{}, err
	}
	return ontologyapi.DatasetFact{ID: dataset.ID, TenantID: dataset.TenantID}, nil
}

// connectorsGetter is the narrow connectors lookup used to build the ingestion
// connector authorization fact. *connectors.Module satisfies it lazily.
type connectorsGetter interface {
	GetConnector(ctx context.Context, tenantID, id string) (*connectordomain.Connector, error)
}

// connectorReader adapts the connectors service to ingestion api.ConnectorReader.
type connectorReader struct {
	service func() connectorsGetter
}

func (r connectorReader) GetConnector(ctx context.Context, tenantID, id string) (*connectordomain.Connector, error) {
	svc := r.service()
	if svc == nil {
		return nil, fmt.Errorf("connectors service is not available")
	}
	return svc.GetConnector(ctx, tenantID, id)
}
