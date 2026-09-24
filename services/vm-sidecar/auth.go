package main

import (
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
)

// requireAuth validates the same ES256 JWT every other NivaroOS service
// checks (external.GetPublicKey fetches it via JWKS from user-service,
// cached 10s) - no private key or shared secret needs to live in this
// service. This service previously had NO auth at all: any device that
// could reach this port (28641) could create/delete/control VMs and open
// the VNC console unauthenticated, whether from the LAN or, if this box is
// reachable off-LAN (Tailscale, port-forwarded), from anywhere.
//
// Token is read from either the Authorization header or a ?token= query
// param - matching every other service's lookup order - because the VNC
// console route is a WebSocket upgrade, and neither a browser's WebSocket
// nor Dart's WebSocket.connect can portably set a custom header on the
// handshake, so it has to arrive as a query param there.
//
// Loopback requests skip the check only when they're plainly not from a
// browser (see isLoopbackAutomation) - a bare loopback check alone let any
// web page open in a browser on this box (or a DNS-rebinding page aimed at
// 127.0.0.1) drive the whole API with no token at all.
func requireAuth(next http.Handler, runtimePath string) http.Handler {
	publicKeyFunc := func() (*ecdsa.PublicKey, error) {
		return external.GetPublicKey(runtimePath)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /host/* (the host's own desktop, its display, and installing
		// packages as root) always needs a real token, even from loopback:
		// any local process or host-network container could otherwise
		// drive the machine's desktop.
		if isLoopbackAutomation(r) && !isHostRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		valid, _, err := jwt.Validate(token, publicKeyFunc)
		if err != nil || !valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": 401,
				"message": "token is invalid",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isHostRoute(path string) bool {
	return path == "/host" || strings.HasPrefix(path, "/host/")
}

// isLoopbackAutomation reports whether r is same-host, non-browser
// automation (a script, another local service) - the only kind of caller
// allowed to skip the JWT. It's the shared rule: the socket peer is
// loopback, there's no Origin / Sec-Fetch-Site (every browser sends one),
// and a request that came through the gateway (which is also loopback, and
// is how the UI reaches this service now) counts only when the gateway
// vouched that its own client was same-host automation. Without that last
// part, anyone who can reach the dashboard would get the whole VM API
// without a token.
func isLoopbackAutomation(r *http.Request) bool {
	return middleware.IsLocalAutomation(r)
}

// sameHostOrigin reports whether r's Origin names the host the browser
// used for this request - directly (Host) or behind a reverse proxy or
// tunnel (X-Forwarded-Host) - any port or scheme.
func sameHostOrigin(r *http.Request) bool {
	return middleware.SameHostOrigin(r)
}
