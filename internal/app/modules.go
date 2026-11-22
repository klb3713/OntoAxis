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

// businessModules keeps typed references to the nine core business modules so
// the runtime composition root can wire cross-module seams (authorization
// guards, resource readers, search, ingestion runtime) before the module
// instances are handed to the registry. The order of list() is the stable
// business module order; authorization is deliberately not part of it.
type businessModules struct {
	tenancy    *tenancy.Module
	connectors *connectors.Module
	ingestion  *ingestion.Module
	datasets   *datasets.Module
	pipelines  *pipelines.Module
	ontology   *ontology.Module
	objects    *objects.Module
	query      *query.Module
	actions    *actions.Module
}

// list returns the nine business modules in their fixed order.
func (b *businessModules) list() []module.Module {
	return []module.Module{
		b.tenancy,
		b.connectors,
		b.ingestion,
		b.datasets,
		b.pipelines,
		b.ontology,
		b.objects,
		b.query,
		b.actions,
	}
}

// buildBusinessModules constructs the nine core business modules. pipelineOpts
// are forwarded to the pipelines module so the runtime assembly can inject the
// pipeline execution configuration while the test-oriented BuildModules stays
// free of environment coupling.
func buildBusinessModules(pool *pgxpool.Pool, cfg config.Config, pipelineOpts ...pipelines.Option) *businessModules {
	tenancyModule := tenancy.New(pool)
	connectorsModule := connectors.New()
	ingestionModule := ingestion.New()
	datasetsModule := datasets.New(datasets.WithTenantResolver(requestTenantResolver{}))

	pipelineOptions := make([]pipelines.Option, 0, len(pipelineOpts)+2)
	pipelineOptions = append(pipelineOptions,
		pipelines.WithTenantResolver(requestTenantResolver{}),
		pipelines.WithDatasetResolver(datasetLookup{module: datasetsModule}),
	)
	pipelineOptions = append(pipelineOptions, pipelineOpts...)
	pipelinesModule := pipelines.New(pool, pipelineOptions...)

	ontologyModule := ontology.New()
	objectTypes := objectTypeLookup{module: ontologyModule}
	objectsModule := objects.New(pool, objectsservice.WithObjectTypeValidator(objectTypes))
	queryModule := query.New(pool, cfg.TrinoURL, cfg.TrinoUser, cfg.TrinoTimeout, objectsModule.Service())
	actionsModule := actions.New(pool, actionsservice.WithObjectTypeValidator(objectTypes))

	return &businessModules{
		tenancy:    tenancyModule,
		connectors: connectorsModule,
		ingestion:  ingestionModule,
		datasets:   datasetsModule,
		pipelines:  pipelinesModule,
		ontology:   ontologyModule,
		objects:    objectsModule,
		query:      queryModule,
		actions:    actionsModule,
	}
}

// BuildModules is the environment-free process composition used by unit tests.
// It constructs the nine core business modules but performs no authorization,
// search, pipeline or connector runtime wiring; production assembly lives in
// BuildRuntimeAssembly. Module order is advisory; the registry topologically
// sorts by Dependencies.
func BuildModules(pool *pgxpool.Pool, cfg config.Config) []module.Module {
	return buildBusinessModules(pool, cfg).list()
}
