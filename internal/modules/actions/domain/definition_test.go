package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/actions/domain"
)

func nanos(d time.Duration) float64 { return float64(d.Nanoseconds()) }

func validDefinition() domain.ExecutionDefinition {
	objectID := "1034e5c0-8c7a-4a55-9b6f-2b1b8b0f0001"
	return domain.ExecutionDefinition{
		Steps: []domain.StepDefinition{
			{
				ID:           "update-object",
				ActivityType: domain.ActivityTypeObjectUpdate,
				Parameters:   map[string]any{"object_id": objectID},
				Compensation: &domain.CompensationSpec{
					ActivityType: domain.ActivityTypeObjectUpdate,
					Parameters:   map[string]any{"object_id": objectID},
				},
				Timeout: 5 * time.Minute,
			},
			{
				ID:           "noop-step",
				ActivityType: domain.ActivityTypeNoop,
				ReadOnly:     true,
			},
		},
		RetryPolicy:     domain.DefaultRetryPolicy(),
		ApprovalTimeout: 30 * time.Minute,
		WorkflowTimeout: time.Hour,
	}
}

func TestDefaultRetryPolicy(t *testing.T) {
	t.Parallel()
	policy := domain.DefaultRetryPolicy()
	if policy.MaximumAttempts != 3 {
		t.Errorf("MaximumAttempts = %d, want 3", policy.MaximumAttempts)
	}
	if policy.InitialInterval != time.Second {
		t.Errorf("InitialInterval = %s, want 1s", policy.InitialInterval)
	}
	if policy.MaximumInterval != 30*time.Second {
		t.Errorf("MaximumInterval = %s, want 30s", policy.MaximumInterval)
	}
	if policy.BackoffCoefficient != 2.0 {
		t.Errorf("BackoffCoefficient = %v, want 2.0", policy.BackoffCoefficient)
	}
}

func TestSupportedActivityTypes(t *testing.T) {
	t.Parallel()
	required := []string{
		"object.update",
		"object.create",
		"object.delete",
		"test.transient_failure",
		"test.permanent_failure",
	}
	got := domain.SupportedActivityTypes()
	index := make(map[string]int, len(got))
	for i, activityType := range got {
		index[activityType] = i
	}
	for _, activityType := range required {
		if _, ok := index[activityType]; !ok {
			t.Errorf("SupportedActivityTypes() missing %q: %v", activityType, got)
		}
		if !domain.IsSupportedActivityType(activityType) {
			t.Errorf("IsSupportedActivityType(%q) = false, want true", activityType)
		}
	}
	if !domain.IsSupportedActivityType("noop") {
		t.Errorf("IsSupportedActivityType(%q) = false, want true: %v", "noop", got)
	}
	for _, activityType := range got {
		if _, ok := index[activityType]; !ok {
			t.Fatalf("activity type %q appears twice in %v", activityType, got)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("SupportedActivityTypes() is not sorted: %v", got)
		}
	}
	if domain.IsSupportedActivityType("object.unknown") {
		t.Error("unknown activity type must be rejected")
	}
	if domain.IsSupportedActivityType("") {
		t.Error("empty activity type must be rejected")
	}
}

