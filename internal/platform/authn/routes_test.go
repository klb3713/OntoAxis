package authn

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v3"
)

type edgeRouteTable struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name  string `yaml:"name"`
		Owner string `yaml:"owner"`
		Stage string `yaml:"stage"`
	} `yaml:"metadata"`
	Spec struct {
		Gateway struct {
			Provider     string `yaml:"provider"`
			Listener     string `yaml:"listener"`
			PublicRoutes []struct {
				Method string `yaml:"method"`
				Path   string `yaml:"path"`
			} `yaml:"publicRoutes"`
			Metrics struct {
				Path         string `yaml:"path"`
				InternalOnly bool   `yaml:"internalOnly"`
			} `yaml:"metrics"`
			ClientHeadersToStrip    []string `yaml:"clientHeadersToStrip"`
			InternalHeadersToInject []string `yaml:"internalHeadersToInject"`
		} `yaml:"gateway"`
		Routes []struct {
			Name   string `yaml:"name"`
			Prefix string `yaml:"prefix"`
			Module string `yaml:"module"`
			Auth   string `yaml:"auth"`
		} `yaml:"routes"`
	} `yaml:"spec"`
}

func loadEdgeRouteTable(t *testing.T) edgeRouteTable {
	t.Helper()
	path := filepath.Join("..", "..", "..", "config", "edge", "routes.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var table edgeRouteTable
	if err := yaml.Unmarshal(data, &table); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return table
}

func TestEdgeRouteTableCoversAllBusinessPrefixes(t *testing.T) {
	table := loadEdgeRouteTable(t)
	if table.APIVersion != "ontoaxis.edge/v1" || table.Kind != "EdgeRouteTable" {
		t.Fatalf("apiVersion/kind = %q/%q", table.APIVersion, table.Kind)
	}
	if table.Metadata.Name != "oa-edge" {
		t.Fatalf("metadata.name = %q", table.Metadata.Name)
	}

	want := []struct {
		name   string
		prefix string
	}{
		{"tenancy", "/api/v1/tenancy"},
		{"ontology", "/api/v1/ontology"},
		{"datasets", "/api/v1/datasets"},
		{"connectors", "/api/v1/connectors"},
		{"ingestion", "/api/v1/ingestion"},
		{"pipelines", "/api/v1/pipelines"},
		{"objects", "/api/v1/objects"},
		{"query", "/api/v1/query"},
		{"actions", "/api/v1/actions"},
	}
	if len(table.Spec.Routes) != len(want) {
		t.Fatalf("route count = %d, want %d", len(table.Spec.Routes), len(want))
	}
	for i, route := range table.Spec.Routes {
		if route.Name != want[i].name || route.Prefix != want[i].prefix {
			t.Fatalf("route[%d] = %s %s, want %s %s", i, route.Name, route.Prefix, want[i].name, want[i].prefix)
		}
		if route.Module != want[i].name {
			t.Fatalf("route %s module = %q, want %q", route.Name, route.Module, want[i].name)
		}
		if route.Auth != "jwt" {
			t.Fatalf("route %s auth = %q, want jwt", route.Name, route.Auth)
		}
	}
}

func TestEdgeRouteTablePublicAndInternalRoutes(t *testing.T) {
	table := loadEdgeRouteTable(t)
	gotPublic := make([]string, 0, len(table.Spec.Gateway.PublicRoutes))
	for _, route := range table.Spec.Gateway.PublicRoutes {
		gotPublic = append(gotPublic, route.Method+" "+route.Path)
	}
	wantPublic := []string{"GET /healthz", "GET /readyz", "GET /.well-known/*"}
	if !reflect.DeepEqual(gotPublic, wantPublic) {
		t.Fatalf("publicRoutes = %v, want %v", gotPublic, wantPublic)
	}
	if table.Spec.Gateway.Metrics.Path != "/metrics" || !table.Spec.Gateway.Metrics.InternalOnly {
		t.Fatalf("metrics = %+v, want internal-only /metrics", table.Spec.Gateway.Metrics)
	}
}

func TestEdgeRouteTableHeaderContract(t *testing.T) {
	table := loadEdgeRouteTable(t)
	wantStrip := []string{"X-Tenant-Id", "X-User-Id", "X-Org-Ids", "X-Workspace-Ids", "X-Roles"}
	if !reflect.DeepEqual(table.Spec.Gateway.ClientHeadersToStrip, wantStrip) {
		t.Fatalf("clientHeadersToStrip = %v, want %v", table.Spec.Gateway.ClientHeadersToStrip, wantStrip)
	}
	wantInject := []string{
		"x-ontoaxis-tenant-id",
		"x-ontoaxis-user-id",
		"x-ontoaxis-org-ids",
		"x-ontoaxis-workspace-ids",
		"x-ontoaxis-roles",
		"x-ontoaxis-request-id",
	}
	if !reflect.DeepEqual(table.Spec.Gateway.InternalHeadersToInject, wantInject) {
		t.Fatalf("internalHeadersToInject = %v, want %v", table.Spec.Gateway.InternalHeadersToInject, wantInject)
	}
}
