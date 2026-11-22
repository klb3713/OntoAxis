package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	datasetdomain "github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
	objectsdomain "github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	ontologyapi "github.com/klb3713/ontoaxis/internal/modules/ontology/api"
	ontologydomain "github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
	"github.com/klb3713/ontoaxis/internal/platform/config"
)

// clearAssemblyEnv neutralises every environment variable the runtime assembly
// reads so each test starts from a known, disabled-by-default baseline.
func clearAssemblyEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		// authorization
		"ONTOAXIS_AUTHZ_OPENFGA_API_URL",
		"ONTOAXIS_AUTHZ_OPENFGA_STORE_ID",
		"ONTOAXIS_AUTHZ_OPENFGA_MODEL_ID",
		"ONTOAXIS_AUTHZ_OPENFGA_API_TOKEN",
		"ONTOAXIS_AUTHZ_OPENFGA_TIMEOUT",
		"ONTOAXIS_AUTHZ_DECISION_TIMEOUT",
		"ONTOAXIS_AUTHZ_POLICY_DIR",
		"ONTOAXIS_AUTHZ_AUDIT_SINK",
		"ONTOAXIS_AUTHZ_SOURCE_COMMIT",
		"AUTH_DISABLED",
		"ONTOAXIS_AUTHZ_DISABLED",
		"ONTOAXIS_AUTHZ_FAIL_OPEN",
		// pipelines
		"ONTOAXIS_KESTRA_URL",
		"ONTOAXIS_KESTRA_API_TOKEN",
		"ONTOAXIS_KESTRA_TIMEOUT",
		"ONTOAXIS_PIPELINE_NAMESPACE_PREFIX",
		"ONTOAXIS_PIPELINE_DBT_CATALOG",
		"ONTOAXIS_PIPELINE_DBT_IMAGE",
		"ONTOAXIS_PIPELINE_RECONCILE_THROTTLE",
		"ONTOAXIS_PIPELINE_RUN_RECOVERY_TIMEOUT",
		"ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS",
		"ONTOAXIS_PIPELINE_IDEMPOTENCY_TTL",
		// connectors runtime
		"ONTOAXIS_CONNECT_URL",
		"ONTOAXIS_CONNECT_TIMEOUT",
		"ONTOAXIS_CONNECT_RETRY_MAX",
		"ONTOAXIS_CONNECT_VERIFY_MAX_ROWS",
		"ONTOAXIS_ICEBERG_CATALOG_URI",
		"ONTOAXIS_ICEBERG_WAREHOUSE",
		"ONTOAXIS_ICEBERG_NAMESPACE",
		"ONTOAXIS_ICEBERG_CATALOG_USER",
		// search
		"ONTOAXIS_SEARCH_ENABLED",
		"ONTOAXIS_SEARCH_URL",
		"ONTOAXIS_SEARCH_API_KEY",
		"ONTOAXIS_SEARCH_TIMEOUT",
		"ONTOAXIS_SEARCH_INDEX_VERSION",
		"ONTOAXIS_SEARCH_SYNC_WINDOW",
		"ONTOAXIS_SEARCH_POLL_INTERVAL",
		"ONTOAXIS_SEARCH_CONSUMER_BATCH",
		"ONTOAXIS_SEARCH_CONSUMER_MAX_ATTEMPTS",
		// actions
		"ONTOAXIS_ACTIONS_EXECUTION_MODE",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

// TestBuildRuntimeAssemblyRequiresAuthzEnv proves the assembly never starts
// without authorization configuration (no fail-open default).
func TestBuildRuntimeAssemblyRequiresAuthzEnv(t *testing.T) {
	clearAssemblyEnv(t)

	_, err := BuildRuntimeAssembly(context.Background(), nil, config.Defaults(), nil, nil)
	if err == nil {
		t.Fatal("expected an error when ONTOAXIS_AUTHZ_* is missing, got nil")
	}
	if !strings.Contains(err.Error(), "authorization") {
		t.Fatalf("expected authorization error, got %v", err)
	}
}

// TestBuildRuntimeAssemblyRejectsInvalidPipelineEnv proves an invalid pipeline
// duration aborts startup before any module or authorizer is built.
func TestBuildRuntimeAssemblyRejectsInvalidPipelineEnv(t *testing.T) {
	clearAssemblyEnv(t)
	t.Setenv("ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS", "0")

	_, err := BuildRuntimeAssembly(context.Background(), nil, config.Defaults(), nil, nil)
	if err == nil {
		t.Fatal("expected an error for invalid pipeline retry attempts, got nil")
	}
	if !strings.Contains(err.Error(), "pipeline") {
		t.Fatalf("expected pipeline error, got %v", err)
	}
}

