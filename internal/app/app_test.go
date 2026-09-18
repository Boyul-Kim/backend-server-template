package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/boyulkim/api-template/internal/app"
	"github.com/boyulkim/api-template/internal/config"
	"github.com/boyulkim/api-template/internal/middleware"
	"github.com/boyulkim/api-template/internal/testutil"
)

// These tests exercise the full wiring (settings, middleware, routes, hooks
// and collection rules) over HTTP, the way clients see it.

const (
	alice     = "alice0000000001"
	bob       = "bob000000000001"
	aliceNote = "alicenote000001"
	bobNote   = "bobnote00000001"
)

var devConfig = config.Config{Env: config.EnvDevelopment, HTTPAddr: "127.0.0.1:8090"}

// run executes s against a fresh app with two users, authenticated as `as`
// ("" for a guest). seed may add fixtures.
func run(t *testing.T, s tests.ApiScenario, as string, seed func(testing.TB, core.App)) {
	t.Helper()

	if s.Headers == nil {
		s.Headers = map[string]string{}
	}
	headers := s.Headers // read by the scenario after the factory runs

	s.TestAppFactory = func(tb testing.TB) *tests.TestApp {
		testApp := testutil.NewApp(tb)
		app.Register(testApp, devConfig)

		tokens := map[string]string{
			alice: testutil.CreateUser(tb, testApp, alice, "alice@example.com"),
			bob:   testutil.CreateUser(tb, testApp, bob, "bob@example.com"),
		}
		if as != "" {
			headers["Authorization"] = tokens[as]
		}
		if seed != nil {
			seed(tb, testApp)
		}
		return testApp
	}

	s.Test(t)
}

func countNotes(t testing.TB, app core.App, owner string) int64 {
	t.Helper()
	n, err := app.CountRecords("notes", dbx.HashExp{"owner": owner})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSystemEndpoints(t *testing.T) {
	run(t, tests.ApiScenario{
		Name:            "version",
		Method:          http.MethodGet,
		URL:             "/api/v1/version",
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"version":"dev"`},
	}, "", nil)

	run(t, tests.ApiScenario{
		Name:            "readiness",
		Method:          http.MethodGet,
		URL:             "/api/v1/ready",
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"status":"ok"`},
	}, "", nil)

	run(t, tests.ApiScenario{
		Name:            "request id is generated",
		Method:          http.MethodGet,
		URL:             "/api/health",
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"code":200`},
		AfterTestFunc: func(t testing.TB, _ *tests.TestApp, res *http.Response) {
			if res.Header.Get(middleware.RequestIDHeader) == "" {
				t.Error("missing request id header")
			}
		},
	}, "", nil)

	run(t, tests.ApiScenario{
		Name:            "valid request id is propagated",
		Method:          http.MethodGet,
		URL:             "/api/health",
		Headers:         map[string]string{middleware.RequestIDHeader: "lb-abc.123"},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"code":200`},
		AfterTestFunc: func(t testing.TB, _ *tests.TestApp, res *http.Response) {
			if got := res.Header.Get(middleware.RequestIDHeader); got != "lb-abc.123" {
				t.Errorf("request id = %q", got)
			}
		},
	}, "", nil)

	run(t, tests.ApiScenario{
		Name:            "malformed request id is replaced",
		Method:          http.MethodGet,
		URL:             "/api/health",
		Headers:         map[string]string{middleware.RequestIDHeader: "<script>"},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"code":200`},
		AfterTestFunc: func(t testing.TB, _ *tests.TestApp, res *http.Response) {
			if got := res.Header.Get(middleware.RequestIDHeader); got == "" || got == "<script>" {
				t.Errorf("request id = %q", got)
			}
		},
	}, "", nil)
}

func TestNoteStats(t *testing.T) {
	seed := func(t testing.TB, app core.App) {
		testutil.CreateRecord(t, app, "notes", map[string]any{"owner": alice, "title": "a", "pinned": true})
		testutil.CreateRecord(t, app, "notes", map[string]any{"owner": alice, "title": "b"})
		testutil.CreateRecord(t, app, "notes", map[string]any{"owner": bob, "title": "c"})
	}

	run(t, tests.ApiScenario{
		Name:            "guest is rejected",
		Method:          http.MethodGet,
		URL:             "/api/v1/notes/stats",
		ExpectedStatus:  http.StatusUnauthorized,
		ExpectedContent: []string{`"data":{}`},
	}, "", seed)

	run(t, tests.ApiScenario{
		Name:            "counts only the requester's notes",
		Method:          http.MethodGet,
		URL:             "/api/v1/notes/stats",
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"total":2`, `"pinned":1`},
	}, alice, seed)
}

