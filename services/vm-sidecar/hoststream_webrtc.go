// hoststream_webrtc.go: Host Desktop "Smooth (beta)" - the host screen over
// WebRTC, next to the noVNC lane (console.go), which stays the default and
// the fallback. Contract: /host/stream + /host/webrtc/offer, data channel
// "input" (JSON), one H.264 track.
//
//	capture + encode   gst-launch-1.0 child (ximagesrc on X11; on GNOME
//	                   Wayland our own mutter ScreenCast session, see
//	                   host-webrtc-wayland below) -> rtph264pay -> RTP on a
//	                   loopback UDP port
//	media              pion forwards those packets (TrackLocalStaticRTP);
//	                   ICE on one UDP port (28650), LAN/Tailscale only
//	input, clipboard   RFB messages on hostvnc.sock (x11vnc / the Wayland
//	                   helper) through hostInputTracker, which lets go of
//	                   held keys when the session ends
//
// Inert until a client offers: no socket, no child, nothing polled.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/pion/ice/v4"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// Knobs tests turn down; production values are the contract's.
var (
	webrtcUDPPort       = 28650
	webrtcAllowLoopback = false // tests only: offer 127.0.0.1 candidates
	webrtcRFBSocket     = hostVNCSocketPath
	webrtcHungAfter     = 3 * time.Second  // no RTP this long: restart the encoder
	webrtcPLIEvery      = 2 * time.Second  // at most one PLI restart per this
	webrtcLinger        = 10 * time.Second // peer gone (or never came) this long: end
	webrtcHeartbeat     = 2 * time.Second
	webrtcCapture       = resolveCapture
	gstLaunch           = "gst-launch-1.0"
	gstInspect          = "gst-inspect-1.0"
)

// Why Smooth is unavailable: en_US.json keys (the UI's keys are the
// English text).
const (
	reasonNoGstreamer        = "GStreamer is not installed - reinstall Host Desktop from the dashboard"
	reasonWaylandUnsupported = "Smooth video on Wayland works with GNOME only for now"
	reasonNoDisplay          = "No desktop session to stream"
	reasonMissingPlugins     = "GStreamer plugins for Smooth video are missing - reinstall Host Desktop from the dashboard"
	reasonNoEncoder          = "No H.264 video encoder found on this server"
	reasonVncDown            = "The Host Desktop VNC server is not running"
	reasonNoLanAddress       = "This server has no LAN or Tailscale address for Smooth video"
)

// ---------------------------------------------------------------------------
// Encoder probe
// ---------------------------------------------------------------------------

// webrtcEncoders in order of preference.
var webrtcEncoders = []string{"nvh264enc", "vah264enc", "x264enc"}

var gstElements = []string{"nvh264enc", "cudaupload", "cudaconvert", "vah264enc", "x264enc",
	"ximagesrc", "pipewiresrc", "videorate", "videoscale", "videoconvert", "rtph264pay", "udpsink"}

var gstProbe struct {
	sync.Mutex
	have map[string]bool
}

// probeGst: which GStreamer elements exist. Kept once a usable set was
// found; until then (packages still installing) it looks again each time.
func probeGst() map[string]bool {
	gstProbe.Lock()
	defer gstProbe.Unlock()
	if gstProbe.have != nil {
		return gstProbe.have
	}
	have := map[string]bool{}
	for _, e := range gstElements {
		have[e] = exec.Command(gstInspect, "--exists", e).Run() == nil
	}
	if have["rtph264pay"] && pickEncoder(have, nil) != "" {
		gstProbe.have = have
	}
	return have
}

// pickEncoder: the first encoder that exists and hasn't failed this session.
func pickEncoder(have, broken map[string]bool) string {
	for _, e := range webrtcEncoders {
		if !have[e] || broken[e] || (e == "nvh264enc" && !(have["cudaupload"] && have["cudaconvert"])) {
			continue
		}
		return e
	}
	return ""
}

// ---------------------------------------------------------------------------
// What to capture
// ---------------------------------------------------------------------------

type captureTarget struct {
	x11        hostXTarget // X11: Display set
	waylandUID string      // GNOME Wayland: the session user
	reason     string      // not capturable: one of the reasons above
}

