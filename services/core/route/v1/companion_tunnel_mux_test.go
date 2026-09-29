package v1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTunnelPhone is the phone half of the stream protocol, in Go, for
// testing the server's mux without a WebSocket. It serves files from
// memory with Range, takes uploads, obeys (and checks) credit.
type fakeTunnelPhone struct {
	t      *testing.T
	mux    *tunnelMux
	in     chan []byte // messages from the server (kind byte prefix: 't' text, 'b' binary)
	files  map[string][]byte
	window int // our receive window for uploads

	mu        sync.Mutex
	streams   map[uint32]*fakePhoneStream
	resets    int
	maxAhead  int // most unacknowledged bytes we ever had in flight to the server
	uploads   map[string][]byte
	stall     atomic.Bool // stop sending body data (a stuck phone)
	noHead    atomic.Bool // never answer
	ignoreCr  atomic.Bool // send without waiting for credit (misbehaving phone)
	opened    int32
	sentBytes int64
}

type fakePhoneStream struct {
	sid     uint32
	credit  int
	ahead   int
	wake    chan struct{}
	reset   chan struct{}
	upload  *bytes.Buffer
	upDone  chan struct{}
	path    string
	consume int // upload bytes consumed, not yet credited
}

func newFakeTunnelPhone(t *testing.T, window int) *fakeTunnelPhone {
	p := &fakeTunnelPhone{t: t, in: make(chan []byte, 4096), files: map[string][]byte{}, window: window,
		streams: map[uint32]*fakePhoneStream{}, uploads: map[string][]byte{}}
	p.mux = newTunnelMux(func(bin bool, data []byte) error {
		c := make([]byte, len(data)+1)
		c[0] = 't'
		if bin {
			c[0] = 'b'
		}
		copy(c[1:], data)
		p.in <- c
		return nil
	}, window)
	go p.loop()
	t.Cleanup(func() { close(p.in) })
	return p
}

func (p *fakeTunnelPhone) loop() {
	for m := range p.in {
		kind, data := m[0], m[1:]
		if kind == 't' {
			var open struct {
				Action  string            `json:"action"`
				SID     uint32            `json:"sid"`
				Method  string            `json:"method"`
				Path    string            `json:"path"`
				Query   string            `json:"query"`
				Headers map[string]string `json:"headers"`
				Body    bool              `json:"body"`
				Window  int               `json:"window"`
			}
			if json.Unmarshal(data, &open) != nil || open.Action != "http" {
				continue
			}
			atomic.AddInt32(&p.opened, 1)
			s := &fakePhoneStream{sid: open.SID, credit: open.Window, wake: make(chan struct{}, 1), reset: make(chan struct{}), path: open.Query}
			if open.Body {
				s.upload, s.upDone = &bytes.Buffer{}, make(chan struct{})
			}
			p.mu.Lock()
			p.streams[s.sid] = s
			p.mu.Unlock()
			go p.serve(s, open.Method, open.Path, open.Query, open.Headers)
			continue
		}
		if len(data) < 5 {
			continue
		}
		fk, sid, payload := data[0], binary.BigEndian.Uint32(data[1:5]), data[5:]
		p.mu.Lock()
		s := p.streams[sid]
		if s == nil {
			p.mu.Unlock()
			continue
		}
		switch fk {
		case tunnelFrameCredit:
			n := int(binary.BigEndian.Uint32(payload))
			s.credit += n
			s.ahead -= n
			signal(s.wake)
		case tunnelFrameReset:
			p.resets++
			close(s.reset)
			delete(p.streams, sid)
		case tunnelFrameData:
			if s.upload != nil {
				if s.upload.Len()+len(payload)-s.consume > p.window {
					p.t.Errorf("server sent past our upload window")
				}
				s.upload.Write(payload)
				// Consume at once and credit back.
				n := len(payload)
				s.consume += n
				p.mu.Unlock()
				var c [4]byte
				binary.BigEndian.PutUint32(c[:], uint32(n))
				p.mux.handleFrame(frame(tunnelFrameCredit, sid, c[:]))
				continue
			}
		case tunnelFrameEnd:
			if s.upDone != nil {
				close(s.upDone)
			}
		}
		p.mu.Unlock()
	}
}

func (p *fakeTunnelPhone) head(sid uint32, status int, h map[string]string) {
	b, _ := json.Marshal(map[string]interface{}{"type": "http_head", "sid": sid, "status": status, "headers": h})
	p.mux.handleHead(b)
}

