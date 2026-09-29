package termsession

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/wsterm"
)

// Session is one terminal that outlives its viewers.
type Session struct {
	m         *Manager
	id        string
	owner     string
	kind      string
	user      string
	container string
	contID    string
	shell     string
	legacy    bool
	created   time.Time

	writeMu sync.Mutex // serialises input from several viewers

	mu         sync.Mutex
	title      string
	proc       Process
	ring       *Ring
	alt        bool   // the program is on the alternate screen (vim, htop...)
	altTail    []byte // last bytes of output, for markers split across reads
	state      string
	exitCode   int
	exitReason string
	killReason string
	exitedAt   time.Time
	lastAct    time.Time
	detachedAt time.Time
	cols, rows uint16
	clients    map[*client]struct{}
	active     *client // the viewer whose size the terminal has
	done       chan struct{}
}

func newSession(m *Manager, id string, spec Spec, cols, rows uint16) *Session {
	now := m.now()
	return &Session{
		m: m, id: id, owner: spec.Owner, kind: spec.Kind, user: spec.User,
		container: spec.Container, contID: spec.ContainerID, shell: spec.Shell, legacy: spec.Legacy,
		created: now, title: spec.Title, ring: NewRing(m.opts.ScrollbackBytes),
		state: StateRunning, lastAct: now, detachedAt: now,
		cols: cols, rows: rows, clients: map[*client]struct{}{},
		done: make(chan struct{}),
	}
}

// ID is the session id.
func (s *Session) ID() string { return s.id }

// Owner is the owning user id.
func (s *Session) Owner() string { return s.owner }

// Done is closed once the session has exited.
func (s *Session) Done() <-chan struct{} { return s.done }

// Title is the current title.
func (s *Session) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

// State is StateRunning or StateExited.
func (s *Session) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Clients is the number of attached viewers.
func (s *Session) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

// Size is the terminal's current size.
func (s *Session) Size() (cols, rows uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

// Container is the name of the container a container session runs in.
func (s *Session) Container() string { return s.container }

// ContainerID is the id of that container.
func (s *Session) ContainerID() string { return s.contID }

// Kind is KindHost or KindContainer.
func (s *Session) Kind() string { return s.kind }

func (s *Session) lastActivity() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAct
}

func (s *Session) started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proc != nil
}

type status struct {
	state                              string
	clients                            int
	exitedAt, lastActivity, detachedAt time.Time
}

func (s *Session) status() status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return status{s.state, len(s.clients), s.exitedAt, s.lastAct, s.detachedAt}
}

// Info is the JSON shape of a session (see the API doc).
type Info struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Kind            string     `json:"kind"`
	User            string     `json:"user,omitempty"`
	Container       string     `json:"container,omitempty"`
	ContainerID     string     `json:"container_id,omitempty"`
	Shell           string     `json:"shell,omitempty"`
	State           string     `json:"state"`
	ExitCode        *int       `json:"exit_code"`
	ExitReason      string     `json:"exit_reason,omitempty"`
	ExitedAt        *time.Time `json:"exited_at"`
	CreatedAt       time.Time  `json:"created_at"`
	LastActivityAt  time.Time  `json:"last_activity_at"`
	Clients         int        `json:"clients"`
	Cols            uint16     `json:"cols"`
	Rows            uint16     `json:"rows"`
	Cwd             string     `json:"cwd,omitempty"`
	Command         string     `json:"command,omitempty"`
	ScrollbackBytes int        `json:"scrollback_bytes"`
	Legacy          bool       `json:"legacy"`
	AttachPath      string     `json:"attach_path,omitempty"`
}

// Info snapshots the session.
func (s *Session) Info() Info {
	s.mu.Lock()
	in := Info{
		ID: s.id, Title: s.title, Kind: s.kind, User: s.user, Container: s.container,
		ContainerID: s.contID, Shell: s.shell, State: s.state, CreatedAt: s.created.UTC(), LastActivityAt: s.lastAct.UTC(),
		Clients: len(s.clients), Cols: s.cols, Rows: s.rows, ScrollbackBytes: s.ring.Len(), Legacy: s.legacy,
	}
	proc := s.proc
	if s.state == StateExited {
		code, at := s.exitCode, s.exitedAt.UTC()
		in.ExitCode, in.ExitedAt, in.ExitReason = &code, &at, s.exitReason
	}
	s.mu.Unlock()
	if proc != nil && in.State == StateRunning {
		pi := proc.Info()
		in.Cwd, in.Command = pi.Cwd, pi.Command
	}
	if base := s.m.opts.AttachBase; base != "" {
		in.AttachPath = strings.TrimRight(base, "/") + "/" + s.id + "/attach"
	}
	return in
}

