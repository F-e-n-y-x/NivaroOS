package cloudcache

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Uploads that can never finish.
//
// rclone's VFS retries a failed upload forever (the wait doubles up to 5
// minutes). Some failures are permanent - TeraBox refusing a file over the
// free plan's 4 GB, a 413, a quota error - and retrying them just keeps the
// file pending (and its disk space held) for good. Such an upload is
// "stuck": it is parked (no more retries) and listed for the user, who can
// save the file to a folder on this server, retry it or discard it.

// MaxTries is how many failed attempts make an upload stuck even when the
// error doesn't look permanent.
const MaxTries = 10

// StuckFile is where stuck uploads are remembered across restarts (rclone
// forgets its retry count; without this each restart would retry them).
const StuckFile = "/var/lib/nivaroos/local-storage/cloud-stuck-uploads.json"

var (
	permanentPhrases = []string{
		"more than allowed free plan limit", // TeraBox, files over 4 GB
		"free plan limit",
		"file too large", "file is too large", "too large to upload", "exceeds the maximum",
		"maximum file size", "max file size", "file size limit", "size of upload file",
		"entity too large", "payload too large",
		"storagequotaexceeded", "quota exceeded", "quotaexceeded", "insufficient storage", "insufficient_space",
		"not enough space", "no space left on the remote", "storage is full", "storage full",
		"invalid file name", "invalid filename", "name contains invalid", "path too long", "filename too long",
		"forbidden file type", "file type is not allowed",
	}
	// Transient: never permanent, whatever else the message says.
	transientPhrases = []string{
		"ratelimit", "rate limit", "too many requests", "timeout", "timed out", "temporar", "try again",
		"connection reset", "connection refused", "eof", "broken pipe", "no such host", "tls handshake",
		"context canceled", "context deadline", "token expired", "invalid_grant", "unauthorized",
	}
	statusRe = regexp.MustCompile(`(?i)(?:http(?: error)?|status(?: code)?|error|code)[ :=#]*\(?([45]\d\d)\b`)
)

// permanentStatus are 4xx codes that retrying won't fix. 401 (token),
// 404/409 (parent folder still being made), 408, 423, 425 and 429 are left
// to rclone's retries.
var permanentStatus = map[string]bool{"400": true, "403": true, "405": true, "411": true, "413": true, "414": true, "415": true, "422": true, "507": true}

// IsPermanent reports whether an upload error means retrying can't help.
func IsPermanent(msg string) bool {
	l := strings.ToLower(msg)
	for _, p := range permanentPhrases {
		if strings.Contains(l, p) {
			return true
		}
	}
	for _, p := range transientPhrases {
		if strings.Contains(l, p) {
			return false
		}
	}
	for _, m := range statusRe.FindAllStringSubmatch(msg, -1) {
		if permanentStatus[m[1]] {
			return true
		}
	}
	return false
}

// ShouldStop decides whether an upload that just failed should stop
// retrying: a permanent error, or tries failed attempts (counted from the
// last user retry) reaching MaxTries.
func ShouldStop(msg string, tries int) bool {
	return IsPermanent(msg) || tries >= MaxTries
}

// Reason turns rclone's upload error into the text the user sees.
func Reason(msg string, tries int) string {
	msg = strings.TrimSpace(msg)
	if i := strings.Index(msg, "will retry in "); i >= 0 {
		if j := strings.Index(msg[i:], ": "); j >= 0 {
			msg = strings.TrimSpace(msg[i+j+2:])
		}
	}
	msg = strings.TrimSpace(strings.TrimPrefix(msg, "vfs cache: failed to transfer file from cache to remote:"))
	l := strings.ToLower(msg)
	switch {
	case strings.Contains(l, "free plan limit"):
		return "The file is bigger than your plan allows for one file (TeraBox's free plan takes files up to 4 GB). " + "Details: " + msg
	case msg == "":
		return fmt.Sprintf("The upload failed %d times.", tries)
	case !IsPermanent(msg):
		return fmt.Sprintf("The upload failed %d times. Last error: %s", tries, msg)
	}
	return msg
}

// Stuck is one upload that stopped retrying.
type Stuck struct {
	Remote string    `json:"remote"`
	Name   string    `json:"name"` // path inside the remote
	Size   int64     `json:"size"`
	Reason string    `json:"reason"`
	Tries  int       `json:"tries"`
	Since  time.Time `json:"since"`
}

func (s Stuck) Key() string { return s.Remote + ":" + s.Name }

// StuckStore is the persisted list of stuck uploads.
type StuckStore struct {
	path  string
	mu    sync.Mutex
	items map[string]Stuck
}