func (p *fakeTunnelPhone) serve(s *fakePhoneStream, method, path, query string, h map[string]string) {
	if p.noHead.Load() {
		return
	}
	file := strings.TrimPrefix(query, "path=")
	if method == http.MethodPost && path == "/upload" {
		select {
		case <-s.upDone:
		case <-s.reset:
			return
		}
		p.mu.Lock()
		p.uploads[file] = append([]byte(nil), s.upload.Bytes()...)
		p.mu.Unlock()
		p.head(s.sid, 200, map[string]string{"Content-Type": "application/json"})
		p.send(s, []byte(`{"success":true}`))
		p.mux.handleFrame(frame(tunnelFrameEnd, s.sid, nil))
		return
	}
	data, ok := p.files[file]
	if !ok {
		p.head(s.sid, 404, nil)
		p.send(s, []byte(`{"success":false,"message":"File not found"}`))
		p.mux.handleFrame(frame(tunnelFrameEnd, s.sid, nil))
		return
	}
	status, start, end := 200, 0, len(data)-1
	hdr := map[string]string{"Content-Type": "video/mp4", "Accept-Ranges": "bytes"}
	if r := h["Range"]; strings.HasPrefix(r, "bytes=") {
		fmt.Sscanf(strings.TrimPrefix(r, "bytes="), "%d-%d", &start, &end)
		if end >= len(data) {
			end = len(data) - 1
		}
		status = 206
		hdr["Content-Range"] = fmt.Sprintf("bytes %d-%d/%d", start, end, len(data))
	}
	hdr["Content-Length"] = fmt.Sprint(end - start + 1)
	p.head(s.sid, status, hdr)
	if p.stall.Load() {
		<-s.reset
		return
	}
	if !p.send(s, data[start:end+1]) {
		return
	}
	p.mux.handleFrame(frame(tunnelFrameEnd, s.sid, nil))
}

// send obeys credit (unless ignoreCr), in tunnelFrameSize chunks.
func (p *fakeTunnelPhone) send(s *fakePhoneStream, b []byte) bool {
	for len(b) > 0 {
		p.mu.Lock()
		for s.credit <= 0 && !p.ignoreCr.Load() {
			p.mu.Unlock()
			select {
			case <-s.wake:
			case <-s.reset:
				return false
			case <-time.After(5 * time.Second):
				p.t.Errorf("phone stuck without credit")
				return false
			}
			p.mu.Lock()
		}
		n := tunnelFrameSize
		if n > len(b) {
			n = len(b)
		}
		if !p.ignoreCr.Load() && n > s.credit {
			n = s.credit
		}
		s.credit -= n
		s.ahead += n
		if s.ahead > p.maxAhead {
			p.maxAhead = s.ahead
		}
		p.sentBytes += int64(n)
		p.mu.Unlock()
		select {
		case <-s.reset:
			return false
		default:
		}
		p.mux.handleFrame(frame(tunnelFrameData, s.sid, b[:n]))
		b = b[n:]
	}
	return true
}

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

