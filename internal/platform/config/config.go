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
	HTTPAddr        string        `koanf:"http_addr"`
	MetricsAddr     string        `koanf:"metrics_addr"`
	DatabaseURL     string        `koanf:"database_url"`
	LogLevel        string        `koanf:"log_level"`
	ShutdownTimeout time.Duration `koanf:"shutdown_timeout"`
	TrustGateway    bool          `koanf:"trust_gateway"`
	OIDCIssuer      string        `koanf:"oidc_issuer"`
	OIDCAudience    string        `koanf:"oidc_audience"`
	OIDCJWKSURL     string        `koanf:"oidc_jwks_url"`
	TrinoURL        string        `koanf:"trino_url"`
	TrinoUser       string        `koanf:"trino_user"`
	TrinoTimeout    time.Duration `koanf:"trino_timeout"`
}

// Defaults returns the non-secret local defaults. DATABASE_URL intentionally has
// no default and must be supplied by the operator.
func Defaults() Config {
	return Config{
		HTTPAddr:        ":8080",
		MetricsAddr:     ":9090",
		LogLevel:        "info",
		ShutdownTimeout: 10 * time.Second,
		OIDCIssuer:      "https://auth.ontoaxis.local/realms/ontoaxis",
		OIDCAudience:    "ontoaxis-api",
		TrinoUser:       "ontoaxis",
		TrinoTimeout:    30 * time.Second,
	}
}

// Load reads configuration from an optional ONTOAXIS_CONFIG file followed by
// ONTOAXIS_* environment variables. Environment variables override file values.
func Load() (Config, error) {
	k := koanf.New(".")
	cfg := Defaults()
	if err := k.Load(confmap.Provider(map[string]any{
		"http_addr":        cfg.HTTPAddr,
		"metrics_addr":     cfg.MetricsAddr,
		"database_url":     cfg.DatabaseURL,
		"log_level":        cfg.LogLevel,
		"shutdown_timeout": cfg.ShutdownTimeout.String(),
		"trust_gateway":    cfg.TrustGateway,
		"oidc_issuer":      cfg.OIDCIssuer,
		"oidc_audience":    cfg.OIDCAudience,
		"oidc_jwks_url":    cfg.OIDCJWKSURL,
		"trino_url":        cfg.TrinoURL,
		"trino_user":       cfg.TrinoUser,
		"trino_timeout":    cfg.TrinoTimeout.String(),
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
	return cfg, nil
}
