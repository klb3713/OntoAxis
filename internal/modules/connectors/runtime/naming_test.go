package runtime_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
)

const (
	testTenantA = "11111111-1111-7111-8111-111111111111"
	testTenantB = "22222222-2222-7222-8222-222222222222"
	testConnA   = "019c0000-0000-7000-8000-000000000001"
	testConnB   = "019c0001-0000-7000-8000-000000000002"
)

var connectorNamePattern = regexp.MustCompile(`^[a-z0-9.-]+$`)

func TestNamesForDeterministic(t *testing.T) {
	t.Parallel()

	naming := runtime.NewNaming()
	names, err := naming.NamesFor(testTenantA, testConnA)
	if err != nil {
		t.Fatalf("NamesFor() error = %v", err)
	}

	if names.TenantHash != "fa1ffa66" {
		t.Errorf("TenantHash = %q, want %q", names.TenantHash, "fa1ffa66")
	}
	if names.ConnectorHash != "2ff3a64e" {
		t.Errorf("ConnectorHash = %q, want %q", names.ConnectorHash, "2ff3a64e")
	}
	if names.Source != "oa-fa1ffa66-2ff3a64e-source" {
		t.Errorf("Source = %q, want %q", names.Source, "oa-fa1ffa66-2ff3a64e-source")
	}
	if names.Sink != "oa-fa1ffa66-2ff3a64e-sink" {
		t.Errorf("Sink = %q, want %q", names.Sink, "oa-fa1ffa66-2ff3a64e-sink")
	}
	if names.TopicPrefix != "oa.fa1ffa66.2ff3a64e" {
		t.Errorf("TopicPrefix = %q, want %q", names.TopicPrefix, "oa.fa1ffa66.2ff3a64e")
	}

	// Same input MUST produce byte-identical names.
	again, err := naming.NamesFor(testTenantA, testConnA)
	if err != nil {
		t.Fatalf("NamesFor() second call error = %v", err)
	}
	if names != again {
		t.Errorf("NamesFor() is not deterministic: %v != %v", names, again)
	}

	for _, name := range []string{names.Source, names.Sink} {
		if !connectorNamePattern.MatchString(name) {
			t.Errorf("runtime connector name %q contains characters outside [a-z0-9.-]", name)
		}
	}
}

func TestNamesForIsolatesTenants(t *testing.T) {
	t.Parallel()

	naming := runtime.NewNaming()
	a, err := naming.NamesFor(testTenantA, testConnA)
	if err != nil {
		t.Fatalf("NamesFor(tenantA) error = %v", err)
	}
	b, err := naming.NamesFor(testTenantB, testConnA)
	if err != nil {
		t.Fatalf("NamesFor(tenantB) error = %v", err)
	}

	if a.TenantHash == b.TenantHash {
		t.Fatalf("different tenants produced the same hash %q", a.TenantHash)
	}
	if a.Source == b.Source || a.Sink == b.Sink || a.TopicPrefix == b.TopicPrefix {
		t.Fatalf("different tenants produced overlapping runtime names: %v vs %v", a, b)
	}
	if a.ConnectorHash != b.ConnectorHash {
		t.Errorf("connector hash must not depend on tenant: %q != %q", a.ConnectorHash, b.ConnectorHash)
	}
}

func TestNamesForIsolatesConnectors(t *testing.T) {
	t.Parallel()

	naming := runtime.NewNaming()
	a, err := naming.NamesFor(testTenantA, testConnA)
	if err != nil {
		t.Fatalf("NamesFor(connA) error = %v", err)
	}
	b, err := naming.NamesFor(testTenantA, testConnB)
	if err != nil {
		t.Fatalf("NamesFor(connB) error = %v", err)
	}

	if a.ConnectorHash == b.ConnectorHash {
		t.Fatalf("different connectors produced the same hash %q", a.ConnectorHash)
	}
	if a.Source == b.Source || a.Sink == b.Sink || a.TopicPrefix == b.TopicPrefix {
		t.Fatalf("different connectors of one tenant produced overlapping runtime names: %v vs %v", a, b)
	}
}

func TestNamesForCanonicalizesUUIDCase(t *testing.T) {
	t.Parallel()

	naming := runtime.NewNaming()
	lower, err := naming.NamesFor(testTenantA, testConnA)
	if err != nil {
		t.Fatalf("NamesFor(lower) error = %v", err)
	}
	upper, err := naming.NamesFor(strings.ToUpper(testTenantA), strings.ToUpper(testConnA))
	if err != nil {
		t.Fatalf("NamesFor(upper) error = %v", err)
	}
	if lower != upper {
		t.Errorf("UUID casing changed runtime names: %v != %v", lower, upper)
	}
}

