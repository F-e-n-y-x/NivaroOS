package service

import (
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/common"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
)

// Which events are notifications
//
// The bus carries two kinds of traffic: state streams (widget numbers,
// progress, file-operation ticks, hot-plug) that only matter to whoever is
// watching right now, and outcomes a person should learn about even if no
// screen was open when they happened. Only the second kind is persisted in
// the feed. The rule is an allow-list, so a new producer never floods the
// feed by accident:
//
//  1. The notification contract - any event whose name ends in ":notify"
//     and carries a non-empty `title`. This is how a service raises a
//     notification on purpose. Properties (all strings, as on every bus
//     event): title (required, English), message (English), level
//     (info|success|warning|error), key + args (an i18n key and a JSON
//     object of its arguments, so a client can render it in the viewer's
//     language), action (JSON object, what opening it does), icon, and
//     category (backup|app|storage|system; default: from the source).
//     Today: nivaroos-backup / nivaroos:backup:notify (run failed, waiting
//     for a decision, stale, offline, partial, success when enabled,
//     migration report ...), whose `window` property becomes the action
//     {"target":"backup","window":{"kind":..,"props":..}}.
//
//  2. Outcomes of long or unattended app-management work, which the web
//     UI has always put in its Notification Center:
//     app:install-end / -error, app:uninstall-end / -error,
//     app:update-end (only when the image really changed) / -error, and
//     the failures app:apply-changes-error, app:start-error,
//     app:stop-error, app:restart-error.
//     Not persisted: *-begin and *-progress (live progress), the
//     successful start/stop/restart/apply-changes and "already up to date"
//     (instant feedback to the click that caused them), app-store and
//     docker image/container events (implementation detail of the above).
//
//  3. local-storage:storage-job:end / :error - the result of a format or
//     storage creation, which can take minutes.
//
// Trust limits (any authenticated user, and same-host automation - which
// includes app containers on host networking - may publish events):
//
//   - rule 1 accepts only the NivaroOS services in notifySources; a
//     ":notify" from any other source is not stored;
//   - an icon must be an image on this server (a same-origin path) or a
//     data:image URI - never a remote URL, which every viewer's browser
//     would fetch (tracking their IP). App outcomes (rule 2) may also carry
//     the app store's http(s) icon URL, as the app store itself shows it;
//   - NotificationService.Ingest rate-limits entries per source, so a
//     flood can't evict the real notifications (see notificationSourceBurst).
//
// Not notifications: nivaroos:system:utilization, nivaroos:file:operate /
// :recover / :changed (core's SendNotify publishes these - it is a
// transport, not a notification API), local-storage:disk:added / :removed
// (hot-plug state; the web UI still shows its own toast), storage-job
// progress, backup run-* / job-changed (progress and list refreshes), and
// the message bus's own heartbeat and feed events.

const (
	notifyTitleMax   = 256
	notifyMessageMax = 4096
	notifyIconMax    = 2048
	notifyJSONMax    = 8192
)

// notifySources are the sources whose ":notify" events are stored. Add a
// service here when it starts raising notifications (the Download Station
// sidecar is expected to publish as nivaroos-download).
var notifySources = map[string]bool{
	"nivaroos-backup":   true,
	"nivaroos":          true, // core
	"app-management":    true,
	"local-storage":     true,
	"nivaroos-download": true,
}

type appOutcome struct {
	level string
	key   string
	title string // English
}

var appOutcomes = map[string]appOutcome{
	"app:install-end":         {model.NotificationLevelSuccess, "notify.app.installed", "App installed"},
	"app:install-error":       {model.NotificationLevelError, "notify.app.install_failed", "App installation failed"},
	"app:uninstall-end":       {model.NotificationLevelInfo, "notify.app.uninstalled", "App uninstalled"},
	"app:uninstall-error":     {model.NotificationLevelError, "notify.app.uninstall_failed", "App uninstall failed"},
	"app:update-end":          {model.NotificationLevelSuccess, "notify.app.updated", "App updated"},
	"app:update-error":        {model.NotificationLevelError, "notify.app.update_failed", "App update failed"},
	"app:apply-changes-error": {model.NotificationLevelError, "notify.app.apply_failed", "Applying app settings failed"},
	"app:start-error":         {model.NotificationLevelError, "notify.app.start_failed", "App failed to start"},
	"app:stop-error":          {model.NotificationLevelError, "notify.app.stop_failed", "App failed to stop"},
	"app:restart-error":       {model.NotificationLevelError, "notify.app.restart_failed", "App failed to restart"},
}

