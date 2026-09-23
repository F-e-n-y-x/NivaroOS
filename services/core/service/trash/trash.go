// Package trash is the Files app's recycle bin.
//
// Deleting used to be permanent and immediate. Now a delete moves items
// into a trash folder on the *same filesystem* as the item - so trashing is
// always a rename (instant, even for a 50 GB folder, and it can't fail
// half-way the way a copy can) - with a small JSON record of where each
// item came from. Restore renames it back (recreating missing parent
// folders, and never overwriting something that now has the same name).
//
// Layout, per filesystem root:
//
//	<root>/.nivaroos-trash/files/<id>/<original name>
//	<root>/.nivaroos-trash/info/<id>.json
package trash

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const trashDirName = ".nivaroos-trash"

// Item is one trashed file or folder.
type Item struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	OriginalPath string    `json:"original_path"`
	DeletedAt    time.Time `json:"deleted_at"`
	Size         int64     `json:"size"`
	IsDir        bool      `json:"is_dir"`
	Items        int       `json:"items,omitempty"` // entries inside a folder
	Root         string    `json:"root"`
}

type Options struct {
	// Where the list of trash roots ever used is kept.
	IndexPath string
	// Maps a path to the root of its filesystem (where its trash lives).
	// Default: the mount point, with the system disk's trash under /DATA
	// (or /var/lib/nivaroos for paths outside /DATA).
	RootFor func(path string) (string, error)
}

type Bin struct {
	mu    sync.Mutex
	opts  Options
	roots map[string]bool
}

var (
	ErrNoTrashHere = errors.New("this location has no Trash")
	errIsTrash     = errors.New("that's the Trash itself")
)

func New(opts Options) *Bin {
	if opts.RootFor == nil {
		opts.RootFor = defaultRootFor
	}
	b := &Bin{opts: opts, roots: map[string]bool{}}
	if raw, err := os.ReadFile(opts.IndexPath); err == nil {
		var list []string
		if json.Unmarshal(raw, &list) == nil {
			for _, r := range list {
				b.roots[r] = true
			}
		}
	}
	return b
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func trashDir(root string) string { return filepath.Join(root, trashDirName) }

// IsTrashPath reports whether p is inside any trash folder.
func IsTrashPath(p string) bool {
	return strings.Contains(filepath.Clean(p)+"/", "/"+trashDirName+"/")
}

func (b *Bin) pathOf(it Item) string {
	return filepath.Join(trashDir(it.Root), "files", it.ID, it.Name)
}

func (b *Bin) infoPath(root, id string) string {
	return filepath.Join(trashDir(root), "info", id+".json")
}

func (b *Bin) rememberRoot(root string) {
	if b.roots[root] {
		return
	}
	b.roots[root] = true
	list := make([]string, 0, len(b.roots))
	for r := range b.roots {
		list = append(list, r)
	}
	sort.Strings(list)
	raw, _ := json.Marshal(list)
	_ = os.MkdirAll(filepath.Dir(b.opts.IndexPath), 0o755)
	tmp := b.opts.IndexPath + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, b.opts.IndexPath)
	}
}

// Trash moves each path into the trash of its filesystem. It stops at the
// first path that can't be trashed and returns what was trashed so far
// with the error, so the caller can tell the user exactly what happened.
func (b *Bin) Trash(paths []string) ([]Item, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Item
	for _, p := range paths {
		it, err := b.trashOne(filepath.Clean(p))
		if err != nil {
			return out, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		out = append(out, it)
	}
	return out, nil
}

func (b *Bin) trashOne(p string) (Item, error) {
	if p == "/" || IsTrashPath(p) || filepath.Base(p) == trashDirName {
		return Item{}, errIsTrash
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return Item{}, err
	}
	root, err := b.opts.RootFor(p)
	if err != nil {
		return Item{}, err
	}
	if p == root {
		return Item{}, errors.New("a drive's top folder can't be moved to the Trash")
	}
	if !sameDevice(p, root) {
		return Item{}, ErrNoTrashHere
	}
	it := Item{ID: newID(), Name: fi.Name(), OriginalPath: p, DeletedAt: time.Now(), IsDir: fi.IsDir(), Root: root}
	it.Size, it.Items = measure(p, fi)

	holder := filepath.Join(trashDir(root), "files", it.ID)
	if err := os.MkdirAll(holder, 0o700); err != nil {
		return Item{}, err
	}
	if err := os.MkdirAll(filepath.Dir(b.infoPath(root, it.ID)), 0o700); err != nil {
		return Item{}, err
	}
	// Record first: if the rename then fails, the stray record is
	// removed; the reverse order could lose track of trashed data.
	raw, _ := json.Marshal(it)
	if err := os.WriteFile(b.infoPath(root, it.ID), raw, 0o600); err != nil {
		os.Remove(holder)
		return Item{}, err
	}
	if err := os.Rename(p, b.pathOf(it)); err != nil {
		os.Remove(b.infoPath(root, it.ID))
		os.Remove(holder)
		if errors.Is(err, syscall.EXDEV) {
			return Item{}, ErrNoTrashHere
		}
		return Item{}, err
	}
	b.rememberRoot(root)
	return it, nil
}

func measure(p string, fi os.FileInfo) (size int64, items int) {
	if !fi.IsDir() {
		return fi.Size(), 0
	}
	_ = filepath.Walk(p, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		items++
		if info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	})
	return size, items - 1
}

