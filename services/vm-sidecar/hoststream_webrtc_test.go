package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEncodeSize(t *testing.T) {
	for _, c := range []struct {
		maxW, w, h, ow, oh int
	}{
		{1920, 3398, 1220, 1920, 688}, // <= maxW wide, even
		{1920, 1280, 720, 1280, 720},
		{1280, 3398, 1220, 1280, 458},
		{0, 3398, 1220, 3398, 1220}, // native
		{0, 1365, 767, 1364, 766},   // odd: even
	} {
		if w, h := encodeSize(c.maxW, c.w, c.h); w != c.ow || h != c.oh {
			t.Errorf("max %d %dx%d -> %dx%d, want %dx%d", c.maxW, c.w, c.h, w, h, c.ow, c.oh)
		}
	}
}

func TestResolvePreset(t *testing.T) {
	for _, c := range []struct {
		enc, preset string
		fps, kbps   int
		want        presetSpec
	}{
		{"nvh264enc", "low", 0, 0, presetSpec{24, 2500, 1280}},
		{"x264enc", "low", 0, 0, presetSpec{20, 2000, 1280}},
		{"vah264enc", "balanced", 0, 0, presetSpec{30, 8000, 1920}},
		{"x264enc", "balanced", 0, 0, presetSpec{30, 8000, 1920}},
		{"nvh264enc", "high", 0, 0, presetSpec{60, 25000, 0}},
		{"x264enc", "high", 0, 0, presetSpec{30, 20000, 0}},
		{"x264enc", "high", 60, 0, presetSpec{30, 20000, 0}},      // x264 tops out at 30
		{"nvh264enc", "low", 0, 4000, presetSpec{24, 4000, 1280}}, // override
		// custom (no preset, an older client): the raw values, as before
		{"nvh264enc", "", 60, 12000, presetSpec{60, 12000, 0}},
		{"x264enc", "", 60, 8000, presetSpec{30, 8000, 1920}},
	} {
		if got := resolvePreset(c.enc, c.preset, c.fps, c.kbps); got != c.want {
			t.Errorf("%s %q %d/%d: %+v, want %+v", c.enc, c.preset, c.fps, c.kbps, got, c.want)
		}
	}
}

// argv: the gst-launch line one encoder run gets for a preset.
func argv(enc, preset string, w, h int) string {
	sp := resolvePreset(enc, preset, 0, 0)
	return strings.Join(x11Argv(":5", encParams{Encoder: enc, Preset: preset, FPS: sp.FPS, Kbps: sp.Kbps, MaxW: sp.MaxW,
		W: w, H: h, Seq: -1, TS: -1, Port: 5004}), " ")
}

func TestPresetArgv(t *testing.T) {
	for _, c := range []struct {
		enc, preset string
		want        []string
		not         []string
	}{
		{"x264enc", "low", []string{"framerate=20/1", "width=1280,height=458", "x264enc tune=zerolatency speed-preset=veryfast bitrate=2000",
			"key-int-max=40", "profile=main"}, nil},
		{"x264enc", "balanced", []string{"framerate=30/1", "width=1920,height=688", "speed-preset=ultrafast bitrate=8000", "profile=constrained-baseline"}, nil},
		{"x264enc", "high", []string{"framerate=30/1", "speed-preset=ultrafast bitrate=20000"}, []string{"videoscale"}},
		{"nvh264enc", "low", []string{"framerate=24/1", "width=1280,height=458", "nvh264enc preset=p4 tune=ultra-low-latency rc-mode=vbr const-quality=28",
			"max-bitrate=2500", "gop-size=48"}, []string{"spatial-aq"}},
		{"nvh264enc", "balanced", []string{"framerate=30/1", "width=1920,height=688", "preset=p1", "const-quality=23", "max-bitrate=8000"}, nil},
		{"nvh264enc", "high", []string{"framerate=60/1", "preset=p4", "const-quality=19", "max-bitrate=25000", "gop-size=120", "spatial-aq=true"}, []string{"videoscale"}},
		{"vah264enc", "low", []string{"framerate=24/1", "target-usage=4", "bitrate=2500"}, nil},
		{"vah264enc", "balanced", []string{"target-usage=7", "bitrate=8000"}, nil},
	} {
		a := argv(c.enc, c.preset, 3398, 1220)
		for _, w := range c.want {
			if !strings.Contains(a, w) {
				t.Errorf("%s %s lacks %q:\n%s", c.enc, c.preset, w, a)
			}
		}
		for _, w := range c.not {
			if strings.Contains(a, w) {
				t.Errorf("%s %s has %q:\n%s", c.enc, c.preset, w, a)
			}
		}
	}
}