// resolveCapture reads the display the same way the rest of Host Desktop
// does: the wrapper script's state file, else the live session.
func resolveCapture() captureTarget {
	if readHostDesktopState()["SESSION_TYPE"] == "wayland" {
		b, _ := os.ReadFile(hostDesktopWaylandStatusPath)
		// ponytail: GNOME only. KDE/portal would need a second consent (its
		// restore token is single-use and shared with the helper); wlroots
		// has no PipeWire node here. Hand the helper's fd over to add them.
		if parseKeyValueLines(string(b))["BACKEND"] != "mutter" {
			return captureTarget{reason: reasonWaylandUnsupported}
		}
		if uid := activeSeatSession().UID; uid != "" {
			return captureTarget{waylandUID: uid}
		}
		return captureTarget{reason: reasonNoDisplay}
	}
	t := resolveHostX()
	if t.Display == "" || t.XWayland {
		return captureTarget{reason: reasonNoDisplay}
	}
	return captureTarget{x11: t}
}

// webrtcAvailability: the encoder the next session starts with, or why
// there can't be one.
func webrtcAvailability(c captureTarget, vncUp bool) (enc, reason string) {
	if _, err := exec.LookPath(gstLaunch); err != nil {
		return "", reasonNoGstreamer
	}
	if c.reason != "" {
		return "", c.reason
	}
	have := probeGst()
	need := []string{"rtph264pay", "udpsink", "videoconvert", "videoscale", "ximagesrc"}
	if c.waylandUID != "" {
		need = []string{"rtph264pay", "udpsink", "videoconvert", "videoscale", "pipewiresrc", "videorate"}
	}
	for _, e := range need {
		if !have[e] {
			return "", reasonMissingPlugins
		}
	}
	if enc = pickEncoder(have, nil); enc == "" {
		return "", reasonNoEncoder
	}
	if !vncUp {
		return enc, reasonVncDown // input and size go through it
	}
	return enc, ""
}

// ---------------------------------------------------------------------------
// gst-launch argv
// ---------------------------------------------------------------------------

type encParams struct {
	Encoder   string
	FPS, Kbps int
	W, H      int   // framebuffer size
	Seq       int   // first RTP sequence number, -1: random
	TS        int64 // first RTP timestamp, -1: random
	Port      int   // loopback RTP port
}

// encodeSize: even (4:2:0), and at most 1920 wide on x264 (a CPU core
// per 3398x1220@30 otherwise).
func encodeSize(enc string, w, h int) (int, int) {
	if enc == "x264enc" && w > 1920 {
		h, w = h*1920/w, 1920
	}
	return w &^ 1, h &^ 1
}

// encodeFPS: x264 tops out at 30.
func encodeFPS(enc string, fps int) int {
	if enc == "x264enc" {
		return min(fps, 30)
	}
	return fps
}

// gstTail: everything after the source's raw frames, ending in RTP to
// 127.0.0.1:Port.
func gstTail(p encParams) []string {
	fps := encodeFPS(p.Encoder, p.FPS)
	gop := strconv.Itoa(2 * fps) // keyframe floor; PLI restarts the encoder
	kb := strconv.Itoa(p.Kbps)
	a := []string{"!", "queue", "leaky=downstream", "max-size-buffers=1", "max-size-bytes=0", "max-size-time=0"}
	if w, h := encodeSize(p.Encoder, p.W, p.H); w > 0 && (w != p.W || h != p.H) {
		a = append(a, "!", "videoscale", "n-threads=0", "!", fmt.Sprintf("video/x-raw,width=%d,height=%d,pixel-aspect-ratio=1/1", w, h))
	}
	switch p.Encoder {
	case "nvh264enc":
		// Capped VBR: an idle screen costs ~0.2 Mbit/s instead of the cap.
		a = append(a, "!", "cudaupload", "!", "cudaconvert", "!", "nvh264enc", "preset=p1", "tune=ultra-low-latency",
			"rc-mode=vbr", "const-quality=23", "bitrate="+kb, "max-bitrate="+kb, "gop-size="+gop, "bframes=0", "zerolatency=true",
			"!", "video/x-h264,profile=main")
	case "vah264enc":
		a = append(a, "!", "videoconvert", "n-threads=0", "!", "video/x-raw,format=NV12", "!", "vah264enc", "rate-control=vbr",
			"bitrate="+kb, "key-int-max="+gop, "b-frames=0", "target-usage=7", "!", "video/x-h264,profile=main")
	default:
		a = append(a, "!", "videoconvert", "n-threads=0", "!", "video/x-raw,format=I420", "!", "x264enc", "tune=zerolatency",
			"speed-preset=ultrafast", "bitrate="+kb, "vbv-buf-capacity=1000", "key-int-max="+gop, "bframes=0",
			"!", "video/x-h264,profile=constrained-baseline")
	}
	a = append(a, "!", "rtph264pay", "config-interval=-1", "pt=96", "mtu=1200", "aggregate-mode=zero-latency")
	if p.Seq >= 0 {
		a = append(a, "seqnum-offset="+strconv.Itoa(p.Seq))
	}
	if p.TS >= 0 {
		a = append(a, "timestamp-offset="+strconv.FormatInt(p.TS, 10))
	}
	return append(a, "!", "udpsink", "host=127.0.0.1", "port="+strconv.Itoa(p.Port), "sync=false", "async=false")
}

