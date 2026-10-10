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
		// Set, not appended: exec takes the first of duplicate keys.
		os.Setenv("XDG_RUNTIME_DIR", runtimeDir)
		os.Setenv("WAYLAND_DISPLAY", wl)
		env := os.Environ()
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
	conn, err := becomeSessionUser(u, wl)
	if err != nil {
		st.set("error", backend, "Could not reach the desktop session's D-Bus: "+err.Error())
		return 1
	}
	defer conn.Close()

	start := func() (*wlSession, error) { return startMutterSession(conn) }
	if backend != "mutter" {
		tokenFile := filepath.Join(u.HomeDir, ".local/state/nivaroos/host-desktop-restore-token")
		start = func() (*wlSession, error) {
			return startPortalSession(conn, tokenFile, func() {
				st.set("consent", backend, "Waiting for someone at the server to allow screen sharing: click \"Allow\"/\"Share\" in the dialog on the server's own screen (needed once).")
			})
		}
	}
	sess, err := start()
	if errors.Is(err, errPortalDenied) {
		st.set("denied", backend, "Screen sharing was declined on the server. Use \"Ask again\" to show the request on the server's screen again.")
		return wlExitDenied
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
	srv.useClip(sess.clip, sess.closed)
	// A display mode change breaks the stream: mutter (GNOME 50) drops its
	// PipeWire node without a word, and KWin's renegotiated stream stalls
	// once the GStreamer child reconnects. So a fresh session takes over -
	// on mutter's MonitorsChanged, or (portal) when the frame size
	// changes; viewers stay connected and get a DesktopSize.
	var monitorsChanged chan *dbus.Signal
	if backend == "mutter" {
		_ = conn.AddMatchSignal(dbus.WithMatchObjectPath("/org/gnome/Mutter/DisplayConfig"), dbus.WithMatchInterface("org.gnome.Mutter.DisplayConfig"), dbus.WithMatchMember("MonitorsChanged"))
		monitorsChanged = make(chan *dbus.Signal, 8)
		conn.Signal(monitorsChanged)
	}
	streams := make(chan wlStream, 1)
	resized := make(chan struct{}, 1)
	var onResize func()
	if backend != "mutter" {
		onResize = func() {
			select {
			case resized <- struct{}{}:
			default:
			}
		}
	}
	fatal := make(chan error, 2)
	listening := false
	first := sess.wlStream
	go func() {
		fatal <- capturePipeWire(first, streams, onResize, func(w, h int, frame []byte) {
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
	for {
		select {
		case <-sess.closed:
			log.Printf("the desktop ended the screen-sharing session")
			st.set("error", backend, "The desktop ended the screen-sharing session (screen locked or logged out?) - reconnecting.")
			return 1
		case err := <-fatal:
			log.Printf("capture: %v", err)
			st.set("error", backend, "Screen capture stopped: "+fmt.Sprint(err))
			return 1
		case s := <-monitorsChanged:
			if s.Path != "/org/gnome/Mutter/DisplayConfig" || s.Name != "org.gnome.Mutter.DisplayConfig.MonitorsChanged" {
				continue
			}
			// One change often comes as a burst; let it settle.
			time.Sleep(500 * time.Millisecond)
			for len(monitorsChanged) > 0 {
				<-monitorsChanged
			}
		case <-resized:
		}
		old := sess
		if sess, err = start(); err != nil {
			log.Printf("restarting after a display change: %v", err)
			st.set("error", backend, "Restarting screen sharing after a display change failed: "+err.Error())
			return 1
		}
		log.Printf("display changed: new stream, node %d", sess.node)
		st.set("running", backend, "")
		srv.setInput(sess.input)
		srv.useClip(sess.clip, sess.closed)
		streams <- sess.wlStream
		old.stop()
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

// becomeSessionUser drops to u, sets up its session environment (Wayland
// display wl) and connects to its session bus.
func becomeSessionUser(u *user.User, wl string) (*dbus.Conn, error) {
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	if err := dropPrivileges(u, uid, gid); err != nil {
		return nil, fmt.Errorf("dropping to %s: %w", u.Username, err)
	}
	runtimeDir := "/run/user/" + u.Uid
	os.Setenv("HOME", u.HomeDir)
	os.Setenv("USER", u.Username)
	os.Setenv("LOGNAME", u.Username)
	os.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	os.Setenv("WAYLAND_DISPLAY", wl)
	os.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+runtimeDir+"/bus")
	return dbus.ConnectSessionBus()
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

// wlStream: the PipeWire node to capture.
type wlStream struct {
	node uint32
	pwFD *os.File // portal only: the PipeWire remote to read the node from
}

type wlSession struct {
	wlStream
	stop   func() // ends the session
	input  wlInput
	clip   *wlClip // nil: this desktop doesn't share its clipboard
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
			sess := &wlSession{wlStream: wlStream{node: node}, input: mutterInput{sess: rd, stream: string(streamPath)}, closed: closed,
				stop: func() { rd.Call(mutterRD+".Session.Stop", 0) }}
			if err := rd.Call(mutterRD+".Session.EnableClipboard", 0, noOpts).Err; err == nil {
				sess.clip = &wlClip{conn: conn, obj: rd, iface: mutterRD + ".Session", sigPath: rdPath, mimesKey: "mime-types", ownerKey: "session-is-owner"}
			} else {
				log.Printf("mutter clipboard: %v", err)
			}
			return sess, nil
		case <-closed:
			return nil, errors.New("session closed before its stream started")
		case <-timeout:
			return nil, errors.New("no PipeWire stream within 15s")
		}
	}
}

const (
	portalBus  = "org.freedesktop.portal.Desktop"
	portalRD   = "org.freedesktop.portal.RemoteDesktop"
	portalSC   = "org.freedesktop.portal.ScreenCast"
	portalClip = "org.freedesktop.portal.Clipboard"
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
	// Before Start, or the portal won't share the clipboard; older portals
	// have no Clipboard interface at all.
	if err := conn.Object(portalBus, "/org/freedesktop/portal/desktop").Call(portalClip+".RequestClipboard", 0, session, noOpts).Err; err != nil {
		log.Printf("portal clipboard: %v", err)
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
	pwFD := os.NewFile(uintptr(fd), "pipewire")
	sess := &wlSession{wlStream: wlStream{node: node, pwFD: pwFD}, input: portalInput{obj: obj, session: session, stream: node}, closed: closed,
		stop: func() {
			conn.Object(portalBus, session).Call("org.freedesktop.portal.Session.Close", 0)
			pwFD.Close()
		}}
	if on, _ := res["clipboard_enabled"].Value().(bool); on {
		sess.clip = &wlClip{conn: conn, obj: obj, iface: portalClip, session: []interface{}{session}, sigPath: "/org/freedesktop/portal/desktop", mimesKey: "mime_types", ownerKey: "session_is_owner"}
	}
	return sess, nil
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
// Host clipboard (text): mutter's RemoteDesktop.Session and the portal's
// Clipboard interface are the same API, apart from the interface, the
// portal's leading session argument and the option key spelling.
// ---------------------------------------------------------------------------

// maxClipText bounds clipboard text both ways.
const maxClipText = 10 << 20

// clipTextMimes: what we offer when a viewer's text becomes the host
// clipboard, and (in this order) what we ask the host for.
var clipTextMimes = []string{"text/plain;charset=utf-8", "UTF8_STRING", "text/plain", "STRING", "TEXT"}

type wlClip struct {
	conn               *dbus.Conn
	obj                dbus.BusObject
	iface              string
	session            []interface{} // portal: the session handle
	sigPath            dbus.ObjectPath
	mimesKey, ownerKey string

	mu   sync.Mutex
	text string // what we offer while we own the selection
}

func (c *wlClip) call(method string, args ...interface{}) *dbus.Call {
	return c.obj.Call(c.iface+"."+method, 0, append(append([]interface{}{}, c.session...), args...)...)
}

// set makes text the host clipboard; the host asks for it with
// SelectionTransfer when something pastes.
func (c *wlClip) set(text string) {
	c.mu.Lock()
	c.text = text
	c.mu.Unlock()
	if err := c.call("SetSelection", map[string]dbus.Variant{c.mimesKey: dbus.MakeVariant(clipTextMimes)}).Err; err != nil {
		log.Printf("clipboard SetSelection: %v", err)
	}
}

// watch serves paste requests and reports text copied on the host.
func (c *wlClip) watch(done <-chan struct{}, onHostText func(string)) {
	_ = c.conn.AddMatchSignal(dbus.WithMatchObjectPath(c.sigPath), dbus.WithMatchInterface(c.iface))
	sig := make(chan *dbus.Signal, 16)
	c.conn.Signal(sig)
	defer c.conn.RemoveSignal(sig)
	for {
		var s *dbus.Signal
		select {
		case s = <-sig:
		case <-done:
			return
		}
		if s.Path != c.sigPath || !strings.HasPrefix(s.Name, c.iface+".") {
			continue
		}
		body := s.Body
		if len(c.session) > 0 {
			if len(body) == 0 || body[0] != c.session[0] {
				continue
			}
			body = body[1:]
		}
		switch strings.TrimPrefix(s.Name, c.iface+".") {
		case "SelectionOwnerChanged":
			if len(body) != 1 {
				continue
			}
			opts, _ := body[0].(map[string]dbus.Variant)
			if own, _ := opts[c.ownerKey].Value().(bool); own {
				continue // our own SetSelection
			}
			mimes := variantStrings(opts[c.mimesKey].Value())
			if mime := pickTextMime(mimes); mime != "" {
				if text, err := c.read(mime); err != nil {
					log.Printf("clipboard read: %v", err)
				} else if text != "" {
					onHostText(text)
				}
			}
		case "SelectionTransfer":
			if len(body) != 2 {
				continue
			}
			serial, _ := body[1].(uint32)
			c.mu.Lock()
			text := c.text
			c.mu.Unlock()
			ok := c.write(serial, text) == nil
			c.call("SelectionWriteDone", serial, ok)
		}
	}
}

// variantStrings: an as, or mutter's (as) - its mime-types come in a struct.
func variantStrings(v interface{}) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []interface{}:
		if len(x) == 1 {
			return variantStrings(x[0])
		}
	}
	return nil
}

func pickTextMime(offered []string) string {
	for _, want := range clipTextMimes {
		for _, m := range offered {
			if strings.EqualFold(m, want) {
				return m
			}
		}
	}
	return ""
}

func (c *wlClip) read(mime string) (string, error) {
	var fd dbus.UnixFD
	if err := c.call("SelectionRead", mime).Store(&fd); err != nil {
		return "", err
	}
	f := pipeFile(int(fd))
	defer f.Close()
	_ = f.SetReadDeadline(time.Now().Add(5 * time.Second))
	b, err := io.ReadAll(io.LimitReader(f, maxClipText))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\x00"), nil
}

func (c *wlClip) write(serial uint32, text string) error {
	var fd dbus.UnixFD
	if err := c.call("SelectionWrite", serial).Store(&fd); err != nil {
		return err
	}
	f := pipeFile(int(fd))
	defer f.Close()
	_ = f.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := io.WriteString(f, text)
	return err
}

// pipeFile: non-blocking, so read/write deadlines work on it.
func pipeFile(fd int) *os.File {
	_ = syscall.SetNonblock(fd, true)
	return os.NewFile(uintptr(fd), "clipboard")
}

// ---------------------------------------------------------------------------
// PipeWire capture
// ---------------------------------------------------------------------------

var gstCapsRe = regexp.MustCompile(`GstCapsFilter:nvout\.GstPad:src: caps = video/x-raw.*width=\(int\)(\d+), height=\(int\)(\d+)`)

// capturePipeWire runs the GStreamer child until the stream ends, handing
// over every BGRx frame. A size change restarts the child (its frames are
// a raw byte stream, sized by the caps it printed).
// A new stream from streams replaces the current one; with onResize set,
// a size change calls it and waits for that new stream instead of
// reconnecting.
func capturePipeWire(cur wlStream, streams <-chan wlStream, onResize func(), onFrame func(w, h int, frame []byte)) error {
	failures := 0
	for {
		started := time.Now()
		next, err := runGstOnce(cur, streams, onFrame)
		if next != nil {
			cur = *next
			continue
		}
		if err == nil && onResize != nil {
			onResize()
			cur = <-streams
			continue
		}
		if err == nil {
			continue // new size: straight back
		}
		if time.Since(started) > 30*time.Second {
			failures = 0
		}
		failures++
		if failures > 5 {
			return err
		}
		log.Printf("gstreamer: %v - restarting", err)
		time.Sleep(time.Second)
	}
}

// runGstOnce returns nil when the size changed, or with next set when a
// new node replaced this one (the child is stopped for it).
func runGstOnce(st wlStream, streams <-chan wlStream, onFrame func(w, h int, frame []byte)) (next *wlStream, err error) {
	node, pwFD := st.node, st.pwFD
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
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
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		pw.Close()
		return nil, err
	}
	pw.Close()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	done := make(chan struct{})
	got := make(chan wlStream, 1)
	defer func() {
		close(done)
		select {
		case n := <-got:
			next, err = &n, nil
		default:
		}
	}()
	go func() {
		select {
		case n := <-streams:
			got <- n
			_ = cmd.Process.Kill()
		case <-done:
		}
	}()

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
			return nil, errors.New("gst-launch-1.0 exited before negotiating a format")
		}
		cur = s
	case <-time.After(30 * time.Second):
		return nil, errors.New("no video format from PipeWire within 30s")
	}
	if cur[0] <= 0 || cur[1] <= 0 || cur[0] > 16384 || cur[1] > 16384 {
		return nil, fmt.Errorf("bad frame size %dx%d", cur[0], cur[1])
	}
	frame := make([]byte, cur[0]*cur[1]*4)
	for {
		if _, err := io.ReadFull(pr, frame); err != nil {
			return nil, fmt.Errorf("reading frames: %w", err)
		}
		select {
		case s, ok := <-sizes:
			// ponytail: the frame read alongside a renegotiation may be
			// misframed; restarting at the new size fixes it a moment later.
			if ok && s != cur {
				return nil, nil
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
	wmu         sync.Mutex // the updater and clipboard replies both write
	pf          pixelFormat
	zlib        bool
	desktopSize bool
	extClip     bool // speaks the Extended Clipboard (UTF-8)
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
	clip    string            // the shared clipboard text, last copied on either side
	toHost  func(text string) // nil: no host clipboard
}

func (s *rfbServer) setInput(in wlInput) {
	s.mu.Lock()
	s.in = in
	s.mu.Unlock()
}

func (s *rfbServer) input() wlInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.in
}

// useClip shares clip (nil: none) with the viewers until done closes.
func (s *rfbServer) useClip(clip *wlClip, done <-chan struct{}) {
	s.mu.Lock()
	s.toHost = nil
	if clip != nil {
		s.toHost = clip.set
	}
	s.mu.Unlock()
	if clip != nil {
		go clip.watch(done, func(text string) { s.clipboard(text, nil) })
	}
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
	// Held until ServerInit is out: a clipboard broadcast must not overtake it.
	c.wmu.Lock()
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
	_, err = conn.Write(init)
	c.wmu.Unlock()
	if err != nil {
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
			hadExtClip := c.extClip
			for i := 0; i < n; i++ {
				switch int32(binary.BigEndian.Uint32(encs[4*i:])) {
				case 6:
					c.zlib = true
				case -223:
					c.desktopSize = true
				case encExtClip:
					c.extClip = true
				}
			}
			s.mu.Unlock()
			if c.extClip && !hadExtClip {
				// Our caps: text, every action, up to maxClipText unasked.
				if err := c.write(extClipMsg(clipCaps|clipRequest|clipPeek|clipNotify|clipProvide|clipText,
					binary.BigEndian.AppendUint32(nil, maxClipText))); err != nil {
					return err
				}
			}
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
			s.input().Key(binary.BigEndian.Uint32(b[3:]), b[0] != 0)
		case 5: // PointerEvent
			b, err := readFull(r, 5)
			if err != nil {
				return err
			}
			s.pointer(c, b[0], int(binary.BigEndian.Uint16(b[1:])), int(binary.BigEndian.Uint16(b[3:])))
		case 6: // ClientCutText
			b, err := readFull(r, 7)
			if err != nil {
				return err
			}
			n := int(int32(binary.BigEndian.Uint32(b[3:])))
			if n < -maxClipText-4 || n > maxClipText {
				return fmt.Errorf("clipboard message of %d bytes", n)
			}
			if n >= 0 {
				data, err := readFull(r, n)
				if err != nil {
					return err
				}
				s.clipboard(latin1ToString(data), c)
				continue
			}
			data, err := readFull(r, -n)
			if err != nil {
				return err
			}
			if err := s.extClipboard(c, data); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported client message %d", t)
		}
	}
}

func (c *rfbClient) write(b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.conn.Write(b)
	return err
}

// Extended Clipboard (the QEMU/noVNC extension: UTF-8, zlib).
const (
	encExtClip  = int32(-1063131698) // 0xC0A1E5CE
	clipText    = 1 << 0
	clipCaps    = 1 << 24
	clipRequest = 1 << 25
	clipPeek    = 1 << 26
	clipNotify  = 1 << 27
	clipProvide = 1 << 28
)

// extClipMsg: a ServerCutText with a negative length - the extension.
func extClipMsg(flags uint32, payload []byte) []byte {
	b := []byte{3, 0, 0, 0}
	b = binary.BigEndian.AppendUint32(b, uint32(-int32(4+len(payload))))
	b = binary.BigEndian.AppendUint32(b, flags)
	return append(b, payload...)
}

// encodeClipProvide: zlib of (u32 length, UTF-8 text with CRLF line ends
// and a trailing NUL), as noVNC and QEMU write it.
func encodeClipProvide(text string) []byte {
	body := []byte(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n", "\r\n") + "\x00")
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write(binary.BigEndian.AppendUint32(nil, uint32(len(body))))
	zw.Write(body)
	zw.Close()
	return buf.Bytes()
}

// decodeClipProvide: the text of a Provide payload whose formats include text
// (text is the first format, so it comes first).
func decodeClipProvide(payload []byte) (string, error) {
	zr, err := zlib.NewReader(bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	defer zr.Close()
	var n [4]byte
	if _, err := io.ReadFull(zr, n[:]); err != nil {
		return "", err
	}
	size := binary.BigEndian.Uint32(n[:])
	if size > maxClipText {
		return "", fmt.Errorf("clipboard text of %d bytes", size)
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(zr, b); err != nil {
		return "", err
	}
	return strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\r\n", "\n"), nil
}

func latin1ToString(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// legacyCutText: plain ServerCutText is Latin-1; anything else becomes "?".
func legacyCutText(text string) []byte {
	var data []byte
	for _, r := range text {
		if r > 0xff {
			r = '?'
		}
		data = append(data, byte(r))
	}
	b := []byte{3, 0, 0, 0}
	b = binary.BigEndian.AppendUint32(b, uint32(len(data)))
	return append(b, data...)
}

// clipboard: text was copied - on the host (from nil) or by a viewer. Every
// other viewer hears of it; a viewer's text becomes the host clipboard.
func (s *rfbServer) clipboard(text string, from *rfbClient) {
	s.mu.Lock()
	if text == s.clip {
		// Unchanged - also the host echoing what a viewer just set.
		s.mu.Unlock()
		return
	}
	s.clip = text
	toHost := s.toHost
	var others []*rfbClient
	for c := range s.clients {
		if c != from {
			others = append(others, c)
		}
	}
	s.mu.Unlock()
	for _, c := range others {
		if c.extClip {
			c.write(extClipMsg(clipNotify|clipText, nil))
		} else {
			c.write(legacyCutText(text))
		}
	}
	if from != nil && toHost != nil {
		toHost(text)
	}
}

// extClipboard handles one Extended Clipboard message from a viewer.
func (s *rfbServer) extClipboard(c *rfbClient, data []byte) error {
	if len(data) < 4 {
		return errors.New("short clipboard message")
	}
	flags := binary.BigEndian.Uint32(data)
	if flags&clipText == 0 {
		return nil // only text is shared; caps need no answer
	}
	s.mu.Lock()
	text := s.clip
	s.mu.Unlock()
	switch {
	case flags&clipCaps != 0:
	case flags&clipRequest != 0:
		return c.write(extClipMsg(clipProvide|clipText, encodeClipProvide(text)))
	case flags&clipPeek != 0:
		if text == "" {
			return c.write(extClipMsg(clipNotify, nil))
		}
		return c.write(extClipMsg(clipNotify|clipText, nil))
	case flags&clipNotify != 0:
		return c.write(extClipMsg(clipRequest|clipText, nil))
	case flags&clipProvide != 0:
		t, err := decodeClipProvide(data[4:])
		if err != nil {
			return fmt.Errorf("clipboard: %w", err)
		}
		s.clipboard(t, c)
	}
	return nil
}

func (s *rfbServer) pointer(c *rfbClient, mask uint8, x, y int) {
	s.mu.Lock()
	w, h, in := s.w, s.h, s.in
	s.mu.Unlock()
	in.Motion(float64(min(x, w-1)), float64(min(y, h-1)))
	prev := c.buttons
	c.buttons = mask
	for i, code := range rfbButtons {
		bit := uint8(1) << i
		if mask&bit != prev&bit {
			in.Button(code, mask&bit != 0)
		}
	}
	// Bits 3-6 are wheel up/down/left/right "clicks": one step per press.
	for i, sc := range [4]struct {
		axis  uint32
		steps int32
	}{{0, -1}, {0, 1}, {1, -1}, {1, 1}} {
		bit := uint8(1) << (3 + i)
		if mask&bit != 0 && prev&bit == 0 {
			in.Scroll(sc.axis, sc.steps)
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
		if err := c.write(msg); err != nil {
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