func TestGstArgv(t *testing.T) {
	p := encParams{Encoder: "x264enc", FPS: 30, Kbps: 8000, MaxW: 1920, W: 3398, H: 1220, Seq: -1, TS: -1, Port: 5004}
	a := strings.Join(x11Argv(":5", p), " ")
	for _, want := range []string{
		"ximagesrc display-name=:5 use-damage=false show-pointer=false ! video/x-raw,framerate=30/1",
		"videoscale n-threads=0 ! video/x-raw,width=1920,height=688,pixel-aspect-ratio=1/1",
		"x264enc tune=zerolatency speed-preset=ultrafast bitrate=8000",
		"key-int-max=60", // 2 s at 30 fps
		"profile=constrained-baseline",
		"rtph264pay config-interval=-1 pt=96 mtu=1200 aggregate-mode=zero-latency ! udpsink host=127.0.0.1 port=5004",
	} {
		if !strings.Contains(a, want) {
			t.Errorf("argv lacks %q:\n%s", want, a)
		}
	}
	if strings.Contains(a, "seqnum-offset") || strings.Contains(a, "timestamp-offset") {
		t.Errorf("first run must leave RTP offsets random: %s", a)
	}

	p = encParams{Encoder: "nvh264enc", FPS: 60, Kbps: 12000, W: 1920, H: 1080, Seq: 7, TS: 9000, Port: 1}
	a = strings.Join(x11Argv(":0", p), " ")
	for _, want := range []string{"framerate=60/1", "cudaupload ! cudaconvert ! nvh264enc preset=p1 tune=ultra-low-latency rc-mode=vbr",
		"max-bitrate=12000", "gop-size=120", "seqnum-offset=7", "timestamp-offset=9000"} {
		if !strings.Contains(a, want) {
			t.Errorf("argv lacks %q:\n%s", want, a)
		}
	}
	if strings.Contains(a, "videoscale") {
		t.Errorf("GPU at an even size must not scale: %s", a)
	}
	// GPU scaling when the GStreamer has it.
	p = encParams{Encoder: "nvh264enc", Preset: "balanced", FPS: 30, Kbps: 8000, MaxW: 1920, CudaScale: true, W: 3398, H: 1220, Seq: -1, TS: -1, Port: 1}
	a = strings.Join(x11Argv(":0", p), " ")
	if !strings.Contains(a, "cudaupload ! cudaconvertscale ! video/x-raw(memory:CUDAMemory),width=1920,height=688,pixel-aspect-ratio=1/1 ! nvh264enc") ||
		strings.Contains(a, "videoscale") {
		t.Errorf("GPU scale: %s", a)
	}

	w := strings.Join(waylandSource(42, 30), " ")
	if w != "pipewiresrc path=42 keepalive-time=500 ! videorate ! video/x-raw,framerate=30/1" {
		t.Errorf("wayland source: %s", w)
	}
}

func TestPickEncoder(t *testing.T) {
	all := map[string]bool{"nvh264enc": true, "cudaupload": true, "cudaconvert": true, "vah264enc": true, "x264enc": true}
	if e := pickEncoder(all, nil); e != "nvh264enc" {
		t.Errorf("got %s", e)
	}
	if e := pickEncoder(all, map[string]bool{"nvh264enc": true}); e != "vah264enc" {
		t.Errorf("broken nvenc: got %s", e)
	}
	noCuda := map[string]bool{"nvh264enc": true, "x264enc": true}
	if e := pickEncoder(noCuda, nil); e != "x264enc" {
		t.Errorf("nvenc without cudaupload: got %s", e)
	}
	if e := pickEncoder(map[string]bool{}, nil); e != "" {
		t.Errorf("nothing: got %s", e)
	}
}

