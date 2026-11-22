package authz

import (
	"fmt"
	"sort"
)

// Action is a stable leaf action name declared by the platform catalog.
type Action string

// Group is one of the six Cedar action groups.
type Group string

const (
	GroupRead   Group = "read"
	GroupWrite  Group = "write"
	GroupDelete Group = "delete"
	GroupInvoke Group = "invoke"
	GroupAdmin  Group = "admin"
	GroupAudit  Group = "audit"
)

// ActionSpec is a leaf action and the OpenFGA relation it needs.
type ActionSpec struct {
	Action       Action
	Group        Group
	ResourceType string
	Relation     string
}

// Catalog is the closed set of leaf actions. Unknown actions are rejected;
// there is no implicit mapping and no fail-open fallback.
type Catalog struct {
	byAction map[Action]ActionSpec
	byGroup  map[Group][]Action
}

var validGroups = map[Group]struct{}{
	GroupRead: {}, GroupWrite: {}, GroupDelete: {}, GroupInvoke: {}, GroupAdmin: {}, GroupAudit: {},
}

var groupRelation = map[Group]string{
	GroupRead:   "can_read",
	GroupAudit:  "can_read",
	GroupWrite:  "can_write",
	GroupDelete: "can_write",
	GroupAdmin:  "can_write",
	GroupInvoke: "can_invoke",
}

var validResourceTypes = map[string]struct{}{
	"tenant": {}, "organization": {}, "workspace": {}, "project": {},
	"dataset": {}, "object_type": {}, "object": {}, "connector": {},
	"ingest_job": {}, "pipeline": {}, "pipeline_run": {}, "pipeline_schedule": {},
	"action_type": {}, "action_execution": {}, "saved_query": {},
	"audit_event": {}, "operation": {},
}

