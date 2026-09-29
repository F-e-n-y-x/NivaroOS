package v1

// Streams over the phone's reverse WebSocket tunnel (S-04).
//
// When the server can't open a connection to the phone (it's away from home
// and has no Tailscale), every file request - listing, download with Range,
// upload, rename, delete, mkdir, thumbnails - is carried as an HTTP-shaped
// request over the tunnel the phone already keeps open to the server. The
// phone replays it against its own file server (on loopback) and streams the
// answer back. Many requests share one WebSocket, so each is a stream:
//
//	server -> phone  text   {"action":"http","sid":N,"method":"GET","path":"/download",
//	                          "query":"path=...","headers":{"Range":"bytes=0-"},
//	                          "body":false,"window":W}
//	phone  -> server text   {"type":"http_head","sid":N,"status":206,"headers":{...}}
//	either direction binary [kind u8][sid u32 big-endian][payload]
//	    kind 1 DATA    payload = at most tunnelFrameSize body bytes
//	    kind 2 END     end of that direction's body
//	    kind 3 CREDIT  payload = u32: the receiver consumed that many more bytes
//	    kind 4 RESET   payload = utf-8 reason: abort the stream (cancel)
//
// Flow control is credit based, per stream and per direction: a sender may
// have at most the receiver's window of unacknowledged DATA bytes in flight
// (the server's window rides in the open message, the phone's in its
// register message). The receiver returns CREDIT as its consumer reads, so a
// slow browser (or a slow phone disk) holds the other side back instead of
// filling memory. A receiver that gets more than its window resets the
// stream. Memory per stream is therefore bounded by the window, and per
// tunnel by window * tunnelMaxStreams.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	tunnelFrameData   = 1
	tunnelFrameEnd    = 2
	tunnelFrameCredit = 3
	tunnelFrameReset  = 4

	// tunnelFrameSize is the largest DATA payload either side sends.
	tunnelFrameSize = 64 << 10
	// tunnelDefaultWindow: how much of one response body the server buffers
	// (and so lets the phone send ahead). 1 MiB keeps a 100 ms mobile link
	// at ~10 MB/s while bounding memory.
	tunnelDefaultWindow = 1 << 20
	// tunnelMaxWindow caps what a phone may advertise for itself.
	tunnelMaxWindow = 8 << 20
	// tunnelMaxStreams: concurrent streams per phone (a Files grid of
	// thumbnails opens many at once); more wait for a free slot.
	tunnelMaxStreams = 16
)

// errTunnelClosed: the phone's tunnel went away mid-stream.
var errTunnelClosed = errors.New("the phone's connection to the server closed")

// tunnelStreamReset is a RESET the phone sent (it refused or aborted).
type tunnelStreamReset struct{ reason string }

func (e *tunnelStreamReset) Error() string {
	if e.reason == "" {
		return "the phone aborted the transfer"
	}
	return "the phone aborted the transfer: " + e.reason
}

// tunnelMux carries streams over one phone tunnel. write sends one
// WebSocket message and must be safe for concurrent use (companionTunnel
// serializes it).
type tunnelMux struct {
	write func(binary bool, data []byte) error
	// peerWindow: the phone's receive window for request (upload) bodies.
	peerWindow int
	// window: our receive window per stream for response bodies.
	window int
	// headTimeout: how long the phone may take to start answering.
	headTimeout time.Duration
	// idleTimeout: a stream with no progress (no data, no credit) for this
	// long is aborted.
	idleTimeout time.Duration

	slots chan struct{}

	mu      sync.Mutex
	streams map[uint32]*tunnelStream
	nextID  uint32
	closed  error
}

func newTunnelMux(write func(binary bool, data []byte) error, peerWindow int) *tunnelMux {
	if peerWindow <= 0 {
		peerWindow = tunnelDefaultWindow
	}
	if peerWindow > tunnelMaxWindow {
		peerWindow = tunnelMaxWindow
	}
	return &tunnelMux{
		write:       write,
		peerWindow:  peerWindow,
		window:      tunnelDefaultWindow,
		headTimeout: 30 * time.Second,
		idleTimeout: 60 * time.Second,
		slots:       make(chan struct{}, tunnelMaxStreams),
		streams:     map[uint32]*tunnelStream{},
	}
}