func TestParseExecutionDefinitionDefaults(t *testing.T) {
	t.Parallel()
	def, err := domain.ParseExecutionDefinition(nil)
	if err != nil {
		t.Fatalf("ParseExecutionDefinition(nil) error = %v", err)
	}
	if len(def.Steps) != 0 {
		t.Errorf("steps = %v, want empty", def.Steps)
	}
	if def.RetryPolicy != domain.DefaultRetryPolicy() {
		t.Errorf("RetryPolicy = %+v, want default %+v", def.RetryPolicy, domain.DefaultRetryPolicy())
	}
	if def.ApprovalTimeout != 0 || def.WorkflowTimeout != 0 {
		t.Errorf("timeouts = %+v, want zero", def)
	}

	partial, err := domain.ParseExecutionDefinition(map[string]any{
		"retry": map[string]any{"maximum_attempts": float64(5)},
	})
	if err != nil {
		t.Fatalf("ParseExecutionDefinition(partial retry) error = %v", err)
	}
	if partial.RetryPolicy.MaximumAttempts != 5 {
		t.Errorf("MaximumAttempts = %d, want 5", partial.RetryPolicy.MaximumAttempts)
	}
	if partial.RetryPolicy.InitialInterval != time.Second ||
		partial.RetryPolicy.MaximumInterval != 30*time.Second ||
		partial.RetryPolicy.BackoffCoefficient != 2.0 {
		t.Errorf("partial retry did not fill defaults: %+v", partial.RetryPolicy)
	}

	withStepRetry, err := domain.ParseExecutionDefinition(map[string]any{
		"steps": []any{
			map[string]any{
				"id":            "read",
				"activity_type": "noop",
				"read_only":     true,
				"retry":         map[string]any{"backoff_coefficient": 1.5},
			},
		},
	})
	if err != nil {
		t.Fatalf("ParseExecutionDefinition(step retry) error = %v", err)
	}
	if len(withStepRetry.Steps) != 1 || withStepRetry.Steps[0].RetryPolicy == nil {
		t.Fatalf("step retry policy was not decoded: %+v", withStepRetry.Steps)
	}
	stepPolicy := withStepRetry.Steps[0].RetryPolicy
	if stepPolicy.BackoffCoefficient != 1.5 || stepPolicy.MaximumAttempts != 3 ||
		stepPolicy.InitialInterval != time.Second || stepPolicy.MaximumInterval != 30*time.Second {
		t.Errorf("step retry defaults not applied: %+v", *stepPolicy)
	}

	unknownField, err := domain.ParseExecutionDefinition(map[string]any{
		"steps":         []any{},
		"future_field":  "tolerated",
		"future_object": map[string]any{"nested": true},
	})
	if err != nil {
		t.Fatalf("ParseExecutionDefinition(unknown fields) error = %v", err)
	}
	if unknownField.RetryPolicy != domain.DefaultRetryPolicy() {
		t.Errorf("unknown-field parse lost retry defaults: %+v", unknownField.RetryPolicy)
	}
}

func TestParseExecutionDefinitionFull(t *testing.T) {
	t.Parallel()
	src := map[string]any{
		"steps": []any{
			map[string]any{
				"id":                "cmd",
				"activity_type":     "object.update",
				"parameters":        map[string]any{"object_id": "obj-1", "count": float64(2)},
				"requires_approval": true,
				"timeout":           nanos(10 * time.Minute),
				"retry": map[string]any{
					"maximum_attempts":    float64(4),
					"initial_interval":    nanos(2 * time.Second),
					"maximum_interval":    nanos(45 * time.Second),
					"backoff_coefficient": 1.7,
				},
				"compensation": map[string]any{
					"activity_type": "object.update",
					"parameters":    map[string]any{"object_id": "obj-1", "revert": true},
				},
			},
			map[string]any{
				"id":            "ro",
				"activity_type": "test.transient_failure",
				"read_only":     true,
			},
		},
		"retry": map[string]any{
			"maximum_attempts":    float64(2),
			"initial_interval":    nanos(500 * time.Millisecond),
			"maximum_interval":    nanos(20 * time.Second),
			"backoff_coefficient": 2.5,
		},
		"approval_timeout": nanos(15 * time.Minute),
		"workflow_timeout": nanos(2 * time.Hour),
	}
	def, err := domain.ParseExecutionDefinition(src)
	if err != nil {
		t.Fatalf("ParseExecutionDefinition error = %v", err)
	}
	if len(def.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(def.Steps))
	}
	step := def.Steps[0]
	if step.ID != "cmd" || step.ActivityType != domain.ActivityTypeObjectUpdate {
		t.Errorf("step identity = %+v", step)
	}
	if !step.RequiresApproval || step.ReadOnly || step.AllowWithoutCompensation {
		t.Errorf("step flags = %+v", step)
	}
	if step.Timeout != 10*time.Minute {
		t.Errorf("step timeout = %s, want 10m", step.Timeout)
	}
	if step.Parameters["object_id"] != "obj-1" {
		t.Errorf("step parameters = %+v", step.Parameters)
	}
	if step.RetryPolicy == nil ||
		step.RetryPolicy.MaximumAttempts != 4 ||
		step.RetryPolicy.InitialInterval != 2*time.Second ||
		step.RetryPolicy.MaximumInterval != 45*time.Second ||
		step.RetryPolicy.BackoffCoefficient != 1.7 {
		t.Errorf("step retry = %+v", step.RetryPolicy)
	}
	if step.Compensation == nil ||
		step.Compensation.ActivityType != domain.ActivityTypeObjectUpdate ||
		step.Compensation.Parameters["revert"] != true {
		t.Errorf("step compensation = %+v", step.Compensation)
	}
	if def.RetryPolicy.MaximumAttempts != 2 ||
		def.RetryPolicy.InitialInterval != 500*time.Millisecond ||
		def.RetryPolicy.MaximumInterval != 20*time.Second ||
		def.RetryPolicy.BackoffCoefficient != 2.5 {
		t.Errorf("execution retry = %+v", def.RetryPolicy)
	}
	if def.ApprovalTimeout != 15*time.Minute {
		t.Errorf("ApprovalTimeout = %s, want 15m", def.ApprovalTimeout)
	}
	if def.WorkflowTimeout != 2*time.Hour {
		t.Errorf("WorkflowTimeout = %s, want 2h", def.WorkflowTimeout)
	}
	if err := domain.ValidateExecutionDefinition(def); err != nil {
		t.Errorf("parsed full definition failed validation: %v", err)
	}
}

