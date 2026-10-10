// hostvnc_wayland.go: Host Desktop on a Wayland session.
//
// x11vnc can only capture Xorg, so on Wayland the wrapper script
// (hostdesktop/nivaroos-host-desktop.sh) runs
//
//	nivaroos-vm-sidecar host-vnc-wayland --uid N --desktop D --socket S --status F
//
// as root. It serves plain RFB on the same root-only unix socket x11vnc
// uses, so /host/console, the web noVNC panel and the app's RFB client
// work unchanged. Backend per compositor:
//
//	GNOME (mutter)         mutter's own RemoteDesktop + ScreenCast D-Bus API:
//	                       no consent dialog, keysym input.
//	wlroots-style (sway,   wayvnc, exec'd on the socket directly.
//	Hyprland, labwc...)
//	anything else (KDE     xdg-desktop-portal RemoteDesktop + ScreenCast: the
//	Plasma 6, ...)         host user clicks "Allow" once; the restore token
//	                       is kept so later starts don't ask again.
//
// Frames come from PipeWire through a gst-launch-1.0 child (pipewiresrc ->
// BGRx on a pipe); this file is the small RFB server (Raw + Zlib,
// DesktopSize) on top of it. While waiting for consent / after a refusal
// it says so in the status file (KEY=value, read by GetHostDesktopStatus).
package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

// Exit codes the wrapper script acts on.
const (
	wlExitDenied  = 3 // the host user declined the sharing request
	wlExitMissing = 4 // a needed package isn't installed (yet)
	wlExitLocked  = 5 // GNOME: the screen is locked
)

// hostDesktopWaylandStatusPath: written by the helper, read by the sidecar.
const hostDesktopWaylandStatusPath = "/run/nivaroos/host-desktop-wayland.status"

func init() {
	if len(os.Args) < 2 || os.Args[1] != "host-vnc-wayland" {
		return
	}
	log.SetPrefix("host-vnc-wayland: ")
	log.SetFlags(0)
	os.Exit(runHostVNCWayland(os.Args[2:]))
}

// wlrootsCompositors speak wlr-screencopy + virtual keyboard/pointer, which
// is what wayvnc needs.
var wlrootsCompositors = []string{"sway", "Hyprland", "labwc", "river", "wayfire", "dwl", "hikari"}

// pickWaylandBackend: procs are the command names (/proc/PID/comm) running
// as the session user, desktop is logind's Desktop= (XDG_CURRENT_DESKTOP)
// for when /proc is hidden.
func pickWaylandBackend(procs map[string]bool, desktop string) string {
	if procs["gnome-shell"] {
		return "mutter"
	}
	for _, c := range wlrootsCompositors {
		if procs[c] {
			return "wayvnc"
		}
	}
	if procs["kwin_wayland"] {
		return "portal"
	}
	d := strings.ToLower(desktop)
	if strings.Contains(d, "gnome") {
		return "mutter"
	}
	for _, c := range wlrootsCompositors {
		if strings.Contains(d, strings.ToLower(c)) {
			return "wayvnc"
		}
	}
	return "portal"
}

// userProcs: comm names of processes owned by uid.
func userProcs(uid int) map[string]bool {
	out := map[string]bool{}
	dirs, _ := filepath.Glob("/proc/[0-9]*")
	for _, d := range dirs {
		st, err := os.Stat(d)
		if err != nil {
			continue
		}
		if s, ok := st.Sys().(*syscall.Stat_t); !ok || int(s.Uid) != uid {
			continue
		}
		if b, err := os.ReadFile(d + "/comm"); err == nil {
			out[strings.TrimSpace(string(b))] = true
		}
	}
	return out
}

