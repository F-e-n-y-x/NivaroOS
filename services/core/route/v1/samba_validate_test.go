package v1

import (
	"os"
	"path/filepath"
	"testing"
)

// A share name goes into smb.conf as "[name]": "x]\nroot preexec = ..." ran
// a command as root on the next connection.
func TestShareNameCantInjectSambaConfig(t *testing.T) {
	for _, bad := range []string{"x]\nroot preexec = id", "a\rb", "[global]", "global", "homes", "printers", "", "a/b", "name;cmd"} {
		if err := validateShareName(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, good := range []string{"Media", "tower", "My Photos", "backup_2026", "blue-drive.v2"} {
		if err := validateShareName(good); err != nil {
			t.Errorf("rejected %q: %v", good, err)
		}
	}
}

// Sharing (and chmod 0777-ing) any path was allowed - "/" or "/etc".
func TestSharePathMustBeAFolderInsideTheDataRoots(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "DATA")
	os.MkdirAll(filepath.Join(data, "Media"), 0o755)
	os.WriteFile(filepath.Join(data, "file.txt"), []byte("x"), 0o644)
	roots := []string{data}

	if err := validateSharePath(filepath.Join(data, "Media"), roots); err != nil {
		t.Errorf("folder inside a root rejected: %v", err)
	}
	if err := validateSharePath(data, roots); err != nil {
		t.Errorf("the data root itself rejected: %v", err)
	}
	for _, bad := range []string{"/", "/etc", root, filepath.Join(data, "..", "..", "etc"), filepath.Join(data, "file.txt"), filepath.Join(data, "missing"), filepath.Join(data, "Me\ndia"), "relative/path"} {
		if err := validateSharePath(bad, roots); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
