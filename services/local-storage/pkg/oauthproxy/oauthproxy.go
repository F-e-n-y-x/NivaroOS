// Package oauthproxy makes rclone's own `rclone authorize` OAuth flow work
// from a browser on a different device than this server.
//
// rclone's local webserver for that flow always binds to 127.0.0.1:53682
// AND always tells the OAuth provider (Google, etc.) to redirect back to
// that same literal address - both hardcoded upstream, not configurable.
// The provider's redirect is generated server-side and handed straight to
// the browser as a fresh URL, so nothing this service does to the
// terminal's displayed text can touch it - only something actually
// listening on 127.0.0.1:53682 from the browser's point of view helps.
//
// Since rclone binds only the loopback address, binding this proxy to
// every OTHER (LAN-facing) address on the same port number 53682 doesn't
// conflict with it. That means whichever address the browser used to
// reach NivaroOS in the first place, replacing "127.0.0.1" with that same
// address and keeping the port unchanged (53682) - whether by the
// terminal's own auto-rewrite (Settings' "Open Terminal" button) or by
// hand, editing the URL bar after the provider redirects there - lands on
// this proxy, which forwards it on to rclone's real listener.
package oauthproxy

import (
	"io"
	"net"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"go.uber.org/zap"
)

// Port matches rclone's own hardcoded local-auth-server port exactly -
// nothing to rewrite but the host when following a printed/redirected link.
const Port = "53682"

const rcloneAuthorizeAddr = "127.0.0.1:" + Port

// Start binds Port on every non-loopback IPv4 address this box currently
// has (typically just its LAN IP) and forwards each connection to
// rclone's local authorize webserver. Cheap when nobody's running
// `rclone authorize` - a dial failure just closes the accepted connection.
//
// Network interfaces can change (DHCP, a new NIC) after startup, so this
// re-scans and binds any newly-appeared address every 5 minutes rather
// than only once at boot.
func Start() {
	bound := make(map[string]bool)
	bindAll(bound)
	go func() {
		for range time.Tick(5 * time.Minute) {
			bindAll(bound)
		}
	}()
}

func bindAll(bound map[string]bool) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		logger.Error("oauthproxy: failed to list interfaces", zap.Error(err))
		return
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		ip := ipNet.IP.String()
		if bound[ip] {
			continue
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(ip, Port))
		if err != nil {
			// Most likely something else already owns this address:port
			// (or it's mid-DAD/not fully usable yet) - not worth erroring
			// over, we'll just retry it on the next scan.
			continue
		}
		bound[ip] = true
		go acceptLoop(ln)
	}
}

func acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			logger.Error("oauthproxy: accept failed", zap.Error(err))
			return
		}
		go forward(conn)
	}
}

func forward(client net.Conn) {
	defer client.Close()
	upstream, err := net.Dial("tcp", rcloneAuthorizeAddr)
	if err != nil {
		// Nothing is running `rclone authorize` right now - expected most
		// of the time, not worth logging as an error.
		return
	}
	defer upstream.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()
	<-done
}
