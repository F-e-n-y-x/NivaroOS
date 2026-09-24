package jobs

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
)

// Download tokens (spec §11, §14): POST /downloads hands out a random
// token that GET /downloads/<token> accepts once, within 60 s, from the
// IP that asked for it - so a browser download needs no JWT in its URL.

const downloadTokenTTL = 60 * time.Second

type downloadGrant struct {
	req     DownloadRequest
	ip      string
	expires time.Time
}

type downloadTokens struct {
	mu     sync.Mutex
	grants map[string]downloadGrant
}

func newDownloadTokens() *downloadTokens {
	return &downloadTokens{grants: map[string]downloadGrant{}}
}

func (d *downloadTokens) issue(req DownloadRequest, ip string, now time.Time) DownloadToken {
	tok := "dl_" + randomHex(16)
	d.mu.Lock()
	d.grants[tok] = downloadGrant{req: req, ip: ip, expires: now.Add(downloadTokenTTL)}
	d.mu.Unlock()
	return DownloadToken{Token: tok, ExpiresIn: int(downloadTokenTTL / time.Second)}
}

// redeem consumes a token. It is gone after the first attempt, even a
// wrong-IP one: a leaked link must not stay usable.
func (d *downloadTokens) redeem(tok, ip string, now time.Time) (DownloadRequest, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	g, ok := d.grants[tok]
	if !ok {
		return DownloadRequest{}, false
	}
	delete(d.grants, tok)
	if now.After(g.expires) || g.ip != ip {
		return DownloadRequest{}, false
	}
	return g.req, true
}

func (d *downloadTokens) gc(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, g := range d.grants {
		if now.After(g.expires) {
			delete(d.grants, k)
		}
	}
}

// clientIP is the browser's address. Behind the local gateway (loopback
// peer with X-Forwarded-For) it is the last X-Forwarded-For hop - the one
// the gateway added itself; earlier hops are whatever the client sent.
// Any other peer is taken as is.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if middleware.IsLoopbackAddr(r.RemoteAddr) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if last := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(last) != nil {
				return last
			}
		}
	}
	return host
}

// ---------------------------------------------------------------------
// Rate limit

// rateLimiter allows n requests per window per key (spec §11: create,
// update and restore, 30 a minute per user).
type rateLimiter struct {
	n      int
	window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
}

func newRateLimiter(n int, window time.Duration) *rateLimiter {
	return &rateLimiter{n: n, window: window, hits: map[string][]time.Time{}}
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	hs := l.hits[key]
	i := 0
	for i < len(hs) && !hs[i].After(cutoff) {
		i++
	}
	hs = hs[i:]
	if len(hs) >= l.n {
		l.hits[key] = hs
		return false
	}
	l.hits[key] = append(hs, now)
	return true
}