type tunnelHead struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
}

// tunnelStream is one request/response exchange.
type tunnelStream struct {
	id uint32
	m  *tunnelMux

	head chan tunnelHead // buffered 1

	mu sync.Mutex
	// dataCh wakes the body reader, credCh the request-body pump; end is
	// closed once the stream is over (finished or failed).
	dataCh  chan struct{}
	credCh  chan struct{}
	end     chan struct{}
	endOnce sync.Once
	// Response body received and not yet read, bounded by m.window.
	buf      []byte
	eof      bool
	err      error // terminal: reset by the phone, tunnel closed, cancelled
	unacked  int   // bytes read by the consumer but not yet credited back
	sendCred int   // how much request body we may still send
	lastProg time.Time
	done     bool // removed from the mux
}

func frame(kind byte, sid uint32, payload []byte) []byte {
	b := make([]byte, 5+len(payload))
	b[0] = kind
	binary.BigEndian.PutUint32(b[1:5], sid)
	copy(b[5:], payload)
	return b
}

func (m *tunnelMux) open() (*tunnelStream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed != nil {
		return nil, m.closed
	}
	m.nextID++
	if m.nextID == 0 {
		m.nextID = 1
	}
	s := &tunnelStream{
		id: m.nextID, m: m, head: make(chan tunnelHead, 1), sendCred: m.peerWindow, lastProg: time.Now(),
		dataCh: make(chan struct{}, 1), credCh: make(chan struct{}, 1), end: make(chan struct{}),
	}
	m.streams[s.id] = s
	return s, nil
}

func (m *tunnelMux) stream(sid uint32) *tunnelStream {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streams[sid]
}

func (m *tunnelMux) remove(s *tunnelStream) {
	m.mu.Lock()
	if m.streams[s.id] == s {
		delete(m.streams, s.id)
	}
	m.mu.Unlock()
}

// active reports the number of open streams (tests, diagnostics).
func (m *tunnelMux) active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.streams)
}

// closeAll fails every open stream: the tunnel is gone.
func (m *tunnelMux) closeAll(err error) {
	if err == nil {
		err = errTunnelClosed
	}
	m.mu.Lock()
	if m.closed == nil {
		m.closed = err
	}
	streams := m.streams
	m.streams = map[uint32]*tunnelStream{}
	m.mu.Unlock()
	for _, s := range streams {
		s.fail(err, false)
	}
}

// handleHead routes a phone's http_head message.
func (m *tunnelMux) handleHead(msg []byte) {
	var h struct {
		SID uint32 `json:"sid"`
		tunnelHead
	}
	if json.Unmarshal(msg, &h) != nil {
		return
	}
	if s := m.stream(h.SID); s != nil {
		select {
		case s.head <- h.tunnelHead:
		default:
		}
		s.progress()
	}
}

// handleFrame routes one binary frame from the phone. It never blocks: DATA
// is buffered (bounded by the window), everything else just updates state.
func (m *tunnelMux) handleFrame(b []byte) {
	if len(b) < 5 {
		return
	}
	kind, sid, payload := b[0], binary.BigEndian.Uint32(b[1:5]), b[5:]
	s := m.stream(sid)
	if s == nil {
		return // a stream we already finished (late frames after a cancel)
	}
	switch kind {
	case tunnelFrameData:
		s.mu.Lock()
		if s.err == nil && !s.eof {
			if len(s.buf)+len(payload) > m.window || len(payload) > tunnelFrameSize {
				s.mu.Unlock()
				s.fail(errors.New("the phone sent more than the flow-control window allows"), true)
				return
			}
			s.buf = append(s.buf, payload...)
			s.lastProg = time.Now()
		}
		s.mu.Unlock()
		signal(s.dataCh)
	case tunnelFrameEnd:
		s.mu.Lock()
		s.eof = true
		s.lastProg = time.Now()
		s.mu.Unlock()
		signal(s.dataCh)
	case tunnelFrameCredit:
		if len(payload) >= 4 {
			n := int(binary.BigEndian.Uint32(payload[:4]))
			s.mu.Lock()
			s.sendCred += n
			if s.sendCred > m.peerWindow {
				s.sendCred = m.peerWindow
			}
			s.lastProg = time.Now()
			s.mu.Unlock()
			signal(s.credCh)
		}
	case tunnelFrameReset:
		s.fail(&tunnelStreamReset{reason: string(payload)}, false)
	}
}