// OpenStuckStore loads path (a missing or broken file is an empty list).
func OpenStuckStore(path string) *StuckStore {
	s := &StuckStore{path: path, items: map[string]Stuck{}}
	if raw, err := os.ReadFile(path); err == nil {
		var list []Stuck
		if json.Unmarshal(raw, &list) == nil {
			for _, it := range list {
				s.items[it.Key()] = it
			}
		}
	}
	return s
}

func (s *StuckStore) Get(remote, name string) (Stuck, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[remote+":"+name]
	return it, ok
}

// List returns the stuck uploads of remote ("" = all), sorted.
func (s *StuckStore) List(remote string) []Stuck {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Stuck{}
	for _, it := range s.items {
		if remote == "" || it.Remote == remote {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

func (s *StuckStore) Put(it Stuck) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[it.Key()] = it
	return s.saveLocked()
}

func (s *StuckStore) Delete(remote, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[remote+":"+name]; !ok {
		return nil
	}
	delete(s.items, remote+":"+name)
	return s.saveLocked()
}

func (s *StuckStore) saveLocked() error {
	list := make([]Stuck, 0, len(s.items))
	for _, it := range s.items {
		list = append(list, it)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Key() < list[j].Key() })
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// PendingItem is one file in the cache's metadata that hasn't uploaded.
type PendingItem struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// PendingItems lists remote's pending (dirty) files in dir.
func PendingItems(dir, remote string) []PendingItem {
	var out []PendingItem
	_ = walkFiles(MetaRoot(dir, remote), func(rel, full string, _ fs.DirEntry) error {
		if m, ok := readMeta(full); ok && m.Dirty {
			out = append(out, PendingItem{Name: filepath.ToSlash(rel), Size: m.Size})
		}
		return nil
	})
	return out
}

// CleanItems lists remote's cached files in dir that are safe to drop:
// metadata readable and not dirty.
func CleanItems(dir, remote string) []string {
	var out []string
	_ = walkFiles(MetaRoot(dir, remote), func(rel, full string, _ fs.DirEntry) error {
		if m, ok := readMeta(full); ok && !m.Dirty {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

// SafeRel reports whether name is a clean relative path that stays inside
// its root (no "..", not absolute).
func SafeRel(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\x00") {
		return false
	}
	c := filepath.Clean(name)
	return c == filepath.FromSlash(name) && c != "." && c != ".." && !strings.HasPrefix(c, "../")
}

// SaveResult is what SaveCopy wrote.
type SaveResult struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// SaveCopy copies remote's cached file name (dir/vfs/remote/name) into
// destDir, keeping the file name (adding " (2)", " (3)"... rather than
// overwriting), then reads both back and compares size and SHA-256. The
// cached file is left untouched; the caller drops it only after this
// returns nil. wantSize is the size rclone recorded (the cached file must be
// complete).
func SaveCopy(dir, remote, name, destDir string, wantSize int64) (SaveResult, error) {
	var res SaveResult
	if !SafeRel(name) {
		return res, fmt.Errorf("bad file name %q", name)
	}
	src := filepath.Join(DataRoot(dir, remote), filepath.FromSlash(name))
	fi, err := os.Stat(src)
	if err != nil {
		return res, fmt.Errorf("the cached copy of %s is missing: %w", filepath.Base(name), err)
	}
	if !fi.Mode().IsRegular() {
		return res, fmt.Errorf("the cached copy of %s is not a file", filepath.Base(name))
	}
	if wantSize >= 0 && fi.Size() != wantSize {
		return res, fmt.Errorf("the cached copy of %s is incomplete (%d of %d bytes)", filepath.Base(name), fi.Size(), wantSize)
	}
	if st, err := os.Stat(destDir); err != nil || !st.IsDir() {
		return res, fmt.Errorf("%s is not a folder", destDir)
	}
	dst, err := freeName(destDir, filepath.Base(name))
	if err != nil {
		return res, err
	}
	srcSum, n, err := copyHashing(src, dst, fi)
	if err != nil {
		_ = os.Remove(dst)
		return res, err
	}
	if n != fi.Size() {
		_ = os.Remove(dst)
		return res, fmt.Errorf("short copy: %d of %d bytes", n, fi.Size())
	}
	dstSum, dn, err := hashFile(dst)
	if err != nil || dn != n || dstSum != srcSum {
		_ = os.Remove(dst)
		if err == nil {
			err = errors.New("the saved copy doesn't match the cached file")
		}
		return res, fmt.Errorf("checking the saved copy: %w", err)
	}
	return SaveResult{Path: dst, Bytes: n, SHA256: srcSum}, nil
}

func freeName(dir, base string) (string, error) {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		p := filepath.Join(dir, cand)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return p, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("too many files named %s in %s", base, dir)
}

func copyHashing(src, dst string, fi fs.FileInfo) (string, int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", n, err
	}
	_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	return fmt.Sprintf("%x", h.Sum(nil)), n, nil
}

func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return fmt.Sprintf("%x", h.Sum(nil)), n, err
}
