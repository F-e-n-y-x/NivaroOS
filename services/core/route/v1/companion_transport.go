package v1

// How the server reaches a phone's file server (S-04), in order:
//
//  1. Direct, over one of the addresses the phone reported (its Wi-Fi LAN
//     address first, then its Tailscale addresses). Only the device's own
//     reported addresses are dialled, only private-LAN / Tailscale ranges
//     are allowed (never loopback, link-local, public or metadata
//     addresses), a MagicDNS name must resolve into Tailscale's range, and
//     an address is used only after the phone behind it proved it holds
//     this device's secret (GET /hello: HMAC over a fresh nonce - the
//     secret itself is never sent to an unproven address). The address
//     that worked is cached.
//  2. Through the phone's reverse tunnel to the server, streamed
//     (companion_tunnel_mux.go), when the phone's app supports it.
//
// companionDo is the one entry point every proxy (list, download, range,
// upload, rename, mkdir, delete, thumbnails, copy/move) uses.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Route kinds reported to the UI (CompanionDevice.Route).
const (
	companionRouteLAN       = "lan"
	companionRouteTailscale = "tailscale"
	companionRouteTunnel    = "tunnel"
	// companionRouteTunnelList: an older app's tunnel - folders list, files
	// don't open until the app is updated.
	companionRouteTunnelList = "tunnel_list"
)

var (
	tailscaleV4 = mustCIDR("100.64.0.0/10")
	tailscaleV6 = mustCIDR("fd7a:115c:a1e0::/48")
	lanRanges   = []*net.IPNet{mustCIDR("10.0.0.0/8"), mustCIDR("172.16.0.0/12"), mustCIDR("192.168.0.0/16"), mustCIDR("fc00::/7")}
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// companionIPKindHook lets tests classify loopback test servers; nil in
// production.
var companionIPKindHook func(ip net.IP) (kind string, ok bool)

// companionIPKind classifies an IP as "lan", "tailscale" or "" (never dial).
func companionIPKind(ip net.IP) string {
	if companionIPKindHook != nil {
		if k, ok := companionIPKindHook(ip); ok {
			return k
		}
	}
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return ""
	}
	if tailscaleV4.Contains(ip) || tailscaleV6.Contains(ip) {
		return companionRouteTailscale
	}
	for _, n := range lanRanges {
		if n.Contains(ip) {
			return companionRouteLAN
		}
	}
	return ""
}

// companionAddrKind classifies a reported address: an IP literal, or a
// MagicDNS name (<host>.<tailnet>.ts.net), which counts as Tailscale and is
// checked again at dial time.
func companionAddrKind(addr string) string {
	addr = strings.TrimSpace(addr)
	if ip := net.ParseIP(addr); ip != nil {
		return companionIPKind(ip)
	}
	if isMagicDNSName(addr) {
		return companionRouteTailscale
	}
	return ""
}

func isMagicDNSName(h string) bool {
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	if len(h) > 253 || !strings.HasSuffix(h, ".ts.net") || strings.Count(h, ".") < 3 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// companionEndpoint is one address to try.
type companionEndpoint struct {
	Host string // IP literal or MagicDNS name
	Kind string // lan | tailscale
	// Legacy: dev.IP of an app too old for /hello - accepted on a /status
	// answer, as before S-04 (LAN only).
	Legacy bool
}

func (e companionEndpoint) hostPort(port int) string {
	return net.JoinHostPort(e.Host, strconv.Itoa(port))
}

// companionCandidates lists the device's reported addresses in the order
// they're tried: LAN, then Tailscale IPv4, Tailscale IPv6, MagicDNS.
// Anything outside the allowed ranges is dropped.
func companionCandidates(dev *CompanionDevice) []companionEndpoint {
	seen := map[string]bool{}
	var lan, ts4, ts6, names []companionEndpoint
	add := func(a string) {
		a = strings.TrimSpace(a)
		if a == "" || seen[strings.ToLower(a)] {
			return
		}
		seen[strings.ToLower(a)] = true
		kind := companionAddrKind(a)
		if kind == "" {
			return
		}
		ep := companionEndpoint{Host: a, Kind: kind}
		ip := net.ParseIP(a)
		switch {
		case kind == companionRouteLAN:
			lan = append(lan, ep)
		case ip == nil:
			names = append(names, ep)
		case ip.To4() != nil:
			ts4 = append(ts4, ep)
		default:
			ts6 = append(ts6, ep)
		}
	}
	add(dev.IP)
	for _, a := range dev.Addresses {
		add(a)
	}
	out := append(append(append(lan, ts4...), ts6...), names...)
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// companionDialer dials only addresses companionIPKind allows - a MagicDNS
// name resolving to anything outside Tailscale's range is refused at
// connect time (DNS rebinding).
var companionDialer = &net.Dialer{
	Timeout:   4 * time.Second,
	KeepAlive: 30 * time.Second,
	Control: func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if companionIPKind(net.ParseIP(host)) == "" {
			return fmt.Errorf("companion: refusing to connect to %s", host)
		}
		return nil
	},
}

// Direct server->phone calls. The dial timeout is short: an address the
// server can't route to must fail fast so the next one (or the tunnel) is
// tried.
var companionTransport = &http.Transport{
	DialContext:         companionDialer.DialContext,
	TLSHandshakeTimeout: 10 * time.Second,
	MaxIdleConnsPerHost: 8,
	IdleConnTimeout:     90 * time.Second,
}

func companionClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: companionTransport}
}