// findWaylandDisplay: the first wayland-N socket in the user's runtime dir.
func findWaylandDisplay(runtimeDir string) string {
	m, _ := filepath.Glob(filepath.Join(runtimeDir, "wayland-*"))
	sort.Strings(m)
	for _, p := range m {
		if strings.HasSuffix(p, ".lock") {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.Mode()&os.ModeSocket != 0 {
			return filepath.Base(p)
		}
	}
	return ""
}

type wlStatus struct{ f *os.File }

func (s wlStatus) set(state, backend, reason string) {
	if s.f == nil {
		return
	}
	b := fmt.Sprintf("STATE=%s\nBACKEND=%s\nREASON=%s\n", state, backend, strings.ReplaceAll(reason, "\n", " "))
	_ = s.f.Truncate(0)
	_, _ = s.f.WriteAt([]byte(b), 0)
}

func runHostVNCWayland(args []string) int {
	fs := flag.NewFlagSet("host-vnc-wayland", flag.ContinueOnError)
	uidStr := fs.String("uid", "", "session user's uid")
	desktop := fs.String("desktop", "", "logind Desktop= of the session")
	sock := fs.String("socket", hostVNCSocketPath, "unix socket to serve RFB on")
	statusPath := fs.String("status", hostDesktopWaylandStatusPath, "status file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	u, err := user.LookupId(*uidStr)
	if err != nil {
		log.Printf("unknown session user %q: %v", *uidStr, err)
		return 1
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	var st wlStatus
	if f, err := os.OpenFile(*statusPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644); err == nil {
		st.f = f
	}

	runtimeDir := "/run/user/" + u.Uid
	wl := findWaylandDisplay(runtimeDir)
	if wl == "" {
		st.set("error", "", "The Wayland session of "+u.Username+" has no wayland socket in "+runtimeDir+".")
		return 1
	}
	backend := pickWaylandBackend(userProcs(uid), *desktop)
	log.Printf("session of %s on %s, backend %s", u.Username, wl, backend)

	if backend == "wayvnc" {
		bin, err := exec.LookPath("wayvnc")
		if err != nil {
			st.set("missing", backend, "wayvnc is not installed - reinstall Host Desktop from the dashboard to add Wayland support.")
			return wlExitMissing
		}
		st.set("running", backend, "")
		// Root (like x11vnc) so the socket is created root-only 0600 in
		// /run/nivaroos; it connects to the user's compositor socket.
		syscall.Umask(0o077)
		env := append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir, "WAYLAND_DISPLAY="+wl)
		// No config (root's would be someone else's), control socket next
		// to ours rather than in the user's runtime dir.
		ctl := filepath.Join(filepath.Dir(*sock), "wayvncctl")
		err = syscall.Exec(bin, []string{"wayvnc", "-C", "/dev/null", "-S", ctl, "--unix-socket", *sock}, env)
		log.Printf("exec wayvnc: %v", err)
		return 1
	}

	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		st.set("missing", backend, "GStreamer (gst-launch-1.0 and its PipeWire plugin) is not installed - reinstall Host Desktop from the dashboard to add Wayland support.")
		return wlExitMissing
	}

	// Bind the socket as root (0600 root, like x11vnc's), listen only once
	// the first frame is in: until then a connect is refused and the panel
	// shows the status reason instead of an endless "Connecting...".
	fd, err := bindUnixSocket(*sock)
	if err != nil {
		log.Printf("bind %s: %v", *sock, err)
		return 1
	}

	// The session bus only accepts its own user; PipeWire and the portal
	// should see that user too.
	if err := dropPrivileges(u, uid, gid); err != nil {
		log.Printf("dropping to %s: %v", u.Username, err)
		return 1
	}
	os.Setenv("HOME", u.HomeDir)
	os.Setenv("USER", u.Username)
	os.Setenv("LOGNAME", u.Username)
	os.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	os.Setenv("WAYLAND_DISPLAY", wl)
	os.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+runtimeDir+"/bus")

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		st.set("error", backend, "Could not reach the desktop session's D-Bus: "+err.Error())
		return 1
	}
	defer conn.Close()

	var sess *wlSession
	switch backend {
	case "mutter":
		sess, err = startMutterSession(conn)
	default:
		tokenFile := filepath.Join(u.HomeDir, ".local/state/nivaroos/host-desktop-restore-token")
		sess, err = startPortalSession(conn, tokenFile, func() {
			st.set("consent", backend, "Waiting for someone at the server to allow screen sharing: click \"Allow\"/\"Share\" in the dialog on the server's own screen (needed once).")
		})
		if errors.Is(err, errPortalDenied) {
			st.set("denied", backend, "Screen sharing was declined on the server. Use \"Ask again\" to show the request on the server's screen again.")
			return wlExitDenied
		}
	}
	if err != nil && strings.Contains(err.Error(), "inhibited") {
		// GNOME refuses (and ends) screen sharing while the screen is locked.
		st.set("locked", backend, "The server's screen is locked, and GNOME doesn't share a locked screen. Unlock it on the server, or use \"Unlock the server's screen\" here.")
		return wlExitLocked
	}
	if err != nil {
		st.set("error", backend, "Starting the "+backend+" screen-sharing session failed: "+err.Error())
		log.Printf("%s: %v", backend, err)
		return 1
	}

	srv := newRFBServer(sess.input)
	fatal := make(chan error, 2)
	listening := false
	go func() {
		fatal <- capturePipeWire(sess.node, sess.pwFD, func(w, h int, frame []byte) {
			srv.pushFrame(w, h, frame)
			if !listening {
				listening = true
				ln, err := listenUnixFD(fd, *sock)
				if err != nil {
					fatal <- err
					return
				}
				st.set("running", backend, "")
				go srv.serve(ln)
			}
		})
	}()
	select {
	case <-sess.closed:
		log.Printf("the desktop ended the screen-sharing session")
		st.set("error", backend, "The desktop ended the screen-sharing session (screen locked or logged out?) - reconnecting.")
		return 1
	case err := <-fatal:
		log.Printf("capture: %v", err)
		st.set("error", backend, "Screen capture stopped: "+fmt.Sprint(err))
		return 1
	}
}

