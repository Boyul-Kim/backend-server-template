// Package hooks holds record and model lifecycle hooks, i.e. business logic
// that must run no matter which endpoint touched the data.
package hooks

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Register binds all hooks to the app.
func Register(app core.App) {
	// Default the owner of a note created through the REST API to the
	// requester, so clients don't have to send it. This runs after the
	// collection's create rule, which rejects an owner other than the
	// requester.
	app.OnRecordCreateRequest("notes").BindFunc(func(e *core.RecordRequestEvent) error {
		if e.Auth != nil && e.Record.GetString("owner") == "" {
			e.Record.Set("owner", e.Auth.Id)
		}
		return e.Next()
	})

	// Model-level hooks run for every write (REST API, custom routes,
	// migrations, jobs), not just HTTP requests.
	app.OnRecordValidate("notes").BindFunc(func(e *core.RecordEvent) error {
		e.Record.Set("title", strings.TrimSpace(e.Record.GetString("title")))
		return e.Next()
	})
}
