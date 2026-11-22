package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSecretEnvName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ref  string
		want string
	}{
		{ref: "postgres/dev", want: "ONTOAXIS_SECRET_POSTGRES_DEV"},
		{ref: "prod/pg-main.01", want: "ONTOAXIS_SECRET_PROD_PG_MAIN_01"},
		{ref: "MixedCase", want: "ONTOAXIS_SECRET_MIXEDCASE"},
	}
	for _, tt := range tests {
		if got := SecretEnvName(tt.ref); got != tt.want {
			t.Errorf("SecretEnvName(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func newTestResolver(values map[string]string) (*EnvSecretResolver, *string) {
	var requested string
	resolver := &EnvSecretResolver{lookup: func(name string) (string, bool) {
		requested = name
		value, ok := values[name]
		return value, ok
	}}
	return resolver, &requested
}

func TestEnvSecretResolverSuccess(t *testing.T) {
	t.Parallel()

	resolver, requested := newTestResolver(map[string]string{
		"ONTOAXIS_SECRET_POSTGRES_DEV": `{"username":"cdc_user","password":"cdc_pass"}`,
	})

	secrets, err := resolver.Resolve(context.Background(), "11111111-1111-7111-8111-111111111111", "postgres/dev")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if *requested != "ONTOAXIS_SECRET_POSTGRES_DEV" {
		t.Errorf("lookup name = %q, want %q", *requested, "ONTOAXIS_SECRET_POSTGRES_DEV")
	}
	if secrets["username"] != "cdc_user" || secrets["password"] != "cdc_pass" {
		t.Errorf("Resolve() = %v, want username/password from env", secrets)
	}
}

func TestEnvSecretResolverMissing(t *testing.T) {
	t.Parallel()

	resolver, _ := newTestResolver(nil)
	_, err := resolver.Resolve(context.Background(), "11111111-1111-7111-8111-111111111111", "prod/missing")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Resolve() error = %v, want %v", err, ErrSecretNotFound)
	}
	if strings.Contains(err.Error(), "prod/missing") {
		t.Errorf("Resolve() error leaks secret_ref: %v", err)
	}
}

func TestEnvSecretResolverMalformedValues(t *testing.T) {
	t.Parallel()

	const sentinel = "super-secret-value"
	tests := []struct {
		name  string
		value string
	}{
		{name: "invalid json", value: `{"username":"` + sentinel},
		{name: "json array", value: `["` + sentinel + `"]`},
		{name: "empty object", value: `{}`},
		{name: "empty value", value: `{"username":""}`},
		{name: "empty payload", value: ``},
		{name: "not an object", value: `"` + sentinel + `"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolver, _ := newTestResolver(map[string]string{
				"ONTOAXIS_SECRET_PROD_PG": tt.value,
			})
			_, err := resolver.Resolve(context.Background(), "11111111-1111-7111-8111-111111111111", "prod/pg")
			if !errors.Is(err, ErrSecretInvalid) && !errors.Is(err, ErrSecretNotFound) {
				t.Fatalf("Resolve() error = %v, want ErrSecretInvalid or ErrSecretNotFound", err)
			}
			if err == nil || strings.Contains(err.Error(), sentinel) {
				t.Fatalf("Resolve() error leaks a secret value: %v", err)
			}
			if strings.Contains(err.Error(), "prod/pg") {
				t.Fatalf("Resolve() error leaks secret_ref: %v", err)
			}
		})
	}
}

func TestEnvSecretResolverEmptyRef(t *testing.T) {
	t.Parallel()

	resolver, _ := newTestResolver(nil)
	if _, err := resolver.Resolve(context.Background(), "11111111-1111-7111-8111-111111111111", ""); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("Resolve() error = %v, want %v", err, ErrSecretUnavailable)
	}
}

func TestEnvSecretResolverCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resolver, _ := newTestResolver(map[string]string{
		"ONTOAXIS_SECRET_PROD_PG": `{"username":"u","password":"p"}`,
	})
	if _, err := resolver.Resolve(ctx, "11111111-1111-7111-8111-111111111111", "prod/pg"); !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("Resolve() error = %v, want %v", err, ErrSecretUnavailable)
	}
}

func TestNewEnvSecretResolverReadsProcessEnv(t *testing.T) {
	envName := SecretEnvName("integration/test")
	t.Setenv(envName, `{"username":"env_user","password":"env_pass"}`)

	secrets, err := NewEnvSecretResolver().Resolve(context.Background(), "11111111-1111-7111-8111-111111111111", "integration/test")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if secrets["username"] != "env_user" || secrets["password"] != "env_pass" {
		t.Errorf("Resolve() = %v, want env credentials", secrets)
	}
}