var defaultActionSpecs = []ActionSpec{
	{Action: "action_execution:cancel", Group: GroupInvoke, ResourceType: "action_execution", Relation: "can_invoke"},
	{Action: "action_execution:read", Group: GroupRead, ResourceType: "action_execution", Relation: "can_read"},
	{Action: "action_execution:approve", Group: GroupAdmin, ResourceType: "action_execution", Relation: "can_write"},
	{Action: "action_execution:retry", Group: GroupInvoke, ResourceType: "action_execution", Relation: "can_invoke"},
	{Action: "action_type:create", Group: GroupWrite, ResourceType: "tenant", Relation: "can_write"},
	{Action: "action_type:delete", Group: GroupDelete, ResourceType: "action_type", Relation: "can_write"},
	{Action: "action_type:preview", Group: GroupRead, ResourceType: "action_type", Relation: "can_read"},
	{Action: "action_type:read", Group: GroupRead, ResourceType: "action_type", Relation: "can_read"},
	{Action: "action_type:run", Group: GroupInvoke, ResourceType: "action_type", Relation: "can_invoke"},
	{Action: "action_type:update", Group: GroupWrite, ResourceType: "action_type", Relation: "can_write"},
	{Action: "action_type:validate", Group: GroupRead, ResourceType: "action_type", Relation: "can_read"},
	{Action: "audit:read", Group: GroupAudit, ResourceType: "tenant", Relation: "can_read"},
	{Action: "authorization:check", Group: GroupAdmin, ResourceType: "tenant", Relation: "can_write"},
	{Action: "authorization:explain", Group: GroupAdmin, ResourceType: "tenant", Relation: "can_write"},
	{Action: "authorization:list", Group: GroupAdmin, ResourceType: "tenant", Relation: "can_write"},
	{Action: "connector:create", Group: GroupWrite, ResourceType: "tenant", Relation: "can_write"},
	{Action: "connector:delete", Group: GroupDelete, ResourceType: "connector", Relation: "can_write"},
	{Action: "connector:infer_schema", Group: GroupInvoke, ResourceType: "connector", Relation: "can_invoke"},
	{Action: "connector:read", Group: GroupRead, ResourceType: "connector", Relation: "can_read"},
	{Action: "connector:sync", Group: GroupInvoke, ResourceType: "connector", Relation: "can_invoke"},
	{Action: "connector:test", Group: GroupInvoke, ResourceType: "connector", Relation: "can_invoke"},
	{Action: "connector:update", Group: GroupWrite, ResourceType: "connector", Relation: "can_write"},
	{Action: "dataset:create", Group: GroupWrite, ResourceType: "tenant", Relation: "can_write"},
	{Action: "dataset:delete", Group: GroupDelete, ResourceType: "dataset", Relation: "can_write"},
	{Action: "dataset:preview", Group: GroupRead, ResourceType: "dataset", Relation: "can_read"},
	{Action: "dataset:read", Group: GroupRead, ResourceType: "dataset", Relation: "can_read"},
	{Action: "dataset:update", Group: GroupWrite, ResourceType: "dataset", Relation: "can_write"},
	{Action: "dataset:versions", Group: GroupRead, ResourceType: "dataset", Relation: "can_read"},
	{Action: "identity:list", Group: GroupAudit, ResourceType: "tenant", Relation: "can_read"},
	{Action: "ingestion:cancel", Group: GroupInvoke, ResourceType: "ingest_job", Relation: "can_invoke"},
	{Action: "ingestion:create", Group: GroupInvoke, ResourceType: "connector", Relation: "can_invoke"},
	{Action: "ingestion:read", Group: GroupRead, ResourceType: "ingest_job", Relation: "can_read"},
	{Action: "ingestion:watch", Group: GroupRead, ResourceType: "ingest_job", Relation: "can_read"},
	{Action: "object:batch_upsert", Group: GroupWrite, ResourceType: "object_type", Relation: "can_write"},
	{Action: "object:create", Group: GroupWrite, ResourceType: "object_type", Relation: "can_write"},
	{Action: "object:delete", Group: GroupDelete, ResourceType: "object", Relation: "can_write"},
	{Action: "object:read", Group: GroupRead, ResourceType: "object", Relation: "can_read"},
	{Action: "object:update", Group: GroupWrite, ResourceType: "object", Relation: "can_write"},
	{Action: "object_link:create", Group: GroupWrite, ResourceType: "object", Relation: "can_write"},
	{Action: "object_link:delete", Group: GroupDelete, ResourceType: "object", Relation: "can_write"},
	{Action: "object_link:read", Group: GroupRead, ResourceType: "object", Relation: "can_read"},
	{Action: "ontology:binding:materialize", Group: GroupInvoke, ResourceType: "object_type", Relation: "can_invoke"},
	{Action: "ontology:binding:write", Group: GroupWrite, ResourceType: "object_type", Relation: "can_write"},
	{Action: "ontology:link_type:create", Group: GroupWrite, ResourceType: "object_type", Relation: "can_write"},
	{Action: "ontology:link_type:delete", Group: GroupDelete, ResourceType: "object_type", Relation: "can_write"},
	{Action: "ontology:link_type:read", Group: GroupRead, ResourceType: "object_type", Relation: "can_read"},
	{Action: "ontology:link_type:update", Group: GroupWrite, ResourceType: "object_type", Relation: "can_write"},
	{Action: "ontology:object_type:create", Group: GroupWrite, ResourceType: "tenant", Relation: "can_write"},
	{Action: "ontology:object_type:delete", Group: GroupDelete, ResourceType: "object_type", Relation: "can_write"},
	{Action: "ontology:object_type:read", Group: GroupRead, ResourceType: "object_type", Relation: "can_read"},
	{Action: "ontology:object_type:update", Group: GroupWrite, ResourceType: "object_type", Relation: "can_write"},
	{Action: "ontology:property:create", Group: GroupWrite, ResourceType: "object_type", Relation: "can_write"},
	{Action: "ontology:property:delete", Group: GroupDelete, ResourceType: "object_type", Relation: "can_write"},
	{Action: "ontology:property:read", Group: GroupRead, ResourceType: "object_type", Relation: "can_read"},
	{Action: "ontology:property:update", Group: GroupWrite, ResourceType: "object_type", Relation: "can_write"},
	{Action: "operation:read", Group: GroupRead, ResourceType: "operation", Relation: "can_read"},
	{Action: "pipeline:create", Group: GroupWrite, ResourceType: "tenant", Relation: "can_write"},
	{Action: "pipeline:delete", Group: GroupDelete, ResourceType: "pipeline", Relation: "can_write"},
	{Action: "pipeline:invoke", Group: GroupInvoke, ResourceType: "pipeline", Relation: "can_invoke"},
	{Action: "pipeline:publish", Group: GroupAdmin, ResourceType: "pipeline", Relation: "can_write"},
	{Action: "pipeline:read", Group: GroupRead, ResourceType: "pipeline", Relation: "can_read"},
	{Action: "pipeline:update", Group: GroupWrite, ResourceType: "pipeline", Relation: "can_write"},
	{Action: "pipeline_run:read", Group: GroupRead, ResourceType: "pipeline_run", Relation: "can_read"},
	{Action: "pipeline_schedule:delete", Group: GroupDelete, ResourceType: "pipeline_schedule", Relation: "can_write"},
	{Action: "pipeline_schedule:read", Group: GroupRead, ResourceType: "pipeline_schedule", Relation: "can_read"},
	{Action: "pipeline_schedule:write", Group: GroupWrite, ResourceType: "pipeline_schedule", Relation: "can_write"},
	{Action: "query:execute", Group: GroupRead, ResourceType: "tenant", Relation: "can_read"},
	{Action: "query:explain", Group: GroupRead, ResourceType: "tenant", Relation: "can_read"},
	{Action: "saved_query:create", Group: GroupWrite, ResourceType: "tenant", Relation: "can_write"},
	{Action: "saved_query:delete", Group: GroupDelete, ResourceType: "saved_query", Relation: "can_write"},
	{Action: "saved_query:read", Group: GroupRead, ResourceType: "saved_query", Relation: "can_read"},
	{Action: "tenancy:membership:add", Group: GroupAdmin, ResourceType: "organization", Relation: "can_write"},
	{Action: "tenancy:membership:read", Group: GroupRead, ResourceType: "organization", Relation: "can_read"},
	{Action: "tenancy:membership:remove", Group: GroupAdmin, ResourceType: "organization", Relation: "can_write"},
	{Action: "tenancy:organization:create", Group: GroupAdmin, ResourceType: "tenant", Relation: "can_write"},
	{Action: "tenancy:organization:delete", Group: GroupAdmin, ResourceType: "organization", Relation: "can_write"},
	{Action: "tenancy:organization:read", Group: GroupRead, ResourceType: "tenant", Relation: "can_read"},
	{Action: "tenancy:organization:update", Group: GroupAdmin, ResourceType: "organization", Relation: "can_write"},
	{Action: "tenancy:project:create", Group: GroupAdmin, ResourceType: "workspace", Relation: "can_write"},
	{Action: "tenancy:project:delete", Group: GroupAdmin, ResourceType: "project", Relation: "can_write"},
	{Action: "tenancy:project:read", Group: GroupRead, ResourceType: "workspace", Relation: "can_read"},
	{Action: "tenancy:project:update", Group: GroupAdmin, ResourceType: "project", Relation: "can_write"},
	{Action: "tenancy:project_resource:add", Group: GroupAdmin, ResourceType: "project", Relation: "can_write"},
	{Action: "tenancy:project_resource:read", Group: GroupRead, ResourceType: "project", Relation: "can_read"},
	{Action: "tenancy:project_resource:remove", Group: GroupAdmin, ResourceType: "project", Relation: "can_write"},
	{Action: "tenancy:workspace:create", Group: GroupAdmin, ResourceType: "organization", Relation: "can_write"},
	{Action: "tenancy:workspace:delete", Group: GroupAdmin, ResourceType: "workspace", Relation: "can_write"},
	{Action: "tenancy:workspace:read", Group: GroupRead, ResourceType: "organization", Relation: "can_read"},
	{Action: "tenancy:workspace:update", Group: GroupAdmin, ResourceType: "workspace", Relation: "can_write"},
}

