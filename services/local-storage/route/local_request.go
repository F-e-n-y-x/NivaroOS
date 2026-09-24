package route

import (
	"net"
	"net/http"
	"strings"
)

// IsTrustedLocalRequest: a request may skip JWT only when it comes straight
// from a local process - the socket peer itself is loopback, and nothing
// says it was proxied (X-Forwarded-For, X-Real-IP, Forwarded: the gateway's
// httputil.ReverseProxy adds X-Forwarded-For) or sent by a browser (Origin,
// Sec-Fetch-Site: a web page can make the browser talk to 127.0.0.1).
//
// The same rule is being added to services/common by another change; it is
// implemented here locally so this service doesn't depend on that landing.
func IsTrustedLocalRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	for _, h := range []string{"X-Forwarded-For", "X-Real-IP", "X-Real-Ip", "Forwarded", "X-Forwarded-Host", "Origin", "Sec-Fetch-Site", "Sec-Fetch-Mode"} {
		if len(r.Header.Values(h)) > 0 {
			return false
		}
	}
	return true
}