func bindUnixSocket(path string) (int, error) {
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	_ = os.Remove(path)
	old := syscall.Umask(0o077)
	err = syscall.Bind(fd, &syscall.SockaddrUnix{Name: path})
	syscall.Umask(old)
	if err != nil {
		syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

func listenUnixFD(fd int, path string) (net.Listener, error) {
	if err := syscall.Listen(fd, 16); err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	return net.FileListener(f)
}

func dropPrivileges(u *user.User, uid, gid int) error {
	var groups []int
	if ids, err := u.GroupIds(); err == nil {
		for _, g := range ids {
			if n, err := strconv.Atoi(g); err == nil {
				groups = append(groups, n)
			}
		}
	}
	if err := syscall.Setgroups(groups); err != nil {
		return err
	}
	if err := syscall.Setgid(gid); err != nil {
		return err
	}
	if err := syscall.Setuid(uid); err != nil {
		return err
	}
	// A setuid process is "not dumpable": its /proc/PID stays root's, and
	// xdg-desktop-portal, which reads it to identify the caller, refuses.
	if _, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 1, 0); e != 0 {
		return e
	}
	return nil
}

// ---------------------------------------------------------------------------
// Desktop sessions (mutter / portal)
// ---------------------------------------------------------------------------

// wlInput injects the viewer's input into the session.
type wlInput interface {
	Motion(x, y float64)
	Button(evdev int32, down bool)
	Scroll(axis uint32, steps int32)
	Key(keysym uint32, down bool)
}

type wlSession struct {
	node   uint32
	pwFD   *os.File // portal only: the PipeWire remote to read the node from
	input  wlInput
	closed chan struct{}
}

// watchClosed closes ch when a signal matching iface.member arrives on
// path, or the bus connection goes away.
func watchClosed(conn *dbus.Conn, path dbus.ObjectPath, iface, member string) chan struct{} {
	_ = conn.AddMatchSignal(dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(iface), dbus.WithMatchMember(member))
	sig := make(chan *dbus.Signal, 16)
	conn.Signal(sig)
	closed := make(chan struct{})
	go func() {
		for s := range sig {
			if s.Path == path && s.Name == iface+"."+member {
				break
			}
		}
		close(closed)
	}()
	return closed
}

const (
	mutterRD = "org.gnome.Mutter.RemoteDesktop"
	mutterSC = "org.gnome.Mutter.ScreenCast"
)

type mutterInput struct {
	sess   dbus.BusObject
	stream string
}

func (m mutterInput) call(method string, args ...interface{}) {
	m.sess.Call(mutterRD+".Session."+method, dbus.FlagNoReplyExpected, args...)
}
func (m mutterInput) Motion(x, y float64) {
	m.call("NotifyPointerMotionAbsolute", m.stream, x, y)
}
func (m mutterInput) Button(b int32, down bool)    { m.call("NotifyPointerButton", b, down) }
func (m mutterInput) Scroll(axis uint32, n int32)  { m.call("NotifyPointerAxisDiscrete", axis, n) }
func (m mutterInput) Key(keysym uint32, down bool) { m.call("NotifyKeyboardKeysym", keysym, down) }

func startMutterSession(conn *dbus.Conn) (*wlSession, error) {
	var rdPath dbus.ObjectPath
	if err := conn.Object(mutterRD, "/org/gnome/Mutter/RemoteDesktop").Call(mutterRD+".CreateSession", 0).Store(&rdPath); err != nil {
		return nil, fmt.Errorf("RemoteDesktop.CreateSession: %w", err)
	}
	rd := conn.Object(mutterRD, rdPath)
	idv, err := rd.GetProperty(mutterRD + ".Session.SessionId")
	if err != nil {
		return nil, fmt.Errorf("SessionId: %w", err)
	}
	var scPath dbus.ObjectPath
	if err := conn.Object(mutterSC, "/org/gnome/Mutter/ScreenCast").Call(mutterSC+".CreateSession", 0,
		map[string]dbus.Variant{"remote-desktop-session-id": idv}).Store(&scPath); err != nil {
		return nil, fmt.Errorf("ScreenCast.CreateSession: %w", err)
	}
	var streamPath dbus.ObjectPath
	// "" = the primary monitor; cursor-mode 1 = drawn into the frames.
	if err := conn.Object(mutterSC, scPath).Call(mutterSC+".Session.RecordMonitor", 0, "",
		map[string]dbus.Variant{"cursor-mode": dbus.MakeVariant(uint32(1))}).Store(&streamPath); err != nil {
		return nil, fmt.Errorf("RecordMonitor: %w", err)
	}
	_ = conn.AddMatchSignal(dbus.WithMatchObjectPath(streamPath), dbus.WithMatchInterface(mutterSC+".Stream"), dbus.WithMatchMember("PipeWireStreamAdded"))
	added := make(chan *dbus.Signal, 4)
	conn.Signal(added)
	defer conn.RemoveSignal(added)
	closed := watchClosed(conn, rdPath, mutterRD+".Session", "Closed")

	if err := rd.Call(mutterRD+".Session.Start", 0).Err; err != nil {
		return nil, fmt.Errorf("Session.Start: %w", err)
	}
	timeout := time.After(15 * time.Second)
	for {
		select {
		case s := <-added:
			if s.Path != streamPath || len(s.Body) != 1 {
				continue
			}
			node, _ := s.Body[0].(uint32)
			return &wlSession{node: node, input: mutterInput{sess: rd, stream: string(streamPath)}, closed: closed}, nil
		case <-closed:
			return nil, errors.New("session closed before its stream started")
		case <-timeout:
			return nil, errors.New("no PipeWire stream within 15s")
		}
	}
}

const (
	portalBus = "org.freedesktop.portal.Desktop"
	portalRD  = "org.freedesktop.portal.RemoteDesktop"
	portalSC  = "org.freedesktop.portal.ScreenCast"
)

var errPortalDenied = errors.New("screen sharing was declined")

type portalInput struct {
	obj     dbus.BusObject
	session dbus.ObjectPath
	stream  uint32
}

var noOpts = map[string]dbus.Variant{}

// call waits for each reply: fire-and-forget calls can reach the compositor
// out of order through the portal, losing keys (seen on KDE).
func (p portalInput) call(method string, args ...interface{}) {
	p.obj.Call(portalRD+"."+method, 0, append([]interface{}{p.session, noOpts}, args...)...)
}
func (p portalInput) Motion(x, y float64) { p.call("NotifyPointerMotionAbsolute", p.stream, x, y) }
func (p portalInput) Button(b int32, down bool) {
	p.call("NotifyPointerButton", b, boolU32(down))
}
func (p portalInput) Scroll(axis uint32, n int32) { p.call("NotifyPointerAxisDiscrete", axis, n) }
func (p portalInput) Key(keysym uint32, down bool) {
	p.call("NotifyKeyboardKeysym", int32(keysym), boolU32(down))
}

func boolU32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

var portalTokenN int

// portalRequest makes a portal call whose answer comes as a Response signal
// on a Request object; opts gets the handle_token. Blocks until answered.
func portalRequest(conn *dbus.Conn, method string, opts map[string]dbus.Variant, args ...interface{}) (map[string]dbus.Variant, error) {
	portalTokenN++
	token := fmt.Sprintf("nivaroos%d_%d", os.Getpid(), portalTokenN)
	opts["handle_token"] = dbus.MakeVariant(token)
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	path := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + sender + "/" + token)
	_ = conn.AddMatchSignal(dbus.WithMatchObjectPath(path), dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response"))
	sig := make(chan *dbus.Signal, 4)
	conn.Signal(sig)
	defer conn.RemoveSignal(sig)
	if err := conn.Object(portalBus, "/org/freedesktop/portal/desktop").Call(method, 0, append(args, opts)...).Err; err != nil {
		return nil, err
	}
	for s := range sig {
		if s.Path != path || len(s.Body) != 2 {
			continue
		}
		code, _ := s.Body[0].(uint32)
		results, _ := s.Body[1].(map[string]dbus.Variant)
		if code != 0 {
			return nil, errPortalDenied
		}
		return results, nil
	}
	return nil, errors.New("session bus connection closed")
}

func startPortalSession(conn *dbus.Conn, tokenFile string, askingUser func()) (*wlSession, error) {
	res, err := portalRequest(conn, portalRD+".CreateSession", map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant(fmt.Sprintf("nivaroos%d", os.Getpid())),
	})
	if err != nil {
		return nil, fmt.Errorf("CreateSession: %w", err)
	}
	var session dbus.ObjectPath
	switch v := res["session_handle"].Value().(type) {
	case string:
		session = dbus.ObjectPath(v)
	case dbus.ObjectPath:
		session = v
	}
	if session == "" {
		return nil, errors.New("CreateSession returned no session")
	}
	closed := watchClosed(conn, session, "org.freedesktop.portal.Session", "Closed")

	// keyboard|pointer, remembered until revoked (persist_mode 2).
	devOpts := map[string]dbus.Variant{"types": dbus.MakeVariant(uint32(3)), "persist_mode": dbus.MakeVariant(uint32(2))}
	if tok, err := os.ReadFile(tokenFile); err == nil && len(bytes.TrimSpace(tok)) > 0 {
		devOpts["restore_token"] = dbus.MakeVariant(string(bytes.TrimSpace(tok)))
	}
	if _, err := portalRequest(conn, portalRD+".SelectDevices", devOpts, session); err != nil {
		return nil, fmt.Errorf("SelectDevices: %w", err)
	}
	srcOpts := map[string]dbus.Variant{"types": dbus.MakeVariant(uint32(1)), "multiple": dbus.MakeVariant(false)}
	if v, err := conn.Object(portalBus, "/org/freedesktop/portal/desktop").GetProperty(portalSC + ".AvailableCursorModes"); err == nil {
		if m, _ := v.Value().(uint32); m&2 != 0 {
			srcOpts["cursor_mode"] = dbus.MakeVariant(uint32(2)) // embedded
		}
	}
	if _, err := portalRequest(conn, portalSC+".SelectSources", srcOpts, session); err != nil {
		return nil, fmt.Errorf("SelectSources: %w", err)
	}
	askingUser()
	res, err = portalRequest(conn, portalRD+".Start", map[string]dbus.Variant{}, session, "")
	if err != nil {
		if errors.Is(err, errPortalDenied) {
			_ = os.Remove(tokenFile)
		}
		return nil, err
	}
	if tok, ok := res["restore_token"].Value().(string); ok && tok != "" {
		_ = os.MkdirAll(filepath.Dir(tokenFile), 0o700)
		_ = os.WriteFile(tokenFile, []byte(tok+"\n"), 0o600)
	}
	node, ok := firstPortalStream(res["streams"].Value())
	if !ok {
		return nil, errors.New("the portal returned no screen stream")
	}
	var fd dbus.UnixFD
	if err := conn.Object(portalBus, "/org/freedesktop/portal/desktop").Call(portalSC+".OpenPipeWireRemote", 0, session, noOpts).Store(&fd); err != nil {
		return nil, fmt.Errorf("OpenPipeWireRemote: %w", err)
	}
	obj := conn.Object(portalBus, "/org/freedesktop/portal/desktop")
	return &wlSession{node: node, pwFD: os.NewFile(uintptr(fd), "pipewire"), input: portalInput{obj: obj, session: session, stream: node}, closed: closed}, nil
}

