package config

import (
	"strings"
	"testing"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(lookupFrom(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != EnvDevelopment {
		t.Errorf("Env = %q, want %q", cfg.Env, EnvDevelopment)
	}
	if cfg.RateLimitEnabled || cfg.HideControls {
		t.Error("rate limiting and hidden controls should default to off in development")
	}
}

func TestLoadProductionRequiresSecureValues(t *testing.T) {
	_, err := load(lookupFrom(map[string]string{"APP_ENV": "production"}))
	if err == nil {
		t.Fatal("expected an error for an unconfigured production env")
	}
	for _, want := range []string{"APP_URL", EncryptionKeyEnv, "CORS_ORIGINS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadProduction(t *testing.T) {
	cfg, err := load(lookupFrom(map[string]string{
		"APP_ENV":               "production",
		"APP_URL":               "https://api.example.com",
		"CORS_ORIGINS":          "https://example.com",
		EncryptionKeyEnv:        strings.Repeat("a", 32),
		"TRUSTED_PROXY_HEADERS": "X-Forwarded-For, ,X-Real-IP",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.RateLimitEnabled || !cfg.HideControls {
		t.Error("rate limiting and hidden controls should default to on in production")
	}
	if got := strings.Join(cfg.TrustedProxyHeaders, "|"); got != "X-Forwarded-For|X-Real-IP" {
		t.Errorf("TrustedProxyHeaders = %q", got)
	}
}

func TestLoadInvalidValues(t *testing.T) {
	_, err := load(lookupFrom(map[string]string{
		"APP_ENV":         "staging",
		"SMTP_PORT":       "abc",
		"SUPERUSER_EMAIL": "admin@example.com",
	}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"SMTP_PORT", "APP_ENV", "SUPERUSER_PASSWORD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}
