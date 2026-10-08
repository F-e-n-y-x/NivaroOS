package termsession

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"
)

// ---- ring ----

func TestRingKeepsTheNewestBytes(t *testing.T) {
	r := NewRing(8)
	r.Write([]byte("abc"))
	if got := string(r.Bytes()); got != "abc" || r.Wrapped() {
		t.Fatalf("got %q wrapped=%v", got, r.Wrapped())
	}
	r.Write([]byte("defgh"))
	r.Write([]byte("ij"))
	if got := string(r.Bytes()); got != "cdefghij" {
		t.Fatalf("got %q", got)
	}
	if !r.Wrapped() || r.Written() != 10 || r.Len() != 8 {
		t.Fatalf("wrapped=%v written=%d len=%d", r.Wrapped(), r.Written(), r.Len())
	}
	r.Write([]byte("0123456789XYZ")) // bigger than the ring
	if got := string(r.Bytes()); got != "56789XYZ" {
		t.Fatalf("got %q", got)
	}
}

func TestRingSnapshotStartsAtALineOnceWrapped(t *testing.T) {
	r := NewRing(16)
	r.Write([]byte("line one\nline two\nthree\n"))
	// Holds the last 16 bytes, "\nline two\nthree\n": the replay starts
	// after the first newline, never inside a line fragment.
	snap := string(r.Snapshot())
	if snap != "line two\nthree\n" {
		t.Fatalf("snapshot %q (ring %q)", snap, r.Bytes())
	}
	// Not wrapped: everything, verbatim.
	r2 := NewRing(64)
	r2.Write([]byte("partial"))
	if string(r2.Snapshot()) != "partial" {
		t.Fatal(string(r2.Snapshot()))
	}
	// Wrapped with no newline: never starts inside a UTF-8 sequence.
	r3 := NewRing(5)
	r3.Write([]byte("xxéé")) // x x c3 a9 c3 a9 -> keeps "x c3 a9 c3 a9"? cap 5 -> "\xa9\xc3\xa9"... check below
	r3.Write([]byte("é"))
	snap3 := r3.Snapshot()
	if len(snap3) == 0 || snap3[0]&0xC0 == 0x80 {
		t.Fatalf("snapshot starts mid-rune: % x", snap3)
	}
}

// ---- fake process ----

type fakeProc struct {
	outR *io.PipeReader
	outW *io.PipeWriter

	mu     sync.Mutex
	input  bytes.Buffer
	sizes  [][2]uint16
	code   int
	closed bool

	once sync.Once
	done chan struct{}
}

func newFakeProc() *fakeProc {
	r, w := io.Pipe()
	return &fakeProc{outR: r, outW: w, done: make(chan struct{}), code: -1}
}

func (p *fakeProc) Read(b []byte) (int, error) { return p.outR.Read(b) }
func (p *fakeProc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.input.Write(b)
}
func (p *fakeProc) Resize(c, r uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sizes = append(p.sizes, [2]uint16{c, r})
	return nil
}
func (p *fakeProc) Done() <-chan struct{} { return p.done }
func (p *fakeProc) ExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code
}
func (p *fakeProc) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.once.Do(func() { p.outW.Close(); close(p.done) })
	return nil
}
func (p *fakeProc) Info() ProcInfo { return ProcInfo{Cwd: "/tmp", Command: "sh"} }

func (p *fakeProc) emit(s string) { _, _ = p.outW.Write([]byte(s)) }
func (p *fakeProc) exit(code int) {
	p.mu.Lock()
	p.code = code
	p.mu.Unlock()
	p.once.Do(func() { p.outW.Close(); close(p.done) })
}
func (p *fakeProc) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}
func (p *fakeProc) typed() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.input.String()
}
func (p *fakeProc) lastSize() [2]uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sizes) == 0 {
		return [2]uint16{}
	}
	return p.sizes[len(p.sizes)-1]
}

// ---- harness ----

type harness struct {
	t     *testing.T
	m     *Manager
	srv   *httptest.Server
	procs []*fakeProc
	mu    sync.Mutex
}

