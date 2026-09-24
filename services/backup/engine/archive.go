package engine

import (
	"archive/tar"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/klauspost/compress/zstd"
	rfs "github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/filter"
	"github.com/rclone/rclone/fs/operations"
	"github.com/rclone/rclone/fs/walk"
)

// Archive files (spec §2, §8.3, §9): nivaro_<job>_<run start UTC>.tar.zst,
// written as .partial and renamed only when complete, with a small index
// beside it so browsing a version doesn't mean decompressing all of it.
const (
	archivePrefix      = "nivaro_"
	archiveExt         = ".tar.zst"
	archivePartial     = ".partial"
	archiveIndexSuffix = ".index.zst"
)

func archiveName(jobID string, t time.Time) string {
	return archivePrefix + jobID + "_" + t.UTC().Format(VersionTimeLayout) + archiveExt
}

// parseArchiveName returns the timestamp of one of jobID's archives; ok
// is false for anything else (partials, indexes, other jobs, strangers).
// The time comes from the name, never the file's modtime.
func parseArchiveName(jobID, name string) (time.Time, bool) {
	pre := archivePrefix + jobID + "_"
	if !strings.HasPrefix(name, pre) || !strings.HasSuffix(name, archiveExt) {
		return time.Time{}, false
	}
	ts := strings.TrimSuffix(strings.TrimPrefix(name, pre), archiveExt)
	t, err := time.Parse(VersionTimeLayout, ts)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

type archiveInfo struct {
	name string
	t    time.Time
	size int64
}

// listArchives lists jobID's finished archives at a destination, newest
// first.
func (e *Engine) listArchives(ctx context.Context, t *target, jobID string) ([]archiveInfo, error) {
	f, err := t.fsAt(ctx, "", fsOpts{})
	if err != nil {
		return nil, engineErr(err, "opening the destination")
	}
	entries, err := f.List(ctx, "")
	if errors.Is(err, rfs.ErrorDirNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, engineErr(err, "listing the destination")
	}
	var out []archiveInfo
	for _, en := range entries {
		o, ok := en.(rfs.Object)
		if !ok {
			continue
		}
		if ts, ok := parseArchiveName(jobID, path.Base(o.Remote())); ok {
			out = append(out, archiveInfo{name: path.Base(o.Remote()), t: ts, size: o.Size()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].t.After(out[j].t) })
	return out, nil
}

// archiveSource is one source of an archive, with the folder its files
// go under inside the archive ("" for a single-source archive, so a
// restore to the original folder puts files straight back).
type archiveSource struct {
	t      *target
	prefix string
	filter *filter.Filter
}

// archivePrefixes names each source's folder inside a multi-source
// archive after the source folder, made unique.
func archivePrefixes(ts []*target) []string {
	out := make([]string, len(ts))
	if len(ts) == 1 {
		return out
	}
	seen := map[string]int{}
	for i, t := range ts {
		base := path.Base("/" + t.sub)
		if base == "/" || base == "." {
			base = t.ep.Label
		}
		if base == "" {
			base = "source"
		}
		base = strings.NewReplacer("/", "_", "\x00", "").Replace(base)
		key := strings.ToLower(base)
		seen[key]++
		if n := seen[key]; n > 1 {
			base = fmt.Sprintf("%s (%d)", base, n)
		}
		out[i] = base
	}
	return out
}

// indexHeader is the first line of an archive index.
type indexHeader struct {
	V       int       `json:"v"`
	Files   int64     `json:"files"`
	Bytes   int64     `json:"bytes"`
	Created time.Time `json:"created"`
}

// indexEntry is one member of an archive, in its index.
type indexEntry struct {
	P string `json:"p"`           // path inside the archive, no trailing slash
	D bool   `json:"d,omitempty"` // directory
	S int64  `json:"s,omitempty"` // size
	M int64  `json:"m,omitempty"` // modtime, unix seconds
	L string `json:"l,omitempty"` // symlink target
}

// tarWriter streams sources into a tar through zstd, tracking the index.
type tarWriter struct {
	ctx   context.Context
	j     *job
	tw    *tar.Writer
	index []indexEntry
	files int64
	bytes int64
	links map[[2]uint64]string // (dev, inode) -> first name, for hard links
	log   func(lvl, msg string)
}

func (w *tarWriter) addIndex(h *tar.Header) {
	w.index = append(w.index, indexEntry{P: strings.TrimSuffix(h.Name, "/"), D: h.Typeflag == tar.TypeDir,
		S: h.Size, M: h.ModTime.Unix(), L: h.Linkname})
}

// writeLocal archives a local folder tree: owners, modes, times,
// symlinks, hard links and device nodes are kept; sockets can't be.
func (w *tarWriter) writeLocal(src archiveSource) error {
	root := src.t.path
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return Errorf(CodeSourceOffline, "reading %s: %w", root, err)
	}
	rootDev, _ := statDev(rootInfo)
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if walkErr != nil {
			if p == root {
				return Errorf(CodeSourceOffline, "reading %s: %w", root, walkErr)
			}
			w.log(LogWarn, fmt.Sprintf("skipped %s: %v", rel, walkErr))
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == "." {
			return nil
		}
		info, err := os.Lstat(p)
		if err != nil {
			w.log(LogWarn, fmt.Sprintf("skipped %s: %v", rel, err))
			return nil
		}
		if info.IsDir() {
			if dev, ok := statDev(info); ok && dev != rootDev {
				return filepath.SkipDir // another filesystem mounted inside the source
			}
			if !includedDir(w.ctx, src.filter, rel) {
				return filepath.SkipDir
			}
		} else if !includedFile(src.filter, rel, info.Size(), info.ModTime()) {
			return nil
		}
		return w.writeLocalEntry(src.prefix, rel, p, info)
	})
}

