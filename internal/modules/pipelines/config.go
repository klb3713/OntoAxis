package pipelines

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

// RuntimeConfig contains the pipeline execution settings owned by this module.
// Empty KestraURL keeps the module fail-closed for publish/trigger (design D12).
type RuntimeConfig struct {
	KestraURL          string
	KestraTimeout      time.Duration
	KestraAPIToken     string
	NamespacePrefix    string
	DBTCatalog         string
	DBTImage           string
	ReconcileThrottle  time.Duration
	RunRecoveryTimeout time.Duration
	RetryMaxAttempts   int
	IdempotencyTTL     time.Duration
	Environment        string
	Metrics            *observability.Metrics
}

// LoadRuntimeConfigFromEnv loads the D12 keys. Invalid integers/durations are
// returned as errors rather than silently falling back.
func LoadRuntimeConfigFromEnv() (RuntimeConfig, error) {
	cfg := RuntimeConfig{
		KestraURL:          strings.TrimSpace(os.Getenv("ONTOAXIS_KESTRA_URL")),
		KestraAPIToken:     os.Getenv("ONTOAXIS_KESTRA_API_TOKEN"),
		NamespacePrefix:    strings.TrimSpace(os.Getenv("ONTOAXIS_PIPELINE_NAMESPACE_PREFIX")),
		DBTCatalog:         strings.TrimSpace(os.Getenv("ONTOAXIS_PIPELINE_DBT_CATALOG")),
		DBTImage:           strings.TrimSpace(os.Getenv("ONTOAXIS_PIPELINE_DBT_IMAGE")),
		Environment:        strings.TrimSpace(os.Getenv("ONTOAXIS_ENVIRONMENT")),
		KestraTimeout:      10 * time.Second,
		ReconcileThrottle:  2 * time.Second,
		RunRecoveryTimeout: 2 * time.Minute,
		RetryMaxAttempts:   3,
		IdempotencyTTL:     24 * time.Hour,
	}
	var err error
	if cfg.KestraTimeout, err = durationEnv("ONTOAXIS_KESTRA_TIMEOUT", cfg.KestraTimeout); err != nil {
		return RuntimeConfig{}, err
	}
	if cfg.ReconcileThrottle, err = durationEnv("ONTOAXIS_PIPELINE_RECONCILE_THROTTLE", cfg.ReconcileThrottle); err != nil {
		return RuntimeConfig{}, err
	}
	if cfg.RunRecoveryTimeout, err = durationEnv("ONTOAXIS_PIPELINE_RUN_RECOVERY_TIMEOUT", cfg.RunRecoveryTimeout); err != nil {
		return RuntimeConfig{}, err
	}
	if cfg.IdempotencyTTL, err = durationEnv("ONTOAXIS_PIPELINE_IDEMPOTENCY_TTL", cfg.IdempotencyTTL); err != nil {
		return RuntimeConfig{}, err
	}
	if raw := strings.TrimSpace(os.Getenv("ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 1 {
			return RuntimeConfig{}, fmt.Errorf("invalid ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS %q", raw)
		}
		cfg.RetryMaxAttempts = value
	}
	if cfg.KestraTimeout <= 0 || cfg.ReconcileThrottle <= 0 || cfg.RunRecoveryTimeout <= 0 || cfg.IdempotencyTTL <= 0 {
		return RuntimeConfig{}, fmt.Errorf("pipeline runtime durations must be positive")
	}
	return cfg, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid %s %q", key, raw)
	}
	return value, nil
}
