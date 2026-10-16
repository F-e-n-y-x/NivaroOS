package jobs

import (
	"archive/tar"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// NivaroOS settings backup and restore (configbundle.go, configrestore.go).

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// makeDB writes a WAL-mode SQLite database with one row.
func makeDB(t *testing.T, p, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", p+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"CREATE TABLE users (name TEXT)", "INSERT INTO users VALUES ('" + value + "')"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func dbValue(t *testing.T, p string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+p+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v string
	if err := db.QueryRow("SELECT name FROM users").Scan(&v); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return v
}

// sourceRoot is a server's settings plus things that must stay out.
func sourceRoot(t *testing.T) string {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"usr/share/nivaroos/release.json":                  `{"version":"1.5.0","commit":"abc"}`,
		"etc/nivaroos/gateway.ini":                         "[gateway]\nport=8080\n",
		"etc/nivaroos/secret.key":                          "hostkey",
		"etc/nivaroos/casaos.conf.sample":                  "sample",
		"var/lib/nivaroos/db/jwt_key.pem":                  "jwt",
		"var/lib/nivaroos/1/wallpaper.json":                `{"url":"x"}`,
		"var/lib/nivaroos/apps/immich/docker-compose.yml":  "services: {}",
		"var/lib/nivaroos/update/settings.json":            `{"channel":"stable"}`,
		"var/lib/nivaroos/smart-history.json":              "not a setting",
		"root/.config/rclone/rclone.conf":                  "[drive]\ntype = drive\n",
		"DATA/Documents/private.txt":                       "user data",
		"DATA/AppData/immich/library.db":                   "app data",
		"etc/samba/smb.nivaroos.conf":                      "[Media]\npath = /DATA/Media\n",
		"var/lib/nivaroos/companion_devices.json":          "[]",
		"var/lib/nivaroos/db/user.db-wal":                  "stale",
		"var/lib/nivaroos/apps/immich/docker-compose.yml~": "editor backup",
	})
	makeDB(t, filepath.Join(root, "var/lib/nivaroos/db/user.db"), "ayush")
	return root
}

func collected(t *testing.T, man *ConfigManifest) []string {
	var got []string
	for _, f := range man.Files {
		got = append(got, f.Path)
	}
	return got
}

func TestCollectConfigTakesSettingsOnly(t *testing.T) {
	src := sourceRoot(t)
	out := filepath.Join(t.TempDir(), "bundle")
	man, err := CollectConfig(src, out, time.Date(2026, 10, 16, 3, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"etc/nivaroos/gateway.ini", "etc/nivaroos/secret.key", "etc/samba/smb.nivaroos.conf",
		"root/.config/rclone/rclone.conf", "var/lib/nivaroos/1/wallpaper.json",
		"var/lib/nivaroos/apps/immich/docker-compose.yml", "var/lib/nivaroos/apps/immich/docker-compose.yml~",
		"var/lib/nivaroos/companion_devices.json", "var/lib/nivaroos/db/jwt_key.pem", "var/lib/nivaroos/db/user.db",
		"var/lib/nivaroos/update/settings.json",
	}
	if got := collected(t, man); !reflect.DeepEqual(got, want) {
		t.Fatalf("collected\n%v\nwant\n%v", got, want)
	}
	if man.Version != "1.5.0" || man.Commit != "abc" || man.Kind != configKind {
		t.Fatalf("manifest %+v", man)
	}
	// The database copy is consistent and standalone (no -wal needed).
	if v := dbValue(t, filepath.Join(out, configFilesDir, "var/lib/nivaroos/db/user.db")); v != "ayush" {
		t.Fatalf("db copy has %q", v)
	}
	if _, err := LoadConfigBundle(out); err != nil {
		t.Fatalf("own bundle doesn't load: %v", err)
	}
}

func TestConfigAllowed(t *testing.T) {
	for p, want := range map[string]bool{
		"etc/nivaroos/gateway.ini":              true,
		"etc/nivaroos/sub/x":                    true,
		"var/lib/nivaroos/12/wallpaper.json":    true,
		"var/lib/nivaroos/db/user.db":           true,
		"var/lib/nivaroos/db/user.db-wal":       false,
		"var/lib/nivaroos/db/other.db":          false,
		"etc/passwd":                            false,
		"etc/nivaroos/../passwd":                false,
		"/etc/nivaroos/gateway.ini":             false,
		"DATA/Backup/x":                         false,
		"../etc/nivaroos/gateway.ini":           false,
		"var/lib/samba/private/passdb.tdb":      true,
		"var/lib/nivaroos/companion_x.json":     true,
		"var/lib/nivaroos/backup/logs/x.jsonl":  false,
		"root/.config/rclone/rclone.conf.extra": false,
	} {
		if got := configAllowed(p); got != want {
			t.Errorf("configAllowed(%q) = %v, want %v", p, got, want)
		}
	}
}

// fakeHost records host commands; units in active answer "active".
type fakeHost struct {
	active map[string]bool
	calls  []string
	fail   string // a call that fails
}

func (f *fakeHost) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	if !strings.HasPrefix(call, "systemctl is-active") {
		f.calls = append(f.calls, call)
	}
	if call == f.fail {
		return []byte("boom"), fmt.Errorf("exit status 1")
	}
	if name == "systemctl" && args[0] == "is-active" {
		if f.active[args[1]] {
			return []byte("active\n"), nil
		}
		return []byte("inactive\n"), fmt.Errorf("exit status 3")
	}
	return nil, nil
}

