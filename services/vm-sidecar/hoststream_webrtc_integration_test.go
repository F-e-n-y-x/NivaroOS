//go:build webrtc_integration

// A real offer/answer against a throwaway Xvfb + x11vnc on a private unix
// socket (never the host's own display or VNC server):
//
//	PATH=$PATH:<dir with gst-launch-1.0> go test -tags webrtc_integration -run Integration -v .
//
// WEBRTC_IT_ENCODER picks the encoder (default x264enc; nvh264enc opens one
// NVENC session for the test's duration).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

func freeDisplay(t *testing.T) string {
	for n := 70; n < 100; n++ {
		if _, err := os.Stat(fmt.Sprintf("/tmp/.X11-unix/X%d", n)); os.IsNotExist(err) {
			if _, err := os.Stat(fmt.Sprintf("/tmp/.X%d-lock", n)); os.IsNotExist(err) {
				return fmt.Sprintf(":%d", n)
			}
		}
	}
	t.Fatal("no free X display number")
	return ""
}

func startBG(t *testing.T, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	// SIGTERM: Xvfb removes its /tmp/.X<n>-lock (SIGKILL leaks it, and
	// freeDisplay runs out of numbers).
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	return cmd
}

// resizeDesktop: the app's "Screen resolution" / "Match this phone" on the
// test display (skips where the X server can't resize).
func resizeDesktop(t *testing.T, disp string, w, h int) {
	t.Helper()
	if _, err := exec.LookPath("Xtigervnc"); err != nil {
		t.Skip("Xtigervnc (tigervnc-standalone-server) not found: Xvfb can't resize")
	}
	if err := setHostDisplayXrandr(w, h, hostXTarget{Display: disp}); err != nil {
		t.Fatalf("resize to %dx%d: %v", w, h, err)
	}
}

func gstRunning(display string) bool {
	out, _ := exec.Command("pgrep", "-f", "gst-launch-1.0 ximagesrc display-name="+display+" ").Output()
	return len(bytes.TrimSpace(out)) > 0
}

// testDesktop: a throwaway Xvfb + x11vnc on a private socket wired into
// the lane; onDisp runs an X client against that display only.
func testDesktop(t *testing.T) (enc, disp string, onDisp func(string, ...string) *exec.Cmd) {
	for _, b := range []string{"Xvfb", "x11vnc", "xdotool", "xprop", gstLaunch} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s not found", b)
		}
	}
	enc = os.Getenv("WEBRTC_IT_ENCODER")
	if enc == "" {
		enc = "x264enc"
	}
	disp = freeDisplay(t)
	// Xtigervnc (tigervnc-standalone-server) resizes through RandR like the
	// owner's desktop ("Match this phone"); Xvfb can't.
	if _, err := exec.LookPath("Xtigervnc"); err == nil {
		startBG(t, "Xtigervnc", disp, "-geometry", "1280x720", "-depth", "24", "-SecurityTypes", "None", "-rfbport", "-1",
			"-rfbunixpath", t.TempDir()+"/xvnc.sock", "-nolisten", "tcp")
	} else {
		startBG(t, "Xvfb", disp, "-screen", "0", "1280x720x24", "-nolisten", "tcp")
	}
	waitFor(t, "X server", func() bool { return displaySocketExists(disp) })
	// X clients of the test only ever see the throwaway display.
	onDisp = func(name string, args ...string) *exec.Cmd {
		c := exec.Command(name, args...)
		c.Env = append(os.Environ(), "DISPLAY="+disp, "XAUTHORITY=/dev/null")
		return c
	}
	sock := t.TempDir() + "/vnc.sock"
	// x11vnc only reads the real cursor shape (XFIXES) once it sees a window
	// manager; the test display has none.
	t.Setenv("X11VNC_AVOID_WINDOWS", "never")
	// The flags nivaroos-host-desktop.sh gives it that matter here.
	startBG(t, "x11vnc", "-display", disp, "-rfbport", "0", "-rfbportv6", "-1", "-unixsock", sock, "-noipv6", "-nopw",
		"-forever", "-shared", "-nonap", "-xrandr", "resize", "-noxdamage", "-quiet")
	waitFor(t, "x11vnc socket", func() bool { return socketListening(sock) })

	oldSock, oldCap, oldLoop, oldPort, oldLinger := webrtcRFBSocket, webrtcCapture, webrtcAllowLoopback, webrtcUDPPort, webrtcLinger
	t.Cleanup(func() {
		webrtcRFBSocket, webrtcCapture, webrtcAllowLoopback, webrtcUDPPort, webrtcLinger = oldSock, oldCap, oldLoop, oldPort, oldLinger
	})
	webrtcRFBSocket, webrtcAllowLoopback, webrtcUDPPort, webrtcLinger = sock, true, 0, 2*time.Second
	webrtcCapture = func() captureTarget { return captureTarget{x11: hostXTarget{Display: disp}} }
	gstProbe.Lock()
	gstProbe.have = map[string]bool{enc: true, "cudaupload": true, "cudaconvert": true, "ximagesrc": true,
		"videoscale": true, "videoconvert": true, "rtph264pay": true, "udpsink": true}
	gstProbe.Unlock()
	t.Cleanup(func() { gstProbe.Lock(); gstProbe.have = nil; gstProbe.Unlock() })
	return enc, disp, onDisp
}

