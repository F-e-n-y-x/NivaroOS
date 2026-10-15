// Prototype (not product code): Host Desktop over WebRTC.
// Video: ffmpeg x11grab -> h264_nvenc (or libx264) -> RTP on loopback UDP -> pion forwards to the peer.
// Cursor: not drawn into the video (it would add a frame of lag to the pointer); the page shows the local cursor.
// Input: data channel -> RFB PointerEvent/KeyEvent written to the existing VNC unix socket
// (the same socket the sidecar's /host/console proxies), so every input backend is reused.
// One viewer at a time: a new offer closes the previous peer and its encoder.
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pion/webrtc/v4"
)

var (
	listen  = flag.String("listen", "127.0.0.1:14698", "http listen (signaling + page)")
	udpPort = flag.Int("udp", 28650, "single UDP port for all ICE traffic")
	udpIP   = flag.String("udp-ip", "127.0.0.1", "IP to bind the ICE UDP port on")
	display = flag.String("display", ":97", "X display to capture (read-only)")
	size    = flag.String("size", "3398x1220", "capture size")
	fps     = flag.Int("fps", 60, "capture fps")
	enc     = flag.String("enc", "nvenc", "nvenc|x264")
	kbps    = flag.Int("kbps", 12000, "bitrate")
	rfbSock = flag.String("rfb", "", "RFB unix socket for input (empty = view only)")
	pipe    = flag.String("pipeline", "", "custom encoder command run via sh -c; {host} {port} = RTP target (e.g. a gst-launch ... ! rtph264pay ! udpsink pipeline)")
	xtestIn = flag.Bool("xtest", false, "inject input via XTEST on -display instead of RFB")
)

func encoderArgs(rtpURL string) []string {
	in := []string{"-loglevel", "error", "-f", "x11grab", "-draw_mouse", "0", "-framerate", strconv.Itoa(*fps), "-video_size", *size, "-i", *display}
	br := strconv.Itoa(*kbps) + "k"
	// ponytail: fixed 2 s GOP is the loss-recovery ceiling (ffmpeg can't take a PLI);
	// production pipeline should force a keyframe on PLI (gst force-key-unit or encoder restart).
	gop := strconv.Itoa(*fps * 2)
	if *enc == "x264" {
		return append(in, "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-pix_fmt", "yuv420p",
			"-b:v", br, "-maxrate", br, "-bufsize", br, "-g", gop, "-bf", "0", "-f", "rtp", "-payload_type", "96", rtpURL)
	}
	return append(in, "-c:v", "h264_nvenc", "-preset", "p1", "-tune", "ull", "-zerolatency", "1", "-rc", "vbr", "-cq", "23",
		"-b:v", br, "-maxrate", br, "-bufsize", br, "-g", gop, "-bf", "0", "-delay", "0", "-profile:v", "main", "-f", "rtp", "-payload_type", "96", rtpURL)
}

type session struct {
	pc  *webrtc.PeerConnection
	cmd *exec.Cmd
	rfb net.Conn
	x   *xinput
}

func (s *session) close() {
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	if s.rfb != nil {
		s.rfb.Close()
	}
	if s.x != nil {
		s.x.c.Close()
	}
	s.pc.Close()
}

var (
	mu   sync.Mutex
	cur  *session
	api  *webrtc.API
)

func main() {
	flag.Parse()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(*udpIP), Port: *udpPort})
	if err != nil {
		log.Fatal(err)
	}
	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
	se.SetIncludeLoopbackCandidate(*udpIP == "127.0.0.1")
	se.SetNAT1To1IPs([]string{*udpIP}, webrtc.ICECandidateTypeHost)
	api = webrtc.NewAPI(webrtc.WithSettingEngine(se))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "index.html") })
	http.HandleFunc("/offer", offer)
	log.Printf("listening on http://%s, ICE udp %s:%d", *listen, *udpIP, *udpPort)
	log.Fatal(http.ListenAndServe(*listen, nil))
}

