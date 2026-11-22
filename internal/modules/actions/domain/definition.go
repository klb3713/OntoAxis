package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ErrInvalidDefinition marks execution definitions that fail structural,
// activity, retry, timeout or compensation validation. Callers can match it
// with errors.Is and map it to the invalid_definition API error code.
var ErrInvalidDefinition = errors.New("invalid action execution definition")

// Accepted activity types for definition steps and compensations.
const (
	ActivityTypeObjectUpdate         = "object.update"
	ActivityTypeObjectCreate         = "object.create"
	ActivityTypeObjectDelete         = "object.delete"
	ActivityTypeTestTransientFailure = "test.transient_failure"
	ActivityTypeTestPermanentFailure = "test.permanent_failure"
	ActivityTypeNoop                 = "noop"
)

// Definition limits enforced by ValidateExecutionDefinition.
const (
	// MaxDefinitionSteps caps the number of steps in one execution definition.
	MaxDefinitionSteps = 50
	// MaxDefinitionTimeout caps step, approval and workflow timeouts.
	MaxDefinitionTimeout = 24 * time.Hour
	// MaxRetryAttempts caps retry attempts per policy.
	MaxRetryAttempts = 5
	// MaxRetryInterval caps the maximum backoff interval.
	MaxRetryInterval = 2 * time.Minute
	// MinRetryBackoffCoefficient is the smallest accepted backoff coefficient.
	MinRetryBackoffCoefficient = 1.0
	// MaxRetryBackoffCoefficient is the largest accepted backoff coefficient.
	MaxRetryBackoffCoefficient = 3.0
)

// RetryPolicy describes bounded retry and eviction behavior for an activity.
// Durations are encoded as integer nanoseconds by encoding/json, matching
// time.Duration's default JSON representation.
type RetryPolicy struct {
	MaximumAttempts    int           `json:"maximum_attempts"`
	InitialInterval    time.Duration `json:"initial_interval"`
	MaximumInterval    time.Duration `json:"maximum_interval"`
	BackoffCoefficient float64       `json:"backoff_coefficient"`
}

// CompensationSpec identifies the activity and parameters that roll back a
// step's side effect.
type CompensationSpec struct {
	ActivityType string         `json:"activity_type"`
	Parameters   map[string]any `json:"parameters,omitempty"`
}

// StepDefinition is one ordered unit of work in an execution definition.
type StepDefinition struct {
	ID                       string            `json:"id"`
	ActivityType             string            `json:"activity_type"`
	Parameters               map[string]any    `json:"parameters,omitempty"`
	RequiresApproval         bool              `json:"requires_approval,omitempty"`
	ReadOnly                 bool              `json:"read_only,omitempty"`
	AllowWithoutCompensation bool              `json:"allow_without_compensation,omitempty"`
	Compensation             *CompensationSpec `json:"compensation,omitempty"`
	Timeout                  time.Duration     `json:"timeout,omitempty"`
	RetryPolicy              *RetryPolicy      `json:"retry,omitempty"`
}

// ExecutionDefinition is the snapshot captured on an Execution at submission
// time and used by the workflow engine.
type ExecutionDefinition struct {
	Steps           []StepDefinition `json:"steps"`
	RetryPolicy     RetryPolicy      `json:"retry,omitempty"`
	ApprovalTimeout time.Duration    `json:"approval_timeout,omitempty"`
	WorkflowTimeout time.Duration    `json:"workflow_timeout,omitempty"`
}

// DefaultRetryPolicy returns the reviewed default retry policy: 3 attempts,
// 1s initial interval, 2.0 backoff coefficient and a 30s maximum interval.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaximumAttempts:    3,
		InitialInterval:    time.Second,
		MaximumInterval:    30 * time.Second,
		BackoffCoefficient: 2.0,
	}
}

var supportedActivityTypes = map[string]struct{}{
	ActivityTypeObjectUpdate:         {},
	ActivityTypeObjectCreate:         {},
	ActivityTypeObjectDelete:         {},
	ActivityTypeTestTransientFailure: {},
	ActivityTypeTestPermanentFailure: {},
	ActivityTypeNoop:                 {},
}

// SupportedActivityTypes returns the accepted activity types in sorted order.
func SupportedActivityTypes() []string {
	out := make([]string, 0, len(supportedActivityTypes))
	for activityType := range supportedActivityTypes {
		out = append(out, activityType)
	}
	sort.Strings(out)
	return out
}

// IsSupportedActivityType reports whether the activity type may appear in an
// execution definition.
func IsSupportedActivityType(activityType string) bool {
	_, ok := supportedActivityTypes[strings.TrimSpace(activityType)]
	return ok
}

