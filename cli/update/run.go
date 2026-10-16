package update

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Test seams.
var (
	httpClient = &http.Client{Timeout: 30 * time.Minute}
	now        = time.Now
	osRelease  = "/etc/os-release"
	glibc      = func() string {
		out, _ := exec.Command("getconf", "GNU_LIBC_VERSION").Output()
		return strings.TrimPrefix(strings.TrimSpace(string(out)), "glibc ")
	}
	arch = runtime.GOARCH
	// Waiting for backups before a manual install ("Update when they finish").
	backupPoll = 2 * time.Minute
)

func inProgress(s string) bool {
	switch s {
	case StateDownloading, StateVerifying, StateApplying, StateProbation, StateRollingBack, StateWaiting:
		return true
	}
	return false
}

// Check asks GitHub for a newer server release and records the answer.
func Check(ctx context.Context) (State, error) {
	lk, err := lock()
	if err != nil {
		return LoadState(), err
	}
	defer lk.Close()
	return check(ctx)
}

func check(ctx context.Context) (State, error) {
	st, set := LoadState(), LoadSettings()
	st.LastCheck = now()
	list, err := fetchReleases(ctx, httpClient)
	if err != nil {
		st.LastError = "Couldn't check for updates: " + err.Error()
		_ = saveState(&st)
		return st, err
	}
	st.LastError = ""
	st.Unsupported = unsupported()
	st.Available = pickRelease(list, set.Channel, arch, st.Current, st.BadVersions)
	if !inProgress(st.State) {
		st.State = StateIdle
		if st.Available != nil {
			st.State = StateAvailable
		}
	}
	// Announced once per version, and only to release installs (a box built
	// from source compares itself with master instead).
	if a := st.Available; a != nil && st.Current != "" && st.Notified != a.Version {
		notify(ctx, "info", "notify.update.available", "NivaroOS "+a.Version+" is available",
			"Settings > Updates shows what's new.", map[string]string{"version": a.Version})
		st.Notified = a.Version
	}
	return st, saveState(&st)
}

// unsupported says why this box can't take release bundles ("" if it can):
// they are glibc builds for Debian/Ubuntu on amd64/arm64. Other installs
// keep updating from source with the installer.
func unsupported() string {
	b, _ := os.ReadFile(osRelease)
	ok := false
	for _, line := range strings.Split(string(b), "\n") {
		k, v, _ := strings.Cut(line, "=")
		if k == "ID" || k == "ID_LIKE" {
			v = " " + strings.Trim(v, `"`) + " "
			ok = ok || strings.Contains(v, " debian ") || strings.Contains(v, " ubuntu ")
		}
	}
	switch {
	case !ok:
		return "Updates for this system come from the installer (release bundles are for Debian and Ubuntu)."
	case arch != "amd64" && arch != "arm64":
		return "Updates for this architecture come from the installer."
	case glibc() == "":
		return "Updates for this system come from the installer (it has no glibc)."
	}
	return ""
}

// ApplyOptions: an install now (Manual), what to do about running backups,
// or a local bundle (offline / SSH: Local + Sums + Sig).
type ApplyOptions struct {
	Manual         bool
	Mode           string // wait | cancel_backups
	Local          string
	Sums, Sig      string
	AllowDowngrade bool
	Out            io.Writer
}

// Apply downloads, verifies and installs the newest release (or a local
// bundle) with the release's own apply.sh.
func Apply(ctx context.Context, o ApplyOptions) error {
	lk, err := lock()
	if err != nil {
		return err
	}
	defer lk.Close()
	return apply(ctx, o)
}

