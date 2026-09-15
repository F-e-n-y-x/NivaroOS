package main

import (
	"crypto/ecdsa"
	"encoding/json"
	"net"
	"net/http"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
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
// Loopback requests (127.0.0.1/::1) skip the check, the same convention
// every echo-based service's JWT() middleware already uses for same-host
// automation - safe here because a real remote caller (a browser or the
// mobile app hitting this port directly, which is how both currently work -
// neither goes through the gateway's reverse proxy for this service) reports
// its own address in RemoteAddr, never loopback.
func requireAuth(next http.Handler, runtimePath string) http.Handler {
	publicKeyFunc := func() (*ecdsa.PublicKey, error) {
		return external.GetPublicKey(runtimePath)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if host == "127.0.0.1" || host == "::1" {
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