func TestInputToRFB(t *testing.T) {
	if b := inputToRFB(dcMsg{T: "p", X: 300, Y: 70000, M: 5}); !bytes.Equal(b, []byte{5, 5, 1, 44, 0xff, 0xff}) {
		t.Errorf("pointer: % x", b)
	}
	if b := inputToRFB(dcMsg{T: "p", X: -4}); !bytes.Equal(b, []byte{5, 0, 0, 0, 0, 0}) {
		t.Errorf("negative pointer: % x", b)
	}
	if b := inputToRFB(dcMsg{T: "k", KS: 0xffe3, D: true}); !bytes.Equal(b, []byte{4, 1, 0, 0, 0, 0, 0xff, 0xe3}) {
		t.Errorf("key: % x", b)
	}
	if b := inputToRFB(dcMsg{T: "clip", Text: "é€"}); !bytes.Equal(b, []byte{6, 0, 0, 0, 0, 0, 0, 2, 0xe9, '?'}) {
		t.Errorf("clip: % x", b)
	}
	if inputToRFB(dcMsg{T: "bogus"}) != nil {
		t.Error("unknown type must send nothing")
	}
}

// fakeVNC: a minimal RFB 3.8 server on a unix socket that records what the
// client sent after the handshake.
type fakeVNC struct {
	path string
	mu   sync.Mutex
	got  []byte
	conn net.Conn
}

func startFakeVNC(t *testing.T, w, h int) *fakeVNC { return startFakeVNCDropping(t, w, h, 0) }

// startFakeVNCDropping: the first drop connections are closed at once, the
// way x11vnc -unixsock sometimes drops one right after a client left
// ("accept_unix: accept: Invalid argument").
func startFakeVNCDropping(t *testing.T, w, h, drop int) *fakeVNC {
	f := &fakeVNC{path: t.TempDir() + "/vnc.sock"}
	ln, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		for ; err == nil && drop > 0; drop-- {
			c.Close()
			c, err = ln.Accept()
		}
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conn = c
		f.mu.Unlock()
		c.Write([]byte("RFB 003.008\n"))
		buf := make([]byte, 12)
		c.Read(buf)
		c.Write([]byte{1, 1})
		c.Read(buf[:1])
		c.Write([]byte{0, 0, 0, 0})
		c.Read(buf[:1])
		init := binary.BigEndian.AppendUint16(nil, uint16(w))
		init = binary.BigEndian.AppendUint16(init, uint16(h))
		init = append(init, make([]byte, 16)...)
		init = append(init, 0, 0, 0, 4, 't', 'e', 's', 't')
		c.Write(init)
		b := make([]byte, 4096)
		for {
			n, err := c.Read(b)
			if err != nil {
				return
			}
			f.mu.Lock()
			f.got = append(f.got, b[:n]...)
			f.mu.Unlock()
		}
	}()
	return f
}

