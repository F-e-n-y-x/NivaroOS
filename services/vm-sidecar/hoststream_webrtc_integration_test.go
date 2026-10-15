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
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

func gstRunning(display string) bool {
	out, _ := exec.Command("pgrep", "-f", "gst-launch-1.0 -q ximagesrc display-name="+display+" ").Output()
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
	startBG(t, "Xvfb", disp, "-screen", "0", "1280x720x24", "-nolisten", "tcp")
	waitFor(t, "Xvfb", func() bool { return displaySocketExists(disp) })
	// X clients of the test only ever see the throwaway display.
	onDisp = func(name string, args ...string) *exec.Cmd {
		c := exec.Command(name, args...)
		c.Env = append(os.Environ(), "DISPLAY="+disp, "XAUTHORITY=/dev/null")
		return c
	}
	sock := t.TempDir() + "/vnc.sock"
	startBG(t, "x11vnc", "-display", disp, "-rfbport", "0", "-rfbportv6", "-1", "-unixsock", sock, "-noipv6", "-nopw",
		"-forever", "-shared", "-nonap", "-quiet")
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
	waitForLong("size + heartbeat + enc", 6*time.Second, func() bool {
		for {
			select {
			case m := <-msgs1:
				if m.T == "size" && m.W == 1280 && m.H == 720 {
					gotSize = true
				}
				if m.T == "hb" {
					gotHB = true
				}
				// Low: 1280 wide at most, so the 1280x720 test screen stays as is.
				if m.T == "enc" && m.Encoder == enc && m.Preset == "low" && m.W == 1280 && m.Kbps > 0 {
					gotEnc = true
				}
				continue
			default:
			}
			return gotSize && gotHB && gotEnc
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
// 18093) with two scratch files in ui/ (not committed):
//
//	webrtc-harness.html: <div id="app"></div><script type="module" src="/webrtc-harness.js"></script>
//	webrtc-harness.js:
//	  import { createApp, h } from 'vue'
//	  import Viewer from '@/shell/desktop/HostDesktopWebRTC.vue'
//	  window.__states = []
//	  localStorage.setItem('access_token', 'test')
//	  createApp({ render: () => h('div', { style: 'position:fixed;inset:0' }, [h(Viewer, {
//	    fallback: false, preset: 'low',
//	    onState: (s) => window.__states.push(s.state + ':' + s.reason),
//	    onSize: (s) => (window.__size = s), onStats: (s) => (window.__stats = s) })]) }).mount('#app')
func TestIntegrationWebViewerInChrome(t *testing.T) {
	ui := os.Getenv("WEBRTC_IT_UI")
	if ui == "" {
		t.Skip("WEBRTC_IT_UI not set")
	}
	chrome, err := exec.LookPath("google-chrome")
	if err != nil {
		t.Skip("no google-chrome")
	}
	enc, _, onDisp := testDesktop(t)
	api := http.NewServeMux()
	RegisterHostStreamRoutes(api)
	target, _ := url.Parse(ui)
	front := http.NewServeMux()
	front.Handle("/v1/vm-sidecar/", http.StripPrefix("/v1/vm-sidecar", api))
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
	time.Sleep(1500 * time.Millisecond)
	t.Logf("states %s, stats %s", eval("window.__states"), eval("window.__stats"))
	// The server's {"t":"enc"}: the preset and encoder actually running.
	if st := eval("window.__stats"); !strings.Contains(st, `"fps":`) || !strings.Contains(st, `"preset":"low"`) || !strings.Contains(st, `"encoder":"`+enc+`"`) {
		t.Errorf("stats %s", st)
	}
}