// TestBuildRuntimeAssemblyRejectsInvalidConnectorEnv proves an invalid connector
// runtime value aborts startup.
func TestBuildRuntimeAssemblyRejectsInvalidConnectorEnv(t *testing.T) {
	clearAssemblyEnv(t)
	t.Setenv("ONTOAXIS_CONNECT_URL", "http://127.0.0.1:8083")
	t.Setenv("ONTOAXIS_CONNECT_TIMEOUT", "not-a-duration")

	_, err := BuildRuntimeAssembly(context.Background(), nil, config.Defaults(), nil, nil)
	if err == nil {
		t.Fatal("expected an error for invalid connector timeout, got nil")
	}
	if !strings.Contains(err.Error(), "connector") {
		t.Fatalf("expected connector error, got %v", err)
	}
}

// TestBuildModulesNineWithoutAuthz keeps the shared contract that the
// test-oriented BuildModules stays at nine business modules and does not fold
// the platform authorization module into that count.
func TestBuildModulesNineWithoutAuthz(t *testing.T) {
	mods := BuildModules(nil, config.Defaults())
	if len(mods) != 9 {
		t.Fatalf("expected 9 business modules, got %d", len(mods))
	}
	for _, m := range mods {
		if m.Name() == "authorization" {
			t.Fatalf("authorization must not be counted among the nine business modules")
		}
	}
}

// fakeOntologyObjectType is a stub ontology lookup for adapter tests.
type fakeOntologyObjectType struct {
	objectType *ontologydomain.ObjectType
	err        error
}

func (f fakeOntologyObjectType) GetObjectType(context.Context, string, string) (*ontologydomain.ObjectType, error) {
	return f.objectType, f.err
}

// TestObjectTypeFactReaderMapping proves the objects authz reader maps a missing
// object type to objects domain.ErrNotFound and surfaces the tenant-scoped fact.
func TestObjectTypeFactReaderMapping(t *testing.T) {
	tenantID := uuid.New()
	objectTypeID := uuid.New()

	missing := objectTypeFactReader{service: func() ontologyObjectType {
		return fakeOntologyObjectType{err: ontologydomain.ErrNotFound}
	}}
	if _, err := missing.GetObjectTypeFact(context.Background(), tenantID, objectTypeID); !errors.Is(err, objectsdomain.ErrNotFound) {
		t.Fatalf("expected objects domain.ErrNotFound, got %v", err)
	}

	found := objectTypeFactReader{service: func() ontologyObjectType {
		return fakeOntologyObjectType{objectType: &ontologydomain.ObjectType{ID: objectTypeID.String(), TenantID: tenantID.String()}}
	}}
	fact, err := found.GetObjectTypeFact(context.Background(), tenantID, objectTypeID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fact.ID != objectTypeID.String() || fact.TenantID != tenantID.String() {
		t.Fatalf("unexpected fact: %+v", fact)
	}
}

// fakeDatasetGetter is a stub dataset lookup for adapter tests.
type fakeDatasetGetter struct {
	dataset datasetdomain.Dataset
	err     error
}

func (f fakeDatasetGetter) GetDataset(context.Context, string, string) (datasetdomain.Dataset, error) {
	return f.dataset, f.err
}

// TestDatasetFactReaderMapping proves the ontology authz reader maps a missing
// dataset to ontology api.ErrDatasetNotFound and surfaces the fact.
func TestDatasetFactReaderMapping(t *testing.T) {
	missing := datasetFactReader{service: func() datasetGetter {
		return fakeDatasetGetter{err: datasetdomain.ErrNotFound}
	}}
	if _, err := missing.GetDatasetFact(context.Background(), "tenant-1", "dataset-1"); !errors.Is(err, ontologyapi.ErrDatasetNotFound) {
		t.Fatalf("expected ontology api.ErrDatasetNotFound, got %v", err)
	}

	found := datasetFactReader{service: func() datasetGetter {
		return fakeDatasetGetter{dataset: datasetdomain.Dataset{ID: "dataset-1", TenantID: "tenant-1"}}
	}}
	fact, err := found.GetDatasetFact(context.Background(), "tenant-1", "dataset-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fact.ID != "dataset-1" || fact.TenantID != "tenant-1" {
		t.Fatalf("unexpected fact: %+v", fact)
	}
}