func TestParseExecutionDefinitionInvalid(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]any{
		"steps_not_a_list": {"steps": "not-a-list"},
		"step_not_an_object": {
			"steps": []any{"not-an-object"},
		},
		"retry_attempts_wrong_type": {
			"retry": map[string]any{"maximum_attempts": "three"},
		},
		"compensation_wrong_type": {
			"steps": []any{
				map[string]any{
					"id":            "s",
					"activity_type": "noop",
					"read_only":     true,
					"compensation":  "not-an-object",
				},
			},
		},
	}
	for name, src := range cases {
		if _, err := domain.ParseExecutionDefinition(src); !errors.Is(err, domain.ErrInvalidDefinition) {
			t.Errorf("%s: error = %v, want errors.Is(ErrInvalidDefinition)", name, err)
		}
	}
	if _, err := domain.ParseExecutionDefinition(map[string]any{"steps": func() {}}); !errors.Is(err, domain.ErrInvalidDefinition) {
		t.Errorf("unencodable definition: error = %v, want errors.Is(ErrInvalidDefinition)", err)
	}
}

func TestValidateExecutionDefinitionValid(t *testing.T) {
	t.Parallel()
	if err := domain.ValidateExecutionDefinition(validDefinition()); err != nil {
		t.Fatalf("valid definition rejected: %v", err)
	}

	empty := validDefinition()
	empty.Steps = nil
	if err := domain.ValidateExecutionDefinition(empty); err != nil {
		t.Errorf("definition with no steps rejected: %v", err)
	}

	bypass := validDefinition()
	bypass.Steps[0].Compensation = nil
	bypass.Steps[0].AllowWithoutCompensation = true
	if err := domain.ValidateExecutionDefinition(bypass); err != nil {
		t.Errorf("allow_without_compensation step rejected: %v", err)
	}

	readOnly := validDefinition()
	readOnly.Steps[0].Compensation = nil
	readOnly.Steps[0].ReadOnly = true
	if err := domain.ValidateExecutionDefinition(readOnly); err != nil {
		t.Errorf("read-only mutating step rejected: %v", err)
	}

	stepRetry := validDefinition()
	stepRetry.Steps[0].RetryPolicy = &domain.RetryPolicy{
		MaximumAttempts:    2,
		InitialInterval:    time.Second,
		MaximumInterval:    2 * time.Second,
		BackoffCoefficient: 1.5,
	}
	if err := domain.ValidateExecutionDefinition(stepRetry); err != nil {
		t.Errorf("valid step retry rejected: %v", err)
	}

	parsed, err := domain.ParseExecutionDefinition(map[string]any{
		"steps": []any{
			map[string]any{"id": "read", "activity_type": "noop", "read_only": true},
		},
	})
	if err != nil {
		t.Fatalf("ParseExecutionDefinition error = %v", err)
	}
	if err := domain.ValidateExecutionDefinition(parsed); err != nil {
		t.Errorf("parsed definition with applied defaults rejected: %v", err)
	}
}

