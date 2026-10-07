package engine

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	rfs "github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/operations"
)

// Encrypted archives (Encryption.Mode "archive", spec §18): each run is
// one full 7z, AES-256 with encrypted headers (-mhe=on: without the
// password not even the file names show), split into volumes:
//
//	nivaro_<job>_<ts>.7z.001, .002, ...  the archive (a tar inside, so owners,
//	                                      modes and links survive as with .tar.zst)
//	nivaro_<job>_<ts>.index.7z           its index, encrypted the same way
//
// The index is uploaded last: a set without it is unfinished and never
// listed (and removed by the next run). A password-protected ZIP is not
// offered on purpose: ZipCrypto is broken and even AES ZIPs show every
// file name.
//
// 7z only reads the password from its stdin (never from the command
// line, which every local user can read in /proc), so the tar is built in
// the staging folder first and 7z reads it from there.

const (
	sevenZipExt   = ".7z"
	sevenZipIndex = ".index.7z"
)

// sevenZipBins are the names 7-Zip is installed as (7zip, p7zip-full).
var sevenZipBins = []string{"7z", "7zz", "7za"}

// SevenZip returns the 7z program's path, "" when it isn't installed.
func SevenZip() string {
	for _, b := range sevenZipBins {
		if p, err := exec.LookPath(b); err == nil {
			return p
		}
	}
	return ""
}

func (t *target) sevenZip() bool { return t.enc != nil && t.enc.Mode == EncryptArchive }

// archivePassword is what opens t's archives ("" for .tar.zst ones).
func (t *target) archivePassword() string {
	if t.sevenZip() {
		return t.enc.Keys.Password
	}
	return ""
}

func (t *target) volumeBytes() int64 {
	if t.enc != nil && t.enc.VolumeBytes > 0 {
		return t.enc.VolumeBytes
	}
	return DefaultVolumeBytes
}

// volumeRe matches one volume of a 7z set: "<set>.001".
var volumeRe = regexp.MustCompile(`^(.+\.7z)\.(\d{3,})$`)

// sevenZipIndexName is the index of a set ("nivaro_x_ts.7z" ->
// "nivaro_x_ts.index.7z").
func sevenZipIndexName(set string) string {
	return strings.TrimSuffix(set, sevenZipExt) + sevenZipIndex
}

// sevenZipSets groups the 7z volumes among entries by set: set -> total
// size; finished reports the sets whose index exists.
func sevenZipSets(entries rfs.DirEntries) (sizes map[string]int64, vols map[string][]string, finished map[string]bool) {
	sizes, vols, finished = map[string]int64{}, map[string][]string{}, map[string]bool{}
	for _, en := range entries {
		o, ok := en.(rfs.Object)
		if !ok {
			continue
		}
		base := path.Base(o.Remote())
		if m := volumeRe.FindStringSubmatch(base); m != nil {
			sizes[m[1]] += max(o.Size(), 0)
			vols[m[1]] = append(vols[m[1]], base)
		} else if strings.HasSuffix(base, sevenZipIndex) {
			finished[strings.TrimSuffix(base, sevenZipIndex)+sevenZipExt] = true
		}
	}
	for _, v := range vols {
		sort.Strings(v)
	}
	return sizes, vols, finished
}

// run7z runs 7z with the password on its stdin and returns its error,
// mapped to wrong_password when that is what 7z said.
func run7z(ctx context.Context, dir, pw string, args ...string) error {
	bin := SevenZip()
	if bin == "" {
		return Errorf(CodeSevenZipMissing, "7z is not installed (package 7zip or p7zip-full)")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(pw + "\n")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return sevenZipErr(ctx, err, out.String())
	}
	return nil
}

func sevenZipErr(ctx context.Context, err error, out string) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if strings.Contains(out, "Wrong password") {
		return Errorf(CodeWrongPassword, "the password does not open the archive")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return Errorf(CodeIOError, "7z: %v: %s", err, strings.Join(lines[max(0, len(lines)-4):], " | "))
}

