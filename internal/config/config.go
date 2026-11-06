// Package config loads the service configuration from environment variables.
//
// Every value has a safe default for local development. In production the
// loader fails fast on missing or insecure values instead of starting a
// half-configured server.
package config

import (
	"errors"
	"fmt"
	"net/netip"
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

	// exampleEncryptionKey is the placeholder shipped in .env.example.
	// Production rejects it. Keep the two in sync.
	exampleEncryptionKey = "0123456789abcdef0123456789abcdef"
)

// exampleEncryptionKeys are documentation and test-fixture values that must
// never encrypt production settings.
var exampleEncryptionKeys = map[string]struct{}{
	exampleEncryptionKey:               {},
	"0123456789ABCDEF0123456789ABCDEF": {},
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": {},
}

type Config struct {
	Env      string // APP_ENV
	HTTPAddr string // HTTP_ADDR
	DataDir  string // PB_DATA_DIR, empty = PocketBase default
	// CORSOrigins is a comma separated list passed to `serve --origins`.
	CORSOrigins string // CORS_ORIGINS

	AppName string // APP_NAME
	AppURL  string // APP_URL

	// AllowInsecureHTTP permits http:// APP_URL and CORS origins in production.
	AllowInsecureHTTP bool // ALLOW_INSECURE_HTTP
	// AllowInsecureOverrides permits RATE_LIMIT_ENABLED=false and HIDE_CONTROLS=false.
	AllowInsecureOverrides bool // ALLOW_INSECURE_OVERRIDES

	// Bootstrap superuser created on first start if no superuser exists.
	SuperuserEmail    string // SUPERUSER_EMAIL
	SuperuserPassword string // SUPERUSER_PASSWORD or SUPERUSER_PASSWORD_FILE

	// SuperuserIPs is applied to PocketBase on every boot and cleared when unset.
	SuperuserIPs []string // SUPERUSER_IPS
	// AuthSignup is AUTH_SIGNUP (closed, invite, or open). Empty when unset.
	// Signup policy is applied by a later change; this is read so the SMTP
	// escape hatch can require closed.
	AuthSignup string
	// SuperuserMFADisabled is true only when SUPERUSER_MFA=false.
	// MFA enrollment is a later change; this flag is the SMTP escape hatch.
	SuperuserMFADisabled bool

	SMTP    SMTP
	S3      S3
	Backups Backups

	// SMTPDisabled is the production escape hatch for a missing SMTP_HOST.
	SMTPDisabled bool // SMTP_DISABLED

	RateLimitEnabled     bool     // RATE_LIMIT_ENABLED
	RateLimitExcludedIPs []string // RATE_LIMIT_EXCLUDED_IPS
	// TrustedProxyNone is true when TRUSTED_PROXY_HEADERS=none.
	TrustedProxyNone    bool
	TrustedProxyHeaders []string // TRUSTED_PROXY_HEADERS
	LogsMaxDays         int      // LOGS_MAX_DAYS
	// HideControls hides the collection editing controls in the dashboard so
	// the schema can only change through migrations.
	HideControls bool // HIDE_CONTROLS

	// Warnings are non-fatal production escape hatches that callers should log.
	Warnings []string

	encryptionKey           string
	encryptionKeyFromFile   bool
	rateLimitExplicitOff    bool
	hideControlsExplicitOff bool
}

type SMTP struct {
	Host          string // SMTP_HOST, empty = SMTP disabled
	Port          int    // SMTP_PORT
	Username      string // SMTP_USERNAME
	Password      string // SMTP_PASSWORD or SMTP_PASSWORD_FILE
	TLS           bool   // SMTP_TLS, default true on port 465 and false otherwise
	SenderName    string // SMTP_SENDER_NAME
	SenderAddress string // SMTP_SENDER_ADDRESS
}

type S3 struct {
	Bucket         string // S3_BUCKET, empty = local file storage
	Region         string // S3_REGION
	Endpoint       string // S3_ENDPOINT
	AccessKey      string // S3_ACCESS_KEY
	Secret         string // S3_SECRET or S3_SECRET_FILE
	ForcePathStyle bool   // S3_FORCE_PATH_STYLE
}

type Backups struct {
	Cron     string // BACKUPS_CRON, empty = no scheduled backups
	MaxKeep  int    // BACKUPS_MAX_KEEP
	S3Secret string // BACKUPS_S3_SECRET or BACKUPS_S3_SECRET_FILE
}

