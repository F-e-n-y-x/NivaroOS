//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
)

// fakeHost stands in for systemctl, systemd-run, pdbedit, id and useradd
// (NIVAROOS_HOST_CMD): it logs each command; the gateway and Backup are
// "active", and App Management (fakeAppManagement).
const fakeHost = `#!/bin/sh
echo "$*" >> "$FAKE_HOST_LOG"
if [ "$1 $2" = "systemctl is-active" ]; then
	case "$3" in nivaroos-gateway.service|nivaroos-backup.service|nivaroos-app-management.service) echo active; exit 0 ;; esac
	echo inactive; exit 3
fi
exit 0
`

// fakeAppManagement stands in for app-management under root (its
// recreating is tested in services/app-management): a pending restore
// becomes installing, then each app ok without its data.
func fakeAppManagement(t *testing.T, root string) {
	file := filepath.Join(root, "var/lib/nivaroos/app-management/restore.json")
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(50 * time.Millisecond):
			}
			var st jobs.AppsRestoreStatus
			if raw, err := os.ReadFile(file); err != nil || json.Unmarshal(raw, &st) != nil || st.State != "pending" {
				continue
			}
			for _, status := range []string{"installing", "ok"} {
				st.State = "running"
				for i := range st.Apps {
					st.Apps[i].Status, st.Apps[i].DataMissing = status, true
				}
				if status == "ok" {
					st.State = "done"
				}
				raw, _ := json.Marshal(st)
				_ = os.WriteFile(file+".tmp", raw, 0o600)
				_ = os.Rename(file+".tmp", file)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
}

func settingsRoot(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	root, err := os.MkdirTemp(baseDir, name+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !t.Failed() {
			os.RemoveAll(root)
		}
	})
	for p, body := range files {
		writeFile(t, filepath.Join(root, p), []byte(body))
	}
	return root
}