func TestValidateExecutionDefinitionInvalid(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		mutate func(*domain.ExecutionDefinition)
		want   string
	}{
		"duplicate_step_id": {
			mutate: func(def *domain.ExecutionDefinition) {
				def.Steps = append(def.Steps, domain.StepDefinition{
					ID:           "update-object",
					ActivityType: domain.ActivityTypeNoop,
					ReadOnly:     true,
				})
			},
			want: "duplicate step id",
		},
		"empty_step_id": {
			mutate: func(def *domain.ExecutionDefinition) { def.Steps[0].ID = "   " },
			want:   "id is required",
		},
		"unknown_activity": {
			mutate: func(def *domain.ExecutionDefinition) { def.Steps[0].ActivityType = "object.unknown" },
			want:   "unknown activity type",
		},
		"unknown_compensation_activity": {
			mutate: func(def *domain.ExecutionDefinition) { def.Steps[0].Compensation.ActivityType = "object.unknown" },
			want:   "unknown compensation activity type",
		},
		"missing_compensation": {
			mutate: func(def *domain.ExecutionDefinition) { def.Steps[0].Compensation = nil },
			want:   "compensation is required",
		},
		"zero_execution_retry": {
			mutate: func(def *domain.ExecutionDefinition) { def.RetryPolicy = domain.RetryPolicy{} },
			want:   "retry maximum_attempts",
		},
		"execution_retry_zero_attempts": {
			mutate: func(def *domain.ExecutionDefinition) { def.RetryPolicy.MaximumAttempts = 0 },
			want:   "retry maximum_attempts",
		},
		"execution_retry_too_many_attempts": {
			mutate: func(def *domain.ExecutionDefinition) { def.RetryPolicy.MaximumAttempts = 6 },
			want:   "retry maximum_attempts",
		},
		"execution_retry_max_interval": {
			mutate: func(def *domain.ExecutionDefinition) {
				def.RetryPolicy.MaximumInterval = 2*time.Minute + time.Nanosecond
			},
			want: "maximum_interval",
		},
		"execution_retry_coefficient_low": {
			mutate: func(def *domain.ExecutionDefinition) { def.RetryPolicy.BackoffCoefficient = 0.9 },
			want:   "backoff_coefficient",
		},
		"execution_retry_coefficient_high": {
			mutate: func(def *domain.ExecutionDefinition) { def.RetryPolicy.BackoffCoefficient = 3.1 },
			want:   "backoff_coefficient",
		},
		"execution_retry_initial_above_max": {
			mutate: func(def *domain.ExecutionDefinition) {
				def.RetryPolicy.InitialInterval = 1 * time.Minute
				def.RetryPolicy.MaximumInterval = 30 * time.Second
			},
			want: "initial_interval",
		},
		"step_retry_too_many_attempts": {
			mutate: func(def *domain.ExecutionDefinition) {
				def.Steps[0].RetryPolicy = &domain.RetryPolicy{
					MaximumAttempts:    6,
					InitialInterval:    time.Second,
					MaximumInterval:    2 * time.Second,
					BackoffCoefficient: 2.0,
				}
			},
			want: "retry maximum_attempts",
		},
		"step_timeout_negative": {
			mutate: func(def *domain.ExecutionDefinition) { def.Steps[0].Timeout = -time.Nanosecond },
			want:   "timeout",
		},
		"step_timeout_too_large": {
			mutate: func(def *domain.ExecutionDefinition) {
				def.Steps[0].Timeout = domain.MaxDefinitionTimeout + time.Nanosecond
			},
			want: "timeout",
		},
		"approval_timeout_negative": {
			mutate: func(def *domain.ExecutionDefinition) { def.ApprovalTimeout = -time.Nanosecond },
			want:   "approval_timeout",
		},
		"approval_timeout_too_large": {
			mutate: func(def *domain.ExecutionDefinition) {
				def.ApprovalTimeout = domain.MaxDefinitionTimeout + time.Nanosecond
			},
			want: "approval_timeout",
		},
		"workflow_timeout_negative": {
			mutate: func(def *domain.ExecutionDefinition) { def.WorkflowTimeout = -time.Nanosecond },
			want:   "workflow_timeout",
		},
		"workflow_timeout_too_large": {
			mutate: func(def *domain.ExecutionDefinition) {
				def.WorkflowTimeout = domain.MaxDefinitionTimeout + time.Nanosecond
			},
			want: "workflow_timeout",
		},
		"too_many_steps": {
			mutate: func(def *domain.ExecutionDefinition) {
				steps := make([]domain.StepDefinition, 0, domain.MaxDefinitionSteps+1)
				for i := 0; i <= domain.MaxDefinitionSteps; i++ {
					steps = append(steps, domain.StepDefinition{
						ID:           "step-" + itoa(i),
						ActivityType: domain.ActivityTypeNoop,
						ReadOnly:     true,
					})
				}
				def.Steps = steps
			},
			want: "steps count",
		},
	}
	for name, tc := range cases {
		def := validDefinition()
		tc.mutate(&def)
		err := domain.ValidateExecutionDefinition(def)
		if err == nil {
			t.Errorf("%s: validation succeeded, want error", name)
			continue
		}
		if !errors.Is(err, domain.ErrInvalidDefinition) {
			t.Errorf("%s: error %v does not wrap ErrInvalidDefinition", name, err)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not contain %q", name, err, tc.want)
		}
	}
}

