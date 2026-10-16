package jobs

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Restoring a NivaroOS settings bundle (configbundle.go). Open decrypts
// it, LoadConfigBundle checks it, Plan is the dry run, Apply writes it:
// every NivaroOS service stopped (reverse start order), the files put in
// place (the ones they replace copied aside first), SMB users imported,
// the services started again in order. Only paths configPaths names are
// ever written, never /DATA.

// configUnits is installer/bundle/apply.sh's ORDER: the start order;
// stop is the reverse.
var configUnits = []string{
	"nivaroos-gateway.service", "nivaroos-message-bus.service", "nivaroos-user-service.service",
	"nivaroos-local-storage.service", "nivaroos-app-management.service", "nivaroos-gpu-sidecar.service",
	"nivaroos-fans.service", "nivaroos.service", "nivaroos-vm-sidecar.service", "nivaroos-download-sidecar.service",
	"nivaroos-backup.service", "nivaroos-host-firewall.service", "nivaroos-watchdog.timer", "nivaroos-update-check.timer",
}

// restartOnly: stopping the firewall's oneshot would drop its rules for
// a moment; it is only restarted afterwards.
const restartOnly = "nivaroos-host-firewall.service"

const maxConfigBundle = 512 << 20

// OpenConfigBundle decrypts a settings backup (its .7z or first .7z.001
// volume) into dir. secret is the backup password, or the generated
// settings key in any case and with or without dashes, or a recovery key
// (needs keyFile, the .nivaroos-encryption.json beside the backup).
func OpenConfigBundle(ctx context.Context, file, secret string, keyFileRaw []byte, dir string) error {
	if secret == "" {
		return &engine.Error{Code: ErrWrongPassword, Detail: "the password or key is needed"}
	}
	cands := []string{secret}
	if n := normRecoveryKey(secret); n != "" {
		var g []string
		for i := 0; i < len(n); i += 6 {
			g = append(g, n[i:min(i+6, len(n))])
		}
		cands = append(cands, strings.Join(g, "-"))
	}
	var kf keyFile
	if json.Unmarshal(keyFileRaw, &kf) == nil && kf.Recovery != nil {
		if pw, err := kf.Recovery.open(normRecoveryKey(secret)); err == nil {
			cands = append(cands, string(pw))
		}
	}
	var err error
	for _, pw := range dedupe(cands) {
		if err = extract7z(ctx, file, pw, dir); engine.CodeOf(err) != ErrWrongPassword {
			return err
		}
	}
	return err
}

