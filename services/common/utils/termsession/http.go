package termsession

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	modelCommon "github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/wsterm"
)

// CreateRequest is the POST body (and, for the old endpoints, what the
// query string maps to).
type CreateRequest struct {
	Title     string `json:"title"`
	Container string `json:"container"` // container sessions
	Shell     string `json:"shell"`     // container sessions: bash|zsh|fish|ash|dash|sh
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

// StartError carries the HTTP status for a failed start (e.g. 404 for an
// unknown container, 400 for an unknown shell).
type StartError struct {
	Status int
	Err    error
}

func (e *StartError) Error() string { return e.Err.Error() }
func (e *StartError) Unwrap() error { return e.Err }

// Starter validates a create request and returns the session spec (Kind,
// User/Container/Shell; Owner/Title/Legacy are filled in by the API) and
// the function that starts the process at a given size.
type Starter func(ctx *echo.Context, req CreateRequest) (Spec, func(cols, rows uint16) (Process, error), error)

// API serves the session routes on top of a Manager.
type API struct {
	M        *Manager
	Upgrader *websocket.Upgrader
	Start    Starter
	// Filter optionally narrows GET list results (e.g. ?container=).
	Filter func(ctx *echo.Context, s *Session) bool
}

// Register mounts the routes on g, which must be the group at the
// manager's AttachBase, e.g. v1.Group("/sys/terminal-sessions").
func (a *API) Register(g *echo.Group) {
	g.GET("", a.List)
	g.POST("", a.Create)
	g.GET("/:sid", a.Get)
	g.PUT("/:sid", a.Rename)
	g.DELETE("/:sid", a.Delete)
	g.GET("/:sid/attach", a.Attach)
}

// Owner is the authenticated user id (set by the JWT middleware).
func Owner(ctx *echo.Context) string { return ctx.Request().Header.Get("user_id") }

func respond(ctx *echo.Context, status int, data interface{}) error {
	return ctx.JSON(status, modelCommon.Result{Success: status, Message: http.StatusText(status), Data: data})
}

func fail(ctx *echo.Context, status int, msg string) error {
	return ctx.JSON(status, modelCommon.Result{Success: status, Message: msg})
}

// Limits is returned alongside the list so a UI can explain them.
type Limits struct {
	MaxSessions            int   `json:"max_sessions"`
	ScrollbackBytes        int   `json:"scrollback_bytes"`
	DetachedTimeoutSeconds int64 `json:"detached_timeout_seconds"` // -1 = never
	ExitedRetentionSeconds int64 `json:"exited_retention_seconds"`
}

// ListResponse is the GET list payload.
type ListResponse struct {
	Sessions []Info `json:"sessions"`
	Running  int    `json:"running"`
	Limits   Limits `json:"limits"`
}

func secs(d time.Duration) int64 {
	if d < 0 {
		return -1
	}
	return int64(d / time.Second)
}

func (a *API) List(ctx *echo.Context) error {
	owner := Owner(ctx)
	if owner == "" {
		return fail(ctx, http.StatusUnauthorized, "unauthorized")
	}
	o := a.M.Options()
	resp := ListResponse{Sessions: []Info{}, Limits: Limits{
		MaxSessions: o.MaxPerUser, ScrollbackBytes: o.ScrollbackBytes,
		DetachedTimeoutSeconds: secs(o.DetachedTimeout), ExitedRetentionSeconds: secs(o.ExitedRetention),
	}}
	for _, s := range a.M.List(owner) {
		if a.Filter != nil && !a.Filter(ctx, s) {
			continue
		}
		in := s.Info()
		if in.State == StateRunning {
			resp.Running++
		}
		resp.Sessions = append(resp.Sessions, in)
	}
	return respond(ctx, http.StatusOK, resp)
}

func (a *API) lookup(ctx *echo.Context) (*Session, error) {
	owner := Owner(ctx)
	if owner == "" {
		return nil, fail(ctx, http.StatusUnauthorized, "unauthorized")
	}
	s, err := a.M.Get(owner, ctx.Param("sid"))
	if err != nil {
		return nil, fail(ctx, http.StatusNotFound, err.Error())
	}
	return s, nil
}

func (a *API) Get(ctx *echo.Context) error {
	s, err := a.lookup(ctx)
	if s == nil {
		return err
	}
	return respond(ctx, http.StatusOK, s.Info())
}

func clampSize(n int) uint16 {
	if n <= 0 || n > 10000 {
		return 0
	}
	return uint16(n)
}

// create starts a session for the request; the returned status/message
// describe a failure.
func (a *API) create(ctx *echo.Context, req CreateRequest, legacy bool) (*Session, int, error) {
	owner := Owner(ctx)
	if owner == "" {
		return nil, http.StatusUnauthorized, errors.New("unauthorized")
	}
	title := ""
	if req.Title != "" {
		if title = CleanTitle(req.Title); title == "" {
			return nil, http.StatusBadRequest, errors.New("invalid title")
		}
	}
	spec, start, err := a.Start(ctx, req)
	if err != nil {
		var se *StartError
		if errors.As(err, &se) {
			return nil, se.Status, se.Err
		}
		return nil, http.StatusInternalServerError, err
	}
	spec.Owner, spec.Title, spec.Legacy = owner, title, legacy
	cols, rows := clampSize(req.Cols), clampSize(req.Rows)
	if cols == 0 || rows == 0 {
		cols, rows = 120, 32
	}
	s, err := a.M.Create(spec, cols, rows, start)
	var se *StartError
	if errors.As(err, &se) {
		return nil, se.Status, se.Err
	}
	if errors.Is(err, ErrLimit) {
		return nil, http.StatusTooManyRequests, errors.New("too many terminal sessions: close one first (limit " + strconv.Itoa(a.M.Options().MaxPerUser) + ")")
	}
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("failed to start terminal: " + err.Error())
	}
	return s, http.StatusCreated, nil
}

