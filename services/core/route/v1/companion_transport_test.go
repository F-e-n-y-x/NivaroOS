package v1

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

func TestCompanionAddressKinds(t *testing.T) {
	cases := map[string]string{
		"192.168.1.20":          companionRouteLAN,
		"10.0.0.7":              companionRouteLAN,
		"172.16.4.2":            companionRouteLAN,
		"100.64.0.5":            companionRouteTailscale,
		"100.127.255.1":         companionRouteTailscale,
		"fd7a:115c:a1e0::1234":  companionRouteTailscale,
		"phone.tail1234.ts.net": companionRouteTailscale,
		"100.128.0.1":           "", // public, just outside CGNAT
		"8.8.8.8":               "",
		"127.0.0.1":             "",
		"169.254.169.254":       "", // cloud metadata
		"0.0.0.0":               "",
		"::1":                   "",
		"fe80::1":               "",
		"example.com":           "",
		"ts.net":                "",
		"evil.ts.net.example":   "",
		"a..tail.ts.net":        "",
		"Local Device":          "",
	}
	for addr, want := range cases {
		if got := companionAddrKind(addr); got != want {
			t.Errorf("%s: %q, want %q", addr, got, want)
		}
	}
}

func TestCompanionCandidatesOrderAndFilter(t *testing.T) {
	dev := &CompanionDevice{IP: "100.64.0.9", Addresses: []string{
		"fd7a:115c:a1e0::9", "phone.tail1234.ts.net", "8.8.8.8", "192.168.1.20", "100.64.0.9", "127.0.0.1",
	}}
	var got []string
	for _, c := range companionCandidates(dev) {
		got = append(got, c.Kind+":"+c.Host)
	}
	want := "lan:192.168.1.20 tailscale:100.64.0.9 tailscale:fd7a:115c:a1e0::9 tailscale:phone.tail1234.ts.net"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v", got)
	}
	if f := filterCompanionAddresses([]string{"8.8.8.8", " 100.64.0.2 ", "100.64.0.2", "10.1.1.1"}); strings.Join(f, ",") != "100.64.0.2,10.1.1.1" {
		t.Fatalf("filter: %v", f)
	}
}

// The dialer itself refuses what isn't allowed, whatever a name resolves to.
func TestCompanionDialerRefusesDisallowedTargets(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := companionDialer.Dial("tcp", ln.Addr().String()); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("loopback dial: %v", err)
	}
}

// fakePhoneHTTP is the phone's file server: /hello proves the secret,
// /download serves Range, /upload stores, /files lists.
type fakePhoneHTTP struct {
	id, secret string
	files      map[string][]byte
	legacy     bool // an app from before /hello
	hellos     atomic.Int32
	mu         sync.Mutex
	uploads    map[string][]byte
}

func (f *fakePhoneHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/status" {
		w.Write([]byte(`{"success":true}`))
		return
	}
	if r.URL.Path == "/hello" && !f.legacy {
		f.hellos.Add(1)
		json.NewEncoder(w).Encode(map[string]string{"id": f.id, "proof": companionHelloProof(f.secret, f.id, r.URL.Query().Get("nonce"))})
		return
	}
	if r.Header.Get("X-Companion-Secret") != f.secret {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	p := r.URL.Query().Get("path")
	switch r.URL.Path {
	case "/download":
		data, ok := f.files[p]
		if !ok {
			w.WriteHeader(404)
			w.Write([]byte(`{"success":false,"message":"File not found"}`))
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, p, time.Unix(0, 0), bytes.NewReader(data))
	case "/upload":
		b, err := io.ReadAll(r.Body)
		if err != nil || (r.ContentLength >= 0 && int64(len(b)) != r.ContentLength) {
			w.WriteHeader(400)
			w.Write([]byte(`{"success":false,"message":"The upload was incomplete"}`))
			return
		}
		f.mu.Lock()
		f.uploads[p] = b
		f.mu.Unlock()
		w.Write([]byte(`{"success":true}`))
	case "/files":
		w.Write([]byte(`{"success":true,"files":[{"name":"DCIM","is_dir":true}]}`))
	case "/delete", "/mkdir", "/rename":
		w.Write([]byte(`{"success":true}`))
	default:
		w.WriteHeader(404)
		w.Write([]byte(`{"success":false,"message":"Not found"}`))
	}
}

