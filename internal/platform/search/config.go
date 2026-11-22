package search

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Default values for ONTOAXIS_SEARCH_* configuration (design D11).
const (
	defaultSearchTimeout       = 1500 * time.Millisecond
	defaultSearchIndexVersion  = "v1"
	defaultSyncWindow          = 5 * time.Second
	defaultPollInterval        = 500 * time.Millisecond
	defaultConsumerBatch       = 50
	defaultConsumerMaxAttempts = 5
	defaultHTTPAddr            = ":8081"
	defaultMetricsAddr         = ":9094"
)

// Config is the search subsystem configuration loaded from the environment.
type Config struct {
	Enabled       bool
	URL           string
	APIKey        string
	Timeout       time.Duration
	IndexVersion  string
	SyncWindow    time.Duration
	PollInterval  time.Duration
	ConsumerBatch int
	MaxAttempts   int
	HTTPAddr      string
	MetricsAddr   string
}

// DefaultConfig returns the disabled-by-default configuration from design D11.
func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		URL:           "",
		APIKey:        "",
		Timeout:       defaultSearchTimeout,
		IndexVersion:  defaultSearchIndexVersion,
		SyncWindow:    defaultSyncWindow,
		PollInterval:  defaultPollInterval,
		ConsumerBatch: defaultConsumerBatch,
		MaxAttempts:   defaultConsumerMaxAttempts,
		HTTPAddr:      defaultHTTPAddr,
		MetricsAddr:   defaultMetricsAddr,
	}
}

// LoadConfigFromEnv reads ONTOAXIS_SEARCH_* variables, applies defaults, and
// validates the result. A disabled configuration never requires URL or API key
// and returns defaults for anything unset.
func LoadConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()

	enabled, err := envBool("ONTOAXIS_SEARCH_ENABLED", cfg.Enabled)
	if err != nil {
		return Config{}, err
	}
	cfg.Enabled = enabled
	cfg.URL = strings.TrimSpace(envString("ONTOAXIS_SEARCH_URL", cfg.URL))
	cfg.APIKey = strings.TrimSpace(envString("ONTOAXIS_SEARCH_API_KEY", cfg.APIKey))

	if cfg.Timeout, err = envDuration("ONTOAXIS_SEARCH_TIMEOUT", cfg.Timeout); err != nil {
		return Config{}, err
	}
	cfg.IndexVersion = strings.TrimSpace(envString("ONTOAXIS_SEARCH_INDEX_VERSION", cfg.IndexVersion))
	if cfg.SyncWindow, err = envDuration("ONTOAXIS_SEARCH_SYNC_WINDOW", cfg.SyncWindow); err != nil {
		return Config{}, err
	}
	if cfg.PollInterval, err = envDuration("ONTOAXIS_SEARCH_POLL_INTERVAL", cfg.PollInterval); err != nil {
		return Config{}, err
	}
	if cfg.ConsumerBatch, err = envInt("ONTOAXIS_SEARCH_CONSUMER_BATCH", cfg.ConsumerBatch); err != nil {
		return Config{}, err
	}
	if cfg.MaxAttempts, err = envInt("ONTOAXIS_SEARCH_CONSUMER_MAX_ATTEMPTS", cfg.MaxAttempts); err != nil {
		return Config{}, err
	}
	cfg.HTTPAddr = strings.TrimSpace(envString("ONTOAXIS_SEARCH_HTTP_ADDR", cfg.HTTPAddr))
	cfg.MetricsAddr = strings.TrimSpace(envString("ONTOAXIS_SEARCH_METRICS_ADDR", cfg.MetricsAddr))

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate enforces the D11 bounds. It is safe to call on a disabled config.
func (c Config) Validate() error {
	if c.Enabled {
		if c.URL == "" {
			return fmt.Errorf("ONTOAXIS_SEARCH_URL is required when search is enabled")
		}
		if c.APIKey == "" {
			return fmt.Errorf("ONTOAXIS_SEARCH_API_KEY is required when search is enabled")
		}
	}
	if c.URL != "" {
		parsed, err := url.Parse(c.URL)
		if err != nil {
			return fmt.Errorf("ONTOAXIS_SEARCH_URL is invalid")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("ONTOAXIS_SEARCH_URL must use http or https")
		}
		if parsed.Host == "" {
			return fmt.Errorf("ONTOAXIS_SEARCH_URL must include a host")
		}
	}
	if c.Timeout <= 0 || c.Timeout > 10*time.Second {
		return fmt.Errorf("ONTOAXIS_SEARCH_TIMEOUT must be greater than 0 and at most 10s")
	}
	if err := ValidateIndexVersion(c.IndexVersion); err != nil {
		return fmt.Errorf("ONTOAXIS_SEARCH_INDEX_VERSION is invalid: %w", err)
	}
	if c.SyncWindow < time.Second || c.SyncWindow > 60*time.Second {
		return fmt.Errorf("ONTOAXIS_SEARCH_SYNC_WINDOW must be between 1s and 60s")
	}
	if c.PollInterval < 100*time.Millisecond || c.PollInterval > 10*time.Second {
		return fmt.Errorf("ONTOAXIS_SEARCH_POLL_INTERVAL must be between 100ms and 10s")
	}
	if c.ConsumerBatch < 1 || c.ConsumerBatch > 500 {
		return fmt.Errorf("ONTOAXIS_SEARCH_CONSUMER_BATCH must be between 1 and 500")
	}
	if c.MaxAttempts < 1 || c.MaxAttempts > 20 {
		return fmt.Errorf("ONTOAXIS_SEARCH_CONSUMER_MAX_ATTEMPTS must be between 1 and 20")
	}
	return nil
}

func envString(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return parsed, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be a Go duration: %w", key, err)
	}
	return parsed, nil
}

func envInt(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}
