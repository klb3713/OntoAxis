package authz

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	cedar "github.com/cedar-policy/cedar-go"
	xast "github.com/cedar-policy/cedar-go/x/exp/ast"
	"github.com/cedar-policy/cedar-go/x/exp/schema"
	"github.com/cedar-policy/cedar-go/x/exp/schema/resolved"
	"github.com/cedar-policy/cedar-go/x/exp/schema/validate"
	"github.com/openfga/language/pkg/go/transformer"
)

// Bundle is the immutable, versioned authorization bundle loaded at startup.
type Bundle struct {
	ModelDSL       []byte
	ModelTypeCount int
	SchemaJSON     []byte
	Schema         *resolved.Schema
	PolicySet      *cedar.PolicySet
	PolicyIDs      []string
	PolicyText     map[string][]byte
	Digest         string
	ModelDigest    string
}

const (
	ModelPath  = "openfga/model.fga"
	SchemaPath = "platform/schema.cedar.json"
)

// LoadBundle loads the embedded bundle. It compiles the OpenFGA model, resolves
// the Cedar schema, validates every Cedar policy against the schema, and
// computes a content digest for audit.
func LoadBundle(fsys fs.FS) (*Bundle, error) {
	model, err := fs.ReadFile(fsys, ModelPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ModelPath, err)
	}
	proto, err := transformer.TransformDSLToProto(string(model))
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", ModelPath, err)
	}
	schemaJSON, err := fs.ReadFile(fsys, SchemaPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", SchemaPath, err)
	}
	var s schema.Schema
	if err := json.Unmarshal(schemaJSON, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", SchemaPath, err)
	}
	resolvedSchema, err := s.Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", SchemaPath, err)
	}

	files, err := fs.Glob(fsys, "platform/*.cedar")
	if err != nil {
		return nil, fmt.Errorf("list Cedar policies: %w", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no Cedar policies found")
	}
	combined := cedar.NewPolicySet()
	validator := validate.New(resolvedSchema)
	bundle := &Bundle{ModelDSL: model, SchemaJSON: schemaJSON, Schema: resolvedSchema, PolicySet: combined, PolicyText: make(map[string][]byte)}
	if proto.TypeDefinitions != nil {
		bundle.ModelTypeCount = len(proto.TypeDefinitions)
	}
	h := sha256.New()
	writeDigestPart(h, ModelPath, model)
	writeDigestPart(h, SchemaPath, schemaJSON)
	for _, name := range files {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		writeDigestPart(h, name, data)
		bundle.PolicyText[name] = data
		ps, err := cedar.NewPolicySetFromBytes(path.Base(name), data)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", name, err)
		}
		found := 0
		for i := 0; ; i++ {
			internal := cedar.PolicyID("policy" + strconv.Itoa(i))
			policy := ps.Get(internal)
			if policy == nil {
				break
			}
			stable := cedar.PolicyID(name + "#" + strconv.Itoa(i))
			if err := validator.Policy(string(stable), (*xast.Policy)(policy.AST())); err != nil {
				return nil, fmt.Errorf("validate %s: %w", stable, err)
			}
			combined.Add(stable, policy)
			bundle.PolicyIDs = append(bundle.PolicyIDs, string(stable))
			found++
		}
		if found == 0 {
			return nil, fmt.Errorf("cedar file %s contains no policies", name)
		}
	}
	if len(bundle.PolicyIDs) == 0 {
		return nil, fmt.Errorf("cedar policy set is empty")
	}
	bundle.Digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	modelSum := sha256.Sum256(model)
	bundle.ModelDigest = "sha256:" + hex.EncodeToString(modelSum[:])
	return bundle, nil
}

func writeDigestPart(h interface{ Write([]byte) (int, error) }, name string, data []byte) {
	_, _ = h.Write([]byte(name))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(data)
	_, _ = h.Write([]byte{0})
}

// ValidateCatalogAgainstSchema cross-checks the Go catalog, the Cedar schema,
// and the policy text. It fails startup on any drift.
func ValidateCatalogAgainstSchema(cat *Catalog, bundle *Bundle) error {
	if cat == nil || bundle == nil {
		return fmt.Errorf("authz catalog or bundle is nil")
	}
	doc, err := parseSchemaDoc(bundle)
	if err != nil {
		return err
	}
	leaves := make(map[string]schemaAction)
	for name, action := range doc.Actions {
		if len(action.MemberOf) == 0 {
			continue
		}
		leaves[name] = action
	}
	if len(leaves) != len(cat.Actions()) {
		return fmt.Errorf("catalog has %d actions but schema has %d leaves", len(cat.Actions()), len(leaves))
	}
	for _, spec := range cat.Actions() {
		action, ok := leaves[string(spec.Action)]
		if !ok {
			return fmt.Errorf("catalog action %q is absent from the Cedar schema", spec.Action)
		}
		if len(action.MemberOf) != 1 || action.MemberOf[0].ID != string(spec.Group) {
			return fmt.Errorf("catalog action %q group does not match the Cedar schema", spec.Action)
		}
		if len(action.AppliesTo.ResourceTypes) == 0 {
			return fmt.Errorf("catalog action %q has no resource type in the Cedar schema", spec.Action)
		}
		want := fgaTypeToCedar[spec.ResourceType]
		matched := false
		for _, resourceType := range action.AppliesTo.ResourceTypes {
			if resourceType == want {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("catalog action %q resource type %q is not in schema %v", spec.Action, spec.ResourceType, action.AppliesTo.ResourceTypes)
		}
	}
	for _, group := range cat.Groups() {
		needle := `Action::"` + string(group) + `"`
		covered := false
		for _, data := range bundle.PolicyText {
			if strings.Contains(string(data), needle) {
				covered = true
				break
			}
		}
		if !covered {
			return fmt.Errorf("no Cedar policy covers group %q", group)
		}
	}
	return nil
}

type schemaDoc struct {
	Actions map[string]schemaAction `json:"actions"`
}

type schemaAction struct {
	MemberOf []struct {
		ID string `json:"id"`
	} `json:"memberOf"`
	AppliesTo struct {
		ResourceTypes []string `json:"resourceTypes"`
	} `json:"appliesTo"`
}

func parseSchemaDoc(bundle *Bundle) (schemaDoc, error) {
	if len(bundle.SchemaJSON) == 0 {
		return schemaDoc{}, fmt.Errorf("authz bundle schema JSON is empty")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bundle.SchemaJSON, &raw); err != nil {
		return schemaDoc{}, err
	}
	body, ok := raw[""]
	if !ok {
		return schemaDoc{}, fmt.Errorf("cedar schema has no root namespace")
	}
	var doc schemaDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return schemaDoc{}, err
	}
	return doc, nil
}
