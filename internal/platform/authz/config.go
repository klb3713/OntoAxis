package authz

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Default timeouts from the authorization boundary. They are deliberately
// short: authorization must not become a slow dependency of every request.
const (
	DefaultOpenFGATimeout   = 200 * time.Millisecond
	DefaultDecisionTimeout  = 300 * time.Millisecond
	DefaultAuditSink        = "stdout"
	EnvironmentOpenFGAURL   = "ONTOAXIS_AUTHZ_OPENFGA_API_URL"
	EnvironmentOpenFGAStore = "ONTOAXIS_AUTHZ_OPENFGA_STORE_ID"
	EnvironmentOpenFGAModel = "ONTOAXIS_AUTHZ_OPENFGA_MODEL_ID"
	EnvironmentOpenFGAToken = "ONTOAXIS_AUTHZ_OPENFGA_API_TOKEN"
	EnvironmentOpenFGATime  = "ONTOAXIS_AUTHZ_OPENFGA_TIMEOUT"
	EnvironmentDecisionTime = "ONTOAXIS_AUTHZ_DECISION_TIMEOUT"
	EnvironmentPolicyDir    = "ONTOAXIS_AUTHZ_POLICY_DIR"
	EnvironmentAuditSink    = "ONTOAXIS_AUTHZ_AUDIT_SINK"
	EnvironmentSourceCommit = "ONTOAXIS_AUTHZ_SOURCE_COMMIT"
)

// Config is the runtime contract for the authorization decision point.
type Config struct {
	OpenFGAApiURL   string
	OpenFGAStoreID  string
	OpenFGAModelID  string
	OpenFGAAPIToken string
	OpenFGATimeout  time.Duration
	DecisionTimeout time.Duration
	// PolicyDir optionally overrides the embedded policy bundle for local
	// development. Production must leave it empty; there is no hot reload.
	PolicyDir    string
	AuditSink    string
	SourceCommit string
}

// LoadConfigFromEnv loads and validates ONTOAXIS_AUTHZ_* settings.
//
// There is intentionally no disabled/fail-open switch. Setting the legacy
// AUTH_DISABLED or an explicit fail-open flag is a configuration error.
func LoadConfigFromEnv() (Config, error) {
	if err := rejectUnsafeEnv(); err != nil {
		return Config{}, err
	}
	cfg := Config{
		OpenFGAApiURL:   strings.TrimSpace(os.Getenv(EnvironmentOpenFGAURL)),
		OpenFGAStoreID:  strings.TrimSpace(os.Getenv(EnvironmentOpenFGAStore)),
		OpenFGAModelID:  strings.TrimSpace(os.Getenv(EnvironmentOpenFGAModel)),
		OpenFGAAPIToken: strings.TrimSpace(os.Getenv(EnvironmentOpenFGAToken)),
		OpenFGATimeout:  DefaultOpenFGATimeout,
		DecisionTimeout: DefaultDecisionTimeout,
		PolicyDir:       strings.TrimSpace(os.Getenv(EnvironmentPolicyDir)),
		AuditSink:       strings.TrimSpace(os.Getenv(EnvironmentAuditSink)),
		SourceCommit:    strings.TrimSpace(os.Getenv(EnvironmentSourceCommit)),
	}
	if cfg.AuditSink == "" {
		cfg.AuditSink = DefaultAuditSink
	}
	var err error
	if raw := strings.TrimSpace(os.Getenv(EnvironmentOpenFGATime)); raw != "" {
		cfg.OpenFGATimeout, err = time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s: %w", EnvironmentOpenFGATime, err)
		}
	}
	if raw := strings.TrimSpace(os.Getenv(EnvironmentDecisionTime)); raw != "" {
		cfg.DecisionTimeout, err = time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s: %w", EnvironmentDecisionTime, err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func rejectUnsafeEnv() error {
	for _, key := range []string{"AUTH_DISABLED", "ONTOAXIS_AUTHZ_DISABLED", "ONTOAXIS_AUTHZ_FAIL_OPEN"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		enabled, err := strconv.ParseBool(raw)
		if err != nil || enabled {
			return fmt.Errorf("authorization cannot be disabled or fail open (%s=%q)", key, raw)
		}
	}
	return nil
}

// Validate checks the startup contract. Missing or malformed values fail
// closed; there is no default store, model, or anonymous subject.
func (c Config) Validate() error {
	if c.OpenFGAApiURL == "" {
		return fmt.Errorf("%s is required", EnvironmentOpenFGAURL)
	}
	u, err := url.Parse(c.OpenFGAApiURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be an absolute URL", EnvironmentOpenFGAURL)
	}
	if c.OpenFGAStoreID == "" {
		return fmt.Errorf("%s is required", EnvironmentOpenFGAStore)
	}
	if c.OpenFGAModelID == "" {
		return fmt.Errorf("%s is required", EnvironmentOpenFGAModel)
	}
	if c.OpenFGATimeout <= 0 {
		return fmt.Errorf("OpenFGA timeout must be positive")
	}
	if c.DecisionTimeout <= 0 {
		return fmt.Errorf("decision timeout must be positive")
	}
	if c.DecisionTimeout < c.OpenFGATimeout {
		return fmt.Errorf("decision timeout must not be shorter than the OpenFGA timeout")
	}
	if c.AuditSink != "" && c.AuditSink != DefaultAuditSink {
		return fmt.Errorf("unsupported audit sink %q; v1 supports only structured stdout", c.AuditSink)
	}
	if c.PolicyDir != "" {
		info, err := os.Stat(c.PolicyDir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("policy dir %q is not a readable directory", c.PolicyDir)
		}
	}
	return nil
}
