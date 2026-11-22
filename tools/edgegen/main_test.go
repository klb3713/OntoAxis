package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func loadTestTable(t *testing.T) (routeTable, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "edge", "routes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var table routeTable
	if err := yaml.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	return table, raw
}

func TestRenderIsDeterministic(t *testing.T) {
	table, raw := loadTestTable(t)
	digest := sha256.Sum256(raw)
	hexDigest := hex.EncodeToString(digest[:])

	first, err := renderEnvoy(table, hexDigest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderEnvoy(table, hexDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("envoy render is not deterministic")
	}
	traefik, err := renderTraefik(table, hexDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(traefik), "oa-edge-forward-auth") || !strings.Contains(string(traefik), "oa-edge-rate-limit") {
		t.Fatalf("traefik output missing middleware:\n%s", traefik)
	}
	if !strings.Contains(string(first), "oa-edge-protected") || !strings.Contains(string(first), "oa-edge-method-not-allowed") {
		t.Fatalf("envoy output missing protected resources:\n%s", first)
	}
}

func TestValidateRejectsDuplicatePrefix(t *testing.T) {
	table, _ := loadTestTable(t)
	table.Spec.Routes = append(table.Spec.Routes, table.Spec.Routes[0])
	if err := validate(table); err == nil {
		t.Fatal("expected duplicate prefix validation error")
	}
}

func TestValidateRejectsUnknownMethod(t *testing.T) {
	table, _ := loadTestTable(t)
	table.Spec.Routes[0].Methods = []string{"BREW"}
	if err := validate(table); err == nil {
		t.Fatal("expected unknown method validation error")
	}
}

func TestEnvoyProtectedHTTPRouteStaysWithinGatewayAPILimit(t *testing.T) {
	table, raw := loadTestTable(t)
	digest := sha256.Sum256(raw)
	out, err := renderEnvoy(table, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(out))
	found := false
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			break
		}
		if doc["kind"] != "HTTPRoute" || doc["metadata"].(map[string]any)["name"] != "oa-edge-protected" {
			continue
		}
		rules := doc["spec"].(map[string]any)["rules"].([]any)
		if len(rules) > 16 {
			t.Fatalf("protected HTTPRoute has %d rules, Gateway API max is 16", len(rules))
		}
		found = true
	}
	if !found {
		t.Fatal("protected HTTPRoute not found")
	}
}

func TestEnvoyRateLimitUsesLocalType(t *testing.T) {
	table, raw := loadTestTable(t)
	digest := sha256.Sum256(raw)
	out, err := renderEnvoy(table, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "type: Local") {
		t.Fatalf("local rate limit type missing:\n%s", out)
	}
}
