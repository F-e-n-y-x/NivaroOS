package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// errForbiddenDestination is matched (errors.Is) by every refusal below.
var errForbiddenDestination = errors.New("destination address is not allowed")

// forbiddenError says why: loopback/link-local are never reachable;
// private (LAN, docker bridge, CGNAT/Tailscale, this box's own addresses)
// is reachable only for a host the user explicitly chose.
type forbiddenError struct{ private bool }

func (e forbiddenError) Error() string {
	if e.private {
		return "destination address is not allowed: it is on the local network - open it by typing its address yourself"
	}
	return errForbiddenDestination.Error()
}

func (e forbiddenError) Is(target error) bool { return target == errForbiddenDestination }

// isAlwaysForbiddenIP: addresses no outbound request may reach, ever.
// Loopback is the important one: every NivaroOS service trusts some
// loopback callers, so letting the browser proxy or download engine fetch
// http://127.0.0.1:<port>/... would let any web page opened in the lite
// browser drive those services' APIs, as root. Link-local covers cloud
// metadata endpoints (169.254.169.254).
func isAlwaysForbiddenIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
		if ip4[0] == 0 { // 0.0.0.0/8 ("this network") reaches the local host on Linux
			return true
		}
	}
	return ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast()
}

var extraPrivateNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{
		"100.64.0.0/10", // CGNAT - also Tailscale's address range
		"198.18.0.0/15", // benchmarking, used by some container/VPN setups
		"192.0.0.0/24",  // IETF protocol assignments
		"64:ff9b:1::/48",
	} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

// isPrivateIP: RFC 1918 (10/8, 172.16/12 - which includes docker's
// 172.17.0.0/16 bridge - 192.168/16), IPv6 ULA fc00::/7, CGNAT, and this
// host's own interface addresses. A web page in the lite browser must not
// be able to make the proxy talk to the LAN or to containers on the
// bridge (routers, other NivaroOS apps' unauthenticated admin ports) behind
// the user's back; the user can still open such a host by typing it.
func isPrivateIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsPrivate() {
		return true
	}
	for _, n := range extraPrivateNets {
		if n.Contains(ip) {
			return true
		}
	}
	return isOwnAddress(ip)
}

var ownAddrs struct {
	sync.Mutex
	at    time.Time
	addrs []net.IP
}

func isOwnAddress(ip net.IP) bool {
	ownAddrs.Lock()
	if time.Since(ownAddrs.at) > time.Minute {
		ownAddrs.at = time.Now()
		ownAddrs.addrs = nil
		if as, err := net.InterfaceAddrs(); err == nil {
			for _, a := range as {
				if n, ok := a.(*net.IPNet); ok {
					ownAddrs.addrs = append(ownAddrs.addrs, n.IP)
				}
			}
		}
	}
	addrs := ownAddrs.addrs
	ownAddrs.Unlock()
	for _, a := range addrs {
		if a.Equal(ip) {
			return true
		}
	}
	return false
}

// isForbiddenIP reports whether a connection to ip must be refused.
func isForbiddenIP(ip net.IP, allowPrivate bool) error {
	if isAlwaysForbiddenIP(ip) {
		return forbiddenError{}
	}
	if !allowPrivate && isPrivateIP(ip) {
		return forbiddenError{private: true}
	}
	return nil
}

// ---- per-request "the user chose this host" ----

type privateHostsKey struct{}

// withPrivateHosts marks hosts (the one a user typed into the address bar,
// or pasted as a download link) as allowed to resolve to a private
// address for requests made under ctx. Redirects to any other host stay on
// the strict path.
func withPrivateHosts(ctx context.Context, hosts ...string) context.Context {
	set := map[string]bool{}
	if prev, ok := ctx.Value(privateHostsKey{}).(map[string]bool); ok {
		for h := range prev {
			set[h] = true
		}
	}
	for _, h := range hosts {
		if h = normalizeHost(h); h != "" {
			set[h] = true
		}
	}
	return context.WithValue(ctx, privateHostsKey{}, set)
}

func privateAllowed(ctx context.Context, host string) bool {
	set, _ := ctx.Value(privateHostsKey{}).(map[string]bool)
	return set[normalizeHost(host)]
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	return strings.TrimSuffix(h, ".")
}

// ---- transport ----