func (w *tarWriter) writeLocalEntry(prefix, rel, p string, info os.FileInfo) error {
	if info.Mode()&os.ModeSocket != 0 {
		w.log(LogWarn, fmt.Sprintf("skipped %s: sockets can't be archived", rel))
		return nil
	}
	link := ""
	if info.Mode()&os.ModeSymlink != 0 {
		l, err := os.Readlink(p)
		if err != nil {
			w.log(LogWarn, fmt.Sprintf("skipped %s: %v", rel, err))
			return nil
		}
		link = l
	}
	h, err := tar.FileInfoHeader(info, link)
	if err != nil {
		w.log(LogWarn, fmt.Sprintf("skipped %s: %v", rel, err))
		return nil
	}
	h.Format = tar.FormatPAX
	h.Name = memberName(prefix, rel, info.IsDir())
	if st, ok := info.Sys().(*syscall.Stat_t); ok && info.Mode().IsRegular() && st.Nlink > 1 {
		key := [2]uint64{uint64(st.Dev), st.Ino}
		if first, seen := w.links[key]; seen {
			h.Typeflag, h.Linkname, h.Size = tar.TypeLink, first, 0
		} else {
			w.links[key] = h.Name
		}
	}
	if !info.Mode().IsRegular() || h.Typeflag == tar.TypeLink {
		if err := w.tw.WriteHeader(h); err != nil {
			return Errorf(CodeIOError, "writing the archive: %w", err)
		}
		w.addIndex(h)
		return nil
	}
	f, err := os.Open(p)
	if err != nil {
		w.log(LogWarn, fmt.Sprintf("skipped %s: %v", rel, err))
		return nil
	}
	defer f.Close()
	if err := w.tw.WriteHeader(h); err != nil {
		return Errorf(CodeIOError, "writing the archive: %w", err)
	}
	if err := w.copyExact(f, h.Size, rel); err != nil {
		return err
	}
	w.addIndex(h)
	return nil
}

// copyExact writes exactly size bytes of r into the current member: a
// file that shrank while being read is padded with zeros (and logged),
// one that grew is cut at the size the header announced.
func (w *tarWriter) copyExact(r io.Reader, size int64, rel string) error {
	buf := make([]byte, 256<<10)
	var done int64
	for done < size {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		n := int64(len(buf))
		if size-done < n {
			n = size - done
		}
		got, err := io.ReadFull(r, buf[:n])
		if got > 0 {
			if _, werr := w.tw.Write(buf[:got]); werr != nil {
				return Errorf(classifyError(werr), "writing the archive: %w", werr)
			}
			done += int64(got)
			w.bytes += int64(got)
			w.j.addOwn(int64(got), 0, rel)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			w.log(LogWarn, fmt.Sprintf("%s changed while it was archived; the copy is padded", rel))
			zeros := make([]byte, 64<<10)
			for done < size {
				k := int64(len(zeros))
				if size-done < k {
					k = size - done
				}
				if _, werr := w.tw.Write(zeros[:k]); werr != nil {
					return Errorf(classifyError(werr), "writing the archive: %w", werr)
				}
				done += k
			}
			break
		}
		if err != nil {
			return Errorf(CodeIOError, "reading %s: %w", rel, err)
		}
	}
	w.files++
	w.j.addOwn(0, 1, rel)
	return nil
}

