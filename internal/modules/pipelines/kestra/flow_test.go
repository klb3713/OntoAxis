package kestra_test

import (
	"bytes"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/domain"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
)

const (
	flowTenantUUID   = "11111111-1111-4111-8111-111111111111"
	flowPipelineUUID = "22222222-2222-4222-8222-222222222222"
	flowTenantHex    = "11111111111141118111111111111111"
	flowPipelineHex  = "22222222222242228222222222222222"
	flowVersion      = 3
)

func dbtTask(id string, dependsOn ...string) domain.Task {
	return domain.Task{
		ID:        id,
		Type:      domain.TaskTypeDBT,
		DependsOn: dependsOn,
		Model:     "model_" + id,
		SQL:       "select 1 as id",
	}
}

func buildFlowIR(t *testing.T, version int, dag domain.DAG, environment string) kestra.FlowIR {
	t.Helper()
	ir, err := kestra.BuildFlowIR(flowTenantUUID, flowPipelineUUID, version, dag, environment)
	if err != nil {
		t.Fatalf("BuildFlowIR: %v", err)
	}
	return ir
}

func renderFlow(t *testing.T, ir kestra.FlowIR) []byte {
	t.Helper()
	content, err := kestra.RenderFlowYAML(ir)
	if err != nil {
		t.Fatalf("RenderFlowYAML: %v", err)
	}
	return content
}

// renderedFlow mirrors the rendered YAML for semantic assertions.
type renderedFlow struct {
	ID        string            `yaml:"id"`
	Namespace string            `yaml:"namespace"`
	Revision  int               `yaml:"revision"`
	Labels    map[string]string `yaml:"labels"`
	Tasks     []renderedTask    `yaml:"tasks"`
}

type renderedTask struct {
	ID        string         `yaml:"id"`
	Type      string         `yaml:"type"`
	Commands  []string       `yaml:"commands"`
	DependsOn []string       `yaml:"dependsOn"`
	Message   string         `yaml:"message"`
	Tasks     []renderedTask `yaml:"tasks"`
}

func parseRenderedFlow(t *testing.T, content []byte) renderedFlow {
	t.Helper()
	var flow renderedFlow
	if err := yaml.Unmarshal(content, &flow); err != nil {
		t.Fatalf("unmarshal rendered flow: %v\n%s", err, content)
	}
	return flow
}

func TestBuildFlowIRIdentity(t *testing.T) {
	ir := buildFlowIR(t, flowVersion, domain.DAG{DAGVersion: domain.DAGVersionV1, Tasks: []domain.Task{
		dbtTask("clean_orders"),
	}}, "")

	if want := "oa_" + flowTenantHex + "." + flowPipelineHex; ir.Namespace != want {
		t.Fatalf("Namespace = %q, want %q", ir.Namespace, want)
	}
	if ir.FlowID != flowPipelineHex {
		t.Fatalf("FlowID = %q, want %q", ir.FlowID, flowPipelineHex)
	}
	if ir.Revision != flowVersion {
		t.Fatalf("Revision = %d, want %d", ir.Revision, flowVersion)
	}
	if ir.NoOp {
		t.Fatal("NoOp = true, want false for a DAG with tasks")
	}

	wantLabels := map[string]string{
		"ontoaxis.tenant_id":        flowTenantUUID,
		"ontoaxis.pipeline_id":      flowPipelineUUID,
		"ontoaxis.pipeline_version": "3",
		"ontoaxis.dag_version":      "1",
		"ontoaxis.managed_by":       "ontoaxis-core",
		"ontoaxis.environment":      "local",
	}
	if !reflect.DeepEqual(ir.Labels, wantLabels) {
		t.Fatalf("Labels = %#v, want %#v", ir.Labels, wantLabels)
	}
	if len(ir.Labels) != 6 {
		t.Fatalf("len(Labels) = %d, want 6", len(ir.Labels))
	}
}