func targetRoot(t *testing.T, version string) string {
	dst := t.TempDir()
	writeTree(t, dst, map[string]string{
		"usr/share/nivaroos/release.json": `{"version":"` + version + `"}`,
		"etc/nivaroos/gateway.ini":        "[gateway]\nport=80\n",
		"etc/nivaroos/new-only.conf":      "kept",
		"DATA/Documents/private.txt":      "this server's data",
		"var/lib/nivaroos/db/user.db-wal": "wal of the fresh install's db",
	})
	makeDB(t, filepath.Join(dst, "var/lib/nivaroos/db/user.db"), "fresh-admin")
	return dst
}

func TestConfigRestorePlanAndApply(t *testing.T) {
	src := sourceRoot(t)
	bundle := filepath.Join(t.TempDir(), "bundle")
	man, err := CollectConfig(src, bundle, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dst := targetRoot(t, "1.5.1")
	host := &fakeHost{active: map[string]bool{
		"nivaroos-gateway.service": true, "nivaroos-user-service.service": true,
		"nivaroos-backup.service": true, "nivaroos-host-firewall.service": true,
	}}
	c := &ConfigRestorer{Root: dst, Run: host.run, Now: func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) }}

	plan := c.Plan(context.Background(), bundle, man)
	if plan.Problem != "" || plan.GatewayPort != "8080" || plan.FromVersion != "1.5.0" || plan.InstalledVersion != "1.5.1" {
		t.Fatalf("plan %+v", plan)
	}
	actions := map[string]string{}
	for _, it := range plan.Items {
		actions[it.Path] = it.Action
	}
	if actions["/etc/nivaroos/gateway.ini"] != "replace" || actions["/etc/nivaroos/secret.key"] != "add" || actions["/var/lib/nivaroos/db/user.db"] != "replace" {
		t.Fatalf("actions %v", actions)
	}
	if len(host.calls) != 0 {
		t.Fatalf("the plan ran %v", host.calls)
	}
	if got := readFile(t, filepath.Join(dst, "etc/nivaroos/gateway.ini")); !strings.Contains(got, "port=80\n") {
		t.Fatalf("the plan changed gateway.ini: %q", got)
	}

	if err := c.Apply(context.Background(), bundle, man); err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{
		"systemctl stop nivaroos-backup.service",
		"systemctl stop nivaroos-user-service.service",
		"systemctl stop nivaroos-gateway.service",
		"systemctl try-restart smbd nmbd smb nmb",
		"systemctl start nivaroos-gateway.service",
		"systemctl start nivaroos-user-service.service",
		"systemctl start nivaroos-backup.service",
		"systemctl restart nivaroos-host-firewall.service",
	}
	if !reflect.DeepEqual(host.calls, wantCalls) {
		t.Fatalf("host calls\n%v\nwant\n%v", strings.Join(host.calls, "\n"), strings.Join(wantCalls, "\n"))
	}
	for _, p := range []string{"etc/nivaroos/gateway.ini", "etc/nivaroos/secret.key", "var/lib/nivaroos/apps/immich/docker-compose.yml", "root/.config/rclone/rclone.conf"} {
		if readFile(t, filepath.Join(dst, p)) != readFile(t, filepath.Join(src, p)) {
			t.Errorf("%s not restored", p)
		}
	}
	if v := dbValue(t, filepath.Join(dst, "var/lib/nivaroos/db/user.db")); v != "ayush" {
		t.Fatalf("user.db has %q", v)
	}
	if _, err := os.Stat(filepath.Join(dst, "var/lib/nivaroos/db/user.db-wal")); !os.IsNotExist(err) {
		t.Fatalf("the old database's -wal survived: %v", err)
	}
	if got := readFile(t, filepath.Join(dst, "DATA/Documents/private.txt")); got != "this server's data" {
		t.Fatalf("/DATA changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "DATA/AppData")); !os.IsNotExist(err) {
		t.Fatal("app data was restored")
	}
	if readFile(t, filepath.Join(dst, "etc/nivaroos/new-only.conf")) != "kept" {
		t.Fatal("a file the backup doesn't have was touched")
	}
	aside := filepath.Join(dst, "var/lib/nivaroos/config-restore/before-20261016-120000")
	if got := readFile(t, filepath.Join(aside, "etc/nivaroos/gateway.ini")); !strings.Contains(got, "port=80\n") {
		t.Fatalf("replaced gateway.ini not kept aside: %q", got)
	}
	if st, _ := os.Stat(filepath.Join(dst, "etc/nivaroos/secret.key")); st.Mode().Perm() != 0o600 {
		t.Fatalf("secret.key mode %v", st.Mode())
	}
}