func apply(ctx context.Context, o ApplyOptions) error {
	if o.Out == nil {
		o.Out = io.Discard
	}
	if why := unsupported(); why != "" {
		return errors.New(why)
	}
	st, set := LoadState(), LoadSettings()
	save := func() { _ = saveState(&st) }
	idle := func(errMsg string) error {
		st.State, st.Step, st.Progress = StateIdle, "", 0
		if st.Available != nil {
			st.State = StateAvailable
		}
		st.LastError = errMsg
		save()
		if errMsg == "" {
			return nil
		}
		return errors.New(errMsg)
	}

	var want Expect
	var tarPath string
	var sums, sig []byte
	var err error
	if o.Local == "" {
		if _, err := check(ctx); err != nil {
			return err
		}
		st = LoadState()
		if st.Available == nil {
			fmt.Fprintln(o.Out, "No update available.")
			return nil
		}
		want = Expect{st.Available.Version, st.Available.Channel, arch}
	} else {
		if sums, err = os.ReadFile(o.Sums); err == nil {
			sig, err = os.ReadFile(o.Sig)
		}
		if err != nil {
			return fmt.Errorf("a local bundle needs --sums SHA256SUMS and --sig SHA256SUMS.minisig: %w", err)
		}
		tarPath = o.Local
		want.Arch = arch
		name := filepath.Base(o.Local)
		want.Version = strings.TrimSuffix(strings.TrimPrefix(name, "nivaroos-server-"), "-linux-"+arch+".tar.gz")
		if name != TarName(want.Version, arch) {
			return fmt.Errorf("%s is not a %s bundle (nivaroos-server-X.Y.Z-linux-%s.tar.gz)", name, arch, arch)
		}
		// The channel comes from the signed comment (checked by Stage).
		lines := strings.Split(string(sig), "\n")
		if len(lines) > 2 {
			f := strings.Fields(strings.TrimPrefix(lines[2], "trusted comment: "))
			if len(f) == 3 {
				want.Channel = f[2]
			}
		}
		if want.Channel == "beta" && set.Channel != "beta" {
			return errors.New("that is a beta release and this server is on the stable channel (nivaroos update set --channel beta)")
		}
		if st.Current != "" && compareVersions(want.Version, st.Current) <= 0 && !o.AllowDowngrade {
			return fmt.Errorf("%s is not newer than the installed %s (--allow-downgrade)", want.Version, st.Current)
		}
	}

	// "Update when they finish" (default) or "Cancel them and update". A
	// scheduled install never gets here with backups running (Nightly).
	if runs := runningBackups(ctx); len(runs) > 0 {
		if o.Mode == "cancel_backups" {
			fmt.Fprintf(o.Out, "Cancelling %d backup run(s)...\n", len(runs))
			cancelBackups(ctx, runs)
		}
		st.State = StateWaiting
		save()
		for len(runningBackups(ctx)) > 0 {
			fmt.Fprintln(o.Out, "Waiting for running backups to finish...")
			select {
			case <-ctx.Done():
				return idle("Stopped waiting for backups.")
			case <-time.After(backupPoll):
			}
		}
	}

	if o.Local == "" {
		if free, need := freeBytes(Dir), st.Available.Size*5+500<<20; free < need {
			return idle(fmt.Sprintf("Not enough disk space: free %d MB on %s to update.", (need-free)>>20+1, Dir))
		}
		st.State = StateDownloading
		save()
		if tarPath, sums, sig, err = download(ctx, st.Available, func(pct int) { st.Progress = pct; save() }); err != nil {
			return idle("Download failed: " + err.Error())
		}
	}
	st.State, st.Progress = StateVerifying, 0
	save()
	dir, m, err := Stage(tarPath, sums, sig, want, p("staged"))
	if err != nil {
		_ = os.RemoveAll(p("download"))
		notify(ctx, "error", "notify.update.verify_failed", "An update failed its signature check",
			"NivaroOS "+want.Version+" was not installed: "+err.Error(), map[string]string{"version": want.Version, "error": err.Error()})
		return idle("NivaroOS " + want.Version + " failed verification: " + err.Error())
	}
	if !glibcOK(m.GlibcMin, glibc()) {
		return idle(fmt.Sprintf("NivaroOS %s needs glibc %s (this system has %s): update with the installer.", want.Version, m.GlibcMin, glibc()))
	}
	if st.Current != "" && m.MinFrom != "" && compareVersions(st.Current, m.MinFrom) < 0 {
		return idle(fmt.Sprintf("NivaroOS %s needs %s or newer installed first.", want.Version, m.MinFrom))
	}
	if free, need := freeBytes(Dir), dirSize(dir)*3+500<<20; free < need {
		return idle(fmt.Sprintf("Not enough disk space: free %d MB on %s to update.", (need-free)>>20+1, Dir))
	}

	st.State, st.Step = StateApplying, ""
	save()
	code, line := runApply(ctx, o.Out, filepath.Join(dir, "apply.sh"), []string{"update"}, func(step string) {
		st.Step = step
		switch step {
		case "probation":
			st.State = StateProbation
		case "rolling_back":
			st.State = StateRollingBack
		}
		save()
	})
	return finish(ctx, &st, code, line, want.Version)
}

