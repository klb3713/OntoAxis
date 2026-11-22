package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound     = errors.New("action not found")
	ErrInvalidInput = errors.New("invalid action input")
	ErrConflict     = errors.New("action conflict")
)

const (
	ActionStatusActive   = "active"
	ActionStatusDraft    = "draft"
	ActionStatusDisabled = "disabled"

	ExecutionStatusQueued    = "queued"
	ExecutionStatusRunning   = "running"
	ExecutionStatusSucceeded = "succeeded"
	ExecutionStatusFailed    = "failed"
	ExecutionStatusCancelled = "cancelled"
)

type ActionType struct {
	ID           uuid.UUID      `json:"id"`
	TenantID     uuid.UUID      `json:"tenant_id"`
	Name         string         `json:"name"`
	DisplayName  string         `json:"display_name"`
	Description  string         `json:"description"`
	ObjectTypeID *uuid.UUID     `json:"object_type_id,omitempty"`
	Parameters   map[string]any `json:"parameters"`
	Status       string         `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type Execution struct {
	ID           uuid.UUID      `json:"id"`
	TenantID     uuid.UUID      `json:"tenant_id"`
	ActionTypeID uuid.UUID      `json:"action_type_id"`
	Status       string         `json:"status"`
	Parameters   map[string]any `json:"parameters"`
	Result       map[string]any `json:"result,omitempty"`
	Error        string         `json:"error,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	FinishedAt   *time.Time     `json:"finished_at,omitempty"`
}

func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrInvalidInput
	}
	return nil
}

func NormalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", ActionStatusActive:
		return ActionStatusActive
	case ActionStatusDraft:
		return ActionStatusDraft
	case ActionStatusDisabled:
		return ActionStatusDisabled
	default:
		return ""
	}
}
