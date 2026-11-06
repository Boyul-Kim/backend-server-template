package app

import (
	"slices"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/boyulkim/api-template/internal/config"
	"github.com/boyulkim/api-template/internal/testutil"
)

func TestServeArgs(t *testing.T) {
	cfg := config.Config{HTTPAddr: "0.0.0.0:8090", CORSOrigins: "https://a.com"}

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"no args defaults to serve", nil, []string{"serve", "--http=0.0.0.0:8090", "--origins=https://a.com"}},
		{"explicit flags win", []string{"serve", "--http=:9000", "--origins", "*"}, []string{"serve", "--http=:9000", "--origins", "*"}},
		{"other commands untouched", []string{"migrate", "up"}, []string{"migrate", "up"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ServeArgs(tc.in, cfg); !slices.Equal(got, tc.want) {
				t.Errorf("ServeArgs(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestApplySettings(t *testing.T) {
	app := testutil.NewApp(t)
	defer app.Cleanup()

	cfg := config.Config{
		AppName:             "My API",
		AppURL:              "https://api.example.com",
		RateLimitEnabled:    true,
		HideControls:        true,
		TrustedProxyHeaders: []string{"X-Forwarded-For"},
		SMTP:                config.SMTP{Host: "smtp.example.com", Port: 465, TLS: true, SenderAddress: "no-reply@example.com"},
	}
	if err := applySettings(app, cfg); err != nil {
		t.Fatal(err)
	}

	if err := app.ReloadSettings(); err != nil {
		t.Fatal(err)
	}
	s := app.Settings()
	if s.Meta.AppName != "My API" || s.Meta.AppURL != "https://api.example.com" || !s.Meta.HideControls {
		t.Errorf("meta not applied: %+v", s.Meta)
	}
	if !s.SMTP.Enabled || s.SMTP.Port != 465 || s.Meta.SenderAddress != "no-reply@example.com" {
		t.Errorf("smtp not applied: %+v", s.SMTP)
	}
	if !s.RateLimits.Enabled || !slices.Equal(s.TrustedProxy.Headers, []string{"X-Forwarded-For"}) {
		t.Error("rate limit / trusted proxy not applied")
	}
	if s.S3.Enabled {
		t.Error("S3 must stay disabled when S3_BUCKET is unset")
	}
	if s.TrustedProxy.UseLeftmostIP {
		t.Error("UseLeftmostIP should stay false")
	}
}

func TestSecurityListsFollowEnv(t *testing.T) {
	pb := testutil.NewApp(t)
	defer pb.Cleanup()

	s := pb.Settings()
	s.TrustedProxy.Headers = []string{"X-Forwarded-For"}
	s.TrustedProxy.UseLeftmostIP = true
	s.RateLimits.ExcludedIPs = []string{"203.0.113.5"}
	s.SuperuserIPs = []string{"198.51.100.10"}
	s.SMTP.Enabled = true
	if err := pb.Save(s); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{TrustedProxyNone: true, SMTPDisabled: true}
	if err := applySettings(pb, cfg); err != nil {
		t.Fatal(err)
	}
	if err := pb.ReloadSettings(); err != nil {
		t.Fatal(err)
	}

	got := pb.Settings()
	if len(got.TrustedProxy.Headers) != 0 || got.TrustedProxy.UseLeftmostIP {
		t.Fatalf("TRUSTED_PROXY_HEADERS=none did not clear stored proxy: %+v", got.TrustedProxy)
	}
	if len(got.RateLimits.ExcludedIPs) != 0 {
		t.Fatalf("excluded IPs = %v, want empty when unset", got.RateLimits.ExcludedIPs)
	}
	if len(got.SuperuserIPs) != 0 {
		t.Fatalf("superuser IPs = %v, want empty when unset", got.SuperuserIPs)
	}
	if got.SMTP.Enabled {
		t.Fatal("SMTP_DISABLED did not turn SMTP off")
	}
}

func TestEnsureSuperuser(t *testing.T) {
	app := testutil.NewApp(t)
	defer app.Cleanup()

	count := func() int64 {
		n, err := app.CountRecords(core.CollectionNameSuperusers)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := ensureSuperuser(app, config.Config{}); err != nil || count() != 0 {
		t.Fatalf("unconfigured: err=%v count=%d", err, count())
	}

	cfg := config.Config{SuperuserEmail: "admin@example.com", SuperuserPassword: "supersecret123"}
	if err := ensureSuperuser(app, cfg); err != nil {
		t.Fatal(err)
	}
	su, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, cfg.SuperuserEmail)
	if err != nil || !su.ValidatePassword(cfg.SuperuserPassword) {
		t.Fatalf("superuser not created correctly: %v", err)
	}

	// Idempotent: a second start neither duplicates nor changes the account.
	cfg.SuperuserPassword = "changedpassword1"
	if err := ensureSuperuser(app, cfg); err != nil || count() != 1 {
		t.Fatalf("second run: err=%v count=%d", err, count())
	}
}