// finish records an apply.sh outcome: history, state, notification.
func finish(ctx context.Context, st *State, code int, line, to string) error {
	from := st.Current
	if from == "" {
		from = "source"
	}
	r := Result{At: now(), From: from, To: to, Reason: line}
	var err error
	switch code {
	case 0:
		r.Result, r.Reason = "installed", ""
		st.Available = nil
		_ = os.RemoveAll(p("download"))
		_ = os.RemoveAll(p("staged"))
		notify(ctx, "success", "notify.update.installed", "NivaroOS "+to+" is installed", line, map[string]string{"version": to})
	case 1:
		r.Result = "rolled_back"
		if !slices.Contains(st.BadVersions, to) {
			st.BadVersions = append(st.BadVersions, to)
		}
		st.Available = nil
		err = fmt.Errorf("NivaroOS %s was rolled back: %s", to, line)
		notify(ctx, "warning", "notify.update.rolled_back", "Update to NivaroOS "+to+" was rolled back", line, map[string]string{"version": to, "from": from, "reason": line})
	case 2:
		r.Result = "stuck"
		err = fmt.Errorf("NivaroOS %s failed and the rollback is unhealthy: %s - see sudo nivaroos-recover status", to, line)
		notify(ctx, "error", "notify.update.stuck", "Update to NivaroOS "+to+" failed", "The previous version isn't healthy either. Check: sudo nivaroos-recover status", map[string]string{"version": to})
	default:
		r.Result = "failed"
		err = fmt.Errorf("NivaroOS %s was not installed: %s", to, line)
	}
	appendHistory(r)
	st.LastResult = &r
	st.State, st.Step, st.Progress = StateIdle, "", 0
	if st.Available != nil {
		st.State = StateAvailable
	}
	st.LastError = ""
	if err != nil {
		st.LastError = err.Error()
	}
	if serr := saveState(st); err == nil {
		err = serr
	}
	return err
}

// runApply runs an apply.sh and returns its exit code and the line that
// best says what happened ("failed: ...", "refused: ...", "... installed").
// "::step NAME" lines go to onStep; the rest to out (and the journal).
func runApply(ctx context.Context, out io.Writer, script string, args []string, onStep func(string)) (int, string) {
	cmd := exec.CommandContext(ctx, "bash", append([]string{script}, args...)...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return 3, err.Error()
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return 3, err.Error()
	}
	var last string
	sc := bufio.NewScanner(pipe)
	for sc.Scan() {
		l := sc.Text()
		if step, ok := strings.CutPrefix(l, "::step "); ok {
			onStep(step)
			continue
		}
		fmt.Fprintln(out, l)
		for _, k := range []string{"failed: ", "refused: ", "STUCK: ", " installed (", "rolled back to ", "nothing to do"} {
			if strings.Contains(l, k) {
				last = l
			}
		}
	}
	err = cmd.Wait()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, last
	case errors.As(err, &ee):
		return ee.ExitCode(), last
	}
	return 3, err.Error()
}

// download fetches the bundle into download/ (resuming a partial one with
// a Range request) and SHA256SUMS + its signature into memory.
func download(ctx context.Context, a *Available, progress func(int)) (string, []byte, []byte, error) {
	dir := p("download")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, nil, err
	}
	sums, err := fetchSmall(ctx, a.SumsURL)
	if err != nil {
		return "", nil, nil, err
	}
	sig, err := fetchSmall(ctx, a.SigURL)
	if err != nil {
		return "", nil, nil, err
	}
	// Kept next to the bundle for `nivaroos update verify`.
	_ = os.WriteFile(filepath.Join(dir, "SHA256SUMS"), sums, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "SHA256SUMS.minisig"), sig, 0o600)
	path := filepath.Join(dir, a.TarName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil, nil, err
	}
	defer f.Close()
	have, _ := f.Seek(0, io.SeekEnd)
	if a.Size > 0 && have > a.Size {
		have = 0
	}
	if a.Size == 0 || have < a.Size {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.TarURL, nil)
		if err != nil {
			return "", nil, nil, err
		}
		if have > 0 {
			req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
		}
		res, err := httpClient.Do(req)
		if err != nil {
			return "", nil, nil, err
		}
		defer res.Body.Close()
		switch res.StatusCode {
		case http.StatusPartialContent:
		case http.StatusOK:
			have = 0 // the server sent it all
		default:
			return "", nil, nil, fmt.Errorf("download: %s", res.Status)
		}
		if err := f.Truncate(have); err != nil {
			return "", nil, nil, err
		}
		if _, err := f.Seek(have, io.SeekStart); err != nil {
			return "", nil, nil, err
		}
		pw := &progressWriter{total: a.Size, done: have, report: progress}
		if _, err := io.Copy(io.MultiWriter(f, pw), res.Body); err != nil {
			return "", nil, nil, err
		}
	}
	if st, err := f.Stat(); err != nil || (a.Size > 0 && st.Size() != a.Size) {
		return "", nil, nil, errors.New("download incomplete (size differs from the release's)")
	}
	return path, sums, sig, nil
}