func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// Load reads the configuration from the process environment.
//
// When PB_ENCRYPTION_KEY_FILE is used, the file contents are copied into
// PB_ENCRYPTION_KEY so PocketBase, which reads that variable itself, can
// encrypt stored settings.
func Load() (Config, error) {
	cfg, err := load(os.LookupEnv)
	if err != nil {
		return Config{}, err
	}
	if err := exportEncryptionKey(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func exportEncryptionKey(cfg Config) error {
	if !cfg.encryptionKeyFromFile {
		return nil
	}
	if err := os.Setenv(EncryptionKeyEnv, cfg.encryptionKey); err != nil {
		return fmt.Errorf("set %s from file: %w", EncryptionKeyEnv, err)
	}
	return nil
}

func load(lookup func(string) (string, bool)) (Config, error) {
	e := envReader{lookup: lookup}

	prodDefault := e.str("APP_ENV", EnvDevelopment) == EnvProduction

	cfg := Config{
		Env:         e.str("APP_ENV", EnvDevelopment),
		HTTPAddr:    e.str("HTTP_ADDR", "127.0.0.1:8090"),
		DataDir:     e.str("PB_DATA_DIR", ""),
		CORSOrigins: e.str("CORS_ORIGINS", ""),

		AppName: e.str("APP_NAME", ""),
		AppURL:  e.str("APP_URL", ""),

		AllowInsecureHTTP:      e.bool("ALLOW_INSECURE_HTTP", false),
		AllowInsecureOverrides: e.bool("ALLOW_INSECURE_OVERRIDES", false),

		SuperuserEmail:       e.str("SUPERUSER_EMAIL", ""),
		SuperuserPassword:    e.secret("SUPERUSER_PASSWORD"),
		SuperuserIPs:         e.list("SUPERUSER_IPS"),
		AuthSignup:           e.str("AUTH_SIGNUP", ""),
		SuperuserMFADisabled: e.explicitFalse("SUPERUSER_MFA"),

		S3: S3{
			Bucket:         e.str("S3_BUCKET", ""),
			Region:         e.str("S3_REGION", ""),
			Endpoint:       e.str("S3_ENDPOINT", ""),
			AccessKey:      e.str("S3_ACCESS_KEY", ""),
			Secret:         e.secret("S3_SECRET"),
			ForcePathStyle: e.bool("S3_FORCE_PATH_STYLE", false),
		},
		Backups: Backups{
			Cron:     e.str("BACKUPS_CRON", ""),
			MaxKeep:  e.int("BACKUPS_MAX_KEEP", 7),
			S3Secret: e.secret("BACKUPS_S3_SECRET"),
		},

		LogsMaxDays: e.int("LOGS_MAX_DAYS", 7),
	}

	cfg.encryptionKey = e.secret(EncryptionKeyEnv)
	cfg.encryptionKeyFromFile = e.fromFile[EncryptionKeyEnv]

	cfg.SMTPDisabled = e.bool("SMTP_DISABLED", false)
	smtpPort := e.int("SMTP_PORT", 587)
	cfg.SMTP = SMTP{
		Host:          e.str("SMTP_HOST", ""),
		Port:          smtpPort,
		Username:      e.str("SMTP_USERNAME", ""),
		Password:      e.secret("SMTP_PASSWORD"),
		TLS:           e.bool("SMTP_TLS", smtpPort == 465),
		SenderName:    e.str("SMTP_SENDER_NAME", ""),
		SenderAddress: e.str("SMTP_SENDER_ADDRESS", ""),
	}

	proxy := e.str("TRUSTED_PROXY_HEADERS", "")
	if proxy == "none" {
		cfg.TrustedProxyNone = true
	} else {
		cfg.TrustedProxyHeaders = splitComma(proxy)
	}

	cfg.RateLimitExcludedIPs = e.list("RATE_LIMIT_EXCLUDED_IPS")

	// Secure-by-default toggles that stay relaxed in development.
	cfg.RateLimitEnabled = e.bool("RATE_LIMIT_ENABLED", prodDefault)
	cfg.HideControls = e.bool("HIDE_CONTROLS", prodDefault)
	cfg.rateLimitExplicitOff = e.explicitFalse("RATE_LIMIT_ENABLED")
	cfg.hideControlsExplicitOff = e.explicitFalse("HIDE_CONTROLS")

	warns, verr := cfg.validate()
	cfg.Warnings = warns
	if err := errors.Join(append(e.errs, verr)...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() ([]string, error) {
	var errs []error
	var warns []string

	if c.Env != EnvDevelopment && c.Env != EnvProduction {
		errs = append(errs, fmt.Errorf("APP_ENV must be %q or %q, got %q", EnvDevelopment, EnvProduction, c.Env))
	}

	var appURL *url.URL
	if c.AppURL != "" {
		u, err := url.Parse(c.AppURL)
		if err != nil || u.Scheme == "" || u.Host == "" || u.Opaque != "" {
			errs = append(errs, fmt.Errorf("APP_URL must be an absolute URL, got %q", c.AppURL))
		} else {
			appURL = u
		}
	}

	switch c.AuthSignup {
	case "", "closed", "invite", "open":
	default:
		errs = append(errs, fmt.Errorf("AUTH_SIGNUP must be closed, invite, or open, got %q", c.AuthSignup))
	}

	if (c.SuperuserEmail == "") != (c.SuperuserPassword == "") {
		errs = append(errs, errors.New("SUPERUSER_EMAIL and SUPERUSER_PASSWORD must be set together"))
	}
	if c.SuperuserPassword != "" && len(c.SuperuserPassword) < 10 {
		errs = append(errs, errors.New("SUPERUSER_PASSWORD must be at least 10 characters"))
	}
	for _, ip := range c.SuperuserIPs {
		if !validIPOrCIDR(ip) {
			errs = append(errs, fmt.Errorf("SUPERUSER_IPS: invalid IP or CIDR %q", ip))
		}
	}
	for _, ip := range c.RateLimitExcludedIPs {
		if !validIPOrCIDR(ip) {
			errs = append(errs, fmt.Errorf("RATE_LIMIT_EXCLUDED_IPS: invalid IP or CIDR %q", ip))
		}
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
		} else if appURL != nil && !productionSchemeOK(appURL.Scheme, c.AllowInsecureHTTP) {
			errs = append(errs, fmt.Errorf("APP_URL must use https:// in production, got %q (set ALLOW_INSECURE_HTTP=true to allow http://)", c.AppURL))
		}
		if err := validateEncryptionKey(c.encryptionKey); err != nil {
			errs = append(errs, err)
		}
		if c.CORSOrigins == "" || c.CORSOrigins == "*" {
			errs = append(errs, errors.New("CORS_ORIGINS must list explicit origins in production"))
		} else {
			for _, origin := range splitComma(c.CORSOrigins) {
				if err := validateOrigin(origin, c.AllowInsecureHTTP); err != nil {
					errs = append(errs, fmt.Errorf("CORS_ORIGINS: %w", err))
				}
			}
		}
		if !c.TrustedProxyNone && len(c.TrustedProxyHeaders) == 0 {
			errs = append(errs, errors.New(`TRUSTED_PROXY_HEADERS is required in production (set header names or "none")`))
		}
		if c.rateLimitExplicitOff && !c.AllowInsecureOverrides {
			errs = append(errs, errors.New("RATE_LIMIT_ENABLED=false is not allowed in production (set ALLOW_INSECURE_OVERRIDES=true to override)"))
		}
		if c.hideControlsExplicitOff && !c.AllowInsecureOverrides {
			errs = append(errs, errors.New("HIDE_CONTROLS=false is not allowed in production (set ALLOW_INSECURE_OVERRIDES=true to override)"))
		}
		if c.SMTP.Host == "" && !c.SMTPDisabled {
			errs = append(errs, errors.New("SMTP_HOST is required in production (or set SMTP_DISABLED=true with AUTH_SIGNUP=closed, SUPERUSER_MFA=false, and SUPERUSER_IPS)"))
		}
		if c.SMTPDisabled {
			if c.SMTP.Host != "" {
				errs = append(errs, errors.New("SMTP_HOST cannot be set when SMTP_DISABLED=true"))
			}
			if c.AuthSignup != "closed" {
				errs = append(errs, errors.New("SMTP_DISABLED=true requires AUTH_SIGNUP=closed"))
			}
			if !c.SuperuserMFADisabled {
				errs = append(errs, errors.New("SMTP_DISABLED=true requires SUPERUSER_MFA=false"))
			}
			if len(c.SuperuserIPs) == 0 {
				errs = append(errs, errors.New("SMTP_DISABLED=true requires SUPERUSER_IPS"))
			}
		}

		if c.AllowInsecureHTTP {
			warns = append(warns, "ALLOW_INSECURE_HTTP=true: production allows http:// for APP_URL and CORS origins")
		}
		if c.AllowInsecureOverrides && (c.rateLimitExplicitOff || c.hideControlsExplicitOff) {
			var which []string
			if c.rateLimitExplicitOff {
				which = append(which, "RATE_LIMIT_ENABLED=false")
			}
			if c.hideControlsExplicitOff {
				which = append(which, "HIDE_CONTROLS=false")
			}
			warns = append(warns, "ALLOW_INSECURE_OVERRIDES=true: "+strings.Join(which, ", "))
		}
		if c.SMTPDisabled && c.SMTP.Host == "" && c.AuthSignup == "closed" && c.SuperuserMFADisabled && len(c.SuperuserIPs) > 0 {
			warns = append(warns, "SMTP_DISABLED=true: production SMTP is off (AUTH_SIGNUP=closed, SUPERUSER_MFA=false, SUPERUSER_IPS is set)")
		}
	}

	return warns, errors.Join(errs...)
}

func validateEncryptionKey(key string) error {
	if len(key) != 32 {
		problem := fmt.Sprintf("is %d characters", len(key))
		if key == "" {
			problem = "is not set"
		}
		return fmt.Errorf("%s %s; production requires exactly 32 characters (generate one with `openssl rand -hex 16`)", EncryptionKeyEnv, problem)
	}
	if repeatedRune(key) {
		return fmt.Errorf("%s is a single repeated character", EncryptionKeyEnv)
	}
	if distinctRunes(key) < 8 {
		return fmt.Errorf("%s has fewer than 8 distinct characters", EncryptionKeyEnv)
	}
	if _, ok := exampleEncryptionKeys[key]; ok {
		return fmt.Errorf("%s matches a known example or test value and cannot be used in production", EncryptionKeyEnv)
	}
	return nil
}

func validateOrigin(origin string, allowHTTP bool) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Opaque != "" {
		return fmt.Errorf("%q must be an absolute https:// origin with no path", origin)
	}
	if !productionSchemeOK(u.Scheme, allowHTTP) {
		return fmt.Errorf("%q must use https://", origin)
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("%q must not include a path", origin)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("%q must be an origin with no user info, query, or fragment", origin)
	}
	return nil
}

func productionSchemeOK(scheme string, allowHTTP bool) bool {
	scheme = strings.ToLower(scheme)
	if scheme == "https" {
		return true
	}
	return allowHTTP && scheme == "http"
}

func repeatedRune(s string) bool {
	runes := []rune(s)
	if len(runes) == 0 {
		return false
	}
	for _, r := range runes[1:] {
		if r != runes[0] {
			return false
		}
	}
	return true
}

func distinctRunes(s string) int {
	seen := make(map[rune]struct{}, len(s))
	for _, r := range s {
		seen[r] = struct{}{}
	}
	return len(seen)
}

func validIPOrCIDR(v string) bool {
	if _, err := netip.ParsePrefix(v); err == nil {
		return true
	}
	_, err := netip.ParseAddr(v)
	return err == nil
}

// envReader collects parse errors so all of them are reported at once.
type envReader struct {
	lookup   func(string) (string, bool)
	errs     []error
	fromFile map[string]bool
}

func (e *envReader) str(key, def string) string {
	if v, ok := e.lookup(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

// secret reads key, or key_FILE when that path is set. The file contents are
// trimmed. Setting both is an error.
func (e *envReader) secret(key string) string {
	fileKey := key + "_FILE"
	path, hasFile := e.lookup(fileKey)
	path = strings.TrimSpace(path)
	direct, hasDirect := e.lookup(key)
	direct = strings.TrimSpace(direct)

	if hasFile && path != "" {
		if hasDirect && direct != "" {
			e.errs = append(e.errs, fmt.Errorf("%s and %s are both set; set only one", key, fileKey))
			return ""
		}
		// Path comes from the operator's own environment (Docker/k8s secrets).
		b, err := os.ReadFile(path) //nolint:gosec // G304: intentional secret-file indirection
		if err != nil {
			e.errs = append(e.errs, fmt.Errorf("read %s: %w", fileKey, err))
			return ""
		}
		v := strings.TrimSpace(string(b))
		if v == "" {
			e.errs = append(e.errs, fmt.Errorf("%s is empty", fileKey))
			return ""
		}
		if e.fromFile == nil {
			e.fromFile = map[string]bool{}
		}
		e.fromFile[key] = true
		return v
	}
	return direct
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

// explicitFalse reports whether key is set to a false boolean.
func (e *envReader) explicitFalse(key string) bool {
	raw := e.str(key, "")
	if raw == "" {
		return false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return !v
}

func (e *envReader) list(key string) []string {
	return splitComma(e.str(key, ""))
}

func splitComma(v string) []string {
	var out []string
	for part := range strings.SplitSeq(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
