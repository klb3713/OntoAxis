package search

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
)

// MaxTotalHits is the Meilisearch pagination window configured for every
// object index. Results beyond this window are reported as truncated.
const MaxTotalHits = 10000

// PrimaryKey is the Meilisearch primary key for object documents.
const PrimaryKey = "id"

// propertiesTextLimit bounds properties_text to 4096 bytes (on a rune
// boundary) so index documents cannot grow without limit.
const propertiesTextLimit = 4096

// displayedNameKeys is the ordered list of property keys used to derive a
// document display name.
var displayedNameKeys = []string{"name", "title", "label", "display_name"}

// sensitiveKeyPattern matches property key names that must never enter the
// search index. Matching is case-insensitive and applies to whole subtrees.
var sensitiveKeyPattern = regexp.MustCompile(`(?i)password|passwd|secret|token|api_key|credential|private_key`)

// Document is the index representation of an object instance. It is the only
// shape written to Meilisearch: raw object properties are never indexed.
type Document struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	ObjectTypeID   string    `json:"object_type_id"`
	DisplayName    string    `json:"display_name"`
	PropertiesText string    `json:"properties_text"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// BuildDocument maps a domain object to its deterministic index document.
func BuildDocument(obj domain.Object) (Document, error) {
	if obj.ID == uuid.Nil {
		return Document{}, Errorf(ErrInvalid, "build_document", "object id is empty")
	}
	return Document{
		ID:             obj.ID.String(),
		TenantID:       obj.TenantID.String(),
		ObjectTypeID:   obj.ObjectTypeID.String(),
		DisplayName:    displayName(obj),
		PropertiesText: FlattenProperties(obj.Properties),
		Version:        obj.Version,
		CreatedAt:      obj.CreatedAt.UTC(),
		UpdatedAt:      obj.UpdatedAt.UTC(),
	}, nil
}

// displayName returns the first non-empty string among name, title, label and
// display_name, falling back to the object ID.
func displayName(obj domain.Object) string {
	for _, key := range displayedNameKeys {
		raw, ok := obj.Properties[key]
		if !ok {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return obj.ID.String()
}

// FlattenProperties renders properties as deterministic "key=value" lines,
// sorted by key path, excluding sensitive subtrees and non-scalar values.
// Nested maps are traversed to depth 2 (the root map counts as depth 1).
func FlattenProperties(properties map[string]any) string {
	if len(properties) == 0 {
		return ""
	}
	entries := make([]string, 0, len(properties))
	flattenMap("", properties, 1, &entries)
	sort.Strings(entries)
	return truncateUTF8(strings.Join(entries, "\n"), propertiesTextLimit)
}

func flattenMap(prefix string, values map[string]any, depth int, entries *[]string) {
	if len(values) == 0 || depth > 2 {
		return
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if sensitiveKeyPattern.MatchString(key) {
			continue
		}
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		switch value := values[key].(type) {
		case map[string]any:
			if depth < 2 {
				flattenMap(path, value, depth+1, entries)
			}
		case map[string]string:
			if depth < 2 {
				nested := make(map[string]any, len(value))
				for nestedKey, nestedValue := range value {
					nested[nestedKey] = nestedValue
				}
				flattenMap(path, nested, depth+1, entries)
			}
		default:
			if rendered, ok := renderScalar(value); ok {
				*entries = append(*entries, path+"="+rendered)
			}
		}
	}
}

// renderScalar converts a scalar JSON value to its stable text form. Maps,
// slices, nil and other non-scalar values are skipped.
func renderScalar(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case json.Number:
		return v.String(), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32), true
	case int:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	default:
		return "", false
	}
}

// truncateUTF8 truncates s to at most max bytes without splitting a rune.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.ValidString(s[:cut]) {
		cut--
	}
	return s[:cut]
}

// SettingsJSON returns the Meilisearch index settings applied by EnsureIndex.
// A fresh copy is returned so callers cannot mutate package state.
func SettingsJSON() json.RawMessage {
	return json.RawMessage(`{
  "searchableAttributes": ["display_name", "properties_text"],
  "filterableAttributes": ["tenant_id", "object_type_id", "updated_at"],
  "sortableAttributes": ["updated_at", "created_at"],
  "displayedAttributes": ["id", "object_type_id", "display_name", "version", "updated_at"],
  "pagination": {"maxTotalHits": 10000}
}`)
}