// cursorWindow maps a full-screen window with the X "watch" cursor on the
// test display (python3-xlib), so the shape under the pointer changes the
// way it does over a window edge.
func cursorWindow(t *testing.T, onDisp func(string, ...string) *exec.Cmd) {
	t.Helper()
	if exec.Command("python3", "-c", "import Xlib").Run() != nil {
		t.Skip("python3-xlib not found")
	}
	c := onDisp("python3", "-c", `from Xlib import display
import time
d = display.Display(); s = d.screen(); f = d.open_font('cursor')
cur = f.create_glyph_cursor(f, 150, 151, (0, 0, 0), (65535, 65535, 65535))
s.root.create_window(0, 0, s.width_in_pixels, s.height_in_pixels, 0, s.root_depth, cursor=cur, override_redirect=1).map()
d.sync(); time.sleep(600)`)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
}

func TestIntegrationWebRTCOfferStreamsAndInjects(t *testing.T) {
	enc, disp, onDisp := testDesktop(t)
	mux := http.NewServeMux()
	RegisterHostStreamRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var st struct {
		Modes map[string]map[string]any `json:"modes"`
	}
	resp, err := http.Get(srv.URL + "/host/stream")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	if st.Modes["webrtc"]["available"] != true || st.Modes["webrtc"]["encoder"] != enc || st.Modes["webrtc"]["default_preset"] != "balanced" {
		t.Fatalf("/host/stream: %v", st.Modes)
	}
	if kind := map[bool]string{true: "gpu", false: "cpu"}[enc != "x264enc"]; st.Modes["webrtc"]["encoder_type"] != kind {
		t.Errorf("encoder_type %v, want %s", st.Modes["webrtc"]["encoder_type"], kind)
	}
	if low, _ := st.Modes["webrtc"]["presets"].(map[string]any)["low"].(map[string]any); low == nil || low["max_width"] != 1280.0 {
		t.Errorf("presets: %v", st.Modes["webrtc"]["presets"])
	}

	// --- client ---
	var se webrtc.SettingEngine
	se.SetIncludeLoopbackCandidate(true)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))
	connect := func() (*webrtc.PeerConnection, *webrtc.DataChannel, *atomic.Int64, chan dcMsg) {
		pc, err := api.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			t.Fatal(err)
		}
		dc, err := pc.CreateDataChannel("input", nil)
		if err != nil {
			t.Fatal(err)
		}
		msgs := make(chan dcMsg, 64)
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			var v dcMsg
			if json.Unmarshal(m.Data, &v) == nil {
				select {
				case msgs <- v:
				default:
				}
			}
		})
		var pkts atomic.Int64
		pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			if !strings.EqualFold(tr.Codec().MimeType, webrtc.MimeTypeH264) {
				t.Errorf("codec %s", tr.Codec().MimeType)
			}
			for {
				if _, _, err := tr.ReadRTP(); err != nil {
					return
				}
				pkts.Add(1)
			}
		})
		offer, _ := pc.CreateOffer(nil)
		gathered := webrtc.GatheringCompletePromise(pc)
		_ = pc.SetLocalDescription(offer)
		<-gathered
		body, _ := json.Marshal(map[string]any{"sdp": pc.LocalDescription().SDP, "type": "offer", "preset": "low"})
		resp, err := http.Post(srv.URL+"/host/webrtc/offer", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var ans struct{ SDP, Type string }
		_ = json.NewDecoder(resp.Body).Decode(&ans)
		if resp.StatusCode != 200 || ans.Type != "answer" {
			t.Fatalf("offer: %d %+v", resp.StatusCode, ans)
		}
		if !strings.Contains(ans.SDP, "127.0.0.1") || strings.Contains(ans.SDP, "srflx") {
			t.Errorf("answer candidates: %s", ans.SDP)
		}
		if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: ans.SDP}); err != nil {
			t.Fatal(err)
		}
		return pc, dc, &pkts, msgs
	}

	pc1, dc1, pkts1, msgs1 := connect()
	waitForLong := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	waitForLong("video packets", 15*time.Second, func() bool { return pkts1.Load() > 30 })
	gotSize, gotHB, gotEnc := false, false, false
	firstCursor := ""
	waitForLong("size + heartbeat + enc + cursor", 6*time.Second, func() bool {
		for {
			select {
			case m := <-msgs1:
				if m.T == "size" && m.W == 1280 && m.H == 720 {
					gotSize = true
				}
				if m.T == "hb" {
					gotHB = true
				}
				// x11vnc's Cursor rects become {"t":"cursor"} PNGs (the video has no cursor).
				if m.T == "cursor" && m.W > 0 && m.W <= maxCursor && m.PNG != "" {
					firstCursor = m.PNG
				}
				// Low: 1280 wide at most, so the 1280x720 test screen stays as is.
				if m.T == "enc" && m.Encoder == enc && m.Preset == "low" && m.W == 1280 && m.Kbps > 0 {
					gotEnc = true
				}
				continue
			default:
			}
			return gotSize && gotHB && gotEnc && firstCursor != ""
		}
	})

	// A window with the "watch" cursor (hotspot 15,9) under the pointer:
	// its shape comes through.
	cursorWindow(t, onDisp)
	waitForLong("the window's cursor shape", 6*time.Second, func() bool {
		for {
			select {
			case m := <-msgs1:
				if m.T == "cursor" && m.X == 15 && m.Y == 9 && m.PNG != "" && m.PNG != firstCursor {
					return true
				}
				continue
			default:
			}
			return false
		}
	})

	// Network adaptation: the client switches preset on the data channel;
	// the encoder restarts with it and the same peer keeps getting video.
	_ = dc1.SendText(`{"t":"preset","preset":"high"}`)
	waitForLong("enc high", 6*time.Second, func() bool {
		for {
			select {
			case m := <-msgs1:
				if m.T == "enc" && m.Preset == "high" {
					return true
				}
				continue
			default:
			}
			return false
		}
	})
	before := pkts1.Load()
	waitForLong("video after the preset switch", 10*time.Second, func() bool { return pkts1.Load() > before+30 })

	// Input reaches the RFB socket: x11vnc moves Xvfb's pointer.
	_ = dc1.SendText(`{"t":"p","x":321,"y":123,"m":0}`)
	waitForLong("pointer on the test display", 5*time.Second, func() bool {
		out, _ := onDisp("xdotool", "getmouselocation").CombinedOutput()
		return strings.HasPrefix(string(out), "x:321 y:123")
	})
	// Clipboard: x11vnc keeps a viewer's text in CUT_BUFFER0 (and the
	// selections when a client asks).
	_ = dc1.SendText(`{"t":"clip","text":"nivaroos-webrtc"}`)
	waitForLong("clipboard on the test display", 5*time.Second, func() bool {
		out, _ := onDisp("xprop", "-root", "CUT_BUFFER0").Output()
		return strings.Contains(string(out), `"nivaroos-webrtc"`)
	})

	// A second viewer replaces the first.
	var closed1 sync.Once
	gone1 := make(chan struct{})
	dc1.OnClose(func() { closed1.Do(func() { close(gone1) }) })
	_, _, pkts2, _ := connect()
	waitForLong("second viewer's video", 15*time.Second, func() bool { return pkts2.Load() > 30 })
	select {
	case <-gone1:
	case <-time.After(10 * time.Second):
		t.Errorf("first viewer still open (state %s)", pc1.ConnectionState())
	}
	if !gstRunning(disp) {
		t.Fatal("no encoder for the second viewer?")
	}

	// The viewer leaves: the encoder is gone within the linger time.
	webrtcState.Lock()
	cur := webrtcState.cur
	webrtcState.Unlock()
	cur.close()
	waitForLong("encoder exit", 5*time.Second, func() bool { return !gstRunning(disp) })
}

