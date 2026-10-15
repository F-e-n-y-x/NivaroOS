package v1

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/version"
	"github.com/labstack/echo/v5"
)

// Server updates (Settings > Updates, the app's System updates card): core
// only reads the self-updater's state files and asks nivaroos-update.service
// to act (request.json + systemctl start). The work - check, download,
// verify, install, rollback - is nivaroos-cli's (docs/guides/server-updates.md),
// in its own unit, so restarting core mid-update can't cut it off.

var (
	serverUpdateDir = "/var/lib/nivaroos/update"
	// startServerUpdate starts the updater; swapped by tests.
	startServerUpdate = func() error {
		return exec.Command("systemctl", "start", "--no-block", "nivaroos-update.service").Run()
	}
	serverUpdateRunning = func() bool {
		out, _ := exec.Command("systemctl", "is-active", "nivaroos-update.service").Output()
		s := strings.TrimSpace(string(out))
		return s == "active" || s == "activating"
	}
)

// Same defaults as nivaroos-cli (cli/update/state.go LoadSettings):
// install automatically at about 04:00 on the release's channel.
type serverUpdateSettings struct {
	Channel string `json:"channel"`
	Auto    string `json:"auto"`
	Window  string `json:"window"`
}

func loadServerUpdateSettings() serverUpdateSettings {
	var s serverUpdateSettings
	if b, err := os.ReadFile(filepath.Join(serverUpdateDir, "settings.json")); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Channel != "stable" && s.Channel != "beta" {
		s.Channel = "stable"
		if version.Info().Channel == "beta" {
			s.Channel = "beta"
		}
	}
	if s.Auto != "notify" {
		s.Auto = "install"
	}
	if _, err := time.Parse("15:04", s.Window); err != nil {
		s.Window = "04:00"
	}
	return s
}

// GetServerUpdate: GET /v1/sys/update/status
func GetServerUpdate(ctx *echo.Context) error {
	state := json.RawMessage(`{"state":"idle"}`)
	if b, err := os.ReadFile(filepath.Join(serverUpdateDir, "state.json")); err == nil && json.Valid(b) {
		state = b
	}
	history := []json.RawMessage{}
	if b, err := os.ReadFile(filepath.Join(serverUpdateDir, "history.jsonl")); err == nil {
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		for i := len(lines) - 1; i >= 0 && len(history) < 20; i-- {
			if json.Valid([]byte(lines[i])) {
				history = append(history, json.RawMessage(lines[i]))
			}
		}
	}
	_, snapErr := os.Stat(filepath.Join(serverUpdateDir, "snap", "apply.sh"))
	snapState, _ := os.ReadFile(filepath.Join(serverUpdateDir, "snap", "state"))
	return ctx.JSON(http.StatusOK, model.Result{Success: 200, Message: "ok", Data: map[string]interface{}{
		"build":    version.Info(),
		"state":    state,
		"settings": loadServerUpdateSettings(),
		"history":  history,
		"running":  serverUpdateRunning(),
		// The last update can still be undone (its snapshot is there).
		"can_rollback": snapErr == nil && strings.TrimSpace(string(snapState)) == "committed",
	}})
}

func requestServerUpdate(ctx *echo.Context, req map[string]interface{}) error {
	if !requestIsAdmin(ctx) {
		return ctx.JSON(http.StatusForbidden, model.Result{Success: http.StatusForbidden, Message: "only an administrator can update the server"})
	}
	if serverUpdateRunning() {
		return ctx.JSON(http.StatusConflict, model.Result{Success: http.StatusConflict, Message: "an update is already running"})
	}
	b, _ := json.Marshal(req)
	if err := os.MkdirAll(serverUpdateDir, 0o700); err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: 500, Message: err.Error()})
	}
	tmp := filepath.Join(serverUpdateDir, "request.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: 500, Message: err.Error()})
	}
	if err := os.Rename(tmp, filepath.Join(serverUpdateDir, "request.json")); err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: 500, Message: err.Error()})
	}
	if err := startServerUpdate(); err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: 500, Message: "could not start nivaroos-update.service: " + err.Error()})
	}
	return ctx.JSON(http.StatusAccepted, model.Result{Success: 200, Message: "started"})
}

// PostServerUpdateCheck: POST /v1/sys/update/check
func PostServerUpdateCheck(ctx *echo.Context) error {
	return requestServerUpdate(ctx, map[string]interface{}{"action": "check"})
}

// PostServerUpdateApply: POST /v1/sys/update/apply {"mode":"wait"|"cancel_backups"}
func PostServerUpdateApply(ctx *echo.Context) error {
	var body struct {
		Mode string `json:"mode"`
	}
	_ = json.NewDecoder(ctx.Request().Body).Decode(&body)
	if body.Mode != "cancel_backups" {
		body.Mode = "wait"
	}
	return requestServerUpdate(ctx, map[string]interface{}{"action": "apply", "mode": body.Mode})
}

// PostServerUpdateRollback: POST /v1/sys/update/rollback {"restore_data":bool}
func PostServerUpdateRollback(ctx *echo.Context) error {
	var body struct {
		RestoreData bool `json:"restore_data"`
	}
	_ = json.NewDecoder(ctx.Request().Body).Decode(&body)
	return requestServerUpdate(ctx, map[string]interface{}{"action": "rollback", "restore_data": body.RestoreData})
}

// PutServerUpdateSettings: PUT /v1/sys/update/settings {channel, auto, window}
func PutServerUpdateSettings(ctx *echo.Context) error {
	if !requestIsAdmin(ctx) {
		return ctx.JSON(http.StatusForbidden, model.Result{Success: http.StatusForbidden, Message: "only an administrator can change update settings"})
	}
	s := loadServerUpdateSettings()
	var in serverUpdateSettings
	if err := json.NewDecoder(ctx.Request().Body).Decode(&in); err != nil {
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: 400, Message: "bad settings"})
	}
	if in.Channel != "" {
		s.Channel = in.Channel
	}
	if in.Auto != "" {
		s.Auto = in.Auto
	}
	if in.Window != "" {
		s.Window = in.Window
	}
	if _, err := time.Parse("15:04", s.Window); err != nil || (s.Channel != "stable" && s.Channel != "beta") || (s.Auto != "install" && s.Auto != "notify") {
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: 400, Message: "channel stable|beta, auto install|notify, window HH:MM"})
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := filepath.Join(serverUpdateDir, "settings.json.tmp")
	err := os.MkdirAll(serverUpdateDir, 0o700)
	if err == nil {
		err = os.WriteFile(tmp, append(b, '\n'), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(serverUpdateDir, "settings.json"))
	}
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: 500, Message: err.Error()})
	}
	return ctx.JSON(http.StatusOK, model.Result{Success: 200, Message: "ok", Data: s})
}
