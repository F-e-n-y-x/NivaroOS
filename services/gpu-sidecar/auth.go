package main

import (
	"crypto/ecdsa"
	"net/http"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
)

// requireAuth validates the same ES256 JWT every other NivaroOS service
// checks (external.GetPublicKey fetches it via JWKS from user-service), read
// from the Authorization header or ?token=. This service previously had no
// auth at all while POST /driver-install runs a root package install.
//
// Only same-host, non-browser automation calling 127.0.0.1:28640 directly
// (see common/middleware.IsLocalAutomation: loopback socket peer, no
// X-Forwarded-For/X-Real-IP, no Origin/Sec-Fetch-Site) may skip the token.
// Requests proxied by the gateway always carry X-Forwarded-For, so browser
// and mobile clients always need a token.
func requireAuth(next http.Handler, runtimePath string) http.Handler {
	publicKeyFunc := func() (*ecdsa.PublicKey, error) {
		return external.GetPublicKey(runtimePath)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nivaroos_middleware.IsLocalAutomation(r) {
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if valid, _, err := jwt.Validate(token, publicKeyFunc); err != nil || !valid {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"success": 401,
				"message": "token is invalid",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