// ClassifyNotification turns an event into a feed entry, or reports that
// it isn't one (see the rules above).
func ClassifyNotification(event model.Event) (model.Notification, bool) {
	p := event.Properties
	if p == nil {
		p = map[string]string{}
	}
	var n model.Notification
	switch {
	case event.SourceID == common.MessageBusSourceID:
		return n, false

	case strings.HasSuffix(event.Name, ":notify"):
		title := strings.TrimSpace(p["title"])
		if title == "" || !notifySources[event.SourceID] {
			return n, false
		}
		n = model.Notification{
			Category: p["category"],
			Level:    p["level"],
			Title:    title,
			Message:  p["message"],
			Key:      p["key"],
			Args:     p["args"],
			Action:   p["action"],
			Icon:     localIcon(p["icon"]),
		}
		if n.Category == "" {
			n.Category = categoryOfSource(event.SourceID)
		}
		if n.Action == "" && p["window"] != "" && n.Category == model.NotificationCategoryBackup {
			n.Action = backupAction(p["window"])
		}

	case event.SourceID == "app-management":
		o, ok := appOutcomes[event.Name]
		if !ok {
			return n, false
		}
		if event.Name == "app:update-end" && p["docker:image:updated"] == "false" {
			return n, false
		}
		app := appTitle(p)
		args := map[string]string{"app": app}
		msg := app
		if errText := strings.TrimSpace(p["message"]); errText != "" && o.level == model.NotificationLevelError {
			args["error"] = errText
			msg = app + ": " + errText
		}
		if raw := p["app:title"]; raw != "" {
			args["app_title"] = raw
		}
		n = model.Notification{
			Category: model.NotificationCategoryApp,
			Level:    o.level,
			Title:    o.title,
			Message:  msg,
			Key:      o.key,
			Args:     mustJSON(args),
			Icon:     appIcon(p["app:icon"]),
		}

	case event.SourceID == "local-storage" && (event.Name == "local-storage:storage-job:end" || event.Name == "local-storage:storage-job:error"):
		kind := p["local-storage:job_kind"]
		path := p["local-storage:path"]
		failed := event.Name == "local-storage:storage-job:error"
		args := map[string]string{"kind": kind, "path": path, "mount_point": p["local-storage:mount_point"]}
		n = model.Notification{
			Category: model.NotificationCategoryStorage,
			Action:   `{"target":"settings","props":{"section":"storage"}}`,
		}
		verb := "Storage setup"
		if kind == "format" {
			verb = "Formatting"
		}
		if failed {
			args["error"] = p["local-storage:message"]
			n.Level, n.Key, n.Title = model.NotificationLevelError, "notify.storage.job_failed", verb+" failed"
			n.Message = strings.TrimSpace(path + ": " + p["local-storage:message"])
		} else {
			n.Level, n.Key, n.Title = model.NotificationLevelSuccess, "notify.storage.job_done", verb+" finished"
			n.Message = path
			if mp := p["local-storage:mount_point"]; mp != "" {
				n.Message = path + " → " + mp
			}
		}
		n.Args = mustJSON(args)

	default:
		return n, false
	}

	n.SourceID = event.SourceID
	n.EventName = event.Name
	n.EventUUID = event.UUID
	n.Level = normalizeLevel(n.Level)
	n.Category = normalizeCategory(n.Category)
	n.Title = truncateRunes(n.Title, notifyTitleMax)
	n.Message = truncateRunes(n.Message, notifyMessageMax)
	n.Key = truncateRunes(n.Key, notifyTitleMax)
	n.Icon = truncateRunes(n.Icon, notifyIconMax)
	n.Args = jsonObjectOrEmpty(n.Args)
	n.Action = jsonObjectOrEmpty(n.Action)
	return n, true
}

// localIcon keeps icon only when viewers' browsers can load it without
// contacting another site: a same-origin absolute path ("/img/x.svg", not
// "//host/x") or a data:image URI.
func localIcon(icon string) string {
	icon = strings.TrimSpace(icon)
	lower := strings.ToLower(icon)
	switch {
	case icon == "":
		return ""
	case strings.HasPrefix(lower, "data:image/") && !strings.HasPrefix(lower, "data:image/svg"):
		// SVG is left out: a data: SVG is inert in <img>, but it is also
		// the one image type that can reference other URLs.
		return icon
	case strings.HasPrefix(icon, "/") && !strings.HasPrefix(icon, "//") && !strings.Contains(icon, "\\"):
		return icon
	}
	return ""
}

// appIcon is localIcon plus the http(s) URLs app store icons use.
func appIcon(icon string) string {
	if l := localIcon(icon); l != "" {
		return l
	}
	icon = strings.TrimSpace(icon)
	if u, err := url.Parse(icon); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil {
		return icon
	}
	return ""
}

func categoryOfSource(sourceID string) string {
	switch sourceID {
	case "nivaroos-backup":
		return model.NotificationCategoryBackup
	case "app-management":
		return model.NotificationCategoryApp
	case "local-storage":
		return model.NotificationCategoryStorage
	}
	return model.NotificationCategorySystem
}

func normalizeCategory(c string) string {
	switch c {
	case model.NotificationCategoryBackup, model.NotificationCategoryApp, model.NotificationCategoryStorage, model.NotificationCategorySystem:
		return c
	}
	return model.NotificationCategorySystem
}

func normalizeLevel(l string) string {
	switch strings.ToLower(strings.TrimSpace(l)) {
	case "error", "err", "failed", "failure", "danger":
		return model.NotificationLevelError
	case "warning", "warn":
		return model.NotificationLevelWarning
	case "success", "ok", "done":
		return model.NotificationLevelSuccess
	}
	return model.NotificationLevelInfo
}

// backupAction wraps the backup service's `window` property
// ({"kind":"run","props":{...}}, a ui/src/apps/backup/windows.js kind).
func backupAction(window string) string {
	var w map[string]interface{}
	if json.Unmarshal([]byte(window), &w) != nil || w == nil {
		return ""
	}
	return mustJSON(map[string]interface{}{"target": "backup", "window": w})
}

// appTitle is the English title of an app-management event's app.
func appTitle(p map[string]string) string {
	if raw := p["app:title"]; raw != "" {
		var titles map[string]string
		if json.Unmarshal([]byte(raw), &titles) == nil {
			for _, k := range []string{"custom", "en_us", "en_US", "en"} {
				if t := strings.TrimSpace(titles[k]); t != "" {
					return t
				}
			}
		} else if !strings.HasPrefix(raw, "{") {
			return raw
		}
	}
	if name := p["app:name"]; name != "" {
		return name
	}
	if name := p["name"]; name != "" {
		return name
	}
	return "App"
}

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// jsonObjectOrEmpty keeps s only when it is a JSON object of sane size.
func jsonObjectOrEmpty(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > notifyJSONMax || !strings.HasPrefix(s, "{") || !json.Valid([]byte(s)) {
		return ""
	}
	return s
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}