type progressWriter struct {
	total, done int64
	last        time.Time
	report      func(int)
}

func (w *progressWriter) Write(b []byte) (int, error) {
	w.done += int64(len(b))
	if w.total > 0 && time.Since(w.last) > 2*time.Second {
		w.last = time.Now()
		w.report(int(w.done * 100 / w.total))
	}
	return len(b), nil
}

func fetchSmall(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 64<<10))
}

func freeBytes(dir string) int64 {
	var s syscall.Statfs_t
	if syscall.Statfs(dir, &s) != nil {
		return 1 << 62
	}
	return int64(s.Bavail) * s.Bsize
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi.Mode().IsRegular() {
			n += fi.Size()
		}
		return nil
	})
	return n
}

// Download fetches the newest release into download/ (nothing installed).
func Download(ctx context.Context, out io.Writer) error {
	lk, err := lock()
	if err != nil {
		return err
	}
	defer lk.Close()
	st, err := check(ctx)
	if err != nil {
		return err
	}
	if st.Available == nil {
		fmt.Fprintln(out, "No update available.")
		return nil
	}
	path, _, _, err := download(ctx, st.Available, func(int) {})
	if err == nil {
		fmt.Fprintln(out, "Downloaded "+path)
	}
	return err
}

// Verify checks the downloaded release (signature, hashes, MANIFEST) and
// stages it in staged/<version>; nothing is installed.
func Verify(ctx context.Context, out io.Writer) error {
	lk, err := lock()
	if err != nil {
		return err
	}
	defer lk.Close()
	a := LoadState().Available
	if a == nil {
		return errors.New("no update available - nivaroos update download first")
	}
	sums, err := os.ReadFile(p("download/SHA256SUMS"))
	if err != nil {
		return errors.New("nothing downloaded - nivaroos update download first")
	}
	sig, err := os.ReadFile(p("download/SHA256SUMS.minisig"))
	if err != nil {
		return err
	}
	dir, m, err := Stage(p("download/"+a.TarName), sums, sig, Expect{a.Version, a.Channel, arch}, p("staged"))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "NivaroOS %s verified (signed, %d files match MANIFEST): %s\n", m.Version, len(m.Files), dir)
	return nil
}

// Rollback undoes the last update (whole set) with the snapshot's own
// apply.sh. auto: the watchdog saw a service keep failing after it.
func Rollback(ctx context.Context, restoreData bool, reason string, auto bool, out io.Writer) error {
	lk, err := lock()
	if err != nil {
		return err
	}
	defer lk.Close()
	return rollback(ctx, restoreData, reason, auto, out)
}

func rollback(ctx context.Context, restoreData bool, reason string, auto bool, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	st := LoadState()
	to, _ := os.ReadFile(p("snap/to-version"))
	from, _ := os.ReadFile(p("snap/from-version"))
	args := []string{"rollback"}
	if restoreData {
		args = append(args, "--restore-data")
	}
	if reason != "" {
		args = append(args, "--reason", reason)
	}
	st.State = StateRollingBack
	_ = saveState(&st)
	code, line := runApply(ctx, out, p("snap/apply.sh"), args, func(string) {})
	ver := strings.TrimSpace(string(to))
	r := Result{At: now(), From: ver, To: strings.TrimSpace(string(from)), Reason: reason}
	switch code {
	case 0:
		r.Result = "rolled_back"
		if auto && !slices.Contains(st.BadVersions, ver) {
			st.BadVersions = append(st.BadVersions, ver)
		}
		notify(ctx, "warning", "notify.update.rolled_back", "Update to NivaroOS "+ver+" was rolled back", strings.TrimSpace(reason+" "+line),
			map[string]string{"version": ver, "reason": reason})
	case 2:
		r.Result = "stuck"
		notify(ctx, "error", "notify.update.stuck", "Rolling back NivaroOS "+ver+" failed", "Check: sudo nivaroos-recover status", map[string]string{"version": ver})
	default:
		st.State = StateIdle
		_ = saveState(&st)
		return errors.New(strings.TrimPrefix(line, "refused: "))
	}
	appendHistory(r)
	st.LastResult = &r
	st.State, st.Step = StateIdle, ""
	if err := saveState(&st); err != nil {
		return err
	}
	if code != 0 {
		return errors.New(line)
	}
	return nil
}