// Loopback stand-ins: 127.0.0.1 plays the phone's Wi-Fi address, 127.0.0.2
// its Tailscale address, 127.0.0.3 another device on the home LAN.
func useLoopbackAddressKinds(t *testing.T) {
	companionIPKindHook = func(ip net.IP) (string, bool) {
		switch ip.String() {
		case "127.0.0.1", "127.0.0.3", "127.0.0.4":
			return companionRouteLAN, true
		case "127.0.0.2":
			return companionRouteTailscale, true
		}
		return "", false
	}
	resetCompanionRoutes()
	t.Cleanup(func() { companionIPKindHook = nil; resetCompanionRoutes() })
}

func resetCompanionRoutes() {
	companionRoutesMu.Lock()
	companionRoutes = map[string]*companionRouteEntry{}
	companionRoutesMu.Unlock()
}

// listenOn serves h on host at a port free on every host given.
func listenOn(t *testing.T, h http.Handler, port int, host string) *http.Server {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return srv
}

func freePort(t *testing.T) int {
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp", "127.0.0.2:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		ok := true
		for _, h := range []string{"127.0.0.1", "127.0.0.3", "127.0.0.4"} {
			l, err := net.Listen("tcp", net.JoinHostPort(h, strconv.Itoa(port)))
			if err != nil {
				ok = false
				break
			}
			l.Close()
		}
		if ok {
			return port
		}
	}
	t.Fatal("no free port")
	return 0
}

func TestCompanionRouteSelection(t *testing.T) {
	useLoopbackAddressKinds(t)
	companionProbeTimeoutLAN = 300 * time.Millisecond
	t.Cleanup(func() { companionProbeTimeoutLAN = 1500 * time.Millisecond })
	port := freePort(t)
	phone := &fakePhoneHTTP{id: "ph", secret: "s3cret", files: map[string][]byte{"/a": pattern(1000)}, uploads: map[string][]byte{}}
	other := &fakePhoneHTTP{id: "someone-else", secret: "other", files: map[string][]byte{}}

	// At home: Wi-Fi address answers -> LAN.
	listenOn(t, phone, port, "127.0.0.2")
	lanSrv := listenOn(t, phone, port, "127.0.0.1")
	dev := &CompanionDevice{ID: "ph", Name: "P", Secret: "s3cret", Port: port, IP: "127.0.0.1", Addresses: []string{"127.0.0.2"}}
	if ep := companionDirectEndpoint(context.Background(), dev); ep == nil || ep.Kind != companionRouteLAN {
		t.Fatalf("home: %+v", ep)
	}
	// Cached: no more handshakes.
	n := phone.hellos.Load()
	for i := 0; i < 5; i++ {
		companionDirectEndpoint(context.Background(), dev)
	}
	if phone.hellos.Load() != n {
		t.Fatal("route not cached")
	}

	// Away: the Wi-Fi address is gone -> a request fails over, then the
	// Tailscale address is used.
	lanSrv.Close()
	resp, err := companionDo(context.Background(), dev, companionRequest{Method: "GET", Path: "/download", Query: map[string][]string{"path": {"/a"}}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.Route != companionRouteTailscale || len(b) != 1000 {
		t.Fatalf("away: route %s, %d bytes", resp.Route, len(b))
	}

	// Another device answering on the phone's old Wi-Fi address (a hotel
	// network numbered like home) never gets used - nor the secret.
	resetCompanionRoutes()
	var leaked atomic.Bool
	listenOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Companion-Secret") != "" {
			leaked.Store(true)
		}
		other.ServeHTTP(w, r)
	}), port, "127.0.0.3")
	dev.IP = "127.0.0.3"
	if ep := companionDirectEndpoint(context.Background(), dev); ep == nil || ep.Kind != companionRouteTailscale {
		t.Fatalf("impostor on LAN: %+v", ep)
	}
	if leaked.Load() {
		t.Fatal("secret sent to an unverified address")
	}
}

