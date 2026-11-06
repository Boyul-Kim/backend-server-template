package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// validEncryptionKey is 32 hex characters with 16 distinct digits, the shape
// `openssl rand -hex 16` produces.
const validEncryptionKey = "4f3c1a9e7b2d8c65e0a1f4b79c2d6e83"

func validProductionEnv() map[string]string {
	return map[string]string{
		"APP_ENV":               EnvProduction,
		"APP_URL":               "https://api.example.com",
		"CORS_ORIGINS":          "https://example.com",
		EncryptionKeyEnv:        validEncryptionKey,
		"TRUSTED_PROXY_HEADERS": "X-Forwarded-For",
		"SMTP_HOST":             "smtp.example.com",
		"SMTP_SENDER_ADDRESS":   "no-reply@example.com",
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
	if cfg.SMTP.TLS {
		t.Error("SMTP_TLS should default to off on port 587")
	}
	if cfg.SMTP.Port != 587 {
		t.Errorf("SMTP port = %d, want 587", cfg.SMTP.Port)
	}
}

func TestLoadProductionRequiresSecureValues(t *testing.T) {
	_, err := load(lookupFrom(map[string]string{"APP_ENV": "production"}))
	if err == nil {
		t.Fatal("expected an error for an unconfigured production env")
	}
	for _, want := range []string{"APP_URL", EncryptionKeyEnv, "CORS_ORIGINS", "TRUSTED_PROXY_HEADERS", "SMTP_HOST"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadProduction(t *testing.T) {
	if len(validEncryptionKey) != 32 {
		t.Fatalf("fixture key length = %d, want 32", len(validEncryptionKey))
	}
	cfg, err := load(lookupFrom(validProductionEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.RateLimitEnabled || !cfg.HideControls {
		t.Error("rate limiting and hidden controls should default to on in production")
	}
	if cfg.encryptionKey != validEncryptionKey {
		t.Errorf("encryption key = %q", cfg.encryptionKey)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", cfg.Warnings)
	}
}

func TestLoadProductionTrustedProxyList(t *testing.T) {
	env := validProductionEnv()
	env["TRUSTED_PROXY_HEADERS"] = "X-Forwarded-For, ,X-Real-IP"
	cfg, err := load(lookupFrom(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Join(cfg.TrustedProxyHeaders, "|"); got != "X-Forwarded-For|X-Real-IP" {
		t.Errorf("TrustedProxyHeaders = %q", got)
	}
	if cfg.TrustedProxyNone {
		t.Error("TrustedProxyNone should be false")
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

func TestProductionRejectsInsecureConfig(t *testing.T) {
	exampleKey := encryptionKeyFromExample(t)
	if exampleKey != exampleEncryptionKey {
		t.Fatalf(".env.example PB_ENCRYPTION_KEY = %q, want %q", exampleKey, exampleEncryptionKey)
	}

	cases := []struct {
		name  string
		tweak func(map[string]string)
		want  string
	}{
		{
			name:  "http APP_URL",
			tweak: func(m map[string]string) { m["APP_URL"] = "http://api.example.com" },
			want:  "https://",
		},
		{
			name:  "RATE_LIMIT_ENABLED=false without override",
			tweak: func(m map[string]string) { m["RATE_LIMIT_ENABLED"] = "false" },
			want:  "RATE_LIMIT_ENABLED",
		},
		{
			name:  "HIDE_CONTROLS=false without override",
			tweak: func(m map[string]string) { m["HIDE_CONTROLS"] = "false" },
			want:  "HIDE_CONTROLS",
		},
		{
			name:  "repeated encryption key",
			tweak: func(m map[string]string) { m[EncryptionKeyEnv] = strings.Repeat("a", 32) },
			want:  "repeated",
		},
		{
			name: "encryption key with fewer than 8 distinct characters",
			tweak: func(m map[string]string) {
				m[EncryptionKeyEnv] = strings.Repeat("abcdefg", 4) + "abcd"
			},
			want: "distinct",
		},
		{
			name:  "encryption key from .env.example",
			tweak: func(m map[string]string) { m[EncryptionKeyEnv] = exampleKey },
			want:  "example",
		},
		{
			name:  "missing TRUSTED_PROXY_HEADERS",
			tweak: func(m map[string]string) { delete(m, "TRUSTED_PROXY_HEADERS") },
			want:  "TRUSTED_PROXY_HEADERS",
		},
		{
			name:  "false is not a trusted proxy header",
			tweak: func(m map[string]string) { m["TRUSTED_PROXY_HEADERS"] = "false" },
			want:  "boolean",
		},
		{
			name:  "none mixed with a header",
			tweak: func(m map[string]string) { m["TRUSTED_PROXY_HEADERS"] = "none,X-Forwarded-For" },
			want:  "cannot be combined",
		},
		{
			name:  "header name with whitespace",
			tweak: func(m map[string]string) { m["TRUSTED_PROXY_HEADERS"] = "X Forwarded For" },
			want:  "not a valid header name",
		},
		{
			name:  "CORS origin with a path",
			tweak: func(m map[string]string) { m["CORS_ORIGINS"] = "https://example.com/app" },
			want:  "path",
		},
		{
			name:  "http CORS origin",
			tweak: func(m map[string]string) { m["CORS_ORIGINS"] = "http://example.com" },
			want:  "https://",
		},
		{
			name: "missing SMTP_HOST",
			tweak: func(m map[string]string) {
				delete(m, "SMTP_HOST")
				delete(m, "SMTP_SENDER_ADDRESS")
			},
			want: "SMTP_HOST",
		},
		{
			name: "SMTP_DISABLED without the escape hatch",
			tweak: func(m map[string]string) {
				delete(m, "SMTP_HOST")
				delete(m, "SMTP_SENDER_ADDRESS")
				m["SMTP_DISABLED"] = "true"
			},
			want: "SMTP_DISABLED",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := validProductionEnv()
			tc.tweak(env)
			_, err := load(lookupFrom(env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestProductionAllowsExplicitEscapeHatches(t *testing.T) {
	t.Run("insecure http", func(t *testing.T) {
		env := validProductionEnv()
		env["ALLOW_INSECURE_HTTP"] = "true"
		env["APP_URL"] = "http://api.internal:8090"
		env["CORS_ORIGINS"] = "http://app.internal:3000"
		cfg, err := load(lookupFrom(env))
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.AllowInsecureHTTP || len(cfg.Warnings) == 0 || !strings.Contains(cfg.Warnings[0], "ALLOW_INSECURE_HTTP") {
			t.Fatalf("warnings = %v", cfg.Warnings)
		}
	})

	t.Run("insecure overrides", func(t *testing.T) {
		env := validProductionEnv()
		env["ALLOW_INSECURE_OVERRIDES"] = "true"
		env["RATE_LIMIT_ENABLED"] = "false"
		env["HIDE_CONTROLS"] = "false"
		cfg, err := load(lookupFrom(env))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.RateLimitEnabled || cfg.HideControls {
			t.Fatalf("toggles were not applied: rate=%v hide=%v", cfg.RateLimitEnabled, cfg.HideControls)
		}
		if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "ALLOW_INSECURE_OVERRIDES") {
			t.Fatalf("warnings = %v", cfg.Warnings)
		}
	})

	t.Run("smtp disabled", func(t *testing.T) {
		env := validProductionEnv()
		delete(env, "SMTP_HOST")
		delete(env, "SMTP_SENDER_ADDRESS")
		env["SMTP_DISABLED"] = "true"
		env["AUTH_SIGNUP"] = "closed"
		env["SUPERUSER_MFA"] = "false"
		env["SUPERUSER_IPS"] = "10.0.0.0/8, 192.0.2.10"
		cfg, err := load(lookupFrom(env))
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.SMTPDisabled || !cfg.SuperuserMFADisabled || cfg.AuthSignup != "closed" {
			t.Fatalf("escape hatch not recorded: %+v", cfg)
		}
		if len(cfg.SuperuserIPs) != 2 {
			t.Fatalf("superuser IPs = %v", cfg.SuperuserIPs)
		}
		if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "SMTP_DISABLED") {
			t.Fatalf("warnings = %v", cfg.Warnings)
		}
	})

	t.Run("trusted proxy none", func(t *testing.T) {
		for _, raw := range []string{"none", "NONE", "None"} {
			env := validProductionEnv()
			env["TRUSTED_PROXY_HEADERS"] = raw
			cfg, err := load(lookupFrom(env))
			if err != nil {
				t.Fatalf("%s: %v", raw, err)
			}
			if !cfg.TrustedProxyNone || len(cfg.TrustedProxyHeaders) != 0 {
				t.Fatalf("%s: none = %v headers = %v", raw, cfg.TrustedProxyNone, cfg.TrustedProxyHeaders)
			}
		}
	})
}

func TestSMTPTLSDefaultsByPort(t *testing.T) {
	cases := []struct {
		port string
		tls  string
		want bool
	}{
		{port: "465", want: true},
		{port: "587", want: false},
		{port: "25", want: false},
		{port: "465", tls: "false", want: false},
		{port: "587", tls: "true", want: true},
	}
	for _, tc := range cases {
		name := "port " + tc.port
		if tc.tls != "" {
			name += " tls " + tc.tls
		}
		t.Run(name, func(t *testing.T) {
			env := map[string]string{
				"SMTP_HOST":           "smtp.example.com",
				"SMTP_PORT":           tc.port,
				"SMTP_SENDER_ADDRESS": "no-reply@example.com",
			}
			if tc.tls != "" {
				env["SMTP_TLS"] = tc.tls
			}
			cfg, err := load(lookupFrom(env))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.SMTP.TLS != tc.want {
				t.Fatalf("TLS = %v, want %v", cfg.SMTP.TLS, tc.want)
			}
		})
	}
}

func TestSecretFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	if err := os.WriteFile(keyPath, []byte(validEncryptionKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	passPath := filepath.Join(dir, "smtp")
	if err := os.WriteFile(passPath, []byte("smtp-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env := validProductionEnv()
	delete(env, EncryptionKeyEnv)
	env[EncryptionKeyEnv+"_FILE"] = keyPath
	env["SMTP_PASSWORD_FILE"] = passPath

	cfg, err := load(lookupFrom(env))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.encryptionKeyFromFile || cfg.encryptionKey != validEncryptionKey {
		t.Fatalf("key = %q fromFile=%v", cfg.encryptionKey, cfg.encryptionKeyFromFile)
	}
	if cfg.SMTP.Password != "smtp-secret" {
		t.Fatalf("smtp password = %q", cfg.SMTP.Password)
	}

	env[EncryptionKeyEnv] = validEncryptionKey
	_, err = load(lookupFrom(env))
	if err == nil || !strings.Contains(err.Error(), "both set") {
		t.Fatalf("error = %v, want both set", err)
	}
}

func TestExportEncryptionKeyFromFile(t *testing.T) {
	t.Setenv(EncryptionKeyEnv, "")
	cfg := Config{encryptionKey: validEncryptionKey, encryptionKeyFromFile: true}
	if err := exportEncryptionKey(cfg); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(EncryptionKeyEnv); got != validEncryptionKey {
		t.Fatalf("%s = %q", EncryptionKeyEnv, got)
	}
}

func encryptionKeyFromExample(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, EncryptionKeyEnv+"="); ok {
			return after
		}
	}
	t.Fatal("PB_ENCRYPTION_KEY not found in .env.example")
	return ""
}