// writeRemote archives a folder of a network endpoint through rclone:
// names, sizes and times are kept; there are no owners or modes to keep.
func (w *tarWriter) writeRemote(ctx context.Context, src archiveSource) error {
	f, err := src.t.fsAt(ctx, "", fsOpts{})
	if err != nil {
		return engineErr(err, "opening the source")
	}
	return walk.Walk(ctx, f, "", false, -1, func(dir string, entries rfs.DirEntries, err error) error {
		if err != nil {
			return engineErr(err, "reading the source")
		}
		for _, en := range entries {
			switch x := en.(type) {
			case rfs.Directory:
				h := &tar.Header{Typeflag: tar.TypeDir, Name: memberName(src.prefix, x.Remote(), true), Mode: 0o755,
					ModTime: x.ModTime(ctx), Format: tar.FormatPAX}
				if err := w.tw.WriteHeader(h); err != nil {
					return Errorf(CodeIOError, "writing the archive: %w", err)
				}
				w.addIndex(h)
			case rfs.Object:
				if x.Size() < 0 {
					w.log(LogWarn, fmt.Sprintf("skipped %s: its size is unknown", x.Remote()))
					continue
				}
				h := &tar.Header{Typeflag: tar.TypeReg, Name: memberName(src.prefix, x.Remote(), false), Mode: 0o644,
					Size: x.Size(), ModTime: x.ModTime(ctx), Format: tar.FormatPAX}
				rc, err := x.Open(ctx)
				if err != nil {
					w.log(LogWarn, fmt.Sprintf("skipped %s: %v", x.Remote(), err))
					continue
				}
				if err := w.tw.WriteHeader(h); err != nil {
					rc.Close()
					return Errorf(CodeIOError, "writing the archive: %w", err)
				}
				err = w.copyExact(rc, h.Size, x.Remote())
				rc.Close()
				if err != nil {
					return err
				}
				w.addIndex(h)
			}
		}
		return nil
	})
}

func memberName(prefix, rel string, dir bool) string {
	n := rel
	if prefix != "" {
		n = prefix + "/" + rel
	}
	if dir {
		n += "/"
	}
	return n
}

// runArchive writes one .tar.zst of every source (or, dry, previews what
// it would contain).
func (e *Engine) runArchive(ctx context.Context, j *job, dry bool) (Result, error) {
	r := j.req
	p, res, err := e.prepareRun(ctx, j, jobTypeArchive)
	if err != nil {
		return res, err
	}
	prefixes := archivePrefixes(p.sources)
	srcs := make([]archiveSource, len(p.sources))
	for i, t := range p.sources {
		fi, err := newFilter(p.sourceSpec(i, false))
		if err != nil {
			return res, err
		}
		srcs[i] = archiveSource{t: t, prefix: prefixes[i], filter: fi}
	}
	j.setTotals(p.scan.Files, p.scan.Bytes)
	res.Counts.SourceFiles = p.scan.Files
	res.Counts.BytesTotal = p.scan.Bytes
	if dry {
		return e.previewArchive(ctx, j, p, srcs, res)
	}

	dest := p.dest
	dctx, _ := rcloneContext(ctx)
	dst, guard, err := e.openDest(dctx, j, dest, "", fsOpts{}, r.DestFolderID)
	if err != nil {
		return res, err
	}
	if p.marker == nil {
		if guard != nil {
			if err := guard.check(); err != nil {
				return res, err
			}
		}
		if dest.local {
			if err := mkdirPrivate(dest.path); err != nil {
				return res, err
			}
		}
		if err := e.writeMarker(ctx, dest, r.JobID, r.DestFolderID); err != nil {
			return res, err
		}
		res.MarkerWritten = true
		j.logLine(LogInfo, "marker_written", "backup.log.marker_written", nil)
	}
	e.removePartials(dctx, j, dst, r.JobID)

	j.phase(stepTransfer, "backup.phase.transfer")
	name := archiveName(r.JobID, j.startedAt())
	// Written as .partial and renamed when complete. A remote that can't
	// rename server-side would download and re-upload the whole archive
	// to rename it; there the upload goes to the final name directly
	// (such object stores only show an object once its upload finished)
	// and a failed upload is removed.
	upload := name + archivePartial
	if !operations.CanServerSideMove(dst) {
		upload = name
	}
	idx, written, err := e.streamArchive(dctx, j, dst, upload, srcs, r.Options.LowPriority)
	if err != nil {
		e.removeQuietly(dctx, dst, upload)
		return res, err
	}
	if upload != name {
		if err := operations.MoveFile(dctx, dst, dst, name, upload); err != nil {
			e.removeQuietly(dctx, dst, upload)
			return res, engineErr(err, "renaming the finished archive")
		}
	}
	if err := writeIndex(dctx, dst, name+archiveIndexSuffix, idx, e.now()); err != nil {
		j.logRaw(LogWarn, "the archive index could not be written (browsing it will be slower): "+err.Error())
	}
	res.ArchiveName = name
	res.Counts.Added = idx.header.Files
	res.Counts.BytesTransferred = written
	if archives, err := e.listArchives(dctx, dest, r.JobID); err == nil {
		res.Counts.DestFiles = int64(len(archives))
	}
	j.logLine(LogInfo, "archive_done", "backup.log.archive_done", map[string]interface{}{"name": name, "bytes": written})

	if r.Options.Verify {
		j.phase(stepVerify, "backup.phase.verify")
		n, err := verifyArchive(dctx, dst, name)
		if err != nil {
			return res, err
		}
		if n != idx.header.Files {
			return res, Errorf(CodeIOError, "the archive holds %d files, %d were written", n, idx.header.Files)
		}
	}
	return res, nil
}

