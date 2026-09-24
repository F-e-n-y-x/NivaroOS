package jobs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
)

// Auth (spec §0, modelled on services/download-sidecar/auth.go): the same
// ES256 access token every NivaroOS service checks, from the
// Authorization header or ?token=. Reads need a valid token; every write
// also needs the admin role. Same-host automation - the installer, core,
// the uninstaller's release step - skips the token under the strict rule
// of services/common/middleware/localauth.go: loopback peer, no browser
// headers, and no proxy headers unless the local gateway vouched for it.

type callerKey struct{}

// Caller is who made a request, for audit lines and rate limits.
type Caller struct {
	User  string // username, or "local" for loopback automation
	ID    int
	Admin bool
	Local bool
}

func (c Caller) key() string {
	if c.Local {
		return "local"
	}
	return "user:" + strconv.Itoa(c.ID) + ":" + c.User
}

func callerOf(r *http.Request) Caller {
	c, _ := r.Context().Value(callerKey{}).(Caller)
	return c
}

// requireAuth wraps the API. Unauthenticated routes are served before it.
func (s *Service) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if middleware.IsLocalAutomation(r) {
			ctx := context.WithValue(r.Context(), callerKey{}, Caller{User: "local", Admin: true, Local: true})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
		valid, claims, err := jwt.Validate(token, s.publicKey)
		if err != nil || !valid || claims == nil {
			writeError(w, http.StatusUnauthorized, ErrorBody{ErrorCode: ErrUnauthorized})
			return
		}
		c := Caller{User: claims.Username, ID: claims.ID, Admin: tokenIsAdmin(token)}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !c.Admin {
			writeError(w, http.StatusForbidden, ErrorBody{ErrorCode: ErrForbidden, Detail: "Backup & Sync changes need an administrator account"})
			return
		}
		ctx := context.WithValue(r.Context(), callerKey{}, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// tokenIsAdmin enforces "admins only" once NivaroOS tokens carry a role.
// Today's access tokens (common/utils/jwt.Claims) hold only username and
// id - every account is the owner - so a token with no role claim is an
// admin. When a role/roles claim is present it must include "admin" (or
// "owner"): this service reads and writes every folder as root. Only
// called after jwt.Validate verified the signature. Same rule as
// Download Station's tokenRoleAllowed.
func tokenIsAdmin(token string) bool {
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

// withCORS allows cross-origin calls only from a page served by this same
// host (the web UI on its own port); it echoes that Origin rather than
// "*". Through the gateway the UI is same-origin and needs none of this.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		allowed := middleware.SameHostOrigin(r)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		}
		if r.Method == http.MethodOptions {
			if allowed {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// stripPrefix maps /v1/backup/x onto /x, so the service answers with or
// without the gateway's prefix.
func stripPrefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == APIBase || strings.HasPrefix(p, APIBase+"/") {
			rest := strings.TrimPrefix(p, APIBase)
			if rest == "" {
				rest = "/"
			}
			r2 := r.Clone(r.Context())
			r2.URL = cloneURL(r.URL)
			r2.URL.Path, r2.URL.RawPath = rest, ""
			next.ServeHTTP(w, r2)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func cloneURL(u *url.URL) *url.URL {
	c := *u
	return &c
}
