package authz

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/klb3713/ontoaxis/policies"
)

func TestLoadBundleEmbedded(t *testing.T) {
	bundle, err := LoadBundle(policies.FS)
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}
	if bundle.ModelTypeCount != 20 {
		t.Fatalf("expected 20 model types, got %d", bundle.ModelTypeCount)
	}
	if !strings.HasPrefix(bundle.Digest, "sha256:") || !strings.HasPrefix(bundle.ModelDigest, "sha256:") {
		t.Fatalf("digest format: bundle=%q model=%q", bundle.Digest, bundle.ModelDigest)
	}
	if len(bundle.PolicyIDs) == 0 {
		t.Fatal("expected policy ids")
	}
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatalf("default catalog: %v", err)
	}
	if err := ValidateCatalogAgainstSchema(catalog, bundle); err != nil {
		t.Fatalf("catalog/schema mismatch: %v", err)
	}
}

func TestBundleDigestStable(t *testing.T) {
	first, err := LoadBundle(policies.FS)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadBundle(policies.FS)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || first.ModelDigest != second.ModelDigest {
		t.Fatalf("digest is not stable: %q/%q vs %q/%q", first.Digest, first.ModelDigest, second.Digest, second.ModelDigest)
	}
}

func TestBundlePolicyIDsArePathQualified(t *testing.T) {
	bundle, err := LoadBundle(policies.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range bundle.PolicyIDs {
		if !strings.HasPrefix(id, "platform/") || !strings.Contains(id, "#") {
			t.Fatalf("unstable policy id %q", id)
		}
	}
}

func TestLoadBundleRejectsMissingPolicyFile(t *testing.T) {
	mfs := copyBundleFS(t)
	delete(mfs, "platform/role-matrix.cedar")
	bundle, err := LoadBundle(mfs)
	if err != nil {
		t.Fatalf("bundle without role-matrix should still compile: %v", err)
	}
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCatalogAgainstSchema(catalog, bundle); err == nil {
		t.Fatal("expected failure without role-matrix policy")
	}
}

func TestLoadBundleRejectsTamperedDigest(t *testing.T) {
	base, err := LoadBundle(policies.FS)
	if err != nil {
		t.Fatal(err)
	}
	mfs := copyBundleFS(t)
	mfs["platform/role-matrix.cedar"] = &fstest.MapFile{Data: append(append([]byte{}, base.PolicyText["platform/role-matrix.cedar"]...), []byte("\n// tampered\n")...)}
	tampered, err := LoadBundle(mfs)
	if err != nil {
		t.Fatal(err)
	}
	if tampered.Digest == base.Digest {
		t.Fatal("digest did not change after policy tamper")
	}
}

func TestValidateCatalogAgainstSchemaDetectsUndeclaredGroup(t *testing.T) {
	mfs := copyBundleFS(t)
	delete(mfs, "platform/role-matrix.cedar")
	tampered, err := LoadBundle(mfs)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCatalogAgainstSchema(catalog, tampered); err == nil {
		t.Fatal("expected missing action-group coverage to be rejected")
	}
}

func copyBundleFS(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	paths := []string{ModelPath, SchemaPath}
	entries, err := fs.Glob(policies.FS, "platform/*.cedar")
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, entries...)
	for _, name := range paths {
		data, err := fs.ReadFile(policies.FS, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = &fstest.MapFile{Data: data}
	}
	return out
}