func extract7z(ctx context.Context, file, pw, dir string) error {
	bin := engine.SevenZip()
	if bin == "" {
		return &engine.Error{Code: ErrSevenZipMissing, Detail: "7z is not installed (package 7zip or p7zip-full)"}
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "x", "-so", "-bd", "-y", "--", filepath.Base(file))
	cmd.Dir = filepath.Dir(file)
	cmd.Stdin = strings.NewReader(pw + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		return err
	}
	terr := untarConfig(bufio.NewReader(out), dir)
	_, _ = io.Copy(io.Discard, out)
	werr := cmd.Wait()
	switch {
	case strings.Contains(stderr.String(), "Wrong password"):
		return &engine.Error{Code: ErrWrongPassword, Detail: "the password or key does not open this backup"}
	case werr != nil:
		return &engine.Error{Code: ErrValidation, Detail: "not a NivaroOS settings backup (7z: " + strings.TrimSpace(lastLine(stderr.String())) + ")"}
	case terr != nil:
		return &engine.Error{Code: ErrValidation, Detail: "not a NivaroOS settings backup: " + terr.Error()}
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// untarConfig unpacks regular files and folders only, nothing outside
// dir, at most maxConfigBundle bytes.
func untarConfig(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if name == "." {
			continue
		}
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe name %q", h.Name)
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if total += h.Size; total > maxConfigBundle {
				return errors.New("too big for a settings backup")
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, h.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
	}
}

// LoadConfigBundle reads and checks an opened bundle: its manifest, every
// path allowed, every file's checksum.
func LoadConfigBundle(dir string) (*ConfigManifest, error) {
	bad := func(format string, a ...interface{}) error {
		return &engine.Error{Code: ErrValidation, Detail: "not a usable NivaroOS settings backup: " + fmt.Sprintf(format, a...)}
	}
	raw, err := os.ReadFile(filepath.Join(dir, configManifestName))
	if err != nil {
		return nil, bad("no %s", configManifestName)
	}
	var man ConfigManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return nil, bad("%s: %v", configManifestName, err)
	}
	if man.Kind != configKind {
		return nil, bad("kind %q", man.Kind)
	}
	if man.Format < 1 || man.Format > configFormat {
		return nil, bad("format %d needs a newer NivaroOS", man.Format)
	}
	for _, f := range man.Files {
		if !configAllowed(f.Path) {
			return nil, bad("%s is not a settings file", f.Path)
		}
		sum, n, err := hashFile(filepath.Join(dir, configFilesDir, filepath.FromSlash(f.Path)))
		if err != nil || sum != f.SHA256 || n != f.Size {
			return nil, bad("%s is missing or damaged", f.Path)
		}
	}
	return &man, nil
}

// configAllowed: p (relative, slash separated) is or is inside a
// configPaths match, and never /DATA.
func configAllowed(p string) bool {
	if p == "" || p != path.Clean(p) || path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") ||
		p == "DATA" || strings.HasPrefix(p, "DATA/") || skipConfigName(path.Base(p)) {
		return false
	}
	for a := p; a != "." && a != "/"; a = path.Dir(a) {
		for _, pat := range configPaths {
			if ok, _ := path.Match(pat, a); ok {
				return true
			}
		}
	}
	return false
}

// ConfigPlan is a restore's dry run.
type ConfigPlan struct {
	FromVersion      string    `json:"from_version"` // "" unknown
	FromHostname     string    `json:"from_hostname"`
	Created          time.Time `json:"created"`
	InstalledVersion string    `json:"installed_version"`
	// Problem says why this backup can't be restored here ("" = it can).
	Problem     string           `json:"problem,omitempty"`
	Warnings    []string         `json:"warnings"`
	Items       []ConfigPlanItem `json:"items"`
	SMBUsers    []string         `json:"smb_users"`
	GatewayPort string           `json:"gateway_port,omitempty"` // the dashboard's port after the restore
}

// ConfigPlanItem is one file of the plan.
type ConfigPlanItem struct {
	Path   string `json:"path"`   // absolute, on this server
	Action string `json:"action"` // add | replace | same | import (SMB users)
	Size   int64  `json:"size"`
}

// ConfigRestorer restores into Root ("/" on a real server).
type ConfigRestorer struct {
	Root  string
	Force bool // restore a backup from a newer NivaroOS anyway
	Out   io.Writer
	Run   func(ctx context.Context, name string, args ...string) ([]byte, error) // default hostCommand
	Now   func() time.Time
	// StatusID marks the apps' progress as this restore's (tokenID of
	// the Welcome screen's token).
	StatusID string

	appsQueued, appMgmtStarted bool // configapps.go
}

func (c *ConfigRestorer) root() string {
	if c.Root == "" {
		return "/"
	}
	return c.Root
}

// host runs a host command. Restoring into anything but the real root
// without a fake (NIVAROOS_HOST_CMD, Run) never touches the host's
// services or users.
func (c *ConfigRestorer) host(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.Run != nil {
		return c.Run(ctx, name, args...)
	}
	if filepath.Clean(c.root()) != "/" && os.Getenv("NIVAROOS_HOST_CMD") == "" {
		return nil, errors.New("skipped: " + name + " (not restoring into /)")
	}
	return hostCommand(ctx, name, args...)
}

func (c *ConfigRestorer) say(format string, a ...interface{}) {
	if c.Out != nil {
		fmt.Fprintf(c.Out, format+"\n", a...)
	}
}

func (c *ConfigRestorer) target(rel string) string {
	return filepath.Join(c.root(), filepath.FromSlash(rel))
}

// Plan compares the bundle in dir with what is on this server.
func (c *ConfigRestorer) Plan(ctx context.Context, dir string, man *ConfigManifest) ConfigPlan {
	p := ConfigPlan{FromVersion: man.Version, FromHostname: man.Hostname, Created: man.Created,
		Warnings: []string{}, Items: []ConfigPlanItem{}, SMBUsers: []string{}}
	p.InstalledVersion, _ = releaseInfo(c.root())
	switch {
	case man.Version == "" || p.InstalledVersion == "":
		p.Warnings = append(p.Warnings, "the NivaroOS version of the backup or of this server is unknown")
	case compareVersions(man.Version, p.InstalledVersion) > 0 && !c.Force:
		p.Problem = fmt.Sprintf("this backup is from NivaroOS %s, newer than this server's %s: update NivaroOS first", man.Version, p.InstalledVersion)
	}
	for _, f := range man.Files {
		it := ConfigPlanItem{Path: "/" + f.Path, Size: f.Size}
		switch sum, _, err := hashFile(c.target(f.Path)); {
		case f.Path == passdbPath:
			it.Action = "import"
			p.SMBUsers = c.smbUsers(ctx, filepath.Join(dir, configFilesDir, passdbPath))
		case err != nil:
			it.Action = "add"
		case sum == f.SHA256:
			it.Action = "same"
		default:
			it.Action = "replace"
		}
		if f.Path == "etc/nivaroos/gateway.ini" {
			p.GatewayPort = iniValue(filepath.Join(dir, configFilesDir, f.Path), "port")
		}
		p.Items = append(p.Items, it)
	}
	return p
}

func (c *ConfigRestorer) smbUsers(ctx context.Context, passdb string) []string {
	out, err := c.host(ctx, "pdbedit", "-L", "-b", "tdbsam:"+passdb)
	users := []string{}
	if err != nil {
		return users
	}
	for _, l := range strings.Split(string(out), "\n") {
		if name, uid, ok := strings.Cut(l, ":"); ok && name != "" && !strings.ContainsAny(name, " \t") {
			if _, err := strconv.Atoi(strings.SplitN(uid, ":", 2)[0]); err == nil {
				users = append(users, name)
			}
		}
	}
	return users
}

func iniValue(file, key string) string {
	raw, _ := os.ReadFile(file)
	for _, l := range strings.Split(string(raw), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// compareVersions compares dotted numeric versions ("1.5.0").
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(as), len(bs)); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(strings.SplitN(as[i], "-", 2)[0])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(strings.SplitN(bs[i], "-", 2)[0])
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

// Apply restores the bundle in dir. On a failure while writing, the
// files written so far are put back as they were; the services are
// started again either way.
func (c *ConfigRestorer) Apply(ctx context.Context, dir string, man *ConfigManifest) error {
	plan := c.Plan(ctx, dir, man)
	if plan.Problem != "" {
		return errors.New(plan.Problem)
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	aside := c.target("var/lib/nivaroos/config-restore/before-" + now().UTC().Format("20060102-150405"))

	var active []string
	for _, u := range configUnits {
		if out, _ := c.host(ctx, "systemctl", "is-active", u); strings.TrimSpace(string(out)) == "active" {
			active = append(active, u)
		}
	}
	defer c.start(context.WithoutCancel(ctx), active)
	c.appMgmtStarted = slices.Contains(active, "nivaroos-app-management.service")
	for i := len(active) - 1; i >= 0; i-- {
		if active[i] == restartOnly {
			continue
		}
		c.say("stopping %s", active[i])
		if out, err := c.host(ctx, "systemctl", "stop", active[i]); err != nil {
			return fmt.Errorf("stopping %s: %v %s", active[i], err, out)
		}
	}

	modes := map[string]uint32{}
	for _, f := range man.Files {
		modes[f.Path] = f.Mode
	}
	var done []ConfigPlanItem
	smb := false
	for _, it := range plan.Items {
		if it.Action != "add" && it.Action != "replace" {
			continue
		}
		rel := strings.TrimPrefix(it.Path, "/")
		if err := c.place(dir, rel, aside, it.Action == "replace", os.FileMode(modes[rel]).Perm()); err != nil {
			c.undo(done, aside)
			return fmt.Errorf("restoring %s: %w (the files restored so far were put back)", it.Path, err)
		}
		done = append(done, it)
		smb = smb || strings.HasPrefix(rel, "etc/samba/")
		c.say("restored %s", it.Path)
	}
	if len(plan.SMBUsers) > 0 {
		c.importSMB(ctx, filepath.Join(dir, configFilesDir, passdbPath), plan.SMBUsers)
		smb = true
	}
	if smb {
		_, _ = c.host(ctx, "systemctl", "try-restart", "smbd", "nmbd", "smb", "nmb")
	}
	if len(done) > 0 {
		c.say("the files it replaced are in %s", aside)
	}
	if err := c.queueApps(man); err != nil {
		c.say("warning: the apps won't be recreated: %v", err)
	}
	return nil
}

// place writes one file; a replaced one is copied to aside first. A
// database's -wal/-shm of the old one would corrupt the new one.
func (c *ConfigRestorer) place(dir, rel, aside string, replace bool, mode os.FileMode) error {
	dst := c.target(rel)
	if replace {
		raw, err := os.ReadFile(dst)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(aside, rel)), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(aside, rel), raw, 0o600); err != nil {
			return err
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilesDir, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o600
	}
	dmode := os.FileMode(0o755)
	if mode&0o077 == 0 {
		dmode = 0o700
	}
	if err := os.MkdirAll(filepath.Dir(dst), dmode); err != nil {
		return err
	}
	if strings.HasSuffix(rel, ".db") {
		for _, s := range []string{"-wal", "-shm", "-journal"} {
			if err := os.Remove(dst + s); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return writeFileAtomic(dst, raw, mode)
}

// undo puts back what place changed.
func (c *ConfigRestorer) undo(done []ConfigPlanItem, aside string) {
	for _, it := range done {
		rel := strings.TrimPrefix(it.Path, "/")
		if it.Action == "add" {
			_ = os.Remove(c.target(rel))
			continue
		}
		if raw, err := os.ReadFile(filepath.Join(aside, rel)); err == nil {
			st, _ := os.Stat(c.target(rel))
			mode := os.FileMode(0o600)
			if st != nil {
				mode = st.Mode().Perm()
			}
			_ = writeFileAtomic(c.target(rel), raw, mode)
		}
	}
}

// importSMB creates the system users the SMB users need (as Settings >
// Users does) and imports their passwords.
func (c *ConfigRestorer) importSMB(ctx context.Context, passdb string, users []string) {
	for _, u := range users {
		if _, err := c.host(ctx, "id", "-u", u); err == nil {
			continue
		}
		if out, err := c.host(ctx, "useradd", "-m", "-s", "/bin/bash", u); err != nil {
			c.say("warning: creating the system user %s: %v %s", u, err, out)
		}
	}
	if out, err := c.host(ctx, "pdbedit", "-i", "tdbsam:"+passdb); err != nil {
		c.say("warning: importing the SMB users: %v %s", err, out)
		return
	}
	c.say("imported %d SMB user(s)", len(users))
}

func (c *ConfigRestorer) start(ctx context.Context, units []string) {
	for _, u := range units {
		verb := "start"
		if u == restartOnly {
			verb = "restart"
		}
		c.say("starting %s", u)
		if out, err := c.host(ctx, "systemctl", verb, u); err != nil {
			c.say("warning: %s %s: %v %s", verb, u, err, out)
		}
	}
}

// ---------------------------------------------------------------------
// The config-restore subcommand

// ConfigRestoreCmd is the subcommand name.
const ConfigRestoreCmd = "config-restore"

// RunConfigRestore is `nivaroos-backup config-restore`: install.sh
// --restore, and the transient unit POST /config-restore/apply starts.
//
//	config-restore -file <nivaro_..._ts.7z.001> [-key-file F] [-yes] [-force] [-root /]
//	config-restore -dir <opened bundle> [-yes] [-remove-dir]
//
// The key is read from NIVAROOS_RESTORE_KEY, else one line of stdin.
// Without -yes it only prints the plan.
func RunConfigRestore(args []string, stdin io.Reader, out io.Writer) int {
	fs := flag.NewFlagSet(ConfigRestoreCmd, flag.ContinueOnError)
	fs.SetOutput(out)
	root := fs.String("root", "/", "restore into this root (tests)")
	file := fs.String("file", "", "the settings backup: its .7z.001 (or .7z) file")
	keyFile := fs.String("key-file", "", "the backup's .nivaroos-encryption.json, for a recovery key (default: the one beside -file)")
	dir := fs.String("dir", "", "an already opened bundle (instead of -file)")
	yes := fs.Bool("yes", false, "restore; without it only the plan is shown")
	force := fs.Bool("force", false, "restore a backup from a newer NivaroOS anyway")
	removeDir := fs.Bool("remove-dir", false, "remove -dir afterwards")
	statusID := fs.String("status-id", "", "mark the apps' progress as this restore's (the web restore)")
	appsStatus := fs.Bool("apps-status", false, "only show the restored apps' progress and results")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *appsStatus {
		st, err := readAppsStatus(filepath.Join(*root, appsRestoreFile))
		if err != nil {
			fmt.Fprintln(out, "config-restore: no restored apps")
			return 1
		}
		printAppsResult(out, st)
		return 0
	}
	if (*file == "") == (*dir == "") {
		fmt.Fprintln(out, "config-restore: give -file or -dir")
		return 2
	}
	ctx := context.Background()
	c := &ConfigRestorer{Root: *root, Force: *force, Out: out, StatusID: *statusID}
	bundle := *dir
	if *file != "" {
		key := os.Getenv("NIVAROOS_RESTORE_KEY")
		if key == "" {
			line, _ := bufio.NewReader(stdin).ReadString('\n')
			key = strings.TrimRight(line, "\r\n")
		}
		kfPath := *keyFile
		if kfPath == "" {
			kfPath = filepath.Join(filepath.Dir(*file), engine.KeyFileName)
		}
		kf, _ := os.ReadFile(kfPath)
		work := c.target("var/lib/nivaroos")
		_ = os.MkdirAll(work, 0o755)
		tmp, err := os.MkdirTemp(work, ".config-restore-")
		if err != nil {
			fmt.Fprintln(out, "config-restore:", err)
			return 1
		}
		defer os.RemoveAll(tmp)
		bundle = filepath.Join(tmp, "bundle")
		if err := OpenConfigBundle(ctx, *file, key, kf, bundle); err != nil {
			fmt.Fprintln(out, "config-restore:", describeErr(err))
			return 1
		}
	} else if *removeDir {
		defer os.RemoveAll(filepath.Dir(bundle))
	}
	man, err := LoadConfigBundle(bundle)
	if err != nil {
		fmt.Fprintln(out, "config-restore:", describeErr(err))
		return 1
	}
	plan := c.Plan(ctx, bundle, man)
	printPlan(out, plan)
	if plan.Problem != "" {
		fmt.Fprintln(out, "config-restore: refused:", plan.Problem)
		return 3
	}
	if !*yes {
		fmt.Fprintln(out, "dry run: nothing was changed (add -yes to restore)")
		return 0
	}
	if err := c.Apply(ctx, bundle, man); err != nil {
		fmt.Fprintln(out, "config-restore: failed:", err)
		return 1
	}
	c.waitApps(ctx)
	fmt.Fprintln(out, "config-restore: done")
	return 0
}

func printPlan(out io.Writer, p ConfigPlan) {
	fmt.Fprintf(out, "NivaroOS settings from %s, %s (NivaroOS %s; this server: %s)\n",
		p.FromHostname, p.Created.Local().Format("2006-01-02 15:04"), orUnknown(p.FromVersion), orUnknown(p.InstalledVersion))
	for _, it := range p.Items {
		fmt.Fprintf(out, "  %-8s %s\n", it.Action, it.Path)
	}
	if len(p.SMBUsers) > 0 {
		fmt.Fprintf(out, "  SMB users: %s\n", strings.Join(p.SMBUsers, ", "))
	}
	if p.GatewayPort != "" {
		fmt.Fprintf(out, "  dashboard port afterwards: %s\n", p.GatewayPort)
	}
	for _, w := range p.Warnings {
		fmt.Fprintln(out, "  warning:", w)
	}
}