func (f *fakeVNC) received() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.got)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestRFBInputReleasedOnClose(t *testing.T) {
	f := startFakeVNC(t, 1920, 1080)
	c, err := dialHostRFB(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if c.w != 1920 || c.h != 1080 {
		t.Fatalf("ServerInit size %dx%d", c.w, c.h)
	}
	s := &webrtcSession{rfb: c}
	s.onInput([]byte(`{"t":"k","ks":65507,"d":true}`))
	s.onInput([]byte(`{"t":"p","x":10,"y":20,"m":1}`))
	c.close()
	// SetPixelFormat(20) + SetEncodings(4+8), the two inputs, then the
	// releases the tracker adds on close.
	want := []byte{4, 1, 0, 0, 0, 0, 0xff, 0xe3, 5, 1, 0, 10, 0, 20, 4, 0, 0, 0, 0, 0, 0xff, 0xe3, 5, 0, 0, 10, 0, 20}
	waitFor(t, "release on close", func() bool { return bytes.HasSuffix(f.received(), want) })
	if got := f.received(); len(got) != 32+len(want) {
		t.Errorf("unexpected bytes: % x", got)
	}
}

// Owner bug (Firefox, 2026-10-16): every re-offer closed the old viewer's
// RFB connection and x11vnc dropped the new one (EOF), so the offer failed
// and the browser waited out a retry backoff - a freeze + reload each time.
func TestDialHostRFBRetriesDroppedConnection(t *testing.T) {
	f := startFakeVNCDropping(t, 1280, 720, 1)
	c, err := dialHostRFB(f.path)
	if err != nil {
		t.Fatalf("dial after one dropped connection: %v", err)
	}
	c.close()
}

func TestRFBReleaseMessage(t *testing.T) {
	f := startFakeVNC(t, 800, 600)
	c, err := dialHostRFB(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	s := &webrtcSession{rfb: c}
	s.onInput([]byte(`{"t":"k","ks":97,"d":true}`))
	s.onInput([]byte(`{"t":"release"}`))
	want := []byte{4, 1, 0, 0, 0, 0, 0, 97, 4, 0, 0, 0, 0, 0, 0, 97}
	waitFor(t, "release", func() bool { return bytes.HasSuffix(f.received(), want) })
	if len(c.t.keys) != 0 {
		t.Errorf("tracker still holds %v", c.t.keys)
	}
}

func TestReadRFBServer(t *testing.T) {
	var b []byte
	b = append(b, 0, 0, 0, 2)                                        // FramebufferUpdate, 2 rects
	b = append(b, 0, 0, 0, 0, 0, 2, 0, 1, 0, 0, 0, 0)                // Raw 2x1
	b = append(b, make([]byte, 8)...)                                // its pixels
	b = append(b, 0, 0, 0, 0, 5, 0, 2, 0xd0, 0xff, 0xff, 0xff, 0x21) // DesktopSize 1280x720
	b = append(b, 2)                                                 // Bell
	b = append(b, 1, 0, 0, 0, 0, 1, 1, 2, 3, 4, 5, 6)                // SetColourMapEntries, 1 entry
	b = append(b, 3, 0, 0, 0, 0, 0, 0, 2, 0xe9, 'a')                 // ServerCutText "éa"
	var w, h int
	var clip string
	err := readRFBServer(bufio.NewReader(bytes.NewReader(b)), func(nw, nh int) { w, h = nw, nh }, func(s string) { clip = s })
	if err == nil || err.Error() != "EOF" {
		t.Errorf("err = %v, want EOF", err)
	}
	if w != 1280 || h != 720 || clip != "éa" {
		t.Errorf("size %dx%d clip %q", w, h, clip)
	}
}

func TestEncoderHung(t *testing.T) {
	defer func(d time.Duration) { webrtcHungAfter = d }(webrtcHungAfter)
	webrtcHungAfter = 3 * time.Second
	t0 := time.Unix(1000, 0)
	if encoderHung(t0.Add(2*time.Second), t0, time.Time{}) {
		t.Error("2 s after start is not hung")
	}
	if !encoderHung(t0.Add(4*time.Second), t0, time.Time{}) {
		t.Error("4 s without a packet is hung")
	}
	if encoderHung(t0.Add(10*time.Second), t0, t0.Add(9*time.Second)) {
		t.Error("a packet 1 s ago is not hung")
	}
}

func TestRTPContinuesAcrossRestart(t *testing.T) {
	var r rtpState
	if seq, ts, _, _ := r.next(time.Now()); seq != -1 || ts != -1 {
		t.Fatal("no packet yet: random offsets")
	}
	pkt := []byte{0x80, 96, 0xff, 0xff, 0, 0, 0, 100, 0, 0, 0, 1}
	r.note(pkt)
	seq, ts, n, _ := r.next(r.at.Add(time.Second))
	if seq != 0 || ts != 100+90000+1 || n != 1 { // seq wraps, ts moves on with the clock
		t.Errorf("seq %d ts %d n %d", seq, ts, n)
	}
}

type countWriter struct {
	mu sync.Mutex
	n  int
}

func (c *countWriter) Write(b []byte) (int, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return len(b), nil
}

// supervised runs superviseEncoder with a fake encoder child: spawn is
// recorded, and feed (if set) gets each run's RTP port.
func supervised(t *testing.T, feed func(p encParams, stop <-chan struct{})) (*webrtcSession, func() []encParams) {
	t.Helper()
	oldHung, oldPLI := webrtcHungAfter, webrtcPLIEvery
	webrtcHungAfter, webrtcPLIEvery = 300*time.Millisecond, 400*time.Millisecond
	var mu sync.Mutex
	var runs []encParams
	s := &webrtcSession{track: &countWriter{}, fps: 30, kbps: 1000, w: 1920, h: 1080,
		have:    map[string]bool{"nvh264enc": true, "cudaupload": true, "cudaconvert": true, "x264enc": true},
		restart: make(chan string, 1), presetCh: make(chan struct{}, 1), stop: make(chan struct{})}
	s.spawn = func(p encParams) *exec.Cmd {
		mu.Lock()
		runs = append(runs, p)
		mu.Unlock()
		if feed != nil {
			go feed(p, s.stop)
		}
		return exec.Command("sleep", "30")
	}
	done := make(chan struct{})
	go func() { s.superviseEncoder(); close(done) }()
	t.Cleanup(func() {
		s.once.Do(func() { close(s.stop) })
		<-done
		webrtcHungAfter, webrtcPLIEvery = oldHung, oldPLI
	})
	return s, func() []encParams { mu.Lock(); defer mu.Unlock(); return slices.Clone(runs) }
}

func TestSupervisorRestartsHungAndDemotes(t *testing.T) {
	_, runs := supervised(t, nil)
	waitFor(t, "three runs", func() bool { return len(runs()) >= 3 })
	r := runs()
	if r[0].Encoder != "nvh264enc" || r[1].Encoder != "nvh264enc" || r[2].Encoder != "x264enc" {
		t.Errorf("encoders %s %s %s: two silent runs must move on to the next encoder", r[0].Encoder, r[1].Encoder, r[2].Encoder)
	}
	if r[2].W != 1920 || r[2].H != 1080 || r[2].Kbps != 1000 {
		t.Errorf("params %+v", r[2])
	}
}

// rtpFeed: a fake encoder that sends an RTP packet every 20 ms.
func rtpFeed() func(p encParams, stop <-chan struct{}) {
	var seq uint16 = 500
	return func(p encParams, stop <-chan struct{}) {
		c, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p.Port)))
		if err != nil {
			return
		}
		defer c.Close()
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			seq++
			pkt := []byte{0x80, 96, byte(seq >> 8), byte(seq), 0, 0, 0, 0, 0, 0, 0, 1}
			if _, err := c.Write(pkt); err != nil {
				return
			}
		}
	}
}

