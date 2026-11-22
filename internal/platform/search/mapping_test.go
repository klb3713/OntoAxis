package search

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
)

func TestBuildDocumentAndDisplayName(t *testing.T) {
	tenantID := uuid.New()
	objectTypeID := uuid.New()
	objectID := uuid.New()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("X", 3600))
	updated := created.Add(time.Hour)
	object := domain.Object{
		ID:           objectID,
		TenantID:     tenantID,
		ObjectTypeID: objectTypeID,
		Version:      7,
		Properties: map[string]any{
			"name":    "  Alpha  ",
			"title":   "Beta",
			"profile": map[string]any{"city": "Paris"},
		},
		CreatedAt: created,
		UpdatedAt: updated,
	}
	document, err := BuildDocument(object)
	if err != nil {
		t.Fatalf("BuildDocument error = %v", err)
	}
	if document.ID != objectID.String() || document.TenantID != tenantID.String() || document.ObjectTypeID != objectTypeID.String() {
		t.Fatalf("unexpected IDs in document: %+v", document)
	}
	if document.DisplayName != "Alpha" {
		t.Fatalf("DisplayName = %q, want Alpha", document.DisplayName)
	}
	if document.Version != 7 {
		t.Fatalf("Version = %d, want 7", document.Version)
	}
	if !document.CreatedAt.Equal(created) || document.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v, want UTC %v", document.CreatedAt, created)
	}
	if !strings.Contains(document.PropertiesText, "profile.city=Paris") {
		t.Fatalf("PropertiesText = %q, want nested property", document.PropertiesText)
	}
}

func TestBuildDocumentDisplayNameFallbackAndValidation(t *testing.T) {
	object := domain.Object{ID: uuid.New(), TenantID: uuid.New(), ObjectTypeID: uuid.New(), Properties: map[string]any{
		"name":  "   ",
		"title": "Title wins",
	}}
	document, err := BuildDocument(object)
	if err != nil {
		t.Fatalf("BuildDocument error = %v", err)
	}
	if document.DisplayName != "Title wins" {
		t.Fatalf("DisplayName = %q, want Title wins", document.DisplayName)
	}
	fallback := domain.Object{ID: uuid.New(), TenantID: uuid.New(), ObjectTypeID: uuid.New()}
	document, err = BuildDocument(fallback)
	if err != nil {
		t.Fatalf("BuildDocument error = %v", err)
	}
	if document.DisplayName != fallback.ID.String() {
		t.Fatalf("DisplayName = %q, want object ID", document.DisplayName)
	}
	if _, err := BuildDocument(domain.Object{}); err == nil {
		t.Fatal("BuildDocument with empty ID returned nil error")
	}
}

func TestFlattenPropertiesDeterministicAndSafe(t *testing.T) {
	properties := map[string]any{
		"z_key":    true,
		"a_key":    "value",
		"score":    42.5,
		"password": "do-not-index",
		"nested": map[string]any{
			"city":   "Paris",
			"secret": "hidden",
			"deep":   map[string]any{"x": "y"},
		},
		"tags": []string{"one", "two"},
		"nil":  nil,
	}
	first := FlattenProperties(properties)
	second := FlattenProperties(properties)
	if first != second {
		t.Fatalf("FlattenProperties is not deterministic:\n%q\n%q", first, second)
	}
	for _, want := range []string{"a_key=value", "z_key=true", "score=42.5", "nested.city=Paris"} {
		if !strings.Contains(first, want) {
			t.Fatalf("FlattenProperties missing %q in %q", want, first)
		}
	}
	for _, forbidden := range []string{"password", "secret", "do-not-index", "hidden", "tags", "deep"} {
		if strings.Contains(first, forbidden) {
			t.Fatalf("FlattenProperties leaked %q in %q", forbidden, first)
		}
	}
	if lines := strings.Split(first, "\n"); len(lines) != len([]string{"a_key=value", "nested.city=Paris", "score=42.5", "z_key=true"}) {
		t.Fatalf("FlattenProperties lines = %q", lines)
	}
}

func TestFlattenPropertiesTruncatesOnRuneBoundary(t *testing.T) {
	properties := map[string]any{"big": strings.Repeat("é", 4000)}
	text := FlattenProperties(properties)
	if len(text) > 4096 {
		t.Fatalf("FlattenProperties length = %d, want <= 4096", len(text))
	}
	if !utf8.ValidString(text) {
		t.Fatalf("FlattenProperties produced invalid UTF-8")
	}
	if !strings.HasPrefix(text, "big=") {
		t.Fatalf("FlattenProperties = %q", text[:10])
	}
}

func TestDocumentJSONTagsAndSettings(t *testing.T) {
	document := Document{ID: "id", TenantID: "tenant", ObjectTypeID: "type", DisplayName: "name", PropertiesText: "a=b", Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	for _, key := range []string{`"id"`, `"tenant_id"`, `"object_type_id"`, `"display_name"`, `"properties_text"`, `"version"`, `"created_at"`, `"updated_at"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("document JSON missing %s: %s", key, raw)
		}
	}
	if strings.Contains(string(raw), `"CreatedAt"`) {
		t.Fatalf("document JSON uses Go field names: %s", raw)
	}

	var settings map[string]any
	if err := json.Unmarshal(SettingsJSON(), &settings); err != nil {
		t.Fatalf("SettingsJSON is not valid JSON: %v", err)
	}
	assertStrings := func(key string, want []string) {
		t.Helper()
		rawValues, ok := settings[key].([]any)
		if !ok {
			t.Fatalf("settings[%s] = %#v", key, settings[key])
		}
		if len(rawValues) != len(want) {
			t.Fatalf("settings[%s] = %v, want %v", key, rawValues, want)
		}
		for i, value := range rawValues {
			if value != want[i] {
				t.Fatalf("settings[%s][%d] = %v, want %v", key, i, value, want[i])
			}
		}
	}
	assertStrings("searchableAttributes", []string{"display_name", "properties_text"})
	assertStrings("filterableAttributes", []string{"tenant_id", "object_type_id", "updated_at"})
	assertStrings("sortableAttributes", []string{"updated_at", "created_at"})
	assertStrings("displayedAttributes", []string{"id", "object_type_id", "display_name", "version", "updated_at"})
	pagination, ok := settings["pagination"].(map[string]any)
	if !ok || pagination["maxTotalHits"] != float64(MaxTotalHits) {
		t.Fatalf("settings pagination = %#v", settings["pagination"])
	}
}

func TestErrorTypedKinds(t *testing.T) {
	base := errors.New("boom")
	err := Wrap(ErrUnavailable, "test_op", base)
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, base) {
		t.Fatalf("Wrap does not expose both kind and cause: %v", err)
	}
	var typed *Error
	if !errors.As(err, &typed) {
		t.Fatalf("Wrap did not produce *Error")
	}
	if typed.Op != "test_op" {
		t.Fatalf("Op = %q", typed.Op)
	}
	if got := err.Error(); !strings.Contains(got, "unavailable") || !strings.Contains(got, "test_op") {
		t.Fatalf("Error() = %q", got)
	}
	if !IsUnavailable(err) || IsInvalid(err) || IsNotFound(err) {
		t.Fatalf("typed predicates misclassify %v", err)
	}
}