func TestBuildFlowIRCanonicalUUIDIdentity(t *testing.T) {
	const (
		upperTenant      = "11111111-1111-4111-8111-111111111111"
		upperPipeline    = "AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE"
		upperPipelineHex = "aaaaaaaabbbb4ccc8dddeeeeeeeeeeee"
	)
	ir, err := kestra.BuildFlowIR("  "+upperTenant+"  ", upperPipeline, 7, domain.DAG{
		DAGVersion: domain.DAGVersionV1,
		Tasks:      []domain.Task{dbtTask("alpha")},
	}, "  staging  ")
	if err != nil {
		t.Fatalf("BuildFlowIR: %v", err)
	}
	wantNamespace := "oa_" + flowTenantHex + "." + upperPipelineHex
	if ir.Namespace != wantNamespace {
		t.Fatalf("Namespace = %q, want %q", ir.Namespace, wantNamespace)
	}
	if ir.FlowID != upperPipelineHex {
		t.Fatalf("FlowID = %q, want %q", ir.FlowID, upperPipelineHex)
	}
	if ir.Labels["ontoaxis.tenant_id"] != flowTenantUUID {
		t.Fatalf("tenant label = %q, want canonical lower-case UUID", ir.Labels["ontoaxis.tenant_id"])
	}
	if ir.Labels["ontoaxis.pipeline_id"] != "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" {
		t.Fatalf("pipeline label = %q, want canonical lower-case UUID", ir.Labels["ontoaxis.pipeline_id"])
	}
	if ir.Labels["ontoaxis.environment"] != "staging" {
		t.Fatalf("environment label = %q, want trimmed staging", ir.Labels["ontoaxis.environment"])
	}
	if ir.Revision != 7 || ir.Labels["ontoaxis.pipeline_version"] != "7" {
		t.Fatalf("version identity = revision %d / label %q, want 7", ir.Revision, ir.Labels["ontoaxis.pipeline_version"])
	}
}

func TestBuildFlowIRRejectsInvalidIdentity(t *testing.T) {
	tests := []struct {
		name       string
		tenantID   string
		pipelineID string
	}{
		{name: "empty tenant", tenantID: "", pipelineID: flowPipelineUUID},
		{name: "whitespace tenant", tenantID: "   ", pipelineID: flowPipelineUUID},
		{name: "non-uuid tenant", tenantID: "tenant-1", pipelineID: flowPipelineUUID},
		{name: "numeric tenant", tenantID: "1234", pipelineID: flowPipelineUUID},
		{name: "empty pipeline", tenantID: flowTenantUUID, pipelineID: ""},
		{name: "non-uuid pipeline", tenantID: flowTenantUUID, pipelineID: "../etc/passwd"},
		{name: "uuid with bad length", tenantID: flowTenantUUID, pipelineID: "22222222-2222-4222-8222-22222222222"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ir, err := kestra.BuildFlowIR(test.tenantID, test.pipelineID, 1, domain.DAG{
				DAGVersion: domain.DAGVersionV1,
				Tasks:      []domain.Task{dbtTask("alpha")},
			}, "local")
			if err == nil {
				t.Fatalf("BuildFlowIR = %+v, want error", ir)
			}
			if !errors.Is(err, domain.ErrInvalidArgument) {
				t.Fatalf("error = %v, want errors.Is ErrInvalidArgument", err)
			}
			if !reflect.DeepEqual(ir, kestra.FlowIR{}) {
				t.Fatalf("FlowIR = %+v, want zero value on error", ir)
			}
		})
	}
}

func TestBuildFlowIRNoTasks(t *testing.T) {
	dag, err := domain.ParseDAG([]byte(`{"dag_version":1}`))
	if err != nil {
		t.Fatalf("ParseDAG: %v", err)
	}
	if err := domain.ValidateDAGDefinition(dag); err != nil {
		t.Fatalf("ValidateDAG: %v", err)
	}

	ir := buildFlowIR(t, 1, dag, "")
	if !ir.NoOp {
		t.Fatal("NoOp = false, want true for a DAG without tasks")
	}
	if len(ir.Tasks) != 0 {
		t.Fatalf("Tasks = %+v, want empty", ir.Tasks)
	}
	if len(ir.Labels) != 6 {
		t.Fatalf("len(Labels) = %d, want 6", len(ir.Labels))
	}
	if ir.Labels["ontoaxis.environment"] != kestra.DefaultEnvironment {
		t.Fatalf("environment = %q, want %q", ir.Labels["ontoaxis.environment"], kestra.DefaultEnvironment)
	}
}