// firstPortalStream pulls the node id out of Start's a(ua{sv}) streams.
func firstPortalStream(v interface{}) (uint32, bool) {
	switch s := v.(type) {
	case [][]interface{}:
		if len(s) > 0 && len(s[0]) > 0 {
			n, ok := s[0][0].(uint32)
			return n, ok
		}
	case []interface{}:
		if len(s) > 0 {
			return firstPortalStream([][]interface{}{toIfaceSlice(s[0])})
		}
	}
	return 0, false
}

func toIfaceSlice(v interface{}) []interface{} {
	s, _ := v.([]interface{})
	return s
}

// ---------------------------------------------------------------------------
// PipeWire capture
// ---------------------------------------------------------------------------

var gstCapsRe = regexp.MustCompile(`GstCapsFilter:nvout\.GstPad:src: caps = video/x-raw.*width=\(int\)(\d+), height=\(int\)(\d+)`)

// capturePipeWire runs the GStreamer child until the stream ends, handing
// over every BGRx frame. A size change restarts the child (its frames are
// a raw byte stream, sized by the caps it printed).
func capturePipeWire(node uint32, pwFD *os.File, onFrame func(w, h int, frame []byte)) error {
	failures := 0
	for {
		started := time.Now()
		err := runGstOnce(node, pwFD, onFrame)
		if time.Since(started) > 30*time.Second {
			failures = 0
		}
		failures++
		if failures > 5 {
			return err
		}
		if err != nil {
			log.Printf("gstreamer: %v - restarting", err)
		}
		time.Sleep(time.Second)
	}
}

