package search

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTenantHashIsStableLowerHex16(t *testing.T) {
	tenant := "0f5a3a0e-2f3c-4a1b-9d7e-6c8b5a4f3e21"
	sum := sha256.Sum256([]byte(tenant))
	want := hex.EncodeToString(sum[:])[:16]
	if got := TenantHash(tenant); got != want {
		t.Fatalf("TenantHash = %q, want %q", got, want)
	}
	upper := strings.ToUpper(tenant)
	if got, want := TenantHash(upper), TenantHash(tenant); got != want {
		t.Fatalf("TenantHash is not case-stable: %q != %q", got, want)
	}
	if strings.ContainsAny(TenantHash(tenant), "ABCDEF") {
		t.Fatalf("TenantHash must be lower-case hex")
	}
}

func TestIndexAndStagingNames(t *testing.T) {
	tenant := uuid.NewString()
	index := IndexName("v2", tenant)
	if !strings.HasPrefix(index, "objects_v2_") {
		t.Fatalf("IndexName = %q, want objects_v2_ prefix", index)
	}
	if len(index) != len("objects_v2_")+16 {
		t.Fatalf("IndexName = %q, want 16-char tenant hash", index)
	}
	if strings.Contains(index, tenant) {
		t.Fatalf("IndexName leaks the raw tenant UUID: %q", index)
	}
	if got, want := StagingIndexName(index, 1730000000), index+"_r1730000000"; got != want {
		t.Fatalf("StagingIndexName = %q, want %q", got, want)
	}
}

func TestValidateIndexVersion(t *testing.T) {
	for _, version := range []string{"v1", "v0", "v123"} {
		if err := ValidateIndexVersion(version); err != nil {
			t.Fatalf("ValidateIndexVersion(%q) = %v, want nil", version, err)
		}
	}
	for _, version := range []string{"", "1", "v", "v1x", "V1", "v-1", "v1.0"} {
		err := ValidateIndexVersion(version)
		if err == nil {
			t.Fatalf("ValidateIndexVersion(%q) = nil, want error", version)
		}
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("ValidateIndexVersion(%q) error %v is not ErrInvalid", version, err)
		}
	}
}