func TestNoteImport(t *testing.T) {
	run(t, tests.ApiScenario{
		Name:            "creates all notes",
		Method:          http.MethodPost,
		URL:             "/api/v1/notes/import",
		Body:            strings.NewReader(`{"notes":[{"title":"  one  "},{"title":"two","pinned":true}]}`),
		ExpectedStatus:  http.StatusCreated,
		ExpectedContent: []string{`"ids":[`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			if n := countNotes(t, app, alice); n != 2 {
				t.Errorf("created %d notes, want 2", n)
			}
			if _, err := app.FindFirstRecordByData("notes", "title", "one"); err != nil {
				t.Error("title was not trimmed")
			}
		},
	}, alice, nil)

	run(t, tests.ApiScenario{
		Name:            "rejects an invalid item without creating any",
		Method:          http.MethodPost,
		URL:             "/api/v1/notes/import",
		Body:            strings.NewReader(`{"notes":[{"title":"ok"},{"title":"   "}]}`),
		ExpectedStatus:  http.StatusBadRequest,
		ExpectedContent: []string{`"notes":{"1":{"title":{"code":"validation_required"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			if n := countNotes(t, app, alice); n != 0 {
				t.Errorf("created %d notes, want 0", n)
			}
		},
	}, alice, nil)

	run(t, tests.ApiScenario{
		Name:            "rejects an empty list",
		Method:          http.MethodPost,
		URL:             "/api/v1/notes/import",
		Body:            strings.NewReader(`{"notes":[]}`),
		ExpectedStatus:  http.StatusBadRequest,
		ExpectedContent: []string{`"notes":{"code":"validation_required"`},
	}, alice, nil)

	run(t, tests.ApiScenario{
		Name:            "guest is rejected",
		Method:          http.MethodPost,
		URL:             "/api/v1/notes/import",
		Body:            strings.NewReader(`{"notes":[{"title":"x"}]}`),
		ExpectedStatus:  http.StatusUnauthorized,
		ExpectedContent: []string{`"data":{}`},
	}, "", nil)
}

// TestNotesCollectionRules covers PocketBase's built-in REST API for the
// collection: the create hook and the owner-only rules from the migration.
func TestNotesCollectionRules(t *testing.T) {
	seed := func(t testing.TB, app core.App) {
		testutil.CreateRecord(t, app, "notes", map[string]any{"id": aliceNote, "owner": alice, "title": "alice note"})
		testutil.CreateRecord(t, app, "notes", map[string]any{"id": bobNote, "owner": bob, "title": "bob note"})
	}

	run(t, tests.ApiScenario{
		Name:               "list returns only own notes",
		Method:             http.MethodGet,
		URL:                "/api/collections/notes/records",
		ExpectedStatus:     http.StatusOK,
		ExpectedContent:    []string{`"totalItems":1`, `"title":"alice note"`},
		NotExpectedContent: []string{`bob note`},
	}, alice, seed)

	run(t, tests.ApiScenario{
		Name:            "guest sees nothing",
		Method:          http.MethodGet,
		URL:             "/api/collections/notes/records",
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"totalItems":0`},
	}, "", seed)

	run(t, tests.ApiScenario{
		Name:            "create defaults owner to the requester",
		Method:          http.MethodPost,
		URL:             "/api/collections/notes/records",
		Body:            strings.NewReader(`{"title":"new"}`),
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"owner":"` + alice + `"`, `"title":"new"`},
	}, alice, seed)

	run(t, tests.ApiScenario{
		Name:            "cannot create a note for another user",
		Method:          http.MethodPost,
		URL:             "/api/collections/notes/records",
		Body:            strings.NewReader(`{"title":"x","owner":"` + bob + `"}`),
		ExpectedStatus:  http.StatusBadRequest,
		ExpectedContent: []string{`"data":{}`},
	}, alice, seed)

	run(t, tests.ApiScenario{
		Name:            "cannot read another user's note",
		Method:          http.MethodGet,
		URL:             "/api/collections/notes/records/" + bobNote,
		ExpectedStatus:  http.StatusNotFound,
		ExpectedContent: []string{`"data":{}`},
	}, alice, seed)

	run(t, tests.ApiScenario{
		Name:            "cannot update another user's note",
		Method:          http.MethodPatch,
		URL:             "/api/collections/notes/records/" + bobNote,
		Body:            strings.NewReader(`{"title":"mine now"}`),
		ExpectedStatus:  http.StatusNotFound,
		ExpectedContent: []string{`"data":{}`},
	}, alice, seed)

	run(t, tests.ApiScenario{
		Name:            "cannot hand own note to another user",
		Method:          http.MethodPatch,
		URL:             "/api/collections/notes/records/" + aliceNote,
		Body:            strings.NewReader(`{"owner":"` + bob + `"}`),
		ExpectedStatus:  http.StatusNotFound,
		ExpectedContent: []string{`"data":{}`},
	}, alice, seed)
}
