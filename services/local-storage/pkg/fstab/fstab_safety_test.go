package fstab

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func managedFstab(t *testing.T, n int) *FStab {
	t.Helper()
	var b strings.Builder
	b.WriteString("UUID=root-uuid\t/\text4\tdefaults\t0\t1\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "UUID=disk-%02d\t/DATA/d%02d\tntfs3\tdefaults,nofail\t0\t2\t# %s\n", i, i, ManagedComment)
	}
	f := &FStab{path: filepath.Join(t.TempDir(), "fstab")}
	if err := os.WriteFile(f.path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

// Every change wrote the same temp file (no O_TRUNC, no lock): toggling a
// few drives at once could interleave writes and lose or garble lines -
// including the root filesystem's.
func TestConcurrentChangesNeverLoseOrGarbleLines(t *testing.T) {
	f := managedFstab(t, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		mp := fmt.Sprintf("/DATA/d%02d", i)
		wg.Add(1)
		go func(mp string, i int) {
			defer wg.Done()
			if err := f.RemoveByMountPoint(mp, true); err != nil { // disable
				t.Errorf("disable %s: %v", mp, err)
			}
			if i%2 == 0 {
				if err := f.Enable(mp); err != nil {
					t.Errorf("enable %s: %v", mp, err)
				}
			}
		}(mp, i)
	}
	wg.Wait()

	raw, _ := os.ReadFile(f.path)
	text := string(raw)
	if !strings.Contains(text, "UUID=root-uuid\t/\text4") {
		t.Fatalf("root line lost:\n%s", text)
	}
	all, err := f.GetAllEntries()
	if err != nil {
		t.Fatalf("fstab no longer parses: %v\n%s", err, text)
	}
	seen := map[string]int{}
	for _, e := range all {
		seen[e.MountPoint]++
	}
	for i := 0; i < 20; i++ {
		if mp := fmt.Sprintf("/DATA/d%02d", i); seen[mp] != 1 {
			t.Errorf("%s appears %d times", mp, seen[mp])
		}
	}
	if strings.Count(text, "\n") != 21 {
		t.Errorf("line count %d, want 21:\n%s", strings.Count(text, "\n"), text)
	}
}

// Editing a disabled drive: the remove step only matched active lines, so
// the commented one survived and the edit appended a second entry.
func TestRemovingAMountPointAlsoRemovesItsDisabledLine(t *testing.T) {
	f := managedFstab(t, 2)
	if err := f.RemoveByMountPoint("/DATA/d01", true); err != nil { // disable
		t.Fatal(err)
	}
	if err := f.RemoveAnyByMountPoint("/DATA/d01"); err != nil {
		t.Fatal(err)
	}
	e := Entry{Source: "UUID=disk-01", MountPoint: "/DATA/d01", FSType: "ntfs3", Options: "defaults,nofail", Pass: 2}
	if err := f.Add(e, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(f.path)
	if n := strings.Count(string(raw), "/DATA/d01"); n != 1 {
		t.Fatalf("/DATA/d01 appears %d times:\n%s", n, raw)
	}
}

// One malformed line made the whole Persistent Mounts panel fail to load.
func TestAMalformedLineDoesNotHideTheOthers(t *testing.T) {
	f := managedFstab(t, 2)
	raw, _ := os.ReadFile(f.path)
	os.WriteFile(f.path, append(raw, []byte("UUID=bad\t/DATA/bad\text4\tdefaults\tX\tY\n")...), 0o644)
	all, err := f.GetAllEntries()
	if err != nil {
		t.Fatalf("one bad line broke the list: %v", err)
	}
	if len(all) < 3 { // root + 2 managed
		t.Fatalf("got %d entries", len(all))
	}
}