func TestBuildFlowIRTopologicalOrder(t *testing.T) {
	// Scrambled declaration order with two independent sources: the expected
	// order is the deterministic topological order with ties by task id.
	dag := domain.DAG{DAGVersion: domain.DAGVersionV1, Tasks: []domain.Task{
		dbtTask("omega", "middle", "beta"),
		dbtTask("zulu"),
		dbtTask("middle", "zulu"),
		dbtTask("beta", "alpha"),
		dbtTask("alpha"),
	}}

	ir := buildFlowIR(t, flowVersion, dag, "")

	gotIDs := make([]string, 0, len(ir.Tasks))
	for _, task := range ir.Tasks {
		gotIDs = append(gotIDs, task.ID)
	}
	wantIDs := []string{"alpha", "beta", "zulu", "middle", "omega"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("task order = %v, want topological order %v", gotIDs, wantIDs)
	}

	byID := make(map[string]kestra.FlowTask, len(ir.Tasks))
	for _, task := range ir.Tasks {
		byID[task.ID] = task
	}
	if got := byID["omega"].DependsOn; !reflect.DeepEqual(got, []string{"beta", "middle"}) {
		t.Fatalf("omega.DependsOn = %v, want sorted [beta middle]", got)
	}
	if got := byID["middle"].DependsOn; !reflect.DeepEqual(got, []string{"zulu"}) {
		t.Fatalf("middle.DependsOn = %v, want [zulu]", got)
	}
	if got := byID["alpha"].DependsOn; len(got) != 0 {
		t.Fatalf("alpha.DependsOn = %v, want empty", got)
	}
	if byID["middle"].Model != "model_middle" || byID["middle"].SQL != "select 1 as id" {
		t.Fatalf("middle task = %+v, want model/sql preserved", byID["middle"])
	}
}

func TestBuildFlowIRPreservesTaskFields(t *testing.T) {
	dag := domain.DAG{DAGVersion: domain.DAGVersionV1, Tasks: []domain.Task{
		{
			ID:           "middle",
			Type:         domain.TaskTypeDBT,
			DependsOn:    []string{" slog ", "alpha", "slog"},
			Model:        "middle_model",
			Materialized: "table",
			SQL:          "select * from source",
		},
		dbtTask("alpha"),
		dbtTask("slog"),
	}}
	ir := buildFlowIR(t, flowVersion, dag, "")
	if len(ir.Tasks) != 3 {
		t.Fatalf("len(Tasks) = %d, want 3", len(ir.Tasks))
	}
	var middle kestra.FlowTask
	for _, task := range ir.Tasks {
		if task.ID == "middle" {
			middle = task
		}
	}
	want := kestra.FlowTask{
		ID:           "middle",
		DependsOn:    []string{"alpha", "slog"},
		Model:        "middle_model",
		Materialized: "table",
		SQL:          "select * from source",
	}
	if !reflect.DeepEqual(middle, want) {
		t.Fatalf("middle task = %+v, want %+v", middle, want)
	}
}