// Resume, at boot: an update cut off by a reboot is finished or undone.
func Resume(ctx context.Context, out io.Writer) error {
	lk, err := lock()
	if err != nil {
		return err
	}
	defer lk.Close()
	st := LoadState()
	snap, _ := os.ReadFile(p("snap/state"))
	to, _ := os.ReadFile(p("snap/to-version"))
	ver := strings.TrimSpace(string(to))
	switch st.State {
	case StateApplying, StateRollingBack, StateProbation:
		switch strings.TrimSpace(string(snap)) {
		case "committed":
			return finish(ctx, &st, 0, "finished before the restart", ver)
		case "rolled-back":
			return finish(ctx, &st, 1, "rolled back before the restart", ver)
		case "applied":
			if st.State == StateProbation {
				code, line := runApply(ctx, out, p("snap/apply.sh"), []string{"probation"}, func(string) {})
				return finish(ctx, &st, code, line, ver)
			}
		}
		// We can't know how far the swap got; the snapshot is complete.
		if err := rollback(ctx, true, "interrupted by a restart", true, out); err != nil {
			return err
		}
		return nil
	case StateDownloading, StateVerifying, StateWaiting:
		st.State, st.Step, st.Progress = StateIdle, "", 0
		if st.Available != nil {
			st.State = StateAvailable
		}
		return saveState(&st)
	}
	return nil
}

// Run is nivaroos-update.service: what request.json asks for (core wrote
// it), or else the nightly check, and the automatic install when
// Settings say so, it is about the window time and nothing is going on.
func Run(ctx context.Context, out io.Writer) error {
	var req Request
	if readJSON("request.json", &req) == nil {
		_ = os.Remove(p("request.json"))
		switch req.Action {
		case "check":
			_, err := Check(ctx)
			return err
		case "apply":
			return Apply(ctx, ApplyOptions{Manual: true, Mode: req.Mode, Out: out})
		case "rollback":
			return Rollback(ctx, req.RestoreData, "requested from Settings", false, out)
		}
		return fmt.Errorf("unknown request %q", req.Action)
	}
	// The timer firing during a manual update is not a failed night.
	if err := Nightly(ctx, out); !errors.Is(err, ErrBusy) {
		return err
	}
	fmt.Fprintln(out, "an update is already running - nothing to do now")
	return nil
}

func Nightly(ctx context.Context, out io.Writer) error {
	lk, err := lock()
	if err != nil {
		return err
	}
	defer lk.Close()
	st, err := check(ctx)
	if err != nil {
		return err
	}
	set := LoadSettings()
	if st.Available == nil || set.Auto != "install" || st.Current == "" || st.Unsupported != "" || !inWindow(now(), set.Window) {
		return nil
	}
	if why := busy(ctx); why != "" {
		st.Skipped = fmt.Sprintf("Not installed tonight: %s - trying again tomorrow night.", why)
		fmt.Fprintln(out, st.Skipped)
		return saveState(&st)
	}
	st.Skipped = ""
	_ = saveState(&st)
	return apply(ctx, ApplyOptions{Out: out})
}

// glibcOK: have (e.g. "2.41") is at least min ("2.34"; "" or "0" = any).
func glibcOK(min, have string) bool {
	var a, b, c, d int
	if _, err := fmt.Sscanf(min, "%d.%d", &a, &b); err != nil {
		return true
	}
	if _, err := fmt.Sscanf(have, "%d.%d", &c, &d); err != nil {
		return false
	}
	return c > a || (c == a && d >= b)
}

// inWindow: from 30 min before the window time to 90 min after it.
func inWindow(t time.Time, window string) bool {
	w, err := time.ParseInLocation("15:04", window, t.Location())
	if err != nil {
		return false
	}
	at := time.Date(t.Year(), t.Month(), t.Day(), w.Hour(), w.Minute(), 0, 0, t.Location())
	return !t.Before(at.Add(-30*time.Minute)) && t.Before(at.Add(90*time.Minute))
}
