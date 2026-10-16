package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"go.uber.org/zap"
)

// Apps of a restored NivaroOS settings backup
//
// services/backup's config-restore puts the apps' compose files back
// under AppsPath and writes RestoreFile ("pending", one entry per app).
// On start this service recreates each of them through the normal install
// path (pull, create volumes and networks, start), one app at a time,
// apps providing an external network before the apps using it, and
// writes every step back into RestoreFile: config-restore and the Welcome
// screen show it. A restored app's host access was approved on the old
// server: it counts as approved. /DATA is only read (is the data there?).

// RestoreFile is the shared status file (services/backup reads it too).
var RestoreFile = "/var/lib/nivaroos/app-management/restore.json"

// RestoreStatus is RestoreFile.
type RestoreStatus struct {
	ID    string       `json:"id,omitempty"` // sha256 of the Welcome screen's restore token
	State string       `json:"state"`        // pending | running | done
	Apps  []RestoreApp `json:"apps"`
}

// RestoreApp is one app's progress.
type RestoreApp struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`           // waiting | installing | ok | failed | skipped
	Reason      string   `json:"reason,omitempty"` // why failed / skipped
	DataMissing bool     `json:"data_missing,omitempty"`
	HostAccess  []string `json:"host_access,omitempty"` // approved on the old server
}

func finished(status string) bool { return status == "ok" || status == "failed" || status == "skipped" }

// RecreateRestoredApps runs a pending restore (see above); a run cut
// short (this service restarted) goes on with the apps not finished.
func RecreateRestoredApps(ctx context.Context) {
	s := MyService.Compose()
	install := func(ctx context.Context, app *ComposeApp) error {
		if _, loaded := s.installationInProgress.LoadOrStore(app.Name, true); loaded {
			return ErrComposeAppAlreadyInstalling
		}
		defer s.installationInProgress.Delete(app.Name)
		ctx = common.WithProperties(ctx, map[string]string{common.PropertyTypeAppName.Name: app.Name})
		go PublishEventWrapper(ctx, common.EventTypeAppInstallBegin, nil)
		defer PublishEventWrapper(ctx, common.EventTypeAppInstallEnd, nil)
		return app.PullAndInstall(ctx)
	}
	if err := recreateApps(ctx, RestoreFile, s.Exists, install); err != nil {
		logger.Error("recreating the restored apps", zap.Error(err))
	}
}

func recreateApps(ctx context.Context, file string, exists func(context.Context, string) (bool, error), install func(context.Context, *ComposeApp) error) error {
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var st RestoreStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return err
	}
	if st.State == "done" {
		return nil
	}
	st.State = "running"
	save := func() {
		if err := writeJSONAtomic(file, st); err != nil {
			logger.Error("writing the restore status", zap.Error(err))
		}
	}
	save()

	apps := map[string]*ComposeApp{}
	var todo []string
	for i := range st.Apps {
		a := &st.Apps[i]
		if finished(a.Status) {
			continue
		}
		// a name config-restore found as a folder under AppsPath; never a path
		if a.Name == "" || a.Name != filepath.Base(a.Name) || strings.HasPrefix(a.Name, ".") {
			a.Status, a.Reason = "failed", "not an app name"
			continue
		}
		app, err := LoadComposeAppFromConfigFile(a.Name, filepath.Join(config.AppInfo.AppsPath, a.Name, common.ComposeYAMLFileName))
		if err != nil {
			a.Status, a.Reason = "failed", "its compose file: "+err.Error()
			continue
		}
		apps[a.Name] = app
		todo = append(todo, a.Name)
	}
	save()

	byName := map[string]*RestoreApp{}
	for i := range st.Apps {
		byName[st.Apps[i].Name] = &st.Apps[i]
	}
	for _, name := range restoreOrder(apps, todo) {
		a, app := byName[name], apps[name]
		if ok, err := exists(ctx, name); err != nil {
			a.Status, a.Reason = "failed", err.Error()
			save()
			continue
		} else if ok {
			a.Status, a.Reason = "skipped", "already installed"
			save()
			continue
		}
		a.DataMissing = missingAppData(app)
		a.HostAccess = app.HostAccess()
		a.Status = "installing"
		save()
		if err := install(ctx, app); err != nil {
			a.Status, a.Reason = "failed", err.Error()
		} else {
			a.Status = "ok"
		}
		save()
	}
	st.State = "done"
	save()
	return nil
}

// restoreOrder: names sorted, except that an app using an external
// network another of these apps creates comes after that app. A cycle
// is broken in name order.
func restoreOrder(apps map[string]*ComposeApp, names []string) []string {
	creator := map[string]string{} // network name -> app
	for _, n := range names {
		for _, nw := range apps[n].Networks {
			if !bool(nw.External) {
				creator[nw.Name] = n // compose named it <project>_<key> (or its name:)
			}
		}
	}
	needs := map[string][]string{}
	for _, n := range names {
		for _, nw := range apps[n].Networks {
			if c, ok := creator[nw.Name]; bool(nw.External) && ok && c != n {
				needs[n] = append(needs[n], c)
			}
		}
	}
	rest := append([]string(nil), names...)
	sort.Strings(rest)
	done := map[string]bool{}
	var out []string
	for len(rest) > 0 {
		pick := 0
		for i, n := range rest {
			ready := true
			for _, c := range needs[n] {
				ready = ready && done[c]
			}
			if ready {
				pick = i
				break
			}
		}
		done[rest[pick]] = true
		out = append(out, rest[pick])
		rest = append(rest[:pick], rest[pick+1:]...)
	}
	return out
}

// missingAppData: a bind of app under AppDataRoot doesn't exist (its
// data wasn't restored; the install creates it empty).
func missingAppData(app *ComposeApp) bool {
	for _, svc := range app.Services {
		for _, v := range svc.Volumes {
			src := filepath.Clean(bindSourceToCreate(v, app.Volumes))
			if strings.HasPrefix(src, AppDataRoot+"/") {
				if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
					return true
				}
			}
		}
	}
	return false
}

func writeJSONAtomic(file string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
