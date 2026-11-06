package app

import (
	"context"
	"log/slog"
	"slices"
	"sync/atomic"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/boyulkim/api-template/internal/config"
)

const (
	auditUpdateRejected = "rejected env-owned settings update"
	auditDriftReverted  = "alert: reverted drifted env-owned settings"
)

// lockEnvOwnedSettings freezes the settings that production takes from the
// environment. Call it after applySettings, once the in-memory settings match
// the environment.
//
// PATCH /api/settings that changes a locked field is rejected with 403 and an
// audit log entry. OnSettingsReload compares the loaded row with the boot
// snapshot, restores it, and logs an alert when something else wrote the
// database (for example a direct update of _params). The audit entry is a
// structured log until the later alerting work lands.
func lockEnvOwnedSettings(app core.App, cfg config.Config) error {
	if !cfg.IsProduction() {
		return nil
	}

	baseline, err := app.Settings().Clone()
	if err != nil {
		return err
	}

	var reverting atomic.Bool

	app.OnSettingsUpdateRequest().BindFunc(func(e *core.SettingsUpdateRequestEvent) error {
		fields := diffLocked(e.OldSettings, e.NewSettings)
		if len(fields) == 0 {
			return e.Next()
		}

		data := map[string]any{
			"audit":  "settings_update_rejected",
			"fields": fields,
			"ip":     e.RealIP(),
		}
		if e.Auth != nil {
			data["authId"] = e.Auth.Id
			data["email"] = e.Auth.Email()
		}
		auditLog(e.App, slog.LevelWarn, auditUpdateRejected, data)

		reasons := make(map[string]string, len(fields))
		for _, field := range fields {
			reasons[field] = "This setting is owned by the environment and cannot be changed at runtime."
		}
		return e.ForbiddenError("env-owned settings cannot be changed at runtime", reasons)
	})

	app.OnSettingsReload().BindFunc(func(e *core.SettingsReloadEvent) error {
		if reverting.Load() {
			return e.Next()
		}
		if err := e.Next(); err != nil {
			return err
		}

		fields := diffLocked(e.App.Settings(), baseline)
		if len(fields) == 0 {
			return nil
		}

		auditLog(e.App, slog.LevelError, auditDriftReverted, map[string]any{
			"audit":  "settings_drift_reverted",
			"alert":  "settings_drift",
			"fields": fields,
		})

		restoreLocked(e.App.Settings(), baseline)
		reverting.Store(true)
		defer reverting.Store(false)
		if err := e.App.Save(e.App.Settings()); err != nil {
			return err
		}
		return nil
	})

	return nil
}

// auditLog writes a structured log. When log retention is off, PocketBase
// drops that line, so the same entry is stored directly. That keeps the
// audit trail in tests (which disable request logs) and when LOGS_MAX_DAYS=0.
func auditLog(app core.App, level slog.Level, message string, data map[string]any) {
	attrs := make([]any, 0, len(data)*2)
	for key, value := range data {
		attrs = append(attrs, key, value)
	}
	app.Logger().Log(context.Background(), level, message, attrs...)

	if app.Settings().Logs.MaxDays > 0 {
		return
	}

	row := &core.Log{
		Message: message,
		Level:   int(level),
		Data:    types.JSONMap[any](data),
		Created: types.NowDateTime(),
	}
	row.Id = core.GenerateDefaultRandomId()
	row.MarkAsNew()
	if err := app.AuxSave(row); err != nil {
		app.Logger().Error("failed to persist audit log", slog.String("error", err.Error()))
	}
}

// diffLocked reports env-owned fields whose values differ.
// Nil and empty slices are treated as equal so a JSON round trip does not
// look like a change.
func diffLocked(a, b *core.Settings) []string {
	if a == nil || b == nil {
		return []string{"settings"}
	}

	var fields []string
	if !eqStrings(a.SuperuserIPs, b.SuperuserIPs) {
		fields = append(fields, "superuserIPs")
	}
	if a.TrustedProxy.UseLeftmostIP != b.TrustedProxy.UseLeftmostIP || !eqStrings(a.TrustedProxy.Headers, b.TrustedProxy.Headers) {
		fields = append(fields, "trustedProxy")
	}
	if a.RateLimits.Enabled != b.RateLimits.Enabled ||
		!eqStrings(a.RateLimits.ExcludedIPs, b.RateLimits.ExcludedIPs) ||
		!eqRules(a.RateLimits.Rules, b.RateLimits.Rules) {
		fields = append(fields, "rateLimits")
	}
	if a.SMTP != b.SMTP {
		fields = append(fields, "smtp")
	}
	if a.S3 != b.S3 {
		fields = append(fields, "s3")
	}
	if a.Backups != b.Backups {
		fields = append(fields, "backups")
	}
	if a.Batch != b.Batch {
		fields = append(fields, "batch")
	}
	if a.Logs != b.Logs {
		fields = append(fields, "logs")
	}
	if a.Meta.HideControls != b.Meta.HideControls {
		fields = append(fields, "hideControls")
	}
	// App URL and the sender identity build reset and OAuth links. Leaving
	// them editable lets a superuser point them at http://attacker. App name
	// stays editable.
	if a.Meta.AppURL != b.Meta.AppURL {
		fields = append(fields, "appURL")
	}
	if a.Meta.SenderAddress != b.Meta.SenderAddress {
		fields = append(fields, "senderAddress")
	}
	if a.Meta.SenderName != b.Meta.SenderName {
		fields = append(fields, "senderName")
	}
	return fields
}

func restoreLocked(dst, src *core.Settings) {
	dst.SuperuserIPs = slices.Clone(src.SuperuserIPs)
	dst.TrustedProxy.UseLeftmostIP = src.TrustedProxy.UseLeftmostIP
	dst.TrustedProxy.Headers = slices.Clone(src.TrustedProxy.Headers)
	dst.RateLimits.Enabled = src.RateLimits.Enabled
	dst.RateLimits.ExcludedIPs = slices.Clone(src.RateLimits.ExcludedIPs)
	dst.RateLimits.Rules = slices.Clone(src.RateLimits.Rules)
	dst.SMTP = src.SMTP
	dst.S3 = src.S3
	dst.Backups = src.Backups
	dst.Batch = src.Batch
	dst.Logs = src.Logs
	dst.Meta.HideControls = src.Meta.HideControls
	dst.Meta.AppURL = src.Meta.AppURL
	dst.Meta.SenderAddress = src.Meta.SenderAddress
	dst.Meta.SenderName = src.Meta.SenderName
}

func eqStrings(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return slices.Equal(a, b)
}

func eqRules(a, b []core.RateLimitRule) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return slices.Equal(a, b)
}
