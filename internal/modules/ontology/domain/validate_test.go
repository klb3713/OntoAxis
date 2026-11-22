package domain

import (
	"errors"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{"a", "customer", "customer_order", "a1_b2"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) error = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "Customer", "1customer", "customer-order", "customer name", "a2345678901234567890123456789012345678901234567890123456789012345678901234567890"}
	for _, name := range invalid {
		if err := ValidateName(name); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("ValidateName(%q) error = %v, want ErrInvalidArgument", name, err)
		}
	}
}

func TestValidateID(t *testing.T) {
	if err := ValidateID("018f0000-0000-7000-8000-000000000001"); err != nil {
		t.Fatalf("ValidateID(valid) error = %v", err)
	}
	if err := ValidateID("not-a-uuid"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ValidateID(invalid) error = %v, want ErrInvalidArgument", err)
	}
}

func TestValidateCardinality(t *testing.T) {
	for _, cardinality := range []Cardinality{CardinalityOneToOne, CardinalityOneToMany, CardinalityManyToMany} {
		if err := ValidateCardinality(cardinality); err != nil {
			t.Errorf("ValidateCardinality(%q) error = %v", cardinality, err)
		}
	}
	if err := ValidateCardinality("many"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ValidateCardinality(many) error = %v, want ErrInvalidArgument", err)
	}
}

func TestValidatePageAndNormalizePage(t *testing.T) {
	page, err := NormalizePage(0, 0)
	if err != nil {
		t.Fatalf("NormalizePage(0,0) error = %v", err)
	}
	if page.Page != DefaultPage || page.PerPage != DefaultPerPage {
		t.Fatalf("NormalizePage(0,0) = %+v, want defaults", page)
	}
	if _, err := NormalizePage(-1, 10); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("NormalizePage(-1,10) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := NormalizePage(1, MaxPerPage+1); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("NormalizePage(1,MaxPerPage+1) error = %v, want ErrInvalidArgument", err)
	}
}

func TestValidateSyncModeAndPropertyType(t *testing.T) {
	if err := ValidateSyncMode("manual"); err != nil {
		t.Fatalf("ValidateSyncMode(manual) error = %v", err)
	}
	if err := ValidateSyncMode("Invalid Mode"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ValidateSyncMode(Invalid Mode) error = %v, want ErrInvalidArgument", err)
	}
	if err := ValidatePropertyType(""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ValidatePropertyType(empty) error = %v, want ErrInvalidArgument", err)
	}
	if err := ValidatePropertyType("string"); err != nil {
		t.Fatalf("ValidatePropertyType(string) error = %v", err)
	}
}
