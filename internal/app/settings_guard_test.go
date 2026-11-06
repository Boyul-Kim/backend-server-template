package app

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/logger"

	"github.com/boyulkim/api-template/internal/config"
	"github.com/boyulkim/api-template/internal/testutil"
)

func productionConfig() config.Config {
	return config.Config{
		Env:                 config.EnvProduction,
		HTTPAddr:            "127.0.0.1:8090",
		AppName:             "My API",
		AppURL:              "https://api.example.com",
		CORSOrigins:         "https://example.com",
		RateLimitEnabled:    true,
		HideControls:        true,
		TrustedProxyHeaders: []string{"X-Forwarded-For"},
		SuperuserIPs:        []string{"10.0.0.0/8"},
		// Keep request logs off. PocketBase's activity logger reads settings
		// from another goroutine, which races with a settings reload. The
		// drift test turns retention on after boot, with no request in flight.
		LogsMaxDays: 0,
		SMTP: config.SMTP{
			Host:          "smtp.example.com",
			Port:          587,
			SenderAddress: "no-reply@example.com",
		},
		SuperuserEmail:    "admin@example.com",
		SuperuserPassword: "supersecret123",
	}
}

func TestProductionRejectsEnvOwnedSettingsUpdates(t *testing.T) {
	cfg := productionConfig()
	// httptest dials from 192.0.2.1. SuperuserIPs is enforced before the
	// settings handler, so the request has to originate inside that list.
	cfg.SuperuserIPs = []string{"192.0.2.1"}

	run := func(s tests.ApiScenario) {
		t.Helper()
		if s.Headers == nil {
			s.Headers = map[string]string{}
		}
		headers := s.Headers
		s.TestAppFactory = func(tb testing.TB) *tests.TestApp {
			testApp := testutil.NewApp(tb)
			Register(testApp, cfg)
			// Each app has its own token key, so the token has to be minted here.
			headers["Authorization"] = createSuperuser(tb, testApp, cfg.SuperuserEmail, cfg.SuperuserPassword)
			return testApp
		}
		s.Test(t)
	}

	run(tests.ApiScenario{
		Name:           "rateLimits",
		Method:         http.MethodPatch,
		URL:            "/api/settings",
		Body:           strings.NewReader(`{"rateLimits":{"enabled":false,"excludedIPs":["203.0.113.4"]}}`),
		ExpectedStatus: http.StatusForbidden,
		ExpectedContent: []string{
			"cannot be changed at runtime",
			"rateLimits",
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			if !app.Settings().RateLimits.Enabled || len(app.Settings().RateLimits.ExcludedIPs) != 0 {
				t.Fatalf("rate limits changed: %+v", app.Settings().RateLimits)
			}
			assertLogContains(t, app, auditUpdateRejected, "rateLimits")
		},
	})

	run(tests.ApiScenario{
		Name:           "trustedProxy",
		Method:         http.MethodPatch,
		URL:            "/api/settings",
		Body:           strings.NewReader(`{"trustedProxy":{"headers":["X-Evil"],"useLeftmostIP":true}}`),
		ExpectedStatus: http.StatusForbidden,
		ExpectedContent: []string{
			"cannot be changed at runtime",
			"trustedProxy",
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			s := app.Settings()
			if s.TrustedProxy.UseLeftmostIP || len(s.TrustedProxy.Headers) != 1 || s.TrustedProxy.Headers[0] != "X-Forwarded-For" {
				t.Fatalf("trusted proxy changed: %+v", s.TrustedProxy)
			}
			assertLogContains(t, app, auditUpdateRejected, "trustedProxy")
		},
	})

	run(tests.ApiScenario{
		Name:            "superuserIPs",
		Method:          http.MethodPatch,
		URL:             "/api/settings",
		Body:            strings.NewReader(`{"superuserIPs":["203.0.113.9"]}`),
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"cannot be changed at runtime", "superuserIPs"},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			if len(app.Settings().SuperuserIPs) != 1 || app.Settings().SuperuserIPs[0] != "192.0.2.1" {
				t.Fatalf("superuser IPs changed: %v", app.Settings().SuperuserIPs)
			}
			assertLogContains(t, app, auditUpdateRejected, "superuserIPs")
		},
	})

	run(tests.ApiScenario{
		Name:            "appURL",
		Method:          http.MethodPatch,
		URL:             "/api/settings",
		Body:            strings.NewReader(`{"meta":{"appURL":"http://attacker.example"}}`),
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"cannot be changed at runtime", "appURL"},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			if app.Settings().Meta.AppURL != "https://api.example.com" {
				t.Fatalf("app URL = %q", app.Settings().Meta.AppURL)
			}
			assertLogContains(t, app, auditUpdateRejected, "appURL")
		},
	})

	run(tests.ApiScenario{
		Name:           "sender identity",
		Method:         http.MethodPatch,
		URL:            "/api/settings",
		Body:           strings.NewReader(`{"meta":{"senderAddress":"attacker@evil.example","senderName":"Attacker"}}`),
		ExpectedStatus: http.StatusForbidden,
		ExpectedContent: []string{
			"cannot be changed at runtime",
			"senderAddress",
			"senderName",
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			if app.Settings().Meta.SenderAddress != "no-reply@example.com" || app.Settings().Meta.SenderName == "Attacker" {
				t.Fatalf("sender changed: name=%q address=%q", app.Settings().Meta.SenderName, app.Settings().Meta.SenderAddress)
			}
			assertLogContains(t, app, auditUpdateRejected, "senderAddress")
		},
	})

	run(tests.ApiScenario{
		Name:           "meta app name is still editable",
		Method:         http.MethodPatch,
		URL:            "/api/settings",
		Body:           strings.NewReader(`{"meta":{"appName":"Renamed"}}`),
		ExpectedStatus: http.StatusOK,
		ExpectedContent: []string{
			`"appName":"Renamed"`,
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			if app.Settings().Meta.AppName != "Renamed" {
				t.Fatalf("app name = %q", app.Settings().Meta.AppName)
			}
			if app.Settings().Meta.AppURL != "https://api.example.com" {
				t.Fatalf("app URL changed with the name: %q", app.Settings().Meta.AppURL)
			}
			if !app.Settings().Meta.HideControls {
				t.Fatal("hideControls was cleared")
			}
			flushLogs(t, app)
			type row struct {
				Message string `db:"message"`
			}
			var rows []row
			if err := app.AuxDB().NewQuery("SELECT [[message]] FROM `_logs` WHERE [[message]] = {:message}").
				Bind(dbx.Params{"message": auditDriftReverted}).All(&rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 0 {
				t.Fatalf("allowed settings update logged drift: %+v", rows)
			}
		},
	})
}

