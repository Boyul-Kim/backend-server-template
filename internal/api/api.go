// Package api holds the custom HTTP endpoints served next to PocketBase's
// built-in /api/collections/* REST API.
//
// Prefer the built-in record API plus collection rules for plain CRUD, and add
// routes here for anything that doesn't map onto a single collection
// (aggregations, multi-record writes, integrations, webhooks, ...).
package api

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// Register mounts the custom routes. They are versioned under /api/v1 so the
// contract can evolve without clashing with PocketBase's own routes.
func Register(r *router.Router[*core.RequestEvent]) {
	v1 := r.Group("/api/v1")

	v1.GET("/version", handleVersion)
	v1.GET("/ready", handleReady)

	notes := v1.Group("/notes").Bind(apis.RequireAuth("users"))
	notes.GET("/stats", handleNoteStats)
	notes.POST("/import", handleNoteImport)
}