// NewCatalog validates and freezes a catalog.
func NewCatalog(specs []ActionSpec) (*Catalog, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("authz catalog is empty")
	}
	c := &Catalog{byAction: make(map[Action]ActionSpec, len(specs)), byGroup: make(map[Group][]Action)}
	for _, spec := range specs {
		if spec.Action == "" {
			return nil, fmt.Errorf("authz catalog contains an empty action")
		}
		if _, ok := c.byAction[spec.Action]; ok {
			return nil, fmt.Errorf("authz catalog contains duplicate action %q", spec.Action)
		}
		if _, ok := validGroups[spec.Group]; !ok {
			return nil, fmt.Errorf("authz action %q has unknown group %q", spec.Action, spec.Group)
		}
		if _, ok := validResourceTypes[spec.ResourceType]; !ok {
			return nil, fmt.Errorf("authz action %q has unknown resource type %q", spec.Action, spec.ResourceType)
		}
		if spec.Relation != groupRelation[spec.Group] {
			return nil, fmt.Errorf("authz action %q relation %q does not match group %q", spec.Action, spec.Relation, spec.Group)
		}
		c.byAction[spec.Action] = spec
		c.byGroup[spec.Group] = append(c.byGroup[spec.Group], spec.Action)
	}
	for g := range c.byGroup {
		sort.Slice(c.byGroup[g], func(i, j int) bool { return c.byGroup[g][i] < c.byGroup[g][j] })
	}
	return c, nil
}

// DefaultCatalog returns the embedded closed action catalog.
func DefaultCatalog() (*Catalog, error) { return NewCatalog(defaultActionSpecs) }

// Lookup returns the spec for action. The second result is false for unknown
// actions.
func (c *Catalog) Lookup(action Action) (ActionSpec, bool) {
	if c == nil {
		return ActionSpec{}, false
	}
	spec, ok := c.byAction[action]
	return spec, ok
}

// Actions returns all leaf actions sorted by name.
func (c *Catalog) Actions() []ActionSpec {
	if c == nil {
		return nil
	}
	specs := make([]ActionSpec, 0, len(c.byAction))
	for _, spec := range c.byAction {
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Action < specs[j].Action })
	return specs
}

// GroupActions returns the leaf actions in group, sorted by name.
func (c *Catalog) GroupActions(group Group) []Action {
	if c == nil {
		return nil
	}
	return append([]Action(nil), c.byGroup[group]...)
}

// Groups returns all six groups.
func (c *Catalog) Groups() []Group {
	return []Group{GroupRead, GroupWrite, GroupDelete, GroupInvoke, GroupAdmin, GroupAudit}
}
