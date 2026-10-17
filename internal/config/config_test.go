package config

import (
	"strings"
	"testing"
)

const validSecret = "0123456789abcdef0123456789abcdef"

// setEnv clears every variable Load reads, then applies the overrides, so a
// developer's shell cannot leak into the test.
func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()

	for _, key := range []string{"PORT", "METRICS_PORT", "APP_ENV", "DATABASE_URL", "JWT_SECRET", "TRUST_PROXY_HEADERS"} {
		t.Setenv(key, "")
	}
	for key, value := range overrides {
		t.Setenv(key, value)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": validSecret})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.AppPort != "8080" || cfg.MetricsPort != "9091" || cfg.AppEnv != "local" {
		t.Errorf("defaults = %+v", cfg)
	}
	if cfg.TrustProxyHeaders {
		t.Error("proxy headers are trusted by default")
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		contains string
	}{
		{"missing database url", map[string]string{"JWT_SECRET": validSecret}, "DATABASE_URL"},
		{"missing secret", map[string]string{"DATABASE_URL": "postgres://x"}, "JWT_SECRET is required"},
		{"short secret", map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": "short"}, "at least 32"},
		{"bad proxy flag", map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": validSecret, "TRUST_PROXY_HEADERS": "maybe"}, "TRUST_PROXY_HEADERS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("err = %v, want one mentioning %q", err, tt.contains)
			}
		})
	}
}

func TestLoadTrustProxyHeaders(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": validSecret, "TRUST_PROXY_HEADERS": "true"})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.TrustProxyHeaders {
		t.Error("TRUST_PROXY_HEADERS=true was ignored")
	}
}
