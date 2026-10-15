package jwt

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	jwt "github.com/golang-jwt/jwt/v4"
)

// Media tickets (security audit 2026-10, L5). Image, download and video
// links used to carry the 3 h access token in ?token=, where it ends up in
// browser history, copied links and screenshots. They now carry a ticket
// in ?mt=: signed by the user service like a token, but for one purpose and
// one place, and short-lived:
//
//   - TicketDir: GET/HEAD of the files directly in one folder through the
//     file media routes (TicketDirRoutes) - a listing's thumbnails, a viewer,
//     a download.
//   - TicketURL: GET/HEAD of exactly one URL path from TicketURLs (an
//     avatar, a VM preview).
//
// A ticket is never an access token (Validate refuses its issuer), so only
// ValidateRequest, for the request it was made for, accepts it. It carries
// the session's sid and gen, so ending the session ends its tickets.
//
// ?token= still works on WebSockets (a browser can't send a header there)
// and, for one release, everywhere - until middleware.LocalStrictPath
// exists (QueryToken).

const (
	TicketParam  = "mt"
	TicketIssuer = "nivaroos-ticket"
	TicketDir    = "dir"
	TicketURL    = "url"
)

// TicketLifetime: an unused ticket expires this long after it was issued,
// one in use (a video's range requests) this long after its last use in
// this process. Never later than AccessTokenLifetime after it was issued.
const TicketLifetime = 5 * time.Minute

// TicketDirRoutes serve a file's bytes for its ?path=.
var TicketDirRoutes = map[string]bool{"/v1/file": true, "/v1/image": true, "/v1/file/stream": true}

// TicketURLs are the URL paths a TicketURL may name (as the gateway sees
// them).
var TicketURLs = regexp.MustCompile(`^(/v1/users/avatar|/v1/vm-sidecar/vms/[^/]+/screenshot)$`)

var ticketNow = time.Now

// IssueTicket signs a ticket for the session of from (an access token's
// claims).
func IssueTicket(privateKey *ecdsa.PrivateKey, from *Claims, kind, target string) (string, error) {
	switch kind {
	case TicketDir:
		if !path.IsAbs(target) || path.Clean(target) != target {
			return "", errors.New("ticket folder must be a clean absolute path")
		}
	case TicketURL:
		if !TicketURLs.MatchString(target) {
			return "", errors.New("no tickets for this URL")
		}
	default:
		return "", errors.New("unknown ticket kind")
	}
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}
	now := ticketNow()
	c := Claims{
		Username: from.Username, ID: from.ID, SessionID: from.SessionID, Generation: from.Generation,
		TicketKind: kind, TicketTarget: target,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenLifetime)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    TicketIssuer,
			ID:        hex.EncodeToString(jti),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodES256, c).SignedString(privateKey)
}

// QueryToken is r's ?token=, where one is still allowed (see above).
func QueryToken(r *http.Request) string {
	if middleware.LegacyQueryToken() || isWebSocket(r) {
		return r.URL.Query().Get("token")
	}
	return ""
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// RequestToken is r's credential: the Authorization header, else
// QueryToken, else its ticket.
func RequestToken(r *http.Request) string {
	if t := r.Header.Get("Authorization"); t != "" {
		return t
	}
	if t := QueryToken(r); t != "" {
		return t
	}
	return r.URL.Query().Get(TicketParam)
}

// ValidateRequest is Validate for request r, which may instead carry a
// ticket for it in ?mt=.
func ValidateRequest(r *http.Request, token string, publicKeyFunc func() (*ecdsa.PublicKey, error)) (bool, *Claims, error) {
	mt := r.URL.Query().Get(TicketParam)
	if mt == "" || token != mt || r.Header.Get("Authorization") == mt {
		return Validate(token, publicKeyFunc)
	}
	c, err := ParseToken(mt, publicKeyFunc)
	if err != nil {
		return false, nil, err
	}
	if c.Issuer != TicketIssuer || !ticketAllows(c, r) || !ticketFresh(c) {
		return false, nil, errors.New("invalid ticket")
	}
	if rec, ok := SessionRevoked(c.SessionID); ok {
		return false, nil, &SessionRevokedError{Reason: rec.Reason}
	}
	if err := CheckSession(c); err != nil {
		return false, nil, err
	}
	return true, c, nil
}

func ticketAllows(c *Claims, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	switch c.TicketKind {
	case TicketDir:
		p := r.URL.Query().Get("path")
		return TicketDirRoutes[r.URL.Path] && path.IsAbs(p) && path.Dir(path.Clean(p)) == c.TicketTarget && path.Clean(p) != c.TicketTarget
	case TicketURL:
		return TicketURLs.MatchString(c.TicketTarget) && r.URL.Path == c.TicketTarget
	}
	return false
}

// ponytail: per-process memory of tickets in use; a restarted service
// forgets them, so a video paused across a restart needs a new ticket.
var (
	ticketsMu   sync.Mutex
	ticketsUsed = map[string]time.Time{}
)

func ticketFresh(c *Claims) bool {
	if c.IssuedAt == nil || c.RegisteredClaims.ID == "" {
		return false
	}
	now := ticketNow()
	ticketsMu.Lock()
	defer ticketsMu.Unlock()
	last, used := ticketsUsed[c.RegisteredClaims.ID]
	if !used {
		last = c.IssuedAt.Time
	}
	if now.Sub(last) > TicketLifetime || now.Sub(c.IssuedAt.Time) > AccessTokenLifetime {
		return false
	}
	if !used {
		for k, t := range ticketsUsed {
			if now.Sub(t) > TicketLifetime {
				delete(ticketsUsed, k)
			}
		}
	}
	ticketsUsed[c.RegisteredClaims.ID] = now
	return true
}