func x11Argv(display string, p encParams) []string {
	a := []string{"-q", "ximagesrc", "display-name=" + display, "use-damage=false", "show-pointer=false",
		"!", fmt.Sprintf("video/x-raw,framerate=%d/1", encodeFPS(p.Encoder, p.FPS))}
	return append(a, gstTail(p)...)
}

// waylandSource: pipewiresrc only sends on damage; keepalive + videorate
// keep a still screen flowing so it doesn't read as a hung encoder.
func waylandSource(node uint32, fps int) []string {
	return []string{"pipewiresrc", fmt.Sprintf("path=%d", node), "keepalive-time=500", "!", "videorate",
		"!", fmt.Sprintf("video/x-raw,framerate=%d/1", fps)}
}

// encoderCmd: the child for one encoder run.
func encoderCmd(c captureTarget, p encParams) *exec.Cmd {
	var cmd *exec.Cmd
	if c.waylandUID != "" {
		self, _ := os.Executable()
		args := append([]string{"host-webrtc-wayland", "--uid", c.waylandUID, "--fps", strconv.Itoa(encodeFPS(p.Encoder, p.FPS)), "--"}, gstTail(p)...)
		cmd = exec.Command(self, args...)
	} else {
		cmd = exec.Command(gstLaunch, x11Argv(c.x11.Display, p)...)
		cmd.Env = xEnv(c.x11)
	}
	cmd.Stderr = os.Stderr
	// A dead sidecar must never leak an encoder (or an NVENC session).
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return cmd
}

// host-webrtc-wayland: run as a child of the sidecar, drops to the session
// user, opens its own mutter ScreenCast session (cursor hidden) and runs
// pipewiresrc + the encoder tail until either ends.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "host-webrtc-wayland" {
		return
	}
	log.SetPrefix("host-webrtc-wayland: ")
	log.SetFlags(0)
	os.Exit(runHostWebRTCWayland(os.Args[2:]))
}

func runHostWebRTCWayland(args []string) int {
	fs := flag.NewFlagSet("host-webrtc-wayland", flag.ContinueOnError)
	uid := fs.String("uid", "", "session user's uid")
	fps := fs.Int("fps", 30, "frames per second")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	u, err := user.LookupId(*uid)
	if err != nil {
		log.Print(err)
		return 1
	}
	wl := findWaylandDisplay("/run/user/" + u.Uid)
	if wl == "" {
		log.Print("no wayland socket")
		return 1
	}
	conn, err := becomeSessionUser(u, wl)
	if err != nil {
		log.Print(err)
		return 1
	}
	defer conn.Close()
	sess, err := startMutterSession(conn, 0)
	if err != nil {
		log.Print(err)
		return 1
	}
	defer sess.stop()
	cmd := exec.Command(gstLaunch, append(append([]string{"-q"}, waylandSource(sess.node, *fps)...), fs.Args()...)...)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		log.Print(err)
		return 1
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	select {
	case <-exited:
	case <-sess.closed:
		_ = cmd.Process.Kill()
		<-exited
	}
	return 1
}

// ---------------------------------------------------------------------------
// RFB (input, clipboard, size) on hostvnc.sock
// ---------------------------------------------------------------------------

type rfbConn struct {
	mu sync.Mutex // writes come from the data channel and the heartbeat
	t  *hostInputTracker
	r  *bufio.Reader
	w  int
	h  int
}

// dialHostRFB: an RFB 3.8 client, security None (all the root-only socket
// offers). Every byte goes through hostInputTracker so it can release what
// this viewer held when it closes.
func dialHostRFB(path string) (*rfbConn, error) {
	nc, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c := &rfbConn{t: &hostInputTracker{Conn: nc}, r: bufio.NewReader(nc)}
	if err := c.handshake(); err != nil {
		nc.Close()
		return nil, err
	}
	return c, nil
}