func (a *API) Create(ctx *echo.Context) error {
	var req CreateRequest
	if ctx.Request().ContentLength != 0 {
		if err := ctx.Bind(&req); err != nil {
			return fail(ctx, http.StatusBadRequest, "invalid body")
		}
	}
	s, status, err := a.create(ctx, req, false)
	if err != nil {
		return fail(ctx, status, err.Error())
	}
	return respond(ctx, http.StatusCreated, s.Info())
}

func (a *API) Rename(ctx *echo.Context) error {
	s, err := a.lookup(ctx)
	if s == nil {
		return err
	}
	var body struct {
		Title *string `json:"title"`
	}
	if err := ctx.Bind(&body); err != nil || body.Title == nil {
		return fail(ctx, http.StatusBadRequest, "title is required")
	}
	t := CleanTitle(*body.Title)
	if t == "" {
		return fail(ctx, http.StatusBadRequest, "invalid title")
	}
	s.SetTitle(t)
	return respond(ctx, http.StatusOK, s.Info())
}

func (a *API) Delete(ctx *echo.Context) error {
	owner := Owner(ctx)
	if owner == "" {
		return fail(ctx, http.StatusUnauthorized, "unauthorized")
	}
	if err := a.M.Kill(owner, ctx.Param("sid")); err != nil {
		return fail(ctx, http.StatusNotFound, err.Error())
	}
	return respond(ctx, http.StatusOK, nil)
}

// Attach upgrades to a WebSocket viewer of an existing session.
func (a *API) Attach(ctx *echo.Context) error {
	s, err := a.lookup(ctx)
	if s == nil {
		return err
	}
	cols := clampSize(atoi(ctx.QueryParam("cols")))
	rows := clampSize(atoi(ctx.QueryParam("rows")))
	if cols == 0 || rows == 0 {
		cols, rows = 0, 0
	}
	ws, err := a.Upgrader.Upgrade(ctx.Response(), ctx.Request(), nil)
	if err != nil {
		return nil // Upgrade already wrote the HTTP error
	}
	s.Attach(ws, AttachOptions{Control: true, Cols: cols, Rows: rows})
	return nil
}

// ServeLegacy implements an old plain-connect endpoint: upgrade, create a
// new session from req, attach without control frames. The session keeps
// running after the socket closes and shows up in the list (legacy=true).
func (a *API) ServeLegacy(ctx *echo.Context, req CreateRequest) error {
	ws, err := a.Upgrader.Upgrade(ctx.Response(), ctx.Request(), nil)
	if err != nil {
		return nil
	}
	s, _, err := a.create(ctx, req, true)
	if err != nil {
		wsterm.SendError(ws, err.Error())
		_ = ws.Close()
		return nil
	}
	s.Attach(ws, AttachOptions{Cols: clampSize(req.Cols), Rows: clampSize(req.Rows)})
	return nil
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
