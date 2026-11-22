package domain_test

import (
	"errors"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
)

func TestParseType(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"postgres", "mysql", "s3", "rest_api", "kafka"} {
		got, err := domain.ParseType(value)
		if err != nil {
			t.Fatalf("ParseType(%q) error = %v", value, err)
		}
		if string(got) != value {
			t.Fatalf("ParseType(%q) = %q", value, got)
		}
	}
	if _, err := domain.ParseType("oracle"); !errors.Is(err, domain.ErrInvalidType) {
		t.Fatalf("ParseType(oracle) error = %v, want ErrInvalidType", err)
	}
}

func TestParseStatus(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"active", "paused", "error"} {
		got, err := domain.ParseStatus(value)
		if err != nil {
			t.Fatalf("ParseStatus(%q) error = %v", value, err)
		}
		if string(got) != value {
			t.Fatalf("ParseStatus(%q) = %q", value, got)
		}
	}
	if _, err := domain.ParseStatus("deleted"); !errors.Is(err, domain.ErrInvalidStatus) {
		t.Fatalf("ParseStatus(deleted) error = %v, want ErrInvalidStatus", err)
	}
}

func TestValidateName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"pg", "my-connector", "s3_bucket"} {
		if err := domain.ValidateName(name); err != nil {
			t.Fatalf("ValidateName(%q) error = %v", name, err)
		}
	}
	for _, name := range []string{"", "My Connector", "-bad", "UPPER", "中文"} {
		if err := domain.ValidateName(name); !errors.Is(err, domain.ErrInvalidName) {
			t.Fatalf("ValidateName(%q) error = %v, want ErrInvalidName", name, err)
		}
	}
}

func TestValidateConfigRejectsSensitiveKeysRecursively(t *testing.T) {
	t.Parallel()
	cases := []map[string]any{
		{"password": "x"},
		{"connection": map[string]any{"api_key": "x"}},
		{"items": []any{map[string]any{"client_secret": "x"}}},
		{"API-KEY": "x"},
	}
	for _, cfg := range cases {
		if err := domain.ValidateConfig(cfg); !errors.Is(err, domain.ErrSensitiveConfig) {
			t.Fatalf("ValidateConfig(%v) error = %v, want ErrSensitiveConfig", cfg, err)
		}
	}
}

func TestValidateConfigAcceptsSafeValues(t *testing.T) {
	t.Parallel()
	cfg := map[string]any{
		"host":    "db.internal",
		"port":    float64(5432),
		"ssl":     true,
		"options": map[string]any{"schema": "public"},
		"hosts":   []any{"a", "b"},
	}
	if err := domain.ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
	if err := domain.ValidateConfig(nil); !errors.Is(err, domain.ErrInvalidConfig) {
		t.Fatalf("ValidateConfig(nil) error = %v, want ErrInvalidConfig", err)
	}
}

func TestValidateTags(t *testing.T) {
	t.Parallel()
	if err := domain.ValidateTags([]string{"prod", "analytics"}); err != nil {
		t.Fatalf("ValidateTags() error = %v", err)
	}
	if err := domain.ValidateTags([]string{"ok", " "}); !errors.Is(err, domain.ErrInvalidTags) {
		t.Fatalf("ValidateTags() error = %v, want ErrInvalidTags", err)
	}
}
