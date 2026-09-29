package cloudcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const teraboxLog = "vfs cache: failed to upload try #3, will retry in 40s: vfs cache: failed to transfer file from cache to remote: Size of upload file more than allowed free plan limit (4GB)"

func TestIsPermanent(t *testing.T) {
	perm := []string{
		teraboxLog,
		"http error 413: 413 Request Entity Too Large",
		"googleapi: Error 403: The user's Drive storage quota has been exceeded., storageQuotaExceeded",
		"HTTP error 400 (400 Bad Request) returned body: invalid path",
		"status code 422",
	}
	for _, m := range perm {
		if !IsPermanent(m) {
			t.Errorf("should be permanent: %q", m)
		}
	}
	transient := []string{
		"googleapi: Error 403: Rate Limit Exceeded, rateLimitExceeded",
		"http error 429: Too Many Requests",
		"http error 401: Unauthorized",
		"http error 503: Service Unavailable",
		"http error 404: Not Found",
		"Post \"https://x\": dial tcp: i/o timeout",
		"unexpected EOF",
		"",
	}
	for _, m := range transient {
		if IsPermanent(m) {
			t.Errorf("should not be permanent: %q", m)
		}
	}
}

func TestShouldStopAndReason(t *testing.T) {
	if !ShouldStop(teraboxLog, 1) {
		t.Fatal("TeraBox limit stops at once")
	}
	if ShouldStop("unexpected EOF", MaxTries-1) || !ShouldStop("unexpected EOF", MaxTries) {
		t.Fatal("transient errors stop after MaxTries")
	}
	r := Reason(teraboxLog, 3)
	if !strings.Contains(r, "4 GB") || !strings.HasSuffix(r, "Size of upload file more than allowed free plan limit (4GB)") || strings.Contains(r, "will retry") {
		t.Fatalf("reason: %q", r)
	}
	if r := Reason("vfs cache: failed to upload try #10, will retry in 5m0s: unexpected EOF", 10); !strings.Contains(r, "failed 10 times") || !strings.HasSuffix(r, "unexpected EOF") {
		t.Fatalf("reason: %q", r)
	}
}

func TestStuckStorePersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lib", "stuck.json")
	s := OpenStuckStore(p)
	if err := s.Put(Stuck{Remote: "tb", Name: "a/b.iso", Size: 7, Reason: "too big", Tries: 1}); err != nil {
		t.Fatal(err)
	}
	s.Put(Stuck{Remote: "gd", Name: "x", Reason: "r"})
	s2 := OpenStuckStore(p)
	if it, ok := s2.Get("tb", "a/b.iso"); !ok || it.Size != 7 || it.Reason != "too big" {
		t.Fatalf("reloaded: %+v %v", it, ok)
	}
	if l := s2.List("tb"); len(l) != 1 {
		t.Fatalf("list tb: %+v", l)
	}
	if l := s2.List(""); len(l) != 2 || l[0].Remote != "gd" {
		t.Fatalf("list all: %+v", l)
	}
	s2.Delete("tb", "a/b.iso")
	if _, ok := OpenStuckStore(p).Get("tb", "a/b.iso"); ok {
		t.Fatal("delete not saved")
	}
	os.WriteFile(p, []byte("garbage"), 0o600)
	if l := OpenStuckStore(p).List(""); len(l) != 0 {
		t.Fatal("broken file should load empty")
	}
}

func TestSafeRel(t *testing.T) {
	for _, ok := range []string{"a", "a/b.iso", "dir/x y (2).txt"} {
		if !SafeRel(ok) {
			t.Errorf("%q should be safe", ok)
		}
	}
	for _, bad := range []string{"", "/etc/passwd", "../x", "a/../../x", "a/./b", ".", "a//b"} {
		if SafeRel(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestSaveCopyVerifies(t *testing.T) {
	d, dest := t.TempDir(), t.TempDir()
	item(t, d, "tb", "iso/big.iso", "0123456789", true)
	res, err := SaveCopy(d, "tb", "iso/big.iso", dest, 10)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(res.Path)
	if res.Path != filepath.Join(dest, "big.iso") || string(raw) != "0123456789" || res.Bytes != 10 || len(res.SHA256) != 64 {
		t.Fatalf("save: %+v %q", res, raw)
	}
	// the cached copy is untouched (the caller drops it)
	if !exists(filepath.Join(DataRoot(d, "tb"), "iso/big.iso")) {
		t.Fatal("cached file removed by SaveCopy")
	}
	// a second save doesn't overwrite
	res2, err := SaveCopy(d, "tb", "iso/big.iso", dest, 10)
	if err != nil || res2.Path != filepath.Join(dest, "big (2).iso") {
		t.Fatalf("second save: %+v %v", res2, err)
	}
}

func TestSaveCopyRefusesIncompleteOrBad(t *testing.T) {
	d, dest := t.TempDir(), t.TempDir()
	item(t, d, "tb", "part.bin", "01234", true)
	if _, err := SaveCopy(d, "tb", "part.bin", dest, 10); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("want incomplete, got %v", err)
	}
	if _, err := SaveCopy(d, "tb", "../part.bin", dest, 5); err == nil {
		t.Fatal("path escape accepted")
	}
	if _, err := SaveCopy(d, "tb", "part.bin", filepath.Join(dest, "nope"), 5); err == nil {
		t.Fatal("missing folder accepted")
	}
	if ents, _ := os.ReadDir(dest); len(ents) != 0 {
		t.Fatalf("failed saves left files: %v", ents)
	}
}