// List returns every trashed item on every filesystem that's currently
// present (a trash on an unplugged USB drive simply isn't listed), newest
// first.
func (b *Bin) List() []Item {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.listLocked()
}

func (b *Bin) listLocked() []Item {
	var out []Item
	for root := range b.roots {
		entries, err := os.ReadDir(filepath.Join(trashDir(root), "info"))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(trashDir(root), "info", e.Name()))
			if err != nil {
				continue
			}
			var it Item
			if json.Unmarshal(raw, &it) != nil {
				continue
			}
			it.Root = root
			if _, err := os.Lstat(b.pathOf(it)); err != nil {
				continue // record without data (removed by hand)
			}
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeletedAt.After(out[j].DeletedAt) })
	return out
}

func (b *Bin) find(id string) (Item, bool) {
	for _, it := range b.listLocked() {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

type Restored struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type Failure struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

type RestoreResult struct {
	Restored []Restored `json:"restored"`
	Failed   []Failure  `json:"failed"`
}

// Restore puts items back where they were. A name that's been reused
// since is never overwritten: the item comes back as "name (restored)".
func (b *Bin) Restore(ids []string) RestoreResult {
	b.mu.Lock()
	defer b.mu.Unlock()
	res := RestoreResult{Restored: []Restored{}, Failed: []Failure{}}
	for _, id := range ids {
		it, ok := b.find(id)
		if !ok {
			res.Failed = append(res.Failed, Failure{ID: id, Error: "no longer in the Trash"})
			continue
		}
		dest := it.OriginalPath
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			res.Failed = append(res.Failed, Failure{ID: id, Name: it.Name, Error: err.Error()})
			continue
		}
		if _, err := os.Lstat(dest); err == nil {
			dest = restoredName(dest)
		}
		if err := os.Rename(b.pathOf(it), dest); err != nil {
			res.Failed = append(res.Failed, Failure{ID: id, Name: it.Name, Error: err.Error()})
			continue
		}
		os.Remove(filepath.Join(trashDir(it.Root), "files", it.ID))
		os.Remove(b.infoPath(it.Root, it.ID))
		res.Restored = append(res.Restored, Restored{ID: id, Path: dest})
	}
	return res
}

func restoredName(p string) string {
	dir, name := filepath.Split(p)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		suffix := " (restored)"
		if i > 1 {
			suffix = fmt.Sprintf(" (restored %d)", i)
		}
		c := filepath.Join(dir, stem+suffix+ext)
		if _, err := os.Lstat(c); os.IsNotExist(err) {
			return c
		}
	}
}

// Delete removes items from the trash permanently.
func (b *Bin) Delete(ids []string) []Failure {
	b.mu.Lock()
	defer b.mu.Unlock()
	failed := []Failure{}
	for _, id := range ids {
		it, ok := b.find(id)
		if !ok {
			continue
		}
		if err := b.removeLocked(it); err != nil {
			failed = append(failed, Failure{ID: id, Name: it.Name, Error: err.Error()})
		}
	}
	return failed
}

func (b *Bin) removeLocked(it Item) error {
	if err := os.RemoveAll(filepath.Join(trashDir(it.Root), "files", it.ID)); err != nil {
		return err
	}
	return os.Remove(b.infoPath(it.Root, it.ID))
}

// Empty permanently removes everything in every present trash.
func (b *Bin) Empty() []Failure {
	b.mu.Lock()
	defer b.mu.Unlock()
	failed := []Failure{}
	for _, it := range b.listLocked() {
		if err := b.removeLocked(it); err != nil {
			failed = append(failed, Failure{ID: it.ID, Name: it.Name, Error: err.Error()})
		}
	}
	return failed
}

// Purge permanently removes items trashed longer ago than maxAge.
func (b *Bin) Purge(maxAge time.Duration) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	cutoff := time.Now().Add(-maxAge)
	for _, it := range b.listLocked() {
		if it.DeletedAt.Before(cutoff) && b.removeLocked(it) == nil {
			n++
		}
	}
	return n
}

// backdate is for tests: pretend an item was trashed d ago.
func (b *Bin) backdate(id string, d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it, ok := b.find(id)
	if !ok {
		return
	}
	it.DeletedAt = it.DeletedAt.Add(-d)
	raw, _ := json.Marshal(it)
	_ = os.WriteFile(b.infoPath(it.Root, it.ID), raw, 0o600)
}

// Size of everything in the trash.
func (b *Bin) Usage() (count int, bytes int64) {
	for _, it := range b.List() {
		count++
		bytes += it.Size
	}
	return
}

func sameDevice(a, b string) bool {
	var sa, sb syscall.Stat_t
	if syscall.Lstat(a, &sa) != nil || syscall.Stat(b, &sb) != nil {
		return false
	}
	return sa.Dev == sb.Dev
}