func TestConfigRestoreRefusesANewerBackup(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	man, err := CollectConfig(sourceRoot(t), bundle, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dst := targetRoot(t, "1.4.9")
	host := &fakeHost{}
	c := &ConfigRestorer{Root: dst, Run: host.run}
	if p := c.Plan(context.Background(), bundle, man); !strings.Contains(p.Problem, "update NivaroOS first") {
		t.Fatalf("plan %+v", p)
	}
	if err := c.Apply(context.Background(), bundle, man); err == nil || len(host.calls) != 0 {
		t.Fatalf("applied a newer backup: %v %v", err, host.calls)
	}
	if !strings.Contains(readFile(t, filepath.Join(dst, "etc/nivaroos/gateway.ini")), "port=80") {
		t.Fatal("changed anyway")
	}
	c.Force = true
	if err := c.Apply(context.Background(), bundle, man); err != nil {
		t.Fatalf("-force: %v", err)
	}
}

func TestConfigRestoreStopFailureChangesNothing(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	man, _ := CollectConfig(sourceRoot(t), bundle, time.Now())
	dst := targetRoot(t, "1.5.0")
	host := &fakeHost{active: map[string]bool{"nivaroos-gateway.service": true, "nivaroos-user-service.service": true},
		fail: "systemctl stop nivaroos-gateway.service"}
	c := &ConfigRestorer{Root: dst, Run: host.run}
	if err := c.Apply(context.Background(), bundle, man); err == nil {
		t.Fatal("no error")
	}
	if !strings.Contains(readFile(t, filepath.Join(dst, "etc/nivaroos/gateway.ini")), "port=80") {
		t.Fatal("files changed although a service didn't stop")
	}
	if last := host.calls[len(host.calls)-1]; last != "systemctl start nivaroos-user-service.service" {
		t.Fatalf("services not started again: %v", host.calls)
	}
}

func TestLoadConfigBundleRejectsTampering(t *testing.T) {
	for name, edit := range map[string]func(dir string, m *ConfigManifest){
		"outside paths": func(dir string, m *ConfigManifest) {
			m.Files = append(m.Files, ConfigFile{Path: "etc/passwd"})
		},
		"data": func(dir string, m *ConfigManifest) {
			m.Files = append(m.Files, ConfigFile{Path: "DATA/x"})
		},
		"changed file": func(dir string, m *ConfigManifest) {
			_ = os.WriteFile(filepath.Join(dir, configFilesDir, "etc/nivaroos/gateway.ini"), []byte("[gateway]\nport=1\n"), 0o600)
		},
		"kind":         func(dir string, m *ConfigManifest) { m.Kind = "other" },
		"newer format": func(dir string, m *ConfigManifest) { m.Format = configFormat + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "bundle")
			man, err := CollectConfig(sourceRoot(t), dir, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			edit(dir, man)
			raw, _ := json.Marshal(man)
			_ = os.WriteFile(filepath.Join(dir, configManifestName), raw, 0o600)
			if _, err := LoadConfigBundle(dir); engine.CodeOf(err) != ErrValidation {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
}

// sevenZipBundle archives an opened bundle the way an encrypted archive
// job does: a tar inside an AES-256 7z with encrypted headers.
func sevenZipBundle(t *testing.T, bundle, pw string) string {
	t.Helper()
	if engine.SevenZip() == "" {
		t.Skip("7z is not installed")
	}
	dir := t.TempDir()
	tf, err := os.Create(filepath.Join(dir, "b.tar"))
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(tf)
	if err := tw.AddFS(os.DirFS(bundle)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	tf.Close()
	out := filepath.Join(dir, "nivaro_bk_x_20261016T033000Z.7z")
	cmd := exec.Command(engine.SevenZip(), "a", "-bd", "-mhe=on", "-p"+pw, out, filepath.Join(dir, "b.tar"))
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z: %v %s", err, b)
	}
	return out
}

func TestOpenConfigBundleKeys(t *testing.T) {
	src := filepath.Join(t.TempDir(), "bundle")
	if _, err := CollectConfig(sourceRoot(t), src, time.Now()); err != nil {
		t.Fatal(err)
	}
	key := newRecoveryKey()
	file := sevenZipBundle(t, src, key)
	ctx := context.Background()
	for _, secret := range []string{key, strings.ToLower(strings.ReplaceAll(key, "-", " "))} {
		dir := filepath.Join(t.TempDir(), "open")
		if err := OpenConfigBundle(ctx, file, secret, nil, dir); err != nil {
			t.Fatalf("%q: %v", secret, err)
		}
		if _, err := LoadConfigBundle(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := OpenConfigBundle(ctx, file, "wrong password", nil, t.TempDir()); engine.CodeOf(err) != ErrWrongPassword {
		t.Fatalf("wrong key: %v", err)
	}

	// A job with the user's own password and a recovery key: the key
	// file beside the backup turns the recovery key into the password.
	keys, recovery, err := newCryptKeys(engine.EncryptArchive, "my own password", true)
	if err != nil {
		t.Fatal(err)
	}
	file = sevenZipBundle(t, src, keys.Password)
	if err := OpenConfigBundle(ctx, file, recovery, keys.KeyFile, filepath.Join(t.TempDir(), "o")); err != nil {
		t.Fatalf("recovery key: %v", err)
	}
	if err := OpenConfigBundle(ctx, file, recovery, nil, t.TempDir()); engine.CodeOf(err) != ErrWrongPassword {
		t.Fatalf("recovery key without the key file: %v", err)
	}
}

func TestConfigSourceMustBeAnEncryptedArchive(t *testing.T) {
	ok := sampleJob("settings")
	ok.Type = TypeArchive
	ok.Sources[0].Preset = PresetConfig
	ok.Dest.Encryption = &engine.Encryption{Mode: engine.EncryptArchive}
	if engine.SevenZip() != "" {
		if _, fe := NormalizeJob(ok, ValidateEnv{}); len(fe) > 0 {
			t.Fatalf("valid settings job: %v", fe)
		}
	}
	plain := ok
	plain.Dest.Encryption = nil
	copyJob := ok
	copyJob.Type = TypeCopy
	copyJob.Dest.Encryption = &engine.Encryption{Mode: engine.EncryptFolder}
	two := ok
	two.Sources = append([]Endpoint{}, ok.Sources...)
	two.Sources = append(two.Sources, Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: "etc"})
	for name, c := range map[string]struct {
		j     Job
		field string
	}{"unencrypted": {plain, "dest.encryption"}, "copy": {copyJob, "type"}, "two sources": {two, "sources[0]"}} {
		if _, fe := NormalizeJob(c.j, ValidateEnv{}); fe[c.field] == "" {
			t.Errorf("%s: no error on %s: %v", name, c.field, fe)
		}
	}
}

// The default job: created once, its key shown only to admins, the
// settings staged for the run and wiped after it.
func TestDefaultConfigJob(t *testing.T) {
	if engine.SevenZip() == "" {
		t.Skip("7z is not installed")
	}
	root := sourceRoot(t)
	dest := filepath.Join(root, "DATA/Backup", DefaultConfigJobName)
	h := newHarness(t, false, func(c *Config, h *harness) {
		c.ConfigJob, c.ConfigRoot, c.ConfigJobDest = true, root, dest
	})
	h.svc.store.Secrets = testBox(t, 9)
	var stagedFiles int
	h.eng.ResolvePathFn = func(req engine.ResolvePathRequest) (engine.ResolvePathResult, error) {
		if strings.Contains(req.Path, ConfigBundleDir) {
			if raw, err := os.ReadFile(filepath.Join(req.Path, configManifestName)); err == nil {
				var m ConfigManifest
				_ = json.Unmarshal(raw, &m)
				stagedFiles = len(m.Files)
			}
			return engine.ResolvePathResult{OK: true, Endpoint: &Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: strings.TrimPrefix(req.Path, "/")}}, nil
		}
		return engine.ResolvePathResult{OK: true, Endpoint: &Endpoint{Kind: EPVolume, RefID: "data-uuid", SubPath: "Backup/NivaroOS settings"}}, nil
	}
	h.start()
	jobs, _ := h.svc.store.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("%d jobs", len(jobs))
	}
	j := jobs[0]
	if j.Name != DefaultConfigJobName || j.Type != TypeArchive || !j.Enabled || j.Sources[0].Preset != PresetConfig ||
		j.Dest.Encryption == nil || j.Dest.Encryption.Mode != engine.EncryptArchive || j.Retention.KeepLast != 14 ||
		len(j.Triggers) != 1 || j.Triggers[0].Cron != "30 3 * * *" {
		t.Fatalf("default job %+v", j)
	}

	rec := doRequest(h.svc, "GET", "/v1/backup/jobs/"+j.ID+"/settings-key", nil, reqOpts{})
	wantStatus(t, rec, 200)
	var key ConfigKey
	envelope(t, rec, &key)
	if key.Key == "" || key.Key != j.Dest.Encryption.Keys.Password {
		t.Fatalf("key %q", key.Key)
	}
	other := h.createJob(sampleJob("docs"))
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/jobs/"+other.ID+"/settings-key", nil, reqOpts{}), 404)
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/jobs/"+j.ID+"/settings-key", nil, reqOpts{remote: "192.168.1.9:5000"}), 401)

	runID := h.run(j, KindBackup)
	_, req := h.startedJob(1)
	if stagedFiles == 0 || req.Sources[0].Preset != PresetConfig || req.Sources[0].RefID != "root-uuid" {
		t.Fatalf("staged %d files, source %+v", stagedFiles, req.Sources)
	}
	h.finish(1, okResult(3))
	h.finish(2, engine.Result{}) // keep the last 14
	if r := h.waitStatus(runID, StatusSuccess, StatusFailed, StatusPartial); RunStatus(r.Status) != StatusSuccess {
		t.Fatalf("run %s %s", r.Status, r.ErrorCode)
	}
	if entries, _ := os.ReadDir(h.svc.configStageDir(j.ID)); len(entries) != 0 {
		t.Fatalf("staged settings left behind: %v", entries)
	}

	// Deleted stays deleted.
	h.stop()
	h.svc.store = nil
	h2 := newHarness(t, false, func(c *Config, hh *harness) {
		c.DataDir, c.ConfigJob, c.ConfigRoot, c.ConfigJobDest = h.svc.cfg.DataDir, true, root, dest
	})
	if _, err := h2.svc.store.DeleteJob(j.ID, ""); err != nil {
		t.Fatal(err)
	}
	h2.start()
	if jobs, _ := h2.svc.store.ListJobs(); len(jobs) != 1 || jobs[0].ID != other.ID {
		t.Fatalf("after delete + restart: %+v", jobs)
	}
}

func TestConfigRestoreRoutesNeedAdminUnlessFresh(t *testing.T) {
	fresh := false
	h := newHarness(t, true, func(c *Config, h *harness) {
		c.FreshInstall = func(context.Context) bool { return fresh }
	})
	lan := reqOpts{remote: "192.168.1.9:5000"}
	body := ConfigRestoreApply{Token: strings.Repeat("ab", 16)}
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/config-restore/apply", body, lan), 401)
	fresh = true
	// Past auth: no such opened bundle.
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/config-restore/apply", body, lan), 400)
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/jobs", nil, lan), 401)
}