// TestIntegrationWebViewerInChrome runs the real web viewer
// (ui/src/shell/desktop/HostDesktopWebRTC.vue) in headless Chrome against
// the lane. WEBRTC_IT_UI is a Vite dev server of ui/ (pnpm exec vite --port
// 18093) serving the test page ui/webrtc-harness.{html,js}.
func TestIntegrationWebViewerInChrome(t *testing.T) {
	ui := os.Getenv("WEBRTC_IT_UI")
	if ui == "" {
		t.Skip("WEBRTC_IT_UI not set")
	}
	chrome, err := exec.LookPath("google-chrome")
	if err != nil {
		t.Skip("no google-chrome")
	}
	enc, disp, onDisp := testDesktop(t)
	api := http.NewServeMux()
	RegisterHostStreamRoutes(api)
	target, _ := url.Parse(ui)
	front := http.NewServeMux()
	front.Handle("/v1/vm-sidecar/", http.StripPrefix("/v1/vm-sidecar", api))
	front.HandleFunc("/__log", func(http.ResponseWriter, *http.Request) {})
	front.Handle("/", httputil.NewSingleHostReverseProxy(target))
	srv := httptest.NewServer(front)
	defer srv.Close()

	prof := t.TempDir()
	cmd := exec.Command(chrome, "--headless=new", "--no-sandbox", "--no-first-run", "--user-data-dir="+prof,
		"--remote-debugging-port=0", "--window-size=1280,720", "--autoplay-policy=no-user-gesture-required",
		srv.URL+"/webrtc-harness.html")
	// Headless: no X at all, and certainly not the host's display.
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "DISPLAY=") && !strings.HasPrefix(e, "WAYLAND_DISPLAY=") && !strings.HasPrefix(e, "XAUTHORITY=") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })

	var port string
	waitFor(t, "DevToolsActivePort", func() bool {
		b, err := os.ReadFile(prof + "/DevToolsActivePort")
		port, _, _ = strings.Cut(string(b), "\n")
		return err == nil && port != ""
	})
	var pages []struct{ Type, URL, WebSocketDebuggerURL string }
	waitFor(t, "page target", func() bool {
		resp, err := http.Get("http://127.0.0.1:" + port + "/json")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		_ = json.NewDecoder(resp.Body).Decode(&pages)
		for _, p := range pages {
			if p.Type == "page" && strings.Contains(p.URL, "webrtc-harness") {
				return true
			}
		}
		return false
	})
	var wsURL string
	for _, p := range pages {
		if p.Type == "page" && strings.Contains(p.URL, "webrtc-harness") {
			wsURL = p.WebSocketDebuggerURL
		}
	}
	cdp, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cdp.Close()
	id := 0
	call := func(method string, params any) json.RawMessage {
		id++
		_ = cdp.WriteJSON(map[string]any{"id": id, "method": method, "params": params})
		for {
			var m struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
			}
			if err := cdp.ReadJSON(&m); err != nil {
				t.Fatal(err)
			}
			if m.ID == id {
				return m.Result
			}
		}
	}
	eval := func(expr string) string {
		var r struct {
			Result struct {
				Value any `json:"value"`
			} `json:"result"`
		}
		_ = json.Unmarshal(call("Runtime.evaluate", map[string]any{"expression": "JSON.stringify(" + expr + ")", "returnByValue": true}), &r)
		s, _ := r.Result.Value.(string)
		return s
	}
	live := false
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end) && !live; time.Sleep(200 * time.Millisecond) {
		live = strings.Contains(eval("window.__states"), `"live:`)
	}
	if !live {
		t.Fatalf("viewer never went live: states %s", eval("window.__states"))
	}
	if sz := eval("window.__size"); sz != `{"w":1280,"h":720}` {
		t.Errorf("size message: %s", sz)
	}
	// The middle of the (letterboxed) video is the middle of the desktop.
	var vh float64
	_ = json.Unmarshal([]byte(eval("document.querySelector('video').getBoundingClientRect().height")), &vh)
	eval(`(() => { window.__sent = []; const o = RTCDataChannel.prototype.send; RTCDataChannel.prototype.send = function (d) { window.__sent.push(d); return o.call(this, d) }; return 1 })()`)
	var where string
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		call("Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": 640, "y": vh / 2})
		out, _ := onDisp("xdotool", "getmouselocation").Output()
		if where = string(out); strings.HasPrefix(where, "x:640 y:360") {
			break
		}
	}
	if !strings.HasPrefix(where, "x:640 y:360") {
		t.Fatalf("pointer at %q; page sent %s; rect %s", where, eval("window.__sent"), eval("document.querySelector('video').getBoundingClientRect()"))
	}

	// A window drag / edge resize: press, move with the button held, release.
	// Every move goes out with the button (no drag of the <video> eating it),
	// the host's pointer follows, and the right edge is the last pixel.
	var vw float64
	_ = json.Unmarshal([]byte(eval("document.querySelector('video').getBoundingClientRect().width")), &vw)
	eval(`(() => { window.__sent = []; return 1 })()`)
	call("Input.dispatchMouseEvent", map[string]any{"type": "mousePressed", "x": vw - 0.5, "y": vh / 2, "button": "left", "buttons": 1, "clickCount": 1})
	for i := 1; i <= 10; i++ {
		call("Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": vw - 0.5 - float64(i*20), "y": vh / 2, "button": "left", "buttons": 1})
	}
	// Where the last move lands (object-fit: contain may letterbox it sideways).
	sc := min(vw/1280, vh/720)
	want := fmt.Sprintf("x:%d ", int((vw-200.5-(vw-1280*sc)/2)/sc))
	var out []byte
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end) && !strings.HasPrefix(string(out), want); time.Sleep(50 * time.Millisecond) {
		out, _ = onDisp("xdotool", "getmouselocation").Output()
	}
	call("Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": vw - 200.5, "y": vh / 2, "button": "left", "buttons": 0, "clickCount": 1})
	var sent []string
	_ = json.Unmarshal([]byte(eval("window.__sent")), &sent)
	held := 0
	for _, m := range sent {
		if strings.Contains(m, `"m":1`) {
			held++
		}
	}
	if len(sent) < 12 || !strings.Contains(sent[0], `"x":1279,`) || !strings.Contains(sent[0], `"m":1`) || held != 11 || !strings.Contains(sent[len(sent)-1], `"m":0`) {
		t.Errorf("drag sent %v", sent)
	}
	if !strings.HasPrefix(string(out), want) {
		t.Errorf("pointer during the drag at %q, want %s", out, want)
	}

	// The host's cursor shape is the page's cursor: a window with the
	// "watch" cursor (hotspot 15,9) under the pointer.
	cursorWindow(t, onDisp)
	var css string
	for end := time.Now().Add(6 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		_ = json.Unmarshal([]byte(eval("document.querySelector('.hd-webrtc').style.cursor")), &css)
		if strings.Contains(css, "data:image/png;base64,") && strings.Contains(eval("window.__cursor"), `"x":15,"y":9`) {
			break
		}
	}
	if !strings.HasPrefix(css, `url("data:image/png;base64,`) || !strings.HasSuffix(css, ", default") {
		t.Errorf("CSS cursor %q, last message %s", css, eval("window.__cursor"))
	}
	time.Sleep(1500 * time.Millisecond)
	t.Logf("states %s, stats %s", eval("window.__states"), eval("window.__stats"))
	// The server's {"t":"enc"}: the preset and encoder actually running.
	if st := eval("window.__stats"); !strings.Contains(st, `"fps":`) || !strings.Contains(st, `"preset":"low"`) || !strings.Contains(st, `"encoder":"`+enc+`"`) {
		t.Errorf("stats %s", st)
	}

	// The host resizes mid-stream (a phone-shaped screen): same peer, the
	// new size, the picture at Low's 1280-wide cap, still decoding.
	resizeDesktop(t, disp, 2400, 1080)
	var video string
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		video = eval("[document.querySelector('video').videoWidth, document.querySelector('video').videoHeight]")
		if video == "[1280,576]" && eval("window.__size") == `{"w":2400,"h":1080}` {
			break
		}
	}
	if video != "[1280,576]" || eval("window.__size") != `{"w":2400,"h":1080}` {
		t.Errorf("after the resize: video %s, size %s", video, eval("window.__size"))
	}
	var f0, f1 float64
	_ = json.Unmarshal([]byte(eval("document.querySelector('video').getVideoPlaybackQuality().totalVideoFrames")), &f0)
	time.Sleep(2 * time.Second)
	_ = json.Unmarshal([]byte(eval("document.querySelector('video').getVideoPlaybackQuality().totalVideoFrames")), &f1)
	if f1-f0 < 20 {
		t.Errorf("%v frames in 2 s after the resize", f1-f0)
	}
	if st := eval("window.__states"); strings.Count(st, "live:") != 1 || strings.Contains(st, "reconnecting") {
		t.Errorf("re-offered across the resize: states %s", st)
	}
}

