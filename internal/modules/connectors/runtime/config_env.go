package runtime

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// EnvConnectURL is the Kafka Connect REST base URL. Empty disables the runtime.
	EnvConnectURL = "ONTOAXIS_CONNECT_URL"
	// EnvConnectTimeout bounds one Kafka Connect REST request (Go duration).
	EnvConnectTimeout = "ONTOAXIS_CONNECT_TIMEOUT"
	// EnvConnectRetryMax is the retry count after the first attempt.
	EnvConnectRetryMax = "ONTOAXIS_CONNECT_RETRY_MAX"
	// EnvConnectVerifyMaxRows bounds count-based sync verification.
	EnvConnectVerifyMaxRows = "ONTOAXIS_CONNECT_VERIFY_MAX_ROWS"
	// EnvIcebergCatalogURI is the Iceberg JDBC catalog URI.
	EnvIcebergCatalogURI = "ONTOAXIS_ICEBERG_CATALOG_URI"
	// EnvIcebergWarehouse is the Iceberg warehouse root holding Dataset directories.
	EnvIcebergWarehouse = "ONTOAXIS_ICEBERG_WAREHOUSE"
	// EnvIcebergNamespace is the generated Iceberg catalog namespace.
	EnvIcebergNamespace = "ONTOAXIS_ICEBERG_NAMESPACE"
	// EnvIcebergCatalogUser is the non-secret Iceberg catalog JDBC user.
	EnvIcebergCatalogUser = "ONTOAXIS_ICEBERG_CATALOG_USER"
)

// maxConnectRetry bounds ONTOAXIS_CONNECT_RETRY_MAX to avoid unbounded retries.
const maxConnectRetry = 10

// LoadConfigFromEnv reads runtime wiring from the process environment. The bool
// result reports whether Kafka Connect is configured at all: an empty
// ONTOAXIS_CONNECT_URL disables the runtime while leaving the module usable.
// Validation errors never echo environment values.
func LoadConfigFromEnv() (Config, bool, error) {
	baseURL := strings.TrimSpace(os.Getenv(EnvConnectURL))
	if baseURL == "" {
		return Config{}, false, nil
	}
	cfg := Config{ConnectURL: baseURL}
	if raw := strings.TrimSpace(os.Getenv(EnvConnectTimeout)); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			return Config{}, false, fmt.Errorf("%w: %s must be a positive duration", ErrRuntimeNotConfigured, EnvConnectTimeout)
		}
		cfg.ConnectTimeout = timeout
	}
	if raw := strings.TrimSpace(os.Getenv(EnvConnectRetryMax)); raw != "" {
		retries, err := strconv.Atoi(raw)
		if err != nil || retries < 0 || retries > maxConnectRetry {
			return Config{}, false, fmt.Errorf("%w: %s must be an integer between 0 and %d", ErrRuntimeNotConfigured, EnvConnectRetryMax, maxConnectRetry)
		}
		cfg.ConnectRetryMax = retries
	}
	if raw := strings.TrimSpace(os.Getenv(EnvConnectVerifyMaxRows)); raw != "" {
		maxRows, err := strconv.Atoi(raw)
		if err != nil || maxRows < 0 {
			return Config{}, false, fmt.Errorf("%w: %s must be a non-negative integer", ErrRuntimeNotConfigured, EnvConnectVerifyMaxRows)
		}
		cfg.VerifyMaxRows = maxRows
	}
	cfg.Warehouse = Warehouse{
		CatalogURI:    strings.TrimSpace(os.Getenv(EnvIcebergCatalogURI)),
		WarehousePath: strings.TrimSpace(os.Getenv(EnvIcebergWarehouse)),
		Namespace:     strings.TrimSpace(os.Getenv(EnvIcebergNamespace)),
		CatalogUser:   strings.TrimSpace(os.Getenv(EnvIcebergCatalogUser)),
	}
	return cfg, true, nil
}
