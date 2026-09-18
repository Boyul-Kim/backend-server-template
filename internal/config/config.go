// Package config loads the service configuration from environment variables.
//
// Every value has a safe default for local development. In production the
// loader fails fast on missing or insecure values instead of starting a
// half-configured server.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"

	// EncryptionKeyEnv is the env variable PocketBase reads to encrypt the
	// stored app settings (SMTP/S3 secrets, OAuth2 client secrets, etc.).
	EncryptionKeyEnv = "PB_ENCRYPTION_KEY"
)

type Config struct {
	Env      string // APP_ENV
	HTTPAddr string // HTTP_ADDR
	DataDir  string // PB_DATA_DIR, empty = PocketBase default
	// CORSOrigins is a comma separated list passed to `serve --origins`.
	CORSOrigins string // CORS_ORIGINS

	AppName string // APP_NAME
	AppURL  string // APP_URL

	// Bootstrap superuser created on first start if no superuser exists.
	SuperuserEmail    string // SUPERUSER_EMAIL
	SuperuserPassword string // SUPERUSER_PASSWORD

	SMTP    SMTP
	S3      S3
	Backups Backups

	RateLimitEnabled    bool     // RATE_LIMIT_ENABLED
	TrustedProxyHeaders []string // TRUSTED_PROXY_HEADERS
	LogsMaxDays         int      // LOGS_MAX_DAYS
	// HideControls hides the collection editing controls in the dashboard so
	// the schema can only change through migrations.
	HideControls bool // HIDE_CONTROLS
}

type SMTP struct {
	Host          string // SMTP_HOST, empty = SMTP disabled
	Port          int    // SMTP_PORT
	Username      string // SMTP_USERNAME
	Password      string // SMTP_PASSWORD
	TLS           bool   // SMTP_TLS
	SenderName    string // SMTP_SENDER_NAME
	SenderAddress string // SMTP_SENDER_ADDRESS
}

type S3 struct {
	Bucket         string // S3_BUCKET, empty = local file storage
	Region         string // S3_REGION
	Endpoint       string // S3_ENDPOINT
	AccessKey      string // S3_ACCESS_KEY
	Secret         string // S3_SECRET
	ForcePathStyle bool   // S3_FORCE_PATH_STYLE
}

type Backups struct {
	Cron    string // BACKUPS_CRON, empty = no scheduled backups
	MaxKeep int    // BACKUPS_MAX_KEEP
}

func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// Load reads the configuration from the process environment.
func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	e := envReader{lookup: lookup}

	cfg := Config{
		Env:         e.str("APP_ENV", EnvDevelopment),
		HTTPAddr:    e.str("HTTP_ADDR", "127.0.0.1:8090"),
		DataDir:     e.str("PB_DATA_DIR", ""),
		CORSOrigins: e.str("CORS_ORIGINS", ""),

		AppName: e.str("APP_NAME", ""),
		AppURL:  e.str("APP_URL", ""),

		SuperuserEmail:    e.str("SUPERUSER_EMAIL", ""),
		SuperuserPassword: e.str("SUPERUSER_PASSWORD", ""),

		SMTP: SMTP{
			Host:          e.str("SMTP_HOST", ""),
			Port:          e.int("SMTP_PORT", 587),
			Username:      e.str("SMTP_USERNAME", ""),
			Password:      e.str("SMTP_PASSWORD", ""),
			TLS:           e.bool("SMTP_TLS", false),
			SenderName:    e.str("SMTP_SENDER_NAME", ""),
			SenderAddress: e.str("SMTP_SENDER_ADDRESS", ""),
		},
		S3: S3{
			Bucket:         e.str("S3_BUCKET", ""),
			Region:         e.str("S3_REGION", ""),
			Endpoint:       e.str("S3_ENDPOINT", ""),
			AccessKey:      e.str("S3_ACCESS_KEY", ""),
			Secret:         e.str("S3_SECRET", ""),
			ForcePathStyle: e.bool("S3_FORCE_PATH_STYLE", false),
		},
		Backups: Backups{
			Cron:    e.str("BACKUPS_CRON", ""),
			MaxKeep: e.int("BACKUPS_MAX_KEEP", 7),
		},

		TrustedProxyHeaders: e.list("TRUSTED_PROXY_HEADERS"),
		LogsMaxDays:         e.int("LOGS_MAX_DAYS", 7),
	}

	// Secure-by-default toggles that stay relaxed in development.
	prod := cfg.IsProduction()
	cfg.RateLimitEnabled = e.bool("RATE_LIMIT_ENABLED", prod)
	cfg.HideControls = e.bool("HIDE_CONTROLS", prod)

	if err := errors.Join(append(e.errs, cfg.validate(lookup))...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate(lookup func(string) (string, bool)) error {
	var errs []error

	if c.Env != EnvDevelopment && c.Env != EnvProduction {
		errs = append(errs, fmt.Errorf("APP_ENV must be %q or %q, got %q", EnvDevelopment, EnvProduction, c.Env))
	}

	if c.AppURL != "" {
		if u, err := url.Parse(c.AppURL); err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("APP_URL must be an absolute URL, got %q", c.AppURL))
		}
	}

	if (c.SuperuserEmail == "") != (c.SuperuserPassword == "") {
		errs = append(errs, errors.New("SUPERUSER_EMAIL and SUPERUSER_PASSWORD must be set together"))
	}
	if c.SuperuserPassword != "" && len(c.SuperuserPassword) < 10 {
		errs = append(errs, errors.New("SUPERUSER_PASSWORD must be at least 10 characters"))
	}

	if c.SMTP.Host != "" && c.SMTP.SenderAddress == "" {
		errs = append(errs, errors.New("SMTP_SENDER_ADDRESS is required when SMTP_HOST is set"))
	}

	if c.S3.Bucket != "" && (c.S3.Region == "" || c.S3.Endpoint == "" || c.S3.AccessKey == "" || c.S3.Secret == "") {
		errs = append(errs, errors.New("S3_REGION, S3_ENDPOINT, S3_ACCESS_KEY and S3_SECRET are required when S3_BUCKET is set"))
	}

	if c.IsProduction() {
		if c.AppURL == "" {
			errs = append(errs, errors.New("APP_URL is required in production"))
		}
		if key, _ := lookup(EncryptionKeyEnv); len(key) != 32 {
			problem := fmt.Sprintf("is %d characters", len(key))
			if key == "" {
				problem = "is not set"
			}
			errs = append(errs, fmt.Errorf("%s %s; production requires exactly 32 characters (generate one with `openssl rand -hex 16`)", EncryptionKeyEnv, problem))
		}
		if c.CORSOrigins == "" || c.CORSOrigins == "*" {
			errs = append(errs, errors.New("CORS_ORIGINS must list explicit origins in production"))
		}
	}

	return errors.Join(errs...)
}

// envReader collects parse errors so all of them are reported at once.
type envReader struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (e *envReader) str(key, def string) string {
	if v, ok := e.lookup(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (e *envReader) int(key string, def int) int {
	raw := e.str(key, "")
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: invalid integer %q", key, raw))
		return def
	}
	return v
}

func (e *envReader) bool(key string, def bool) bool {
	raw := e.str(key, "")
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: invalid boolean %q", key, raw))
		return def
	}
	return v
}

func (e *envReader) list(key string) []string {
	var out []string
	for part := range strings.SplitSeq(e.str(key, ""), ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