func (c *rfbConn) handshake() error {
	_ = c.t.Conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer c.t.Conn.SetDeadline(time.Time{})
	ver := make([]byte, 12)
	if _, err := io.ReadFull(c.r, ver); err != nil {
		return err
	}
	if !strings.HasPrefix(string(ver), "RFB 003.00") || ver[10] < '7' {
		return fmt.Errorf("unsupported RFB server %q", ver)
	}
	if err := c.send([]byte("RFB 003.008\n"), false); err != nil {
		return err
	}
	n, err := c.r.ReadByte()
	if err != nil || n == 0 {
		return errors.New("RFB server refused the connection")
	}
	types := make([]byte, n)
	if _, err := io.ReadFull(c.r, types); err != nil {
		return err
	}
	if !strings.ContainsRune(string(types), 1) {
		return errors.New("RFB server wants a password")
	}
	if err := c.send([]byte{1}, false); err != nil { // None
		return err
	}
	res := make([]byte, 4)
	if _, err := io.ReadFull(c.r, res); err != nil || binary.BigEndian.Uint32(res) != 0 {
		return errors.New("RFB security handshake failed")
	}
	if err := c.send([]byte{1}, false); err != nil { // shared
		return err
	}
	hdr := make([]byte, 24)
	if _, err := io.ReadFull(c.r, hdr); err != nil {
		return err
	}
	c.w, c.h = int(binary.BigEndian.Uint16(hdr)), int(binary.BigEndian.Uint16(hdr[2:]))
	if _, err := c.r.Discard(int(binary.BigEndian.Uint32(hdr[20:]))); err != nil {
		return err
	}
	// 32 bpp true colour (so a Raw rect is w*h*4), and DesktopSize.
	pf := []byte{0, 0, 0, 0, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0}
	enc := []byte{2, 0, 0, 2, 0, 0, 0, 0}
	enc = binary.BigEndian.AppendUint32(enc, uint32(0xFFFFFF21)) // -223 DesktopSize
	return c.send(append(pf, enc...), false)
}

// send writes b; input marks viewer activity (CapsLock watcher, relock),
// the 1x1 size polls and handshake don't.
func (c *rfbConn) send(b []byte, input bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.t.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if input {
		_, err := c.t.Write(b)
		return err
	}
	c.t.feed(b)
	_, err := c.t.Conn.Write(b)
	return err
}

// pollSize asks for a 1x1 incremental update: the server answers a resize
// with DesktopSize, and otherwise with at most that one pixel.
// ponytail: the Wayland helper ignores the region and sends its dirty
// tiles raw (local socket, every 2 s); ask x11vnc-only if that shows up.
func (c *rfbConn) pollSize() error {
	return c.send([]byte{3, 1, 0, 0, 0, 0, 0, 1, 0, 1}, false)
}

func (c *rfbConn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.t.Close() // releases held keys/buttons first
}

// readRFBServer parses server messages until an error: DesktopSize ->
// onSize, ServerCutText (Latin-1) -> onClip; pixels are skipped.
func readRFBServer(r *bufio.Reader, onSize func(w, h int), onClip func(string)) error {
	b := make([]byte, 16)
	for {
		t, err := r.ReadByte()
		if err != nil {
			return err
		}
		switch t {
		case 0: // FramebufferUpdate
			if _, err := io.ReadFull(r, b[:3]); err != nil {
				return err
			}
			for n := binary.BigEndian.Uint16(b[1:]); n > 0; n-- {
				if _, err := io.ReadFull(r, b[:12]); err != nil {
					return err
				}
				w, h := int(binary.BigEndian.Uint16(b[4:])), int(binary.BigEndian.Uint16(b[6:]))
				switch int32(binary.BigEndian.Uint32(b[8:])) {
				case 0: // Raw, 32 bpp
					if _, err := r.Discard(w * h * 4); err != nil {
						return err
					}
				case -223:
					onSize(w, h)
				default:
					return fmt.Errorf("unexpected RFB encoding %d", int32(binary.BigEndian.Uint32(b[8:])))
				}
			}
		case 1: // SetColourMapEntries
			if _, err := io.ReadFull(r, b[:5]); err != nil {
				return err
			}
			if _, err := r.Discard(6 * int(binary.BigEndian.Uint16(b[3:]))); err != nil {
				return err
			}
		case 2: // Bell
		case 3: // ServerCutText
			if _, err := io.ReadFull(r, b[:7]); err != nil {
				return err
			}
			n := binary.BigEndian.Uint32(b[3:])
			if n > 16<<20 {
				return errors.New("RFB clipboard too large")
			}
			text := make([]byte, n)
			if _, err := io.ReadFull(r, text); err != nil {
				return err
			}
			onClip(latin1ToString(text))
		default:
			return fmt.Errorf("unexpected RFB message %d", t)
		}
	}
}

