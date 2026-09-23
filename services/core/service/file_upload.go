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
	f        *os.File
	received []bool
	count    int64
	size     int64
	done     bool
	err      error
	last     time.Time
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
	if u.done {
		// A retried chunk after completion (lost response): report success.
		return UploadResult{Complete: true, Path: dest}, u.err
	}

	offset := (c.ChunkNumber - 1) * c.ChunkSize
	if c.TotalChunks == 1 {
		offset = 0
	}
	n, err := copyAt(u.f, c.Data, offset)
	if err != nil {
		return UploadResult{}, fmt.Errorf("writing upload: %w", err)
	}
	if n != c.CurrentChunkSize {
		return UploadResult{}, fmt.Errorf("chunk %d arrived incomplete (%d of %d bytes) - it will be retried", c.ChunkNumber, n, c.CurrentChunkSize)
	}
	if !u.received[c.ChunkNumber-1] {
		u.received[c.ChunkNumber-1] = true
		u.count++
	}
	if u.count < int64(len(u.received)) {
		return UploadResult{}, nil
	}

	// Last chunk: make it durable, verify, then publish.
	u.done = true
	u.err = s.finalize(u)
	s.mu.Lock()
	delete(s.uploads, u.key)
	s.mu.Unlock()
	return UploadResult{Complete: true, Path: dest}, u.err
}

// HasChunk backs the uploader's test-chunk request (resume support).
func (s *FileUploadService) HasChunk(c UploadChunk) bool {
	s.mu.Lock()
	u, ok := s.uploads[uploadKey(c)]
	s.mu.Unlock()
	if !ok {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
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
		u.tmp = filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".nvupload-"+key[:12])
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

func (s *FileUploadService) finalize(u *upload) error {
	fail := func(err error) error {
		u.f.Close()
		_ = os.Remove(u.tmp)
		if u.cleanup != "" {
			_ = os.RemoveAll(u.cleanup)
		}
		return err
	}
	if err := u.f.Sync(); err != nil {
		return fail(fmt.Errorf("saving upload: %w", err))
	}
	if err := u.f.Close(); err != nil {
		return fail(fmt.Errorf("saving upload: %w", err))
	}
	st, err := os.Stat(u.tmp)
	if err != nil {
		return fail(err)
	}
	if st.Size() != u.size {
		return fail(fmt.Errorf("upload verification failed: %d of %d bytes", st.Size(), u.size))
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
		return fail(err)
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
				return n, werr
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
				u.f.Close()
				_ = os.Remove(u.tmp)
				if u.cleanup != "" {
					_ = os.RemoveAll(u.cleanup)
				}
				delete(s.uploads, k)
			}
			u.mu.Unlock()
		}
		s.mu.Unlock()
	}
}