func offer(w http.ResponseWriter, r *http.Request) {
	var o webrtc.SessionDescription
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if cur != nil { // single viewer: takeover
		cur.close()
		cur = nil
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s := &session{pc: pc}
	track, _ := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "desk")
	sender, _ := pc.AddTrack(track)
	go func() { // drain RTCP so interceptors (NACK) run
		b := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(b); err != nil {
				return
			}
		}
	}()
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnMessage(func(m webrtc.DataChannelMessage) { s.input(m.Data) })
	})
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		log.Printf("peer %s", st)
		if st == webrtc.PeerConnectionStateConnected {
			s.start(track)
		}
		if st == webrtc.PeerConnectionStateFailed || st == webrtc.PeerConnectionStateClosed {
			mu.Lock()
			if cur == s {
				cur = nil
			}
			mu.Unlock()
			s.close()
		}
	})
	if err := pc.SetRemoteDescription(o); err != nil {
		pc.Close()
		http.Error(w, err.Error(), 400)
		return
	}
	ans, _ := pc.CreateAnswer(nil)
	done := webrtc.GatheringCompletePromise(pc)
	pc.SetLocalDescription(ans)
	<-done
	cur = s
	json.NewEncoder(w).Encode(pc.LocalDescription())
}

func (s *session) start(track *webrtc.TrackLocalStaticRTP) {
	if *xtestIn {
		if x, err := newXInput(*display); err != nil {
			log.Printf("xtest input: %v", err)
		} else {
			s.x = x
		}
	} else if *rfbSock != "" {
		if c, err := rfbConnect(*rfbSock); err != nil {
			log.Printf("rfb input: %v", err)
		} else {
			s.rfb = c
		}
	}
	// ffmpeg packetizes RTP itself (correct marker bits and timestamps, no Annex-B framing delay);
	// we only forward the packets. Loopback port picked by the kernel.
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		log.Printf("rtp listen: %v", err)
		return
	}
	cmd := exec.Command("ffmpeg", encoderArgs("rtp://"+udp.LocalAddr().String()+"?pkt_size=1200")...)
	if *pipe != "" {
		a := udp.LocalAddr().(*net.UDPAddr)
		cmd = exec.Command("sh", "-c", strings.NewReplacer("{host}", a.IP.String(), "{port}", strconv.Itoa(a.Port)).Replace(*pipe))
	}
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL} // never leak an NVENC session
	if err := cmd.Start(); err != nil {
		udp.Close()
		log.Printf("encoder: %v", err)
		return
	}
	s.cmd = cmd
	go func() {
		defer udp.Close()
		go cmd.Wait()
		b := make([]byte, 1500)
		for {
			udp.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, _, err := udp.ReadFrom(b)
			if err != nil {
				log.Printf("rtp: %v", err)
				return
			}
			if _, err := track.Write(b[:n]); err != nil {
				return
			}
		}
	}()
}

// ---- input: minimal RFB 3.8 client, security None, never requests frames ----

func rfbConnect(path string) (net.Conn, error) {
	c, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 12)
	if _, err := io.ReadFull(c, buf); err != nil {
		return nil, err
	}
	c.Write([]byte("RFB 003.008\n"))
	n := make([]byte, 1)
	io.ReadFull(c, n)
	types := make([]byte, n[0])
	io.ReadFull(c, types)
	c.Write([]byte{1}) // None
	res := make([]byte, 4)
	io.ReadFull(c, res)
	if binary.BigEndian.Uint32(res) != 0 {
		return nil, errors.New("rfb security failed")
	}
	c.Write([]byte{1}) // shared
	hdr := make([]byte, 24)
	io.ReadFull(c, hdr)
	name := make([]byte, binary.BigEndian.Uint32(hdr[20:]))
	io.ReadFull(c, name)
	go io.Copy(io.Discard, c) // ponytail: drops ServerCutText; parse it when clipboard is wired
	return c, nil
}

type inMsg struct {
	T string `json:"t"`
	X int    `json:"x"`
	Y int    `json:"y"`
	M int    `json:"m"`
	K uint32 `json:"k"`
	D bool   `json:"d"`
}

func (s *session) input(b []byte) {
	var m inMsg
	if json.Unmarshal(b, &m) != nil {
		return
	}
	if s.x != nil {
		if m.T == "p" {
			s.x.pointer(m.X, m.Y, m.M)
		} else if m.T == "k" {
			s.x.key(m.K, m.D)
		}
		return
	}
	if s.rfb == nil {
		return
	}
	switch m.T {
	case "p":
		s.rfb.Write([]byte{5, byte(m.M), byte(m.X >> 8), byte(m.X), byte(m.Y >> 8), byte(m.Y)})
	case "k":
		d := byte(0)
		if m.D {
			d = 1
		}
		s.rfb.Write([]byte{4, d, 0, 0, byte(m.K >> 24), byte(m.K >> 16), byte(m.K >> 8), byte(m.K)})
	}
}
