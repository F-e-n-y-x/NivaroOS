package v1

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
)

// Crash reports the phone app sends when its user taps "Send to my
// server" (nothing is sent on its own, and nothing goes anywhere else).
// Each upload is one JSON file; the oldest go once there are too many or
// they take too much room. Settings > About in the web UI lists them.
//
//	POST /v1/sys/crash-reports      any signed-in user (the phone)
//	GET  /v1/sys/crash-reports      administrators: the list
//	GET  /v1/sys/crash-reports/:id  administrators: one report's JSON

var crashReportsDir = "/var/lib/nivaroos/crash-reports"

const (
	crashReportMaxBytes = 256 << 10
	crashReportsMax     = 100
	crashReportsMaxSize = 5 << 20
)

var (
	crashReportID = regexp.MustCompile(`^[0-9]{19}-[0-9a-f]{8}$`)
	crashMu       sync.Mutex
)

// crashUpload is the part of the app's upload the list shows; the file
// keeps the whole body.
type crashUpload struct {
	AppVersion string `json:"app_version"`
	Device     string `json:"device"`
	Reports    []struct {
		Time  string `json:"time"`
		Error string `json:"error"`
	} `json:"reports"`
}

type crashReportItem struct {
	ID         string    `json:"id"`
	ReceivedAt time.Time `json:"received_at"`
	Size       int64     `json:"size"`
	AppVersion string    `json:"app_version"`
	Device     string    `json:"device"`
	Count      int       `json:"count"`
	FirstError string    `json:"first_error"`
}

func PostCrashReport(ctx *echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(ctx.Request().Body, crashReportMaxBytes+1))
	if err != nil {
		return memClearFail(ctx, http.StatusBadRequest, "could not read the report", nil)
	}
	if len(body) > crashReportMaxBytes {
		return memClearFail(ctx, http.StatusRequestEntityTooLarge, "the report is larger than 256 KB", nil)
	}
	var up crashUpload
	if json.Unmarshal(body, &up) != nil || len(up.Reports) == 0 {
		return memClearFail(ctx, http.StatusBadRequest, "not a crash report", nil)
	}
	var r [4]byte
	_, _ = rand.Read(r[:])
	id := strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + hex.EncodeToString(r[:])
	crashMu.Lock()
	defer crashMu.Unlock()
	if err := os.MkdirAll(crashReportsDir, 0o700); err != nil {
		return memClearFail(ctx, http.StatusInternalServerError, err.Error(), nil)
	}
	if err := os.WriteFile(filepath.Join(crashReportsDir, id+".json"), body, 0o600); err != nil {
		return memClearFail(ctx, http.StatusInternalServerError, err.Error(), nil)
	}
	pruneCrashReports()
	return ok(ctx, map[string]string{"id": id})
}

// crashFiles lists the stored reports, newest first.
func crashFiles() []os.DirEntry {
	entries, _ := os.ReadDir(crashReportsDir)
	out := entries[:0]
	for _, e := range entries {
		if crashReportID.MatchString(strings.TrimSuffix(e.Name(), ".json")) && strings.HasSuffix(e.Name(), ".json") {
			out = append(out, e)
		}
	}
	// The ids start with the receive time in nanoseconds: name order is age.
	sort.Slice(out, func(i, j int) bool { return out[i].Name() > out[j].Name() })
	return out
}

func pruneCrashReports() {
	var total int64
	for i, e := range crashFiles() {
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
		if i >= crashReportsMax || total > crashReportsMaxSize {
			_ = os.Remove(filepath.Join(crashReportsDir, e.Name()))
		}
	}
}

func GetCrashReports(ctx *echo.Context) error {
	if !requestIsAdmin(ctx) {
		return memClearFail(ctx, http.StatusForbidden, "Crash reports need an administrator account", nil)
	}
	crashMu.Lock()
	defer crashMu.Unlock()
	items := []crashReportItem{}
	for _, e := range crashFiles() {
		id := strings.TrimSuffix(e.Name(), ".json")
		raw, err := os.ReadFile(filepath.Join(crashReportsDir, e.Name()))
		if err != nil {
			continue
		}
		var up crashUpload
		_ = json.Unmarshal(raw, &up)
		ns, _ := strconv.ParseInt(id[:19], 10, 64)
		it := crashReportItem{ID: id, ReceivedAt: time.Unix(0, ns).UTC(), Size: int64(len(raw)), AppVersion: up.AppVersion, Device: up.Device, Count: len(up.Reports)}
		if len(up.Reports) > 0 {
			it.FirstError = trimRunes(up.Reports[0].Error, 300)
		}
		items = append(items, it)
	}
	return ok(ctx, items)
}

func GetCrashReport(ctx *echo.Context) error {
	if !requestIsAdmin(ctx) {
		return memClearFail(ctx, http.StatusForbidden, "Crash reports need an administrator account", nil)
	}
	id := ctx.Param("id")
	if !crashReportID.MatchString(id) {
		return memClearFail(ctx, http.StatusNotFound, "no such report", nil)
	}
	raw, err := os.ReadFile(filepath.Join(crashReportsDir, id+".json"))
	if err != nil {
		return memClearFail(ctx, http.StatusNotFound, "no such report", nil)
	}
	ctx.Response().Header().Set("Content-Disposition", `attachment; filename="nivaroos-crash-`+id+`.json"`)
	return ctx.Blob(http.StatusOK, "application/json", raw)
}

// trimRunes cuts s to n runes.
func trimRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
