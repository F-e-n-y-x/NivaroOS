package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/service/transfer"
)

// FileUploadService assembles chunked uploads (simple-uploader.js, see
// ui/src/apps/files/UploadTray.vue).
//
// The previous implementation could panic the whole core service (a
// double mutex unlock on a close/rename error), reported success when the
// final rename failed, leaked one open file per chunk, keyed uploads only
// by the browser's size+name identifier (two same-named uploads into
// different folders corrupted each other), never sent uploads into a
// companion device folder to the device, and didn't sanitize relativePath.
//
// Now each upload is keyed by destination + relative path + identifier +
// size; chunks are written into one shared temp file at their offset, each
// chunk's length is checked, and when the last chunk lands the file is
// fsynced, size-verified and only then renamed into place (or handed to
// the companion device). Nothing ever appears under the real name early.
type FileUploadService struct {
	mu          sync.Mutex
	uploads     map[string]*upload
	janitorOnce sync.Once
	stop        chan struct{}
	stopOnce    sync.Once
}

type upload struct {
	mu       sync.Mutex
	key      string
	dest     string // final path
	tmp      string
	parts    string // received chunk numbers, one per line (survives a restart)
	f        *os.File
	received []bool
	count    int64
	size     int64
	done     bool
	err      error
	// failed: the upload hit a disk error (full, read-only, I/O) and its
	// temp file is gone. Every further chunk of this attempt gets the error
	// - starting a fresh upload over the lost part would let the browser
	// count the file as done when it never arrived.
	failed error
	last   time.Time
	// staged uploads go somewhere else when complete (companion devices)
	deliver func(tmp string) error
	cleanup string // directory to remove after delivery
}

// UploadChunk is one chunk request.
type UploadChunk struct {
	Path             string // destination folder
	RelativePath     string // file path relative to Path ("a.jpg" or "folder/a.jpg")
	Identifier       string
	ChunkNumber      int64 // 1-based
	ChunkSize        int64
	CurrentChunkSize int64
	TotalChunks      int64
	TotalSize        int64
	Data             io.Reader
}

// UploadResult tells the caller whether this chunk completed the file.
type UploadResult struct {
	Complete bool
	Path     string
}

// UploadStagingDir holds uploads that can't be staged next to their
// destination (companion devices). On disk, never tmpfs - /tmp is RAM on
// many installs and a multi-GB upload would exhaust it.
var UploadStagingDir = "/var/lib/nivaroos/uploads"

const uploadIdleTimeout = 6 * time.Hour

func NewFileUploadService() *FileUploadService {
	return &FileUploadService{uploads: map[string]*upload{}, stop: make(chan struct{})}
}

// Close stops the background cleanup (tests; the shared instance lives for
// the whole process).
func (s *FileUploadService) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// Uploads is the one upload service shared by /v2/casaos/file/upload (web
// UI) and /v1/file/upload (mobile app), so both get the same guarantees.
var Uploads = NewFileUploadService()

var (
	errBadUploadPath = errors.New("invalid file path")
	errBadChunk      = errors.New("invalid chunk")
	errUploadDisk    = errors.New("saving upload")
)

// resolveUploadDest validates relativePath and returns the final absolute
// path, guaranteed to be inside dir.
func resolveUploadDest(dir, rel string) (string, error) {
	if dir == "" || rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\x00") {
		return "", errBadUploadPath
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errBadUploadPath
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", errBadUploadPath
		}
	}
	base := filepath.Clean(dir)
	full := filepath.Join(base, clean)
	if !strings.HasPrefix(full, base+"/") {
		return "", errBadUploadPath
	}
	return full, nil
}

func uploadKey(c UploadChunk) string {
	h := sha1.Sum([]byte(filepath.Clean(c.Path) + "\x00" + c.RelativePath + "\x00" + c.Identifier + "\x00" + fmt.Sprint(c.TotalSize)))
	return hex.EncodeToString(h[:])
}

