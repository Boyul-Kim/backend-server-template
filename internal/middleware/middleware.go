// Package middleware holds app-wide request middleware.
//
// PocketBase already registers panic recovery, rate limiting, auth token
// loading, activity logging, CORS, body limits and basic security headers.
// Only add what it doesn't cover here.
package middleware

import (
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/security"

	"github.com/boyulkim/api-template/internal/config"
)

const (
	RequestIDHeader = "X-Request-Id"
	// RequestIDKey is the request store key holding the request id.
	RequestIDKey = "requestId"
)

// Register binds the global middleware to the router.
func Register(r *router.Router[*core.RequestEvent], cfg config.Config) {
	r.Bind(RequestID())

	if cfg.IsProduction() && strings.HasPrefix(cfg.AppURL, "https://") {
		r.Bind(HSTS())
	}
}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// RequestID propagates an incoming X-Request-Id (e.g. from a load balancer)
// or generates one, echoes it in the response and attaches it to the
// request activity log so a client-reported id can be traced in the logs.
func RequestID() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: "appRequestId",
		// Run before PocketBase's activity logger so the id is logged even
		// for requests rejected by the rate limiter or auth middleware.
		Priority: apis.DefaultActivityLoggerMiddlewarePriority - 1,
		Func: func(e *core.RequestEvent) error {
			id := e.Request.Header.Get(RequestIDHeader)
			if !validRequestID.MatchString(id) {
				id = security.RandomString(20)
			}

			e.Set(RequestIDKey, id)
			e.Set(apis.RequestEventKeyLogMeta, map[string]any{RequestIDKey: id})
			e.Response.Header().Set(RequestIDHeader, id)

			return e.Next()
		},
	}
}

// HSTS tells browsers to only use HTTPS. Only enable it when the API is
// served exclusively over HTTPS.
func HSTS() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       "appHSTS",
		Priority: apis.DefaultSecurityHeadersMiddlewarePriority + 1,
		Func: func(e *core.RequestEvent) error {
			e.Response.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			return e.Next()
		},
	}
}