// archiveIndex is the collected index of a written archive.
type archiveIndex struct {
	header  indexHeader
	entries []indexEntry
}

// streamArchive tars and compresses every source straight into the
// destination (no temporary copy on the root disk). A remote that can't
// take an upload of unknown size (no PutStream) would make rclone spool
// the whole archive into the system temp folder; for those the archive
// is built in the staging folder instead - which the free-space check
// sized for it - and uploaded from there.
func (e *Engine) streamArchive(ctx context.Context, j *job, dst rfs.Fs, name string, srcs []archiveSource, low bool) (*archiveIndex, int64, error) {
	w := &tarWriter{ctx: ctx, j: j, links: map[[2]uint64]string{}, log: func(lvl, msg string) { j.logRaw(lvl, msg) }}
	conc := 2
	if low {
		conc = 1
	}
	if dst.Features().PutStream == nil {
		return e.stageArchive(ctx, w, dst, name, srcs, conc)
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := w.writeAll(ctx, pw, srcs, conc)
		pw.CloseWithError(err)
		done <- err
	}()
	o, err := operations.Rcat(ctx, dst, name, pr, e.now(), nil)
	_ = pr.CloseWithError(errors.New("upload ended"))
	werr := <-done
	if werr != nil {
		if ctx.Err() != nil {
			return nil, 0, context.Cause(ctx)
		}
		return nil, 0, engineErr(werr, "")
	}
	if err != nil {
		return nil, 0, engineErr(err, "writing the archive")
	}
	return w.result(e.now()), o.Size(), nil
}

