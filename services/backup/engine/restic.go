package engine

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/rclone/rclone/fs/walk"
	"github.com/rclone/rclone/lib/http/serve"
)

// Versioned backups (job type "backup"): restic keeps a deduplicated,
// encrypted repository in the job's destination folder. restic never
// reaches the destination itself: the engine serves the repository to it
// over restic's REST protocol on a private unix socket, from the same
// rclone filesystem every other job type writes through (mount guard,
// SMB credentials, Google Drive tuning), so every location NivaroOS
// offers can hold one. The repository is plain restic: `restic -r
// <folder>` (or rclone:<remote>:<folder>) opens it on any computer.

// ResticPath is where the installer puts the pinned restic;
// NIVAROOS_RESTIC overrides it (tests).
const ResticPath = "/usr/lib/nivaroos/bin/restic"

// resticHost is the host name of every snapshot, so a reinstall or a new
// hostname keeps the same snapshot history (restic groups by host).
const resticHost = "nivaroos"

// snapshotPrefix starts a restic snapshot's version id.
const snapshotPrefix = "s_"

// Restic returns the restic program, "" when it isn't installed.
func Restic() string {
	p := os.Getenv("NIVAROOS_RESTIC")
	if p == "" {
		p = ResticPath
	}
	if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
		return p
	}
	return ""
}

var snapshotIDRe = regexp.MustCompile(`^[0-9a-f]{8,64}$`)

// snapshotID returns the restic snapshot of a version id.
func snapshotID(versionID string) (string, bool) {
	id, ok := strings.CutPrefix(versionID, snapshotPrefix)
	return id, ok && snapshotIDRe.MatchString(id)
}

// resticDirs are a repository's folders (and config its one file).
var resticDirs = []string{"data", "index", "keys", "locks", "snapshots"}

var resticNameRe = regexp.MustCompile(`^[0-9a-f]{2,64}$`)

// resticRepo serves one repository to restic and runs restic on it.
type resticRepo struct {
	e   *Engine
	t   *target
	f   fs.Fs
	bin string
	dir string // private folder: the socket, the exclude file
	srv *http.Server
	// ctx ends the repository's restic runs; a backend error no retry
	// can fix (auth, full, unmounted) ends it at once with that error.
	ctx    context.Context
	cancel context.CancelCauseFunc
	log    func(lvl, msg string)

	uploaded  atomic.Int64
	errMu     sync.Mutex
	lastErr   error
	closeOnce sync.Once
}