func TestSupervisorPLIAndSize(t *testing.T) {
	s, runs := supervised(t, rtpFeed())
	waitFor(t, "first run", func() bool { return len(runs()) == 1 })
	s.requestRestart("pli") // right after the start: ignored
	time.Sleep(webrtcHungAfter * 2)
	if n := len(runs()); n != 1 {
		t.Fatalf("%d runs: a streaming encoder was restarted (early PLI or false hang)", n)
	}
	s.requestRestart("pli") // > webrtcPLIEvery after the start: honoured
	waitFor(t, "PLI restart", func() bool { return len(runs()) == 2 })
	if r := runs()[1]; r.Seq <= 0 || r.TS < 0 {
		t.Errorf("restart must continue RTP: %+v", r)
	}
	s.sizeMu.Lock()
	s.w, s.h = 1280, 720
	s.sizeMu.Unlock()
	s.requestRestart("size")
	waitFor(t, "size restart", func() bool { return len(runs()) == 3 })
	if r := runs()[2]; r.W != 1280 || r.Encoder != "nvh264enc" {
		t.Errorf("after resize: %+v", r)
	}
}

func TestOfferStream(t *testing.T) {
	for _, c := range []struct {
		preset           string
		fps, kbps        int
		wp               string
		wantFPS, wantKbp int
	}{
		{"low", 0, 0, "low", 0, 0},     // the preset's own values
		{"high", 60, 0, "high", 60, 0}, // fps override
		{"balanced", 24, 900000, "balanced", 30, 100000},
		{"", 0, 0, "", 30, 12000},         // older client: as before
		{"turbo", 60, 8000, "", 60, 8000}, // unknown preset: custom
		{"", 45, -1, "", 30, 12000},
	} {
		p, f, k := offerStream(c.preset, c.fps, c.kbps)
		if p != c.wp || f != c.wantFPS || k != c.wantKbp {
			t.Errorf("%q %d %d -> %q %d %d", c.preset, c.fps, c.kbps, p, f, k)
		}
	}
}