func runGstOnce(node uint32, pwFD *os.File, onFrame func(w, h int, frame []byte)) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	defer pr.Close()
	src := []string{"pipewiresrc", fmt.Sprintf("path=%d", node)}
	extra := []*os.File{pw} // fd 3: frames
	if pwFD != nil {
		src = append(src, "fd=4")
		extra = append(extra, pwFD)
	}
	args := append([]string{"-v"}, src...)
	args = append(args, "!", "videoconvert", "!", "capsfilter", "name=nvout", "caps=video/x-raw,format=BGRx", "!", "fdsink", "fd=3", "sync=false")
	cmd := exec.Command("gst-launch-1.0", args...)
	cmd.ExtraFiles = extra
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		pw.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		pw.Close()
		return err
	}
	pw.Close()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	sizes := make(chan [2]int, 8)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if m := gstCapsRe.FindStringSubmatch(sc.Text()); m != nil {
				w, _ := strconv.Atoi(m[1])
				h, _ := strconv.Atoi(m[2])
				sizes <- [2]int{w, h}
			}
		}
		close(sizes)
	}()
	var cur [2]int
	select {
	case s, ok := <-sizes:
		if !ok {
			return errors.New("gst-launch-1.0 exited before negotiating a format")
		}
		cur = s
	case <-time.After(30 * time.Second):
		return errors.New("no video format from PipeWire within 30s")
	}
	if cur[0] <= 0 || cur[1] <= 0 || cur[0] > 16384 || cur[1] > 16384 {
		return fmt.Errorf("bad frame size %dx%d", cur[0], cur[1])
	}
	frame := make([]byte, cur[0]*cur[1]*4)
	for {
		if _, err := io.ReadFull(pr, frame); err != nil {
			return fmt.Errorf("reading frames: %w", err)
		}
		select {
		case s, ok := <-sizes:
			// ponytail: the frame read alongside a renegotiation may be
			// misframed; restarting at the new size fixes it a moment later.
			if ok && s != cur {
				return nil
			}
		default:
		}
		onFrame(cur[0], cur[1], frame)
	}
}

