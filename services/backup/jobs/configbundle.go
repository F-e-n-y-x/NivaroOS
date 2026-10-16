package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
)

// NivaroOS settings backup (feature gaps 2026-10, item 4): an archive job
// whose only source is the PresetConfig preset. Before each run the job
// side stages this server's settings into DataDir/config-bundle/<job> -
// the files of configPaths plus manifest.json - and the run archives that
// folder like any other, always as an encrypted 7z (checkConfigSource).
// Never anything under /DATA. Restore: configrestore.go, the
// config-restore subcommand, POST /config-restore/{open,apply} and
// install.sh --restore.

const (
	ConfigBundleDir    = "config-bundle"  // under DataDir: the staged bundle a run archives
	ConfigRestoreDir   = "config-restore" // under DataDir: opened bundles waiting to be applied
	configManifestName = "manifest.json"
	configFilesDir     = "files"
	configFormat       = 1
	configKind         = "nivaroos-config"
	// DefaultConfigJobName / DefaultConfigJobDest: the job a fresh install
	// gets (Config.ConfigJob).
	DefaultConfigJobName = "NivaroOS settings"
	DefaultConfigJobDest = "/DATA/Backup/NivaroOS settings"
	metaConfigJob        = "config.default_job" // its id: the one job whose key the UI may show
	metaConfigSeeded     = "config.seeded"      // set once, so a deleted default job stays deleted
	maxConfigFileBytes   = 32 << 20
	// passdbPath holds the SMB users. It is never copied over the live
	// one: a restore creates missing system users and imports it with
	// pdbedit.
	passdbPath = "var/lib/samba/private/passdb.tdb"
)

// configPaths are what a settings backup holds: globs relative to the
// root; a directory is taken whole. Restores only ever write paths these
// match (configAllowed).
var configPaths = []string{
	"etc/nivaroos",                            // every service's config: gateway port and HTTPS switch, Host Desktop, the host secret key (opens every sealed secret)
	"var/lib/nivaroos/db/user.db",             // accounts
	"var/lib/nivaroos/db/jwt_key.pem",         // signs sessions: browsers and paired phones stay logged in
	"var/lib/nivaroos/db/casaOS.db",           // core: shares, saved network shares, settings
	"var/lib/nivaroos/db/local-storage.db",    // drives and pools
	"var/lib/nivaroos/db/local-storage.json",  //
	"var/lib/nivaroos/backup/backup.db",       // Backup & Sync jobs and their sealed keys
	"var/lib/nivaroos/[0-9]*",                 // each account's desktop settings (wallpaper, dock, widgets)
	"var/lib/nivaroos/apps",                   // installed apps' compose files (not their data)
	"var/lib/nivaroos/companion_*.json",       // paired phones
	"var/lib/nivaroos/revoked_sessions.json",  //
	"var/lib/nivaroos/user-sessions.json",     //
	"var/lib/nivaroos/schedules.json",         // Scheduled Tasks
	"var/lib/nivaroos/container_updates.json", // app update settings
	"var/lib/nivaroos/trash-roots.json",       //
	"var/lib/nivaroos/fans/settings.json",     //
	"var/lib/nivaroos/update/settings.json",   // updater channel and window
	"var/lib/nivaroos/tls",                    // HTTPS certificates
	"root/.config/rclone/rclone.conf",         // cloud accounts
	"etc/samba/smb.conf",                      //
	"etc/samba/smb.nivaroos.conf",             // shares
	passdbPath,                                // SMB users
}

// ConfigManifest is manifest.json of a settings bundle.
type ConfigManifest struct {
	Format   int          `json:"format"`
	Kind     string       `json:"kind"`
	Version  string       `json:"version"` // NivaroOS release it came from ("" unknown)
	Commit   string       `json:"commit,omitempty"`
	Hostname string       `json:"hostname"`
	Created  time.Time    `json:"created"`
	Files    []ConfigFile `json:"files"`
}

