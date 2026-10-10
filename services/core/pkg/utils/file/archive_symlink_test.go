package file

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// A symlink planted in a shared folder must never pull its target (here
// /etc/passwd, or a whole folder outside the share) into the ZIP.
func TestAddFileReportSkipsSymlinks(t *testing.T) {
	share := filepath.Join(t.TempDir(), "share")
	os.MkdirAll(filepath.Join(share, "sub"), 0o755)
	os.WriteFile(filepath.Join(share, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(share, "sub", "b.txt"), []byte("b"), 0o644)
	os.Symlink("/etc/passwd", filepath.Join(share, "passwd"))
	os.Symlink("/etc", filepath.Join(share, "sub", "etc"))
	os.Symlink(share, filepath.Join(share, "loop"))

	var buf bytes.Buffer
	_, ar, _ := GetCompressionAlgorithm("zip")
	ar.Create(&buf)
	var skipped []string
	if err := AddFileReport(ar, share, share, func(p string, err error) { skipped = append(skipped, filepath.Base(p)) }); err != nil {
		t.Fatal(err)
	}
	ar.Close()

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name == "a.txt" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			if string(b) != "a" {
				t.Fatalf("a.txt = %q", b)
			}
		}
	}
	sort.Strings(names)
	sort.Strings(skipped)
	if got, want := names, []string{"a.txt", "sub/", "sub/b.txt"}; !equal(got, want) {
		t.Fatalf("archive = %v, want %v", got, want)
	}
	if want := []string{"etc", "loop", "passwd"}; !equal(skipped, want) {
		t.Fatalf("skipped = %v, want %v", skipped, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