// ---------------------------------------------------------------------------
// RFB server
// ---------------------------------------------------------------------------

const rfbTile = 64

var rfbVersionRe = regexp.MustCompile(`^RFB 003\.00(\d)`)

type pixelFormat struct {
	BPP, Depth, BigEndian, TrueColor uint8
	RMax, GMax, BMax                 uint16
	RShift, GShift, BShift           uint8
}

// serverPF matches the BGRx frames byte for byte.
var serverPF = pixelFormat{BPP: 32, Depth: 24, TrueColor: 1, RMax: 255, GMax: 255, BMax: 255, RShift: 16, GShift: 8, BShift: 0}

type rfbClient struct {
	conn        net.Conn
	pf          pixelFormat
	zlib        bool
	desktopSize bool
	want        bool
	resized     bool
	closed      bool
	dirty       []bool
	buttons     uint8
}

type rfbServer struct {
	mu      sync.Mutex
	cond    *sync.Cond
	w, h    int
	fb      []byte
	clients map[*rfbClient]bool
	in      wlInput
}

func newRFBServer(in wlInput) *rfbServer {
	s := &rfbServer{clients: map[*rfbClient]bool{}, in: in}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func tilesFor(w, h int) (int, int) { return (w + rfbTile - 1) / rfbTile, (h + rfbTile - 1) / rfbTile }

func allTrue(n int) []bool {
	b := make([]bool, n)
	for i := range b {
		b[i] = true
	}
	return b
}

// pushFrame records a new frame and marks what changed for every client.
func (s *rfbServer) pushFrame(w, h int, frame []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, ty := tilesFor(w, h)
	if w != s.w || h != s.h {
		s.w, s.h = w, h
		s.fb = append(s.fb[:0], frame...)
		for c := range s.clients {
			c.resized = true
			c.dirty = allTrue(tx * ty)
		}
		s.cond.Broadcast()
		return
	}
	stride := w * 4
	changed := false
	for j := 0; j < ty; j++ {
		for i := 0; i < tx; i++ {
			x0, y0 := i*rfbTile, j*rfbTile
			x1, y1 := min(x0+rfbTile, w), min(y0+rfbTile, h)
			diff := false
			for y := y0; y < y1; y++ {
				a, b := y*stride+x0*4, y*stride+x1*4
				if !bytes.Equal(s.fb[a:b], frame[a:b]) {
					diff = true
					copy(s.fb[a:b], frame[a:b])
					for y2 := y + 1; y2 < y1; y2++ {
						a, b := y2*stride+x0*4, y2*stride+x1*4
						copy(s.fb[a:b], frame[a:b])
					}
					break
				}
			}
			if diff {
				changed = true
				for c := range s.clients {
					c.dirty[j*tx+i] = true
				}
			}
		}
	}
	if changed {
		s.cond.Broadcast()
	}
}

func (s *rfbServer) serve(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			return
		}
		go s.handle(c)
	}
}

func readFull(r io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

func (s *rfbServer) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		return
	}
	ver, err := readFull(br, 12)
	if err != nil {
		return
	}
	minor := 3
	if m := rfbVersionRe.FindSubmatch(ver); m != nil {
		minor = int(m[1][0] - '0')
	}
	// Security "None": the socket is root-only, /host/console's JWT is the gate.
	if minor < 7 {
		conn.Write([]byte{0, 0, 0, 1})
	} else {
		conn.Write([]byte{1, 1})
		if _, err := readFull(br, 1); err != nil {
			return
		}
		if minor >= 8 {
			conn.Write([]byte{0, 0, 0, 0})
		}
	}
	if _, err := readFull(br, 1); err != nil { // ClientInit (shared flag)
		return
	}

	c := &rfbClient{conn: conn, pf: serverPF}
	s.mu.Lock()
	tx, ty := tilesFor(s.w, s.h)
	c.dirty = make([]bool, tx*ty)
	init := make([]byte, 0, 64)
	init = binary.BigEndian.AppendUint16(init, uint16(s.w))
	init = binary.BigEndian.AppendUint16(init, uint16(s.h))
	init = append(init, encodePF(serverPF)...)
	name := "NivaroOS Host Desktop"
	init = binary.BigEndian.AppendUint32(init, uint32(len(name)))
	init = append(init, name...)
	s.clients[c] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		c.closed = true
		delete(s.clients, c)
		s.cond.Broadcast()
		s.mu.Unlock()
	}()
	if _, err := conn.Write(init); err != nil {
		return
	}
	go s.updater(c)
	if err := s.readLoop(c, br); err != nil && !errors.Is(err, io.EOF) {
		log.Printf("client: %v", err)
	}
}