// stageArchive builds the archive in the staging folder, then uploads it
// with its size known.
func (e *Engine) stageArchive(ctx context.Context, w *tarWriter, dst rfs.Fs, name string, srcs []archiveSource, conc int) (*archiveIndex, int64, error) {
	if err := os.MkdirAll(e.cfg.StagingDir, 0o700); err != nil {
		return nil, 0, Errorf(classifyError(err), "creating the staging folder: %w", err)
	}
	tmp, err := os.CreateTemp(e.cfg.StagingDir, ".archive-*"+archiveExt+archivePartial)
	if err != nil {
		return nil, 0, Errorf(classifyError(err), "creating the staging file: %w", err)
	}
	defer func() {
		tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if err := w.writeAll(ctx, tmp, srcs, conc); err != nil {
		if ctx.Err() != nil {
			return nil, 0, context.Cause(ctx)
		}
		return nil, 0, engineErr(err, "")
	}
	if err := tmp.Close(); err != nil {
		return nil, 0, Errorf(classifyError(err), "writing the staging file: %w", err)
	}
	lf, err := newBackendFs(ctx, "local", ":local", e.cfg.StagingDir, configmap.Simple{})
	if err != nil {
		return nil, 0, engineErr(err, "opening the staging folder")
	}
	src, err := lf.NewObject(ctx, filepath.Base(tmp.Name()))
	if err != nil {
		return nil, 0, engineErr(err, "opening the staged archive")
	}
	o, err := operations.Copy(ctx, dst, nil, name, src)
	if err != nil {
		return nil, 0, engineErr(err, "uploading the archive")
	}
	return w.result(e.now()), o.Size(), nil
}

// writeAll writes the whole archive of srcs to out: tar through zstd.
func (w *tarWriter) writeAll(ctx context.Context, out io.Writer, srcs []archiveSource, conc int) error {
	bw := bufio.NewWriterSize(out, 1<<20)
	enc, err := zstd.NewWriter(bw, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(conc))
	if err != nil {
		return Errorf(CodeInternal, "zstd: %w", err)
	}
	w.tw = tar.NewWriter(enc)
	for _, s := range srcs {
		if s.t.local {
			err = w.writeLocal(s)
		} else {
			err = w.writeRemote(filter.ReplaceConfig(ctx, s.filter), s)
		}
		if err != nil {
			enc.Close()
			return err
		}
	}
	if err := w.tw.Close(); err != nil {
		enc.Close()
		return Errorf(CodeIOError, "finishing the archive: %w", err)
	}
	if err := enc.Close(); err != nil {
		return Errorf(CodeIOError, "finishing the archive: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return Errorf(classifyError(err), "finishing the archive: %w", err)
	}
	return nil
}

// result is the index of everything written.
func (w *tarWriter) result(now time.Time) *archiveIndex {
	return &archiveIndex{header: indexHeader{V: 1, Files: w.files, Bytes: w.bytes, Created: now.UTC()}, entries: w.index}
}

// writeIndex stores an archive's index beside it: a zstd-compressed
// NDJSON file whose first line is the header.
func writeIndex(ctx context.Context, dst rfs.Fs, name string, idx *archiveIndex, now time.Time) error {
	pr, pw := io.Pipe()
	go func() {
		err := func() error {
			enc, err := zstd.NewWriter(pw, zstd.WithEncoderConcurrency(1))
			if err != nil {
				return err
			}
			je := json.NewEncoder(enc)
			if err := je.Encode(idx.header); err != nil {
				enc.Close()
				return err
			}
			for _, en := range idx.entries {
				if err := je.Encode(en); err != nil {
					enc.Close()
					return err
				}
			}
			return enc.Close()
		}()
		pw.CloseWithError(err)
	}()
	_, err := operations.Rcat(ctx, dst, name, pr, now, nil)
	_ = pr.Close()
	return err
}

// readIndex loads an archive's index; ok=false when there is none.
func readIndex(ctx context.Context, f rfs.Fs, archive string) (*archiveIndex, bool, error) {
	o, err := f.NewObject(ctx, archive+archiveIndexSuffix)
	if err != nil {
		if errIsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, engineErr(err, "reading the archive index")
	}
	rc, err := o.Open(ctx)
	if err != nil {
		return nil, false, engineErr(err, "reading the archive index")
	}
	defer rc.Close()
	dec, err := zstd.NewReader(rc, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true))
	if err != nil {
		return nil, false, Errorf(CodeIOError, "reading the archive index: %w", err)
	}
	defer dec.Close()
	jd := json.NewDecoder(dec)
	idx := &archiveIndex{}
	if err := jd.Decode(&idx.header); err != nil {
		return nil, false, Errorf(CodeIOError, "reading the archive index: %w", err)
	}
	for {
		var en indexEntry
		if err := jd.Decode(&en); err == io.EOF {
			break
		} else if err != nil {
			return nil, false, Errorf(CodeIOError, "reading the archive index: %w", err)
		}
		idx.entries = append(idx.entries, en)
	}
	return idx, true, nil
}

// openArchive opens an archive at f for reading its members in order.
func openArchive(ctx context.Context, f rfs.Fs, name string) (*tar.Reader, func(), error) {
	o, err := f.NewObject(ctx, name)
	if err != nil {
		if errIsNotFound(err) {
			return nil, nil, Errorf(CodeNotFound, "archive %s not found", name)
		}
		return nil, nil, engineErr(err, "opening the archive")
	}
	rc, err := o.Open(ctx)
	if err != nil {
		return nil, nil, engineErr(err, "opening the archive")
	}
	dec, err := zstd.NewReader(bufio.NewReaderSize(rc, 1<<20), zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true))
	if err != nil {
		rc.Close()
		return nil, nil, Errorf(CodeIOError, "opening the archive: %w", err)
	}
	closer := func() {
		dec.Close()
		rc.Close()
	}
	return tar.NewReader(dec), closer, nil
}

