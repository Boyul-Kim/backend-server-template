package app

import (
	"errors"
	"fmt"

	"github.com/pocketbase/pocketbase/core"

	"github.com/boyulkim/api-template/internal/config"
)

// applySettings makes the environment the source of truth for PocketBase
// settings. Values that are not set in the environment keep whatever is
// stored in the database, so they can still be managed from the dashboard.
func applySettings(app core.App, cfg config.Config) error {
	s := app.Settings()

	if cfg.AppName != "" {
		s.Meta.AppName = cfg.AppName
	}
	if cfg.AppURL != "" {
		s.Meta.AppURL = cfg.AppURL
	}
	s.Meta.HideControls = cfg.HideControls

	if cfg.SMTP.Host != "" {
		s.SMTP.Enabled = true
		s.SMTP.Host = cfg.SMTP.Host
		s.SMTP.Port = cfg.SMTP.Port
		s.SMTP.Username = cfg.SMTP.Username
		s.SMTP.Password = cfg.SMTP.Password
		s.SMTP.TLS = cfg.SMTP.TLS
		s.Meta.SenderAddress = cfg.SMTP.SenderAddress
		if cfg.SMTP.SenderName != "" {
			s.Meta.SenderName = cfg.SMTP.SenderName
		}
	}

	if cfg.S3.Bucket != "" {
		s.S3 = core.S3Config{
			Enabled:        true,
			Bucket:         cfg.S3.Bucket,
			Region:         cfg.S3.Region,
			Endpoint:       cfg.S3.Endpoint,
			AccessKey:      cfg.S3.AccessKey,
			Secret:         cfg.S3.Secret,
			ForcePathStyle: cfg.S3.ForcePathStyle,
		}
	}

	if cfg.Backups.Cron != "" {
		s.Backups.Cron = cfg.Backups.Cron
		s.Backups.CronMaxKeep = cfg.Backups.MaxKeep
	}

	s.RateLimits.Enabled = cfg.RateLimitEnabled
	if len(cfg.TrustedProxyHeaders) > 0 {
		s.TrustedProxy.Headers = cfg.TrustedProxyHeaders
	}
	s.Logs.MaxDays = cfg.LogsMaxDays

	if err := app.Save(s); err != nil {
		return fmt.Errorf("apply settings from env: %w", err)
	}
	return nil
}

// ensureSuperuser creates the bootstrap superuser if one is configured and
// no superuser exists yet. It never modifies an existing account, so rotating
// SUPERUSER_PASSWORD after the first start has no effect (use the dashboard
// or `server superuser update` instead).
func ensureSuperuser(app core.App, cfg config.Config) error {
	if cfg.SuperuserEmail == "" {
		return nil
	}

	total, err := app.CountRecords(core.CollectionNameSuperusers)
	if err != nil {
		return fmt.Errorf("count superusers: %w", err)
	}
	if total > 0 {
		return nil
	}

	collection, err := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}

	su := core.NewRecord(collection)
	su.SetEmail(cfg.SuperuserEmail)
	su.SetPassword(cfg.SuperuserPassword)

	if err := app.Save(su); err != nil {
		return errors.Join(errors.New("create bootstrap superuser"), err)
	}

	app.Logger().Info("created bootstrap superuser", "email", cfg.SuperuserEmail)
	return nil
}