// Network adaptation: a "preset" message restarts the encoder with that
// preset (same peer, RTP continues), clearing custom overrides.
func TestSupervisorPresetSwitch(t *testing.T) {
	s, runs := supervised(t, rtpFeed())
	waitFor(t, "first run", func() bool { return len(runs()) == 1 })
	if r := runs()[0]; r.Preset != "" || r.FPS != 30 || r.Kbps != 1000 || r.MaxW != 0 {
		t.Fatalf("custom run: %+v", r)
	}
	cw := s.track.(*countWriter)
	waitFor(t, "packets", func() bool { cw.mu.Lock(); defer cw.mu.Unlock(); return cw.n > 2 })
	s.onInput([]byte(`{"t":"preset","preset":"low"}`))
	waitFor(t, "low run", func() bool { return len(runs()) == 2 })
	if r := runs()[1]; r.Encoder != "nvh264enc" || r.Preset != "low" || r.FPS != 24 || r.Kbps != 2500 || r.MaxW != 1280 || r.Seq <= 0 {
		t.Errorf("after preset low: %+v", r)
	}
	s.onInput([]byte(`{"t":"preset","preset":"low"}`))   // same: nothing
	s.onInput([]byte(`{"t":"preset","preset":"turbo"}`)) // unknown: ignored
	time.Sleep(webrtcHungAfter * 2)
	if n := len(runs()); n != 2 {
		t.Fatalf("%d runs: a repeated or unknown preset restarted the encoder", n)
	}
	s.onInput([]byte(`{"t":"preset","preset":"high"}`))
	waitFor(t, "high run", func() bool { return len(runs()) == 3 })
	if r := runs()[2]; r.Preset != "high" || r.FPS != 60 || r.Kbps != 25000 {
		t.Errorf("after preset high: %+v", r)
	}
}

func TestIPAndInterfaceFilter(t *testing.T) {
	for ip, want := range map[string]bool{"192.168.1.5": true, "10.0.0.1": true, "172.20.1.1": true, "100.101.2.3": true,
		"fd7a:115c:a1e0::1": true, "8.8.8.8": false, "2001:db8::1": false, "127.0.0.1": false, "169.254.1.1": false} {
		if got := webrtcIPAllowed(net.ParseIP(ip)); got != want {
			t.Errorf("%s: %v", ip, got)
		}
	}
	for name, want := range map[string]bool{"enp3s0": true, "wlan0": true, "tailscale0": true, "docker0": false, "virbr0": false, "veth12": false, "br-abc": false, "lo": false} {
		if got := webrtcInterfaceAllowed(name); got != want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestOfferRejectsBadBodyAndUnavailable(t *testing.T) {
	defer func(f func() captureTarget, sock string) { webrtcCapture, webrtcRFBSocket = f, sock }(webrtcCapture, webrtcRFBSocket)
	webrtcRFBSocket = t.TempDir() + "/none.sock" // never the live hostvnc.sock
	webrtcCapture = func() captureTarget { return captureTarget{reason: reasonNoDisplay} }

	rec := httptest.NewRecorder()
	handleWebRTCOffer(rec, httptest.NewRequest(http.MethodPost, "/host/webrtc/offer", strings.NewReader(`{"type":"answer"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad body: %d", rec.Code)
	}
	if _, err := exec.LookPath(gstLaunch); err != nil {
		t.Skip("no gst-launch-1.0: the reason would be reasonNoGstreamer")
	}
	rec = httptest.NewRecorder()
	handleWebRTCOffer(rec, httptest.NewRequest(http.MethodPost, "/host/webrtc/offer", strings.NewReader(`{"type":"offer","sdp":"v=0"}`)))
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body["error_code"] != "webrtc_unavailable" || body["reason"] != reasonNoDisplay {
		t.Errorf("unavailable: %d %v", rec.Code, body)
	}

	rec = httptest.NewRecorder()
	handleHostStream(rec, httptest.NewRequest(http.MethodGet, "/host/stream", nil))
	var st struct {
		Modes map[string]map[string]any `json:"modes"`
		Port  int                       `json:"udp_port"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st.Port != 28650 || st.Modes["webrtc"]["available"] != false || st.Modes["webrtc"]["reason"] != reasonNoDisplay || st.Modes["nova"] == nil || st.Modes["vnc"]["available"] != false {
		t.Errorf("stream modes: %s", rec.Body.String())
	}
}
