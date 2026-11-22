// Command edgegen renders deterministic Envoy Gateway and Traefik resources
// from config/edge/routes.yaml. It is the only writer of deploy/edge/generated.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type routeTable struct {
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
			} `yaml:"listener"`
			PublicRoutes []struct {
				Name   string `yaml:"name"`
				Method string `yaml:"method"`
				Path   string `yaml:"path"`
				Auth   string `yaml:"auth"`
			} `yaml:"publicRoutes"`
			ClientHeadersToStrip    []string `yaml:"clientHeadersToStrip"`
			InternalHeadersToInject []string `yaml:"internalHeadersToInject"`
		} `yaml:"gateway"`
		Upstream struct {
			Service string `yaml:"service"`
			Port    int    `yaml:"port"`
			Scheme  string `yaml:"scheme"`
		} `yaml:"upstream"`
		Auth struct {
			Type             string `yaml:"type"`
			IssuerEnv        string `yaml:"issuerEnv"`
			AudienceEnv      string `yaml:"audienceEnv"`
			JWKSURLEnv       string `yaml:"jwksURLEnv"`
			ClockSkewSeconds int    `yaml:"clockSkewSeconds"`
		} `yaml:"auth"`
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
		Routes []struct {
			Name     string   `yaml:"name"`
			Prefix   string   `yaml:"prefix"`
			Module   string   `yaml:"module"`
			Auth     string   `yaml:"auth"`
			Priority int      `yaml:"priority"`
			Methods  []string `yaml:"methods"`
		} `yaml:"routes"`
	} `yaml:"spec"`
}

func main() {
	var (
		configPath = flag.String("config", "config/edge/routes.yaml", "path to the edge route table")
		outDir     = flag.String("output", "deploy/edge/generated", "generated output directory")
		check      = flag.Bool("check", false, "verify generated files are up to date instead of writing")
	)
	flag.Parse()

	raw, err := os.ReadFile(*configPath)
	if err != nil {
		fatal(err)
	}
	var table routeTable
	if err := yaml.Unmarshal(raw, &table); err != nil {
		fatal(fmt.Errorf("parse %s: %w", *configPath, err))
	}
	if err := validate(table); err != nil {
		fatal(err)
	}
	digest := sha256.Sum256(raw)
	hexDigest := hex.EncodeToString(digest[:])

	envoy, err := renderEnvoy(table, hexDigest)
	if err != nil {
		fatal(err)
	}
	traefik, err := renderTraefik(table, hexDigest)
	if err != nil {
		fatal(err)
	}

	outputs := map[string][]byte{
		"envoy-httproute.yaml": envoy,
		"traefik-dynamic.yaml": traefik,
	}
	if *check {
		for name, want := range outputs {
			path := filepath.Join(*outDir, name)
			got, err := os.ReadFile(path)
			if err != nil {
				fatal(fmt.Errorf("read %s: %w", path, err))
			}
			if !bytes.Equal(got, want) {
				fatal(fmt.Errorf("%s is out of date; run make edge-generate", path))
			}
		}
		fmt.Println("edge route output is up to date")
		return
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}
	names := make([]string, 0, len(outputs))
	for name := range outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(*outDir, name)
		if err := os.WriteFile(path, outputs[name], 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("wrote %s\n", path)
	}
}