// scanArchiveIndex builds an index by reading the whole archive (when
// its index file is missing).
func scanArchiveIndex(ctx context.Context, f rfs.Fs, name string) (*archiveIndex, error) {
	tr, closer, err := openArchive(ctx, f, name)
	if err != nil {
		return nil, err
	}
	defer closer()
	idx := &archiveIndex{header: indexHeader{V: 1}}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Errorf(CodeIOError, "reading the archive: %w", err)
		}
		idx.entries = append(idx.entries, indexEntry{P: strings.TrimSuffix(h.Name, "/"), D: h.Typeflag == tar.TypeDir, S: h.Size, M: h.ModTime.Unix(), L: h.Linkname})
		if h.Typeflag == tar.TypeReg {
			idx.header.Files++
			idx.header.Bytes += h.Size
		}
	}
	return idx, nil
}

// verifyArchive decodes a whole archive (every checksum zstd carries is
// checked) and returns how many files it holds.
func verifyArchive(ctx context.Context, f rfs.Fs, name string) (int64, error) {
	tr, closer, err := openArchive(ctx, f, name)
	if err != nil {
		return 0, err
	}
	defer closer()
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		h, err := tr.Next()
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return 0, Errorf(CodeIOError, "the archive is damaged: %w", err)
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return 0, Errorf(CodeIOError, "the archive is damaged at %s: %w", h.Name, err)
		}
		if h.Typeflag == tar.TypeReg {
			n++
		}
	}
}

// removePartials deletes half-written archives a crash left behind
// (spec §8.3: the archive is rebuilt, never resumed).
func (e *Engine) removePartials(ctx context.Context, j *job, dst rfs.Fs, jobID string) {
	entries, err := dst.List(ctx, "")
	if err != nil {
		return
	}
	for _, en := range entries {
		o, ok := en.(rfs.Object)
		if !ok {
			continue
		}
		base := path.Base(o.Remote())
		if _, ok := parseArchiveName(jobID, strings.TrimSuffix(base, archivePartial)); ok && strings.HasSuffix(base, archivePartial) {
			if err := o.Remove(ctx); err != nil {
				j.logRaw(LogWarn, "could not remove the unfinished archive "+base+": "+err.Error())
			}
		}
	}
}

func (e *Engine) removeQuietly(ctx context.Context, dst rfs.Fs, name string) {
	// The run is failing anyway; use a fresh context so a cancelled run
	// still cleans up its partial file.
	cctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if o, err := dst.NewObject(cctx, name); err == nil {
		if err := o.Remove(cctx); err != nil {
			e.cfg.Logf("engine: removing %s: %v", name, err)
		}
	}
}

// previewArchive writes the plan of an archive: every file it would add.
func (e *Engine) previewArchive(ctx context.Context, j *job, p *prepared, srcs []archiveSource, res Result) (Result, error) {
	var out *bufio.Writer
	var pf *os.File
	if j.req.PreviewFile != "" {
		var err error
		pf, err = createPreviewFile(j.req.PreviewFile)
		if err != nil {
			return res, err
		}
		defer pf.Close()
		out = bufio.NewWriterSize(pf, 256<<10)
	}
	var files, bytes int64
	for i, s := range srcs {
		sctx := filter.ReplaceConfig(ctx, s.filter)
		sctx, _ = rcloneContext(sctx)
		f, err := s.t.fsAt(sctx, "", fsOpts{oneFileSystem: true})
		if err != nil {
			return res, engineErr(err, "opening the source")
		}
		prefix := srcs[i].prefix
		_, err = scanSource(sctx, f, scanOpts{onFile: func(remote string, size int64) error {
			files++
			bytes += max(size, 0)
			if out == nil {
				return nil
			}
			raw, err := json.Marshal(PreviewItem{Op: "add", Path: memberName(prefix, remote, false), Size: size})
			if err != nil {
				return err
			}
			_, err = out.Write(append(raw, '\n'))
			return err
		}})
		if err != nil {
			return res, engineErr(err, "planning")
		}
	}
	if out != nil {
		if err := out.Flush(); err != nil {
			return res, Errorf(CodeIOError, "writing the preview: %w", err)
		}
	}
	res.Counts.Added, res.Counts.BytesAdd = files, bytes
	return res, nil
}
