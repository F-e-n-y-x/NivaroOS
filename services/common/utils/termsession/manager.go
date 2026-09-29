// Package termsession keeps interactive terminals (a host shell on a pty,
// a docker exec) running on the server independently of any WebSocket, so
// a closed tab, a dropped mobile connection or a second device can come
// back to the same shell. Each session keeps a bounded scrollback ring
// that is replayed to every viewer that (re)attaches, then streams live.
//
// Sessions live in the owning service's memory: they end when that
// service restarts. The HTTP/WebSocket contract is documented in
// docs/specs/2026-09-29-terminal-sessions.md.
package termsession

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kinds of session.
const (
	KindHost      = "host"
	KindContainer = "container"
)

// Session states.
const (
	StateRunning = "running"
	StateExited  = "exited"
)

// Why a session ended.
const (
	ReasonExited  = "exited"  // the process ended by itself
	ReasonKilled  = "killed"  // DELETE / Kill
	ReasonTimeout = "timeout" // detached and idle for longer than the timeout
	ReasonEvicted = "evicted" // an old-client session made room for a new one
)

var (
	ErrNotFound = errors.New("terminal session not found")
	ErrLimit    = errors.New("too many terminal sessions")
)

// ProcInfo is best-effort live detail about a session's process.
type ProcInfo struct {
	Cwd     string // working directory of the foreground process
	Command string // name of the foreground process (e.g. "vim")
}

// Process is one running interactive program behind a session.
type Process interface {
	// Read returns output; it fails once the process is gone and its
	// output drained (or after Close).
	Read(p []byte) (int, error)
	// Write sends input.
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	// Done is closed once the process has exited (its output may still be
	// draining). It may simply be closed when Read fails.
	Done() <-chan struct{}
	// ExitCode is the exit status, valid once Read has failed; -1 if unknown
	// or killed by a signal.
	ExitCode() int
	// Close terminates the process (if still running) and releases it; Read
	// must fail promptly afterwards. Idempotent.
	Close() error
	// Info is cheap best-effort detail (may be zero).
	Info() ProcInfo
}

// Spec describes a session being created.
type Spec struct {
	Owner       string // user id the session belongs to
	Kind        string // KindHost / KindContainer
	Title       string // "" = a default name
	User        string // host account the shell runs as (host sessions)
	Container   string // container name (container sessions)
	ContainerID string // container id (container sessions)
	Shell       string // shell path
	// Legacy marks a session opened through an old plain-connect endpoint:
	// it has a shorter detached timeout and may be evicted when the owner
	// is at the session limit.
	Legacy bool
}

// Options bound the manager. Zero fields take the defaults.
type Options struct {
	MaxPerUser            int           // running sessions per user (12)
	MaxTotal              int           // running sessions overall (48)
	ScrollbackBytes       int           // ring size per session (2 MiB)
	DetachedTimeout       time.Duration // kill after this long detached+idle (24h); <0 = never
	LegacyDetachedTimeout time.Duration // same, for Legacy sessions (1h)
	ExitedRetention       time.Duration // keep an exited session listed (10m); at most MaxPerUser per user and MaxTotal overall, oldest dropped first
	ReapInterval          time.Duration // housekeeping period (30s)
	ClientQueue           int           // output frames buffered per viewer (512)
	// AttachBase is the route prefix sessions are served under (e.g.
	// "/v1/sys/terminal-sessions"); it fills Info.AttachPath.
	AttachBase string
}

