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
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestIntegrationWebRTCOfferStreamsAndInjects(t *testing.T) {
	for _, b := range []string{"Xvfb", "x11vnc", "xdotool", "xprop", gstLaunch} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s not found", b)
		}
	}
	enc := os.Getenv("WEBRTC_IT_ENCODER")
	if enc == "" {
		enc = "x264enc"
	}
	disp := freeDisplay(t)
	startBG(t, "Xvfb", disp, "-screen", "0", "1280x720x24", "-nolisten", "tcp")
	waitFor(t, "Xvfb", func() bool { return displaySocketExists(disp) })
	// X clients of the test only ever see the throwaway display.
	onDisp := func(name string, args ...string) *exec.Cmd {
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
	if st.Modes["webrtc"]["available"] != true || st.Modes["webrtc"]["encoder"] != enc {
		t.Fatalf("/host/stream: %v", st.Modes)
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
		body, _ := json.Marshal(map[string]any{"sdp": pc.LocalDescription().SDP, "type": "offer", "fps": 30, "max_kbps": 4000})
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
	gotSize, gotHB := false, false
	waitForLong("size + heartbeat", 6*time.Second, func() bool {
		for {
			select {
			case m := <-msgs1:
				if m.T == "size" && m.W == 1280 && m.H == 720 {
					gotSize = true
				}
				if m.T == "hb" {
					gotHB = true
				}
				continue
			default:
			}
			return gotSize && gotHB
		}
	})

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
