// Package oauthproxy forwards a LAN-reachable port to rclone's own
// `rclone authorize` local webserver, which always binds to
// 127.0.0.1:53682 (hardcoded upstream, not configurable) - unreachable to
// anyone whose browser isn't running on this exact machine. The "Open
// Terminal" flow in Settings' Online Accounts panel types a command that
// rewrites the printed 127.0.0.1:53682 link to <this box's LAN address>:
// ListenPort before it ever reaches the terminal, so the link needs
// something real listening there to forward to.
package oauthproxy

import (
	"io"
	"net"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"go.uber.org/zap"
)

// ListenPort is fixed and known to both this proxy and the frontend (which
// bakes it into the rewritten link), so there's nothing to discover/wire up
// at runtime beyond starting the listener.
const ListenPort = "53683"

const rcloneAuthorizeAddr = "127.0.0.1:53682"

// Start listens on 0.0.0.0:ListenPort and forwards each connection to
// rclone's local authorize webserver. It's a no-op cost when nobody's
// running `rclone authorize` - a dial failure just closes the accepted
// connection immediately.
func Start() {
	ln, err := net.Listen("tcp", "0.0.0.0:"+ListenPort)
	if err != nil {
		logger.Error("oauthproxy: failed to start", zap.Error(err))
		return
	}
	go acceptLoop(ln)
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