func TestValidateExecutionDefinitionBoundaries(t *testing.T) {
	t.Parallel()
	atLimit := validDefinition()
	atLimit.Steps = make([]domain.StepDefinition, 0, domain.MaxDefinitionSteps)
	for i := 0; i < domain.MaxDefinitionSteps; i++ {
		atLimit.Steps = append(atLimit.Steps, domain.StepDefinition{
			ID:           "step-" + itoa(i),
			ActivityType: domain.ActivityTypeNoop,
			ReadOnly:     true,
		})
	}
	atLimit.Steps[0].Timeout = domain.MaxDefinitionTimeout
	atLimit.ApprovalTimeout = domain.MaxDefinitionTimeout
	atLimit.WorkflowTimeout = domain.MaxDefinitionTimeout
	atLimit.RetryPolicy = domain.RetryPolicy{
		MaximumAttempts:    domain.MaxRetryAttempts,
		InitialInterval:    0,
		MaximumInterval:    domain.MaxRetryInterval,
		BackoffCoefficient: domain.MaxRetryBackoffCoefficient,
	}
	if err := domain.ValidateExecutionDefinition(atLimit); err != nil {
		t.Errorf("definition at upper bounds rejected: %v", err)
	}

	lowerBound := validDefinition()
	lowerBound.RetryPolicy = domain.RetryPolicy{
		MaximumAttempts:    1,
		InitialInterval:    0,
		MaximumInterval:    0,
		BackoffCoefficient: domain.MinRetryBackoffCoefficient,
	}
	if err := domain.ValidateExecutionDefinition(lowerBound); err != nil {
		t.Errorf("definition at lower bounds rejected: %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits [20]byte
	pos := len(digits)
	for i > 0 {
		pos--
		digits[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(digits[pos:])
}
