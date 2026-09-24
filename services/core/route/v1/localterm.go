package v1

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/wsterm"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/config"
)

// passwdPath is a var so tests can point it at a fixture.
var passwdPath = "/etc/passwd"

// terminalUserConfigKey is an optional [system] key in the core config
// (casaos.conf) naming the host account the web terminal runs as, e.g.
//
//	[system]
//	TerminalUser = alice
//
// Nothing writes it by default; it exists so an admin can pin the account
// when the automatic choice below isn't the one they want.
const terminalUserConfigKey = "TerminalUser"

// passwdEntry is one /etc/passwd line.
type passwdEntry struct {
	Name, Gecos, Home, Shell string
	UID, GID                 int
}

func parsePasswd(r io.Reader) []passwdEntry {
	var out []passwdEntry
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			continue
		}
		f := strings.Split(line, ":")
		if len(f) < 7 {
			continue
		}
		uid, err1 := strconv.Atoi(f[2])
		gid, err2 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, passwdEntry{Name: f[0], UID: uid, GID: gid, Gecos: f[4], Home: f[5], Shell: f[6]})
	}
	return out
}

func loginShell(shell string) bool {
	return shell != "" && !strings.HasSuffix(shell, "nologin") && !strings.HasSuffix(shell, "/false")
}

// pickTerminalUser chooses deterministically from local /etc/passwd
// entries (never NSS/getent, which on an LDAP/SSSD-joined host can list
// directory accounts in arbitrary order):
//  1. configured (if set and present locally),
//  2. the lowest local UID in 1000..59999 with a login shell,
//  3. root.
//
// The returned reason says which rule applied.
func pickTerminalUser(entries []passwdEntry, configured string) (passwdEntry, string) {
	if configured != "" {
		for _, e := range entries {
			if e.Name == configured {
				return e, "configured [system] " + terminalUserConfigKey
			}
		}
	}
	var regular []passwdEntry
	for _, e := range entries {
		if e.UID >= 1000 && e.UID < 60000 && loginShell(e.Shell) {
			regular = append(regular, e)
		}
	}
	if len(regular) > 0 {
		sort.SliceStable(regular, func(i, j int) bool { return regular[i].UID < regular[j].UID })
		return regular[0], "lowest local uid>=1000 in " + passwdPath
	}
	for _, e := range entries {
		if e.UID == 0 {
			return e, "no regular local user, falling back to root"
		}
	}
	return passwdEntry{Name: "root", UID: 0, GID: 0, Home: "/root", Shell: "/bin/sh"}, "no regular local user, falling back to root"
}

func configuredTerminalUser() string {
	if config.Cfg == nil {
		return ""
	}
	return strings.TrimSpace(config.Cfg.Section("system").Key(terminalUserConfigKey).String())
}

// getDefaultDesktopUser returns the host account the web terminal runs as
// (also treated as the protected owner account by sysusers.go).
func getDefaultDesktopUser() (*user.User, error) {
	u, _, err := resolveTerminalUser()
	return u, err
}

func resolveTerminalUser() (*user.User, string, error) {
	f, err := os.Open(passwdPath)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	e, reason := pickTerminalUser(parsePasswd(f), configuredTerminalUser())
	return &user.User{
		Uid:      strconv.Itoa(e.UID),
		Gid:      strconv.Itoa(e.GID),
		Username: e.Name,
		Name:     strings.Split(e.Gecos, ",")[0],
		HomeDir:  e.Home,
	}, reason, nil
}

// WsLocalTerm serves the host terminal over the wsterm protocol (see
// services/common/utils/wsterm): BINARY frames carry input/output, TEXT
// frames starting with 0x00 carry JSON control ({"type":"resize",...}),
// initial size from ?cols=&rows=.
func WsLocalTerm(ctx echo.Context) error {
	wsConn, err := upgrader.Upgrade(ctx.Response().Writer, ctx.Request(), nil)
	if err != nil {
		// Upgrade has already written an HTTP error response.
		return nil
	}
	defer wsConn.Close()

	cols, rows := wsterm.ParseSize(ctx.QueryParam("cols"), ctx.QueryParam("rows"), 120, 32)

	u, reason, err := resolveTerminalUser()
	if err != nil {
		wsterm.SendError(wsConn, "local terminal user not found: "+err.Error())
		return nil
	}
	logger.Info("local terminal session", zap.String("user", u.Username), zap.String("reason", reason))
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	var groupIds []uint32
	if gids, gErr := u.GroupIds(); gErr == nil {
		for _, gidStr := range gids {
			if gidInt, convErr := strconv.Atoi(gidStr); convErr == nil {
				groupIds = append(groupIds, uint32(gidInt))
			}
		}
	}
	if len(groupIds) == 0 {
		groupIds = []uint32{uint32(gid)}
	}

	shell := "/bin/bash"
	if _, statErr := os.Stat(shell); statErr != nil {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell, "-l")
	cmd.Dir = u.HomeDir
	cmd.Env = []string{
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"LANGUAGE=en_US:en",
		"HOME=" + u.HomeDir,
		"USER=" + u.Username,
		"LOGNAME=" + u.Username,
		"SHELL=" + shell,
		"PATH=" + u.HomeDir + "/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games",
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:    uint32(uid),
			Gid:    uint32(gid),
			Groups: groupIds,
		},
		Setsid: true,
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		wsterm.SendError(wsConn, "failed to start terminal: "+err.Error())
		return nil
	}

	waitDone := make(chan struct{})
	go func() {
		_ = cmd.Wait() // always reap - no zombie shells
		close(waitDone)
	}()
	defer func() {
		_ = ptmx.Close()
		// Hang up the whole session (Setsid made the shell its leader), give
		// it a moment to exit, then force it; the Wait goroutine reaps it.
		// (Never signal after the shell was reaped - its pid/pgid could
		// already belong to something else.)
		pid := cmd.Process.Pid
		select {
		case <-waitDone:
			return
		default:
		}
		_ = syscall.Kill(-pid, syscall.SIGHUP)
		select {
		case <-waitDone:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill() // os.Process guards against a reaped pid
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			<-waitDone
		}
	}()

	quit := make(chan struct{}, 2)
	go func() {
		_ = wsterm.CopyOutput(ptmx, wsConn) // ends when the shell exits (EIO)
		quit <- struct{}{}
	}()
	go func() {
		_ = wsterm.Pump(wsConn, ptmx, func(c, r uint16) {
			_ = pty.Setsize(ptmx, &pty.Winsize{Rows: r, Cols: c})
		})
		quit <- struct{}{}
	}()

	<-quit
	wsterm.Close(wsConn, websocket.CloseNormalClosure, "")
	return nil
}