// DefaultOptions are the limits used when nothing is configured.
func DefaultOptions() Options {
	return Options{
		MaxPerUser:            12,
		MaxTotal:              48,
		ScrollbackBytes:       2 << 20,
		DetachedTimeout:       24 * time.Hour,
		LegacyDetachedTimeout: time.Hour,
		ExitedRetention:       10 * time.Minute,
		ReapInterval:          30 * time.Second,
		ClientQueue:           512,
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.MaxPerUser <= 0 {
		o.MaxPerUser = d.MaxPerUser
	}
	if o.MaxTotal <= 0 {
		o.MaxTotal = d.MaxTotal
	}
	if o.ScrollbackBytes <= 0 {
		o.ScrollbackBytes = d.ScrollbackBytes
	}
	if o.DetachedTimeout == 0 {
		o.DetachedTimeout = d.DetachedTimeout
	}
	if o.LegacyDetachedTimeout == 0 {
		o.LegacyDetachedTimeout = d.LegacyDetachedTimeout
	}
	if o.ExitedRetention <= 0 {
		o.ExitedRetention = d.ExitedRetention
	}
	if o.ReapInterval <= 0 {
		o.ReapInterval = d.ReapInterval
	}
	if o.ClientQueue <= 0 {
		o.ClientQueue = d.ClientQueue
	}
	return o
}

// OptionsFromConfig reads the optional [terminal] section of a service
// config through get (key -> value, "" when unset; nil = defaults), e.g.
//
//	[terminal]
//	MaxSessionsPerUser = 12
//	DetachedTimeout    = 24h     ; Go duration, "0" or "never" = never
//	ScrollbackKB       = 2048
//	ExitedRetention    = 10m
//
// Missing or invalid keys keep the defaults.
func OptionsFromConfig(get func(key string) string) Options {
	o := DefaultOptions()
	if get == nil {
		return o
	}
	g := get
	get = func(k string) string { return strings.TrimSpace(g(k)) }
	if n, err := strconv.Atoi(get("MaxSessionsPerUser")); err == nil && n > 0 && n <= 1000 {
		o.MaxPerUser = n
		if o.MaxTotal < n {
			o.MaxTotal = n
		}
	}
	if n, err := strconv.Atoi(get("MaxSessions")); err == nil && n > 0 && n <= 10000 {
		o.MaxTotal = n
	}
	if n, err := strconv.Atoi(get("ScrollbackKB")); err == nil && n >= 16 && n <= 64<<10 {
		o.ScrollbackBytes = n << 10
	}
	parseDur := func(s string) (time.Duration, bool) {
		if s == "" {
			return 0, false
		}
		if s == "0" || strings.EqualFold(s, "never") {
			return -1, true
		}
		d, err := time.ParseDuration(s)
		if err != nil || d < time.Minute {
			return 0, false
		}
		return d, true
	}
	if d, ok := parseDur(get("DetachedTimeout")); ok {
		o.DetachedTimeout = d
		if d > 0 && d < o.LegacyDetachedTimeout {
			o.LegacyDetachedTimeout = d
		}
	}
	if d, ok := parseDur(get("ExitedRetention")); ok && d > 0 {
		o.ExitedRetention = d
	}
	return o
}

// Manager owns every session of one service.
type Manager struct {
	opts Options
	now  func() time.Time

	mu       sync.Mutex
	sessions map[string]*Session

	stopOnce sync.Once
	stop     chan struct{}
}

// NewManager starts a manager (and its housekeeping goroutine).
func NewManager(opts Options) *Manager {
	m := &Manager{
		opts:     opts.withDefaults(),
		now:      time.Now,
		sessions: map[string]*Session{},
		stop:     make(chan struct{}),
	}
	go m.reapLoop()
	return m
}

// Options returns the effective limits.
func (m *Manager) Options() Options { return m.opts }

// Shutdown stops housekeeping and kills every session.
func (m *Manager) Shutdown() {
	m.stopOnce.Do(func() { close(m.stop) })
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, s := range all {
		s.kill(ReasonKilled)
	}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Create starts a session for spec.Owner by calling start (outside any
// lock) once the limits allow it. cols/rows is the initial size.
func (m *Manager) Create(spec Spec, cols, rows uint16, start func(cols, rows uint16) (Process, error)) (*Session, error) {
	if cols == 0 || rows == 0 {
		cols, rows = 120, 32
	}
	var evict *Session
	m.mu.Lock()
	m.pruneExitedLocked()
	running, total := 0, 0
	var oldestLegacy *Session
	for _, s := range m.sessions {
		if s.State() != StateRunning {
			continue
		}
		total++
		if s.owner != spec.Owner {
			continue
		}
		running++
		if s.legacy && s.Clients() == 0 && (oldestLegacy == nil || s.lastActivity().Before(oldestLegacy.lastActivity())) {
			oldestLegacy = s
		}
	}
	if running >= m.opts.MaxPerUser || total >= m.opts.MaxTotal {
		// An old client can't show or manage sessions, so never lock it out:
		// make room by ending its own oldest detached old-client session.
		if !spec.Legacy || oldestLegacy == nil {
			m.mu.Unlock()
			return nil, ErrLimit
		}
		evict = oldestLegacy
		delete(m.sessions, evict.id)
	}
	autoTitle := spec.Title == ""
	if autoTitle {
		spec.Title = m.defaultTitleLocked(spec)
	}
	// Reserve the id so concurrent creates count this one.
	s := newSession(m, newID(), spec, cols, rows)
	m.sessions[s.id] = s
	m.mu.Unlock()

	if evict != nil {
		evict.kill(ReasonEvicted)
	}
	p, err := start(cols, rows)
	if err != nil {
		m.mu.Lock()
		delete(m.sessions, s.id)
		m.mu.Unlock()
		return nil, err
	}
	if sr, ok := p.(ShellReporter); ok {
		if sh := sr.ShellPath(); sh != "" {
			s.mu.Lock()
			s.shell = sh
			if spec.Kind == KindContainer && autoTitle {
				spec.Shell = sh
				s.title = m.containerTitle(spec)
			}
			s.mu.Unlock()
		}
	}
	s.run(p)
	return s, nil
}

// ShellReporter is implemented by a Process that knows which shell it
// ended up running (e.g. the default shell picked inside a container).
type ShellReporter interface{ ShellPath() string }

func (m *Manager) defaultTitleLocked(spec Spec) string {
	if spec.Kind == KindContainer {
		return m.containerTitle(spec)
	}
	used := map[string]bool{}
	for _, s := range m.sessions {
		if s.owner == spec.Owner {
			used[s.Title()] = true
		}
	}
	for n := 1; ; n++ {
		t := "Terminal " + strconv.Itoa(n)
		if !used[t] {
			return t
		}
	}
}

func (m *Manager) containerTitle(spec Spec) string {
	shell := spec.Shell
	if i := strings.LastIndexByte(shell, '/'); i >= 0 {
		shell = shell[i+1:]
	}
	if shell == "" {
		return spec.Container
	}
	return spec.Container + " (" + shell + ")"
}

// Get returns owner's session id.
func (m *Manager) Get(owner, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.owner != owner || !s.started() {
		return nil, ErrNotFound
	}
	return s, nil
}

// List returns owner's sessions, newest first.
func (m *Manager) List(owner string) []*Session {
	m.mu.Lock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s.owner == owner && s.started() {
			out = append(out, s)
		}
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].created.Equal(out[j].created) {
			return out[i].created.After(out[j].created)
		}
		return out[i].id < out[j].id
	})
	return out
}

