// console.go proxies a browser WebSocket to a running VM's VNC TCP
// socket, so the noVNC client embedded in the VmManagerApp can connect
// directly - no separate websockify process needed.
package main

import (
	"encoding/xml"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

var consoleUpgrader = websocket.Upgrader{
	// The VmManagerApp is served from the NivaroOS UI's own origin (same
	// host, different port), so a browser console connection is always
	// cross-origin - accept exactly that, plus no Origin at all (the mobile
	// app's Dart WebSocket and other non-browser clients never send one).
	// Anything else is some other site trying to open a VNC session with a
	// token it got hold of.
	CheckOrigin: func(r *http.Request) bool {
		return r.Header.Get("Origin") == "" || sameHostOrigin(r)
	},
}

// Console keepalive: the browser/Dart side answers pings automatically, so
// a peer that's silently gone (laptop lid closed, mobile network dropped)
// is detected within consolePongWait instead of pinning the VNC socket and
// both goroutines open forever.
const (
	consolePongWait     = 60 * time.Second
	consolePingInterval = 25 * time.Second
	consoleWriteWait    = 10 * time.Second
	// A VNC client->server message is tiny (key/pointer events, clipboard
	// text) - this only exists to stop a peer from making ReadMessage
	// buffer an arbitrarily large frame in memory.
	consoleReadLimit = 4 << 20
)

// consoleGraphicsXML mirrors only the <graphics> element read out of a
// running domain's live XML - a separate, minimal type from domain.go's
// domainXML since this is console.go's own narrow concern.
type consoleGraphicsXML struct {
	Devices struct {
		Graphics []struct {
			Type   string `xml:"type,attr"`
			Port   string `xml:"port,attr"`
			Listen string `xml:"listen,attr"`
		} `xml:"graphics"`
	} `xml:"devices"`
}

// parseVNCAddress finds the VM's VNC listen address in its domain XML.
// The domain template uses port='-1' autoport='yes'; libvirt rewrites
// the live port into GetXMLDesc's output only once the VM actually
// starts, so a still-shown "-1" means there is no VNC server to connect
// to yet.
func parseVNCAddress(domainXMLDesc string) (string, error) {
	var parsed consoleGraphicsXML
	if err := xml.Unmarshal([]byte(domainXMLDesc), &parsed); err != nil {
		return "", err
	}
	for _, g := range parsed.Devices.Graphics {
		if g.Type != "vnc" {
			continue
		}
		port, err := strconv.Atoi(g.Port)
		if err != nil || port < 0 {
			continue
		}
		host := g.Listen
		if host == "" {
			host = "127.0.0.1"
		}
		return host + ":" + strconv.Itoa(port), nil
	}
	return "", fmt.Errorf("no active VNC graphics device found (is the VM running?)")
}

func vncAddress(store *LibvirtStore, name string) (string, error) {
	dom, err := store.lookup(name)
	if err != nil {
		return "", err
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return "", err
	}
	return parseVNCAddress(xmlDesc)
}

func handleConsole(store *LibvirtStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, ok := vmNameFromPath(w, r)
		if !ok {
			return
		}
		addr, err := vncAddress(store, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		tcpConn, err := net.Dial("tcp", addr)
		if err != nil {
			http.Error(w, fmt.Sprintf("connect to VNC server at %s: %v", addr, err), http.StatusBadGateway)
			return
		}
		defer tcpConn.Close()

		wsConn, err := consoleUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("console %s: websocket upgrade: %v", name, err)
			return
		}
		defer wsConn.Close()

		proxyConsole(wsConn, tcpConn)
	}
}

// handleHostConsole proxies to the host desktop's x11vnc, which listens
// only on a root-owned 0600 unix socket (no TCP port, no password): the
// JWT check in requireAuth (never skipped for /host/*) is the one gate.
func handleHostConsole() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vncConn, err := net.DialTimeout("unix", hostVNCSocketPath, 5*time.Second)
		if err != nil {
			reason := GetHostDesktopStatus().Reason
			if reason == "" {
				reason = err.Error()
			}
			http.Error(w, "host desktop VNC server unavailable: "+reason, http.StatusBadGateway)
			return
		}
		defer vncConn.Close()

		wsConn, err := consoleUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("host console: websocket upgrade: %v", err)
			return
		}
		defer wsConn.Close()

		hostConsoleSessions.Add(1)
		defer hostConsoleSessions.Add(-1)
		noteHostConsoleInput()
		proxyConsole(wsConn, hostInputTracker{vncConn})
	}
}

// hostInputTracker records when the viewer last sent anything (keys,
// pointer) - the CapsLock watcher only acts on an idle session.
type hostInputTracker struct{ net.Conn }

func (c hostInputTracker) Write(b []byte) (int, error) {
	noteHostConsoleInput()
	return c.Conn.Write(b)
}

// proxyConsole pumps bytes both ways between a console WebSocket and a VNC
// TCP socket until either side ends, then tears down both - closing
// wsConn when VNC goes away (VM shut down) is what lets the client notice
// and show "disconnected" instead of a frozen last frame.
func proxyConsole(wsConn *websocket.Conn, tcpConn net.Conn) {
	wsConn.SetReadLimit(consoleReadLimit)
	extendDeadline := func() error { return wsConn.SetReadDeadline(time.Now().Add(consolePongWait)) }
	_ = extendDeadline()
	wsConn.SetPongHandler(func(string) error { return extendDeadline() })

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 32*1024)
		for {
			n, err := tcpConn.Read(buf)
			if n > 0 {
				_ = wsConn.SetWriteDeadline(time.Now().Add(consoleWriteWait))
				if writeErr := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					break
				}
			}
			if err != nil {
				_ = wsConn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "VNC server closed the connection"),
					time.Now().Add(consoleWriteWait))
				break
			}
		}
		// Unblocks the ReadMessage loop below if it's still waiting.
		_ = wsConn.Close()
	}()

	// WriteControl is safe to call concurrently with the WriteMessage
	// goroutine above (gorilla/websocket's documented exception).
	stopPing := make(chan struct{})
	go func() {
		ticker := time.NewTicker(consolePingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := wsConn.WriteControl(websocket.PingMessage, nil, time.Now().Add(consoleWriteWait)); err != nil {
					return
				}
			case <-stopPing:
				return
			}
		}
	}()

	for {
		msgType, data, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		_ = extendDeadline()
		if msgType != websocket.BinaryMessage {
			continue
		}
		if _, err := tcpConn.Write(data); err != nil {
			break
		}
	}
	close(stopPing)
	_ = tcpConn.Close()
	<-done
}

func RegisterConsoleRoutes(mux *http.ServeMux, store *LibvirtStore) {
	mux.HandleFunc("GET /vms/{name}/console", handleConsole(store))
	mux.HandleFunc("GET /host/console", handleHostConsole())
}