// build7z writes srcs as one encrypted, split 7z set into dst: the tar is
// staged, 7z encrypts it into volumes, and each finished volume is
// uploaded (and deleted from staging) while 7z writes the next one.
func (e *Engine) build7z(ctx context.Context, w *tarWriter, dst rfs.Fs, set string, srcs []archiveSource, pw string, volBytes int64, conc int) (*archiveIndex, int64, error) {
	if SevenZip() == "" {
		return nil, 0, Errorf(CodeSevenZipMissing, "7z is not installed (package 7zip or p7zip-full)")
	}
	if err := os.MkdirAll(e.cfg.StagingDir, 0o700); err != nil {
		return nil, 0, Errorf(classifyError(err), "creating the staging folder: %w", err)
	}
	dir, err := os.MkdirTemp(e.cfg.StagingDir, ".7z-*")
	if err != nil {
		return nil, 0, Errorf(classifyError(err), "creating the staging folder: %w", err)
	}
	defer os.RemoveAll(dir)
	tarName := strings.TrimSuffix(set, sevenZipExt) + ".tar"
	tf, err := os.OpenFile(filepath.Join(dir, tarName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, 0, Errorf(classifyError(err), "creating the staging file: %w", err)
	}
	err = w.writeTar(ctx, tf, srcs, conc, false)
	if cerr := tf.Close(); err == nil && cerr != nil {
		err = Errorf(classifyError(cerr), "writing the staging file: %w", cerr)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, context.Cause(ctx)
		}
		return nil, 0, engineErr(err, "")
	}

	lf, err := newBackendFs(ctx, "local", ":local", dir, configmap.Simple{})
	if err != nil {
		return nil, 0, engineErr(err, "opening the staging folder")
	}
	var uploaded []string
	var written int64
	upload := func(name string) error {
		o, err := lf.NewObject(ctx, name)
		if err != nil {
			return engineErr(err, "reading "+name)
		}
		if _, err := operations.Copy(ctx, dst, nil, name, o); err != nil {
			return engineErr(err, "uploading "+name)
		}
		uploaded = append(uploaded, name)
		written += o.Size()
		return os.Remove(filepath.Join(dir, name))
	}
	fail := func(err error) (*archiveIndex, int64, error) {
		for _, n := range uploaded {
			e.removeQuietly(ctx, dst, n)
		}
		return nil, 0, err
	}

	// -mx1: fast LZMA2 (most backups are photos and videos); -mhe=on:
	// names encrypted; -p with no value: the password comes on stdin.
	args := []string{"a", "-t7z", "-bd", "-y", "-mhe=on", "-mx=1", fmt.Sprintf("-mmt=%d", conc),
		"-v" + strconv.FormatInt(volBytes, 10) + "b", "-p", "-sdel", "--", set, tarName}
	cmd := exec.CommandContext(ctx, SevenZip(), args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(pw + "\n")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return nil, 0, Errorf(CodeIOError, "starting 7z: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	vol := func(n int) string { return fmt.Sprintf("%s.%03d", set, n) }
	next := 1
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for running := true; running; {
		select {
		case err := <-done:
			if err != nil {
				return fail(sevenZipErr(ctx, err, out.String()))
			}
			running = false
		case <-tick.C:
		}
		// Volume n is complete once 7z has started n+1 (or exited).
		for {
			if _, err := os.Stat(filepath.Join(dir, vol(next))); err != nil {
				break
			}
			if _, err := os.Stat(filepath.Join(dir, vol(next+1))); running && err != nil {
				break
			}
			if err := upload(vol(next)); err != nil {
				cmd.Process.Kill()
				<-done
				return fail(err)
			}
			w.j.addOwn(0, 0, vol(next))
			next++
		}
	}
	if next == 1 {
		return fail(Errorf(CodeIOError, "7z wrote no volumes: %s", strings.TrimSpace(out.String())))
	}

	// The index last: it marks the set finished.
	idx := w.result(e.now())
	var nd bytes.Buffer
	je := json.NewEncoder(&nd)
	_ = je.Encode(idx.header)
	for _, en := range idx.entries {
		_ = je.Encode(en)
	}
	idxTmp := strings.TrimSuffix(set, sevenZipExt) + ".index.jsonl"
	if err := os.WriteFile(filepath.Join(dir, idxTmp), nd.Bytes(), 0o600); err != nil {
		return fail(Errorf(classifyError(err), "writing the index: %w", err))
	}
	idxName := sevenZipIndexName(set)
	if err := run7z(ctx, dir, pw, "a", "-t7z", "-bd", "-y", "-mhe=on", "-p", "--", idxName, idxTmp); err != nil {
		return fail(err)
	}
	if err := upload(idxName); err != nil {
		return fail(err)
	}
	return idx, written, nil
}

// procReader is a 7z's stdout that reports how 7z ended: a wrong
// password or a damaged volume is an error, never a short, clean EOF.
type procReader struct {
	ctx  context.Context
	r    io.Reader
	cmd  *exec.Cmd
	out  *bytes.Buffer
	done bool
	err  error
}

func (p *procReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if err == io.EOF {
		if werr := p.wait(); werr != nil {
			return n, werr
		}
	}
	return n, err
}

func (p *procReader) wait() error {
	if !p.done {
		p.done = true
		if err := p.cmd.Wait(); err != nil {
			p.err = sevenZipErr(p.ctx, err, p.out.String())
		}
	}
	return p.err
}

// open7z decrypts one 7z (a set's first volume, or an index) of f and
// streams its only member. A remote's files are fetched to the staging
// folder first; a local destination's are read in place.
func (e *Engine) open7z(ctx context.Context, f rfs.Fs, first, pw string) (io.Reader, func(), error) {
	if SevenZip() == "" {
		return nil, nil, Errorf(CodeSevenZipMissing, "7z is not installed (package 7zip or p7zip-full)")
	}
	names := []string{first}
	if m := volumeRe.FindStringSubmatch(first); m != nil {
		entries, err := f.List(ctx, "")
		if err != nil {
			return nil, nil, engineErr(err, "listing the backup")
		}
		_, vols, _ := sevenZipSets(entries)
		names = vols[m[1]]
		if len(names) == 0 || names[0] != first {
			return nil, nil, Errorf(CodeNotFound, "archive %s not found", m[1])
		}
	}
	dir, cleanup := "", func() {}
	if f.Features().IsLocal {
		dir = f.Root()
	} else {
		if err := os.MkdirAll(e.cfg.StagingDir, 0o700); err != nil {
			return nil, nil, Errorf(classifyError(err), "creating the staging folder: %w", err)
		}
		tmp, err := os.MkdirTemp(e.cfg.StagingDir, ".7z-read-*")
		if err != nil {
			return nil, nil, Errorf(classifyError(err), "creating the staging folder: %w", err)
		}
		cleanup = func() { os.RemoveAll(tmp) }
		lf, err := newBackendFs(ctx, "local", ":local", tmp, configmap.Simple{})
		if err != nil {
			cleanup()
			return nil, nil, engineErr(err, "opening the staging folder")
		}
		for _, n := range names {
			o, err := f.NewObject(ctx, n)
			if err == nil {
				_, err = operations.Copy(ctx, lf, nil, n, o)
			}
			if err != nil {
				cleanup()
				if errIsNotFound(err) {
					return nil, nil, Errorf(CodeNotFound, "%s not found", n)
				}
				return nil, nil, engineErr(err, "downloading "+n)
			}
		}
		dir = tmp
	}
	cctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cctx, SevenZip(), "x", "-so", "-bd", "-y", "--", first)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(pw + "\n")
	var out bytes.Buffer
	cmd.Stderr = &out
	stdout, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		cancel()
		cleanup()
		return nil, nil, Errorf(CodeIOError, "starting 7z: %w", err)
	}
	pr := &procReader{ctx: ctx, r: bufio.NewReaderSize(stdout, 1<<20), cmd: cmd, out: &out}
	closer := func() {
		cancel()
		_ = pr.wait()
		cleanup()
	}
	return pr, closer, nil
}

// open7zArchive is openArchive for a 7z set.
func (e *Engine) open7zArchive(ctx context.Context, f rfs.Fs, set, pw string) (*tar.Reader, func(), error) {
	r, closer, err := e.open7z(ctx, f, set+".001", pw)
	if err != nil {
		return nil, nil, err
	}
	return tar.NewReader(r), closer, nil
}

// read7zIndex is readIndex for a 7z set.
func (e *Engine) read7zIndex(ctx context.Context, f rfs.Fs, set, pw string) (*archiveIndex, bool, error) {
	r, closer, err := e.open7z(ctx, f, sevenZipIndexName(set), pw)
	if CodeOf(err) == CodeNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer closer()
	idx, err := decodeIndex(r)
	if err != nil {
		var ee *Error
		if errors.As(err, &ee) {
			return nil, false, err
		}
		return nil, false, Errorf(CodeIOError, "reading the archive index: %w", err)
	}
	return idx, true, nil
}
