package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"
)

// errForbiddenDestination is returned by guardedDialer for any address
// this service must never connect out to on a user's (or a web page's)
// behalf.
var errForbiddenDestination = errors.New("destination address is not allowed")

// isForbiddenIP reports whether an outbound connection to ip must be
// refused. Loopback is the important one: every NivaroOS service
// (including this one - see requireAuth) skips JWT auth for loopback
// callers, so letting the browser proxy or download engine fetch
// http://127.0.0.1:<port>/... would let any web page opened in the lite
// browser drive those services' APIs unauthenticated, as root (e.g. queue a
// "download" into /etc/cron.d). Link-local covers cloud metadata endpoints
// (169.254.169.254). Ordinary LAN addresses stay reachable on purpose -
// router admin pages and other boxes on the network are legitimate things
// to browse or download from, and a request to this box's own LAN IP
// arrives with a non-loopback RemoteAddr, so it still has to authenticate.
func isForbiddenIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast()
}

// guardedDialer checks each resolved IP right before connecting (via
// net.Dialer.Control, which sees the literal address after DNS), so a name
// that resolves - or re-resolves, DNS-rebinding style - to 127.0.0.1 is
// still refused, while the standard dialer's IPv4/IPv6 racing (Happy
// Eyeballs) keeps working on hosts with a broken IPv6 route.
func guardedDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{
		Timeout:   20 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || isForbiddenIP(ip) {
				return errForbiddenDestination
			}
			return nil
		},
	}
	return d.DialContext
}

// newTransport builds the shared outbound transport. allowLoopback exists
// only for the unit tests (httptest servers listen on 127.0.0.1).
func newTransport(allowLoopback bool) *http.Transport {
	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 45 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if !allowLoopback {
		t.DialContext = guardedDialer()
	}
	return t
}

// A current desktop Chrome UA - plenty of download/file-host sites serve a
// stripped-down or "unsupported browser" page to anything else, and some
// refuse range requests to unknown clients outright.
const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