// dcMsg: one data channel message, either direction.
type dcMsg struct {
	T    string `json:"t"`
	X    int    `json:"x,omitempty"`
	Y    int    `json:"y,omitempty"`
	M    int    `json:"m,omitempty"`
	KS   uint32 `json:"ks,omitempty"`
	D    bool   `json:"d,omitempty"`
	Text string `json:"text,omitempty"`
	W    int    `json:"w,omitempty"`
	H    int    `json:"h,omitempty"`
	Code string `json:"code,omitempty"`
}

func clampU16(v int) uint16 { return uint16(max(0, min(v, 0xffff))) }

// inputToRFB: the RFB bytes for a client message ("release" is handled by
// the caller: it needs the tracker's state).
func inputToRFB(m dcMsg) []byte {
	switch m.T {
	case "p":
		b := []byte{5, byte(m.M)}
		b = binary.BigEndian.AppendUint16(b, clampU16(m.X))
		return binary.BigEndian.AppendUint16(b, clampU16(m.Y))
	case "k":
		d := byte(0)
		if m.D {
			d = 1
		}
		return binary.BigEndian.AppendUint32([]byte{4, d, 0, 0}, m.KS)
	case "clip":
		b := legacyCutText(m.Text) // ServerCutText layout; Latin-1 (diag A4)
		b[0] = 6                   // ClientCutText
		return b
	}
	return nil
}

// ---------------------------------------------------------------------------
// Session
// ---------------------------------------------------------------------------

type webrtcSession struct {
	pc     *webrtc.PeerConnection
	mux    io.Closer
	track  io.Writer
	rfb    *rfbConn
	claims *jwt.Claims
	cap    captureTarget
	fps    int
	kbps   int
	have   map[string]bool
	spawn  func(encParams) *exec.Cmd // encoderCmd; tests swap it

	sizeMu sync.Mutex
	w, h   int

	dc      atomic.Pointer[webrtc.DataChannel]
	restart chan string
	stop    chan struct{}
	once    sync.Once
	started atomic.Bool
	lingerT *time.Timer
}

// webrtcOfferMu serializes offers; webrtcState.cur is the one session.
var webrtcOfferMu sync.Mutex

var webrtcState struct {
	sync.Mutex
	cur *webrtcSession
}

func (s *webrtcSession) size() (int, int) {
	s.sizeMu.Lock()
	defer s.sizeMu.Unlock()
	return s.w, s.h
}

func (s *webrtcSession) send(m dcMsg) {
	if dc := s.dc.Load(); dc != nil && dc.ReadyState() == webrtc.DataChannelStateOpen {
		b, _ := json.Marshal(m)
		_ = dc.SendText(string(b))
	}
}

func (s *webrtcSession) requestRestart(why string) {
	select {
	case s.restart <- why:
	default:
	}
}

func (s *webrtcSession) close() {
	s.once.Do(func() {
		close(s.stop)
		if s.lingerT != nil {
			s.lingerT.Stop()
		}
		if s.pc != nil {
			_ = s.pc.Close()
		}
		if s.mux != nil {
			_ = s.mux.Close()
		}
		if s.rfb != nil {
			s.rfb.close()
			noteHostConsoleClosed()
		}
		webrtcState.Lock()
		if webrtcState.cur == s {
			webrtcState.cur = nil
		}
		webrtcState.Unlock()
	})
}

func (s *webrtcSession) linger() {
	if s.lingerT != nil {
		s.lingerT.Reset(webrtcLinger)
	}
}

func (s *webrtcSession) onInput(b []byte) {
	var m dcMsg
	if json.Unmarshal(b, &m) != nil {
		return
	}
	if m.T == "release" {
		s.rfb.mu.Lock()
		rel := s.rfb.t.releases()
		s.rfb.mu.Unlock()
		if len(rel) > 0 {
			_ = s.rfb.send(rel, true)
		}
		return
	}
	if out := inputToRFB(m); out != nil {
		_ = s.rfb.send(out, true)
	}
}

