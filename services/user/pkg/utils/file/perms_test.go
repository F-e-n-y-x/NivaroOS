package file

import (
	"os"
	"path/filepath"
	"testing"
)

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// User data directories were created 0777: any local account could plant
// or replace another user's settings.
func TestRestrictWorldWritable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "conf")
	user1 := filepath.Join(root, "1")
	if err := os.MkdirAll(user1, 0o777); err != nil {
		t.Fatal(err)
	}
	os.Chmod(root, 0o777)
	os.Chmod(user1, 0o777)
	os.WriteFile(filepath.Join(user1, "app_order.json"), []byte("[]"), 0o644)
	private := filepath.Join(root, "2")
	os.Mkdir(private, 0o700)

	RestrictWorldWritable(root)
	RestrictWorldWritable(root) // idempotent

	if m := mode(t, root); m != 0o755 {
		t.Errorf("root %o", m)
	}
	if m := mode(t, user1); m != 0o755 {
		t.Errorf("user dir %o", m)
	}
	if m := mode(t, private); m != 0o700 {
		t.Errorf("a stricter dir was loosened to %o", m)
	}
	if m := mode(t, filepath.Join(user1, "app_order.json")); m != 0o644 {
		t.Errorf("file mode changed to %o", m)
	}
}

func TestMkDirIsNotWorldWritable(t *testing.T) {
	d := filepath.Join(t.TempDir(), "3")
	if err := MkDir(d); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, d); m != 0o755 {
		t.Fatalf("MkDir made %o", m)
	}
}
