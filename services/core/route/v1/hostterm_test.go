package v1

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"

	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/termsession"
)

func shProc(t *testing.T) func(cols, rows uint16) (termsession.Process, error) {
	return func(cols, rows uint16) (termsession.Process, error) {
		cmd := exec.Command("/bin/sh")
		cmd.Env = []string{"PS1=$ ", "PATH=/usr/bin:/bin", "TERM=xterm-256color"}
		cmd.Dir = t.TempDir()
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		return startPTY(cmd, cols, rows)
	}
}

// attachServer serves one session's Attach over a real WebSocket.
func attachServer(t *testing.T, s *termsession.Session) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		s.Attach(ws, termsession.AttachOptions{Control: true})
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func readUntil(t *testing.T, ws *websocket.Conn, want string) string {
	t.Helper()
	var got strings.Builder
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(got.String(), want) {
		_ = ws.SetReadDeadline(deadline)
		mt, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %q: %v (got %q)", want, err, got.String())
		}
		if mt == websocket.BinaryMessage {
			got.Write(data)
		} else if len(data) > 0 && data[0] == 0 {
			got.WriteString("<ctl" + string(data[1:]) + ">")
		}
	}
	return got.String()
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestHostShellSurvivesDetachAndReplaysOnReattach(t *testing.T) {
	m := termsession.NewManager(termsession.Options{})
	defer m.Shutdown()
	var proc *ptyProc
	s, err := m.Create(termsession.Spec{Owner: "1", Kind: termsession.KindHost}, 100, 30, func(c, r uint16) (termsession.Process, error) {
		p, err := shProc(t)(c, r)
		if err == nil {
			proc = p.(*ptyProc)
		}
		return p, err
	})
	if err != nil {
		t.Fatal(err)
	}
	url := attachServer(t, s)

	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws, `"type":"live"`)
	ws.WriteMessage(websocket.BinaryMessage, []byte("echo mark-$((6*7))\n"))
	readUntil(t, ws, "mark-42")
	ws.Close()

	time.Sleep(300 * time.Millisecond)
	if !alive(proc.cmd.Process.Pid) || s.State() != termsession.StateRunning {
		t.Fatal("shell died when the viewer left")
	}
	if in := s.Info(); in.Command != "sh" || in.Cwd == "" {
		t.Fatalf("info %+v", in)
	}

	ws2, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.Close()
	got := readUntil(t, ws2, `"type":"live"`)
	if !strings.Contains(got, "mark-42") {
		t.Fatalf("replay lacks earlier output: %q", got)
	}
	ws2.WriteMessage(websocket.BinaryMessage, []byte("exit 7\n"))
	got = readUntil(t, ws2, `"type":"exit"`)
	if !strings.Contains(got, `"code":7`) {
		t.Fatalf("exit message: %q", got)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session not marked exited")
	}
	if in := s.Info(); in.ExitCode == nil || *in.ExitCode != 7 {
		t.Fatalf("exit code %+v", in)
	}
}

func TestKillingAHostSessionEndsItsJobs(t *testing.T) {
	m := termsession.NewManager(termsession.Options{})
	defer m.Shutdown()
	var proc *ptyProc
	s, err := m.Create(termsession.Spec{Owner: "1", Kind: termsession.KindHost}, 80, 24, func(c, r uint16) (termsession.Process, error) {
		p, err := shProc(t)(c, r)
		if err == nil {
			proc = p.(*ptyProc)
		}
		return p, err
	})
	if err != nil {
		t.Fatal(err)
	}
	url := attachServer(t, s)
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	readUntil(t, ws, `"type":"live"`)
	// A foreground job that would otherwise keep the terminal busy.
	ws.WriteMessage(websocket.BinaryMessage, []byte("sleep 300\n"))
	deadline := time.Now().Add(5 * time.Second)
	for s.Info().Command != "sleep" {
		if time.Now().After(deadline) {
			t.Fatalf("foreground command never became sleep: %+v", s.Info())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := m.Kill("1", s.ID()); err != nil {
		t.Fatal(err)
	}
	got := readUntil(t, ws, `"type":"exit"`)
	if !strings.Contains(got, `"reason":"killed"`) {
		t.Fatalf("exit: %q", got)
	}
	select {
	case <-proc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shell still running after kill")
	}
}

func TestResizeReachesThePty(t *testing.T) {
	p, err := shProc(t)(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Resize(132, 43); err != nil {
		t.Fatal(err)
	}
	pp := p.(*ptyProc)
	var ws *unix.Winsize
	_ = pp.control(func(fd int) error {
		w, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
		ws = w
		return err
	})
	if ws == nil || ws.Col != 132 || ws.Row != 43 {
		t.Fatalf("winsize %+v", ws)
	}
}

func TestTerminalSessionRoutesNeverSkipAuth(t *testing.T) {
	for _, p := range []string{
		HostTerminalBase, HostTerminalBase + "/", HostTerminalBase + "/abc", HostTerminalBase + "/abc/attach",
	} {
		if !nivaroos_middleware.MatchRoute(HostTerminalBase+"/*", p) {
			t.Errorf("%s is not covered by the never-skip pattern", p)
		}
	}
}