// ConfigFile is one file of a bundle, at files/<Path>.
type ConfigFile struct {
	Path   string `json:"path"` // relative to the root, slash separated
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"` // permission bits
	SHA256 string `json:"sha256"`
}

// releaseInfo reads release.json under root.
func releaseInfo(root string) (version, commit string) {
	raw, err := os.ReadFile(filepath.Join(root, "usr/share/nivaroos/release.json"))
	if err != nil {
		return "", ""
	}
	var r struct{ Version, Commit string }
	if json.Unmarshal(raw, &r) != nil {
		return "", ""
	}
	return strings.TrimPrefix(r.Version, "v"), r.Commit
}

// CollectConfig stages root's settings into out (emptied first).
// Unreadable files are skipped with a log line rather than failing the
// backup; SQLite databases are copied with VACUUM INTO, so a database in
// use is still a consistent copy.
func CollectConfig(root, out string, now time.Time) (*ConfigManifest, error) {
	if err := os.RemoveAll(out); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(out, configFilesDir), 0o700); err != nil {
		return nil, err
	}
	man := &ConfigManifest{Format: configFormat, Kind: configKind, Created: now.UTC(), Files: []ConfigFile{}}
	man.Version, man.Commit = releaseInfo(root)
	man.Hostname, _ = os.Hostname()
	seen := map[string]bool{}
	for _, pat := range configPaths {
		matches, _ := filepath.Glob(filepath.Join(root, pat))
		for _, m := range matches {
			_ = filepath.WalkDir(m, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					log.Printf("backup: settings: skipped %s: %v", p, err)
					return nil
				}
				rel, _ := filepath.Rel(root, p)
				rel = filepath.ToSlash(rel)
				if !d.Type().IsRegular() || seen[rel] || skipConfigName(d.Name()) {
					return nil
				}
				seen[rel] = true
				f, err := collectFile(p, filepath.Join(out, configFilesDir, rel))
				if err != nil {
					log.Printf("backup: settings: skipped %s: %v", p, err)
					return nil
				}
				f.Path = rel
				man.Files = append(man.Files, f)
				return nil
			})
		}
	}
	sort.Slice(man.Files, func(i, j int) bool { return man.Files[i].Path < man.Files[j].Path })
	raw, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return nil, err
	}
	return man, writeFileAtomic(filepath.Join(out, configManifestName), raw, 0o600)
}

func skipConfigName(n string) bool {
	for _, s := range []string{".sample", "-wal", "-shm", "-journal"} {
		if strings.HasSuffix(n, s) {
			return true
		}
	}
	return false
}

func collectFile(src, dst string) (ConfigFile, error) {
	st, err := os.Stat(src)
	if err != nil {
		return ConfigFile{}, err
	}
	if st.Size() > maxConfigFileBytes {
		return ConfigFile{}, fmt.Errorf("%d MiB is over the %d MiB a settings file may have", st.Size()>>20, maxConfigFileBytes>>20)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return ConfigFile{}, err
	}
	if strings.HasSuffix(src, ".db") {
		if err := vacuumInto(src, dst); err == nil {
			return describeConfigFile(dst, st.Mode())
		}
		_ = os.Remove(dst) // not SQLite after all, or locked: copy the bytes
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return ConfigFile{}, err
	}
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		return ConfigFile{}, err
	}
	return describeConfigFile(dst, st.Mode())
}

