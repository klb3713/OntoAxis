// Package domain contains the pipeline authoring domain model. It does not
// import pgx, chi or any platform package so it can be reused by the service,
// repository and HTTP layers without cycles.
package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PipelineType mirrors ontoaxis.v1.pipelines.PipelineType after wire parsing.
type PipelineType string

const (
	PipelineTypeBatch       PipelineType = "batch"
	PipelineTypeFaster      PipelineType = "faster"
	PipelineTypeIncremental PipelineType = "incremental"
)

// ParsePipelineType validates a wire value. Missing or unknown values are
// rejected on writes; there is no implicit default.
func ParsePipelineType(raw string) (PipelineType, error) {
	value := PipelineType(strings.ToLower(strings.TrimSpace(raw)))
	if !value.Valid() {
		return "", fmt.Errorf("%w: unsupported pipeline type %q", ErrInvalidArgument, raw)
	}
	return value, nil
}

// Valid reports whether the type is one of the three ARCHITECTURE 5.1 values.
func (t PipelineType) Valid() bool {
	return t == PipelineTypeBatch || t == PipelineTypeFaster || t == PipelineTypeIncremental
}

// PipelineStatus is the lifecycle state of a Pipeline definition.
type PipelineStatus string

const (
	PipelineStatusDraft     PipelineStatus = "draft"
	PipelineStatusPublished PipelineStatus = "published"
	PipelineStatusArchived  PipelineStatus = "archived"
)

// Valid reports whether the status is one of the ARCHITECTURE 5.1 values.
func (s PipelineStatus) Valid() bool {
	return s == PipelineStatusDraft || s == PipelineStatusPublished || s == PipelineStatusArchived
}

// PipelineRunStatus is the lifecycle state of one execution attempt.
type PipelineRunStatus string

const (
	PipelineRunStatusQueued    PipelineRunStatus = "queued"
	PipelineRunStatusRunning   PipelineRunStatus = "running"
	PipelineRunStatusSucceeded PipelineRunStatus = "succeeded"
	PipelineRunStatusFailed    PipelineRunStatus = "failed"
	PipelineRunStatusCancelled PipelineRunStatus = "cancelled"
)

// Valid reports whether the run status is one of the ARCHITECTURE 5.1 values.
func (s PipelineRunStatus) Valid() bool {
	switch s {
	case PipelineRunStatusQueued, PipelineRunStatusRunning, PipelineRunStatusSucceeded,
		PipelineRunStatusFailed, PipelineRunStatusCancelled:
		return true
	default:
		return false
	}
}

// TriggerType records who started a Pipeline run.
type TriggerType string

const (
	TriggerTypeManual   TriggerType = "manual"
	TriggerTypeSchedule TriggerType = "schedule"
)

// Valid reports whether the trigger type is supported.
func (t TriggerType) Valid() bool {
	return t == TriggerTypeManual || t == TriggerTypeSchedule
}

// Pipeline is one tenant-scoped Pipeline definition (pipeline.pipelines).
type Pipeline struct {
	ID          string
	TenantID    string
	Name        string
	DisplayName string
	Description string
	Type        PipelineType
	DAG         json.RawMessage
	Version     int
	Status      PipelineStatus
	CreatedBy   string
	DeletedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PipelineVersion is an immutable published snapshot (pipeline.versions).
type PipelineVersion struct {
	ID          string
	TenantID    string
	PipelineID  string
	Version     int
	DAG         json.RawMessage
	PublishedAt time.Time
	PublishedBy string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PipelineRun is one execution attempt (pipeline.runs).
type PipelineRun struct {
	ID          string
	TenantID    string
	PipelineID  string
	Version     int
	Status      PipelineRunStatus
	TriggerType TriggerType
	StartedAt   *time.Time
	FinishedAt  *time.Time
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Schedule is one cron schedule attached to a Pipeline
// (pipeline.schedules). Schedule execution is delegated to Kestra in later
// stages; this module only stores the definition.
type Schedule struct {
	ID         string
	TenantID   string
	PipelineID string
	Cron       string
	Timezone   string
	Enabled    bool
	NextRunAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ValidateDAG ensures the definition is a JSON object. A nil definition is
// normalized to an empty object by the service layer before this check.
func ValidateDAG(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: dag must not be empty", ErrInvalidArgument)
	}
	if !json.Valid(raw) {
		return fmt.Errorf("%w: dag must be valid JSON", ErrInvalidArgument)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return fmt.Errorf("%w: dag must be a JSON object", ErrInvalidArgument)
	}
	if object == nil {
		return fmt.Errorf("%w: dag must be a JSON object", ErrInvalidArgument)
	}
	return nil
}

// DatasetIDsFromDAG extracts dataset references from the two documented
// shapes: a top-level dataset_ids array and inputs[].dataset_id. Unknown
// shapes are ignored; the definition contract is intentionally narrow.
func DatasetIDsFromDAG(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		DatasetIDs []string `json:"dataset_ids"`
		Inputs     []struct {
			DatasetID string `json:"dataset_id"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(payload.DatasetIDs)+len(payload.Inputs))
	out := make([]string, 0, len(payload.DatasetIDs)+len(payload.Inputs))
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, id := range payload.DatasetIDs {
		add(id)
	}
	for _, input := range payload.Inputs {
		add(input.DatasetID)
	}
	return out
}
