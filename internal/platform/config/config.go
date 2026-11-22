package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config contains process-wide configuration loaded from ONTOAXIS_* environment
// variables and an optional YAML file. Secrets are never logged by the app.
type Config struct {
	HTTPAddr                   string        `koanf:"http_addr"`
	MetricsAddr                string        `koanf:"metrics_addr"`
	DatabaseURL                string        `koanf:"database_url"`
	LogLevel                   string        `koanf:"log_level"`
	ShutdownTimeout            time.Duration `koanf:"shutdown_timeout"`
	TrustGateway               bool          `koanf:"trust_gateway"`
	OIDCIssuer                 string        `koanf:"oidc_issuer"`
	OIDCAudience               string        `koanf:"oidc_audience"`
	OIDCJWKSURL                string        `koanf:"oidc_jwks_url"`
	TrinoURL                   string        `koanf:"trino_url"`
	TrinoUser                  string        `koanf:"trino_user"`
	TrinoTimeout               time.Duration `koanf:"trino_timeout"`
	Environment                string        `koanf:"environment"`
	Commit                     string        `koanf:"commit"`
	OTLPEndpoint               string        `koanf:"otel_exporter_otlp_endpoint"`
	OTELSampleRatio            float64       `koanf:"otel_sample_ratio"`
	ActionsExecutionMode       string        `koanf:"actions_execution_mode"`
	TemporalHostPort           string        `koanf:"temporal_host_port"`
	ActionsApprovalTimeout     time.Duration `koanf:"actions_approval_timeout"`
	ActionsWorkflowTimeout     time.Duration `koanf:"actions_workflow_timeout"`
	KestraURL                  string        `koanf:"kestra_url"`
	KestraTimeout              time.Duration `koanf:"kestra_timeout"`
	KestraAPIToken             string        `koanf:"kestra_api_token"`
	PipelineDBTCatalog         string        `koanf:"pipeline_dbt_catalog"`
	PipelineDBTImage           string        `koanf:"pipeline_dbt_image"`
	PipelineNamespacePrefix    string        `koanf:"pipeline_namespace_prefix"`
	PipelineReconcileThrottle  time.Duration `koanf:"pipeline_reconcile_throttle"`
	PipelineRunRecoveryTimeout time.Duration `koanf:"pipeline_run_recovery_timeout"`
	PipelineRetryMaxAttempts   int           `koanf:"pipeline_retry_max_attempts"`
	PipelineIdempotencyTTL     time.Duration `koanf:"pipeline_idempotency_ttl"`
}

// Defaults returns the non-secret local defaults. DATABASE_URL intentionally has
// no default and must be supplied by the operator.
func Defaults() Config {
	return Config{
		HTTPAddr:                   ":8080",
		MetricsAddr:                ":9090",
		LogLevel:                   "info",
		ShutdownTimeout:            10 * time.Second,
		OIDCIssuer:                 "https://auth.ontoaxis.local/realms/ontoaxis",
		OIDCAudience:               "ontoaxis-api",
		TrinoUser:                  "ontoaxis",
		TrinoTimeout:               30 * time.Second,
		Environment:                "local",
		Commit:                     "unknown",
		OTELSampleRatio:            1.0,
		ActionsExecutionMode:       "temporal",
		ActionsApprovalTimeout:     30 * time.Minute,
		ActionsWorkflowTimeout:     5 * time.Minute,
		KestraTimeout:              30 * time.Second,
		PipelineDBTCatalog:         "memory",
		PipelineNamespacePrefix:    "oa",
		PipelineReconcileThrottle:  2 * time.Second,
		PipelineRunRecoveryTimeout: 2 * time.Minute,
		PipelineRetryMaxAttempts:   3,
		PipelineIdempotencyTTL:     24 * time.Hour,
	}
}

