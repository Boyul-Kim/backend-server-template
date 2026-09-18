package api

import (
	"context"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/boyulkim/api-template/internal/version"
)

// Liveness is served by PocketBase at GET /api/health.

// handleReady reports whether the service can serve traffic, i.e. its
// databases answer queries. Use it as the readiness probe.
func handleReady(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 2*time.Second)
	defer cancel()

	if _, err := e.App.DB().NewQuery("SELECT 1").WithContext(ctx).Execute(); err != nil {
		e.App.Logger().Error("readiness check failed", "error", err)
		return apis.NewApiError(http.StatusServiceUnavailable, "Database unavailable.", nil)
	}

	return e.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func handleVersion(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, map[string]string{
		"version": version.Version,
		"commit":  version.Commit,
	})
}
