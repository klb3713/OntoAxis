package domain

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var (
	namePattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	syncPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	maxDescLength = 1024
)

// ValidateName validates an ontology object/property/link name.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w: name must match ^[a-z][a-z0-9_]{0,62}$", ErrInvalidArgument)
	}
	return nil
}

// ValidateID validates an application-generated UUID identifier.
func ValidateID(id string) error {
	if _, err := uuid.Parse(strings.TrimSpace(id)); err != nil {
		return fmt.Errorf("%w: identifier must be a UUID", ErrInvalidArgument)
	}
	return nil
}

// ValidateDisplayName validates an optional display name.
func ValidateDisplayName(displayName string) error {
	if len(displayName) > 128 {
		return fmt.Errorf("%w: display_name must be at most 128 characters", ErrInvalidArgument)
	}
	return nil
}

// ValidateDescription validates an optional description.
func ValidateDescription(description string) error {
	if len(description) > maxDescLength {
		return fmt.Errorf("%w: description must be at most %d characters", ErrInvalidArgument, maxDescLength)
	}
	return nil
}

// ValidatePropertyType validates the property type label.
func ValidatePropertyType(propertyType string) error {
	if strings.TrimSpace(propertyType) == "" {
		return fmt.Errorf("%w: property_type is required", ErrInvalidArgument)
	}
	if len(propertyType) > 64 {
		return fmt.Errorf("%w: property_type must be at most 64 characters", ErrInvalidArgument)
	}
	return nil
}

// ValidateCardinality validates a link cardinality.
func ValidateCardinality(cardinality Cardinality) error {
	switch cardinality {
	case CardinalityOneToOne, CardinalityOneToMany, CardinalityManyToMany:
		return nil
	default:
		return fmt.Errorf("%w: cardinality must be one of one_to_one, one_to_many, many_to_many", ErrInvalidArgument)
	}
}

// ValidateSyncMode validates a dataset sync mode label.
func ValidateSyncMode(syncMode string) error {
	if syncMode == "" {
		return nil
	}
	if !syncPattern.MatchString(syncMode) {
		return fmt.Errorf("%w: sync_mode must match ^[a-z][a-z0-9_-]{0,31}$", ErrInvalidArgument)
	}
	return nil
}

// ValidatePage validates a concrete pagination request (defaults must already
// have been applied by the caller).
func ValidatePage(page Page) error {
	if page.Page < 1 {
		return fmt.Errorf("%w: page must be at least 1", ErrInvalidArgument)
	}
	if page.PerPage < 1 || page.PerPage > MaxPerPage {
		return fmt.Errorf("%w: per_page must be between 1 and %d", ErrInvalidArgument, MaxPerPage)
	}
	return nil
}

// NormalizePage applies defaults and validates a pagination request.
func NormalizePage(page, perPage int) (Page, error) {
	if page == 0 {
		page = DefaultPage
	}
	if perPage == 0 {
		perPage = DefaultPerPage
	}
	p := Page{Page: page, PerPage: perPage}
	if err := ValidatePage(p); err != nil {
		return Page{}, err
	}
	return p, nil
}

// NormalizeName trims surrounding whitespace before validation.
func NormalizeName(name string) string {
	return strings.TrimSpace(name)
}
