package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The disc must carry the scripts the UI and README point users to.
func TestGuestToolsEmbedsTheSetupScripts(t *testing.T) {
	for _, f := range []string{"guesttools/windows/NivaroOS-Guest-Tools-Setup.bat", "guesttools/linux/nivaroos-guest-setup.sh", "guesttools/README.txt"} {
		b, err := guestToolsFS.ReadFile(f)
		if err != nil || len(b) == 0 {
			t.Fatalf("%s missing from the embedded guest tools: %v", f, err)
		}
	}
	bat, _ := guestToolsFS.ReadFile("guesttools/windows/NivaroOS-Guest-Tools-Setup.bat")
	for _, want := range []string{"virtio-win-guest-tools.exe", "winfsp.msi", "spice-vdagent-x64.msi", "VirtioFsSvc"} {
		if !strings.Contains(string(bat), want) {
			t.Fatalf("Windows setup doesn't reference %s", want)
		}
	}
	sh, _ := guestToolsFS.ReadFile("guesttools/linux/nivaroos-guest-setup.sh")
	if !strings.Contains(string(sh), "TAG=share") {
		t.Fatal("Linux setup must mount the virtiofs tag the VMs export (share)")
	}
}

// The SPICE agent MSI is only put on the disc when it matches the pinned
// checksum; a good cached copy is reused without downloading again.
func TestSpiceAgentDownloadIsVerified(t *testing.T) {
	body := "pretend msi"
	sum := "e5d4a1a7a0cbd4f0a5d86b0e0e3a0a7c2a0e2c7b1e0b8c9f3a4a1c2e3f4d5a6b"
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(body))
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "vdagent.msi")

	if err := ensureVerifiedDownload(context.Background(), srv.URL, dst, int64(len(body)), sum); err == nil {
		t.Fatal("a download with the wrong checksum must be refused")
	}
	if fileExists(dst) {
		t.Fatal("a download with the wrong checksum must not be kept")
	}
	good := fileSHA256Of(t, body)
	if err := ensureVerifiedDownload(context.Background(), srv.URL, dst, int64(len(body)), good); err != nil {
		t.Fatal(err)
	}
	if err := ensureVerifiedDownload(context.Background(), srv.URL, dst, int64(len(body)), good); err != nil || hits != 2 {
		t.Fatalf("the verified cached copy should be reused (hits %d, err %v)", hits, err)
	}
}

func fileSHA256Of(t *testing.T, content string) string {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return fileSHA256(p)
}