const base = "/v1/sys/terminal-sessions"

func newHarness(t *testing.T, o Options) *harness {
	t.Helper()
	o.AttachBase = base
	if o.ReapInterval == 0 {
		o.ReapInterval = time.Hour // tests call Reap themselves
	}
	h := &harness{t: t, m: NewManager(o)}
	e := echo.New()
	g := e.Group(base, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// Stand-in for the JWT middleware.
			c.Request().Header.Del("user_id")
			if u := c.QueryParam("as"); u != "" {
				c.Request().Header.Set("user_id", u)
			}
			return next(c)
		}
	})
	api := &API{M: h.m, Upgrader: &websocket.Upgrader{}, Start: func(ctx *echo.Context, req CreateRequest) (Spec, func(uint16, uint16) (Process, error), error) {
		if req.Shell == "nope" {
			return Spec{}, nil, &StartError{Status: http.StatusBadRequest, Err: errors.New("unknown shell")}
		}
		return Spec{Kind: KindHost, User: "alice", Shell: "/bin/sh"}, func(c, r uint16) (Process, error) {
			p := newFakeProc()
			h.mu.Lock()
			h.procs = append(h.procs, p)
			h.mu.Unlock()
			return p, nil
		}, nil
	}}
	api.Register(g)
	e.GET("/legacy", func(c *echo.Context) error {
		c.Request().Header.Set("user_id", c.QueryParam("as"))
		return api.ServeLegacy(c, CreateRequest{Cols: atoi(c.QueryParam("cols")), Rows: atoi(c.QueryParam("rows"))})
	})
	h.srv = httptest.NewServer(e)
	t.Cleanup(func() { h.srv.Close(); h.m.Shutdown() })
	return h
}

func (h *harness) proc(i int) *fakeProc {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.procs[i]
}

func (h *harness) do(method, path, body string) (int, map[string]interface{}) {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (h *harness) create(user string) string {
	h.t.Helper()
	code, out := h.do("POST", base+"?as="+user, `{"cols":100,"rows":30}`)
	if code != http.StatusCreated {
		h.t.Fatalf("create: %d %v", code, out)
	}
	return out["data"].(map[string]interface{})["id"].(string)
}

type viewer struct {
	t  *testing.T
	ws *websocket.Conn
}

func (h *harness) attach(user, id, size string) *viewer {
	h.t.Helper()
	ws, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.srv.URL, "http")+base+"/"+id+"/attach?as="+user+size, nil)
	if err != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		h.t.Fatalf("attach: %v (%d)", err, code)
	}
	h.t.Cleanup(func() { ws.Close() })
	return &viewer{t: h.t, ws: ws}
}

type msg struct {
	control map[string]interface{}
	output  string
	closed  *websocket.CloseError
}

func (v *viewer) next() msg {
	v.t.Helper()
	_ = v.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	mt, data, err := v.ws.ReadMessage()
	if err != nil {
		var ce *websocket.CloseError
		if errors.As(err, &ce) {
			return msg{closed: ce}
		}
		v.t.Fatalf("read: %v", err)
	}
	if mt == websocket.TextMessage && len(data) > 0 && data[0] == 0 {
		m := map[string]interface{}{}
		if err := json.Unmarshal(data[1:], &m); err != nil {
			v.t.Fatalf("bad control %q", data)
		}
		return msg{control: m}
	}
	return msg{output: string(data)}
}

// expectControl skips "session" updates.
func (v *viewer) expectControl(typ string) map[string]interface{} {
	v.t.Helper()
	for {
		m := v.next()
		if m.control != nil && m.control["type"] == "session" && typ != "session" {
			continue
		}
		if m.control == nil || m.control["type"] != typ {
			v.t.Fatalf("want control %q, got %+v", typ, m)
		}
		return m.control
	}
}

