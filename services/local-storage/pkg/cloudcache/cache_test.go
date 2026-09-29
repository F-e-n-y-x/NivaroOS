package cloudcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// item writes one cached file (data + metadata) for remote under dir.
func item(t *testing.T, dir, remote, name, body string, dirty bool) {
	t.Helper()
	data := filepath.Join(DataRoot(dir, remote), name)
	meta := filepath.Join(MetaRoot(dir, remote), name)
	for _, p := range []string{data, meta} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(data, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"Size": len(body), "Dirty": dirty})
	if err := os.WriteFile(meta, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestScan(t *testing.T) {
	d := t.TempDir()
	item(t, d, "tb", "a/clean.txt", "hello", false)
	item(t, d, "tb", "b/pending.iso", "0123456789", true)
	item(t, d, "gd", "x", "zz", false)
	u, err := Scan(d, "tb")
	if err != nil {
		t.Fatal(err)
	}
	if u.Files != 2 || u.PendingFiles != 1 || u.PendingBytes != 10 || u.UsedBytes <= 0 {
		t.Fatalf("scan: %+v", u)
	}
	if got := Remotes(d); strings.Join(got, ",") != "gd,tb" {
		t.Fatalf("remotes: %v", got)
	}
	if p := PendingItems(d, "tb"); len(p) != 1 || p[0].Name != "b/pending.iso" || p[0].Size != 10 {
		t.Fatalf("pending items: %+v", p)
	}
	if c := CleanItems(d, "tb"); len(c) != 1 || c[0] != "a/clean.txt" {
		t.Fatalf("clean items: %v", c)
	}
	if u, _ := Scan(d, "missing"); u != (Usage{}) {
		t.Fatalf("missing remote: %+v", u)
	}
}

func TestClearNeverTouchesPending(t *testing.T) {
	d := t.TempDir()
	item(t, d, "tb", "a/clean.txt", "hello", false)
	item(t, d, "tb", "b/pending.iso", "0123456789", true)
	// unreadable metadata counts as pending
	item(t, d, "tb", "c/broken", "data", false)
	os.WriteFile(filepath.Join(MetaRoot(d, "tb"), "c/broken"), []byte("{not json"), 0o600)
	// orphan data file (no metadata)
	os.MkdirAll(filepath.Join(DataRoot(d, "tb"), "o"), 0o700)
	os.WriteFile(filepath.Join(DataRoot(d, "tb"), "o/orphan"), []byte("x"), 0o600)
	item(t, d, "other", "keep", "k", false)

	res, err := Clear(d, "tb")
	if err != nil {
		t.Fatal(err)
	}
	if res.KeptPending != 2 || res.RemovedFiles != 2 {
		t.Fatalf("clear result: %+v", res)
	}
	for _, p := range []string{"b/pending.iso", "c/broken"} {
		if !exists(filepath.Join(DataRoot(d, "tb"), p)) || !exists(filepath.Join(MetaRoot(d, "tb"), p)) {
			t.Fatalf("pending %s was touched", p)
		}
	}
	for _, p := range []string{filepath.Join(DataRoot(d, "tb"), "a/clean.txt"), filepath.Join(MetaRoot(d, "tb"), "a/clean.txt"), filepath.Join(DataRoot(d, "tb"), "o/orphan"), filepath.Join(DataRoot(d, "tb"), "a")} {
		if exists(p) {
			t.Fatalf("%s should be gone", p)
		}
	}
	if !exists(filepath.Join(DataRoot(d, "other"), "keep")) {
		t.Fatal("another account's cache was cleared")
	}
}

func TestMigrateMovesPendingDropsClean(t *testing.T) {
	old, nw := t.TempDir(), filepath.Join(t.TempDir(), "new")
	item(t, old, "tb", "a/clean.txt", "hello", false)
	item(t, old, "tb", "b/pending.iso", "0123456789", true)
	res, err := Migrate(old, nw, MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.MovedFiles != 1 || res.MovedBytes != 10 || len(res.Left) != 0 || !res.RemovedDir {
		t.Fatalf("migrate: %+v", res)
	}
	raw, err := os.ReadFile(filepath.Join(DataRoot(nw, "tb"), "b/pending.iso"))
	if err != nil || string(raw) != "0123456789" {
		t.Fatalf("pending data not moved: %q %v", raw, err)
	}
	if m, ok := readMeta(filepath.Join(MetaRoot(nw, "tb"), "b/pending.iso")); !ok || !m.Dirty {
		t.Fatal("pending metadata not moved")
	}
	if exists(filepath.Join(DataRoot(nw, "tb"), "a/clean.txt")) || exists(old) {
		t.Fatal("clean data should be dropped and the old dir removed")
	}
}

func TestMigrateKeepsPendingOnClashAndNoSpace(t *testing.T) {
	old, nw := t.TempDir(), t.TempDir()
	item(t, old, "tb", "same", "old-version", true)
	item(t, nw, "tb", "same", "new-version", true)
	item(t, old, "tb", "big", "0123456789", true)
	res, err := Migrate(old, nw, MigrateOptions{Free: func(string) (uint64, error) { return 1, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Left) != 1 || res.MovedFiles != 1 || res.RemovedDir {
		t.Fatalf("migrate: %+v", res)
	}
	// sameFS is true for two temp dirs, so the space check is skipped and
	// "big" moves by rename; only the clash stays behind.
	if !exists(filepath.Join(DataRoot(old, "tb"), "same")) {
		t.Fatal("clashing pending item must stay in the old cache")
	}
	raw, _ := os.ReadFile(filepath.Join(DataRoot(nw, "tb"), "same"))
	if string(raw) != "new-version" {
		t.Fatal("pending item in the new cache was overwritten")
	}
}