func vacuumInto(src, dst string) error {
	db, err := sql.Open("sqlite", "file:"+src+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("VACUUM INTO ?", dst)
	return err
}

func describeConfigFile(p string, mode fs.FileMode) (ConfigFile, error) {
	h, n, err := hashFile(p)
	return ConfigFile{Size: n, Mode: uint32(mode.Perm()), SHA256: h}, err
}

func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// ---------------------------------------------------------------------
// The job side

// checkConfigSource: a settings source holds every account and the host
// key, so it only goes into an encrypted archive, alone.
func checkConfigSource(j Job, fe fieldErrors) {
	for i, s := range j.Sources {
		if s.Preset != PresetConfig {
			continue
		}
		if len(j.Sources) != 1 {
			fe.add(fmt.Sprintf("sources[%d]", i), FieldTooMany)
		}
		if j.Type != TypeArchive {
			fe.add("type", FieldInvalid)
		}
		if j.Dest.Encryption == nil || j.Dest.Encryption.Mode != engine.EncryptArchive {
			fe.add("dest.encryption", FieldRequired)
		}
	}
}

func hasConfigSource(j Job) bool { return len(j.Sources) == 1 && j.Sources[0].Preset == PresetConfig }

func (s *Service) configRoot() string {
	if s.cfg.ConfigRoot != "" {
		return s.cfg.ConfigRoot
	}
	return "/"
}

func (s *Service) configStageDir(jobID string) string {
	return filepath.Join(s.store.DataDir(), ConfigBundleDir, safeName(jobID))
}

// stageConfig collects the settings and points the run's source at them:
// the stored endpoint may be another machine's (a restored job).
func (x *runExec) stageConfig(ctx context.Context) ErrorCode {
	dir := x.s.configStageDir(x.job.ID)
	man, err := CollectConfig(x.s.configRoot(), dir, x.now())
	if err != nil {
		x.log.Raw(engine.LogError, "staging the NivaroOS settings: "+err.Error())
		return ErrIOError
	}
	res, err := x.s.engine.ResolvePath(ctx, engine.ResolvePathRequest{Path: dir})
	if err != nil || !res.OK || res.Endpoint == nil {
		x.log.Raw(engine.LogError, fmt.Sprintf("the settings folder %s can't be resolved: %v %s", dir, err, res.Reason))
		return ErrEndpointUnknown
	}
	ep := *res.Endpoint
	ep.Preset, ep.Label = PresetConfig, x.job.Sources[0].Label
	x.job.Sources[0] = ep
	x.log.Raw(engine.LogInfo, fmt.Sprintf("NivaroOS settings: %d files staged (NivaroOS %s)", len(man.Files), orUnknown(man.Version)))
	return ""
}

// unstageConfig empties the staged folder (it holds the host key in
// clear) and keeps it, so the job's source still resolves between runs.
func (x *runExec) unstageConfig() {
	dir := x.s.configStageDir(x.job.ID)
	_ = os.RemoveAll(dir)
	_ = os.MkdirAll(dir, 0o700)
}

func orUnknown(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

// ensureConfigJob creates the default "NivaroOS settings" job once: daily,
// encrypted with a generated key the UI shows (GET
// /jobs/{id}/settings-key), to DefaultConfigJobDest. A failure (no /DATA
// yet, 7z missing) is tried again at the next start.
func (s *Service) ensureConfigJob(ctx context.Context) {
	if v, ok, err := s.store.GetMeta(metaConfigSeeded); err != nil || (ok && v != "") {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	j, err := s.defaultConfigJob(ctx)
	if err != nil {
		log.Printf("backup: the default NivaroOS settings job isn't created yet: %v", err)
		return
	}
	keys, _, err := newCryptKeys(engine.EncryptArchive, newRecoveryKey(), false)
	if err == nil {
		err = s.store.SaveCryptKeys(j.ID, keys)
	}
	if err == nil {
		_, err = s.store.CreateJob(j, s.now())
	}
	if err != nil {
		_ = s.store.DropCryptKeys(j.ID)
		log.Printf("backup: creating the NivaroOS settings job: %v", err)
		return
	}
	_ = s.store.SetMeta(metaConfigJob, j.ID)
	_ = s.store.SetMeta(metaConfigSeeded, s.now().UTC().Format(time.RFC3339))
	log.Printf("backup: created the daily %q job %s", j.Name, j.ID)
}

func (s *Service) defaultConfigJob(ctx context.Context) (Job, error) {
	id := NewJobID()
	stage := s.configStageDir(id)
	dest := s.cfg.ConfigJobDest
	if dest == "" {
		dest = DefaultConfigJobDest
	}
	if _, err := os.Stat(filepath.Dir(filepath.Dir(dest))); err != nil {
		return Job{}, err // no /DATA: never create it on the system disk
	}
	var eps [2]Endpoint
	for i, p := range []string{stage, dest} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			return Job{}, err
		}
		res, err := s.engine.ResolvePath(ctx, engine.ResolvePathRequest{Path: p})
		if err != nil {
			return Job{}, err
		}
		if !res.OK || res.Endpoint == nil {
			return Job{}, fmt.Errorf("%s: %s", p, res.Reason)
		}
		eps[i] = *res.Endpoint
	}
	src, dst := eps[0], eps[1]
	src.Preset, src.Label = PresetConfig, DefaultConfigJobName
	dst.Encryption = &engine.Encryption{Mode: engine.EncryptArchive}
	j, fe := s.normalizeJob(ctx, Job{
		Name: DefaultConfigJobName, Type: TypeArchive, Enabled: true,
		Sources: []Endpoint{src}, Dest: dst,
		Triggers:   []Trigger{{Kind: TriggerSchedule, Cron: "30 3 * * *", CatchUp: true}},
		Conditions: Conditions{DestAvailable: true},
		Retention:  Retention{KeepLast: 14},
	}, "")
	if len(fe) > 0 {
		return Job{}, fmt.Errorf("invalid: %v", fe)
	}
	j.ID = id
	return j, nil
}

// ---------------------------------------------------------------------
// API

// ConfigKey is GET /jobs/{id}/settings-key: the generated key of the
// default settings job. It is the 7z password (and works as a recovery
// key): the one thing needed to restore on a new server.
type ConfigKey struct {
	Key string `json:"key"`
}

func (s *Service) handleConfigKey(w http.ResponseWriter, r *http.Request) {
	if !callerOf(r).Admin {
		writeError(w, http.StatusForbidden, ErrorBody{ErrorCode: ErrForbidden})
		return
	}
	id := r.PathValue("id")
	if def, ok, _ := s.store.GetMeta(metaConfigJob); !ok || def != id {
		// A job made with the user's own password: that is never sent back.
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: "only the default settings job has a generated key"})
		return
	}
	j, err := s.store.GetJob(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if j.Dest.Encryption == nil || j.Dest.Encryption.Keys == nil {
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrEncryptionLocked})
		return
	}
	s.audit(r, "settings_key", id, "")
	writeOK(w, http.StatusOK, ConfigKey{Key: j.Dest.Encryption.Keys.Password})
}

