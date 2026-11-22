package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/actions"
	actionsservice "github.com/klb3713/ontoaxis/internal/modules/actions/service"
	"github.com/klb3713/ontoaxis/internal/modules/connectors"
	datasets "github.com/klb3713/ontoaxis/internal/modules/datasets"
	datasetdomain "github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion"
	"github.com/klb3713/ontoaxis/internal/modules/objects"
	objectsservice "github.com/klb3713/ontoaxis/internal/modules/objects/service"
	"github.com/klb3713/ontoaxis/internal/modules/ontology"
	ontologydomain "github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines"
	"github.com/klb3713/ontoaxis/internal/modules/query"
	"github.com/klb3713/ontoaxis/internal/modules/tenancy"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/config"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

// requestTenantResolver resolves the tenant from the authenticated request
// context. It is shared by modules whose HTTP adapters take an injectable
// resolver instead of reading authn directly.
type requestTenantResolver struct{}

func (requestTenantResolver) ResolveTenant(r *http.Request) (string, error) {
	tc, ok := authn.FromContext(r.Context())
	if !ok || tc.TenantID == uuid.Nil {
		return "", errors.New("authenticated tenant context is required")
	}
	return tc.TenantID.String(), nil
}

// datasetLookup adapts the datasets service to the pipeline module's consumer
// interface without exposing dataset tables.
type datasetLookup struct {
	module *datasets.Module
}

func (l datasetLookup) DatasetExists(ctx context.Context, tenantID, datasetID string) (bool, error) {
	if l.module == nil || l.module.Service() == nil {
		return false, errors.New("datasets module is not ready")
	}
	_, err := l.module.Service().GetDataset(ctx, tenantID, datasetID)
	if errors.Is(err, datasetdomain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// objectTypeLookup adapts the ontology module to the object and action
// boundary validators without exposing ontology tables.
type objectTypeLookup struct{ module *ontology.Module }

func (l objectTypeLookup) ObjectTypeExists(ctx context.Context, tenantID, objectTypeID string) (bool, error) {
	if l.module == nil || l.module.Service() == nil {
		return false, errors.New("ontology module is not ready")
	}
	_, err := l.module.Service().GetObjectType(ctx, tenantID, objectTypeID)
	if errors.Is(err, ontologydomain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// BuildModules is the process composition root. Module order is advisory; the
// registry topologically sorts by Dependencies.
func BuildModules(pool *pgxpool.Pool, cfg config.Config) []module.Module {
	tenancyModule := tenancy.New(pool)
	connectorsModule := connectors.New()
	ingestionModule := ingestion.New()
	datasetsModule := datasets.New(datasets.WithTenantResolver(requestTenantResolver{}))
	pipelinesModule := pipelines.New(pool,
		pipelines.WithTenantResolver(requestTenantResolver{}),
		pipelines.WithDatasetResolver(datasetLookup{module: datasetsModule}),
	)
	ontologyModule := ontology.New()
	objectTypes := objectTypeLookup{module: ontologyModule}
	objectsModule := objects.New(pool, objectsservice.WithObjectTypeValidator(objectTypes))
	queryModule := query.New(pool, cfg.TrinoURL, cfg.TrinoUser, cfg.TrinoTimeout, objectsModule.Service())
	actionsModule := actions.New(pool, actionsservice.WithObjectTypeValidator(objectTypes))

	return []module.Module{
		tenancyModule,
		connectorsModule,
		ingestionModule,
		datasetsModule,
		pipelinesModule,
		ontologyModule,
		objectsModule,
		queryModule,
		actionsModule,
	}
}
