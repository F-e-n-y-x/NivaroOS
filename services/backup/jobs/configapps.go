package jobs

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The apps of a restored settings bundle. Apply lists them in
// appsRestoreFile ("pending"); app-management, started again, recreates
// each one through its normal install path and writes its progress there
// (services/app-management service/restore.go); the CLI prints it and
// the Welcome screen polls POST /config-restore/status for it.

// appsRestoreFile is app-management's RestoreFile, relative to the root.
const appsRestoreFile = "var/lib/nivaroos/app-management/restore.json"

const appsDir = "var/lib/nivaroos/apps/"

// AppsRestoreStatus is appsRestoreFile (without the token hash).
type AppsRestoreStatus struct {
	ID    string            `json:"id,omitempty"`
	State string            `json:"state"` // pending | running | done
	Apps  []AppRestoreState `json:"apps"`
}

// AppRestoreState is one app: waiting | installing | ok | failed | skipped.
type AppRestoreState struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Reason      string   `json:"reason,omitempty"`
	DataMissing bool     `json:"data_missing,omitempty"` // its /DATA/AppData folder wasn't there
	HostAccess  []string `json:"host_access,omitempty"`
}

// ConfigRestoreStatus is POST /config-restore/status.
type ConfigRestoreStatus struct {
	Token string `json:"token"`
}

func tokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// bundleApps: the app folders of a bundle.
func bundleApps(man *ConfigManifest) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range man.Files {
		rest, inApps := strings.CutPrefix(f.Path, appsDir)
		if name, _, ok := strings.Cut(rest, "/"); inApps && ok && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// queueApps hands the bundle's apps to app-management.
func (c *ConfigRestorer) queueApps(man *ConfigManifest) error {
	st := AppsRestoreStatus{ID: c.StatusID, State: "pending"}
	for _, n := range bundleApps(man) {
		st.Apps = append(st.Apps, AppRestoreState{Name: n, Status: "waiting"})
	}
	if len(st.Apps) == 0 {
		return nil
	}
	raw, _ := json.MarshalIndent(st, "", "  ")
	dst := c.target(appsRestoreFile)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(dst, raw, 0o600); err != nil {
		return err
	}
	c.appsQueued = true
	c.say("%d app(s) to recreate", len(st.Apps))
	return nil
}

func readAppsStatus(file string) (*AppsRestoreStatus, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var st AppsRestoreStatus
	return &st, json.Unmarshal(raw, &st)
}

// Waiting for app-management: it should pick the list up within
// appsPickup of starting, and report something at least every appsStall
// (one image pull can take long).
var (
	appsPoll   = 2 * time.Second
	appsPickup = 3 * time.Minute
	appsStall  = 30 * time.Minute
)

// waitApps prints the apps' progress until app-management is done with
// them, then the result of each.
func (c *ConfigRestorer) waitApps(ctx context.Context) {
	if !c.appsQueued {
		return
	}
	if !c.appMgmtStarted {
		c.say("apps: they are recreated when nivaroos-app-management starts")
		return
	}
	file := c.target(appsRestoreFile)
	seen := map[string]string{}
	last := time.Now()
	for {
		st, err := readAppsStatus(file)
		if err == nil {
			for _, a := range st.Apps {
				if seen[a.Name] != a.Status {
					seen[a.Name] = a.Status
					last = time.Now()
					c.say("app %s: %s", a.Name, a.Status)
				}
			}
			if st.State == "done" {
				printAppsResult(c.Out, st)
				return
			}
		}
		pending := err != nil || st.State == "pending"
		if (pending && time.Since(last) > appsPickup) || time.Since(last) > appsStall {
			c.say("apps: app-management hasn't finished them yet; see Apps on the dashboard (or: nivaroos-backup config-restore -apps-status)")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(appsPoll):
		}
	}
}

func printAppsResult(out io.Writer, st *AppsRestoreStatus) {
	if out == nil {
		return
	}
	fmt.Fprintf(out, "apps (%s):\n", st.State)
	for _, a := range st.Apps {
		line := fmt.Sprintf("  %-10s %s", a.Status, a.Name)
		if a.Reason != "" {
			line += ": " + a.Reason
		}
		if a.DataMissing {
			line += " (data not found - restore it from your backups)"
		}
		fmt.Fprintln(out, line)
	}
}

// handleConfigRestoreStatus answers the Welcome screen that started the
// restore (it knows the token; there may be an account by now) with the
// apps' progress. 404 when no apps of that restore were queued.
func (s *Service) handleConfigRestoreStatus(w http.ResponseWriter, r *http.Request) {
	var req ConfigRestoreStatus
	if !decode(w, r, &req) {
		return
	}
	st, err := readAppsStatus((&ConfigRestorer{Root: s.configRoot()}).target(appsRestoreFile))
	if err != nil || req.Token == "" || subtle.ConstantTimeCompare([]byte(st.ID), []byte(tokenID(req.Token))) != 1 {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: "no apps of this restore"})
		return
	}
	st.ID = ""
	writeOK(w, http.StatusOK, st)
}