// MaxTitleLen bounds a session title (in bytes).
const MaxTitleLen = 80

// CleanTitle trims a user-supplied title and strips control characters;
// "" means "invalid/empty".
func CleanTitle(t string) string {
	t = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, t)
	t = strings.TrimSpace(t)
	for len(t) > MaxTitleLen {
		r := []rune(t)
		t = strings.TrimSpace(string(r[:len(r)-1]))
	}
	return t
}

// SetTitle renames the session and tells its viewers.
func (s *Session) SetTitle(t string) {
	s.mu.Lock()
	s.title = t
	s.mu.Unlock()
	s.broadcastInfo()
}

// run wires p to the session and starts its pumps.
func (s *Session) run(p Process) {
	s.mu.Lock()
	s.proc = p
	killed := s.killReason != ""
	s.mu.Unlock()
	readDone := make(chan struct{})
	go s.pumpOutput(p, readDone)
	go s.finish(p, readDone)
	if killed {
		_ = p.Close()
	}
}

func (s *Session) pumpOutput(p Process, readDone chan struct{}) {
	defer close(readDone)
	buf := make([]byte, 32<<10)
	for {
		n, err := p.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			_, _ = s.ring.Write(chunk)
			s.trackAltLocked(chunk)
			s.lastAct = s.m.now()
			for c := range s.clients {
				c.enqueue(frame{kind: frameOutput, data: chunk})
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) finish(p Process, readDone chan struct{}) {
	select {
	case <-readDone:
		// Output ended (EIO): the process is exiting; wait for its status.
		select {
		case <-p.Done():
		case <-time.After(2 * time.Second):
		}
	case <-p.Done():
		// Let buffered output drain, but don't wait on a background job
		// that kept the terminal open after the shell exited.
		select {
		case <-readDone:
		case <-time.After(time.Second):
		}
	}
	_ = p.Close()
	select {
	case <-readDone:
	case <-time.After(3 * time.Second):
	}
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
	}
	code := p.ExitCode()

	s.mu.Lock()
	s.state = StateExited
	s.exitCode = code
	s.exitReason = ReasonExited
	if s.killReason != "" {
		s.exitReason = s.killReason
	}
	s.exitedAt = s.m.now()
	for c := range s.clients {
		c.enqueue(frame{kind: frameExit, code: code, reason: s.exitReason})
	}
	s.mu.Unlock()
	close(s.done)
}

// kill ends the process (no-op once exited).
func (s *Session) kill(reason string) {
	s.mu.Lock()
	if s.state == StateExited || s.killReason != "" {
		s.mu.Unlock()
		return
	}
	s.killReason = reason
	p := s.proc
	s.mu.Unlock()
	if p != nil {
		_ = p.Close()
	}
}

var (
	altOn  = [][]byte{[]byte("\x1b[?1049h"), []byte("\x1b[?1047h"), []byte("\x1b[?47h")}
	altOff = [][]byte{[]byte("\x1b[?1049l"), []byte("\x1b[?1047l"), []byte("\x1b[?47l"), []byte("\x1bc")}
)

const altTailLen = 8

func lastAny(b []byte, seqs [][]byte) int {
	best := -1
	for _, q := range seqs {
		if i := bytes.LastIndex(b, q); i > best {
			best = i
		}
	}
	return best
}

func (s *Session) trackAltLocked(chunk []byte) {
	b := append(append([]byte(nil), s.altTail...), chunk...)
	on, off := lastAny(b, altOn), lastAny(b, altOff)
	if on > off {
		s.alt = true
	} else if off > on {
		s.alt = false
	}
	if len(b) > altTailLen {
		b = b[len(b)-altTailLen:]
	}
	s.altTail = b
}

// ---- viewers ----

const (
	frameOutput = iota
	frameControl
	frameExit
)

type frame struct {
	kind   int
	data   []byte
	code   int
	reason string
}

type client struct {
	ws         *websocket.Conn
	control    bool
	out        chan frame
	cols, rows uint16
	lastActive time.Time

	goneOnce sync.Once
	gone     chan struct{} // closed when the viewer must be dropped
	overflow bool
}

// enqueue never blocks the session: a viewer that can't keep up is
// dropped (it can simply reattach and get the replay).
func (c *client) enqueue(f frame) {
	select {
	case <-c.gone:
		return
	default:
	}
	select {
	case c.out <- f:
	default:
		c.overflow = true
		c.drop()
	}
}

func (c *client) drop() { c.goneOnce.Do(func() { close(c.gone) }) }

// AttachOptions tune one viewer.
type AttachOptions struct {
	// Control enables server->client control TEXT frames (hello, live,
	// session, exit). Off for old clients, which print every TEXT frame.
	Control bool
	// Cols/Rows is the viewer's size (0 = unknown, keep the current one).
	Cols, Rows uint16
}

// Control messages the server sends (TEXT frames, 0x00 + JSON).
type serverControl struct {
	Type        string `json:"type"`
	Session     *Info  `json:"session,omitempty"`
	ReplayBytes *int   `json:"replay_bytes,omitempty"`
	Code        *int   `json:"code,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

func controlFrame(c serverControl) []byte {
	b, _ := json.Marshal(c)
	return append([]byte{wsterm.ControlPrefix}, b...)
}

const (
	pingEvery   = 25 * time.Second // under Cloudflare's 100 s idle cut-off
	readTimeout = 75 * time.Second
	writeWait   = 10 * time.Second
)

// Attach serves ws as a viewer of the session until the viewer leaves or
// the session ends: first the scrollback replay, then live output. Several
// viewers may be attached at once; the terminal takes the size of the one
// that most recently typed or resized.
func (s *Session) Attach(ws *websocket.Conn, o AttachOptions) {
	c := &client{
		ws: ws, control: o.Control, out: make(chan frame, s.m.opts.ClientQueue),
		cols: o.Cols, rows: o.Rows, lastActive: s.m.now(), gone: make(chan struct{}),
	}

	s.mu.Lock()
	replay := s.ring.Snapshot()
	alt := s.alt
	exited := s.state == StateExited
	exitCode, exitReason := s.exitCode, s.exitReason
	var resizeTo [2]uint16
	nudge := false
	if !exited {
		s.clients[c] = struct{}{}
		if o.Cols > 0 && o.Rows > 0 {
			s.active = c
			if o.Cols != s.cols || o.Rows != s.rows {
				s.cols, s.rows = o.Cols, o.Rows
				resizeTo = [2]uint16{o.Cols, o.Rows}
			} else if alt {
				// Same size: a full-screen program won't repaint by itself.
				nudge = true
			}
		}
	}
	proc := s.proc
	s.mu.Unlock()

	if proc != nil && resizeTo[0] > 0 {
		_ = proc.Resize(resizeTo[0], resizeTo[1])
	}
	if !exited {
		s.broadcastInfoExcept(c)
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		s.writeLoop(c, replay, alt, exited, exitCode, exitReason, proc, nudge)
	}()

	if !exited {
		s.readLoop(c, proc)
		s.detach(c)
	}
	c.drop()
	<-writerDone
}

func (s *Session) writeLoop(c *client, replay []byte, alt, exited bool, exitCode int, exitReason string, proc Process, nudge bool) {
	ws := c.ws
	defer ws.Close()
	write := func(mt int, b []byte) bool {
		_ = ws.SetWriteDeadline(time.Now().Add(writeWait))
		return ws.WriteMessage(mt, b) == nil
	}
	closeWith := func(code int, reason string) {
		wsterm.Close(ws, code, reason)
	}
	if c.control {
		info := s.Info()
		n := len(replay)
		if !write(websocket.TextMessage, controlFrame(serverControl{Type: "hello", Session: &info, ReplayBytes: &n})) {
			return
		}
	}
	if alt && len(replay) > 0 {
		// The switch to the alternate screen may have scrolled out of
		// the ring; entering it twice is harmless.
		if !write(websocket.BinaryMessage, []byte("\x1b[?1049h")) {
			return
		}
	}
	for len(replay) > 0 {
		n := min(len(replay), 32<<10)
		if !write(websocket.BinaryMessage, replay[:n]) {
			return
		}
		replay = replay[n:]
	}
	if c.control && !write(websocket.TextMessage, controlFrame(serverControl{Type: "live"})) {
		return
	}
	if exited {
		s.sendExit(c, exitCode, exitReason, write, closeWith)
		return
	}
	if nudge && proc != nil {
		cols, rows := s.Size()
		if rows > 1 {
			_ = proc.Resize(cols, rows-1)
			_ = proc.Resize(cols, rows)
		}
	}

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case f := <-c.out:
			switch f.kind {
			case frameOutput:
				if !write(websocket.BinaryMessage, f.data) {
					return
				}
			case frameControl:
				if c.control && !write(websocket.TextMessage, f.data) {
					return
				}
			case frameExit:
				// Flush output queued before the exit.
				s.sendExit(c, f.code, f.reason, write, closeWith)
				return
			}
		case <-ping.C:
			if ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)) != nil {
				return
			}
		case <-c.gone:
			if c.overflow {
				closeWith(websocket.CloseTryAgainLater, "viewer too slow, reattach")
			} else {
				closeWith(websocket.CloseNormalClosure, "")
			}
			return
		}
	}
}

func (s *Session) sendExit(c *client, code int, reason string, write func(int, []byte) bool, closeWith func(int, string)) {
	if c.control {
		cc := code
		write(websocket.TextMessage, controlFrame(serverControl{Type: "exit", Code: &cc, Reason: reason}))
		closeWith(websocket.CloseNormalClosure, reason)
		return
	}
	// Old clients: exactly the pre-session behaviour.
	closeWith(websocket.CloseNormalClosure, "")
}

func (s *Session) readLoop(c *client, proc Process) {
	ws := c.ws
	ws.SetReadLimit(1 << 20)
	_ = ws.SetReadDeadline(time.Now().Add(readTimeout))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(readTimeout))
	})
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ws.SetReadDeadline(time.Now().Add(readTimeout))
		select {
		case <-c.gone:
			return
		default:
		}
		input, ctrl := wsterm.ParseClientMessage(mt, data)
		if ctrl != nil {
			if cols, rows, ok := ctrl.Resize(); ok {
				s.resize(c, cols, rows, proc)
			}
			continue
		}
		if len(input) > 0 {
			s.input(c, input, proc)
		}
	}
}

// input makes c the active viewer (taking its size) and types data.
func (s *Session) input(c *client, data []byte, proc Process) {
	s.mu.Lock()
	c.lastActive = s.m.now()
	s.lastAct = c.lastActive
	var to [2]uint16
	if s.active != c {
		s.active = c
		if c.cols > 0 && c.rows > 0 && (c.cols != s.cols || c.rows != s.rows) {
			s.cols, s.rows = c.cols, c.rows
			to = [2]uint16{c.cols, c.rows}
		}
	}
	s.mu.Unlock()
	if to[0] > 0 {
		_ = proc.Resize(to[0], to[1])
	}
	s.writeMu.Lock()
	_, _ = proc.Write(data)
	s.writeMu.Unlock()
}

func (s *Session) resize(c *client, cols, rows uint16, proc Process) {
	s.mu.Lock()
	c.cols, c.rows = cols, rows
	c.lastActive = s.m.now()
	s.active = c
	changed := cols != s.cols || rows != s.rows
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
	if changed {
		_ = proc.Resize(cols, rows)
	}
}

func (s *Session) detach(c *client) {
	s.mu.Lock()
	delete(s.clients, c)
	var to [2]uint16
	if s.active == c {
		s.active = nil
		for o := range s.clients {
			if o.cols > 0 && (s.active == nil || o.lastActive.After(s.active.lastActive)) {
				s.active = o
			}
		}
		if a := s.active; a != nil && (a.cols != s.cols || a.rows != s.rows) {
			s.cols, s.rows = a.cols, a.rows
			to = [2]uint16{a.cols, a.rows}
		}
	}
	if len(s.clients) == 0 {
		s.detachedAt = s.m.now()
	}
	proc := s.proc
	running := s.state == StateRunning
	s.mu.Unlock()
	if running && proc != nil && to[0] > 0 {
		_ = proc.Resize(to[0], to[1])
	}
	if running {
		s.broadcastInfoExcept(nil)
	}
}

func (s *Session) broadcastInfo() { s.broadcastInfoExcept(nil) }

// broadcastInfoExcept sends a "session" update to every control viewer
// (but skip, which gets the same data in its hello).
func (s *Session) broadcastInfoExcept(skip *client) {
	info := s.Info()
	f := frame{kind: frameControl, data: controlFrame(serverControl{Type: "session", Session: &info})}
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		if c != skip && c.control {
			c.enqueue(f)
		}
	}
}