func validate(table routeTable) error {
	if table.APIVersion != "ontoaxis.edge/v1" || table.Kind != "EdgeRouteTable" {
		return fmt.Errorf("unsupported route table %s/%s", table.APIVersion, table.Kind)
	}
	if len(table.Spec.Gateway.Providers) == 0 {
		return errors.New("gateway.providers is empty")
	}
	if len(table.Spec.Gateway.PublicRoutes) == 0 {
		return errors.New("gateway.publicRoutes is empty")
	}
	if table.Spec.Upstream.Service == "" || table.Spec.Upstream.Port <= 0 || table.Spec.Upstream.Port > 65535 {
		return fmt.Errorf("invalid upstream %q:%d", table.Spec.Upstream.Service, table.Spec.Upstream.Port)
	}
	if table.Spec.Upstream.Scheme != "http" && table.Spec.Upstream.Scheme != "https" {
		return fmt.Errorf("unsupported upstream scheme %q", table.Spec.Upstream.Scheme)
	}
	if table.Spec.Auth.Type != "jwt" {
		return fmt.Errorf("unsupported gateway auth type %q", table.Spec.Auth.Type)
	}
	seenNames := map[string]struct{}{}
	seenPrefixes := map[string]struct{}{}
	validMethods := map[string]struct{}{"GET": {}, "HEAD": {}, "POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {}, "OPTIONS": {}}
	for _, r := range table.Spec.Routes {
		if r.Auth != "jwt" {
			return fmt.Errorf("route %s has unsupported auth %q", r.Name, r.Auth)
		}
		if _, ok := seenNames[r.Name]; ok {
			return fmt.Errorf("duplicate route name %q", r.Name)
		}
		if _, ok := seenPrefixes[r.Prefix]; ok {
			return fmt.Errorf("duplicate route prefix %q", r.Prefix)
		}
		seenNames[r.Name] = struct{}{}
		seenPrefixes[r.Prefix] = struct{}{}
		if len(r.Methods) == 0 {
			return fmt.Errorf("route %s has no methods", r.Name)
		}
		for _, m := range r.Methods {
			if _, ok := validMethods[m]; !ok {
				return fmt.Errorf("route %s has invalid method %q", r.Name, m)
			}
		}
	}
	return nil
}

func renderEnvoy(table routeTable, digest string) ([]byte, error) {
	annotations := map[string]any{"ontoaxis.dev/route-table-sha256": digest}
	gateway := map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "Gateway",
		"metadata": map[string]any{
			"name":        table.Spec.Gateway.Listener.Name,
			"annotations": annotations,
		},
		"spec": map[string]any{
			"gatewayClassName": "eg",
			"listeners": []any{map[string]any{
				"name":     "http",
				"protocol": "HTTP",
				"port":     table.Spec.Gateway.Listener.Port,
			}},
		},
	}

	publicRules := make([]any, 0, len(table.Spec.Gateway.PublicRoutes))
	for _, p := range table.Spec.Gateway.PublicRoutes {
		publicRules = append(publicRules, map[string]any{
			"matches": []any{map[string]any{"path": pathMatch(p.Path)}},
			"backendRefs": []any{map[string]any{
				"name": table.Spec.Upstream.Service,
				"port": table.Spec.Upstream.Port,
			}},
		})
	}
	publicRoute := map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]any{
			"name":        "oa-edge-public",
			"annotations": annotations,
		},
		"spec": map[string]any{
			"parentRefs": []any{map[string]any{"name": table.Spec.Gateway.Listener.Name}},
			"rules":      publicRules,
		},
	}

	protectedRules := make([]any, 0, len(table.Spec.Routes)+1)
	fallbackMatches := make([]any, 0, len(table.Spec.Routes))
	for _, r := range table.Spec.Routes {
		matches := make([]any, 0, len(r.Methods))
		for _, method := range r.Methods {
			matches = append(matches, map[string]any{
				"path":   map[string]any{"type": "PathPrefix", "value": r.Prefix},
				"method": method,
			})
		}
		protectedRules = append(protectedRules, map[string]any{
			"matches": matches,
			"filters": []any{map[string]any{
				"type": "RequestHeaderModifier",
				"requestHeaderModifier": map[string]any{
					"remove": upstreamHeadersToRemove(table),
				},
			}},
			"backendRefs": []any{map[string]any{
				"name": table.Spec.Upstream.Service,
				"port": table.Spec.Upstream.Port,
			}},
		})
		fallbackMatches = append(fallbackMatches, map[string]any{
			"path": map[string]any{"type": "PathPrefix", "value": r.Prefix},
		})
	}
	protectedRules = append(protectedRules, map[string]any{
		"matches": fallbackMatches,
		"filters": []any{map[string]any{
			"type": "ExtensionRef",
			"extensionRef": map[string]any{
				"group": "gateway.envoyproxy.io",
				"kind":  "HTTPRouteFilter",
				"name":  "oa-edge-method-not-allowed",
			},
		}},
	})
	protectedRoute := map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]any{
			"name":        "oa-edge-protected",
			"annotations": annotations,
		},
		"spec": map[string]any{
			"parentRefs": []any{map[string]any{"name": table.Spec.Gateway.Listener.Name}},
			"rules":      protectedRules,
		},
	}

	securityPolicy := map[string]any{
		"apiVersion": "gateway.envoyproxy.io/v1alpha1",
		"kind":       "SecurityPolicy",
		"metadata": map[string]any{
			"name":        "oa-edge-forward-auth",
			"annotations": annotations,
		},
		"spec": map[string]any{
			"targetRefs": []any{map[string]any{
				"group": "gateway.networking.k8s.io",
				"kind":  "HTTPRoute",
				"name":  "oa-edge-protected",
			}},
			"extAuth": map[string]any{
				"http": map[string]any{
					"backendRefs": []any{map[string]any{
						"name": table.Spec.Upstream.Service,
						"port": table.Spec.Upstream.Port,
					}},
					"path":             "/internal/edge-auth",
					"headersToBackend": table.Spec.Gateway.InternalHeadersToInject,
				},
			},
		},
	}

	ratePolicy := map[string]any{
		"apiVersion": "gateway.envoyproxy.io/v1alpha1",
		"kind":       "BackendTrafficPolicy",
		"metadata": map[string]any{
			"name":        "oa-edge-rate-limit",
			"annotations": annotations,
		},
		"spec": map[string]any{
			"targetRefs": []any{map[string]any{
				"group": "gateway.networking.k8s.io",
				"kind":  "HTTPRoute",
				"name":  "oa-edge-protected",
			}},
			"rateLimit": map[string]any{
				"type": "Local",
				"local": map[string]any{
					"rules": []any{map[string]any{
						"limit": map[string]any{
							"requests": table.Spec.RateLimit.Defaults.RequestsPerSecond,
							"unit":     "Second",
						},
					}},
				},
			},
		},
	}

	methodNotAllowed := map[string]any{
		"apiVersion": "gateway.envoyproxy.io/v1alpha1",
		"kind":       "HTTPRouteFilter",
		"metadata": map[string]any{
			"name":        "oa-edge-method-not-allowed",
			"annotations": annotations,
		},
		"spec": map[string]any{
			"directResponse": map[string]any{
				"statusCode":  405,
				"contentType": "application/json",
				"body": map[string]any{
					"type":   "Inline",
					"inline": `{"error":{"code":"method_not_allowed","message":"method not allowed"}}`,
				},
			},
		},
	}

	return encodeDocs(gateway, publicRoute, protectedRoute, securityPolicy, ratePolicy, methodNotAllowed)
}