func encodePF(p pixelFormat) []byte {
	b := []byte{p.BPP, p.Depth, p.BigEndian, p.TrueColor}
	b = binary.BigEndian.AppendUint16(b, p.RMax)
	b = binary.BigEndian.AppendUint16(b, p.GMax)
	b = binary.BigEndian.AppendUint16(b, p.BMax)
	return append(b, p.RShift, p.GShift, p.BShift, 0, 0, 0)
}

func decodePF(b []byte) pixelFormat {
	return pixelFormat{BPP: b[0], Depth: b[1], BigEndian: b[2], TrueColor: b[3],
		RMax: binary.BigEndian.Uint16(b[4:]), GMax: binary.BigEndian.Uint16(b[6:]), BMax: binary.BigEndian.Uint16(b[8:]),
		RShift: b[10], GShift: b[11], BShift: b[12]}
}

// Linux evdev button codes for VNC button-mask bits 0-2.
var rfbButtons = [3]int32{0x110 /*BTN_LEFT*/, 0x112 /*BTN_MIDDLE*/, 0x111 /*BTN_RIGHT*/}

func (s *rfbServer) readLoop(c *rfbClient, r *bufio.Reader) error {
	for {
		t, err := r.ReadByte()
		if err != nil {
			return err
		}
		switch t {
		case 0: // SetPixelFormat
			b, err := readFull(r, 19)
			if err != nil {
				return err
			}
			pf := decodePF(b[3:])
			if pf.BPP != 8 && pf.BPP != 16 && pf.BPP != 32 {
				return fmt.Errorf("unsupported %d bpp", pf.BPP)
			}
			s.mu.Lock()
			c.pf = pf
			s.mu.Unlock()
		case 2: // SetEncodings
			b, err := readFull(r, 3)
			if err != nil {
				return err
			}
			n := int(binary.BigEndian.Uint16(b[1:]))
			encs, err := readFull(r, 4*n)
			if err != nil {
				return err
			}
			s.mu.Lock()
			c.zlib, c.desktopSize = false, false
			for i := 0; i < n; i++ {
				switch int32(binary.BigEndian.Uint32(encs[4*i:])) {
				case 6:
					c.zlib = true
				case -223:
					c.desktopSize = true
				}
			}
			s.mu.Unlock()
		case 3: // FramebufferUpdateRequest
			b, err := readFull(r, 9)
			if err != nil {
				return err
			}
			s.mu.Lock()
			c.want = true
			if b[0] == 0 {
				for i := range c.dirty {
					c.dirty[i] = true
				}
			}
			s.cond.Broadcast()
			s.mu.Unlock()
		case 4: // KeyEvent
			b, err := readFull(r, 7)
			if err != nil {
				return err
			}
			s.in.Key(binary.BigEndian.Uint32(b[3:]), b[0] != 0)
		case 5: // PointerEvent
			b, err := readFull(r, 5)
			if err != nil {
				return err
			}
			s.pointer(c, b[0], int(binary.BigEndian.Uint16(b[1:])), int(binary.BigEndian.Uint16(b[3:])))
		case 6: // ClientCutText - host clipboard isn't wired up on Wayland
			b, err := readFull(r, 7)
			if err != nil {
				return err
			}
			n := int64(int32(binary.BigEndian.Uint32(b[3:])))
			if n < 0 {
				n = -n
			}
			if _, err := io.CopyN(io.Discard, r, n); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported client message %d", t)
		}
	}
}

func (s *rfbServer) pointer(c *rfbClient, mask uint8, x, y int) {
	s.mu.Lock()
	w, h := s.w, s.h
	s.mu.Unlock()
	s.in.Motion(float64(min(x, w-1)), float64(min(y, h-1)))
	prev := c.buttons
	c.buttons = mask
	for i, code := range rfbButtons {
		bit := uint8(1) << i
		if mask&bit != prev&bit {
			s.in.Button(code, mask&bit != 0)
		}
	}
	// Bits 3-6 are wheel up/down/left/right "clicks": one step per press.
	for i, sc := range [4]struct {
		axis  uint32
		steps int32
	}{{0, -1}, {0, 1}, {1, -1}, {1, 1}} {
		bit := uint8(1) << (3 + i)
		if mask&bit != 0 && prev&bit == 0 {
			s.in.Scroll(sc.axis, sc.steps)
		}
	}
}