// An app from before /hello still works at home (its LAN address, as
// before), never over an unverified Tailscale address.
func TestCompanionLegacyAppLANOnly(t *testing.T) {
	useLoopbackAddressKinds(t)
	port := freePort(t)
	old := &fakePhoneHTTP{id: "old", secret: "s", legacy: true, files: map[string][]byte{"/a": []byte("hi")}}
	listenOn(t, old, port, "127.0.0.1")
	listenOn(t, old, port, "127.0.0.2")
	dev := &CompanionDevice{ID: "old", Secret: "s", Port: port, IP: "127.0.0.1"}
	if ep := companionDirectEndpoint(context.Background(), dev); ep == nil || ep.Kind != companionRouteLAN {
		t.Fatalf("legacy LAN: %+v", ep)
	}
	resetCompanionRoutes()
	dev.IP, dev.Addresses = "", []string{"127.0.0.2"}
	if ep := companionDirectEndpoint(context.Background(), dev); ep != nil {
		t.Fatalf("legacy over tailscale accepted: %+v", ep)
	}
}

// goTunnelPhone is the phone half of the tunnel (what the app does in
// Dart): register with streams, replay each stream against its own file
// server, stream answers back under credit.
func goTunnelPhone(t *testing.T, wsURL string, local http.Handler, window int) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wmu sync.Mutex
	write := func(kind int, b []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		return conn.WriteMessage(kind, b)
	}
	b, _ := json.Marshal(map[string]interface{}{"action": "register", "streams": 1, "window": window})
	write(websocket.TextMessage, b)

	type st struct {
		credit chan int
		body   *io.PipeWriter
		cancel context.CancelFunc
	}
	var mu sync.Mutex
	streams := map[uint32]*st{}
	go func() {
		for {
			kind, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.TextMessage {
				var o struct {
					Action, Method, Path, Query string
					SID                         uint32
					Headers                     map[string]string
					Body                        bool
					Window                      int
				}
				if json.Unmarshal(msg, &o) != nil || o.Action != "http" {
					continue
				}
				ctx, cancel := context.WithCancel(context.Background())
				s := &st{credit: make(chan int, 1024), cancel: cancel}
				var body io.Reader
				if o.Body {
					pr, pw := io.Pipe()
					s.body, body = pw, pr
				}
				mu.Lock()
				streams[o.SID] = s
				mu.Unlock()
				go func() {
					defer cancel()
					req := httptest.NewRequest(o.Method, o.Path+"?"+o.Query, body).WithContext(ctx)
					for k, v := range o.Headers {
						req.Header.Set(k, v)
					}
					if cl := o.Headers["Content-Length"]; cl != "" {
						req.ContentLength, _ = strconv.ParseInt(cl, 10, 64)
					}
					req.Header.Set("X-Companion-Secret", "s3cret")
					rec := &streamingRecorder{h: http.Header{}, head: func(code int, h http.Header) {
						hm := map[string]string{}
						for k := range h {
							hm[k] = h.Get(k)
						}
						b, _ := json.Marshal(map[string]interface{}{"type": "http_head", "sid": o.SID, "status": code, "headers": hm})
						write(websocket.TextMessage, b)
					}}
					credit := o.Window
					rec.write = func(p []byte) (int, error) {
						total := len(p)
						for len(p) > 0 {
							for credit <= 0 {
								select {
								case c := <-s.credit:
									credit += c
								case <-ctx.Done():
									return 0, ctx.Err()
								}
							}
							n := min(len(p), tunnelFrameSize, credit)
							if err := write(websocket.BinaryMessage, frame(tunnelFrameData, o.SID, p[:n])); err != nil {
								return 0, err
							}
							credit -= n
							p = p[n:]
						}
						return total, nil
					}
					local.ServeHTTP(rec, req)
					rec.flushHead()
					if ctx.Err() == nil {
						write(websocket.BinaryMessage, frame(tunnelFrameEnd, o.SID, nil))
					}
				}()
				continue
			}
			if len(msg) < 5 {
				continue
			}
			fk, sid, payload := msg[0], binary.BigEndian.Uint32(msg[1:5]), msg[5:]
			mu.Lock()
			s := streams[sid]
			mu.Unlock()
			if s == nil {
				continue
			}
			switch fk {
			case tunnelFrameCredit:
				s.credit <- int(binary.BigEndian.Uint32(payload))
			case tunnelFrameReset:
				s.cancel()
				if s.body != nil {
					s.body.CloseWithError(errors.New("reset"))
				}
			case tunnelFrameData:
				s.body.Write(payload)
				var c [4]byte
				binary.BigEndian.PutUint32(c[:], uint32(len(payload)))
				write(websocket.BinaryMessage, frame(tunnelFrameCredit, sid, c[:]))
			case tunnelFrameEnd:
				s.body.Close()
			}
		}
	}()
	return conn
}

