package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound     = errors.New("object not found")
	ErrInvalidInput = errors.New("invalid object input")
	ErrConflict     = errors.New("object conflict")
)

// Object is a tenant-scoped JSON object instance.
type Object struct {
	ID           uuid.UUID      `json:"id"`
	TenantID     uuid.UUID      `json:"tenant_id"`
	ObjectTypeID uuid.UUID      `json:"object_type_id"`
	Version      int64          `json:"version"`
	Properties   map[string]any `json:"properties"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// Link is a tenant-scoped relationship between two object instances.
type Link struct {
	ID         uuid.UUID      `json:"id"`
	TenantID   uuid.UUID      `json:"tenant_id"`
	LinkTypeID uuid.UUID      `json:"link_type_id"`
	SourceID   uuid.UUID      `json:"source_id"`
	TargetID   uuid.UUID      `json:"target_id"`
	Properties map[string]any `json:"properties,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// ValidateObject checks the minimum invariants before persistence.
func ValidateObject(objectTypeID uuid.UUID, properties map[string]any) error {
	if objectTypeID == uuid.Nil {
		return ErrInvalidInput
	}
	if len(properties) == 0 {
		return ErrInvalidInput
	}
	for key := range properties {
		if strings.TrimSpace(key) == "" {
			return ErrInvalidInput
		}
	}
	return nil
}

// ValidateLink checks the minimum link invariants before persistence.
func ValidateLink(linkTypeID, sourceID, targetID uuid.UUID) error {
	if linkTypeID == uuid.Nil || sourceID == uuid.Nil || targetID == uuid.Nil {
		return ErrInvalidInput
	}
	return nil
}