type rfbRect struct {
	x, y, w, h int
	data       []byte
}

func (s *rfbServer) updater(c *rfbClient) {
	var zbuf bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&zbuf, zlib.BestSpeed)
	for {
		s.mu.Lock()
		for !c.closed && !(c.want && (c.resized || anyTrue(c.dirty))) {
			s.cond.Wait()
		}
		if c.closed {
			s.mu.Unlock()
			return
		}
		if c.resized && !c.desktopSize {
			s.mu.Unlock()
			c.conn.Close() // can't tell this client; it reconnects at the new size
			return
		}
		resized := c.resized
		w, h := s.w, s.h
		rects := dirtyRects(c.dirty, w, h)
		for i := range rects {
			r := &rects[i]
			r.data = convertRect(s.fb, w, r.x, r.y, r.w, r.h, c.pf)
		}
		for i := range c.dirty {
			c.dirty[i] = false
		}
		c.want, c.resized = false, false
		useZlib := c.zlib
		s.mu.Unlock()

		n := len(rects)
		if resized {
			n++
		}
		msg := []byte{0, 0}
		msg = binary.BigEndian.AppendUint16(msg, uint16(n))
		if resized {
			msg = appendRectHeader(msg, 0, 0, w, h, -223)
		}
		for _, r := range rects {
			if useZlib {
				msg = appendRectHeader(msg, r.x, r.y, r.w, r.h, 6)
				zw.Write(r.data)
				zw.Flush()
				msg = binary.BigEndian.AppendUint32(msg, uint32(zbuf.Len()))
				msg = append(msg, zbuf.Bytes()...)
				zbuf.Reset()
			} else {
				msg = appendRectHeader(msg, r.x, r.y, r.w, r.h, 0)
				msg = append(msg, r.data...)
			}
		}
		if _, err := c.conn.Write(msg); err != nil {
			c.conn.Close()
			return
		}
	}
}

func anyTrue(b []bool) bool {
	for _, v := range b {
		if v {
			return true
		}
	}
	return false
}

func appendRectHeader(b []byte, x, y, w, h int, enc int32) []byte {
	b = binary.BigEndian.AppendUint16(b, uint16(x))
	b = binary.BigEndian.AppendUint16(b, uint16(y))
	b = binary.BigEndian.AppendUint16(b, uint16(w))
	b = binary.BigEndian.AppendUint16(b, uint16(h))
	return binary.BigEndian.AppendUint32(b, uint32(enc))
}

// dirtyRects merges runs of dirty tiles in each tile row into one rect.
func dirtyRects(dirty []bool, w, h int) []rfbRect {
	tx, ty := tilesFor(w, h)
	var out []rfbRect
	for j := 0; j < ty; j++ {
		for i := 0; i < tx; i++ {
			if !dirty[j*tx+i] {
				continue
			}
			start := i
			for i < tx && dirty[j*tx+i] {
				i++
			}
			x0, y0 := start*rfbTile, j*rfbTile
			out = append(out, rfbRect{x: x0, y: y0, w: min(i*rfbTile, w) - x0, h: min(y0+rfbTile, h) - y0})
		}
	}
	return out
}

// convertRect copies a rect of the BGRx framebuffer into the client's
// pixel format.
func convertRect(fb []byte, fbW, x, y, w, h int, pf pixelFormat) []byte {
	bpp := int(pf.BPP) / 8
	out := make([]byte, 0, w*h*bpp)
	if pf == serverPF {
		for row := y; row < y+h; row++ {
			a := (row*fbW + x) * 4
			out = append(out, fb[a:a+w*4]...)
		}
		return out
	}
	for row := y; row < y+h; row++ {
		p := fb[(row*fbW+x)*4:]
		for i := 0; i < w; i++ {
			b, g, r := uint32(p[i*4]), uint32(p[i*4+1]), uint32(p[i*4+2])
			v := (r*uint32(pf.RMax)/255)<<pf.RShift | (g*uint32(pf.GMax)/255)<<pf.GShift | (b*uint32(pf.BMax)/255)<<pf.BShift
			switch bpp {
			case 4:
				if pf.BigEndian != 0 {
					out = binary.BigEndian.AppendUint32(out, v)
				} else {
					out = binary.LittleEndian.AppendUint32(out, v)
				}
			case 2:
				if pf.BigEndian != 0 {
					out = binary.BigEndian.AppendUint16(out, uint16(v))
				} else {
					out = binary.LittleEndian.AppendUint16(out, uint16(v))
				}
			default:
				out = append(out, byte(v))
			}
		}
	}
	return out
}