// ParseExecutionDefinition decodes an execution definition from its stored
// map representation and fills in default retry policies. Unknown JSON fields
// are tolerated so definitions can evolve additively; validation of the known
// fields is done by ValidateExecutionDefinition.
func ParseExecutionDefinition(src map[string]any) (ExecutionDefinition, error) {
	var def ExecutionDefinition
	if len(src) > 0 {
		payload, err := json.Marshal(src)
		if err != nil {
			return ExecutionDefinition{}, fmt.Errorf("%w: encode definition: %v", ErrInvalidDefinition, err)
		}
		if err := json.Unmarshal(payload, &def); err != nil {
			return ExecutionDefinition{}, fmt.Errorf("%w: decode definition: %v", ErrInvalidDefinition, err)
		}
	}
	applyDefinitionDefaults(&def)
	return def, nil
}

func applyDefinitionDefaults(def *ExecutionDefinition) {
	def.RetryPolicy = retryPolicyWithDefaults(def.RetryPolicy)
	for i := range def.Steps {
		if def.Steps[i].RetryPolicy == nil {
			continue
		}
		policy := retryPolicyWithDefaults(*def.Steps[i].RetryPolicy)
		def.Steps[i].RetryPolicy = &policy
	}
}

// retryPolicyWithDefaults replaces zero values with the reviewed defaults. It
// must only be applied after decoding; negative values stay untouched so
// validation can reject them.
func retryPolicyWithDefaults(policy RetryPolicy) RetryPolicy {
	defaults := DefaultRetryPolicy()
	if policy.MaximumAttempts == 0 {
		policy.MaximumAttempts = defaults.MaximumAttempts
	}
	if policy.InitialInterval == 0 {
		policy.InitialInterval = defaults.InitialInterval
	}
	if policy.MaximumInterval == 0 {
		policy.MaximumInterval = defaults.MaximumInterval
	}
	if policy.BackoffCoefficient == 0 {
		policy.BackoffCoefficient = defaults.BackoffCoefficient
	}
	return policy
}

// ValidateExecutionDefinition enforces the reviewed execution definition
// contract. Every failure wraps ErrInvalidDefinition.
func ValidateExecutionDefinition(def ExecutionDefinition) error {
	if len(def.Steps) > MaxDefinitionSteps {
		return invalidDefinition("steps count %d exceeds maximum %d", len(def.Steps), MaxDefinitionSteps)
	}
	seen := make(map[string]struct{}, len(def.Steps))
	for i, step := range def.Steps {
		id := strings.TrimSpace(step.ID)
		if id == "" {
			return invalidDefinition("step %d: id is required", i)
		}
		if _, ok := seen[id]; ok {
			return invalidDefinition("duplicate step id %q", id)
		}
		seen[id] = struct{}{}
		activityType := strings.TrimSpace(step.ActivityType)
		if !IsSupportedActivityType(activityType) {
			return invalidDefinition("step %q: unknown activity type %q", id, step.ActivityType)
		}
		if step.Timeout < 0 || step.Timeout > MaxDefinitionTimeout {
			return invalidDefinition("step %q: timeout must be between 0 and %s", id, MaxDefinitionTimeout)
		}
		if !step.ReadOnly && !step.AllowWithoutCompensation && step.Compensation == nil {
			return invalidDefinition("step %q: compensation is required unless the step is read-only or allow_without_compensation is set", id)
		}
		if step.Compensation != nil && !IsSupportedActivityType(step.Compensation.ActivityType) {
			return invalidDefinition("step %q: unknown compensation activity type %q", id, step.Compensation.ActivityType)
		}
		if step.RetryPolicy != nil {
			if err := validateRetryPolicy(step.RetryPolicy); err != nil {
				return fmt.Errorf("step %q: %w", id, err)
			}
		}
	}
	if err := validateRetryPolicy(&def.RetryPolicy); err != nil {
		return fmt.Errorf("execution retry policy: %w", err)
	}
	if def.ApprovalTimeout < 0 || def.ApprovalTimeout > MaxDefinitionTimeout {
		return invalidDefinition("approval_timeout must be between 0 and %s", MaxDefinitionTimeout)
	}
	if def.WorkflowTimeout < 0 || def.WorkflowTimeout > MaxDefinitionTimeout {
		return invalidDefinition("workflow_timeout must be between 0 and %s", MaxDefinitionTimeout)
	}
	return nil
}

func validateRetryPolicy(policy *RetryPolicy) error {
	if policy.MaximumAttempts < 1 || policy.MaximumAttempts > MaxRetryAttempts {
		return invalidDefinition("retry maximum_attempts must be between 1 and %d", MaxRetryAttempts)
	}
	if policy.InitialInterval < 0 || policy.MaximumInterval < policy.InitialInterval {
		return invalidDefinition("retry initial_interval must be non-negative and not greater than maximum_interval")
	}
	if policy.MaximumInterval > MaxRetryInterval {
		return invalidDefinition("retry maximum_interval must not exceed %s", MaxRetryInterval)
	}
	if policy.BackoffCoefficient < MinRetryBackoffCoefficient || policy.BackoffCoefficient > MaxRetryBackoffCoefficient {
		return invalidDefinition("retry backoff_coefficient must be between %.1f and %.1f", MinRetryBackoffCoefficient, MaxRetryBackoffCoefficient)
	}
	return nil
}

func invalidDefinition(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidDefinition, fmt.Sprintf(format, args...))
}
