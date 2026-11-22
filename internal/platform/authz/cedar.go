package authz

import (
	"fmt"
	"maps"
	"strings"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
)

var fgaTypeToCedar = map[string]string{
	"tenant":            "Tenant",
	"organization":      "Organization",
	"workspace":         "Workspace",
	"project":           "Project",
	"dataset":           "Dataset",
	"object_type":       "ObjectType",
	"object":            "Object",
	"connector":         "Connector",
	"ingest_job":        "IngestJob",
	"pipeline":          "Pipeline",
	"pipeline_run":      "PipelineRun",
	"pipeline_schedule": "PipelineSchedule",
	"action_type":       "ActionType",
	"action_execution":  "ActionExecution",
	"saved_query":       "SavedQuery",
	"audit_event":       "AuditEvent",
	"operation":         "Operation",
}

type cedarEngine struct {
	set            *cedar.PolicySet
	bundle         *Bundle
	actionEntities types.EntityMap
}

func newCedarEngine(bundle *Bundle, catalog *Catalog) (*cedarEngine, error) {
	if bundle == nil || bundle.PolicySet == nil {
		return nil, fmt.Errorf("Cedar policy bundle is empty")
	}
	em := make(types.EntityMap)
	for _, group := range catalog.Groups() {
		uid := actionUID(string(group))
		em[uid] = types.Entity{UID: uid}
	}
	for _, spec := range catalog.Actions() {
		uid := actionUID(string(spec.Action))
		em[uid] = types.Entity{UID: uid, Parents: types.NewEntityUIDSet(actionUID(string(spec.Group)))}
	}
	return &cedarEngine{set: bundle.PolicySet, bundle: bundle, actionEntities: em}, nil
}

func actionUID(name string) types.EntityUID {
	return types.NewEntityUID(types.EntityType("Action"), types.String(name))
}

func resourceUID(cedarType, id string) types.EntityUID {
	return types.NewEntityUID(types.EntityType(cedarType), types.String(id))
}

// Evaluate runs Cedar over trusted subject and resource attributes. It returns
// only stable policy IDs and a reason; policy text and attributes stay inside
// the decision point.
func (e *cedarEngine) Evaluate(tc authn.TenantContext, spec ActionSpec, resource ResourceFact) (bool, Reason, []string, error) {
	cedarType, ok := fgaTypeToCedar[resource.Type]
	if !ok {
		return false, ReasonAttributeMissing, nil, fmt.Errorf("unknown resource type %q", resource.Type)
	}
	userUID := types.NewEntityUID(types.EntityType("User"), types.String(tc.UserID.String()))
	user := types.Entity{
		UID: userUID,
		Attributes: types.NewRecord(types.RecordMap{
			"tenant_id": types.String(tc.TenantID.String()),
			"roles":     rolesSet(tc.Roles),
		}),
	}
	attrs := types.RecordMap{"tenant_id": types.String(resource.TenantID)}
	if resource.Status != "" {
		attrs["status"] = types.String(resource.Status)
	}
	resUID := resourceUID(cedarType, resource.ID)
	res := types.Entity{UID: resUID, Attributes: types.NewRecord(attrs)}

	entities := maps.Clone(e.actionEntities)
	entities[user.UID] = user
	entities[res.UID] = res
	req := types.Request{
		Principal: userUID,
		Action:    actionUID(string(spec.Action)),
		Resource:  resUID,
		Context:   types.NewRecord(nil),
	}
	allowed, diag := e.set.IsAuthorized(entities, req)
	matched := make([]string, 0, len(diag.Reasons))
	for _, reason := range diag.Reasons {
		matched = append(matched, string(reason.PolicyID))
	}
	if allowed {
		return true, ReasonAllow, matched, nil
	}
	if len(diag.Errors) > 0 {
		return false, ReasonAttributeMissing, matched, nil
	}
	return false, classifyCedarDeny(spec, matched), matched, nil
}

func rolesSet(roles []string) types.Set {
	values := make([]types.Value, 0, len(roles))
	for _, role := range roles {
		values = append(values, types.String(role))
	}
	return types.NewSet(values...)
}

func classifyCedarDeny(spec ActionSpec, matched []string) Reason {
	for _, id := range matched {
		switch {
		case strings.HasPrefix(id, "platform/resource-state.cedar#"):
			return ReasonResourceStateDenied
		case strings.HasPrefix(id, "platform/tenancy-scope.cedar#"):
			return ReasonAdminRequired
		case strings.HasPrefix(id, "platform/tenant-isolation.cedar#"):
			return ReasonNoRelationship
		}
	}
	if spec.Group == GroupAdmin {
		return ReasonAdminRequired
	}
	return ReasonRoleNotPermitted
}
