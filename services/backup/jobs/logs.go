package jobs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Run logs (spec §10.5): <data>/logs/<job>/<run>.jsonl, one engine.LogLine
// per line, gzipped when the run ends. Offsets are always into the
// uncompressed stream, so a reader following a live log keeps its place
// when the file is compressed underneath it.

const (
	logPageDefault = 500
	logPageMax     = 2000
	// maxLogLine caps one stored line; rclone can print very long paths.
	maxLogLine = 16 << 10
	// fileErrorsSuffix names the sidecar that keeps a partial run's
	// failed files (Run.file_errors) next to its log.
	fileErrorsSuffix = ".errors.json"
	// maxRunLogBytes caps what one run's log takes of the system disk.
	// The engine already caps per-file lines; this is the backstop for
	// whatever else it streams. The job side's own lines (phases, hooks,
	// the final status) are always written.
	maxRunLogBytes = 64 << 20
)

// logPath is where a run's log lives while it is being written.
func logPath(dataDir, jobID, runID string) string {
	return filepath.Join(dataDir, LogsDir, safeName(jobID), safeName(runID)+".jsonl")
}

// safeName keeps ids usable as file names (they are generated, but a
// hand-edited database must not become a path traversal).
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, s)
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}

// RunLog appends lines to one run's log. Safe for concurrent use.
type RunLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	w    *bufio.Writer
	size int64
	// full: the engine's lines reached maxRunLogBytes and are dropped.
	full bool
}

// OpenRunLog opens (appending) a run's log, creating its folder.
func OpenRunLog(path string) (*RunLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	var size int64
	if fi, err := f.Stat(); err == nil {
		size = fi.Size()
	}
	return &RunLog{path: path, f: f, w: bufio.NewWriter(f), size: size}, nil
}

// Path is the log file being written.
func (l *RunLog) Path() string { return l.path }

// Write appends one engine line and flushes it, so GET /runs/:id/log
// sees it at once. Past maxRunLogBytes engine lines are dropped, after
// one line saying so.
func (l *RunLog) Write(line engine.LogLine) {
	l.write(line, false)
}

func (l *RunLog) write(line engine.LogLine, always bool) {
	if line.T.IsZero() {
		line.T = time.Now()
	}
	if len(line.Raw) > maxLogLine {
		line.Raw = line.Raw[:maxLogLine] + "…"
	}
	raw, err := json.Marshal(line)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		return
	}
	if !always && l.size+int64(len(raw))+1 > maxRunLogBytes {
		if l.full {
			return
		}
		l.full = true
		note, _ := json.Marshal(engine.LogLine{T: time.Now(), Lvl: engine.LogWarn, Code: "raw",
			Raw: fmt.Sprintf("the log reached %d MiB; later engine lines are left out", maxRunLogBytes>>20)})
		raw = note
	}
	n, _ := l.w.Write(raw)
	if l.w.WriteByte('\n') == nil {
		n++
	}
	l.size += int64(n)
	l.w.Flush()
}

// Info / Warn / Error write a translatable job-side line (jobs.LogKeys).
func (l *RunLog) Info(key string, args map[string]interface{}) {
	l.write(engine.LogLine{Lvl: engine.LogInfo, Code: logCode(key), MsgKey: key, Args: args}, true)
}

func (l *RunLog) Warn(key string, args map[string]interface{}) {
	l.write(engine.LogLine{Lvl: engine.LogWarn, Code: logCode(key), MsgKey: key, Args: args}, true)
}

func (l *RunLog) Error(key string, args map[string]interface{}) {
	l.write(engine.LogLine{Lvl: engine.LogError, Code: logCode(key), MsgKey: key, Args: args}, true)
}

// Raw writes a technical line with no translation (hook and engine
// errors for "Show technical output").
func (l *RunLog) Raw(lvl, text string) {
	l.write(engine.LogLine{Lvl: lvl, Code: "raw", Raw: text}, true)
}

// logCode is a LogKeys key's last segment.
func logCode(key string) string {
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		return key[i+1:]
	}
	return key
}

// Close flushes and closes the log.
func (l *RunLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	l.w.Flush()
	err := l.f.Close()
	l.f, l.w = nil, nil
	return err
}

// compressLog gzips a finished log next to itself and removes the plain
// file; it returns the new path. A failure leaves the plain log in place.
func compressLog(path string) (string, error) {
	if strings.HasSuffix(path, ".gz") {
		return path, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return path, err
	}
	defer in.Close()
	gzPath := path + ".gz"
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := io.Copy(zw, in); err != nil {
		return path, err
	}
	if err := zw.Close(); err != nil {
		return path, err
	}
	if err := writeFileAtomic(gzPath, buf.Bytes(), 0o600); err != nil {
		return path, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return gzPath, err
	}
	return gzPath, nil
}