// claimsLive: the token's session hasn't been ended (logout, device
// removed, password changed) since the offer.
func claimsLive(c *jwt.Claims) bool {
	if c == nil {
		return true
	}
	if _, revoked := jwt.SessionRevoked(c.SessionID); revoked {
		return false
	}
	return jwt.CheckSession(c) == nil
}

// heartbeat: hb to the client, the revocation check, and the size poll.
func (s *webrtcSession) heartbeat() {
	t := time.NewTicker(webrtcHeartbeat)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
		if !claimsLive(s.claims) {
			log.Printf("host webrtc: token session ended - closing the stream")
			s.close()
			return
		}
		s.send(dcMsg{T: "hb"})
		if s.rfb.pollSize() != nil {
			s.close()
			return
		}
	}
}

func (s *webrtcSession) readRFB() {
	err := readRFBServer(s.rfb.r, func(w, h int) {
		s.sizeMu.Lock()
		changed := w != s.w || h != s.h
		s.w, s.h = w, h
		s.sizeMu.Unlock()
		if changed {
			s.send(dcMsg{T: "size", W: w, H: h})
			s.requestRestart("size")
		}
	}, func(text string) { s.send(dcMsg{T: "clip", Text: text}) })
	select {
	case <-s.stop:
	default:
		log.Printf("host webrtc: VNC socket: %v - closing the stream", err)
		s.close() // input is gone: the client re-offers
	}
}

// rtpState: the last packet forwarded, so a restarted encoder continues
// the sequence numbers and timestamps instead of jumping.
type rtpState struct {
	sync.Mutex
	n   int64
	seq uint16
	ts  uint32
	at  time.Time
}

func (r *rtpState) note(b []byte) {
	r.Lock()
	r.n++
	r.seq, r.ts, r.at = binary.BigEndian.Uint16(b[2:]), binary.BigEndian.Uint32(b[4:]), time.Now()
	r.Unlock()
}

// next: the offsets for the next encoder run (-1: none yet, random).
func (r *rtpState) next(now time.Time) (seq int, ts int64, packets int64, last time.Time) {
	r.Lock()
	defer r.Unlock()
	if r.n == 0 {
		return -1, -1, 0, time.Time{}
	}
	return int(r.seq + 1), int64(r.ts + uint32(now.Sub(r.at).Milliseconds()*90) + 1), r.n, r.at
}

// encoderHung: no packet for webrtcHungAfter since the later of the start
// and the last packet.
func encoderHung(now, started, lastPkt time.Time) bool {
	ref := started
	if lastPkt.After(ref) {
		ref = lastPkt
	}
	return now.Sub(ref) > webrtcHungAfter
}

// superviseEncoder runs the encoder child until the session ends,
// restarting it on exit, on a hang, on a resize and (rate-limited) on PLI.
// An encoder that twice produces nothing is dropped for the next one.
func (s *webrtcSession) superviseEncoder() {
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		log.Printf("host webrtc: rtp listen: %v", err)
		s.send(dcMsg{T: "err", Code: "encoder_failed"})
		return
	}
	defer udp.Close()
	port := udp.LocalAddr().(*net.UDPAddr).Port
	var rs rtpState
	go func() {
		b := make([]byte, 1500)
		for {
			n, err := udp.Read(b)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}
			rs.note(b[:n])
			_, _ = s.track.Write(b[:n])
		}
	}()

	broken := map[string]bool{}
	fails := 0
	for {
		enc := pickEncoder(s.have, broken)
		if enc == "" {
			log.Printf("host webrtc: every encoder failed")
			s.send(dcMsg{T: "err", Code: "encoder_failed"})
			return
		}
		w, h := s.size()
		seq, ts, before, _ := rs.next(time.Now())
		cmd := s.spawn(encParams{Encoder: enc, FPS: s.fps, Kbps: s.kbps, W: w, H: h, Seq: seq, TS: ts, Port: port})
		started := time.Now()
		if err := cmd.Start(); err != nil {
			log.Printf("host webrtc: %s: %v", enc, err)
			broken[enc] = true
			continue
		}
		exited := make(chan struct{})
		go func() { _ = cmd.Wait(); close(exited) }()
		why := s.waitEncoder(exited, started, &rs)
		_ = cmd.Process.Kill()
		<-exited
		if why == "stop" {
			return
		}
		if _, _, after, _ := rs.next(time.Now()); after == before {
			if fails++; fails >= 2 {
				log.Printf("host webrtc: %s produced no video twice - trying the next encoder", enc)
				broken[enc], fails = true, 0
			}
		} else {
			fails = 0
		}
		log.Printf("host webrtc: restarting %s (%s)", enc, why)
		if why == "exit" || why == "hung" {
			select {
			case <-s.stop:
				return
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
}

// waitEncoder: why the current run ends - "stop", "exit", "hung", "pli",
// "size".
func (s *webrtcSession) waitEncoder(exited <-chan struct{}, started time.Time, rs *rtpState) string {
	tick := time.NewTicker(webrtcHungAfter / 6)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return "stop"
		case <-exited:
			return "exit"
		case why := <-s.restart:
			// A new run starts with a keyframe anyway; PLIs right after
			// one are the browser still waiting for it.
			if why == "pli" && time.Since(started) < webrtcPLIEvery {
				continue
			}
			return why
		case now := <-tick.C:
			_, _, _, last := rs.next(now)
			if encoderHung(now, started, last) {
				return "hung"
			}
		}
	}
}