// readOutput collects output until it contains want.
func (v *viewer) readOutput(want string) string {
	v.t.Helper()
	var got strings.Builder
	for !strings.Contains(got.String(), want) {
		m := v.next()
		if m.closed != nil {
			v.t.Fatalf("closed while waiting for %q (got %q)", want, got.String())
		}
		if m.control != nil {
			continue
		}
		got.WriteString(m.output)
	}
	return got.String()
}

func (v *viewer) send(s string) {
	v.t.Helper()
	if err := v.ws.WriteMessage(websocket.BinaryMessage, []byte(s)); err != nil {
		v.t.Fatal(err)
	}
}

func (v *viewer) resize(c, r int) {
	v.t.Helper()
	b, _ := json.Marshal(map[string]interface{}{"type": "resize", "cols": c, "rows": r})
	if err := v.ws.WriteMessage(websocket.TextMessage, append([]byte{0}, b...)); err != nil {
		v.t.Fatal(err)
	}
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---- behaviour ----

func TestReattachReplaysScrollbackThenGoesLive(t *testing.T) {
	h := newHarness(t, Options{})
	id := h.create("1")
	p := h.proc(0)
	p.emit("$ echo first\r\nfirst\r\n")
	eventually(t, "output buffered", func() bool { s, _ := h.m.Get("1", id); return s.Info().ScrollbackBytes > 0 })

	v := h.attach("1", id, "&cols=100&rows=30")
	hello := v.expectControl("hello")
	sess := hello["session"].(map[string]interface{})
	if sess["id"] != id || sess["state"] != "running" || sess["attach_path"] != base+"/"+id+"/attach" {
		t.Fatalf("hello %+v", hello)
	}
	if int(hello["replay_bytes"].(float64)) != len("$ echo first\r\nfirst\r\n") {
		t.Fatalf("replay_bytes %v", hello["replay_bytes"])
	}
	v.readOutput("first\r\n")
	v.expectControl("live")
	p.emit("live-line\r\n")
	v.readOutput("live-line")

	// Viewer goes away: the process must keep running.
	v.ws.Close()
	s, _ := h.m.Get("1", id)
	eventually(t, "detach", func() bool { return s.Clients() == 0 })
	if p.isClosed() || s.State() != StateRunning {
		t.Fatal("detaching ended the process")
	}
	p.emit("while-away\r\n")

	v2 := h.attach("1", id, "")
	v2.expectControl("hello")
	got := v2.readOutput("while-away")
	if !strings.Contains(got, "first") || !strings.Contains(got, "live-line") {
		t.Fatalf("replay missing earlier output: %q", got)
	}
	v2.expectControl("live")
	v2.send("ls\r")
	eventually(t, "input", func() bool { return p.typed() == "ls\r" })
}

func TestTwoViewersShareOutputAndTheLastActiveOneSetsTheSize(t *testing.T) {
	h := newHarness(t, Options{})
	id := h.create("1")
	p := h.proc(0)
	a := h.attach("1", id, "&cols=80&rows=24")
	a.expectControl("hello")
	a.expectControl("live")
	eventually(t, "a size", func() bool { return p.lastSize() == [2]uint16{80, 24} })

	b := h.attach("1", id, "&cols=200&rows=50")
	b.expectControl("hello")
	b.expectControl("live")
	eventually(t, "b size", func() bool { return p.lastSize() == [2]uint16{200, 50} })
	// a is told there are now two viewers.
	upd := a.expectControl("session")
	if upd["session"].(map[string]interface{})["clients"].(float64) != 2 {
		t.Fatalf("clients update %+v", upd)
	}

	p.emit("shared\r\n")
	a.readOutput("shared")
	b.readOutput("shared")

	// a types: the terminal goes back to a's size, and a's input arrives.
	a.send("x")
	eventually(t, "a active again", func() bool { return p.lastSize() == [2]uint16{80, 24} })
	b.send("y")
	eventually(t, "b active again", func() bool { return p.lastSize() == [2]uint16{200, 50} })
	eventually(t, "both inputs", func() bool { return p.typed() == "xy" })

	b.resize(150, 40)
	eventually(t, "resize", func() bool { return p.lastSize() == [2]uint16{150, 40} })

	// b leaves: a (the remaining viewer) gets its size back.
	b.ws.Close()
	eventually(t, "falls back to a", func() bool { return p.lastSize() == [2]uint16{80, 24} })
}

func TestExitedSessionStaysListedWithItsOutput(t *testing.T) {
	h := newHarness(t, Options{ExitedRetention: time.Minute})
	now := time.Now()
	var clock sync.Mutex
	h.m.now = func() time.Time { clock.Lock(); defer clock.Unlock(); return now }

	id := h.create("1")
	p := h.proc(0)
	v := h.attach("1", id, "")
	v.expectControl("hello")
	v.expectControl("live")
	p.emit("bye\r\n")
	v.readOutput("bye")
	p.exit(3)
	ex := v.expectControl("exit")
	if ex["code"].(float64) != 3 || ex["reason"] != "exited" {
		t.Fatalf("exit %+v", ex)
	}
	if m := v.next(); m.closed == nil || m.closed.Code != websocket.CloseNormalClosure {
		t.Fatalf("want close 1000, got %+v", m)
	}

	_, out := h.do("GET", base+"?as=1", "")
	data := out["data"].(map[string]interface{})
	list := data["sessions"].([]interface{})
	if len(list) != 1 || data["running"].(float64) != 0 {
		t.Fatalf("list %+v", data)
	}
	e := list[0].(map[string]interface{})
	if e["state"] != "exited" || e["exit_code"].(float64) != 3 {
		t.Fatalf("entry %+v", e)
	}

	// Attaching to it replays the final output, then reports the exit.
	v2 := h.attach("1", id, "")
	if v2.expectControl("hello")["session"].(map[string]interface{})["state"] != "exited" {
		t.Fatal("hello should say exited")
	}
	v2.readOutput("bye")
	v2.expectControl("live")
	v2.expectControl("exit")

	// Gone after the retention period.
	clock.Lock()
	now = now.Add(2 * time.Minute)
	clock.Unlock()
	h.m.Reap()
	if _, err := h.m.Get("1", id); err != ErrNotFound {
		t.Fatal("exited session not reaped")
	}
}

func TestKillEndsTheProcessAndTellsViewers(t *testing.T) {
	h := newHarness(t, Options{})
	id := h.create("1")
	p := h.proc(0)
	v := h.attach("1", id, "")
	v.expectControl("hello")
	v.expectControl("live")

	if code, _ := h.do("DELETE", base+"/"+id+"?as=2", ""); code != http.StatusNotFound {
		t.Fatalf("another user killed it: %d", code)
	}
	if code, _ := h.do("DELETE", base+"/"+id+"?as=1", ""); code != http.StatusOK {
		t.Fatalf("kill: %d", code)
	}
	if ex := v.expectControl("exit"); ex["reason"] != "killed" {
		t.Fatalf("exit %+v", ex)
	}
	if !p.isClosed() {
		t.Fatal("process not closed")
	}
	if code, _ := h.do("GET", base+"/"+id+"?as=1", ""); code != http.StatusNotFound {
		t.Fatal("killed session still listed")
	}
}

func TestSessionLimitsPerUser(t *testing.T) {
	h := newHarness(t, Options{MaxPerUser: 2})
	h.create("1")
	id2 := h.create("1")
	if code, out := h.do("POST", base+"?as=1", ""); code != http.StatusTooManyRequests {
		t.Fatalf("third session: %d %v", code, out)
	}
	h.create("2") // other users have their own budget

	// An exited session no longer counts.
	s, _ := h.m.Get("1", id2)
	h.proc(1).exit(0)
	<-s.Done()
	h.create("1")
}

func TestLegacyConnectCreatesAPersistentSessionAndEvictsOnlyOldLegacyOnes(t *testing.T) {
	h := newHarness(t, Options{MaxPerUser: 2})
	dial := func() *websocket.Conn {
		ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.srv.URL, "http")+"/legacy?as=1&cols=90&rows=20", nil)
		if err != nil {
			t.Fatal(err)
		}
		return ws
	}
	ws := dial()
	eventually(t, "legacy session", func() bool { return len(h.m.List("1")) == 1 })
	p := h.proc(0)
	p.emit("prompt$ ")
	// Old clients get plain output and no control frames.
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	mt, data, err := ws.ReadMessage()
	if err != nil || mt != websocket.BinaryMessage || string(data) != "prompt$ " {
		t.Fatalf("legacy frame %d %q %v", mt, data, err)
	}
	if p.lastSize() != ([2]uint16{}) {
		t.Fatal("legacy session should start at the requested size, not resize")
	}
	ws.Close()
	s := h.m.List("1")[0]
	eventually(t, "detached", func() bool { return s.Clients() == 0 })
	if s.State() != StateRunning || !s.Info().Legacy {
		t.Fatal("legacy session should outlive its socket")
	}

	h.create("1") // explicit session: now at the limit (2)
	ws2 := dial() // an old client must not be locked out...
	defer ws2.Close()
	eventually(t, "eviction", func() bool { return p.isClosed() })
	eventually(t, "new legacy session", func() bool {
		n := 0
		for _, s := range h.m.List("1") {
			if s.State() == StateRunning {
				n++
			}
		}
		return n == 2
	})
	// ...but a new-style create at the limit is refused, never evicting.
	if code, _ := h.do("POST", base+"?as=1", ""); code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", code)
	}
}