// TestSettingsBackupAndRestore backs up a throwaway server's settings
// with the default job and restores them into another, from the CLI
// (install.sh --restore) and from the web API (Welcome screen).
func TestSettingsBackupAndRestore(t *testing.T) {
	if engine.SevenZip() == "" {
		t.Skip("7z is not installed")
	}
	hostLog := filepath.Join(baseDir, t.Name()+"-host.log")
	hostCmd := filepath.Join(baseDir, "fake-host.sh")
	if err := os.WriteFile(hostCmd, []byte(fakeHost), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"NIVAROOS_HOST_CMD=" + hostCmd, "FAKE_HOST_LOG=" + hostLog}

	rootA := settingsRoot(t, "server-a", map[string]string{
		"usr/share/nivaroos/release.json":                 `{"version":"1.5.0"}`,
		"etc/nivaroos/gateway.ini":                        "[gateway]\nport=8081\n",
		"var/lib/nivaroos/db/user.db":                     "accounts of server A",
		"var/lib/nivaroos/apps/immich/docker-compose.yml": "services: {}\n",
		"root/.config/rclone/rclone.conf":                 "[drive]\ntype = drive\n",
		"DATA/Documents/private.txt":                      "server A's data",
	})
	dest := filepath.Join(rootA, "DATA/Backup", jobs.DefaultConfigJobName)
	settingsArgs := func(root string) []string {
		return []string{"-config-job=true", "-config-root", root, "-data-dir", filepath.Join(root, "var/lib/nivaroos/backup"), "-config-job-dest", dest}
	}
	a := newInstanceArgs(t, settingsArgs(rootA), env...)

	var list []jobs.JobListItem
	a.must("GET", "/jobs", nil, &list)
	if len(list) != 1 || list[0].Name != jobs.DefaultConfigJobName {
		t.Fatalf("jobs on a fresh server: %+v", list)
	}
	jobID := list[0].ID
	var key jobs.ConfigKey
	a.must("GET", "/jobs/"+jobID+"/settings-key", nil, &key)
	if r := a.waitRun(a.runJob(jobID), 2*time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("settings backup %s %s; log:\n%s", r.Status, r.ErrorCode, tail(a.log(), 3000))
	}
	vols, _ := filepath.Glob(filepath.Join(dest, "nivaro_"+jobID+"_*.7z.001"))
	if len(vols) != 1 {
		t.Fatalf("archives at %s: %v", dest, vols)
	}
	if entries, _ := os.ReadDir(filepath.Join(rootA, "var/lib/nivaroos/backup", jobs.ConfigBundleDir, jobID)); len(entries) != 0 {
		t.Fatal("the staged settings (host key in clear) were left behind")
	}

	rootB := settingsRoot(t, "server-b", map[string]string{
		"usr/share/nivaroos/release.json": `{"version":"1.5.0"}`,
		"etc/nivaroos/gateway.ini":        "[gateway]\nport=80\n",
		"DATA/Documents/b.txt":            "server B's data",
	})
	restore := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binPath, append([]string{"config-restore", "-root", rootB, "-file", vols[0]}, args...)...)
		cmd.Env = append(os.Environ(), append(env, "NIVAROOS_RESTORE_KEY="+key.Key)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config-restore %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	// Dry run: the plan, nothing changed, never /DATA.
	out := restore()
	for _, want := range []string{"replace  /etc/nivaroos/gateway.ini", "add      /var/lib/nivaroos/db/user.db", "add      /etc/nivaroos/secret.key", "dashboard port afterwards: 8081", "dry run"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "/DATA/") {
		t.Fatalf("the plan names /DATA:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(rootB, "etc/nivaroos/gateway.ini")); !strings.Contains(string(b), "port=80\n") {
		t.Fatal("the dry run changed gateway.ini")
	}

	fakeAppManagement(t, rootB)
	out = restore("-yes")
	for _, want := range []string{"1 app(s) to recreate", "app immich: ok", "ok         immich (data not found - restore it from your backups)"} {
		if !strings.Contains(out, want) {
			t.Errorf("restore output lacks %q:\n%s", want, out)
		}
	}
	for _, p := range []string{"etc/nivaroos/gateway.ini", "var/lib/nivaroos/db/user.db", "var/lib/nivaroos/apps/immich/docker-compose.yml", "root/.config/rclone/rclone.conf", "etc/nivaroos/secret.key"} {
		want, _ := os.ReadFile(filepath.Join(rootA, p))
		got, err := os.ReadFile(filepath.Join(rootB, p))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s not restored (%v)", p, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(rootB, "DATA/Documents/b.txt")); string(b) != "server B's data" {
		t.Fatal("/DATA changed")
	}
	if _, err := os.Stat(filepath.Join(rootB, "DATA/Documents/private.txt")); !os.IsNotExist(err) {
		t.Fatal("server A's /DATA came along")
	}
	logged, _ := os.ReadFile(hostLog)
	order := []string{"systemctl stop nivaroos-backup.service", "systemctl stop nivaroos-gateway.service",
		"systemctl start nivaroos-gateway.service", "systemctl start nivaroos-backup.service"}
	at := 0
	for _, line := range strings.Split(string(logged), "\n") {
		if at < len(order) && line == order[at] {
			at++
		}
	}
	if at != len(order) {
		t.Fatalf("services not stopped/started in order %v:\n%s\n%s", order, logged, out)
	}

	// Server B comes up with A's jobs, and A's key still opens them: the
	// host key and the job store came along.
	b := newInstanceArgs(t, settingsArgs(rootB), env...)
	var listB []jobs.JobListItem
	b.must("GET", "/jobs", nil, &listB)
	if len(listB) != 1 || listB[0].ID != jobID {
		t.Fatalf("jobs on the restored server: %+v", listB)
	}
	var keyB jobs.ConfigKey
	b.must("GET", "/jobs/"+jobID+"/settings-key", nil, &keyB)
	if keyB.Key != key.Key {
		t.Fatal("the restored job's key doesn't open")
	}

	// The web path: open (dry run), then apply hands off to a transient
	// unit running config-restore.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("key", strings.ToLower(key.Key))
	fw, _ := mw.CreateFormFile("bundle", filepath.Base(vols[0]))
	raw, _ := os.ReadFile(vols[0])
	_, _ = fw.Write(raw)
	mw.Close()
	req, _ := http.NewRequest("POST", "http://"+b.addr+jobs.APIBase+"/config-restore/open", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := b.do(req)
	if err != nil {
		t.Fatal(err)
	}
	respRaw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var env1 struct {
		Data jobs.ConfigRestoreOpened `json:"data"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(respRaw, &env1) != nil || env1.Data.Token == "" || len(env1.Data.Plan.Items) == 0 {
		t.Fatalf("open: %d %s", resp.StatusCode, respRaw)
	}
	for _, it := range env1.Data.Plan.Items {
		// Server B has been running on its job store since.
		if it.Action != "same" && it.Path != "/var/lib/nivaroos/backup/backup.db" {
			t.Errorf("after the restore %s is %s", it.Path, it.Action)
		}
	}
	var plan jobs.ConfigPlan
	if st := b.call("POST", "/config-restore/apply", jobs.ConfigRestoreApply{Token: env1.Data.Token}, &plan); st != 202 {
		t.Fatalf("apply: %d", st)
	}
	logged, _ = os.ReadFile(hostLog)
	if !strings.Contains(string(logged), "systemd-run --unit=nivaroos-config-restore-"+env1.Data.Token[:8]+" --collect --quiet -- ") ||
		!strings.Contains(string(logged), "config-restore -root "+rootB+" -dir ") {
		t.Fatalf("apply didn't start the restore unit:\n%s", logged)
	}

	// Run what the unit runs; the Welcome screen follows the apps with
	// its token (no other token, no JWT needed).
	var unit []string
	for _, line := range strings.Split(string(logged), "\n") {
		if _, cmd, ok := strings.Cut(line, "--unit=nivaroos-config-restore-"+env1.Data.Token[:8]+" --collect --quiet -- "); ok {
			unit = strings.Fields(cmd)
		}
	}
	cmd := exec.Command(unit[0], unit[1:]...)
	cmd.Env = append(os.Environ(), env...)
	if raw, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(raw), "app immich: ok") {
		t.Fatalf("the restore unit: %v\n%s", err, raw)
	}
	var apps jobs.AppsRestoreStatus
	if st := b.call("POST", "/config-restore/status", jobs.ConfigRestoreStatus{Token: env1.Data.Token}, &apps); st != 200 ||
		apps.State != "done" || len(apps.Apps) != 1 || apps.Apps[0].Name != "immich" || !apps.Apps[0].DataMissing {
		t.Fatalf("status: %d %+v", st, apps)
	}
	if st := b.call("POST", "/config-restore/status", jobs.ConfigRestoreStatus{Token: strings.Repeat("0", 32)}, nil); st != 404 {
		t.Fatalf("status with another token: %d", st)
	}
}