func TestBuildFlowIRDefensiveValidation(t *testing.T) {
	tests := []struct {
		name     string
		tasks    []domain.Task
		wantCode string
	}{
		{
			name:     "duplicate task id",
			tasks:    []domain.Task{dbtTask("alpha"), dbtTask("alpha")},
			wantCode: domain.CodeDuplicateTaskID,
		},
		{
			name: "duplicate task id ignoring surrounding whitespace",
			tasks: []domain.Task{
				dbtTask("alpha"),
				{ID: " alpha ", Type: domain.TaskTypeDBT, Model: "m", SQL: "select 1"},
			},
			wantCode: domain.CodeDuplicateTaskID,
		},
		{
			name:     "empty task id",
			tasks:    []domain.Task{{ID: "  ", Type: domain.TaskTypeDBT, Model: "m", SQL: "select 1"}},
			wantCode: domain.CodeDuplicateTaskID,
		},
		{
			name:     "missing dependency",
			tasks:    []domain.Task{dbtTask("alpha", "ghost")},
			wantCode: domain.CodeMissingDependency,
		},
		{
			name:     "dependency cycle",
			tasks:    []domain.Task{dbtTask("alpha", "beta"), dbtTask("beta", "alpha")},
			wantCode: domain.CodeDependencyCycle,
		},
		{
			name:     "self dependency",
			tasks:    []domain.Task{dbtTask("alpha", "alpha")},
			wantCode: domain.CodeDependencyCycle,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ir, err := kestra.BuildFlowIR(flowTenantUUID, flowPipelineUUID, 1, domain.DAG{
				DAGVersion: domain.DAGVersionV1,
				Tasks:      test.tasks,
			}, "local")
			if err == nil {
				t.Fatalf("BuildFlowIR = %+v, want error", ir)
			}
			if got := domain.ValidationCode(err); got != test.wantCode {
				t.Fatalf("ValidationCode = %q, want %q (err: %v)", got, test.wantCode, err)
			}
			if !errors.Is(err, domain.ErrInvalidArgument) {
				t.Fatalf("error = %v, want errors.Is ErrInvalidArgument", err)
			}
		})
	}
}

func TestBuildFlowIRFromAuthoringDocument(t *testing.T) {
	raw := []byte(`{
		"dag_version": 1,
		"dataset_ids": ["ds-orders"],
		"inputs": [{"dataset_id": "ds-orders"}],
		"tasks": [
			{
				"id": "report",
				"type": "dbt",
				"depends_on": ["clean_orders"],
				"model": "report",
				"sql": "select * from {{ ref('clean_orders') }}"
			},
			{
				"id": "clean_orders",
				"type": "dbt",
				"model": "clean_orders",
				"materialized": "table",
				"sql": "select 1 as id"
			}
		],
		"outputs": [{"dataset_id": "ds-orders", "model": "report"}]
	}`)
	dag, err := domain.ParseDAG(raw)
	if err != nil {
		t.Fatalf("ParseDAG: %v", err)
	}
	if err := domain.ValidateDAGDefinition(dag); err != nil {
		t.Fatalf("ValidateDAG: %v", err)
	}

	ir := buildFlowIR(t, flowVersion, dag, "")
	if ir.NoOp {
		t.Fatal("NoOp = true, want false")
	}
	gotIDs := []string{ir.Tasks[0].ID, ir.Tasks[1].ID}
	if want := []string{"clean_orders", "report"}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("task order = %v, want %v", gotIDs, want)
	}
	content := string(renderFlow(t, ir))
	if !strings.Contains(content, "dbt run --select report") {
		t.Fatalf("rendered flow does not select the report model:\n%s", content)
	}
	if strings.Contains(content, "ref('clean_orders')") {
		t.Fatalf("renderer leaked SQL into flow YAML:\n%s", content)
	}
}

