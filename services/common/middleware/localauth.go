package middleware

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"
)

// LocalAutomationHeader is set by the gateway (and only the gateway - it
// strips any client-supplied copy first) on a proxied request whose own
// peer was same-host, non-browser automation (e.g. nivaroos-cli talking to
// http://localhost:<gateway port>). Backends honour it only when their
// direct socket peer is loopback, i.e. the request really came through the
// local gateway.
const LocalAutomationHeader = "X-Nivaroos-Local-Automation"

// IsLoopbackAddr reports whether a net/http RemoteAddr ("ip:port") is a
// loopback address. This is the direct socket peer - never a header.
func IsLoopbackAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hasBrowserMarks / hasProxyMarks: every browser sends Sec-Fetch-Site (and Origin on
// cross-origin/WebSocket requests); every well-behaved reverse proxy adds
// X-Forwarded-For / X-Real-IP.
func hasBrowserMarks(r *http.Request) bool {
	return r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != ""
}

// proxyHeaders: any of these means the request was relayed on behalf of
// someone else - the local gateway (X-Forwarded-For), cloudflared
// (Cf-Connecting-Ip, plus X-Forwarded-For from Cloudflare's edge),
// `tailscale serve` (Tailscale-User-*, X-Forwarded-For) or any other
// reverse proxy on this box. Loopback is only "local" when none are present.
var proxyHeaders = []string{
	"X-Forwarded-For", "X-Real-IP", "Forwarded", "X-Forwarded-Host",
	"Cf-Connecting-Ip", "True-Client-Ip", "Cf-Ray",
	"Tailscale-User-Login", "Tailscale-User-Name", "Tailscale-Headers-Info",
	"Via",
}

// HasProxyMarks reports whether r carries any header a relay adds (see
// proxyHeaders) - for services that keep their own loopback checks.
func HasProxyMarks(r *http.Request) bool { return hasProxyMarks(r) }

func hasProxyMarks(r *http.Request) bool {
	for _, h := range proxyHeaders {
		if len(r.Header.Values(h)) > 0 {
			return true
		}
	}
	return false
}

// IsDirectLocalAutomation reports whether r arrived straight from a
// same-host non-browser client: loopback socket peer, no proxy headers,
// no browser headers. This is what the gateway checks before marking a
// request with LocalAutomationHeader.
func IsDirectLocalAutomation(r *http.Request) bool {
	return !LoopbackUntrusted() && IsLoopbackAddr(r.RemoteAddr) && !hasProxyMarks(r) && !hasBrowserMarks(r)
}

// IsLocalAutomation reports whether r may skip JWT auth as same-host
// automation. Unlike echo's c.RealIP() (which trusts X-Forwarded-For /
// X-Real-IP and therefore any header an attacker sends, and which makes
// every gateway-proxied request look like it came from wherever the
// gateway says), this looks only at the socket peer:
//
//   - peer must be loopback, and
//   - no Origin / Sec-Fetch-Site (so a browser tab on this host, or a
//     DNS-rebinding page aimed at 127.0.0.1, never qualifies), and
//   - either no X-Forwarded-For / X-Real-IP at all (a direct local call
//     between services or from a script), or the local gateway vouched
//     for it with LocalAutomationHeader.
//
// Tailscale peers (100.64.0.0/10, fd7a:115c:a1e0::/48) are never loopback
// with tailscaled's normal kernel TUN, so they always need a token. With
// tailscaled in userspace-networking mode, though, inbound tailnet
// connections are re-dialled from 127.0.0.1 and would look local - see
// LoopbackUntrusted, which switches loopback trust off entirely then.
func IsLocalAutomation(r *http.Request) bool {
	if LoopbackUntrusted() || !IsLoopbackAddr(r.RemoteAddr) || hasBrowserMarks(r) {
		return false
	}
	if !hasProxyMarks(r) {
		return true
	}
	return r.Header.Get(LocalAutomationHeader) == "1"
}

// MarkLocalAutomation is called by the gateway on every request before
// proxying it: it removes any client-supplied LocalAutomationHeader and
// re-adds it only when the gateway's own peer qualifies. Must run before
// the gateway rewrites X-Forwarded-For.
func MarkLocalAutomation(r *http.Request) {
	trusted := IsDirectLocalAutomation(r)
	r.Header.Del(LocalAutomationHeader)
	if trusted {
		r.Header.Set(LocalAutomationHeader, "1")
	}
}

// MatchRoute reports whether an echo-style pattern ("/v1/container/:id/terminal")
// matches a concrete request path. ":name" matches exactly one non-empty
// segment, "*" as the last segment matches the rest. Trailing slashes are
// ignored.
func MatchRoute(pattern, path string) bool {
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	xs := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range ps {
		if p == "*" && i == len(ps)-1 {
			return true
		}
		if i >= len(xs) {
			return false
		}
		if strings.HasPrefix(p, ":") {
			if xs[i] == "" {
				return false
			}
			continue
		}
		if p != xs[i] {
			return false
		}
	}
	return len(ps) == len(xs)
}

// LocalAutomationSkipper builds a JWT Skipper that skips auth only for
// IsLocalAutomation requests, and never for any route matching one of
// neverSkip (echo route patterns - e.g. interactive terminals, which must
// always carry a real user token even from loopback).
func LocalAutomationSkipper(neverSkip ...string) func(*echo.Context) bool {
	return func(c *echo.Context) bool {
		r := c.Request()
		path := r.URL.Path
		for _, p := range neverSkip {
			if MatchRoute(p, path) || (c.Path() != "" && c.Path() == p) {
				return false
			}
		}
		return IsLocalAutomation(r)
	}
}

// SameHostOrigin reports whether r's Origin names the same host (any
// scheme/port) the request was addressed to.
func SameHostOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	// Also accept the host an outer reverse proxy says the browser used
	// (X-Forwarded-Host) - a cross-site page can't set that header on a
	// browser request, so it doesn't weaken the check.
	for _, h := range []string{r.Host, strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])} {
		if hostOnly(h) != "" && strings.EqualFold(u.Hostname(), hostOnly(h)) {
			return true
		}
	}
	return false
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		hostport = h
	}
	return strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
}

// CheckWebSocketOrigin is a gorilla websocket.Upgrader CheckOrigin: accept
// non-browser clients (no Origin - the mobile app, scripts) and pages
// served from this same host; reject cross-site pages (CSWSH).
func CheckWebSocketOrigin(r *http.Request) bool {
	if r.Header.Get("Origin") == "" {
		return true
	}
	return SameHostOrigin(r)
}
