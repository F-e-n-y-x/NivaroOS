package main

import (
	"crypto/ecdsa"
	"encoding/base64"
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
// cached 10s) - same scheme as vm-sidecar, which talks to the browser the
// same way (directly on its own port, not through the gateway).
//
// Token comes from the Authorization header or a ?token= query param.
//
// Loopback callers skip the check only when they're plainly not a browser
// (see isLoopbackAutomation). A bare loopback check let any web page open
// in a browser on this box - or a DNS-rebinding page aimed at 127.0.0.1 -
// queue a "download" into /etc/cron.d as root with no token at all.
//
// Lite-browser traffic (/b/...) never reaches this: it's routed to the
// proxy before auth, authorised by its session capability instead.
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
		token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))

		valid, _, err := jwt.Validate(token, publicKeyFunc)
		if err != nil || !valid {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"success": 401,
				"message": "token is invalid",
			})
			return
		}
		if !tokenRoleAllowed(token) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"success": 403,
				"error":   "Download Station needs an administrator account",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenRoleAllowed enforces "admins only" once NivaroOS tokens carry a role.
// Today's access tokens (common/utils/jwt.Claims) hold only username/id -
// every account is the owner - so a token with no role claim is allowed.
// If a role/roles claim is present, it must include "admin" (or "owner"):
// this service writes files as root. The payload is only read after
// jwt.Validate has verified the signature.
func tokenRoleAllowed(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims map[string]interface{}
	if json.Unmarshal(raw, &claims) != nil {
		return false
	}
	isAdmin := func(v interface{}) bool {
		s, ok := v.(string)
		s = strings.ToLower(strings.TrimSpace(s))
		return ok && (s == "admin" || s == "administrator" || s == "owner")
	}
	checked := false
	for _, key := range []string{"role", "roles"} {
		v, ok := claims[key]
		if !ok || v == nil {
			continue
		}
		checked = true
		switch t := v.(type) {
		case string:
			if isAdmin(t) {
				return true
			}
		case []interface{}:
			for _, x := range t {
				if isAdmin(x) {
					return true
				}
			}
		}
	}
	if b, ok := claims["is_admin"].(bool); ok {
		return b
	}
	return !checked
}

// isLoopbackAutomation reports whether r is same-host, non-browser
// automation (a script, another local service) - the only kind of caller
// allowed to skip the JWT. Every modern browser attaches Sec-Fetch-Site to
// every request it makes and Origin to every cross-origin/non-GET one, so
// requiring both to be absent keeps a browser tab on this box (which also
// arrives from loopback) on the normal token path. Copied from vm-sidecar.
func isLoopbackAutomation(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	// A request relayed by a reverse proxy on this box (the gateway's
	// /v1/download-station route) also arrives from loopback, but on behalf
	// of some remote browser - it always needs the token.
	if viaGateway(r) || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" || r.Header.Get("X-Real-Ip") != "" {
		return false
	}
	return r.Header.Get("Origin") == "" && r.Header.Get("Sec-Fetch-Site") == ""
}

type viaGatewayKey struct{}

func viaGateway(r *http.Request) bool {
	v, _ := r.Context().Value(viaGatewayKey{}).(bool)
	return v
}

// sameHostOrigin reports whether r's Origin header names the same host
// this request was addressed to (any port/scheme) - the web UI is served
// from this same box on a different port, so that's the one cross-origin
// caller CORS should accept. Copied from vm-sidecar.
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