func TestRenderFlowYAMLGolden(t *testing.T) {
	dag := domain.DAG{DAGVersion: domain.DAGVersionV1, Tasks: []domain.Task{
		{
			ID:           "clean_orders",
			Type:         domain.TaskTypeDBT,
			Model:        "clean_orders",
			Materialized: "table",
			SQL:          "select * from raw.orders",
		},
		{
			ID:        "report",
			Type:      domain.TaskTypeDBT,
			DependsOn: []string{"clean_orders"},
			Model:     "report",
			SQL:       "select * from {{ ref('clean_orders') }}",
		},
	}}
	ir := buildFlowIR(t, flowVersion, dag, "")

	const want = `id: "22222222222242228222222222222222"
namespace: oa_11111111111141118111111111111111.22222222222242228222222222222222
revision: 3
labels:
  ontoaxis.dag_version: "1"
  ontoaxis.environment: local
  ontoaxis.managed_by: ontoaxis-core
  ontoaxis.pipeline_id: 22222222-2222-4222-8222-222222222222
  ontoaxis.pipeline_version: "3"
  ontoaxis.tenant_id: 11111111-1111-4111-8111-111111111111
tasks:
  - id: ontoaxis_pipeline
    type: io.kestra.plugin.core.flow.Sequential
    tasks:
      - id: clean_orders
        type: io.kestra.plugin.scripts.shell.Commands
        commands:
          - dbt run --select clean_orders --project-dir pipeline_22222222222242228222222222222222/v3 --profiles-dir pipeline_22222222222242228222222222222222/v3
      - id: report
        type: io.kestra.plugin.scripts.shell.Commands
        commands:
          - dbt run --select report --project-dir pipeline_22222222222242228222222222222222/v3 --profiles-dir pipeline_22222222222242228222222222222222/v3
        dependsOn:
          - clean_orders
`
	content := renderFlow(t, ir)
	if string(content) != want {
		t.Fatalf("rendered YAML mismatch\n--- got ---\n%s\n--- want ---\n%s", content, want)
	}

	flow := parseRenderedFlow(t, content)
	if flow.ID != flowPipelineHex || flow.Namespace != ir.Namespace || flow.Revision != flowVersion {
		t.Fatalf("rendered identity = %+v, want namespace %q revision %d", flow, ir.Namespace, flowVersion)
	}
	if len(flow.Labels) != 6 {
		t.Fatalf("rendered labels = %+v, want exactly 6 labels", flow.Labels)
	}
	if len(flow.Tasks) != 1 {
		t.Fatalf("rendered tasks = %d, want 1 Sequential wrapper", len(flow.Tasks))
	}
	wrapper := flow.Tasks[0]
	if wrapper.ID != "ontoaxis_pipeline" || wrapper.Type != "io.kestra.plugin.core.flow.Sequential" {
		t.Fatalf("wrapper = %+v, want Sequential ontoaxis_pipeline", wrapper)
	}
	if len(wrapper.Tasks) != 2 {
		t.Fatalf("wrapper children = %+v, want clean_orders and report", wrapper.Tasks)
	}
	cleanOrders, report := wrapper.Tasks[0], wrapper.Tasks[1]
	if cleanOrders.ID != "clean_orders" || report.ID != "report" {
		t.Fatalf("child order = %q, %q, want topological order", cleanOrders.ID, report.ID)
	}
	projectDir := "pipeline_" + flowPipelineHex + "/v3"
	wantCleanCommand := "dbt run --select clean_orders --project-dir " + projectDir + " --profiles-dir " + projectDir
	if !reflect.DeepEqual(cleanOrders.Commands, []string{wantCleanCommand}) {
		t.Fatalf("clean_orders commands = %v, want [%q]", cleanOrders.Commands, wantCleanCommand)
	}
	if !reflect.DeepEqual(report.DependsOn, []string{"clean_orders"}) {
		t.Fatalf("report dependsOn = %v, want [clean_orders]", report.DependsOn)
	}
	for _, forbidden := range []string{"raw.orders", "ref('clean_orders')", "SQL", "password", "token", "secret(", "://"} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("rendered flow leaked %q:\n%s", forbidden, content)
		}
	}
}

