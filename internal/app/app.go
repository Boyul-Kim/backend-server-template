// Package app assembles the PocketBase application: plugins, settings,
// middleware, routes and hooks.
package app

import (
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"

	"github.com/boyulkim/api-template/internal/api"
	"github.com/boyulkim/api-template/internal/config"
	"github.com/boyulkim/api-template/internal/hooks"
	"github.com/boyulkim/api-template/internal/middleware"

	// Registers the Go migrations in core.AppMigrations.
	_ "github.com/boyulkim/api-template/migrations"
)

// New creates the PocketBase instance with everything registered on it.
func New(cfg config.Config) *pocketbase.PocketBase {
	pb := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDev:           !cfg.IsProduction(),
		DefaultDataDir:       cfg.DataDir,
		DefaultEncryptionEnv: config.EncryptionKeyEnv,
	})

	// `migrate` CLI command. In development, schema changes made in the
	// dashboard are written to ./migrations as Go files automatically.
	migratecmd.MustRegister(pb, pb.RootCmd, migratecmd.Config{
		Dir:          "migrations",
		TemplateLang: migratecmd.TemplateLangGo,
		Automigrate:  !cfg.IsProduction(),
	})

	Register(pb, cfg)

	return pb
}

// Register binds settings sync, middleware, routes and hooks to app.
//
// It takes a core.App (not *pocketbase.PocketBase) so tests can register the
// same wiring on a tests.TestApp.
func Register(app core.App, cfg config.Config) {
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// Migrations have already run at this point, so settings and the
		// superusers collection are guaranteed to exist.
		if err := applySettings(e.App, cfg); err != nil {
			return err
		}
		if err := ensureSuperuser(e.App, cfg); err != nil {
			return err
		}

		// With a bootstrap superuser configured, skip the browser installer.
		if cfg.SuperuserEmail != "" {
			e.InstallerFunc = nil
		}

		middleware.Register(e.Router, cfg)
		api.Register(e.Router)

		return e.Next()
	})

	hooks.Register(app)
}