func TestTunnelMuxDownloadAndRange(t *testing.T) {
	p := newFakeTunnelPhone(t, 256<<10)
	p.files["/v.mp4"] = pattern(5<<20 + 123)

	resp, err := p.mux.Do(context.Background(), tunnelRequest{Method: "GET", Path: "/download", Query: "path=/v.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !bytes.Equal(got, p.files["/v.mp4"]) || resp.ContentLength != int64(len(got)) {
		t.Fatalf("full body: err=%v len=%d cl=%d", err, len(got), resp.ContentLength)
	}

	resp, err = p.mux.Do(context.Background(), tunnelRequest{Method: "GET", Path: "/download", Query: "path=/v.mp4", Headers: map[string]string{"Range": "bytes=1000-1999"}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || !bytes.Equal(got, p.files["/v.mp4"][1000:2000]) || resp.Header.Get("Content-Range") != fmt.Sprintf("bytes 1000-1999/%d", len(p.files["/v.mp4"])) {
		t.Fatalf("range: %d %d %q", resp.StatusCode, len(got), resp.Header.Get("Content-Range"))
	}
	if n := p.mux.active(); n != 0 {
		t.Fatalf("%d streams left open", n)
	}
}

// A consumer that doesn't read holds the phone back: at most one window
// is ever in flight, and memory stays at the window.
func TestTunnelMuxFlowControlBoundsInFlight(t *testing.T) {
	p := newFakeTunnelPhone(t, 256<<10)
	p.mux.window = 256 << 10
	p.files["/big"] = pattern(8 << 20)
	resp, err := p.mux.Do(context.Background(), tunnelRequest{Method: "GET", Path: "/download", Query: "path=/big"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // the phone sends what it may, then waits
	p.mu.Lock()
	sent := p.sentBytes
	p.mu.Unlock()
	if sent > int64(p.mux.window) {
		t.Fatalf("phone sent %d bytes with nobody reading (window %d)", sent, p.mux.window)
	}
	s := p.mux.stream(1)
	s.mu.Lock()
	buffered := len(s.buf)
	s.mu.Unlock()
	if buffered > p.mux.window {
		t.Fatalf("buffered %d > window", buffered)
	}
	// Slow reads still get everything.
	h := sha256.New()
	buf := make([]byte, 7000)
	for {
		n, err := resp.Body.Read(buf)
		h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	want := sha256.Sum256(p.files["/big"])
	if !bytes.Equal(h.Sum(nil), want[:]) {
		t.Fatal("body corrupted")
	}
	if p.maxAhead > p.mux.window {
		t.Fatalf("max in flight %d > window %d", p.maxAhead, p.mux.window)
	}
}

// A phone that ignores credit is cut off, not buffered without bound.
func TestTunnelMuxResetsAPhoneThatOverruns(t *testing.T) {
	p := newFakeTunnelPhone(t, 256<<10)
	p.mux.window = 128 << 10
	p.ignoreCr.Store(true)
	p.files["/big"] = pattern(2 << 20)
	resp, err := p.mux.Do(context.Background(), tunnelRequest{Method: "GET", Path: "/download", Query: "path=/big"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	_, err = io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("overrun accepted")
	}
	waitFor(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.resets == 1 })
}

// Closing the body early (the browser closed the video) cancels the
// transfer on the phone; so does cancelling the context.
func TestTunnelMuxCancel(t *testing.T) {
	p := newFakeTunnelPhone(t, 256<<10)
	p.files["/big"] = pattern(16 << 20)
	resp, err := p.mux.Do(context.Background(), tunnelRequest{Method: "GET", Path: "/download", Query: "path=/big"})
	if err != nil {
		t.Fatal(err)
	}
	io.ReadFull(resp.Body, make([]byte, 100_000))
	resp.Body.Close()
	waitFor(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.resets == 1 })

	ctx, cancel := context.WithCancel(context.Background())
	resp, err = p.mux.Do(ctx, tunnelRequest{Method: "GET", Path: "/download", Query: "path=/big"})
	if err != nil {
		t.Fatal(err)
	}
	io.ReadFull(resp.Body, make([]byte, 100_000))
	cancel()
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("read after cancel: %v", err)
	}
	waitFor(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.resets == 2 })
	waitFor(t, func() bool { return p.mux.active() == 0 && len(p.mux.slots) == 0 })
	p.mu.Lock()
	sent := p.sentBytes
	p.mu.Unlock()
	if sent > 4<<20 {
		t.Fatalf("phone kept sending after cancel: %d bytes", sent)
	}
}

func TestTunnelMuxUploadStreamsWithCredit(t *testing.T) {
	p := newFakeTunnelPhone(t, 128<<10)
	src := pattern(3<<20 + 17)
	resp, err := p.mux.Do(context.Background(), tunnelRequest{Method: "POST", Path: "/upload", Query: "path=/up.bin", Body: bytes.NewReader(src)})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "true") {
		t.Fatalf("upload answer %d %s", resp.StatusCode, body)
	}
	p.mu.Lock()
	got := p.uploads["/up.bin"]
	p.mu.Unlock()
	if !bytes.Equal(got, src) {
		t.Fatalf("uploaded %d of %d bytes", len(got), len(src))
	}
}

func TestTunnelMuxConcurrentStreams(t *testing.T) {
	p := newFakeTunnelPhone(t, 256<<10)
	for i := 0; i < 40; i++ {
		p.files[fmt.Sprintf("/f%d", i)] = pattern(100_000 + i*997)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := p.mux.Do(context.Background(), tunnelRequest{Method: "GET", Path: "/download", Query: fmt.Sprintf("path=/f%d", i)})
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			got, err := io.ReadAll(resp.Body)
			if err != nil || !bytes.Equal(got, p.files[fmt.Sprintf("/f%d", i)]) {
				errs <- fmt.Errorf("stream %d: %v (%d bytes)", i, err, len(got))
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if p.mux.active() != 0 || len(p.mux.slots) != 0 {
		t.Fatalf("leaked: %d streams, %d slots", p.mux.active(), len(p.mux.slots))
	}
}

func TestTunnelMuxTimeoutsAndTunnelLoss(t *testing.T) {
	p := newFakeTunnelPhone(t, 256<<10)
	p.mux.headTimeout = 150 * time.Millisecond
	p.noHead.Store(true)
	if _, err := p.mux.Do(context.Background(), tunnelRequest{Method: "GET", Path: "/download", Query: "path=/x"}); err == nil {
		t.Fatal("no answer, no error")
	}
	p.noHead.Store(false)

	// A phone that stops mid-body hits the idle timeout.
	p.mux.idleTimeout = 200 * time.Millisecond
	p.stall.Store(true)
	p.files["/s"] = pattern(1000)
	resp, err := p.mux.Do(context.Background(), tunnelRequest{Method: "GET", Path: "/download", Query: "path=/s"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := io.ReadAll(resp.Body); err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("stalled phone: %v after %s", err, time.Since(start))
	}
	p.stall.Store(false)

	// The tunnel drops: open streams fail, new ones are refused.
	p.mux.idleTimeout = time.Minute
	p.stall.Store(true)
	resp, err = p.mux.Do(context.Background(), tunnelRequest{Method: "GET", Path: "/download", Query: "path=/s"})
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(50 * time.Millisecond); p.mux.closeAll(nil) }()
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, errTunnelClosed) {
		t.Fatalf("after tunnel loss: %v", err)
	}
	if _, err := p.mux.Do(context.Background(), tunnelRequest{Method: "GET", Path: "/download"}); !errors.Is(err, errTunnelClosed) {
		t.Fatalf("new stream on a closed tunnel: %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