// guardedTransport routes each request through one of two transports:
// strict (public internet only) or lan (private addresses allowed too) -
// separate so a pooled LAN connection can never be reused by a request
// that wasn't allowed to make it. The address check happens in the
// dialer's Control hook, on the literal IP after DNS, so a name that
// resolves - or re-resolves, DNS-rebinding style - to a forbidden address
// is still refused.
//
// With HTTP(S)_PROXY set, the dialer only ever sees the proxy's address;
// the real target is then checked here, by resolving it before handing the
// request to the proxy. (The proxy resolves the name again itself, so a
// rebinding DNS server could still race that - an operator who configures
// an outbound proxy should have it refuse private destinations too.)
type guardedTransport struct {
	strict, lan *http.Transport
	proxy       func(*http.Request) (*url.URL, error)
	proxyAddrs  map[string]bool
	allowAll    bool // unit tests only (httptest servers listen on 127.0.0.1)
	resolver    *net.Resolver
}

func targetAddr(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return net.JoinHostPort(normalizeHost(u.Hostname()), port)
}

func (g *guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if g.allowAll {
		return g.lan.RoundTrip(req)
	}
	host := req.URL.Hostname()
	allowPrivate := privateAllowed(req.Context(), host)
	// The dialer can only vouch for the address it connects to. That's
	// the proxy's when one is in use - and the proxy's own address is
	// dialed unchecked - so in both cases check the real target here.
	viaProxy := false
	if g.proxy != nil {
		if pu, err := g.proxy(req); err == nil && pu != nil {
			viaProxy = true
		}
	}
	if viaProxy || g.proxyAddrs[targetAddr(req.URL)] || net.ParseIP(normalizeHost(host)) != nil {
		if err := g.checkHost(req.Context(), host, allowPrivate); err != nil {
			if req.Body != nil {
				req.Body.Close()
			}
			return nil, err
		}
	}
	if allowPrivate {
		return g.lan.RoundTrip(req)
	}
	return g.strict.RoundTrip(req)
}

func (g *guardedTransport) checkHost(ctx context.Context, host string, allowPrivate bool) error {
	if ip := net.ParseIP(normalizeHost(host)); ip != nil {
		return isForbiddenIP(ip, allowPrivate)
	}
	if h := normalizeHost(host); h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return forbiddenError{}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addrs, err := g.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	for _, a := range addrs {
		if err := isForbiddenIP(a.IP, allowPrivate); err != nil {
			return err
		}
	}
	return nil
}

func (g *guardedTransport) CloseIdleConnections() {
	g.strict.CloseIdleConnections()
	g.lan.CloseIdleConnections()
}

// envProxyAddrs is the set of proxy host:port the transport may dial
// (proxies are commonly on loopback or the LAN - that's the one outbound
// connection to such an address that's legitimate).
func envProxyAddrs(proxy func(*http.Request) (*url.URL, error)) map[string]bool {
	out := map[string]bool{}
	for _, target := range []string{"http://example.com/", "https://example.com/"} {
		req, _ := http.NewRequest(http.MethodGet, target, nil)
		pu, err := proxy(req)
		if err != nil || pu == nil {
			continue
		}
		port := pu.Port()
		if port == "" {
			switch pu.Scheme {
			case "https":
				port = "443"
			case "socks5", "socks5h":
				port = "1080"
			default:
				port = "80"
			}
		}
		out[net.JoinHostPort(normalizeHost(pu.Hostname()), port)] = true
	}
	return out
}

func guardedDialer(allowPrivate bool, proxyAddrs map[string]bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	guarded := &net.Dialer{
		Timeout:   20 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return forbiddenError{}
			}
			return isForbiddenIP(ip, allowPrivate)
		},
	}
	plain := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if proxyAddrs[strings.ToLower(addr)] {
			return plain.DialContext(ctx, network, addr)
		}
		return guarded.DialContext(ctx, network, addr)
	}
}

func baseTransport() *http.Transport {
	return &http.Transport{
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
}

// newTransport builds the shared outbound transport. allowLoopback exists
// only for the unit tests (httptest servers listen on 127.0.0.1).
func newTransport(allowLoopback bool) http.RoundTripper {
	strict, lan := baseTransport(), baseTransport()
	g := &guardedTransport{strict: strict, lan: lan, proxy: http.ProxyFromEnvironment, resolver: net.DefaultResolver}
	if allowLoopback {
		g.allowAll = true
		return g
	}
	proxies := envProxyAddrs(http.ProxyFromEnvironment)
	g.proxyAddrs = proxies
	strict.DialContext = guardedDialer(false, proxies)
	lan.DialContext = guardedDialer(true, proxies)
	return g
}

// A current desktop Chrome UA - plenty of download/file-host sites serve a
// stripped-down or "unsupported browser" page to anything else, and some
// refuse range requests to unknown clients outright.
const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