func TestRenderFlowYAMLNoTasksGolden(t *testing.T) {
	ir := buildFlowIR(t, 1, domain.DAG{DAGVersion: domain.DAGVersionV1}, "")

	const want = `id: "22222222222242228222222222222222"
namespace: oa_11111111111141118111111111111111.22222222222242228222222222222222
revision: 1
labels:
  ontoaxis.dag_version: "1"
  ontoaxis.environment: local
  ontoaxis.managed_by: ontoaxis-core
  ontoaxis.pipeline_id: 22222222-2222-4222-8222-222222222222
  ontoaxis.pipeline_version: "1"
  ontoaxis.tenant_id: 11111111-1111-4111-8111-111111111111
tasks:
  - id: ontoaxis_pipeline
    type: io.kestra.plugin.core.flow.Sequential
    tasks:
      - id: noop
        type: io.kestra.plugin.core.log.Log
        message: pipeline has no tasks
`
	content := renderFlow(t, ir)
	if string(content) != want {
		t.Fatalf("no-op rendered YAML mismatch\n--- got ---\n%s\n--- want ---\n%s", content, want)
	}

	flow := parseRenderedFlow(t, content)
	if len(flow.Tasks) != 1 || len(flow.Tasks[0].Tasks) != 1 {
		t.Fatalf("no-op structure = %+v, want Sequential with one task", flow.Tasks)
	}
	noop := flow.Tasks[0].Tasks[0]
	if noop.ID != "noop" || noop.Type != "io.kestra.plugin.core.log.Log" {
		t.Fatalf("no-op task = %+v, want io.kestra.plugin.core.log.Log noop", noop)
	}
	if noop.Message != "pipeline has no tasks" {
		t.Fatalf("no-op message = %q", noop.Message)
	}
}

func TestRenderFlowYAMLRootKeyOrder(t *testing.T) {
	ir := buildFlowIR(t, flowVersion, domain.DAG{DAGVersion: domain.DAGVersionV1, Tasks: []domain.Task{
		dbtTask("clean_orders"),
	}}, "")
	content := renderFlow(t, ir)

	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("unmarshal yaml node: %v", err)
	}
	if len(document.Content) != 1 {
		t.Fatalf("document nodes = %d, want 1", len(document.Content))
	}
	root := document.Content[0]
	if got, want := mappingKeys(root), []string{"id", "namespace", "revision", "labels", "tasks"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root key order = %v, want %v", got, want)
	}

	tasksNode := mappingValue(root, "tasks")
	if tasksNode == nil || tasksNode.Kind != yaml.SequenceNode || len(tasksNode.Content) != 1 {
		t.Fatalf("tasks node = %+v, want one wrapper task", tasksNode)
	}
	wrapper := tasksNode.Content[0]
	if got, want := mappingKeys(wrapper), []string{"id", "type", "tasks"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wrapper key order = %v, want %v", got, want)
	}
	children := mappingValue(wrapper, "tasks")
	if children == nil || len(children.Content) != 1 {
		t.Fatalf("wrapper children = %+v, want one dbt task", children)
	}
	if got, want := mappingKeys(children.Content[0]), []string{"id", "type", "commands"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dbt task key order = %v, want %v", got, want)
	}
}

func mappingKeys(node *yaml.Node) []string {
	keys := make([]string, 0, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content); index += 2 {
		keys = append(keys, node.Content[index].Value)
	}
	return keys
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func TestRenderFlowYAMLDeterminism(t *testing.T) {
	dag := domain.DAG{DAGVersion: domain.DAGVersionV1, Tasks: []domain.Task{
		dbtTask("omega", "middle", "beta"),
		dbtTask("zulu"),
		dbtTask("middle", "zulu"),
		dbtTask("beta", "alpha"),
		dbtTask("alpha"),
	}}

	first := buildFlowIR(t, flowVersion, dag, "")
	second := buildFlowIR(t, flowVersion, dag, "")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("BuildFlowIR is not deterministic:\nfirst  = %+v\nsecond = %+v", first, second)
	}

	want := renderFlow(t, first)
	for attempt := 0; attempt < 25; attempt++ {
		got := renderFlow(t, buildFlowIR(t, flowVersion, dag, ""))
		if !bytes.Equal(got, want) {
			t.Fatalf("render attempt %d differs:\n%s\n--- want ---\n%s", attempt, got, want)
		}
	}
	if got, again := kestra.FlowHash(want), kestra.FlowHash(want); got != again {
		t.Fatalf("FlowHash is not stable: %q vs %q", got, again)
	}
	if kestra.FlowHash(renderFlow(t, second)) != kestra.FlowHash(want) {
		t.Fatal("FlowHash differs for equivalent inputs")
	}
	other := renderFlow(t, buildFlowIR(t, flowVersion+1, dag, ""))
	if kestra.FlowHash(other) == kestra.FlowHash(want) {
		t.Fatal("FlowHash ignores the pipeline version")
	}
}

