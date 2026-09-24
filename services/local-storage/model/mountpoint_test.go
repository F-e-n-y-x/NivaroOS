package model

import (
	"strings"
	"testing"
)

func TestValidateMountPoint(t *testing.T) {
	good := []string{"/mnt/Storage_sdb1", "/media/My Disk", "/DATA/tower", "/mnt/a.b-c_d"}
	for _, p := range good {
		if err := ValidateMountPoint(p); err != nil {
			t.Errorf("%q refused: %v", p, err)
		}
	}
	bad := []string{
		"", "mnt/x", "/mnt", "/mnt/", "/mnt/../etc", "/mnt/a/b", "/etc/x", "/", "/DATA",
		"/mnt/x;reboot", "/mnt/$(id)", "/mnt/a`b`", "/mnt/-rf", "/mnt/..", "/mnt/.",
		"/mnt/a\nb", "/mnt/ lead", "/mnt/x|y", "/mnt/x&y", "/mnt//x", "/mntx/y",
	}
	for _, p := range bad {
		if err := ValidateMountPoint(p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
}

func TestValidateStorageName(t *testing.T) {
	for _, n := range []string{"", "Tower", "my disk", "日本"} {
		if err := ValidateStorageName(n); err != nil {
			t.Errorf("%q refused: %v", n, err)
		}
	}
	for _, n := range []string{"../etc", "a/b", "..", "a\\b", "x\x00", "a\nb", strings.Repeat("a", 65)} {
		if err := ValidateStorageName(n); err == nil {
			t.Errorf("%q accepted", n)
		}
	}
}

func TestSanitizeMountName(t *testing.T) {
	cases := map[string]string{
		"Samsung SSD 870":  "Samsung SSD 870",
		"x;rm -rf /":       "x_rm -rf _",
		"-opt":             "opt",
		"...":              "_.",
		"..":               "_",
		"a..b":             "a_b",
		"  $(id)  ":        "__id_",
		"WDC WD40EFRX/68N": "WDC WD40EFRX_68N",
	}
	for in, want := range cases {
		if got := SanitizeMountName(in); got != want {
			t.Errorf("SanitizeMountName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGetMountPointStaysInsideMnt(t *testing.T) {
	always := func(string) bool { return true }
	never := func(string) bool { return false }
	d := LSBLKModel{Name: "sdb1", Label: "evil;reboot", Model: "Disk$(id)"}
	for _, name := range []string{"", "Tower", "x y"} {
		mp, err := d.getMountPoint(name, always)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if err := ValidateMountPoint(mp); err != nil || !strings.HasPrefix(mp, "/mnt/") {
			t.Fatalf("%q -> %q unsafe (%v)", name, mp, err)
		}
	}
	for _, name := range []string{"../etc", "../../root", "a/b", ".."} {
		if mp, err := d.getMountPoint(name, always); err == nil {
			t.Errorf("%q accepted -> %q", name, mp)
		}
	}
	mp, err := d.getMountPoint("Tower", never)
	if err != nil || mp != "/mnt/Tower_evil_reboot_Disk__id__sdb1" {
		t.Errorf("collision path = %q, %v", mp, err)
	}
}
