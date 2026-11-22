package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	// secretEnvPrefix prefixes environment-held connector secrets.
	secretEnvPrefix = "ONTOAXIS_SECRET_"
	// SecretKeyUsername is the resolved source database user.
	SecretKeyUsername = "username"
	// SecretKeyPassword is the resolved source database password.
	SecretKeyPassword = "password"
	// SecretKeyIcebergCatalogPassword optionally overrides the Iceberg catalog
	// password; it falls back to SecretKeyPassword.
	SecretKeyIcebergCatalogPassword = "iceberg_catalog_password"
)

// SecretResolver resolves a connector secret reference into string values. The
// returned values are used only to build submit requests and must never be
// persisted or logged. Implementations must not echo secret values in errors.
type SecretResolver interface {
	Resolve(ctx context.Context, tenantID, secretRef string) (map[string]string, error)
}

// EnvSecretResolver resolves secrets from ONTOAXIS_SECRET_<NORMALIZED_REF>
// environment variables containing a flat JSON object of strings. It is the
// default local implementation; production deployments may inject another one.
type EnvSecretResolver struct {
	lookup func(string) (string, bool)
}

var _ SecretResolver = (*EnvSecretResolver)(nil)

// NewEnvSecretResolver builds a resolver over the process environment.
func NewEnvSecretResolver() *EnvSecretResolver {
	return &EnvSecretResolver{lookup: os.LookupEnv}
}

// Resolve reads and validates the secret payload. Missing, empty, or malformed
// secrets return typed errors that never contain the reference or its values.
func (r *EnvSecretResolver) Resolve(ctx context.Context, _ /*tenantID*/, secretRef string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: request cancelled", ErrSecretUnavailable)
	}
	if strings.TrimSpace(secretRef) == "" {
		return nil, fmt.Errorf("%w: connector secret reference is empty", ErrSecretUnavailable)
	}
	lookup := r.lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	raw, ok := lookup(SecretEnvName(secretRef))
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, ErrSecretNotFound
	}
	values := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("%w: secret must be a JSON object of strings", ErrSecretInvalid)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: secret contains no values", ErrSecretInvalid)
	}
	for key, value := range values {
		if strings.TrimSpace(key) == "" || value == "" {
			return nil, fmt.Errorf("%w: secret key %q must not be empty", ErrSecretInvalid, key)
		}
	}
	return values, nil
}

// SecretEnvName normalizes a secret reference into its environment variable name:
// uppercase, with every non-alphanumeric rune replaced by an underscore.
func SecretEnvName(secretRef string) string {
	var builder strings.Builder
	builder.WriteString(secretEnvPrefix)
	for _, r := range strings.ToUpper(strings.TrimSpace(secretRef)) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
}
