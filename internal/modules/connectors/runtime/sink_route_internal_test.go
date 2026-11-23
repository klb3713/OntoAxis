package runtime

import (
	"regexp"
	"testing"
)

// TestSinkRouteRegexpMatchesSchemaAndTable pins the RegexRouter pattern used to
// strip the runtime topic prefix before Iceberg static table routing: the
// remaining "<schema>.<table>" suffix must be captured, and topics outside the
// configured prefix must not match.
func TestSinkRouteRegexpMatchesSchemaAndTable(t *testing.T) {
	names := Names{TopicPrefix: "ontoaxis.cdc.t1.c1"}
	pattern := regexp.MustCompile(sinkRouteRegexp(names))

	for _, tc := range []struct {
		topic  string
		schema string
		table  string
	}{
		{"ontoaxis.cdc.t1.c1.public.orders", "public", "orders"},
		{"ontoaxis.cdc.t1.c1.oa.datasets", "oa", "datasets"},
	} {
		match := pattern.FindStringSubmatch(tc.topic)
		if len(match) != 3 {
			t.Fatalf("topic %q match = %v, want schema/table capture", tc.topic, match)
		}
		if match[1] != tc.schema || match[2] != tc.table {
			t.Fatalf("topic %q captured (%q, %q), want (%q, %q)", tc.topic, match[1], match[2], tc.schema, tc.table)
		}
	}

	if pattern.MatchString("other.cdc.t1.c1.public.orders") {
		t.Fatal("regexp must be anchored to the configured topic prefix")
	}
}