// TestIntegrationWebViewerInFirefox: the owner's browser (2026-10-16 bug:
// constant freezes and reloads). Headless Firefox (WEBRTC_IT_FIREFOX, the
// binary) with OpenH264 (WEBRTC_IT_OPENH264: a dir with libgmpopenh264.so +
// gmpopenh264.info from Mozilla's GMP download) plays the real viewer
// (harness above, WEBRTC_IT_UI) for 20 s with a preset switch: one peer the
// whole time (no re-offer), constrained baseline negotiated (all Firefox
// offers), frames decoded before and after the switch.
func TestIntegrationWebViewerInFirefox(t *testing.T) {
	ui, ff, gmp := os.Getenv("WEBRTC_IT_UI"), os.Getenv("WEBRTC_IT_FIREFOX"), os.Getenv("WEBRTC_IT_OPENH264")
	if ui == "" || ff == "" || gmp == "" {
		t.Skip("WEBRTC_IT_UI, WEBRTC_IT_FIREFOX or WEBRTC_IT_OPENH264 not set")
	}
	_, disp, onDisp := testDesktop(t)
	api := http.NewServeMux()
	RegisterHostStreamRoutes(api)
	target, _ := url.Parse(ui)
	var mu sync.Mutex
	var logs []map[string]any
	front := http.NewServeMux()
	front.Handle("/v1/vm-sidecar/", http.StripPrefix("/v1/vm-sidecar", api))
	front.HandleFunc("/__log", func(_ http.ResponseWriter, r *http.Request) {
		var m map[string]any
		if json.NewDecoder(r.Body).Decode(&m) == nil {
			mu.Lock()
			logs = append(logs, m)
			mu.Unlock()
		}
	})
	front.Handle("/", httputil.NewSingleHostReverseProxy(target))
	srv := httptest.NewServer(front)
	defer srv.Close()

	prof := t.TempDir()
	dir := prof + "/gmp-gmpopenh264/2.6.0"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"libgmpopenh264.so", "gmpopenh264.info"} {
		b, err := os.ReadFile(gmp + "/" + f)
		if err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(dir+"/"+f, b, 0o755)
	}
	_ = os.WriteFile(prof+"/user.js", []byte(`user_pref("media.gmp-gmpopenh264.version", "2.6.0");
user_pref("media.gmp-gmpopenh264.enabled", true);
user_pref("media.gmp-gmpopenh264.visible", true);
user_pref("media.gmp-gmpopenh264.autoupdate", false);
user_pref("media.gmp-manager.updateEnabled", false);
user_pref("media.autoplay.default", 0);
user_pref("media.peerconnection.ice.loopback", true);
user_pref("media.peerconnection.ice.obfuscate_host_addresses", false);
`), 0o644)
	cmd := exec.Command(ff, "--headless", "--no-remote", "--profile", prof, "--window-size", "1280,720",
		srv.URL+"/webrtc-harness.html?preset=low&switch=balanced")
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "DISPLAY=") && !strings.HasPrefix(e, "WAYLAND_DISPLAY=") && !strings.HasPrefix(e, "XAUTHORITY=") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "MOZ_HEADLESS=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
	// Mid-stream the owner's phone sizes (2026-10-19: 3398x1220 -> 1920x824),
	// a host cursor change and (at 15 s) a preset switch.
	time.Sleep(6 * time.Second)
	resizeDesktop(t, disp, 1920, 824)
	time.Sleep(4 * time.Second)
	cursorWindow(t, onDisp)
	time.Sleep(4 * time.Second)
	resizeDesktop(t, disp, 1080, 2400)
	mu.Lock()
	decAtResize := 0.0
	for _, m := range logs {
		if m["ev"] == "stats" {
			decAtResize, _ = m["dec"].(float64)
		}
	}
	mu.Unlock()
	time.Sleep(10 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	var sizes []string
	var lastW, lastH float64
	for _, m := range logs {
		switch m["ev"] {
		case "size":
			sizes = append(sizes, fmt.Sprint(m["w"], "x", m["h"]))
		case "stats":
			lastW, _ = m["vw"].(float64)
			lastH, _ = m["vh"].(float64)
		}
	}
	if !slicesContain(sizes, "1920x824") || sizes[len(sizes)-1] != "1080x2400" {
		t.Errorf("size messages %v", sizes)
	}
	if lastW != 1080 || lastH != 2400 { // Balanced: 1080 wide is under its cap
		t.Errorf("video %vx%v after the resize to 1080x2400", lastW, lastH)
	}
	var states []string
	var decEnd float64
	codec := ""
	cursorCSS := ""
	for _, m := range logs {
		switch m["ev"] {
		case "cursor":
			if m["x"] == 15.0 && m["y"] == 9.0 {
				cursorCSS, _ = m["css"].(string)
			}
		case "state":
			states = append(states, fmt.Sprint(m["s"], ":", m["r"]))
		case "stats":
			if m["pcs"] != 1.0 {
				t.Errorf("re-offered: %v (states %v)", m, states)
			}
			decEnd, _ = m["dec"].(float64)
			if c, _ := m["codec"].(string); c != "" {
				codec = c
			}
		}
	}
	if len(states) < 2 || states[1] != "live:" || slicesContain(states, "reconnecting") {
		t.Errorf("states %v", states)
	}
	if !strings.Contains(codec, "profile-level-id=42e01f") {
		t.Errorf("codec %q: Firefox offers constrained baseline only", codec)
	}
	if !strings.HasPrefix(cursorCSS, `url("data:image/png;base64,`) {
		t.Errorf("the window's cursor shape never became the page's cursor (%q)", cursorCSS)
	}
	if decEnd < 300 { // ~10 s at 20 fps (Low) + ~14 s at 30 (Balanced)
		t.Errorf("only %v frames decoded", decEnd)
	}
	if decEnd-decAtResize < 100 { // 10 s at 30 fps after the last resize
		t.Errorf("%v frames decoded after the last resize", decEnd-decAtResize)
	}
}

func slicesContain(ss []string, prefix string) bool {
	for _, s := range ss {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