type streamingRecorder struct {
	h     http.Header
	code  int
	sent  bool
	head  func(int, http.Header)
	write func([]byte) (int, error)
}

func (r *streamingRecorder) Header() http.Header { return r.h }
func (r *streamingRecorder) WriteHeader(c int) {
	if !r.sent {
		r.code, r.sent = c, true
		r.head(c, r.h)
	}
}
func (r *streamingRecorder) flushHead() { r.WriteHeader(http.StatusOK) }
func (r *streamingRecorder) Write(p []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return r.write(p)
}

func startTunnelServer(t *testing.T) string {
	t.Helper()
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user", &jwt.Claims{ID: 1})
			return next(c)
		}
	})
	e.GET("/v1/companion/devices/:id/ws", GetCompanionDeviceWS)
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/companion/devices/"
}

// Away from home without Tailscale: everything goes through the tunnel -
// listing, a Range request for video, an upload, delete - over a real
// WebSocket through GetCompanionDeviceWS.
func TestCompanionFilesThroughTheTunnel(t *testing.T) {
	useTempCompanionState(t)
	resetCompanionRoutes()
	companionMu.Lock()
	companionLoaded = true
	companionDevices["p"] = &CompanionDevice{ID: "p", Name: "P", OwnerUserID: "1", Secret: "s3cret", LastSeen: time.Now()}
	companionMu.Unlock()

	video := pattern(3<<20 + 5)
	phone := &fakePhoneHTTP{id: "p", secret: "s3cret", files: map[string][]byte{"/storage/emulated/0/v.mp4": video}, uploads: map[string][]byte{}}
	conn := goTunnelPhone(t, startTunnelServer(t)+"p/ws", phone, 512<<10)
	defer conn.Close()
	waitFor(t, func() bool { tt := companionTunnelFor("p"); return tt != nil && tt.streams() != nil })

	companionMu.Lock()
	dev := companionDevices["p"].snapshot()
	companionMu.Unlock()

	items, err := FetchCompanionFilesFromDevice(dev, "/storage/emulated/0")
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %v %v", items, err)
	}

	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Range", "bytes=100-199")
	w := httptest.NewRecorder()
	if err := ProxyCompanionStream(dev, "/storage/emulated/0/v.mp4", w, r); err != nil {
		t.Fatal(err)
	}
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), video[100:200]) || w.Header().Get("X-Companion-Route") != companionRouteTunnel {
		t.Fatalf("range: %d %d bytes route %q", w.Code, w.Body.Len(), w.Header().Get("X-Companion-Route"))
	}

	w = httptest.NewRecorder()
	if err := ProxyCompanionStream(dev, "/storage/emulated/0/v.mp4", w, httptest.NewRequest("GET", "/x", nil)); err != nil || !bytes.Equal(w.Body.Bytes(), video) {
		t.Fatalf("full: %v %d", err, w.Body.Len())
	}

	up := pattern(2<<20 + 3)
	if err := ProxyCompanionUploadStream(dev, "/storage/emulated/0/up.bin", bytes.NewReader(up), int64(len(up))); err != nil {
		t.Fatal(err)
	}
	phone.mu.Lock()
	got := phone.uploads["/storage/emulated/0/up.bin"]
	phone.mu.Unlock()
	if !bytes.Equal(got, up) {
		t.Fatalf("upload: %d of %d bytes", len(got), len(up))
	}
	if err := ProxyCompanionFileDelete(dev, "/storage/emulated/0/up.bin"); err != nil {
		t.Fatal(err)
	}
	if err := ProxyCompanionMkdir(dev, "/storage/emulated/0/New"); err != nil {
		t.Fatal(err)
	}

	// Copy from the phone to the server (the Files app's copy/move).
	dst := t.TempDir() + "/v.mp4"
	h := &companionIOHandlerImpl{}
	if err := h.downloadFileFromCompanion(context.Background(), dev, "/storage/emulated/0/v.mp4", dst, "overwrite", nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); !bytes.Equal(b, video) {
		t.Fatal("copied file differs")
	}

	// The device list says how the phone is reached.
	probeCompanionConnection(dev)
	if dev.Route != companionRouteTunnel || dev.Connection != "remote" {
		t.Fatalf("route %q connection %q", dev.Route, dev.Connection)
	}
}