// freshInstall reports whether no account exists yet (user-service's
// /v1/users/status): the Welcome screen may then restore settings
// without a login, as it may create the first account.
func (s *Service) freshInstall(ctx context.Context) bool {
	if s.cfg.FreshInstall != nil {
		return s.cfg.FreshInstall(ctx)
	}
	addr, err := runtimeAddr(s.cfg.RuntimePath, external.UserServiceAddressFilename)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/v1/users/status", nil)
	resp, err := loopbackClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		Data struct {
			Initialized *bool `json:"initialized"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil {
		return false
	}
	return body.Data.Initialized != nil && !*body.Data.Initialized
}

// ConfigRestoreOpened is POST /config-restore/open: the dry run.
type ConfigRestoreOpened struct {
	Token string     `json:"token"`
	Plan  ConfigPlan `json:"plan"`
}

// ConfigRestoreApply is POST /config-restore/apply.
type ConfigRestoreApply struct {
	Token string `json:"token"`
}

// maxConfigUpload caps an uploaded bundle (they are a few MB).
const maxConfigUpload = 256 << 20

// handleConfigRestoreOpen takes a multipart upload - "bundle" (the
// .7z.001), "key" (password or recovery key), optional "key_file"
// (.nivaroos-encryption.json, for a recovery key) - opens it into
// DataDir/config-restore/<token> and answers the plan. Nothing is
// changed until apply.
func (s *Service) handleConfigRestoreOpen(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	base := filepath.Join(s.store.DataDir(), ConfigRestoreDir)
	pruneOld(base, time.Hour, s.now())
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigUpload)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, ErrorBody{ErrorCode: ErrValidation, Detail: "a multipart form with the bundle is needed: " + err.Error()})
		return
	}
	defer r.MultipartForm.RemoveAll()
	up, _, err := r.FormFile("bundle")
	if err != nil {
		writeValidation(w, map[string]string{"bundle": string(FieldRequired)})
		return
	}
	defer up.Close()
	var keyFile []byte
	if kf, _, err := r.FormFile("key_file"); err == nil {
		keyFile, _ = io.ReadAll(io.LimitReader(kf, 1<<20))
		kf.Close()
	}
	token := randomHex(16)
	dir := filepath.Join(base, token)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.fail(w, err)
		return
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(dir)
		}
	}()
	enc := filepath.Join(dir, "upload.7z.001")
	f, err := os.OpenFile(enc, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err == nil {
		_, err = io.Copy(f, up)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	bundle := filepath.Join(dir, "bundle")
	err = OpenConfigBundle(r.Context(), enc, r.FormValue("key"), keyFile, bundle)
	_ = os.Remove(enc)
	if err != nil {
		s.fail(w, err)
		return
	}
	man, err := LoadConfigBundle(bundle)
	if err != nil {
		s.fail(w, err)
		return
	}
	rs := ConfigRestorer{Root: s.configRoot()}
	plan := rs.Plan(r.Context(), bundle, man)
	ok = true
	s.audit(r, "settings_restore_open", "", fmt.Sprintf("files=%d from=%s", len(man.Files), man.Version))
	writeOK(w, http.StatusOK, ConfigRestoreOpened{Token: token, Plan: plan})
}

// handleConfigRestoreApply starts the restore outside this service (it
// stops every NivaroOS service, this one too): a transient systemd unit
// running `nivaroos-backup config-restore -dir ... -yes`.
func (s *Service) handleConfigRestoreApply(w http.ResponseWriter, r *http.Request) {
	var req ConfigRestoreApply
	if !decode(w, r, &req) {
		return
	}
	if len(req.Token) != 32 || strings.Trim(req.Token, "0123456789abcdef") != "" {
		writeValidation(w, map[string]string{"token": string(FieldInvalid)})
		return
	}
	bundle := filepath.Join(s.store.DataDir(), ConfigRestoreDir, req.Token, "bundle")
	man, err := LoadConfigBundle(bundle)
	if err != nil {
		s.fail(w, err)
		return
	}
	rs := ConfigRestorer{Root: s.configRoot()}
	plan := rs.Plan(r.Context(), bundle, man)
	if plan.Problem != "" {
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrInvalidState, Detail: plan.Problem})
		return
	}
	exe, err := os.Executable()
	if err != nil {
		s.fail(w, err)
		return
	}
	out, err := rs.host(r.Context(), "systemd-run", "--unit=nivaroos-config-restore-"+req.Token[:8], "--collect", "--quiet", "--",
		exe, "config-restore", "-root", rs.Root, "-dir", bundle, "-remove-dir", "-yes", "-status-id", tokenID(req.Token))
	if err != nil {
		s.fail(w, fmt.Errorf("starting the restore: %v: %s", err, strings.TrimSpace(string(out))))
		return
	}
	s.audit(r, "settings_restore_apply", "", fmt.Sprintf("files=%d from=%s", len(man.Files), man.Version))
	writeOK(w, http.StatusAccepted, plan)
}

// pruneOld removes the entries of dir older than age (opened bundles
// nobody applied).
func pruneOld(dir string, age time.Duration, now time.Time) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > age {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

// hostCommand runs a host program (systemctl, systemd-run, pdbedit, id,
// useradd). With NIVAROOS_HOST_CMD set, that program is run instead with
// the command as its arguments (tests: a fake that logs).
func hostCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	if fake := os.Getenv("NIVAROOS_HOST_CMD"); fake != "" {
		args, name = append([]string{name}, args...), fake
	} else if _, err := exec.LookPath(name); err != nil {
		return nil, errors.New(name + " is not installed")
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