func TestDetachedIdleSessionsTimeOut(t *testing.T) {
	h := newHarness(t, Options{DetachedTimeout: time.Hour})
	now := time.Now()
	var clock sync.Mutex
	h.m.now = func() time.Time { clock.Lock(); defer clock.Unlock(); return now }
	advance := func(d time.Duration) { clock.Lock(); now = now.Add(d); clock.Unlock() }

	idle := h.create("1")
	busy := h.create("1")
	watched := h.create("1")
	v := h.attach("1", watched, "")
	v.expectControl("hello")

	advance(50 * time.Minute)
	h.proc(1).emit("still compiling\r\n") // output counts as activity
	sb, _ := h.m.Get("1", busy)
	eventually(t, "output", func() bool { return sb.Info().ScrollbackBytes > 0 })
	advance(20 * time.Minute)
	h.m.Reap()

	si, _ := h.m.Get("1", idle)
	<-si.Done()
	if in := si.Info(); in.State != StateExited || in.ExitReason != ReasonTimeout {
		t.Fatalf("idle session: %+v", in)
	}
	if !h.proc(0).isClosed() || h.proc(1).isClosed() || h.proc(2).isClosed() {
		t.Fatal("only the idle detached session should be ended")
	}
}

func TestAuthAndOwnership(t *testing.T) {
	h := newHarness(t, Options{})
	if code, _ := h.do("GET", base, ""); code != http.StatusUnauthorized {
		t.Fatalf("no user: %d", code)
	}
	if code, _ := h.do("POST", base, ""); code != http.StatusUnauthorized {
		t.Fatalf("no user create: %d", code)
	}
	id := h.create("1")
	if _, out := h.do("GET", base+"?as=2", ""); len(out["data"].(map[string]interface{})["sessions"].([]interface{})) != 0 {
		t.Fatal("user 2 sees user 1's session")
	}
	if code, _ := h.do("GET", base+"/"+id+"?as=2", ""); code != http.StatusNotFound {
		t.Fatal("user 2 can read user 1's session")
	}
	if code, _ := h.do("PUT", base+"/"+id+"?as=2", `{"title":"x"}`); code != http.StatusNotFound {
		t.Fatal("user 2 can rename user 1's session")
	}
	_, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.srv.URL, "http")+base+"/"+id+"/attach?as=2", nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("user 2 attached to user 1's session: %v", err)
	}
	if code, _ := h.do("POST", base+"?as=1", `{"shell":"nope"}`); code != http.StatusBadRequest {
		t.Fatalf("start error status: %d", code)
	}
}

