package authn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type edgeRouteTable struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name            string `yaml:"name"`
		Owner           string `yaml:"owner"`
		Stage           string `yaml:"stage"`
		ContractVersion int    `yaml:"contractVersion"`
	} `yaml:"metadata"`
	Spec struct {
		Gateway struct {
			Providers []string `yaml:"providers"`
			Listener  struct {
				Name     string `yaml:"name"`
				Protocol string `yaml:"protocol"`
				Port     int    `yaml:"port"`
				TLS      struct {
					Enabled bool `yaml:"enabled"`
				} `yaml:"tls"`
			} `yaml:"listener"`
			PublicRoutes []struct {
				Name   string `yaml:"name"`
				Method string `yaml:"method"`
				Path   string `yaml:"path"`
				Auth   string `yaml:"auth"`
			} `yaml:"publicRoutes"`
			InternalRoutes []struct {
				Name     string `yaml:"name"`
				Method   string `yaml:"method"`
				Path     string `yaml:"path"`
				Exposure string `yaml:"exposure"`
			} `yaml:"internalRoutes"`
			ClientHeadersToStrip    []string `yaml:"clientHeadersToStrip"`
			InternalHeadersToInject []string `yaml:"internalHeadersToInject"`
		} `yaml:"gateway"`
		Upstream struct {
			Service string `yaml:"service"`
			Port    int    `yaml:"port"`
			Scheme  string `yaml:"scheme"`
		} `yaml:"upstream"`
		RateLimit struct {
			Defaults struct {
				RequestsPerSecond int `yaml:"requestsPerSecond"`
				Burst             int `yaml:"burst"`
			} `yaml:"defaults"`
			ByTenant struct {
				RequestsPerMinute int `yaml:"requestsPerMinute"`
			} `yaml:"byTenant"`
			ExemptPublicRoutes bool `yaml:"exemptPublicRoutes"`
		} `yaml:"rateLimit"`
		Auth struct {
			Type             string   `yaml:"type"`
			Header           string   `yaml:"header"`
			Scheme           string   `yaml:"scheme"`
			IssuerEnv        string   `yaml:"issuerEnv"`
			AudienceEnv      string   `yaml:"audienceEnv"`
			JWKSURLEnv       string   `yaml:"jwksURLEnv"`
			ClockSkewSeconds int      `yaml:"clockSkewSeconds"`
			RequiredClaims   []string `yaml:"requiredClaims"`
		} `yaml:"auth"`
		Routes []struct {
			Name     string   `yaml:"name"`
			Prefix   string   `yaml:"prefix"`
			Module   string   `yaml:"module"`
			Auth     string   `yaml:"auth"`
			Priority int      `yaml:"priority"`
			Methods  []string `yaml:"methods"`
		} `yaml:"routes"`
		Generation struct {
			Strategy         string   `yaml:"strategy"`
			Ordering         []string `yaml:"ordering"`
			DigestAnnotation string   `yaml:"digestAnnotation"`
			Targets          []struct {
				Provider   string `yaml:"provider"`
				APIVersion string `yaml:"apiVersion"`
				Kind       string `yaml:"kind"`
				Output     string `yaml:"output"`
			} `yaml:"targets"`
			Checks []string `yaml:"checks"`
		} `yaml:"generation"`
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

func TestEdgeRouteTableMetadata(t *testing.T) {
	table := loadEdgeRouteTable(t)
	if table.APIVersion != "ontoaxis.edge/v1" || table.Kind != "EdgeRouteTable" {
		t.Fatalf("apiVersion/kind = %q/%q", table.APIVersion, table.Kind)
	}
	if table.Metadata.Name != "oa-edge" || table.Metadata.Owner != "edge-gateway" || table.Metadata.Stage != "stage-2" {
		t.Fatalf("metadata = %+v, want oa-edge/edge-gateway/stage-2", table.Metadata)
	}
	if table.Metadata.ContractVersion != 1 {
		t.Fatalf("contractVersion = %d, want 1", table.Metadata.ContractVersion)
	}
	if !reflect.DeepEqual(table.Spec.Gateway.Providers, []string{"envoy-gateway", "traefik"}) {
		t.Fatalf("providers = %v, want [envoy-gateway traefik]", table.Spec.Gateway.Providers)
	}
	if table.Spec.Gateway.Listener.Name != "oa-edge" || table.Spec.Gateway.Listener.Protocol != "HTTP" || table.Spec.Gateway.Listener.Port != 8080 {
		t.Fatalf("listener = %+v, want oa-edge/HTTP/8080", table.Spec.Gateway.Listener)
	}
	if table.Spec.Gateway.Listener.TLS.Enabled {
		t.Fatal("listener.tls.enabled = true, want false for the Stage 2 local contract")
	}
}

func TestEdgeRouteTableCoversAllBusinessPrefixes(t *testing.T) {
	table := loadEdgeRouteTable(t)
	want := []struct {
		name   string
		prefix string
		module string
	}{
		{"tenancy", "/api/v1/tenancy", "tenancy"},
		{"ontology", "/api/v1/ontology", "ontology"},
		{"datasets", "/api/v1/datasets", "datasets"},
		{"connectors", "/api/v1/connectors", "connectors"},
		{"ingestion", "/api/v1/ingestion", "ingestion"},
		{"pipelines", "/api/v1/pipelines", "pipelines"},
		{"objects", "/api/v1/objects", "objects"},
		{"query", "/api/v1/query", "query"},
		{"actions", "/api/v1/actions", "actions"},
	}
	if len(table.Spec.Routes) != len(want) {
		t.Fatalf("route count = %d, want %d", len(table.Spec.Routes), len(want))
	}
	seenPrefixes := make(map[string]struct{}, len(table.Spec.Routes))
	seenNames := make(map[string]struct{}, len(table.Spec.Routes))
	for i, route := range table.Spec.Routes {
		if route.Name != want[i].name || route.Prefix != want[i].prefix || route.Module != want[i].module {
			t.Fatalf("route[%d] = %+v, want %+v", i, route, want[i])
		}
		if route.Auth != "jwt" {
			t.Fatalf("route %s auth = %q, want jwt", route.Name, route.Auth)
		}
		if route.Priority <= 0 {
			t.Fatalf("route %s priority = %d, want positive", route.Name, route.Priority)
		}
		if len(route.Methods) == 0 {
			t.Fatalf("route %s has no methods", route.Name)
		}
		for _, method := range route.Methods {
			if !validHTTPMethod(method) {
				t.Fatalf("route %s has invalid method %q", route.Name, method)
			}
		}
		if _, ok := seenPrefixes[route.Prefix]; ok {
			t.Fatalf("duplicate prefix %q", route.Prefix)
		}
		seenPrefixes[route.Prefix] = struct{}{}
		if _, ok := seenNames[route.Name]; ok {
			t.Fatalf("duplicate route name %q", route.Name)
		}
		seenNames[route.Name] = struct{}{}
	}
}

func TestEdgeRouteTablePublicAndInternalRoutes(t *testing.T) {
	table := loadEdgeRouteTable(t)
	wantPublic := []struct {
		name   string
		method string
		path   string
	}{
		{"healthz", "GET", "/healthz"},
		{"readyz", "GET", "/readyz"},
		{"well-known", "GET", "/.well-known/*"},
	}
	if len(table.Spec.Gateway.PublicRoutes) != len(wantPublic) {
		t.Fatalf("public route count = %d, want %d", len(table.Spec.Gateway.PublicRoutes), len(wantPublic))
	}
	for i, route := range table.Spec.Gateway.PublicRoutes {
		if route.Name != wantPublic[i].name || route.Method != wantPublic[i].method || route.Path != wantPublic[i].path {
			t.Fatalf("publicRoutes[%d] = %+v, want %+v", i, route, wantPublic[i])
		}
		if route.Auth != "none" {
			t.Fatalf("public route %s auth = %q, want none", route.Name, route.Auth)
		}
	}
	if len(table.Spec.Gateway.InternalRoutes) != 1 {
		t.Fatalf("internal route count = %d, want 1", len(table.Spec.Gateway.InternalRoutes))
	}
	metrics := table.Spec.Gateway.InternalRoutes[0]
	if metrics.Name != "metrics" || metrics.Method != "GET" || metrics.Path != "/metrics" || metrics.Exposure != "cluster-only" {
		t.Fatalf("metrics route = %+v, want GET /metrics cluster-only", metrics)
	}
	for _, route := range table.Spec.Gateway.PublicRoutes {
		if route.Path == metrics.Path {
			t.Fatalf("internal %s must not be public", metrics.Path)
		}
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

func TestEdgeRouteTableAuthAndUpstreamContract(t *testing.T) {
	table := loadEdgeRouteTable(t)
	if table.Spec.Upstream.Service != "ontoaxis-core" || table.Spec.Upstream.Port != 8080 || table.Spec.Upstream.Scheme != "http" {
		t.Fatalf("upstream = %+v, want ontoaxis-core:8080 http", table.Spec.Upstream)
	}
	auth := table.Spec.Auth
	if auth.Type != "jwt" || auth.Header != "Authorization" || auth.Scheme != "Bearer" {
		t.Fatalf("auth = %+v, want jwt/Authorization/Bearer", auth)
	}
	if auth.IssuerEnv != "ONTOAXIS_OIDC_ISSUER" || auth.AudienceEnv != "ONTOAXIS_OIDC_AUDIENCE" || auth.JWKSURLEnv != "ONTOAXIS_OIDC_JWKS_URL" {
		t.Fatalf("auth env references = %+v", auth)
	}
	if auth.ClockSkewSeconds != 30 {
		t.Fatalf("clockSkewSeconds = %d, want 30", auth.ClockSkewSeconds)
	}
	if !reflect.DeepEqual(auth.RequiredClaims, []string{"sub", "aud", "tenant_id"}) {
		t.Fatalf("requiredClaims = %v, want [sub aud tenant_id]", auth.RequiredClaims)
	}
}

func TestEdgeRouteTableRateLimitContract(t *testing.T) {
	table := loadEdgeRouteTable(t)
	r := table.Spec.RateLimit
	if r.Defaults.RequestsPerSecond != 50 || r.Defaults.Burst != 100 {
		t.Fatalf("rateLimit.defaults = %+v, want 50 rps / 100 burst", r.Defaults)
	}
	if r.ByTenant.RequestsPerMinute != 3000 {
		t.Fatalf("rateLimit.byTenant.requestsPerMinute = %d, want 3000", r.ByTenant.RequestsPerMinute)
	}
	if !r.ExemptPublicRoutes {
		t.Fatal("rateLimit.exemptPublicRoutes = false, want true")
	}
}

func TestEdgeRouteTableGenerationContract(t *testing.T) {
	table := loadEdgeRouteTable(t)
	gen := table.Spec.Generation
	if gen.Strategy != "single-source" {
		t.Fatalf("generation.strategy = %q, want single-source", gen.Strategy)
	}
	if !reflect.DeepEqual(gen.Ordering, []string{"publicRoutes", "routes-by-longest-prefix", "routes-by-name"}) {
		t.Fatalf("generation.ordering = %v", gen.Ordering)
	}
	if gen.DigestAnnotation != "ontoaxis.dev/route-table-sha256" {
		t.Fatalf("digestAnnotation = %q", gen.DigestAnnotation)
	}
	if len(gen.Targets) != 2 {
		t.Fatalf("generation targets = %d, want 2", len(gen.Targets))
	}
	wantProviders := map[string]string{
		"envoy-gateway": "deploy/edge/generated/envoy-httproute.yaml",
		"traefik":       "deploy/edge/generated/traefik-dynamic.yaml",
	}
	for _, target := range gen.Targets {
		wantOutput, ok := wantProviders[target.Provider]
		if !ok {
			t.Fatalf("unexpected generation provider %q", target.Provider)
		}
		if target.Output != wantOutput {
			t.Fatalf("%s output = %q, want %q", target.Provider, target.Output, wantOutput)
		}
		if !strings.HasPrefix(target.APIVersion, "gateway.networking.k8s.io/") && !strings.HasPrefix(target.APIVersion, "traefik.io/") {
			t.Fatalf("%s apiVersion = %q", target.Provider, target.APIVersion)
		}
		if target.Kind == "" {
			t.Fatalf("%s has empty kind", target.Provider)
		}
	}
	if len(gen.Checks) == 0 || !strings.Contains(strings.Join(gen.Checks, " "), "edge-generate-check") {
		t.Fatalf("generation checks = %v, want edge-generate-check", gen.Checks)
	}
}

func TestEdgeRouteTableSchemaIsMachineReadable(t *testing.T) {
	path := filepath.Join("..", "..", "..", "config", "edge", "routes.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("schema $schema = %v", schema["$schema"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || props["spec"] == nil {
		t.Fatal("schema must declare spec property")
	}
}

func validHTTPMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return true
	default:
		return false
	}
}