// ---------------------------------------------------------------------------
// ICE on one UDP port, LAN and Tailscale only
// ---------------------------------------------------------------------------

var webrtcAllowedNets = func() []*net.IPNet {
	var out []*net.IPNet
	// RFC 1918, Tailscale's CGNAT range, IPv6 ULA (Tailscale's fd7a::/48 too).
	for _, c := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "fc00::/7"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

func webrtcIPAllowed(ip net.IP) bool {
	if ip.IsLoopback() {
		return webrtcAllowLoopback
	}
	for _, n := range webrtcAllowedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// webrtcInterfaceAllowed: not container/VM bridges, whose private
// addresses aren't the LAN.
func webrtcInterfaceAllowed(name string) bool {
	if name == "lo" {
		return webrtcAllowLoopback
	}
	for _, p := range []string{"docker", "br-", "veth", "virbr", "vnet", "tap", "macvtap", "cni", "flannel", "podman", "lxc", "incus"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return true
}

func newWebRTCAPI() (*webrtc.API, io.Closer, error) {
	opts := []ice.UDPMuxFromPortOption{
		ice.UDPMuxFromPortWithInterfaceFilter(webrtcInterfaceAllowed),
		ice.UDPMuxFromPortWithIPFilter(webrtcIPAllowed),
		ice.UDPMuxFromPortWithNetworks(ice.NetworkTypeUDP4, ice.NetworkTypeUDP6),
	}
	if webrtcAllowLoopback {
		opts = append(opts, ice.UDPMuxFromPortWithLoopback())
	}
	mux, err := ice.NewMultiUDPMuxFromPort(webrtcUDPPort, opts...)
	if err != nil {
		return nil, nil, err
	}
	if len(mux.GetListenAddresses()) == 0 {
		_ = mux.Close()
		return nil, nil, errors.New("no LAN or Tailscale address")
	}
	var se webrtc.SettingEngine
	se.SetICEUDPMux(mux)
	se.SetInterfaceFilter(webrtcInterfaceAllowed)
	se.SetIPFilter(webrtcIPAllowed)
	se.SetIncludeLoopbackCandidate(webrtcAllowLoopback)
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	return webrtc.NewAPI(webrtc.WithSettingEngine(se)), mux, nil
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

func writeWebRTCUnavailable(w http.ResponseWriter, reason string) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error_code": "webrtc_unavailable", "reason": reason})
}

func handleHostStream(w http.ResponseWriter, r *http.Request) {
	vnc := socketListening(webrtcRFBSocket)
	enc, reason := webrtcAvailability(webrtcCapture(), vnc)
	writeJSON(w, http.StatusOK, map[string]any{
		"modes": map[string]any{
			"vnc":    map[string]any{"available": vnc},
			"webrtc": map[string]any{"available": reason == "", "encoder": enc, "reason": reason},
			"nova":   map[string]any{"available": false, "url": ""},
		},
		"udp_port": webrtcUDPPort,
	})
}

func handleWebRTCOffer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SDP     string `json:"sdp"`
		Type    string `json:"type"`
		FPS     int    `json:"fps"`
		MaxKbps int    `json:"max_kbps"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil || req.Type != "offer" || req.SDP == "" {
		writeError(w, http.StatusBadRequest, errors.New("body must be {\"sdp\":...,\"type\":\"offer\"}"))
		return
	}
	if req.FPS != 60 {
		req.FPS = 30
	}
	if req.MaxKbps <= 0 {
		req.MaxKbps = 12000
	}
	req.MaxKbps = max(500, min(req.MaxKbps, 100000))

	c := webrtcCapture()
	if _, reason := webrtcAvailability(c, true); reason != "" {
		writeWebRTCUnavailable(w, reason)
		return
	}

	webrtcOfferMu.Lock()
	defer webrtcOfferMu.Unlock()
	webrtcState.Lock()
	old := webrtcState.cur
	webrtcState.Unlock()
	if old != nil { // one viewer: the new offer takes over
		old.close()
	}

	s, err := newWebRTCSession(c, req.FPS, req.MaxKbps, requestClaims(r))
	if err != nil {
		log.Printf("host webrtc: %v", err)
		writeWebRTCUnavailable(w, reasonVncDown)
		return
	}
	answer, err := s.answer(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: req.SDP})
	if err != nil {
		s.close()
		log.Printf("host webrtc: offer: %v", err)
		if errors.Is(err, errNoICEPort) {
			writeWebRTCUnavailable(w, reasonNoLanAddress)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	webrtcState.Lock()
	webrtcState.cur = s
	webrtcState.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"sdp": answer.SDP, "type": "answer"})
}

var errNoICEPort = errors.New("ICE port")

func newWebRTCSession(c captureTarget, fps, kbps int, claims *jwt.Claims) (*webrtcSession, error) {
	rfb, err := dialHostRFB(webrtcRFBSocket)
	if err != nil {
		return nil, fmt.Errorf("VNC socket: %w", err)
	}
	hostConsoleSessions.Add(1)
	noteHostConsoleInput()
	s := &webrtcSession{rfb: rfb, claims: claims, cap: c, fps: fps, kbps: kbps, have: probeGst(),
		w: rfb.w, h: rfb.h, restart: make(chan string, 1), stop: make(chan struct{})}
	s.spawn = func(p encParams) *exec.Cmd { return encoderCmd(s.cap, p) }
	return s, nil
}

// answer sets up the peer for offer and returns the complete
// (non-trickle) answer.
func (s *webrtcSession) answer(offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	api, mux, err := newWebRTCAPI()
	if err != nil {
		return nil, fmt.Errorf("%w %d: %v", errNoICEPort, webrtcUDPPort, err)
	}
	s.mux = mux
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	s.pc = pc
	// ponytail: the SDP says constrained baseline for every encoder (what
	// every browser offers); nvenc/va send Main, which they all decode.
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}, "video", "hostdesk")
	if err != nil {
		return nil, err
	}
	s.track = track
	sender, err := pc.AddTrack(track)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			pkts, _, err := sender.ReadRTCP()
			if err != nil {
				return
			}
			for _, p := range pkts {
				switch p.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					s.requestRestart("pli")
				}
			}
		}
	}()
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "input" {
			return
		}
		dc.OnOpen(func() {
			s.dc.Store(dc)
			w, h := s.size()
			s.send(dcMsg{T: "size", W: w, H: h})
		})
		dc.OnMessage(func(m webrtc.DataChannelMessage) { s.onInput(m.Data) })
		dc.OnClose(func() {
			if s.dc.CompareAndSwap(dc, nil) {
				s.onInput([]byte(`{"t":"release"}`))
			}
		})
	})
	s.lingerT = time.AfterFunc(webrtcLinger, s.close) // never connects: end
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		switch st {
		case webrtc.PeerConnectionStateConnected:
			s.lingerT.Stop()
			if s.started.CompareAndSwap(false, true) {
				go s.superviseEncoder()
			}
		case webrtc.PeerConnectionStateDisconnected:
			s.linger()
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			go s.close()
		}
	})
	if err := pc.SetRemoteDescription(offer); err != nil {
		return nil, err
	}
	ans, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(ans); err != nil {
		return nil, err
	}
	select {
	case <-gathered:
	case <-time.After(5 * time.Second):
		return nil, errors.New("ICE gathering timed out")
	}
	go s.readRFB()
	go s.heartbeat()
	return pc.LocalDescription(), nil
}

func RegisterHostStreamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /host/stream", handleHostStream)
	mux.HandleFunc("POST /host/webrtc/offer", handleWebRTCOffer)
}