func TestRenameIsBroadcastAndValidated(t *testing.T) {
	h := newHarness(t, Options{})
	id := h.create("1")
	s, _ := h.m.Get("1", id)
	if s.Title() != "Terminal 1" {
		t.Fatalf("default title %q", s.Title())
	}
	id2 := h.create("1")
	if s2, _ := h.m.Get("1", id2); s2.Title() != "Terminal 2" {
		t.Fatalf("default titles should count up, got %q", s2.Title())
	}
	v := h.attach("1", id, "")
	v.expectControl("hello")
	v.expectControl("live")
	if code, _ := h.do("PUT", base+"/"+id+"?as=1", `{"title":"  \u001b[31mbuild\u0007 box  "}`); code != http.StatusOK {
		t.Fatalf("rename %d", code)
	}
	upd := v.expectControl("session")
	if upd["session"].(map[string]interface{})["title"] != "[31mbuild box" {
		t.Fatalf("title %+v", upd)
	}
	if code, _ := h.do("PUT", base+"/"+id+"?as=1", `{"title":"   "}`); code != http.StatusBadRequest {
		t.Fatal("blank title accepted")
	}
}

func TestReplayReentersTheAlternateScreenAndNudgesARepaint(t *testing.T) {
	h := newHarness(t, Options{ScrollbackBytes: 64})
	id := h.create("1")
	p := h.proc(0)
	p.emit("\x1b[?1049h")
	p.emit(strings.Repeat("v", 100) + "\n" + "screen") // pushes the switch out of the ring
	s, _ := h.m.Get("1", id)
	eventually(t, "wrapped", func() bool { return s.Info().ScrollbackBytes == 64 })

	v := h.attach("1", id, "&cols=100&rows=30") // same size as the session
	v.expectControl("hello")
	if m := v.next(); m.output != "\x1b[?1049h" {
		t.Fatalf("want alt-screen prefix, got %+v", m)
	}
	v.readOutput("screen")
	v.expectControl("live")
	eventually(t, "nudge", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		n := len(p.sizes)
		return n >= 2 && p.sizes[n-2] == [2]uint16{100, 29} && p.sizes[n-1] == [2]uint16{100, 30}
	})
}