// Load reads configuration from an optional ONTOAXIS_CONFIG file followed by
// ONTOAXIS_* environment variables. Environment variables override file values.
func Load() (Config, error) {
	k := koanf.New(".")
	cfg := Defaults()
	if err := k.Load(confmap.Provider(map[string]any{
		"http_addr":                     cfg.HTTPAddr,
		"metrics_addr":                  cfg.MetricsAddr,
		"database_url":                  cfg.DatabaseURL,
		"log_level":                     cfg.LogLevel,
		"shutdown_timeout":              cfg.ShutdownTimeout.String(),
		"trust_gateway":                 cfg.TrustGateway,
		"oidc_issuer":                   cfg.OIDCIssuer,
		"oidc_audience":                 cfg.OIDCAudience,
		"oidc_jwks_url":                 cfg.OIDCJWKSURL,
		"trino_url":                     cfg.TrinoURL,
		"trino_user":                    cfg.TrinoUser,
		"trino_timeout":                 cfg.TrinoTimeout.String(),
		"environment":                   cfg.Environment,
		"commit":                        cfg.Commit,
		"otel_exporter_otlp_endpoint":   cfg.OTLPEndpoint,
		"otel_sample_ratio":             cfg.OTELSampleRatio,
		"actions_execution_mode":        cfg.ActionsExecutionMode,
		"temporal_host_port":            cfg.TemporalHostPort,
		"actions_approval_timeout":      cfg.ActionsApprovalTimeout.String(),
		"actions_workflow_timeout":      cfg.ActionsWorkflowTimeout.String(),
		"kestra_url":                    cfg.KestraURL,
		"kestra_timeout":                cfg.KestraTimeout.String(),
		"kestra_api_token":              cfg.KestraAPIToken,
		"pipeline_dbt_catalog":          cfg.PipelineDBTCatalog,
		"pipeline_dbt_image":            cfg.PipelineDBTImage,
		"pipeline_namespace_prefix":     cfg.PipelineNamespacePrefix,
		"pipeline_reconcile_throttle":   cfg.PipelineReconcileThrottle.String(),
		"pipeline_run_recovery_timeout": cfg.PipelineRunRecoveryTimeout.String(),
		"pipeline_retry_max_attempts":   cfg.PipelineRetryMaxAttempts,
		"pipeline_idempotency_ttl":      cfg.PipelineIdempotencyTTL.String(),
	}, "."), nil); err != nil {
		return Config{}, fmt.Errorf("load defaults: %w", err)
	}
	if path := os.Getenv("ONTOAXIS_CONFIG"); path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return Config{}, fmt.Errorf("load config file %q: %w", path, err)
		}
	}
	if err := k.Load(env.Provider("ONTOAXIS_", ".", func(key string) string {
		return strings.ToLower(strings.TrimPrefix(key, "ONTOAXIS_"))
	}), nil); err != nil {
		return Config{}, fmt.Errorf("load environment: %w", err)
	}
	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("ONTOAXIS_DATABASE_URL is required")
	}
	if cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("ONTOAXIS_SHUTDOWN_TIMEOUT must be positive")
	}
	if cfg.TrinoTimeout <= 0 {
		return Config{}, fmt.Errorf("ONTOAXIS_TRINO_TIMEOUT must be positive")
	}
	if cfg.OTELSampleRatio < 0 || cfg.OTELSampleRatio > 1 {
		return Config{}, fmt.Errorf("ONTOAXIS_OTEL_SAMPLE_RATIO must be between 0 and 1")
	}
	if cfg.ActionsExecutionMode != "temporal" && cfg.ActionsExecutionMode != "noop" {
		return Config{}, fmt.Errorf("ONTOAXIS_ACTIONS_EXECUTION_MODE must be temporal or noop")
	}
	if cfg.ActionsApprovalTimeout <= 0 {
		return Config{}, fmt.Errorf("ONTOAXIS_ACTIONS_APPROVAL_TIMEOUT must be positive")
	}
	if cfg.ActionsWorkflowTimeout <= 0 {
		return Config{}, fmt.Errorf("ONTOAXIS_ACTIONS_WORKFLOW_TIMEOUT must be positive")
	}
	if cfg.KestraTimeout <= 0 {
		return Config{}, fmt.Errorf("ONTOAXIS_KESTRA_TIMEOUT must be positive")
	}
	if cfg.PipelineReconcileThrottle <= 0 {
		return Config{}, fmt.Errorf("ONTOAXIS_PIPELINE_RECONCILE_THROTTLE must be positive")
	}
	if cfg.PipelineRunRecoveryTimeout <= 0 {
		return Config{}, fmt.Errorf("ONTOAXIS_PIPELINE_RUN_RECOVERY_TIMEOUT must be positive")
	}
	if cfg.PipelineRetryMaxAttempts <= 0 {
		return Config{}, fmt.Errorf("ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS must be positive")
	}
	if cfg.PipelineIdempotencyTTL <= 0 {
		return Config{}, fmt.Errorf("ONTOAXIS_PIPELINE_IDEMPOTENCY_TTL must be positive")
	}
	return cfg, nil
}