// Throughput through the tunnel over loopback (a real WebSocket), and
// direct for comparison. Logged, not asserted beyond a floor.
func TestCompanionTunnelThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("throughput")
	}
	useTempCompanionState(t)
	resetCompanionRoutes()
	companionMu.Lock()
	companionLoaded = true
	companionDevices["p"] = &CompanionDevice{ID: "p", Name: "P", OwnerUserID: "1", Secret: "s3cret", LastSeen: time.Now()}
	companionMu.Unlock()
	size := 256 << 20
	if s := os.Getenv("COMPANION_TPUT_MB"); s != "" {
		n, _ := strconv.Atoi(s)
		size = n << 20
	}
	blob := pattern(size)
	phone := &fakePhoneHTTP{id: "p", secret: "s3cret", files: map[string][]byte{"/b": blob}, uploads: map[string][]byte{}}
	conn := goTunnelPhone(t, startTunnelServer(t)+"p/ws", phone, tunnelDefaultWindow)
	defer conn.Close()
	waitFor(t, func() bool { tt := companionTunnelFor("p"); return tt != nil && tt.streams() != nil })
	companionMu.Lock()
	dev := companionDevices["p"].snapshot()
	companionMu.Unlock()

	measure := func(name string, fn func() (int64, error)) float64 {
		start := time.Now()
		n, err := fn()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		mbps := float64(n) / (1 << 20) / time.Since(start).Seconds()
		t.Logf("%s: %d MiB in %s = %.0f MiB/s", name, n>>20, time.Since(start).Round(time.Millisecond), mbps)
		return mbps
	}
	down := measure("tunnel download", func() (int64, error) {
		resp, err := companionDo(context.Background(), dev, companionRequest{Method: "GET", Path: "/download", Query: map[string][]string{"path": {"/b"}}})
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		return io.Copy(io.Discard, resp.Body)
	})
	upMB := measure("tunnel upload", func() (int64, error) {
		return int64(size), ProxyCompanionUploadStream(dev, "/u", bytes.NewReader(blob), int64(size))
	})
	if down < 20 || upMB < 20 {
		t.Fatalf("tunnel too slow: down %.0f up %.0f MiB/s", down, upMB)
	}
	fmt.Fprintf(os.Stderr, "THROUGHPUT tunnel down=%.0fMiB/s up=%.0fMiB/s\n", down, upMB)
}

