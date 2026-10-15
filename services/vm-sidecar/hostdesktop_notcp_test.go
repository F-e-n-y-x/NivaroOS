package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// C1 (security audit 2026-10): x11vnc has no password, so it must never
// listen on TCP - no network namespace where possible, and the wrapper
// kills it if a listener shows up anyway.
func TestHostDesktopNeverListensOnTCP(t *testing.T) {
	script := string(hostDesktopScriptContent)
	for _, want := range []string{"unshare --net --", `refuse_tcp "$CHILD"`} {
		if !strings.Contains(script, want) {
			t.Errorf("wrapper script lacks %q", want)
		}
	}
	if n := strings.Count(script, `refuse_tcp "$CHILD"`); n < 3 {
		t.Errorf("refuse_tcp runs %d times; want after start, in the X11 watch loop and in the Wayland loop", n)
	}

	// Without the entry point the script would go on to start x11vnc on the
	// real display - never run it then.
	if !strings.Contains(script, `"--tcp-listeners"`) {
		t.Fatal("wrapper script lacks the --tcp-listeners entry point")
	}
	path := filepath.Join(t.TempDir(), "hd.sh")
	if err := os.WriteFile(path, hostDesktopScriptContent, 0o755); err != nil {
		t.Fatal(err)
	}
	count := func(pid int) string {
		out, err := exec.Command("bash", path, "--tcp-listeners", strconv.Itoa(pid)).Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}

	quiet := exec.Command("sleep", "30")
	if err := quiet.Start(); err != nil {
		t.Fatal(err)
	}
	defer quiet.Process.Kill()
	if got := count(quiet.Process.Pid); got != "0" {
		t.Errorf("process without sockets: %s listeners, want 0", got)
	}

	// This test process, listening on TCP (IPv4, and IPv6 where the box has it).
	l4, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l4.Close()
	want := 1
	if l6, err := net.Listen("tcp6", "[::1]:0"); err == nil { // the C1 listener was IPv6
		defer l6.Close()
		want = 2
	}
	got, _ := strconv.Atoi(count(os.Getpid()))
	if got < want {
		t.Errorf("process listening on TCP: %d listeners, want >= %d", got, want)
	}
}