func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (s *tunnelStream) closeEnd() { s.endOnce.Do(func() { close(s.end) }) }

func (s *tunnelStream) progress() {
	s.mu.Lock()
	s.lastProg = time.Now()
	s.mu.Unlock()
}

// fail ends the stream with err; with notify a RESET tells the phone to
// stop. Idempotent.
func (s *tunnelStream) fail(err error, notify bool) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	already := s.done
	s.done = true
	s.buf = nil
	s.mu.Unlock()
	s.closeEnd()
	s.m.remove(s)
	if notify && !already {
		_ = s.m.write(true, frame(tunnelFrameReset, s.id, []byte(truncate(err.Error(), 200))))
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// finish removes a stream that ended normally.
func (s *tunnelStream) finish() {
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
	s.closeEnd()
	s.m.remove(s)
}

// wait blocks until pred (checked under s.mu) holds, the stream ends, ctx
// ends or no progress was made for idleTimeout. wake is the channel the
// other side signals when pred may have changed.
func (s *tunnelStream) wait(ctx context.Context, wake <-chan struct{}, pred func() bool) error {
	for {
		s.mu.Lock()
		ok, err, last, done := pred(), s.err, s.lastProg, s.done
		s.mu.Unlock()
		if ok {
			return nil
		}
		if err != nil {
			return err
		}
		if done {
			return errTunnelStreamOver
		}
		idle := s.m.idleTimeout - time.Since(last)
		if idle <= 0 {
			return fmt.Errorf("the phone stopped responding (no data for %s)", s.m.idleTimeout)
		}
		t := time.NewTimer(idle)
		select {
		case <-wake:
		case <-s.end:
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
		t.Stop()
	}
}

var errTunnelStreamOver = errors.New("the transfer already ended")

// tunnelRequest is what companionDo hands the mux.
type tunnelRequest struct {
	Method  string
	Path    string // "/download"
	Query   string // encoded
	Headers map[string]string
	Body    io.Reader // nil for none
}

// Do sends one request over the tunnel and returns the phone's response.
// The response body streams; closing it early cancels the transfer on the
// phone. ctx cancels the whole exchange (request body, head, body).
func (m *tunnelMux) Do(ctx context.Context, req tunnelRequest) (*http.Response, error) {
	select {
	case m.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := sync.OnceFunc(func() { <-m.slots })

	s, err := m.open()
	if err != nil {
		release()
		return nil, err
	}
	open := map[string]interface{}{
		"action":  "http",
		"sid":     s.id,
		"method":  req.Method,
		"path":    req.Path,
		"query":   req.Query,
		"headers": req.Headers,
		"body":    req.Body != nil,
		"window":  m.window,
	}
	data, _ := json.Marshal(open)
	if err := m.write(false, data); err != nil {
		s.fail(err, false)
		release()
		return nil, err
	}

	// Watch ctx for the whole exchange: cancel -> RESET.
	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.fail(ctx.Err(), true)
		case <-stopWatch:
		}
	}()
	cleanup := sync.OnceFunc(func() {
		close(stopWatch)
		release()
	})

	if req.Body != nil {
		go s.pumpBody(ctx, req.Body)
	}

	headTimer := time.NewTimer(m.headTimeout)
	defer headTimer.Stop()
	var head tunnelHead
	select {
	case head = <-s.head:
	case <-headTimer.C:
		s.fail(errors.New("the phone didn't answer in time"), true)
		cleanup()
		return nil, errors.New("the phone didn't answer in time")
	case <-ctx.Done():
		s.fail(ctx.Err(), true)
		cleanup()
		return nil, ctx.Err()
	case <-s.end:
		cleanup()
		return nil, s.terminalErr()
	}

	resp := &http.Response{
		StatusCode:    head.Status,
		Status:        strconv.Itoa(head.Status) + " " + http.StatusText(head.Status),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{},
		ContentLength: -1,
	}
	for k, v := range head.Headers {
		resp.Header.Set(k, v)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
			resp.ContentLength = n
		}
	}
	resp.Body = &tunnelBody{s: s, ctx: ctx, cleanup: cleanup}
	return resp, nil
}