func cfg(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestOptionsFromConfig(t *testing.T) {
	o := OptionsFromConfig(cfg(map[string]string{"MaxSessionsPerUser": "5", "DetachedTimeout": "2h", "ScrollbackKB": " 512 "}))
	if o.MaxPerUser != 5 || o.DetachedTimeout != 2*time.Hour || o.ScrollbackBytes != 512<<10 || o.LegacyDetachedTimeout != time.Hour {
		t.Fatalf("%+v", o)
	}
	o2 := OptionsFromConfig(cfg(map[string]string{"DetachedTimeout": "never", "ScrollbackKB": "1"}))
	if o2.DetachedTimeout != -1 || o2.ScrollbackBytes != 2<<20 {
		t.Fatalf("%+v", o2)
	}
	if d := OptionsFromConfig(nil); d != DefaultOptions() {
		t.Fatalf("%+v", d)
	}
}

func TestForegroundInfoFollowsTheTerminalsForegroundJob(t *testing.T) {
	root := t.TempDir()
	old := procRoot
	procRoot = root
	defer func() { procRoot = old }()
	mk := func(pid, stat, comm, cwd string) {
		d := root + "/" + pid
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(d+"/stat", []byte(stat), 0o644)
		os.WriteFile(d+"/comm", []byte(comm+"\n"), 0o644)
		os.Symlink(cwd, d+"/cwd")
	}
	// The shell (comm with a space and a paren) whose terminal's foreground
	// group is 200.
	mk("100", "100 (ba sh)) S 1 100 100 34816 200 4194560 0 0", "bash", "/home/a")
	mk("200", "200 (vim) S 100 200 100 34816 200 0", "vim", "/srv/app")
	if got := ForegroundInfo(100); got != (ProcInfo{Cwd: "/srv/app", Command: "vim"}) {
		t.Fatalf("got %+v", got)
	}
	// The foreground job vanished: fall back to the shell.
	os.RemoveAll(root + "/200")
	if got := ForegroundInfo(100); got != (ProcInfo{Cwd: "/home/a", Command: "bash"}) {
		t.Fatalf("got %+v", got)
	}
	if got := ForegroundInfo(0); got != (ProcInfo{}) {
		t.Fatalf("got %+v", got)
	}
}

type shellFake struct{ *fakeProc }

func (shellFake) ShellPath() string { return "/bin/ash" }

func TestContainerSessionTitleUsesTheShellItStarted(t *testing.T) {
	m := NewManager(Options{})
	defer m.Shutdown()
	s, err := m.Create(Spec{Owner: "1", Kind: KindContainer, Container: "jellyfin", ContainerID: "abc"}, 80, 24, func(c, r uint16) (Process, error) {
		return shellFake{newFakeProc()}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	in := s.Info()
	if in.Title != "jellyfin (ash)" || in.Shell != "/bin/ash" || in.ContainerID != "abc" || in.Kind != KindContainer {
		t.Fatalf("%+v", in)
	}
}

func TestRingGrowsWithItsOutputAndShrinks(t *testing.T) {
	r := NewRing(1 << 20)
	if r.Footprint() != 0 || r.Cap() != 1<<20 {
		t.Fatalf("empty ring holds %d bytes (cap %d)", r.Footprint(), r.Cap())
	}
	r.Write([]byte("hello\n"))
	if f := r.Footprint(); f > 8<<10 {
		t.Fatalf("a quiet ring holds %d bytes", f)
	}
	big := bytes.Repeat([]byte("0123456789abcde\n"), 100<<10) // 1.6 MiB
	r.Write(big)
	if r.Len() != 1<<20 || r.Footprint() != 1<<20 || !r.Wrapped() {
		t.Fatalf("len=%d footprint=%d wrapped=%v", r.Len(), r.Footprint(), r.Wrapped())
	}
	want := big[len(big)-1000:]
	r.Shrink(1000)
	if got := r.Bytes(); !bytes.Equal(got, want) || r.Footprint() != 1000 {
		t.Fatalf("after shrink: %d bytes, footprint %d", len(got), r.Footprint())
	}
	if s := r.Snapshot(); len(s) == 0 || s[0] == '\n' {
		t.Fatalf("snapshot after shrink %q", s)
	}
}

func TestExitedSessionsAreBounded(t *testing.T) {
	m := NewManager(Options{MaxPerUser: 3, MaxTotal: 5, ReapInterval: time.Hour})
	defer m.Shutdown()
	big := strings.Repeat("x", 600<<10) + "\n"
	for i := 0; i < 20; i++ {
		owner := "1"
		if i%2 == 1 {
			owner = "2"
		}
		if i >= 16 {
			owner = "3"
		}
		p := newFakeProc()
		s, err := m.Create(Spec{Owner: owner, Kind: KindHost}, 80, 24, func(c, r uint16) (Process, error) { return p, nil })
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		go p.emit(big)
		time.Sleep(5 * time.Millisecond)
		p.exit(0)
		<-s.Done()
	}
	m.mu.Lock()
	total, perUser, held := 0, map[string]int{}, 0
	for _, s := range m.sessions {
		total++
		perUser[s.owner]++
		s.mu.Lock()
		held += s.ring.Footprint()
		s.mu.Unlock()
	}
	m.mu.Unlock()
	if total > 5 {
		t.Fatalf("%d exited sessions kept", total)
	}
	for u, n := range perUser {
		if n > 3 {
			t.Fatalf("user %s keeps %d exited sessions", u, n)
		}
	}
	if held > 5*exitedScrollbackBytes {
		t.Fatalf("exited sessions hold %d bytes", held)
	}
	if n := len(m.List("3")); n != 3 {
		t.Fatalf("newest user keeps %d exited sessions, want 3", n)
	}
}