// The same vector the app's test checks (companion_tunnel_streams_test.dart).
func TestCompanionHelloProofVector(t *testing.T) {
	if got := companionHelloProof("s3cret", "dev_1", "00112233445566778899aabbccddeeff"); got != "380ca98629f8f6814373f1f8af97796d9ab6e657731df29f18fcdd99dd4942c3" {
		t.Fatal(got)
	}
}

// The server's tunnel against the app's real phone half (Dart), over a real
// WebSocket on loopback: list, Range, full download, upload, and throughput.
// Needs Flutter; run with NIVAROOS_DART_E2E=/path/to/flutter.
func TestCompanionTunnelDartPhone(t *testing.T) {
	flutter := os.Getenv("NIVAROOS_DART_E2E")
	if flutter == "" {
		t.Skip("set NIVAROOS_DART_E2E to the flutter binary")
	}
	useTempCompanionState(t)
	resetCompanionRoutes()
	companionMu.Lock()
	companionLoaded = true
	companionDevices["p"] = &CompanionDevice{ID: "p", Name: "P", OwnerUserID: "1", Secret: "s3cret", LastSeen: time.Now()}
	companionMu.Unlock()
	mb := 64
	if s := os.Getenv("COMPANION_TPUT_MB"); s != "" {
		mb, _ = strconv.Atoi(s)
	}
	blob := pattern(mb << 20)

	cmd := exec.Command(flutter, "test", "test/services/companion_tunnel_e2e_test.dart")
	cmd.Dir = "../../../../mobile"
	cmd.Env = append(os.Environ(), "NIVAROOS_TUNNEL_WS="+startTunnelServer(t)+"p/ws", "NIVAROOS_TUNNEL_MB="+strconv.Itoa(mb))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if tt := companionTunnelFor("p"); tt != nil && tt.streams() != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("phone never connected:\n%s", out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	companionMu.Lock()
	dev := companionDevices["p"].snapshot()
	companionMu.Unlock()

	if items, err := FetchCompanionFilesFromDevice(dev, "/storage/emulated/0"); err != nil || len(items) != 1 {
		t.Fatalf("list: %v %v", items, err)
	}
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Range", "bytes=5000-9999")
	w := httptest.NewRecorder()
	if err := ProxyCompanionStream(dev, "/b", w, r); err != nil || w.Code != 206 || !bytes.Equal(w.Body.Bytes(), blob[5000:10000]) {
		t.Fatalf("range: %v %d %d", err, w.Code, w.Body.Len())
	}
	start := time.Now()
	resp, err := companionDo(context.Background(), dev, companionRequest{Method: "GET", Path: "/download", Query: map[string][]string{"path": {"/b"}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	down := time.Since(start)
	if err != nil || !bytes.Equal(got, blob) {
		t.Fatalf("download: %v, %d bytes", err, len(got))
	}
	start = time.Now()
	if err := ProxyCompanionUploadStream(dev, "/u", bytes.NewReader(blob), int64(len(blob))); err != nil {
		t.Fatal(err)
	}
	up := time.Since(start)
	msg := fmt.Sprintf("THROUGHPUT dart-phone tunnel %d MiB: down %.0f MiB/s, up %.0f MiB/s", mb, float64(mb)/down.Seconds(), float64(mb)/up.Seconds())
	t.Log(msg)
	fmt.Fprintln(os.Stderr, msg)

	closeCompanionTunnel("p")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("flutter test: %v\n%s", err, out.String())
		}
	case <-time.After(time.Minute):
		cmd.Process.Kill()
		t.Fatal("phone didn't stop")
	}
	if !strings.Contains(out.String(), fmt.Sprintf("uploaded %d bytes", len(blob))) {
		t.Fatalf("phone side:\n%s", out.String())
	}
}