func (s *tunnelStream) terminalErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	return errTunnelClosed
}

// pumpBody sends the request body in DATA frames as credit allows.
func (s *tunnelStream) pumpBody(ctx context.Context, body io.Reader) {
	buf := make([]byte, tunnelFrameSize)
	for {
		// Wait for credit before reading, so a stalled phone doesn't make
		// us pull (and hold) more of the source.
		err := s.wait(ctx, s.credCh, func() bool { return s.sendCred > 0 })
		if err == errTunnelStreamOver {
			return // the phone answered before taking the whole body
		}
		if err != nil {
			s.fail(err, true)
			return
		}
		s.mu.Lock()
		want := s.sendCred
		s.mu.Unlock()
		if want > len(buf) {
			want = len(buf)
		}
		n, rerr := body.Read(buf[:want])
		if n > 0 {
			s.mu.Lock()
			s.sendCred -= n
			dead := s.err
			s.mu.Unlock()
			if dead != nil {
				return
			}
			if werr := s.m.write(true, frame(tunnelFrameData, s.id, buf[:n])); werr != nil {
				s.fail(werr, false)
				return
			}
		}
		if rerr == io.EOF {
			_ = s.m.write(true, frame(tunnelFrameEnd, s.id, nil))
			return
		}
		if rerr != nil {
			s.fail(rerr, true)
			return
		}
	}
}

// tunnelBody is a response body read from the stream.
type tunnelBody struct {
	s       *tunnelStream
	ctx     context.Context
	cleanup func()
	closed  bool
}

func (b *tunnelBody) Read(p []byte) (int, error) {
	s := b.s
	if err := s.wait(b.ctx, s.dataCh, func() bool { return len(s.buf) > 0 || s.eof }); err != nil {
		s.fail(err, true)
		b.cleanup()
		return 0, err
	}
	s.mu.Lock()
	if len(s.buf) == 0 && s.eof {
		s.mu.Unlock()
		s.finish()
		b.cleanup()
		return 0, io.EOF
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	if len(s.buf) == 0 {
		s.buf = nil // let the backing array go
	}
	s.unacked += n
	credit := 0
	// Credit in batches (a quarter window) rather than per Read.
	if s.unacked >= s.m.window/4 || (len(s.buf) == 0 && s.unacked > 0) {
		credit, s.unacked = s.unacked, 0
	}
	s.mu.Unlock()
	if credit > 0 {
		var c [4]byte
		binary.BigEndian.PutUint32(c[:], uint32(credit))
		_ = s.m.write(true, frame(tunnelFrameCredit, s.id, c[:]))
	}
	return n, nil
}

// Close before the end cancels the transfer on the phone.
func (b *tunnelBody) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	s := b.s
	s.mu.Lock()
	finished := s.eof && len(s.buf) == 0
	s.mu.Unlock()
	if finished {
		s.finish()
	} else {
		s.fail(errors.New("cancelled"), true)
	}
	b.cleanup()
	return nil
}
