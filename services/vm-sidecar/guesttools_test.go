package main

import (
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
	for _, want := range []string{"virtio-win-guest-tools.exe", "winfsp.msi", "VirtioFsSvc"} {
		if !strings.Contains(string(bat), want) {
			t.Fatalf("Windows setup doesn't reference %s", want)
		}
	}
	sh, _ := guestToolsFS.ReadFile("guesttools/linux/nivaroos-guest-setup.sh")
	if !strings.Contains(string(sh), "TAG=share") {
		t.Fatal("Linux setup must mount the virtiofs tag the VMs export (share)")
	}
}
