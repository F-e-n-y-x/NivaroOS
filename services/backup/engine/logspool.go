package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// logSpool holds one job's log lines in a file, so a million "copied"
// lines cost disk, not memory, and every JobLog reader can start from
// the first line whenever it attaches.
//
// The spool (and the run log the job side copies it into) lives on the
// system disk, so per-file lines are capped: of each per-file code the
// first logKeepHead lines are kept, then only the last logKeepTail lines
// of all of them together, written at the end after one lines_omitted
// line with the count left out. Every other line (phases, checks,
// guards) is always kept.
type logSpool struct {
	mu      sync.Mutex
	f       *os.File
	size    int64
	closed  bool          // the job ended: no more lines
	wake    chan struct{} // closed and replaced on every append / close
	failed  bool          // a write failed once (disk full); later lines are dropped
	logf    func(string, ...interface{})
	removed bool

	perCode map[string]int // per-file lines seen, by code
	tail    []LogLine      // ring of the latest lines past the head
	tailAt  int            // next ring slot
	omitted int64          // per-file lines past the head (tail included)
}

// Per-file log line caps (see logSpool).
const (
	logKeepHead = 5000
	logKeepTail = 500
)

// cappedLogCodes are the codes written once per file (rclone's own
// messages come as "raw", often one per failing file).
var cappedLogCodes = map[string]bool{
	"copied": true, "updated": true, "recycled": true, "skipped": true, "file_error": true, "raw": true,
}

func newLogSpool(dir string, id JobID, logf func(string, ...interface{})) (*logSpool, error) {
	f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("job-%d.ndjson", id)), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
	if err != nil {
		return nil, Errorf(CodeIOError, "creating the job log: %w", err)
	}
	return &logSpool{f: f, wake: make(chan struct{}), logf: logf, perCode: map[string]int{}}, nil
}

func (s *logSpool) append(l LogLine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.failed {
		return
	}
	if cappedLogCodes[l.Code] {
		s.perCode[l.Code]++
		if s.perCode[l.Code] > logKeepHead {
			s.omitted++
			if len(s.tail) < logKeepTail {
				s.tail = append(s.tail, l)
			} else {
				s.tail[s.tailAt] = l
			}
			s.tailAt = (s.tailAt + 1) % logKeepTail
			return
		}
	}
	s.writeLocked(l)
}

// writeLocked appends one line to the file (s.mu held).
func (s *logSpool) writeLocked(l LogLine) {
	raw, err := json.Marshal(l)
	if err != nil {
		s.logf("engine: encoding a log line: %v", err)
		return
	}
	raw = append(raw, '\n')
	n, err := s.f.WriteAt(raw, s.size)
	if err != nil {
		s.failed = true
		s.logf("engine: job log %s: %v (later lines are dropped)", s.f.Name(), err)
		return
	}
	s.size += int64(n)
	close(s.wake)
	s.wake = make(chan struct{})
}

// finish marks the end of the log; readers drain and stop. Per-file
// lines past the cap end the log: how many were left out, then the last
// ones in order.
func (s *logSpool) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.omitted > 0 && !s.failed {
		if n := s.omitted - int64(len(s.tail)); n > 0 {
			s.writeLocked(LogLine{T: time.Now(), Lvl: LogInfo, Code: "lines_omitted", MsgKey: "backup.log.lines_omitted",
				Args: map[string]interface{}{"count": n}})
		}
		start := 0
		if len(s.tail) == logKeepTail {
			start = s.tailAt
		}
		for i := 0; i < len(s.tail) && !s.failed; i++ {
			s.writeLocked(s.tail[(start+i)%len(s.tail)])
		}
		s.tail = nil
	}
	s.closed = true
	close(s.wake)
}

// remove deletes the spool file (the finished job is dropped).
func (s *logSpool) remove() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.removed {
		return
	}
	s.removed = true
	if !s.closed {
		s.closed = true
		close(s.wake)
	}
	name := s.f.Name()
	_ = s.f.Close()
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		s.logf("engine: removing job log %s: %v", name, err)
	}
}

// stream sends every line from the start, then follows the log until it
// is finished or ctx ends.
func (s *logSpool) stream(ctx context.Context) <-chan LogLine {
	out := make(chan LogLine, 64)
	go func() {
		defer close(out)
		var off int64
		var partial []byte
		buf := make([]byte, 64*1024)
		for {
			s.mu.Lock()
			size, closed, wake, removed := s.size, s.closed, s.wake, s.removed
			s.mu.Unlock()
			if removed {
				return
			}
			for off < size {
				n := int64(len(buf))
				if size-off < n {
					n = size - off
				}
				got, err := s.f.ReadAt(buf[:n], off)
				if err != nil && err != io.EOF {
					return
				}
				off += int64(got)
				partial = append(partial, buf[:got]...)
				for {
					i := bytes.IndexByte(partial, '\n')
					if i < 0 {
						break
					}
					var l LogLine
					if json.Unmarshal(partial[:i], &l) == nil {
						select {
						case out <- l:
						case <-ctx.Done():
							return
						}
					}
					partial = partial[i+1:]
				}
				if got == 0 {
					break
				}
			}
			if closed {
				return
			}
			select {
			case <-wake:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// lines reads the whole spool (tests, summaries).
func (s *logSpool) lines() []LogLine {
	s.mu.Lock()
	size := s.size
	s.mu.Unlock()
	var out []LogLine
	sc := bufio.NewScanner(io.NewSectionReader(s.f, 0, size))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var l LogLine
		if json.Unmarshal(sc.Bytes(), &l) == nil {
			out = append(out, l)
		}
	}
	return out
}