func TestDevelopmentAllowsSettingsUpdates(t *testing.T) {
	cfg := config.Config{Env: config.EnvDevelopment, HTTPAddr: "127.0.0.1:8090"}
	var token string
	scenario := tests.ApiScenario{
		Name:            "dev rate limit change",
		Method:          http.MethodPatch,
		URL:             "/api/settings",
		Body:            strings.NewReader(`{"rateLimits":{"enabled":true}}`),
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"enabled":true`},
		TestAppFactory: func(tb testing.TB) *tests.TestApp {
			testApp := testutil.NewApp(tb)
			Register(testApp, cfg)
			token = createSuperuser(tb, testApp, "admin@example.com", "supersecret123")
			return testApp
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			if !app.Settings().RateLimits.Enabled {
				t.Fatal("development should still allow rate limit changes")
			}
		},
	}
	scenario.Headers = map[string]string{}
	factory := scenario.TestAppFactory
	scenario.TestAppFactory = func(tb testing.TB) *tests.TestApp {
		testApp := factory(tb)
		scenario.Headers["Authorization"] = token
		return testApp
	}
	scenario.Test(t)
}

func TestSettingsDriftRevertedOnReload(t *testing.T) {
	testApp := testutil.NewApp(t)
	defer testApp.Cleanup()

	cfg := productionConfig()
	cfg.LogsMaxDays = 7
	Register(testApp, cfg)

	// Boot through OnServe without serving a request. A live request logs in
	// another goroutine, and that logger reads settings while reload writes them.
	router, err := apis.NewRouter(testApp)
	if err != nil {
		t.Fatal(err)
	}
	event := &core.ServeEvent{App: testApp, Router: router}
	if err := testApp.OnServe().Trigger(event, func(e *core.ServeEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}

	senderName := testApp.Settings().Meta.SenderName
	injectSettings(t, testApp, func(s *core.Settings) {
		s.RateLimits.Enabled = false
		s.RateLimits.ExcludedIPs = []string{"203.0.113.8"}
		s.TrustedProxy.Headers = []string{"X-Injected"}
		s.TrustedProxy.UseLeftmostIP = true
		s.SuperuserIPs = []string{"203.0.113.9"}
		s.Meta.AppURL = "http://attacker.example"
		s.Meta.SenderAddress = "attacker@evil.example"
		s.Meta.SenderName = "Attacker"
	})
	if err := testApp.ReloadSettings(); err != nil {
		t.Fatal(err)
	}

	got := testApp.Settings()
	if !got.RateLimits.Enabled || len(got.RateLimits.ExcludedIPs) != 0 {
		t.Fatalf("rate limits not reverted: %+v", got.RateLimits)
	}
	if got.TrustedProxy.UseLeftmostIP || len(got.TrustedProxy.Headers) != 1 || got.TrustedProxy.Headers[0] != "X-Forwarded-For" {
		t.Fatalf("trusted proxy not reverted: %+v", got.TrustedProxy)
	}
	if len(got.SuperuserIPs) != 1 || got.SuperuserIPs[0] != "10.0.0.0/8" {
		t.Fatalf("superuser IPs not reverted: %v", got.SuperuserIPs)
	}
	if got.Meta.AppURL != "https://api.example.com" {
		t.Fatalf("app URL not reverted: %q", got.Meta.AppURL)
	}
	if got.Meta.SenderAddress != "no-reply@example.com" || got.Meta.SenderName != senderName {
		t.Fatalf("sender not reverted: name=%q address=%q", got.Meta.SenderName, got.Meta.SenderAddress)
	}
	assertLogContains(t, testApp, auditDriftReverted, "appURL")
}

func createSuperuser(t testing.TB, app core.App, email, password string) string {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatal(err)
	}
	su := core.NewRecord(collection)
	su.SetEmail(email)
	su.SetPassword(password)
	if err := app.Save(su); err != nil {
		t.Fatal(err)
	}
	token, err := su.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func injectSettings(t testing.TB, app core.App, mutate func(*core.Settings)) {
	t.Helper()
	clone, err := app.Settings().Clone()
	if err != nil {
		t.Fatal(err)
	}
	mutate(clone)
	exported, err := clone.DBExport(app)
	if err != nil {
		t.Fatal(err)
	}
	res, err := app.DB().NewQuery("UPDATE `_params` SET [[value]] = {:value} WHERE [[id]] = {:id}").
		Bind(dbx.Params{"value": exported["value"], "id": "settings"}).Execute()
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("updated %d settings rows, want 1", n)
	}
}

func assertLogContains(t testing.TB, app core.App, message, extra string) {
	t.Helper()
	flushLogs(t, app)

	type row struct {
		Message string `db:"message"`
		Data    string `db:"data"`
	}
	var rows []row
	err := app.AuxDB().NewQuery("SELECT [[message]], [[data]] FROM `_logs` WHERE [[message]] = {:message}").
		Bind(dbx.Params{"message": message}).All(&rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("no log entry %q", message)
	}
	for _, r := range rows {
		if extra == "" || strings.Contains(r.Data, extra) || strings.Contains(r.Message, extra) {
			return
		}
	}
	t.Fatalf("log %q does not contain %q: %+v", message, extra, rows)
}

func flushLogs(t testing.TB, app core.App) {
	t.Helper()
	h, ok := app.Logger().Handler().(*logger.BatchHandler)
	if !ok {
		t.Fatalf("logger handler type %T", app.Logger().Handler())
	}
	ctx := context.WithValue(context.Background(), logger.BlockKey, true)
	if err := h.WriteAll(ctx); err != nil {
		t.Fatal(err)
	}
}
