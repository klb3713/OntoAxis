package authz

import (
	"fmt"
	"sort"
	"strings"

	"github.com/klb3713/ontoaxis/internal/platform/authn"
	fgaSdk "github.com/openfga/go-sdk"
)

const maxContextualTuples = 8

// BuildContextualTuples builds the per-request relationship facts. They come
// from the verified JWT (tenant membership) and tenant-scoped rows (resource
// ancestry); nothing is persisted and no claim is expanded beyond the current
// resource chain.
func BuildContextualTuples(tc authn.TenantContext, resource ResourceFact) ([]fgaSdk.TupleKey, error) {
	if tc.UserID.String() == "" || tc.TenantID.String() == "" {
		return nil, fmt.Errorf("trusted tenant context is incomplete")
	}
	if resource.TenantID != tc.TenantID.String() {
		return nil, fmt.Errorf("resource tenant does not match the trusted tenant")
	}
	user := "user:" + tc.UserID.String()
	tenant := "tenant:" + resource.TenantID
	tuples := []fgaSdk.TupleKey{
		{User: user, Relation: "member", Object: tenant},
	}
	ancestors := append([]Ancestor(nil), resource.Ancestors...)
	if len(ancestors) == 0 && resource.Type != "tenant" {
		if _, ok := parentRelations[resource.Type]["tenant"]; ok {
			ancestors = append(ancestors, Ancestor{Type: "tenant", ID: resource.TenantID})
		}
	}
	// Complete the ancestor chain to the tenant root when the caller supplied
	// only the immediate parent (for example action_execution -> action_type).
	if len(ancestors) > 0 {
		last := ancestors[len(ancestors)-1]
		if last.Type != "tenant" {
			if _, ok := parentRelations[last.Type]["tenant"]; ok {
				ancestors = append(ancestors, Ancestor{Type: "tenant", ID: resource.TenantID})
			}
		}
	}
	chain := append([]Ancestor{{Type: resource.Type, ID: resource.ID}}, ancestors...)
	for i := 0; i+1 < len(chain); i++ {
		child, parent := chain[i], chain[i+1]
		if child.Type == "tenant" {
			continue
		}
		relation, err := relationFor(child.Type, parent.Type)
		if err != nil {
			return nil, err
		}
		tuples = append(tuples, fgaSdk.TupleKey{
			User:     objectRef(parent.Type, parent.ID),
			Relation: relation,
			Object:   objectRef(child.Type, child.ID),
		})
	}
	if resource.Type == "saved_query" && resource.OwnerID != "" {
		tuples = append(tuples, fgaSdk.TupleKey{
			User:     "user:" + resource.OwnerID,
			Relation: "owner",
			Object:   objectRef("saved_query", resource.ID),
		})
	}
	tuples = dedupeTuples(tuples)
	if len(tuples) > maxContextualTuples {
		return nil, fmt.Errorf("contextual tuple limit exceeded (%d > %d)", len(tuples), maxContextualTuples)
	}
	return tuples, nil
}

func objectRef(typ, id string) string {
	return typ + ":" + id
}

var parentRelations = map[string]map[string]string{
	"organization":      {"tenant": "tenant"},
	"workspace":         {"organization": "organization"},
	"project":           {"workspace": "workspace"},
	"dataset":           {"tenant": "tenant", "project": "project"},
	"object_type":       {"tenant": "tenant", "project": "project"},
	"pipeline":          {"tenant": "tenant", "project": "project"},
	"action_type":       {"tenant": "tenant", "project": "project"},
	"object":            {"object_type": "object_type"},
	"connector":         {"tenant": "tenant", "workspace": "workspace"},
	"ingest_job":        {"connector": "connector"},
	"pipeline_run":      {"pipeline": "pipeline"},
	"pipeline_schedule": {"pipeline": "pipeline"},
	"action_execution":  {"action_type": "action_type"},
	"saved_query":       {"tenant": "tenant"},
	"audit_event":       {"tenant": "tenant"},
	"operation":         {"tenant": "tenant"},
}

func relationFor(childType, parentType string) (string, error) {
	relations, ok := parentRelations[childType]
	if !ok {
		return "", fmt.Errorf("unknown resource type %q in contextual tuple chain", childType)
	}
	relation, ok := relations[parentType]
	if !ok {
		return "", fmt.Errorf("resource %q cannot have parent %q", childType, parentType)
	}
	return relation, nil
}

func dedupeTuples(in []fgaSdk.TupleKey) []fgaSdk.TupleKey {
	seen := make(map[string]struct{}, len(in))
	out := make([]fgaSdk.TupleKey, 0, len(in))
	for _, tuple := range in {
		key := strings.Join([]string{tuple.User, tuple.Relation, tuple.Object}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tuple)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.Object+"\x00"+a.Relation+"\x00"+a.User < b.Object+"\x00"+b.Relation+"\x00"+b.User
	})
	return out
}