// openLogReader opens a log, plain or gzipped. When the plain file is
// gone because it was just compressed, it falls back to the .gz.
func openLogReader(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) && !strings.HasSuffix(path, ".gz") {
		path += ".gz"
		f, err = os.Open(path)
	}
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(path, ".gz") {
		return f, nil
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{zr, f}, nil
}

// ReadLogPage reads up to limit complete lines after byte offset after.
// eof reports that the end of the file was reached; a trailing line
// still being written is left for the next page.
func ReadLogPage(path string, after int64, limit int) (lines []engine.LogLine, next int64, eof bool, err error) {
	if limit <= 0 {
		limit = logPageDefault
	}
	if limit > logPageMax {
		limit = logPageMax
	}
	if after < 0 {
		after = 0
	}
	lines = []engine.LogLine{}
	rc, err := openLogReader(path)
	if err != nil {
		return lines, after, false, err
	}
	defer rc.Close()
	if s, ok := rc.(io.Seeker); ok {
		if _, err := s.Seek(after, io.SeekStart); err != nil {
			return lines, after, false, err
		}
	} else if _, err := io.CopyN(io.Discard, rc, after); err != nil {
		if errors.Is(err, io.EOF) {
			return lines, after, true, nil
		}
		return lines, after, false, err
	}
	br := bufio.NewReaderSize(rc, 64<<10)
	next = after
	for len(lines) < limit {
		raw, rerr := br.ReadBytes('\n')
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return lines, next, true, nil
			}
			return lines, next, false, rerr
		}
		next += int64(len(raw))
		var ll engine.LogLine
		if json.Unmarshal(bytes.TrimSpace(raw), &ll) != nil {
			// A line that isn't ours (hand edit, torn write) is shown raw
			// instead of breaking the page.
			ll = engine.LogLine{Lvl: engine.LogWarn, Code: "raw", Raw: strings.TrimSpace(string(raw))}
		}
		lines = append(lines, ll)
	}
	// Peek so a page that ends exactly at the end of the file says so.
	if _, perr := br.Peek(1); errors.Is(perr, io.EOF) {
		eof = true
	}
	return lines, next, eof, nil
}

// saveFileErrors stores a partial run's file errors next to its log.
func saveFileErrors(logFile string, errs []engine.FileError) error {
	if len(errs) == 0 {
		return nil
	}
	raw, err := json.Marshal(errs)
	if err != nil {
		return err
	}
	return writeFileAtomic(fileErrorsPath(logFile), raw, 0o600)
}

func fileErrorsPath(logFile string) string {
	base := strings.TrimSuffix(strings.TrimSuffix(logFile, ".gz"), ".jsonl")
	return base + fileErrorsSuffix
}

// loadFileErrors reads them back ([] when there are none).
func loadFileErrors(logFile string) []engine.FileError {
	out := []engine.FileError{}
	if logFile == "" {
		return out
	}
	raw, err := os.ReadFile(fileErrorsPath(logFile))
	if err != nil {
		return out
	}
	if json.Unmarshal(raw, &out) != nil {
		return []engine.FileError{}
	}
	return out
}

// removeRunFiles deletes a run's log, file-error sidecar and preview.
func removeRunFiles(r RunRow) {
	for _, p := range []string{r.LogPath, r.PreviewPath} {
		if p == "" {
			continue
		}
		_ = os.Remove(p)
		if !strings.HasSuffix(p, ".gz") && strings.HasSuffix(p, ".jsonl") {
			_ = os.Remove(p + ".gz")
		}
	}
	if r.LogPath != "" {
		_ = os.Remove(fileErrorsPath(r.LogPath))
	}
}

// fileSize is the size of a log on disk (0 when missing).
func fileSize(p string) int64 {
	if p == "" {
		return 0
	}
	st, err := os.Stat(p)
	if err != nil {
		if !strings.HasSuffix(p, ".gz") {
			if st, err = os.Stat(p + ".gz"); err == nil {
				return st.Size()
			}
		}
		return 0
	}
	return st.Size()
}

func logExists(p string) bool {
	if p == "" {
		return false
	}
	if _, err := os.Stat(p); err == nil {
		return true
	}
	if !strings.HasSuffix(p, ".gz") {
		if _, err := os.Stat(p + ".gz"); err == nil {
			return true
		}
	}
	return false
}

// describeErr is a one-line technical error for logs.
func describeErr(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