func TestNamesForRejectsInvalidIdentifiers(t *testing.T) {
	t.Parallel()

	naming := runtime.NewNaming()
	tests := []struct {
		name        string
		tenantID    string
		connectorID string
		wantErr     error
	}{
		{name: "empty tenant", tenantID: "", connectorID: testConnA, wantErr: runtime.ErrInvalidTenant},
		{name: "malformed tenant", tenantID: "tenant-a", connectorID: testConnA, wantErr: runtime.ErrInvalidTenant},
		{name: "empty connector", tenantID: testTenantA, connectorID: "", wantErr: runtime.ErrInvalidConnector},
		{name: "malformed connector", tenantID: testTenantA, connectorID: "connector-1", wantErr: runtime.ErrInvalidConnector},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := naming.NamesFor(tt.tenantID, tt.connectorID); !errors.Is(err, tt.wantErr) {
				t.Fatalf("NamesFor() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNamesFromRuntimeRef(t *testing.T) {
	t.Parallel()

	naming := runtime.NewNaming()
	names, err := naming.NamesFor(testTenantA, testConnA)
	if err != nil {
		t.Fatalf("NamesFor() error = %v", err)
	}

	got, err := naming.NamesFromRuntimeRef(testTenantA, names.Sink)
	if err != nil {
		t.Fatalf("NamesFromRuntimeRef() error = %v", err)
	}
	if got != names {
		t.Errorf("NamesFromRuntimeRef() = %v, want %v", got, names)
	}

	upper, err := naming.NamesFromRuntimeRef(testTenantA, strings.ToUpper(names.Sink))
	if err != nil {
		t.Fatalf("NamesFromRuntimeRef(upper) error = %v", err)
	}
	if upper != names {
		t.Errorf("NamesFromRuntimeRef(upper) = %v, want %v", upper, names)
	}
}

func TestNamesFromRuntimeRefRejectsForeignOrMalformedRefs(t *testing.T) {
	t.Parallel()

	naming := runtime.NewNaming()
	names, err := naming.NamesFor(testTenantA, testConnA)
	if err != nil {
		t.Fatalf("NamesFor() error = %v", err)
	}

	tests := []struct {
		name       string
		tenantID   string
		runtimeRef string
	}{
		{name: "empty ref", tenantID: testTenantA, runtimeRef: ""},
		{name: "malformed ref", tenantID: testTenantA, runtimeRef: "not-a-runtime-ref"},
		{name: "source ref is not a handle", tenantID: testTenantA, runtimeRef: names.Source},
		{name: "foreign tenant", tenantID: testTenantB, runtimeRef: names.Sink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := naming.NamesFromRuntimeRef(tt.tenantID, tt.runtimeRef); !errors.Is(err, runtime.ErrInvalidRuntimeRef) {
				t.Fatalf("NamesFromRuntimeRef() error = %v, want %v", err, runtime.ErrInvalidRuntimeRef)
			}
		})
	}
}

func TestNamesTopicsAndDefaults(t *testing.T) {
	t.Parallel()

	naming := runtime.NewNaming()
	names, err := naming.NamesFor(testTenantA, testConnA)
	if err != nil {
		t.Fatalf("NamesFor() error = %v", err)
	}

	if got, want := names.Topic("public", "orders"), "oa.fa1ffa66.2ff3a64e.public.orders"; got != want {
		t.Errorf("Topic() = %q, want %q", got, want)
	}

	topics := names.Topics("public", []string{"customers", "orders"})
	want := []string{
		"oa.fa1ffa66.2ff3a64e.public.customers",
		"oa.fa1ffa66.2ff3a64e.public.orders",
	}
	if len(topics) != len(want) {
		t.Fatalf("Topics() = %v, want %v", topics, want)
	}
	for i := range want {
		if topics[i] != want[i] {
			t.Errorf("Topics()[%d] = %q, want %q", i, topics[i], want[i])
		}
	}

	if got, want := names.DefaultSlotName(), "oa_fa1ffa66_2ff3a64e_slot"; got != want {
		t.Errorf("DefaultSlotName() = %q, want %q", got, want)
	}
	if got, want := names.DefaultPublicationName(), "oa_fa1ffa66_2ff3a64e_pub"; got != want {
		t.Errorf("DefaultPublicationName() = %q, want %q", got, want)
	}
}

func TestNamesForHashesWholeConnectorID(t *testing.T) {
	t.Parallel()

	naming := runtime.NewNaming()
	// UUIDv7 shares its first 32 bits for connectors created within the same
	// short timestamp window; names must still differ.
	a, err := naming.NamesFor(testTenantA, "019c0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatalf("NamesFor(a) error = %v", err)
	}
	b, err := naming.NamesFor(testTenantA, "019c0000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatalf("NamesFor(b) error = %v", err)
	}
	if a.ConnectorHash == b.ConnectorHash || a.Source == b.Source || a.TopicPrefix == b.TopicPrefix {
		t.Fatalf("same-timestamp UUIDv7 connectors collided: %v vs %v", a, b)
	}
}
