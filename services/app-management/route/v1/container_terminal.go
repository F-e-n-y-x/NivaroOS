package v1

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/errdefs"
	"github.com/labstack/echo/v5"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/service"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/termsession"
)

// ContainerTerminalBase is where container terminal sessions are served.
const ContainerTerminalBase = "/v1/container/terminal-sessions"

var (
	containerTermOnce sync.Once
	containerTermAPI  *termsession.API
)

// ContainerTerminalAPI is the process-wide container terminal session
// manager and its routes (docs/specs/2026-09-29-terminal-sessions.md).
// Limits come from the optional [terminal] section of the app-management
// config.
func ContainerTerminalAPI() *termsession.API {
	containerTermOnce.Do(func() {
		opts := termsession.OptionsFromConfig(containerTerminalConfigKey)
		opts.AttachBase = ContainerTerminalBase
		containerTermAPI = &termsession.API{
			M:        termsession.NewManager(opts),
			Upgrader: &upgrader,
			Start:    startContainerTerminal,
			Filter: func(ctx *echo.Context, s *termsession.Session) bool {
				want := strings.TrimPrefix(ctx.QueryParam("container"), "/")
				return want == "" || want == s.Container() || want == s.ContainerID() ||
					(len(want) >= 12 && strings.HasPrefix(s.ContainerID(), want))
			},
		}
	})
	return containerTermAPI
}

func containerTerminalConfigKey(key string) string {
	if config.Cfg == nil {
		return ""
	}
	sec, err := config.Cfg.GetSection("terminal")
	if err != nil || sec == nil || !sec.HasKey(key) {
		return ""
	}
	return sec.Key(key).String()
}

func startContainerTerminal(_ *echo.Context, req termsession.CreateRequest) (termsession.Spec, func(cols, rows uint16) (termsession.Process, error), error) {
	name := strings.TrimSpace(req.Container)
	if name == "" {
		return termsession.Spec{}, nil, &termsession.StartError{Status: http.StatusBadRequest, Err: errors.New("container is required")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := service.MyService.Docker().DescribeContainer(ctx, name)
	if err != nil {
		status := http.StatusInternalServerError
		if errdefs.IsNotFound(err) {
			status = http.StatusNotFound
		}
		return termsession.Spec{}, nil, &termsession.StartError{Status: status, Err: errors.New("failed to open container shell: " + err.Error())}
	}
	if info.State == nil || !info.State.Running {
		return termsession.Spec{}, nil, &termsession.StartError{Status: http.StatusConflict, Err: errors.New("failed to open container shell: the container isn't running")}
	}
	spec := termsession.Spec{
		Kind:        termsession.KindContainer,
		Container:   strings.TrimPrefix(info.Name, "/"),
		ContainerID: info.ID,
	}
	return spec, func(cols, rows uint16) (termsession.Process, error) {
		sess, err := service.MyService.Docker().CreateContainerShellSession(info.ID, req.Shell, cols, rows)
		if err != nil {
			if errors.Is(err, service.ErrShellNotAvailable) {
				return nil, &termsession.StartError{Status: http.StatusBadRequest, Err: errors.New("failed to open container shell: " + err.Error())}
			}
			return nil, errors.New("failed to open container shell: " + err.Error())
		}
		return newExecProc(sess), nil
	}, nil
}

// execProc is a docker exec session as a termsession.Process.
type execProc struct {
	sess *service.ContainerShellSession

	readDone  chan struct{}
	readOnce  sync.Once
	closeOnce sync.Once
	closed    chan struct{} // sess.Close has returned (exit code known)
}

func newExecProc(sess *service.ContainerShellSession) *execProc {
	return &execProc{sess: sess, readDone: make(chan struct{}), closed: make(chan struct{})}
}

func (p *execProc) Read(b []byte) (int, error) {
	n, err := p.sess.Conn.Reader.Read(b)
	if err != nil {
		p.readOnce.Do(func() { close(p.readDone) })
	}
	return n, err
}

func (p *execProc) Write(b []byte) (int, error)    { return p.sess.Conn.Conn.Write(b) }
func (p *execProc) Resize(cols, rows uint16) error { return p.sess.Resize(cols, rows) }
func (p *execProc) Done() <-chan struct{}          { return p.readDone }
func (p *execProc) ShellPath() string              { return p.sess.Shell }
func (p *execProc) Info() termsession.ProcInfo     { return termsession.ForegroundInfo(p.sess.HostPid()) }

// Close ends the exec in the background (it can take a few seconds to
// confirm and signal a stubborn process); the stream closes right away.
func (p *execProc) Close() error {
	p.closeOnce.Do(func() {
		_ = p.sess.Conn.CloseWrite()
		p.sess.Conn.Close()
		go func() {
			p.sess.Close()
			close(p.closed)
		}()
	})
	return nil
}

func (p *execProc) ExitCode() int {
	select {
	case <-p.closed:
	case <-time.After(8 * time.Second):
		return -1
	}
	return p.sess.ExitCode()
}