// Probe timeouts per kind: a LAN answers in milliseconds; a Tailscale path
// may need a DERP hop first.
var (
	companionProbeTimeoutLAN       = 1500 * time.Millisecond
	companionProbeTimeoutTailscale = 4 * time.Second
	companionRouteTTL              = 2 * time.Minute
	companionNoDirectTTL           = 20 * time.Second
)

type companionRouteEntry struct {
	ep      *companionEndpoint // nil: no direct address worked
	sig     string             // the addresses/secret it was resolved for
	expires time.Time
}

var (
	companionRoutesMu sync.Mutex
	companionRoutes   = map[string]*companionRouteEntry{}
	// companionResolving dedupes concurrent resolutions per device (a
	// thumbnail grid asks for dozens of files at once).
	companionResolving = map[string]chan struct{}{}
)

func companionRouteSig(dev *CompanionDevice) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(dev.Port))
	for _, c := range companionCandidates(dev) {
		b.WriteString("|" + c.Host)
	}
	// A new secret (re-registration after a core restart) re-verifies.
	sum := sha256.Sum256([]byte(dev.Secret))
	b.WriteString("|" + hex.EncodeToString(sum[:4]))
	return b.String()
}

// forgetCompanionRoute drops the cached route (a request on it failed).
func forgetCompanionRoute(id string) {
	companionRoutesMu.Lock()
	delete(companionRoutes, id)
	companionRoutesMu.Unlock()
}

// companionHelloProof is what the phone answers /hello with.
func companionHelloProof(secret, deviceID, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("nivaroos-companion-hello\n" + deviceID + "\n" + nonce))
	return hex.EncodeToString(mac.Sum(nil))
}

