package v1

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/labstack/echo/v5"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/termsession"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/config"
)

// HostTerminalBase is where host terminal sessions are served.
const HostTerminalBase = "/v1/sys/terminal-sessions"

var (
	hostTermOnce sync.Once
	hostTermAPI  *termsession.API
)

// HostTerminalAPI is the process-wide host terminal session manager and
// its routes (see docs/specs/2026-09-29-terminal-sessions.md). Limits come
// from the optional [terminal] section of the core config.
func HostTerminalAPI() *termsession.API {
	hostTermOnce.Do(func() {
		opts := termsession.OptionsFromConfig(terminalConfigKey)
		opts.AttachBase = HostTerminalBase
		hostTermAPI = &termsession.API{
			M:        termsession.NewManager(opts),
			Upgrader: &upgrader,
			Start:    startHostTerminal,
		}
	})
	return hostTermAPI
}

// terminalConfigKey reads [terminal] keys without creating them (a plain
// Section()/Key() lookup would add empty entries to the saved config).
func terminalConfigKey(key string) string {
	if config.Cfg == nil {
		return ""
	}
	sec, err := config.Cfg.GetSection("terminal")
	if err != nil || sec == nil || !sec.HasKey(key) {
		return ""
	}
	return sec.Key(key).String()
}

func startHostTerminal(_ *echo.Context, _ termsession.CreateRequest) (termsession.Spec, func(cols, rows uint16) (termsession.Process, error), error) {
	u, reason, err := resolveTerminalUser()
	if err != nil {
		return termsession.Spec{}, nil, errors.New("local terminal user not found: " + err.Error())
	}
	shell := "/bin/bash"
	if _, statErr := os.Stat(shell); statErr != nil {
		shell = "/bin/sh"
	}
	spec := termsession.Spec{Kind: termsession.KindHost, User: u.Username, Shell: shell}
	return spec, func(cols, rows uint16) (termsession.Process, error) {
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
			Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: groupIds},
			Setsid:     true,
		}
		return startPTY(cmd, cols, rows)
	}, nil
}

// ptyProc is a command on a pty, as a termsession.Process.
type ptyProc struct {
	cmd  *exec.Cmd
	ptmx *os.File // non-blocking, so Close interrupts a pending Read
	done chan struct{}

	closeOnce sync.Once
}

// startPTY starts cmd (whose SysProcAttr should set Setsid) on a new pty.
func startPTY(cmd *exec.Cmd, cols, rows uint16) (*ptyProc, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, err
	}
	// pty hands back a blocking *os.File, where Close can't interrupt a
	// Read stuck on output a background job never produces. Re-wrap the
	// descriptor as a non-blocking (runtime-poller) file.
	fd, err := syscall.Dup(int(f.Fd()))
	_ = f.Close()
	if err == nil {
		err = syscall.SetNonblock(fd, true)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	syscall.CloseOnExec(fd)
	p := &ptyProc{cmd: cmd, ptmx: os.NewFile(uintptr(fd), "/dev/ptmx"), done: make(chan struct{})}
	go func() {
		_ = cmd.Wait() // always reap - no zombie shells
		close(p.done)
	}()
	return p, nil
}

func (p *ptyProc) Read(b []byte) (int, error)  { return p.ptmx.Read(b) }
func (p *ptyProc) Write(b []byte) (int, error) { return p.ptmx.Write(b) }
func (p *ptyProc) Done() <-chan struct{}       { return p.done }

func (p *ptyProc) control(f func(fd int) error) error {
	sc, err := p.ptmx.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	if err := sc.Control(func(fd uintptr) { inner = f(int(fd)) }); err != nil {
		return err
	}
	return inner
}

// Resize sets the window size (through the poller-safe descriptor; pty's
// own Setsize would flip the file back to blocking).
func (p *ptyProc) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return nil
	}
	return p.control(func(fd int) error {
		return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols})
	})
}

func (p *ptyProc) ExitCode() int {
	select {
	case <-p.done:
	default:
		return -1
	}
	if st := p.cmd.ProcessState; st != nil {
		return st.ExitCode() // -1 when killed by a signal
	}
	return -1
}

// Close hangs up the whole session (Setsid made the shell its leader),
// then forces it after a grace period; the Wait goroutine reaps it. It
// never signals once the shell was reaped (its pid could be reused).
func (p *ptyProc) Close() error {
	p.closeOnce.Do(func() {
		_ = p.ptmx.Close()
		select {
		case <-p.done:
			return
		default:
		}
		pid := p.cmd.Process.Pid
		_ = syscall.Kill(-pid, syscall.SIGHUP)
		go func() {
			select {
			case <-p.done:
			case <-time.After(2 * time.Second):
				_ = p.cmd.Process.Kill() // os.Process guards against a reaped pid
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
	})
	return nil
}

// Info reports the foreground process of the terminal (e.g. vim while it
// runs, else the shell): its command name and working directory.
func (p *ptyProc) Info() termsession.ProcInfo {
	pid := 0
	_ = p.control(func(fd int) error {
		pg, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
		if err == nil && pg > 0 {
			pid = pg
		}
		return err
	})
	if pid <= 0 && p.cmd.Process != nil {
		pid = p.cmd.Process.Pid
	}
	var pi termsession.ProcInfo
	if pid <= 0 {
		return pi
	}
	proc := "/proc/" + strconv.Itoa(pid)
	if cwd, err := os.Readlink(proc + "/cwd"); err == nil {
		pi.Cwd = cwd
	}
	if comm, err := os.ReadFile(proc + "/comm"); err == nil {
		pi.Command = strings.TrimSpace(string(comm))
	}
	return pi
}