// Kill ends owner's session id (if running) and removes it from the list.
func (m *Manager) Kill(owner, id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok || s.owner != owner {
		m.mu.Unlock()
		return ErrNotFound
	}
	delete(m.sessions, id)
	m.mu.Unlock()
	s.kill(ReasonKilled)
	return nil
}

// pruneExited forgets the oldest exited sessions beyond MaxPerUser per user
// and MaxTotal overall, so shells that keep ending by themselves can't pile
// up for the whole retention period.
func (m *Manager) pruneExited() {
	m.mu.Lock()
	m.pruneExitedLocked()
	m.mu.Unlock()
}

func (m *Manager) pruneExitedLocked() {
	type ended struct {
		id, owner string
		at        time.Time
	}
	var all []ended
	for id, s := range m.sessions {
		if st := s.status(); st.state == StateExited {
			all = append(all, ended{id, s.owner, st.exitedAt})
		}
	}
	if len(all) <= m.opts.MaxPerUser && len(all) <= m.opts.MaxTotal {
		return
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].at.Equal(all[j].at) {
			return all[i].at.After(all[j].at) // newest first
		}
		return all[i].id < all[j].id
	})
	perUser := map[string]int{}
	kept := 0
	for _, e := range all {
		if perUser[e.owner] >= m.opts.MaxPerUser || kept >= m.opts.MaxTotal {
			delete(m.sessions, e.id)
			continue
		}
		perUser[e.owner]++
		kept++
	}
}

func (m *Manager) reapLoop() {
	t := time.NewTicker(m.opts.ReapInterval)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			m.Reap()
		}
	}
}

// Reap ends sessions that were detached and idle for longer than their
// timeout and forgets exited sessions past their retention.
func (m *Manager) Reap() {
	now := m.now()
	var timedOut []*Session
	m.mu.Lock()
	for id, s := range m.sessions {
		st := s.status()
		if st.state == StateExited {
			if now.Sub(st.exitedAt) >= m.opts.ExitedRetention {
				delete(m.sessions, id)
			}
			continue
		}
		timeout := m.opts.DetachedTimeout
		if s.legacy {
			timeout = m.opts.LegacyDetachedTimeout
		}
		if timeout < 0 || st.clients > 0 {
			continue
		}
		idleSince := st.detachedAt
		if st.lastActivity.After(idleSince) {
			idleSince = st.lastActivity
		}
		if now.Sub(idleSince) >= timeout {
			// It stays listed (as exited, reason "timeout") for the
			// retention period so the user can see what happened.
			timedOut = append(timedOut, s)
		}
	}
	m.pruneExitedLocked()
	m.mu.Unlock()
	for _, s := range timedOut {
		s.kill(ReasonTimeout)
	}
}
