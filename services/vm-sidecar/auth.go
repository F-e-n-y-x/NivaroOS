package main

import (
	"crypto/ecdsa"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"

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
// Loopback requests skip the check only when they're plainly not from a
// browser (see isLoopbackAutomation) - a bare loopback check alone let any
// web page open in a browser on this box (or a DNS-rebinding page aimed at
// 127.0.0.1) drive the whole API with no token at all.
func requireAuth(next http.Handler, runtimePath string) http.Handler {
	publicKeyFunc := func() (*ecdsa.PublicKey, error) {
		return external.GetPublicKey(runtimePath)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isLoopbackAutomation(r) {
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

// isLoopbackAutomation reports whether r is same-host, non-browser
// automation (a script, another local service) - the only kind of caller
// allowed to skip the JWT. Every modern browser attaches Sec-Fetch-Site to
// every request it makes and Origin to every cross-origin/non-GET one, so
// requiring both to be absent keeps a browser tab on this box (which also
// arrives from loopback) on the normal token path.
func isLoopbackAutomation(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	return r.Header.Get("Origin") == "" && r.Header.Get("Sec-Fetch-Site") == ""
}

// sameHostOrigin reports whether r's Origin header names the same host
// this request was addressed to (any port/scheme) - the web UI is served
// from this same box on a different port, so that's the one cross-origin
// caller CORS and the console WebSocket should accept.
func sameHostOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	reqHost := r.Host
	if h, _, err := net.SplitHostPort(reqHost); err == nil {
		reqHost = h
	}
	reqHost = strings.TrimSuffix(strings.TrimPrefix(reqHost, "["), "]")
	return reqHost != "" && strings.EqualFold(u.Hostname(), reqHost)
}
