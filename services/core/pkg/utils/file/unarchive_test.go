package file

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

type entry struct {
	name, body, link string
	typ              byte // tar typeflag; 0 = regular file
}

func writeZip(t *testing.T, path string, entries []entry) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		h.SetMode(0o644)
		body := e.body
		if e.typ == tar.TypeSymlink {
			h.SetMode(fs.ModeSymlink | 0o777)
			body = e.link
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	os.WriteFile(path, buf.Bytes(), 0o644)
}

func writeTarGz(t *testing.T, path string, entries []entry) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: tar.TypeReg, Size: int64(len(e.body)), Linkname: e.link}
		if e.typ != 0 {
			h.Typeflag, h.Size = e.typ, 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(e.body))
	}
	tw.Close()
	gz.Close()
	os.WriteFile(path, buf.Bytes(), 0o644)
}

// setup returns (archive path, empty dest, outside dir holding victim.txt).
func setup(t *testing.T, ext string) (string, string, string) {
	tmp := t.TempDir()
	outside := filepath.Join(tmp, "outside")
	dest := filepath.Join(tmp, "dest")
	os.Mkdir(outside, 0o755)
	os.Mkdir(dest, 0o755)
	os.WriteFile(filepath.Join(outside, "victim.txt"), []byte("original"), 0o644)
	return filepath.Join(tmp, "a"+ext), dest, outside
}

// assertOutsideUntouched fails if anything but the original victim.txt is in outside.
func assertOutsideUntouched(t *testing.T, outside string) {
	t.Helper()
	ents, _ := os.ReadDir(outside)
	if len(ents) != 1 {
		t.Fatalf("outside dir changed: %v", ents)
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "victim.txt")); string(b) != "original" {
		t.Fatalf("victim.txt overwritten: %q", b)
	}
}

func TestUnarchiveRefusesEscapes(t *testing.T) {
	cases := []struct {
		name    string
		ext     string
		entries func(outside string) []entry
		wantErr bool
	}{
		{"zip-slip dotdot", ".zip", func(string) []entry {
			return []entry{{name: "ok.txt", body: "x"}, {name: "../outside/pwn.txt", body: "pwned"}}
		}, true},
		{"zip absolute path", ".zip", func(o string) []entry {
			return []entry{{name: filepath.Join(o, "pwn.txt"), body: "pwned"}}
		}, true},
		{"zip overwrite via dotdot", ".zip", func(string) []entry {
			return []entry{{name: "a/../../outside/victim.txt", body: "pwned"}}
		}, true},
		{"zip symlink then write through it", ".zip", func(o string) []entry {
			return []entry{{name: "link", typ: tar.TypeSymlink, link: o}, {name: "link/pwn.txt", body: "pwned"}}
		}, false},
		{"tar dotdot", ".tar.gz", func(string) []entry {
			return []entry{{name: "../outside/pwn.txt", body: "pwned"}}
		}, true},
		{"tar absolute path", ".tar.gz", func(o string) []entry {
			return []entry{{name: filepath.Join(o, "pwn.txt"), body: "pwned"}}
		}, true},
		{"tar symlink then write through it", ".tar.gz", func(o string) []entry {
			return []entry{{name: "link", typ: tar.TypeSymlink, link: o}, {name: "link/pwn.txt", body: "pwned"}}
		}, false},
		{"tar hardlink to outside file then write", ".tar.gz", func(o string) []entry {
			return []entry{{name: "hl", typ: tar.TypeLink, link: filepath.Join(o, "victim.txt")}, {name: "hl", body: "pwned"}}
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			arc, dest, outside := setup(t, c.ext)
			if c.ext == ".zip" {
				writeZip(t, arc, c.entries(outside))
			} else {
				writeTarGz(t, arc, c.entries(outside))
			}
			err := Unarchive(context.Background(), arc, dest)
			if c.wantErr && err == nil {
				t.Fatal("expected the archive to be refused")
			}
			assertOutsideUntouched(t, outside)
			// links are never created inside dest either
			filepath.WalkDir(dest, func(p string, d fs.DirEntry, _ error) error {
				if d != nil && d.Type()&fs.ModeSymlink != 0 {
					t.Errorf("symlink created: %s", p)
				}
				return nil
			})
		})
	}
}

func TestUnarchiveNormal(t *testing.T) {
	for _, ext := range []string{".zip", ".tar.gz"} {
		arc, dest, _ := setup(t, ext)
		entries := []entry{{name: "dir/", typ: tar.TypeDir}, {name: "dir/a.txt", body: "hello"}, {name: "deep/x/b.txt", body: "world"}}
		if ext == ".zip" {
			entries[0].typ = 0 // zip dirs are just "name/"
			writeZip(t, arc, entries)
		} else {
			writeTarGz(t, arc, entries)
		}
		if err := Unarchive(context.Background(), arc, dest); err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		for p, want := range map[string]string{"dir/a.txt": "hello", "deep/x/b.txt": "world"} {
			if b, err := os.ReadFile(filepath.Join(dest, p)); err != nil || string(b) != want {
				t.Fatalf("%s: %s = %q, %v", ext, p, b, err)
			}
		}
	}
}

// Round trip: what the download/compress side writes, Unarchive reads back.
func TestArchiveRoundTrip(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "folder", "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "folder", "sub", "f.txt"), []byte("data"), 0o644)
	for _, kind := range []string{"zip", "targz", "tarxz"} {
		ext, ar, err := GetCompressionAlgorithm(kind)
		if err != nil {
			t.Fatal(err)
		}
		arc := filepath.Join(t.TempDir(), "out"+ext)
		out, _ := os.Create(arc)
		ar.Create(out)
		if err := AddFile(ar, filepath.Join(src, "folder"), src); err != nil {
			t.Fatal(err)
		}
		if err := ar.Close(); err != nil {
			t.Fatal(err)
		}
		out.Close()
		dest := t.TempDir()
		if err := Unarchive(context.Background(), arc, dest); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if b, _ := os.ReadFile(filepath.Join(dest, "folder", "sub", "f.txt")); string(b) != "data" {
			t.Fatalf("%s: round trip lost data: %q", kind, b)
		}
	}
}
