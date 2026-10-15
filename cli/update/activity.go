package update

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/localtoken"
)

// What an automatic install waits for (owner decision 2026-10-16): no
// backup run, no file transfer, no VM console and no Host Desktop session.
// Swapped by tests.
var (
	BackupAPI     = "http://127.0.0.1:28643/v1/backup"
	SidecarAPI    = "http://127.0.0.1:28641"
	TransfersFile = "/var/lib/nivaroos/transfer-jobs.json"
	RuntimeDir    = "/var/run/nivaroos"
)

var local = &http.Client{Timeout: 5 * time.Second}

// localJSON GETs or POSTs a NivaroOS service on loopback as root (the
// local-automation token). A service that isn't there has nothing running.
func localJSON(ctx context.Context, method, u string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	localtoken.Add(req)
	res, err := local.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %s", u, res.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

type backupRun struct {
	ID      string `json:"id"`
	JobName string `json:"job_name"`
}

// runningBackups: the Backup & Sync runs going on now.
func runningBackups(ctx context.Context) []backupRun {
	var r struct {
		Data struct {
			Runs []backupRun `json:"runs"`
		} `json:"data"`
	}
	if localJSON(ctx, http.MethodGet, BackupAPI+"/runs?status=running", nil, &r) != nil {
		return nil
	}
	return r.Data.Runs
}

func cancelBackups(ctx context.Context, runs []backupRun) {
	for _, r := range runs {
		_ = localJSON(ctx, http.MethodPost, BackupAPI+"/runs/"+url.PathEscape(r.ID)+"/cancel", map[string]string{}, nil)
	}
}

// busy says what is going on that an automatic install must not cut off
// ("" when nothing is).
func busy(ctx context.Context) string {
	var why []string
	if runs := runningBackups(ctx); len(runs) > 0 {
		why = append(why, fmt.Sprintf("%d backup run(s)", len(runs)))
	}
	if n := activeTransfers(); n > 0 {
		why = append(why, fmt.Sprintf("%d file transfer(s)", n))
	}
	var s struct {
		VMConsole   int `json:"vm_console"`
		HostDesktop int `json:"host_desktop"`
	}
	if localJSON(ctx, http.MethodGet, SidecarAPI+"/sessions", nil, &s) == nil {
		if s.VMConsole > 0 {
			why = append(why, fmt.Sprintf("%d VM console(s)", s.VMConsole))
		}
		if s.HostDesktop > 0 {
			why = append(why, "a Host Desktop session")
		}
	}
	return strings.Join(why, ", ")
}

// activeTransfers counts Files copy/move/delete jobs that haven't ended
// (core keeps them in transfer-jobs.json).
func activeTransfers() int {
	b, err := os.ReadFile(TransfersFile)
	if err != nil {
		return 0
	}
	var jobs []struct {
		State string `json:"state"`
	}
	if json.Unmarshal(b, &jobs) != nil {
		return 0
	}
	n := 0
	for _, j := range jobs {
		switch j.State {
		case "queued", "scanning", "running", "syncing":
			n++
		}
	}
	return n
}

// notify puts a notification in the NivaroOS feed (web Notification Center
// and the phone), as core: source "nivaroos", event "nivaroos:update:notify".
func notify(ctx context.Context, level, key, title, message string, args map[string]string) {
	raw, err := os.ReadFile(filepath.Join(RuntimeDir, "message-bus.url"))
	if err != nil {
		return
	}
	base := strings.TrimRight(strings.TrimSpace(string(raw)), "/")
	if u, err := url.Parse(base); err != nil || u.Scheme != "http" || !isLoopback(u.Hostname()) {
		return
	}
	base += "/v2/message_bus"
	const source, event = "nivaroos", "nivaroos:update:notify"
	props := []map[string]string{}
	for _, n := range []string{"title", "message", "level", "category", "key", "args", "action"} {
		props = append(props, map[string]string{"name": n})
	}
	if localJSON(ctx, http.MethodPost, base+"/event_type", []map[string]any{{"sourceID": source, "name": event, "propertyTypeList": props}}, nil) != nil {
		return
	}
	a, _ := json.Marshal(args)
	action, _ := json.Marshal(map[string]any{"target": "settings", "props": map[string]string{"section": "updates"}})
	_ = localJSON(ctx, http.MethodPost, base+"/event/"+source+"/"+url.PathEscape(event), map[string]string{
		"title": title, "message": message, "level": level, "category": "system",
		"key": key, "args": string(a), "action": string(action),
	}, nil)
}

func isLoopback(h string) bool { return h == "127.0.0.1" || h == "localhost" || h == "::1" }
