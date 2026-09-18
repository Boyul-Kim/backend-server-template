// Package testutil provides helpers for integration tests that run against a
// real (temporary) PocketBase instance with all migrations applied.
package testutil

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Registers the Go migrations in core.AppMigrations.
	_ "github.com/boyulkim/api-template/migrations"
)

// NewApp creates an empty, fully migrated test app in a temp dir.
//
// Register app wiring on it (e.g. app.Register) before triggering OnServe.
// The caller must call Cleanup(), which tests.ApiScenario does automatically.
func NewApp(t testing.TB) *tests.TestApp {
	t.Helper()

	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatalf("create test app: %v", err)
	}
	return app
}

// CreateUser creates a verified user with a fixed id (15 chars, [a-z0-9])
// and returns an auth token for it.
func CreateUser(t testing.TB, app core.App, id, email string) string {
	t.Helper()

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}

	user := core.NewRecord(users)
	user.Id = id
	user.SetEmail(email)
	user.SetPassword("password123456")
	user.SetVerified(true)
	if err := app.Save(user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	token, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("create auth token: %v", err)
	}
	return token
}

// CreateRecord saves a record with the given fields (may include "id").
func CreateRecord(t testing.TB, app core.App, collection string, fields map[string]any) *core.Record {
	t.Helper()

	c, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}

	record := core.NewRecord(c)
	record.Load(fields)
	if err := app.Save(record); err != nil {
		t.Fatalf("create %s record: %v", collection, err)
	}
	return record
}