// probeCompanionEndpoint checks that ep reaches THIS device's file server.
func probeCompanionEndpoint(ctx context.Context, dev *CompanionDevice, ep companionEndpoint, port int) bool {
	timeout := companionProbeTimeoutLAN
	if ep.Kind == companionRouteTailscale {
		timeout = companionProbeTimeoutTailscale
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	nb := make([]byte, 16)
	_, _ = rand.Read(nb)
	nonce := hex.EncodeToString(nb)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+ep.hostPort(port)+"/hello?nonce="+nonce, nil)
	if err != nil {
		return false
	}
	resp, err := companionTransport.RoundTrip(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var body struct {
			ID    string `json:"id"`
			Proof string `json:"proof"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body) != nil || dev.Secret == "" {
			return false
		}
		want := companionHelloProof(dev.Secret, dev.ID, nonce)
		return body.ID == dev.ID && hmac.Equal([]byte(body.Proof), []byte(want))
	}
	// An app from before /hello answers 401 (it wants the secret) or 404.
	// Only its own LAN address is trusted then, on a /status answer, as
	// the server always did.
	if !ep.Legacy || (resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusNotFound) {
		return false
	}
	sreq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+ep.hostPort(port)+"/status", nil)
	if err != nil {
		return false
	}
	sresp, err := companionTransport.RoundTrip(sreq)
	if err != nil {
		return false
	}
	sresp.Body.Close()
	return sresp.StatusCode == http.StatusOK
}

// companionDirectEndpoint returns the verified direct address to use, or
// nil when none works right now. Results are cached (a miss for
// companionNoDirectTTL, so a phone away from home doesn't pay a probe per
// request). The probe itself runs detached from ctx: a caller that stops
// waiting (the device list has a small budget) leaves it to finish and
// fill the cache for the next one.
func companionDirectEndpoint(ctx context.Context, dev *CompanionDevice) *companionEndpoint {
	sig := companionRouteSig(dev)
	for {
		companionRoutesMu.Lock()
		if e := companionRoutes[dev.ID]; e != nil && e.sig == sig && time.Now().Before(e.expires) {
			companionRoutesMu.Unlock()
			return e.ep
		}
		wait, busy := companionResolving[dev.ID]
		if !busy {
			wait = make(chan struct{})
			companionResolving[dev.ID] = wait
			snap := *dev
			go func() {
				pctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				ep := resolveCompanionEndpoint(pctx, &snap)
				companionRoutesMu.Lock()
				ttl := companionRouteTTL
				if ep == nil {
					ttl = companionNoDirectTTL
				}
				companionRoutes[snap.ID] = &companionRouteEntry{ep: ep, sig: sig, expires: time.Now().Add(ttl)}
				delete(companionResolving, snap.ID)
				close(wait)
				companionRoutesMu.Unlock()
			}()
		}
		companionRoutesMu.Unlock()
		select {
		case <-wait:
			// Loop: read the result (or resolve again if it was for other
			// addresses).
			companionRoutesMu.Lock()
			e := companionRoutes[dev.ID]
			companionRoutesMu.Unlock()
			if e != nil && e.sig == sig {
				return e.ep
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// resolveCompanionEndpoint probes every candidate at once and returns the
// first one in preference order that proved itself - without waiting for a
// slower, lower-preference one once a better one answered.
func resolveCompanionEndpoint(ctx context.Context, dev *CompanionDevice) *companionEndpoint {
	cands := companionCandidates(dev)
	if len(cands) == 0 || dev.Secret == "" && !companionHasLANIP(dev.IP) {
		return nil
	}
	port := companionPort(dev)
	for i := range cands {
		cands[i].Legacy = cands[i].Kind == companionRouteLAN && cands[i].Host == strings.TrimSpace(dev.IP)
	}
	type result struct {
		i  int
		ok bool
	}
	results := make(chan result, len(cands))
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for i, c := range cands {
		go func(i int, c companionEndpoint) {
			results <- result{i, probeCompanionEndpoint(pctx, dev, c, port)}
		}(i, c)
	}
	status := make([]int, len(cands)) // 0 pending, 1 ok, -1 failed
	for range cands {
		r := <-results
		if r.ok {
			status[r.i] = 1
		} else {
			status[r.i] = -1
		}
		// The best answer is final once everything before it failed.
		for i := range status {
			if status[i] == 0 {
				break
			}
			if status[i] == 1 {
				ep := cands[i]
				return &ep
			}
		}
	}
	return nil
}

func companionPort(dev *CompanionDevice) int {
	if dev.Port > 0 {
		return dev.Port
	}
	return 8765
}

// companionRequest is one call to the phone's file server.
type companionRequest struct {
	Method string
	Path   string // "/download"
	Query  url.Values
	Header http.Header
	Body   io.Reader // nil for none
	Size   int64     // body length, -1 unknown
	// Timeout bounds the whole exchange (0: ctx only).
	Timeout time.Duration
}

// companionResponse is the phone's answer plus how it was reached.
type companionResponse struct {
	*http.Response
	Route string
}

// errCompanionUnreachable: no direct address works and the tunnel can't
// carry the request. The message says why, in words for the user.
type errCompanionUnreachable struct {
	msg   string
	cause error
}

func (e *errCompanionUnreachable) Error() string { return e.msg }
func (e *errCompanionUnreachable) Unwrap() error { return e.cause }

func companionDisplayName(dev *CompanionDevice) string {
	if strings.TrimSpace(dev.Name) == "" {
		return "The phone"
	}
	return dev.Name
}

func companionUnreachableErr(dev *CompanionDevice, cause error) error {
	name := companionDisplayName(dev)
	if t := companionTunnelFor(dev.ID); t != nil && t.streams() == nil {
		return &errCompanionUnreachable{msg: name + " is connected away from home, but its NivaroOS app is too old to send files that way - update the app on the phone (or turn on Tailscale there)", cause: cause}
	}
	if companionOnlineRemotely(dev) {
		return &errCompanionUnreachable{msg: name + " is online but can't be reached right now - open the NivaroOS app on it and turn on file sharing", cause: cause}
	}
	return &errCompanionUnreachable{msg: name + " is offline - open the NivaroOS app on it and turn on file sharing", cause: cause}
}

// companionBodyCounter tells whether a body was touched before a failed dial,
// so the request can be retried elsewhere.
type companionBodyCounter struct {
	r io.Reader
	n int64
}

func (c *companionBodyCounter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// isDialError: the request never reached the phone (nothing was sent).
func isDialError(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

// companionDo sends req to the phone, directly if it can, else through the
// tunnel. The caller closes the response body.
func companionDo(ctx context.Context, dev *CompanionDevice, req companionRequest) (*companionResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var cancel context.CancelFunc = func() {}
	if req.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	resp, err := companionDoInner(ctx, dev, req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func companionDoInner(ctx context.Context, dev *CompanionDevice, req companionRequest) (*companionResponse, error) {
	query := ""
	if req.Query != nil {
		query = req.Query.Encode()
	}
	var body *companionBodyCounter
	if req.Body != nil {
		body = &companionBodyCounter{r: req.Body}
	}

	var directErr error
	// Twice at most: a cached address that stopped answering (the phone
	// left the Wi-Fi) is dropped and the others are probed again at once.
	for attempt := 0; attempt < 2; attempt++ {
		ep := companionDirectEndpoint(ctx, dev)
		if ep == nil {
			break
		}
		u := "http://" + ep.hostPort(companionPort(dev)) + req.Path
		if query != "" {
			u += "?" + query
		}
		var rbody io.Reader
		if body != nil {
			rbody = body
		}
		hreq, err := http.NewRequestWithContext(ctx, req.Method, u, rbody)
		if err != nil {
			return nil, err
		}
		for k, vs := range req.Header {
			for _, v := range vs {
				hreq.Header.Add(k, v)
			}
		}
		if body != nil {
			hreq.ContentLength = req.Size
			if req.Size < 0 {
				hreq.ContentLength = -1
			}
		}
		hreq.Header.Set("X-Companion-Secret", dev.Secret)
		resp, err := companionTransport.RoundTrip(hreq)
		if err == nil {
			markCompanionSeen(dev)
			return &companionResponse{Response: resp, Route: ep.Kind}, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// The cached address stopped working: forget it; retry elsewhere
		// only if nothing of the body was sent.
		forgetCompanionRoute(dev.ID)
		directErr = err
		if body != nil && (body.n > 0 || !isDialError(err)) {
			return nil, companionUnreachableErr(dev, err)
		}
	}

	t := companionTunnelFor(dev.ID)
	if t == nil || t.streams() == nil {
		return nil, companionUnreachableErr(dev, directErr)
	}
	headers := map[string]string{}
	for k := range req.Header {
		headers[k] = req.Header.Get(k)
	}
	if body != nil && req.Size >= 0 {
		headers["Content-Length"] = strconv.FormatInt(req.Size, 10)
	}
	treq := tunnelRequest{Method: req.Method, Path: req.Path, Query: query, Headers: headers}
	if body != nil {
		treq.Body = body
	}
	resp, err := t.streams().Do(ctx, treq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, companionUnreachableErr(dev, err)
	}
	markCompanionSeen(dev)
	return &companionResponse{Response: resp, Route: companionRouteTunnel}, nil
}

// companionCurrentRoute is how the device is reached right now, for the
// device list: a direct route, the streaming tunnel, a list-only tunnel,
// or "" (not reachable).
func companionCurrentRoute(ctx context.Context, dev *CompanionDevice) string {
	if ep := companionDirectEndpoint(ctx, dev); ep != nil {
		return ep.Kind
	}
	if t := companionTunnelFor(dev.ID); t != nil {
		if t.streams() != nil {
			return companionRouteTunnel
		}
		return companionRouteTunnelList
	}
	return ""
}