func TestRenderFlowYAMLRequiresIdentity(t *testing.T) {
	nonsense := []kestra.FlowIR{
		{},
		{FlowID: flowPipelineHex},
		{Namespace: "oa_x.y"},
	}
	for index, ir := range nonsense {
		if _, err := kestra.RenderFlowYAML(ir); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Fatalf("case %d: error = %v, want ErrInvalidArgument", index, err)
		}
	}

	valid := kestra.FlowIR{FlowID: flowPipelineHex, Namespace: "oa_x.y"}
	invalidTasks := []kestra.FlowTask{
		{ID: "", Model: "model"},
		{ID: "   ", Model: "model"},
		{ID: "task", Model: ""},
		{ID: "task", Model: "   "},
	}
	for index, task := range invalidTasks {
		ir := valid
		ir.Tasks = []kestra.FlowTask{task}
		if _, err := kestra.RenderFlowYAML(ir); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Fatalf("invalid task case %d: error = %v, want ErrInvalidArgument", index, err)
		}
	}
}

func TestRenderFlowYAMLSecretFreeLabels(t *testing.T) {
	dag := domain.DAG{DAGVersion: domain.DAGVersionV1, Tasks: []domain.Task{{
		ID:           "orders_daily",
		Type:         domain.TaskTypeDBT,
		Model:        "orders_daily",
		Materialized: "table",
		SQL:          "select * from raw_orders where api_token = 'hunter2' and password = 'hunter2'",
	}}}
	ir := buildFlowIR(t, flowVersion, dag, "production")
	content := renderFlow(t, ir)
	text := string(content)

	for _, forbidden := range []string{
		"hunter2", "raw_orders", "api_token", "password", "secret(", "Bearer",
		"postgres://", "trino://", "://", "DSN", "dsn=",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("rendered flow contains forbidden token %q:\n%s", forbidden, text)
		}
	}

	flow := parseRenderedFlow(t, content)
	if len(flow.Labels) != 6 {
		t.Fatalf("labels = %+v, want exactly the six identity labels", flow.Labels)
	}
	labelLines := make([]string, 0, 6)
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "  ontoaxis.") {
			labelLines = append(labelLines, line)
		}
	}
	if len(labelLines) != 6 {
		t.Fatalf("label lines = %v, want 6", labelLines)
	}
	if !sort.StringsAreSorted(labelLines) {
		t.Fatalf("label lines are not sorted: %v", labelLines)
	}
}

func TestFlowHash(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "empty content",
			input: nil,
			want:  "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:  "abc",
			input: []byte("abc"),
			want:  "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
		{
			name:  "empty slice",
			input: []byte{},
			want:  "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := kestra.FlowHash(test.input); got != test.want {
				t.Fatalf("FlowHash = %q, want %q", got, test.want)
			}
		})
	}

	hash := kestra.FlowHash([]byte("abc"))
	if !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("FlowHash = %q, want sha256: prefix", hash)
	}
	if len(hash) != len("sha256:")+64 {
		t.Fatalf("FlowHash length = %d, want 71", len(hash))
	}
	if hash[7:] != strings.ToLower(hash[7:]) {
		t.Fatalf("FlowHash = %q, want lower-case hex", hash)
	}
	if kestra.FlowHash([]byte("abc")) == kestra.FlowHash([]byte("abd")) {
		t.Fatal("FlowHash collision for different content")
	}
}