// openRepo serves f, the destination folder, to restic until close.
func (e *Engine) openRepo(ctx context.Context, t *target, f fs.Fs, log func(lvl, msg string)) (*resticRepo, error) {
	bin := Restic()
	if bin == "" {
		return nil, Errorf(CodeResticMissing, "restic is not installed at %s (re-run the NivaroOS installer)", ResticPath)
	}
	dir, err := os.MkdirTemp("", "nivaroos-restic-")
	if err != nil {
		return nil, Errorf(CodeIOError, "creating the restic socket folder: %w", err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "s"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, Errorf(CodeIOError, "creating the restic socket: %w", err)
	}
	if log == nil {
		log = func(string, string) {}
	}
	r := &resticRepo{e: e, t: t, f: f, bin: bin, dir: dir, log: log}
	e.resticMu.Lock() // waits out a cache trim
	e.resticOpen++
	e.resticMu.Unlock()
	r.ctx, r.cancel = context.WithCancelCause(ctx)
	r.srv = &http.Server{Handler: r, ReadHeaderTimeout: time.Minute,
		BaseContext: func(net.Listener) context.Context { return r.ctx }}
	go func() { _ = r.srv.Serve(ln) }()
	return r, nil
}

func (r *resticRepo) close() {
	r.closeOnce.Do(func() {
		r.cancel(nil)
		_ = r.srv.Close()
		_ = os.RemoveAll(r.dir)
		e := r.e
		e.resticMu.Lock()
		defer e.resticMu.Unlock()
		if e.resticOpen--; e.resticOpen == 0 {
			if err := trimResticCache(e.cfg.ResticCacheDir); err != nil {
				e.cfg.Logf("engine: trimming the restic cache: %v", err)
			}
		}
	})
}

// resticCacheMax caps restic's cache (var for tests); it is also kept
// under a quarter of the disk space it shares with the free space.
var resticCacheMax int64 = 2 << 30

// trimResticCache removes whole repository caches (restic's cache holds
// one folder per repository, its mtime the last use), least recently
// used first, until the rest fits the cap. restic rebuilds a removed one
// from the repository on its next run.
func trimResticCache(dir string) error {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	type cache struct {
		path string
		used time.Time
		size int64
	}
	var caches []cache
	var total int64
	for _, en := range ents {
		fi, err := en.Info()
		if err != nil || !fi.IsDir() {
			continue
		}
		c := cache{path: filepath.Join(dir, en.Name()), used: fi.ModTime()}
		_ = filepath.Walk(c.path, func(_ string, fi os.FileInfo, err error) error {
			if err == nil && fi.Mode().IsRegular() {
				c.size += fi.Size()
			}
			return nil
		})
		caches = append(caches, c)
		total += c.size
	}
	limit := resticCacheMax
	var st syscall.Statfs_t
	if syscall.Statfs(dir, &st) == nil {
		limit = min(limit, (int64(st.Bavail)*int64(st.Bsize)+total)/4)
	}
	sort.Slice(caches, func(i, k int) bool { return caches[i].used.Before(caches[k].used) })
	for _, c := range caches {
		if total <= limit {
			break
		}
		if err := os.RemoveAll(c.path); err != nil {
			return err
		}
		total -= c.size
	}
	return nil
}

// backendErr records a destination error for the run's result; one that
// a retry can't fix also stops restic, which would otherwise retry it
// for many minutes.
func (r *resticRepo) backendErr(err error) {
	r.errMu.Lock()
	r.lastErr = err
	r.errMu.Unlock()
	switch c := classifyError(err); c {
	case CodeCloudAuth, CodeNoSpace, CodeCancelledUnmounted, CodeCloudQuotaDaily, CodeDestMarkerMismatch:
		r.cancel(engineErr(err, "the backup location"))
	}
}

func (r *resticRepo) lastBackendErr() error {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	return r.lastErr
}

// ServeHTTP is the restic REST protocol (v2) over r.f.
func (r *resticRepo) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	p := strings.Trim(req.URL.Path, "/")
	typ, name, _ := strings.Cut(p, "/")
	ctx := req.Context()
	switch {
	case p == "" && req.Method == http.MethodPost:
		if req.URL.Query().Get("create") != "true" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for _, d := range resticDirs {
			if err := r.f.Mkdir(ctx, d); err != nil {
				r.fail(w, err)
				return
			}
		}
	case p == "config":
		r.object(w, req, "config")
	case slicesContains(resticDirs, typ) && name == "" && req.Method == http.MethodGet:
		r.list(w, req, typ)
	case slicesContains(resticDirs, typ) && resticNameRe.MatchString(name):
		remote := typ + "/" + name
		if typ == "data" {
			remote = "data/" + name[:2] + "/" + name
		}
		r.object(w, req, remote)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (r *resticRepo) fail(w http.ResponseWriter, err error) {
	r.backendErr(err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (r *resticRepo) object(w http.ResponseWriter, req *http.Request, remote string) {
	ctx := req.Context()
	switch req.Method {
	case http.MethodGet, http.MethodHead:
		o, err := r.f.NewObject(ctx, remote)
		if errIsNotFound(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			r.fail(w, err)
			return
		}
		serve.Object(w, req, o)
	case http.MethodPost:
		// A file restic sees must be complete: an upload cut off (a crash)
		// on a backend that writes in place (local, SMB, WebDAV) would
		// leave a short pack that fails every check. Written beside, then
		// moved, where the backend can rename; object stores only show an
		// object once its upload finished. The .partial name never matches
		// a restic name, so lists skip it; sweepPartials removes a crash's.
		name := remote
		if operations.CanServerSideMove(r.f) {
			name = remote + ".partial"
		}
		body := &countingReader{r: req.Body, n: &r.uploaded}
		_, err := operations.RcatSize(ctx, r.f, name, io.NopCloser(body), req.ContentLength, time.Now(), nil)
		if err == nil && name != remote {
			err = operations.MoveFile(ctx, r.f, r.f, remote, name)
		}
		if err != nil {
			r.fail(w, err)
			return
		}
	case http.MethodDelete:
		o, err := r.f.NewObject(ctx, remote)
		if errIsNotFound(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err == nil {
			err = o.Remove(ctx)
		}
		if err != nil {
			r.fail(w, err)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *resticRepo) list(w http.ResponseWriter, req *http.Request, typ string) {
	type item struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	items := []item{}
	err := walk.ListR(req.Context(), r.f, typ, true, -1, walk.ListObjects, func(entries fs.DirEntries) error {
		for _, en := range entries {
			if o, ok := en.(fs.Object); ok && resticNameRe.MatchString(path.Base(o.Remote())) {
				items = append(items, item{Name: path.Base(o.Remote()), Size: o.Size()})
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
		r.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.x.restic.rest.v2")
	_ = json.NewEncoder(w).Encode(items)
}

type countingReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// partialMaxAge is when a .partial is a crash's leftover, not an upload
// under way: an upload is one pack (at most 64 MiB) and restic gives up
// on a stalled one within minutes.
const partialMaxAge = 24 * time.Hour

// sweepPartials removes stale .partial uploads of the repository - its
// own files only (config and the restic folders), nothing else in the
// destination folder. A failure is only logged: they are just space.
func (r *resticRepo) sweepPartials(ctx context.Context) {
	if !operations.CanServerSideMove(r.f) {
		return // uploads were written in place, never as .partial
	}
	cutoff := time.Now().Add(-partialMaxAge)
	n := 0
	err := walk.ListR(ctx, r.f, "", true, -1, walk.ListObjects, func(entries fs.DirEntries) error {
		for _, en := range entries {
			o, ok := en.(fs.Object)
			if !ok {
				continue
			}
			remote, partial := strings.CutSuffix(o.Remote(), ".partial")
			typ, _, _ := strings.Cut(remote, "/")
			own := remote == "config" || (slicesContains(resticDirs, typ) && resticNameRe.MatchString(path.Base(remote)))
			if !partial || !own || o.ModTime(ctx).After(cutoff) {
				continue
			}
			if err := o.Remove(ctx); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		r.log(LogWarn, "removing interrupted uploads: "+err.Error())
	}
	if n > 0 {
		r.log(LogInfo, fmt.Sprintf("removed %d interrupted uploads", n))
	}
}

// exists reports whether the folder already holds a repository.
func (r *resticRepo) exists(ctx context.Context) (bool, error) {
	_, err := r.f.NewObject(ctx, "config")
	if errIsNotFound(err) || errors.Is(err, fs.ErrorDirNotFound) {
		return false, nil
	}
	if err != nil {
		return false, engineErr(err, "reading the backup")
	}
	return true, nil
}

// resticOut is the end of what a run printed on stderr, for its error.
type resticOut struct {
	mu   sync.Mutex
	tail []string
}

func (o *resticOut) add(s string) {
	o.mu.Lock()
	o.tail = append(o.tail, s)
	if len(o.tail) > 20 {
		o.tail = o.tail[1:]
	}
	o.mu.Unlock()
}

func (o *resticOut) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.Join(o.tail, "\n")
}

// command builds a restic invocation on the repository.
func (r *resticRepo) command(ctx context.Context, args ...string) *exec.Cmd {
	full := []string{"--repo", "rest:http+unix://" + filepath.Join(r.dir, "s") + ":/",
		"--cache-dir", r.e.cfg.ResticCacheDir, "--no-extra-verify"}
	env := []string{"PATH=/usr/bin:/bin", "TMPDIR=" + r.e.cfg.StagingDir, "RESTIC_PROGRESS_FPS=1"}
	if r.t.enc != nil && r.t.enc.Mode == EncryptRestic {
		env = append(env, "RESTIC_PASSWORD="+r.t.enc.Keys.Password)
	} else {
		full = append(full, "--insecure-no-password")
	}
	cmd := exec.CommandContext(ctx, r.bin, append(full, args...)...)
	cmd.Env = env
	// A stop asks restic to finish cleanly (it removes its lock); a kill
	// follows if it doesn't.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 30 * time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return cmd
}

// lineWriter calls fn with each complete line written to it.
type lineWriter struct {
	mu  *sync.Mutex
	fn  func([]byte)
	buf []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		w.call(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
}

func (w *lineWriter) call(b []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fn(b)
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.call(w.buf)
		w.buf = nil
	}
}

// run runs restic and hands every output line (JSON with --json) to
// line, one call at a time; stderr lines that aren't JSON go to the job
// log. The error says why restic failed; exit code 3 ("some files could
// not be read") is errIncomplete.
func (r *resticRepo) run(line func([]byte), args ...string) error {
	if line == nil {
		line = func([]byte) {}
	}
	var mu sync.Mutex
	lw := &lineWriter{mu: &mu, fn: line}
	defer lw.flush()
	return r.exec(lw, &mu, line, args...)
}

// exec runs restic with its output going to stdout as it is.
func (r *resticRepo) exec(stdout io.Writer, mu *sync.Mutex, jsonErr func([]byte), args ...string) error {
	for _, d := range []string{r.e.cfg.ResticCacheDir, r.e.cfg.StagingDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return Errorf(classifyError(err), "creating %s: %w", d, err)
		}
	}
	if mu == nil {
		mu = &sync.Mutex{}
	}
	var out resticOut
	ew := &lineWriter{mu: mu, fn: func(b []byte) {
		out.add(string(b))
		if len(b) > 0 && b[0] == '{' {
			// --json errors: the caller logs file errors itself.
			if jsonErr != nil {
				jsonErr(b)
			}
			return
		}
		r.log(LogWarn, "restic: "+string(b))
	}}
	cmd := r.command(r.ctx, args...)
	cmd.Stdout, cmd.Stderr = stdout, ew
	err := cmd.Run()
	ew.flush()
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return Errorf(CodeResticMissing, "starting restic: %w", err)
	}
	return r.exitErr(err, out.String())
}

var errIncomplete = errors.New("restic could not read some files")

func (r *resticRepo) exitErr(err error, tail string) error {
	if err == nil {
		return nil
	}
	if r.ctx.Err() != nil {
		if c := context.Cause(r.ctx); c != nil {
			return c
		}
		return r.ctx.Err()
	}
	var ee *exec.ExitError
	code := -1
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	switch code {
	case 3:
		return errIncomplete
	case 12:
		return Errorf(CodeWrongPassword, "the password does not open this restic repository")
	}
	if be := r.lastBackendErr(); be != nil {
		return engineErr(be, "the backup location")
	}
	switch code {
	case 10:
		return Errorf(CodeNotFound, "there is no restic repository at %s", r.t.display)
	case 11:
		return Errorf(CodeIOError, "the repository is locked by another restic run: %s", tail)
	}
	return Errorf(CodeIOError, "restic failed (%v): %s", err, tail)
}

// unlockStale removes the locks of restic runs that no longer exist (a
// crash): restic passes over them for its shared lock, but not for the
// exclusive one forget, prune and check take.
func (r *resticRepo) unlockStale() error { return r.run(nil, "unlock") }

// runJSON runs restic with --json and decodes its one JSON document.
func (r *resticRepo) runJSON(v interface{}, args ...string) error {
	var buf bytes.Buffer
	if err := r.exec(&buf, nil, nil, append(args, "--json")...); err != nil {
		return err
	}
	if err := json.NewDecoder(&buf).Decode(v); err != nil {
		return Errorf(CodeIOError, "reading restic's answer: %w", err)
	}
	return nil
}

// resticMsg is one line of restic's --json output.
type resticMsg struct {
	MessageType string `json:"message_type"`
	// status
	TotalFiles   int64    `json:"total_files"`
	FilesDone    int64    `json:"files_done"`
	TotalBytes   int64    `json:"total_bytes"`
	BytesDone    int64    `json:"bytes_done"`
	CurrentFiles []string `json:"current_files"`
	// error
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	Item string `json:"item"`
	// verbose_status (dry run)
	Action   string `json:"action"`
	DataSize int64  `json:"data_size"`
	// summary
	FilesNew            int64  `json:"files_new"`
	FilesChanged        int64  `json:"files_changed"`
	DataAdded           int64  `json:"data_added"`
	TotalFilesProcessed int64  `json:"total_files_processed"`
	TotalBytesProcessed int64  `json:"total_bytes_processed"`
	SnapshotID          string `json:"snapshot_id"`
	NumErrors           int64  `json:"num_errors"` // check
}

// resticExcludes renders a job's filters as restic exclude patterns: a
// rule anchored with "/" is relative to its source, so it is anchored at
// each source's absolute path; the rest match at any depth in both.
func resticExcludes(p *prepared) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for i, t := range p.sources {
		rules, err := p.sourceSpec(i, false).rules()
		if err != nil {
			return nil, err
		}
		for _, rule := range rules {
			g, ok := strings.CutPrefix(rule, "- ")
			if !ok {
				return nil, Errorf(CodeInvalidFilter, "versioned backups only take exclude rules, not %q", rule)
			}
			if strings.HasPrefix(g, "/") {
				g = escapeGlob(t.path) + g
			}
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	return out, nil
}

// resticPaths are the absolute folders a backup reads: restic reads
// local folders only.
func resticPaths(p *prepared) ([]string, error) {
	var out []string
	for _, t := range p.sources {
		if !t.local {
			return nil, Errorf(CodePathNotAllowed, "a versioned backup reads local folders only, not %s", t.display)
		}
		out = append(out, t.path)
	}
	return out, nil
}

// resticTuning is restic's backend parallelism and pack size for a
// destination: Google Drive gets many uploads in flight (each one is
// throttled) and 64 MiB packs, its upload chunk, so far fewer files.
func resticTuning(ctx context.Context, p *prepared, o Options) []string {
	args := []string{"-o", fmt.Sprintf("rest.connections=%d", max(fs.GetConfig(ctx).Transfers, 1))}
	if p.dest.q.Drive {
		args = append(args, "--pack-size", "64")
	}
	if o.LowPriority {
		args = append(args, "--read-concurrency", "1")
	}
	return args
}

// runBackup saves one snapshot of every source (or previews it).
func (e *Engine) runBackup(ctx context.Context, j *job, dry bool) (Result, error) {
	r := j.req
	p, res, err := e.prepareRun(ctx, j, jobTypeBackup)
	if err != nil {
		return res, err
	}
	j.setTotals(p.scan.Files, p.scan.Bytes)
	res.Counts.SourceFiles = p.scan.Files
	res.Counts.BytesTotal = p.scan.Bytes
	paths, err := resticPaths(p)
	if err != nil {
		return res, err
	}
	excludes, err := resticExcludes(p)
	if err != nil {
		return res, err
	}
	rctx, _ := transferConfig(ctx, p, r.Options)
	var dst fs.Fs
	var guard *mountGuard
	if dry {
		dst, err = p.dest.fsAt(rctx, "", fsOpts{})
	} else {
		dst, guard, err = e.openDest(rctx, j, p.dest, "", fsOpts{}, r.DestFolderID)
	}
	if err != nil {
		return res, engineErr(err, "opening the destination")
	}
	repo, err := e.openRepo(rctx, p.dest, dst, j.logRaw)
	if err != nil {
		return res, err
	}
	defer repo.close()
	exists, err := repo.exists(rctx)
	if err != nil {
		return res, err
	}
	if dry && !exists {
		// Nothing saved yet: everything is new.
		res.Counts.Added, res.Counts.BytesAdd = p.scan.Files, p.scan.Bytes
		return res, nil
	}
	if !dry {
		if p.marker == nil {
			if err := guard.check(); err != nil {
				return res, err
			}
			if p.dest.local {
				if err := mkdirPrivate(p.dest.path); err != nil {
					return res, err
				}
			}
			if err := e.writeMarker(ctx, p.dest, r.JobID, r.DestFolderID); err != nil {
				return res, err
			}
			res.MarkerWritten = true
			j.logLine(LogInfo, "marker_written", "backup.log.marker_written", nil)
		}
		if !exists {
			if err := repo.run(nil, "init"); err != nil {
				return res, err
			}
			j.logRaw(LogInfo, "created the restic repository at "+p.dest.display)
		}
	}

	if !dry {
		if err := repo.unlockStale(); err != nil {
			return res, err
		}
		repo.sweepPartials(rctx)
	}
	j.phase(stepTransfer, "backup.phase.transfer")
	exFile := filepath.Join(repo.dir, "excludes")
	if err := os.WriteFile(exFile, []byte(strings.Join(excludes, "\n")+"\n"), 0o600); err != nil {
		return res, Errorf(CodeIOError, "writing the exclude list: %w", err)
	}
	args := []string{"backup", "--json", "--host", resticHost, "--one-file-system", "--exclude-file", exFile}
	args = append(args, resticTuning(rctx, p, r.Options)...)
	if r.Filters.MaxSizeBytes > 0 {
		args = append(args, "--exclude-larger-than", fmt.Sprint(r.Filters.MaxSizeBytes))
	}
	var preview *bufio.Writer
	if dry {
		args = append(args, "--dry-run", "-v")
		if r.PreviewFile != "" {
			pf, err := createPreviewFile(r.PreviewFile)
			if err != nil {
				return res, err
			}
			defer pf.Close()
			preview = bufio.NewWriter(pf)
			defer preview.Flush()
		}
	}
	args = append(append(args, "--"), paths...)
	var sum resticMsg
	var added int64
	err = repo.run(func(b []byte) {
		var m resticMsg
		if json.Unmarshal(b, &m) != nil {
			return
		}
		switch m.MessageType {
		case "status":
			cur := ""
			if len(m.CurrentFiles) > 0 {
				cur = m.CurrentFiles[0]
			}
			j.setTotals(m.TotalFiles, m.TotalBytes)
			j.setOwn(m.BytesDone, m.FilesDone, cur)
		case "error":
			res.Counts.Errored++
			if len(res.FileErrors) < maxFileErrors {
				res.FileErrors = append(res.FileErrors, FileError{Path: m.Item, Code: CodeIOError, Detail: m.Error.Message})
			}
			j.appendLine(LogLine{T: time.Now(), Lvl: LogError, Code: "file_error", MsgKey: "backup.log.file_error",
				Args: map[string]interface{}{"path": m.Item, "reason_key": "backup.err.io_error.title"}, Raw: m.Error.Message})
		case "verbose_status":
			op := map[string]string{"new": "add", "modified": "update"}[m.Action]
			if op == "" || strings.HasSuffix(m.Item, "/") {
				return
			}
			if preview != nil && (op == "update" || added < planListAdds) {
				raw, _ := json.Marshal(PreviewItem{Op: op, Path: m.Item, Size: m.DataSize})
				preview.Write(append(raw, '\n'))
			}
			if op == "add" {
				added++
			}
			res.Counts.BytesAdd += m.DataSize
		case "summary":
			sum = m
		}
	}, args...)
	if err != nil && err != errIncomplete {
		return res, err
	}
	res.Counts.Added, res.Counts.Changed = sum.FilesNew, sum.FilesChanged
	res.Counts.SourceFiles = sum.TotalFilesProcessed
	res.Counts.BytesTotal = sum.TotalBytesProcessed
	if dry {
		if preview != nil && added > planListAdds {
			raw, _ := json.Marshal(PreviewItem{Op: PreviewOmittedAdd, Count: added - planListAdds})
			preview.Write(append(raw, '\n'))
		}
		return res, nil
	}
	res.Counts.BytesTransferred = repo.uploaded.Load()
	if sum.SnapshotID != "" {
		res.ArchiveName = sum.SnapshotID[:min(8, len(sum.SnapshotID))]
	}
	j.logRaw(LogInfo, fmt.Sprintf("snapshot %s saved: %d new, %d changed files; %d bytes of new data, %d bytes uploaded",
		res.ArchiveName, sum.FilesNew, sum.FilesChanged, sum.DataAdded, res.Counts.BytesTransferred))
	if r.Options.Verify {
		j.phase(stepVerify, "backup.phase.verify")
		if err := repo.run(nil, "check"); err != nil {
			return res, err
		}
	}
	return res, nil
}

// openDestRepo opens a job destination's repository for an op that
// only reads it (no lock is taken) or, with write, prunes it.
func (e *Engine) openDestRepo(ctx context.Context, j *job, t *target, write bool) (*resticRepo, error) {
	var f fs.Fs
	var err error
	if write {
		f, _, err = e.openDest(ctx, j, t, "", fsOpts{}, j.req.DestFolderID)
	} else {
		f, err = t.fsAt(ctx, "", fsOpts{})
	}
	if err != nil {
		return nil, engineErr(err, "opening the backup")
	}
	var log func(lvl, msg string)
	if j != nil {
		log = j.logRaw
	}
	return e.openRepo(ctx, t, f, log)
}

// runForget drops the snapshots the retention doesn't keep, then the
// data only they used.
func (e *Engine) runForget(ctx context.Context, j *job) (Result, error) {
	var res Result
	t, err := e.prepareDestOp(ctx, j)
	if err != nil {
		return res, err
	}
	if t.local {
		res.MountID = t.mv.m.ID
	}
	j.phase(stepPrune, "backup.phase.prune")
	ret := j.req.Retention
	args := []string{"forget", "--host", resticHost}
	for _, k := range []struct {
		flag string
		n    int
	}{{"--keep-last", ret.KeepLast}, {"--keep-daily", ret.KeepDaily}, {"--keep-weekly", ret.KeepWeekly}, {"--keep-monthly", ret.KeepMonthly}} {
		if k.n > 0 {
			args = append(args, k.flag, fmt.Sprint(k.n))
		}
	}
	if len(args) == 3 {
		return res, nil // keep everything
	}
	rctx, _ := rcloneContext(ctx)
	repo, err := e.openDestRepo(rctx, j, t, true)
	if err != nil {
		return res, err
	}
	defer repo.close()
	if err := repo.unlockStale(); err != nil {
		return res, err
	}
	repo.sweepPartials(rctx)
	var groups []struct {
		Remove []struct {
			ID   string    `json:"id"`
			Time time.Time `json:"time"`
		} `json:"remove"`
	}
	if err := repo.runJSON(&groups, args...); err != nil {
		return res, err
	}
	for _, g := range groups {
		for _, s := range g.Remove {
			res.Counts.Deleted++
			j.logLine(LogInfo, "pruned", "backup.log.pruned", map[string]interface{}{"name": s.Time.UTC().Format(time.RFC3339)})
		}
	}
	if res.Counts.Deleted > 0 {
		// Only data the dropped snapshots used became unused; 10 % unused
		// space is left in mixed packs rather than rewritten.
		if err := repo.run(nil, "prune", "--max-unused", "10%"); err != nil {
			return res, err
		}
	}
	return res, nil
}

// checkSubset is how much of the stored data a check reads back.
const checkSubset = "5%"

// runResticCheck checks the repository's structure and reads a sample
// of its data back (restic check --read-data-subset).
func (e *Engine) runResticCheck(ctx context.Context, j *job) (Result, error) {
	var res Result
	t, err := e.prepareDestOp(ctx, j)
	if err != nil {
		return res, err
	}
	j.phase(stepVerify, "backup.phase.verify")
	rctx, _ := rcloneContext(ctx)
	repo, err := e.openDestRepo(rctx, j, t, false)
	if err != nil {
		return res, err
	}
	defer repo.close()
	var snaps []resticSnapshot
	if err := repo.runJSON(&snaps, "snapshots", "--no-lock"); err != nil {
		return res, err
	}
	res.Counts.Skipped = int64(len(snaps))
	if err := repo.unlockStale(); err != nil {
		return res, err
	}
	var sum resticMsg
	err = repo.run(func(b []byte) {
		var m resticMsg
		if json.Unmarshal(b, &m) == nil && m.MessageType == "summary" {
			sum = m
		}
	}, "check", "--json", "--read-data-subset", checkSubset)
	if err != nil && sum.NumErrors == 0 {
		return res, err
	}
	if sum.NumErrors > 0 {
		res.Counts.Errored = sum.NumErrors
		res.FileErrors = []FileError{{Code: CodeIOError, Detail: fmt.Sprintf("restic check found %d problems in the repository; see the log", sum.NumErrors)}}
		j.appendLine(LogLine{T: time.Now(), Lvl: LogError, Code: "file_error", MsgKey: "backup.log.file_error",
			Args: map[string]interface{}{"path": t.display, "reason_key": "backup.err.io_error.title"}, Raw: res.FileErrors[0].Detail})
	}
	return res, nil
}

// resticSnapshot is one entry of restic snapshots --json.
type resticSnapshot struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Paths   []string  `json:"paths"`
	Summary *struct {
		Files int64 `json:"total_files_processed"`
		Bytes int64 `json:"total_bytes_processed"`
	} `json:"summary"`
}

// root is where a snapshot's version paths start: the source folder of a
// one-source backup, the filesystem root otherwise.
func (s resticSnapshot) root() string {
	if len(s.Paths) == 1 {
		return s.Paths[0]
	}
	return "/"
}

// snapshotVersions lists a repository's snapshots, newest first.
func (e *Engine) snapshotVersions(ctx context.Context, t *target) ([]Version, error) {
	repo, err := e.openDestRepo(ctx, nil, t, false)
	if err != nil {
		return nil, err
	}
	defer repo.close()
	if ok, err := repo.exists(ctx); err != nil || !ok {
		return []Version{}, err
	}
	var snaps []resticSnapshot
	if err := repo.runJSON(&snaps, "snapshots", "--no-lock"); err != nil {
		return nil, err
	}
	sort.Slice(snaps, func(i, k int) bool { return snaps[i].Time.After(snaps[k].Time) })
	out := []Version{}
	for _, s := range snaps {
		ts := s.Time.UTC()
		v := Version{ID: snapshotPrefix + s.ID, Kind: VersionSnapshot, Time: &ts, LabelKey: "backup.ver.snapshot"}
		if s.Summary != nil {
			v.Files, v.Bytes = s.Summary.Files, s.Summary.Bytes
		}
		out = append(out, v)
	}
	return out, nil
}

// snapshotRepo opens a destination's repository and finds a snapshot.
func (e *Engine) snapshotRepo(ctx context.Context, j *job, t *target, id string) (*resticRepo, resticSnapshot, error) {
	repo, err := e.openDestRepo(ctx, j, t, false)
	if err != nil {
		return nil, resticSnapshot{}, err
	}
	var snaps []resticSnapshot
	if err := repo.runJSON(&snaps, "snapshots", "--no-lock", id); err != nil || len(snaps) != 1 {
		repo.close()
		if err == nil || CodeOf(err) == CodeIOError {
			err = Errorf(CodeNotFound, "no snapshot %s in the backup", id)
		}
		return nil, resticSnapshot{}, err
	}
	return repo, snaps[0], nil
}

// resticNode is one line of restic ls --json.
type resticNode struct {
	Name  string    `json:"name"`
	Type  string    `json:"type"`
	Path  string    `json:"path"`
	Size  int64     `json:"size"`
	Mode  uint32    `json:"mode"`
	UID   int       `json:"uid"`
	GID   int       `json:"gid"`
	MTime time.Time `json:"mtime"`
	Link  string    `json:"linktarget"`
	Msg   string    `json:"message_type"`
}

// ls lists rel of a snapshot (relative to its root): rel itself first,
// then its children (or, recursive, everything below it).
func (r *resticRepo) ls(s resticSnapshot, rel string, recursive bool, each func(resticNode)) error {
	args := []string{"ls", "--json", "--no-lock", s.ID + ":" + s.root(), "/" + rel}
	if recursive {
		args = append(args, "--recursive")
	}
	return r.run(func(b []byte) {
		var n resticNode
		if json.Unmarshal(b, &n) == nil && n.Msg == "node" {
			each(n)
		}
	}, args...)
}

// browseSnapshot lists one folder of a snapshot.
func (e *Engine) browseSnapshot(ctx context.Context, t *target, id, rel string) ([]Entry, error) {
	repo, s, err := e.snapshotRepo(ctx, nil, t, id)
	if err != nil {
		return nil, err
	}
	defer repo.close()
	self := "/" + rel
	out := []Entry{}
	found := false
	err = repo.ls(s, rel, false, func(n resticNode) {
		if n.Path == self || (rel == "" && n.Path == "/") {
			found = true
			return
		}
		mt := n.MTime
		out = append(out, Entry{Name: n.Name, Dir: n.Type == "dir", Size: n.Size, MTime: &mt})
	})
	if err != nil {
		return nil, err
	}
	if !found && rel != "" && len(out) == 0 {
		return nil, Errorf(CodeNotFound, "%s is not in this snapshot", rel)
	}
	return out, nil
}

// stat finds rel in a snapshot and the bytes of the files below it.
func (r *resticRepo) stat(s resticSnapshot, rel string) (*resticNode, int64, error) {
	var node *resticNode
	if rel == "" {
		node = &resticNode{Type: "dir", Path: "/"} // the root lists no node of its own
	}
	var total int64
	err := r.ls(s, rel, true, func(n resticNode) {
		if node == nil && n.Path == "/"+rel {
			c := n
			node = &c
		}
		if n.Type == "file" {
			total += n.Size
		}
	})
	if err != nil {
		return nil, 0, err
	}
	if node == nil {
		return nil, 0, Errorf(CodeNotFound, "%s is not in this snapshot", rel)
	}
	return node, total, nil
}

// snapshotTar streams rel of a snapshot as a tar, member names relative
// to the snapshot root: restic dumps a folder as a tar; a single file is
// wrapped in a header made from its listing. wait reports the dump's
// error once the reader is drained.
func (r *resticRepo) snapshotTar(s resticSnapshot, rel string) (*tar.Reader, func() error, int64, error) {
	node, total, err := r.stat(s, rel)
	if err != nil {
		return nil, nil, 0, err
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	if node.Type == "dir" {
		go func() {
			err := r.exec(pw, nil, nil, "dump", "--no-lock", "--archive", "tar", s.ID+":"+s.root(), "/"+rel)
			pw.CloseWithError(err)
			done <- err
		}()
		return tar.NewReader(pr), func() error { return <-done }, total, nil
	}
	go func() {
		tw := tar.NewWriter(pw)
		h := &tar.Header{Name: rel, Mode: int64(node.Mode & 0o7777), Uid: node.UID, Gid: node.GID, ModTime: node.MTime, Format: tar.FormatPAX}
		var err error
		switch node.Type {
		case "symlink":
			h.Typeflag, h.Linkname = tar.TypeSymlink, node.Link
			err = tw.WriteHeader(h)
		case "file":
			h.Typeflag, h.Size = tar.TypeReg, node.Size
			if err = tw.WriteHeader(h); err == nil {
				err = r.exec(tw, nil, nil, "dump", "--no-lock", s.ID+":"+s.root(), "/"+rel)
			}
		default:
			err = Errorf(CodeNotFound, "%s is a %s, which can't be restored", rel, node.Type)
		}
		if err == nil {
			err = tw.Close()
		}
		pw.CloseWithError(err)
		done <- err
	}()
	return tar.NewReader(pr), func() error { return <-done }, total, nil
}

// restoreSnapshot extracts paths (all when empty) of a snapshot to the
// restore target, with the same conflict rules as an archive restore.
func (e *Engine) restoreSnapshot(ctx context.Context, j *job, dest *target, id string, paths []string, tgt *target, tf fs.Fs, guard *mountGuard, spec RestoreSpec, c *restoreCounter) error {
	repo, s, err := e.snapshotRepo(ctx, j, dest, id)
	if err != nil {
		return err
	}
	defer repo.close()
	roots := paths
	if len(roots) == 0 {
		roots = []string{""}
	}
	x := e.newExtractor(j, tgt, tf, guard, spec, c)
	for _, rel := range roots {
		tr, wait, total, err := repo.snapshotTar(s, rel)
		if CodeOf(err) == CodeNotFound {
			c.skip(j, rel, reasonNotFound)
			continue
		}
		if err != nil {
			return err
		}
		if free := e.freeSpace(ctx, tgt); free != nil && !spec.DryRun && *free < total {
			return Errorf(CodeNoSpace, "restoring needs %d bytes, the folder has %d free", total, *free)
		}
		err = x.extractAll(ctx, tr, nil)
		_, _ = io.Copy(io.Discard, tr)
		if werr := wait(); err == nil {
			err = werr
		}
		if err != nil {
			return err
		}
	}
	return x.finishDirs()
}

// downloadSnapshot streams paths of a snapshot: one file raw, else a zip.
func (e *Engine) downloadSnapshot(ctx context.Context, t *target, id string, paths []string) (*Download, error) {
	repo, s, err := e.snapshotRepo(ctx, nil, t, id)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	if len(paths) == 1 {
		node, _, err := repo.stat(s, paths[0])
		if err != nil {
			repo.close()
			return nil, err
		}
		if node.Type == "file" {
			go func() {
				defer repo.close()
				pw.CloseWithError(repo.exec(pw, nil, nil, "dump", "--no-lock", s.ID+":"+s.root(), "/"+paths[0]))
			}()
			name := path.Base(paths[0])
			return &Download{Name: name, ContentType: contentType(name), Size: node.Size, Body: pr}, nil
		}
	}
	go func() {
		defer repo.close()
		zw := zip.NewWriter(pw)
		found := false
		for _, p := range paths {
			tr, wait, _, err := repo.snapshotTar(s, p)
			if CodeOf(err) == CodeNotFound {
				continue
			}
			if err == nil {
				var f bool
				f, err = zipFromTar(zw, tr, nil)
				found = found || f
				_, _ = io.Copy(io.Discard, tr)
				if werr := wait(); err == nil {
					err = werr
				}
			}
			if err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		if !found {
			pw.CloseWithError(Errorf(CodeNotFound, "none of the files are in this snapshot"))
			return
		}
		pw.CloseWithError(zw.Close())
	}()
	return &Download{Name: zipName(paths), ContentType: "application/zip", Size: -1, Body: pr}, nil
}
