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
// cached 10s) - copied from vm-sidecar, which talks to the browser the same
// way (directly on its own port, not through the gateway).
//
// Token comes from the Authorization header or a ?token= query param.
// Loopback callers skip the check, matching every other NivaroOS service.
// That's only safe because this service never lets the lite browser or the
// download engine connect out to loopback itself (see netguard.go) - the
// proxy's requests would otherwise arrive here as 127.0.0.1.
//
// Lite-browser traffic (/b/...) never reaches this: it's routed to the
// proxy before auth, authorised by its session capability instead.
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
