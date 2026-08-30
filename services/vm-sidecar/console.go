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

	"github.com/gorilla/websocket"
)

var consoleUpgrader = websocket.Upgrader{
	// The VmManagerApp is served from CasaOS-UI's own origin, not this
	// sidecar's port, so the console connection is always cross-origin -
	// same trust model as the "*" CORS header used for the REST routes.
	CheckOrigin: func(r *http.Request) bool { return true },
}

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
		name := r.PathValue("name")
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

		done := make(chan struct{})
		go func() {
			defer close(done)
			buf := make([]byte, 32*1024)
			for {
				n, err := tcpConn.Read(buf)
				if n > 0 {
					if writeErr := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()

		for {
			msgType, data, err := wsConn.ReadMessage()
			if err != nil {
				break
			}
			if msgType != websocket.BinaryMessage {
				continue
			}
			if _, err := tcpConn.Write(data); err != nil {
				break
			}
		}
		_ = tcpConn.Close()
		<-done
	}
}

func RegisterConsoleRoutes(mux *http.ServeMux, store *LibvirtStore) {
	mux.HandleFunc("GET /vms/{name}/console", handleConsole(store))
}