// Upload writes one chunk; the chunk that completes the file also
// finalizes it and returns Complete: true.
func (s *FileUploadService) Upload(c UploadChunk) (UploadResult, error) {
	dest, err := resolveUploadDest(c.Path, c.RelativePath)
	if err != nil {
		return UploadResult{}, err
	}
	if c.TotalChunks < 1 {
		c.TotalChunks = 1
	}
	if c.ChunkNumber < 1 || c.ChunkNumber > c.TotalChunks || c.TotalSize < 0 || c.CurrentChunkSize < 0 {
		return UploadResult{}, errBadChunk
	}
	if c.TotalChunks > 1 && c.ChunkSize <= 0 {
		return UploadResult{}, errBadChunk
	}

	// Abandoned-upload cleanup starts with the first upload, not at
	// package load.
	s.janitorOnce.Do(func() { go s.janitor() })
	u, err := s.get(c, dest)
	if err != nil {
		return UploadResult{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.last = time.Now()
	if int64(len(u.received)) != c.TotalChunks {
		return UploadResult{}, errBadChunk
	}
	if u.failed != nil {
		return UploadResult{}, u.failed
	}
	if u.done {
		// A retried chunk after completion (lost response): report success.
		return UploadResult{Complete: true, Path: dest}, u.err
	}

	offset := (c.ChunkNumber - 1) * c.ChunkSize
	if c.TotalChunks == 1 {
		offset = 0
	}
	n, err := copyAt(u.f, c.Data, offset)
	if errors.Is(err, errUploadDisk) {
		return UploadResult{}, s.fail(u, err)
	}
	if err != nil {
		return UploadResult{}, fmt.Errorf("receiving chunk %d: %w", c.ChunkNumber, err)
	}
	if n != c.CurrentChunkSize {
		return UploadResult{}, fmt.Errorf("chunk %d arrived incomplete (%d of %d bytes) - it will be retried", c.ChunkNumber, n, c.CurrentChunkSize)
	}
	if !u.received[c.ChunkNumber-1] {
		// Recorded on disk before it's acknowledged: the browser never
		// sends an acknowledged chunk again, so a restart must not forget it.
		// ponytail: no fsync per chunk (page cache survives a service
		// restart, not a power cut); finalize fsyncs the whole file.
		if err := appendLine(u.parts, c.ChunkNumber); err != nil {
			return UploadResult{}, s.fail(u, fmt.Errorf("%w: %w", errUploadDisk, err))
		}
		u.received[c.ChunkNumber-1] = true
		u.count++
	}
	if u.count < int64(len(u.received)) {
		return UploadResult{}, nil
	}

	// Last chunk: make it durable, verify, then publish.
	if err := s.finalize(u); err != nil {
		return UploadResult{}, s.fail(u, err)
	}
	u.done = true
	_ = os.Remove(u.parts)
	s.mu.Lock()
	delete(s.uploads, u.key)
	s.mu.Unlock()
	return UploadResult{Complete: true, Path: dest}, nil
}

// fail ends an upload attempt for good: frees its disk space and keeps
// the error for the chunks still on their way (see upload.failed). The
// browser's next attempt starts with a chunk check, which clears it.
func (s *FileUploadService) fail(u *upload, err error) error {
	u.discard()
	u.failed = fmt.Errorf("upload failed: %s", transfer.HumanError(err))
	return u.failed
}

// discard closes and deletes an unfinished upload's temp file and chunk
// record. Caller holds u.mu.
func (u *upload) discard() {
	u.f.Close()
	_ = os.Remove(u.tmp)
	_ = os.Remove(u.parts)
	if u.cleanup != "" {
		_ = os.RemoveAll(u.cleanup)
	}
}

// Cancel drops an unfinished upload the user cancelled, so its temp file
// doesn't sit there until the idle cleanup. Only the fields that key an
// upload (Path, RelativePath, Identifier, TotalSize) are used. A finished
// upload is not touched.
// ponytail: a chunk already in flight when the cancel lands can start the
// upload again; the idle cleanup removes that one.
func (s *FileUploadService) Cancel(c UploadChunk) error {
	dest, err := resolveUploadDest(c.Path, c.RelativePath)
	if err != nil {
		return err
	}
	key := uploadKey(c)
	s.mu.Lock()
	u := s.uploads[key]
	delete(s.uploads, key)
	s.mu.Unlock()
	if u != nil {
		u.mu.Lock()
		defer u.mu.Unlock()
		if u.failed == nil && !u.done {
			u.discard()
		}
		// chunks still waiting on u.mu must not write to the closed file
		u.failed = errors.New("upload cancelled")
		return nil
	}
	// Not in memory (service restarted since): remove what's on disk.
	_ = os.RemoveAll(filepath.Join(UploadStagingDir, key))
	dir, name := filepath.Dir(dest), filepath.Base(dest)
	_ = os.Remove(filepath.Join(dir, transfer.HiddenTempName(name, ".nvupload-"+key[:12])))
	_ = os.Remove(filepath.Join(dir, transfer.HiddenTempName(name, ".nvupload-"+key[:12]+"p")))
	return nil
}

func appendLine(p string, n int64) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, n); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// loadParts reads which chunks an earlier run of the service received.
func loadParts(p string, received []bool) int64 {
	raw, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	var count int64
	for _, line := range strings.Fields(string(raw)) {
		var n int64
		if _, err := fmt.Sscan(line, &n); err == nil && n >= 1 && n <= int64(len(received)) && !received[n-1] {
			received[n-1] = true
			count++
		}
	}
	return count
}

// HasChunk backs the uploader's test-chunk request (resume support).
func (s *FileUploadService) HasChunk(c UploadChunk) bool {
	s.mu.Lock()
	u, ok := s.uploads[uploadKey(c)]
	s.mu.Unlock()
	if !ok {
		// Not in memory: after a restart, the chunk record on disk knows.
		dest, err := resolveUploadDest(c.Path, c.RelativePath)
		if err != nil || c.TotalSize <= 0 || c.TotalChunks < 1 {
			return false
		}
		if u, err = s.get(c, dest); err != nil {
			return false
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.failed != nil {
		// A new attempt is starting (the browser checks chunks first).
		s.mu.Lock()
		if s.uploads[u.key] == u {
			delete(s.uploads, u.key)
		}
		s.mu.Unlock()
		return false
	}
	i := c.ChunkNumber - 1
	return i >= 0 && i < int64(len(u.received)) && u.received[i]
}

func (s *FileUploadService) get(c UploadChunk, dest string) (*upload, error) {
	key := uploadKey(c)
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.uploads[key]; ok {
		return u, nil
	}
	u := &upload{key: key, dest: dest, size: c.TotalSize, received: make([]bool, c.TotalChunks), last: time.Now()}

	if CompanionHandler != nil && CompanionHandler.IsCompanionPath(dest) {
		// Stage on disk here, then send the finished file to the device.
		dir := filepath.Join(UploadStagingDir, key)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		u.tmp = filepath.Join(dir, filepath.Base(dest))
		u.parts = filepath.Join(dir, ".parts")
		u.cleanup = dir
		destDir := filepath.Dir(dest)
		u.deliver = func(tmp string) error {
			ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
			defer cancel()
			return CompanionHandler.CopyToCompanion(ctx, tmp, destDir, "overwrite", nil)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if fi, err := os.Stat(dest); err == nil && fi.IsDir() {
			return nil, fmt.Errorf("a folder named %q already exists here", filepath.Base(dest))
		}
		// Next to the destination: same filesystem, so publishing is an
		// atomic rename. Hidden, and filtered from listings.
		u.tmp = filepath.Join(filepath.Dir(dest), transfer.HiddenTempName(filepath.Base(dest), ".nvupload-"+key[:12]))
		u.parts = filepath.Join(filepath.Dir(dest), transfer.HiddenTempName(filepath.Base(dest), ".nvupload-"+key[:12]+"p"))
	}
	if st, err := os.Stat(u.tmp); err == nil && st.Size() == c.TotalSize {
		u.count = loadParts(u.parts, u.received)
	} else {
		_ = os.Remove(u.parts)
	}
	f, err := os.OpenFile(u.tmp, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(c.TotalSize); err != nil {
		f.Close()
		_ = os.Remove(u.tmp)
		return nil, err
	}
	u.f = f
	s.uploads[key] = u
	return u, nil
}

// finalize publishes a fully received upload; on error the caller's
// fail() cleans up.
func (s *FileUploadService) finalize(u *upload) error {
	if err := u.f.Sync(); err != nil {
		return fmt.Errorf("saving upload: %w", err)
	}
	if err := u.f.Close(); err != nil {
		return fmt.Errorf("saving upload: %w", err)
	}
	st, err := os.Stat(u.tmp)
	if err != nil {
		return err
	}
	if st.Size() != u.size {
		return fmt.Errorf("upload verification failed: %d of %d bytes", st.Size(), u.size)
	}
	if u.deliver != nil {
		err := u.deliver(u.tmp)
		_ = os.RemoveAll(u.cleanup)
		if err != nil {
			return fmt.Errorf("sending to the device: %w", err)
		}
		return nil
	}
	if err := os.Rename(u.tmp, u.dest); err != nil {
		return err
	}
	return nil
}

func copyAt(f *os.File, r io.Reader, offset int64) (int64, error) {
	buf := make([]byte, 256*1024)
	var n int64
	for {
		m, err := r.Read(buf)
		if m > 0 {
			if _, werr := f.WriteAt(buf[:m], offset+n); werr != nil {
				return n, fmt.Errorf("%w: %w", errUploadDisk, werr)
			}
			n += int64(m)
		}
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
	}
}

// janitor drops uploads abandoned mid-way (closed tab, lost connection)
// so their temp files don't pile up.
func (s *FileUploadService) janitor() {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
		cutoff := time.Now().Add(-uploadIdleTimeout)
		s.mu.Lock()
		for k, u := range s.uploads {
			u.mu.Lock()
			if u.last.Before(cutoff) {
				u.discard()
				delete(s.uploads, k)
			}
			u.mu.Unlock()
		}
		s.mu.Unlock()
	}
}