func renderTraefik(table routeTable, digest string) ([]byte, error) {
	middlewares := map[string]any{
		"oa-edge-forward-auth": map[string]any{
			"forwardAuth": map[string]any{
				"address":             "http://" + table.Spec.Upstream.Service + ":" + fmt.Sprint(table.Spec.Upstream.Port) + "/internal/edge-auth",
				"authResponseHeaders": table.Spec.Gateway.InternalHeadersToInject,
			},
		},
		"oa-edge-rate-limit": map[string]any{
			"rateLimit": map[string]any{
				"average": table.Spec.RateLimit.Defaults.RequestsPerSecond,
				"burst":   table.Spec.RateLimit.Defaults.Burst,
			},
		},
		"oa-edge-strip-client-identity": map[string]any{
			"headers": map[string]any{
				"customRequestHeaders": upstreamHeaderMap(table),
			},
		},
	}
	routers := map[string]any{}
	services := map[string]any{
		"ontoaxis-core": map[string]any{
			"loadBalancer": map[string]any{
				"servers": []any{map[string]any{
					"url": table.Spec.Upstream.Scheme + "://" + table.Spec.Upstream.Service + ":" + fmt.Sprint(table.Spec.Upstream.Port),
				}},
			},
		},
	}
	for _, p := range table.Spec.Gateway.PublicRoutes {
		name := "oa-edge-public-" + p.Name
		routers[name] = map[string]any{
			"rule":     traefikPathRule(p.Path),
			"priority": 10000,
			"service":  "ontoaxis-core",
		}
	}
	protectedMiddleware := []any{"oa-edge-rate-limit", "oa-edge-forward-auth", "oa-edge-strip-client-identity"}
	for _, r := range table.Spec.Routes {
		for _, method := range r.Methods {
			name := "oa-edge-" + r.Name + "-" + strings.ToLower(method)
			routers[name] = map[string]any{
				"rule":        "PathPrefix(`" + r.Prefix + "`) && Method(`" + method + "`)",
				"priority":    1000 + r.Priority,
				"middlewares": protectedMiddleware,
				"service":     "ontoaxis-core",
			}
		}
		// Method fallback: core owns the 405 response for known business paths.
		routers["oa-edge-"+r.Name+"-fallback"] = map[string]any{
			"rule":        "PathPrefix(`" + r.Prefix + "`)",
			"priority":    100,
			"middlewares": protectedMiddleware,
			"service":     "ontoaxis-core",
		}
	}
	for _, p := range table.Spec.Gateway.PublicRoutes {
		// Non-GET public requests must still produce 405, not a provider 404.
		routers["oa-edge-public-"+p.Name+"-fallback"] = map[string]any{
			"rule":     traefikPathRule(p.Path),
			"priority": 9999,
			"service":  "ontoaxis-core",
		}
	}
	doc := map[string]any{
		"http": map[string]any{
			"routers":     routers,
			"services":    services,
			"middlewares": middlewares,
		},
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return append([]byte("# Generated by tools/edgegen from config/edge/routes.yaml. DO NOT EDIT.\n# route-table-sha256: "+digest+"\n"), out...), nil
}

func encodeDocs(docs ...any) ([]byte, error) {
	var b bytes.Buffer
	for _, doc := range docs {
		b.WriteString("---\n")
		out, err := yaml.Marshal(doc)
		if err != nil {
			return nil, err
		}
		b.Write(out)
	}
	return b.Bytes(), nil
}

func pathMatch(path string) map[string]any {
	if strings.HasSuffix(path, "*") {
		return map[string]any{"type": "PathPrefix", "value": strings.TrimSuffix(path, "*")}
	}
	return map[string]any{"type": "Exact", "value": path}
}

func traefikPathRule(path string) string {
	if strings.HasSuffix(path, "*") {
		return "PathPrefix(`" + strings.TrimSuffix(path, "*") + "`)"
	}
	return "Path(`" + path + "`)"
}

func upstreamHeadersToRemove(_ routeTable) []string {
	// Envoy extAuth must see client identity headers so it can reject spoofing
	// with 403. Authorization is removed after extAuth so the trusted core does
	// not reject the request for carrying an end-user token.
	return []string{"Authorization"}
}

func upstreamHeaderMap(table routeTable) map[string]any {
	out := emptyHeaderMap(table.Spec.Gateway.ClientHeadersToStrip)
	out["Authorization"] = ""
	return out
}

func emptyHeaderMap(headers []string) map[string]any {
	out := make(map[string]any, len(headers))
	for _, h := range headers {
		out[h] = ""
	}
	return out
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "edgegen:", err)
	os.Exit(1)
}
