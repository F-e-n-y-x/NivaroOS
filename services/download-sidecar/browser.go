package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// The lite browser is a rewriting proxy: a page the UI shows in an iframe
// is fetched here, server-side, and every URL in it is rewritten to come
// back through here as well. That's the only way an arbitrary site can be
// shown inside the desktop at all (most send X-Frame-Options/CSP
// frame-ancestors, which this strips), and it's also what lets the proxy
// block ads per request, keep a cookie jar a later download can reuse,
// and notice when a link is actually a file download and hand it to the
// download manager instead.
//
// Proxied URLs look like /b/<session>/<scheme>/<host>/<path>?<query> - the
// real URL's path stays a real path, so relative links in a page (and in
// its scripts) resolve correctly with no rewriting at all. Root-relative
// ones ("/api/x") land on this server's own root and are bounced back into
// the right site via their Referer (see TryRefererRedirect).
//
// The session ID in the path is an unguessable capability minted by an
// authenticated API call - the iframe can't send the JWT header, and
// proxied content must never see the user's real NivaroOS token.
//
// Isolation caveat, shared by every rewriting proxy: every proxied site
// runs on this one origin, so they share localStorage and can read each
// other's proxied responses. It's a tool for reaching download pages, not a
// place to log into a bank.

//go:embed inject.js
var injectJS string

const browserPrefix = "/b/"

type Capture struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	Resumable   bool      `json:"resumable"`
	ContentType string    `json:"content_type"`
	Referer     string    `json:"referer,omitempty"`
	HasCookies  bool      `json:"has_cookies"`
	CreatedAt   time.Time `json:"created_at"`

	headers map[string]string
}

type BrowserSession struct {
	ID       string
	jar      *cookiejar.Jar
	client   *http.Client
	created  time.Time
	lastUsed atomic.Int64

	mu            sync.Mutex
	captures      map[string]*Capture
	blockedByHost map[string]int
	blockedTotal  int
	allowHosts    map[string]bool
	// Hosts the user typed into the address bar (or opened from history /
	// the home page) - the only ones the proxy may reach on a private
	// address (see netguard.go).
	typedHosts map[string]bool
	// Documents this proxy served into the browser, by the nav ID injected
	// into each one, so the address bar shows what was really loaded
	// rather than whatever URL a page's script claims.
	navs map[string]navRecord
}

type navRecord struct {
	url  string
	host string
	at   time.Time
}

const maxNavs = 500

type Browser struct {
	mu        sync.Mutex
	sessions  map[string]*BrowserSession
	transport http.RoundTripper
	adblock   *Adblocker
	settings  *SettingsStore
}

func NewBrowser(transport http.RoundTripper, adblock *Adblocker, settings *SettingsStore) *Browser {
	b := &Browser{sessions: map[string]*BrowserSession{}, transport: transport, adblock: adblock, settings: settings}
	go b.gc()
	return b
}

const sessionIdle = 12 * time.Hour

func (b *Browser) gc() {
	for range time.Tick(10 * time.Minute) {
		cutoff := time.Now().Add(-sessionIdle).Unix()
		b.mu.Lock()
		for id, s := range b.sessions {
			if s.lastUsed.Load() < cutoff {
				delete(b.sessions, id)
			}
		}
		b.mu.Unlock()
	}
}

func (b *Browser) NewSession() *BrowserSession {
	jar, _ := cookiejar.New(nil)
	s := &BrowserSession{
		ID:            newID() + newID(),
		jar:           jar,
		created:       time.Now(),
		captures:      map[string]*Capture{},
		blockedByHost: map[string]int{},
		allowHosts:    map[string]bool{},
		typedHosts:    map[string]bool{},
		navs:          map[string]navRecord{},
	}
	s.client = &http.Client{
		Transport: b.transport,
		Jar:       jar,
		// Redirects go back to the browser (with Location rewritten) so the
		// frame's own URL - and therefore relative-link resolution -
		// follows them.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       0,
	}
	s.lastUsed.Store(time.Now().Unix())
	b.mu.Lock()
	b.sessions[s.ID] = s
	b.mu.Unlock()
	return s
}

func (b *Browser) Session(id string) *BrowserSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sessions[id]
	if s != nil {
		s.lastUsed.Store(time.Now().Unix())
	}
	return s
}

func (b *Browser) DeleteSession(id string) {
	b.mu.Lock()
	delete(b.sessions, id)
	b.mu.Unlock()
}

func (s *BrowserSession) ClearCookies() {
	jar, _ := cookiejar.New(nil)
	s.mu.Lock()
	s.jar = jar
	s.client.Jar = jar
	s.mu.Unlock()
}

type SessionStats struct {
	ID            string         `json:"id"`
	Prefix        string         `json:"prefix"`
	BlockedByHost map[string]int `json:"blocked_by_host"`
	BlockedTotal  int            `json:"blocked_total"`
}

func (s *BrowserSession) Stats() SessionStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := make(map[string]int, len(s.blockedByHost))
	for k, v := range s.blockedByHost {
		m[k] = v
	}
	return SessionStats{ID: s.ID, Prefix: browserPrefix + s.ID + "/", BlockedByHost: m, BlockedTotal: s.blockedTotal}
}

func (s *BrowserSession) Capture(id string) (*Capture, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.captures[id]
	return c, ok
}

// AllowTypedHost records that the user explicitly asked for host.
func (s *BrowserSession) AllowTypedHost(host string) {
	host = normalizeHost(host)
	if host == "" {
		return
	}
	s.mu.Lock()
	if len(s.typedHosts) < 1000 {
		s.typedHosts[host] = true
	}
	s.mu.Unlock()
}

func (s *BrowserSession) typedHostList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.typedHosts))
	for h := range s.typedHosts {
		out = append(out, h)
	}
	return out
}

func (s *BrowserSession) recordNav(u *url.URL) string {
	id := newID()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.navs) >= maxNavs {
		var oldestID string
		var oldest time.Time
		for k, v := range s.navs {
			if oldestID == "" || v.at.Before(oldest) {
				oldestID, oldest = k, v.at
			}
		}
		delete(s.navs, oldestID)
	}
	s.navs[id] = navRecord{url: u.String(), host: strings.ToLower(u.Host), at: time.Now()}
	return id
}

// Nav returns the URL of a document the proxy served under this nav ID.
func (s *BrowserSession) Nav(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.navs[id]
	return n.url, ok
}

// HeadersFor returns the request headers a download of rawURL may reuse
// from capture c. The capture's recorded cookies are only replayed to the
// host they were captured for; for any other URL (the user edited the
// link in the Add window) only the session jar's own cookies for that URL
// - which the jar domain-matches itself - are sent.
func (s *BrowserSession) HeadersFor(c *Capture, rawURL string) map[string]string {
	out := map[string]string{}
	cu, err1 := url.Parse(c.URL)
	u, err2 := url.Parse(strings.TrimSpace(rawURL))
	if err1 == nil && err2 == nil && strings.EqualFold(cu.Host, u.Host) && cu.Scheme == u.Scheme {
		for k, v := range c.headers {
			out[k] = v
		}
		return out
	}
	if c.Referer != "" {
		out["Referer"] = c.Referer
	}
	if err2 != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return out
	}
	s.mu.Lock()
	jar := s.jar
	s.mu.Unlock()
	var parts []string
	for _, ck := range jar.Cookies(u) {
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	if len(parts) > 0 {
		out["Cookie"] = strings.Join(parts, "; ")
	}
	return out
}

func (s *BrowserSession) countBlocked(pageHost string) {
	s.mu.Lock()
	s.blockedByHost[pageHost]++
	s.blockedTotal++
	s.mu.Unlock()
}

// ---- URL mapping ----

func (s *BrowserSession) proxyPath(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	out := browserPrefix + s.ID + "/" + u.Scheme + "/" + u.Host + p
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	} else if u.ForceQuery {
		out += "?"
	}
	if u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	return out
}

// decodeProxyPath turns "/b/<sid>/https/host/rest" (escaped path + raw
// query) back into the real URL. Returns the session ID too.
func decodeProxyPath(escapedPath, rawQuery string) (string, *url.URL, error) {
	if !strings.HasPrefix(escapedPath, browserPrefix) {
		return "", nil, errors.New("not a proxy path")
	}
	rest := escapedPath[len(browserPrefix):]
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 {
		return "", nil, errors.New("incomplete proxy path")
	}
	sid, scheme, host := parts[0], parts[1], parts[2]
	if scheme != "http" && scheme != "https" {
		return sid, nil, errors.New("unsupported scheme")
	}
	if host == "" {
		return sid, nil, errors.New("missing host")
	}
	p := "/"
	if len(parts) == 4 {
		p = "/" + parts[3]
	}
	raw := scheme + "://" + host + p
	if rawQuery != "" {
		raw += "?" + rawQuery
	}
	u, err := url.Parse(raw)
	if err != nil {
		return sid, nil, err
	}
	return sid, u, nil
}

// realFromProxyURL decodes a full proxied URL as it appears in a Referer
// or Origin header (http://nas:28642/b/...).
func realFromProxyURL(raw string) (string, *url.URL) {
	pu, err := url.Parse(raw)
	if err != nil {
		return "", nil
	}
	sid, u, err := decodeProxyPath(pu.EscapedPath(), pu.RawQuery)
	if err != nil {
		return sid, nil
	}
	return sid, u
}

// rewriteURL maps one URL found in a page to its proxied form.
func (s *BrowserSession) rewriteURL(base *url.URL, raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" || strings.HasPrefix(t, "#") {
		return raw
	}
	lower := strings.ToLower(t)
	for _, p := range []string{"javascript:", "data:", "blob:", "mailto:", "tel:", "about:", "sms:", "magnet:", "intent:"} {
		if strings.HasPrefix(lower, p) {
			return raw
		}
	}
	if strings.HasPrefix(t, browserPrefix+s.ID+"/") {
		return raw
	}
	ref, err := base.Parse(t)
	if err != nil || (ref.Scheme != "http" && ref.Scheme != "https") {
		return raw
	}
	return s.proxyPath(ref)
}

// ---- entry points ----

// TryRefererRedirect catches a request to this server's own root that a
// proxied page made with a root-relative URL (e.g. fetch("/api/list")) and
// bounces it to the same path on the page's real site. Runs before auth.
func (b *Browser) TryRefererRedirect(w http.ResponseWriter, r *http.Request) bool {
	ref := r.Header.Get("Referer")
	if ref == "" || !strings.Contains(ref, browserPrefix) {
		return false
	}
	sid, pageURL := realFromProxyURL(ref)
	if pageURL == nil {
		return false
	}
	s := b.Session(sid)
	if s == nil {
		return false
	}
	target := &url.URL{Scheme: pageURL.Scheme, Host: pageURL.Host, Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}
	code := http.StatusFound
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		code = http.StatusTemporaryRedirect
	}
	http.Redirect(w, r, s.proxyPath(target), code)
	return true
}

func (b *Browser) ServeProxy(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), browserPrefix)
	sid := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		sid = rest[:i]
	}
	s := b.Session(sid)
	if s == nil {
		writeSimplePage(w, http.StatusGone, "Browser session expired", "This page belonged to a Download Station browser tab that has been closed or has expired. Open the page again from the address bar.", "")
		return
	}
	internal := browserPrefix + sid + "/__nvds/"
	if strings.HasPrefix(r.URL.Path, internal) {
		b.serveInternal(w, r, s, strings.TrimPrefix(r.URL.Path, internal))
		return
	}
	_, target, err := decodeProxyPath(r.URL.EscapedPath(), r.URL.RawQuery)
	if err != nil {
		writeSimplePage(w, http.StatusBadRequest, "Invalid address", err.Error(), "")
		return
	}
	b.proxy(w, r, s, target)
}

func (b *Browser) serveInternal(w http.ResponseWriter, r *http.Request, s *BrowserSession, what string) {
	switch what {
	case "cosmetic":
		// Incremental element-hiding lookups for classes/ids a page adds
		// after load (see inject.js's MutationObserver).
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		host := r.URL.Query().Get("host")
		page := r.URL.Query().Get("page")
		if !b.adblock.Enabled() || b.adblock.SiteAllowed(host) {
			return
		}
		keys := strings.Split(r.URL.Query().Get("keys"), " ")
		if len(keys) > 500 {
			keys = keys[:500]
		}
		io.WriteString(w, CosmeticCSS(b.adblock.Engine().SelectorsForPage(page, host, keys, false)))
	case "proceed":
		// "Proceed anyway" from a strict-blocking page.
		target, err := url.Parse(r.URL.Query().Get("url"))
		if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
			http.Error(w, "bad url", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.allowHosts[strings.ToLower(target.Hostname())] = true
		s.mu.Unlock()
		http.Redirect(w, r, s.proxyPath(target), http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}

var hopHeaders = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true,
	"Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true, "Host": true,
	"Accept-Encoding": true, "Content-Length": true, "Cookie": true, "Referer": true, "Origin": true,
}

// Response headers that would break framing, leak across the shared proxy
// origin, or point the browser somewhere outside the proxy.
var droppedResponseHeaders = []string{
	"Content-Security-Policy", "Content-Security-Policy-Report-Only", "X-Frame-Options",
	"Strict-Transport-Security", "Set-Cookie", "Set-Cookie2", "Cross-Origin-Opener-Policy",
	"Cross-Origin-Embedder-Policy", "Cross-Origin-Resource-Policy", "Clear-Site-Data", "Report-To",
	"Nel", "Alt-Svc", "Link", "Referrer-Policy", "Permissions-Policy", "Content-Length",
	"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Public-Key-Pins", "Expect-Ct",
	"X-Xss-Protection", "Origin-Agent-Cluster", "Service-Worker-Allowed",
}

func isNavigation(dest string) bool {
	return dest == "" || dest == "document" || dest == "iframe" || dest == "frame"
}

func (b *Browser) proxy(w http.ResponseWriter, r *http.Request, s *BrowserSession, target *url.URL) {
	dest := r.Header.Get("Sec-Fetch-Dest")
	host := strings.ToLower(target.Hostname())

	// The first-party page this request belongs to.
	_, refURL := realFromProxyURL(r.Header.Get("Referer"))
	pageHost := host
	if !isNavigation(dest) && refURL != nil {
		pageHost = strings.ToLower(refURL.Hostname())
	} else if (dest == "iframe" || dest == "frame") && refURL != nil {
		pageHost = strings.ToLower(refURL.Hostname())
	}

	adblockOn := b.adblock.Enabled() && !b.adblock.SiteAllowed(pageHost)
	s.mu.Lock()
	allowedOnce := s.allowHosts[host]
	s.mu.Unlock()
	if adblockOn && !allowedOnce {
		t := resTypeFromFetchDest(dest)
		if dest == "" {
			t = typeDocument
		}
		res := b.adblock.Engine().Match(RequestInfo{URL: target.String(), Host: host, PageHost: pageHost, Type: t})
		if !res.Blocked && t == typeSubdocument {
			res = b.adblock.Engine().Match(RequestInfo{URL: target.String(), Host: host, PageHost: pageHost, Type: typeDocument})
		}
		if res.Blocked {
			b.adblock.CountBlocked()
			s.countBlocked(pageHost)
			if isNavigation(dest) {
				writeBlockedPage(w, s, target, res.Filter)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// WebSocket upgrades aren't proxied (Upgrade is a hop-by-hop header the
	// rewriting proxy drops, and a raw tunnel couldn't be ad-filtered or
	// kept inside the address guard as simply). Say so plainly instead of
	// passing on a half-handshake the page would retry forever. Sites that
	// need live sockets (chat, some players) should be opened in a real tab.
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "WebSocket connections are not supported by the Download Station browser", http.StatusNotImplemented)
		return
	}

	var body io.Reader
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		body = io.LimitReader(r.Body, 256<<20)
	}
	ctx := withPrivateHosts(r.Context(), s.typedHostList()...)
	req, err := http.NewRequestWithContext(ctx, r.Method, target.String(), body)
	if err != nil {
		writeSimplePage(w, http.StatusBadRequest, "Invalid address", err.Error(), "")
		return
	}
	for k, vs := range r.Header {
		if hopHeaders[k] || strings.HasPrefix(k, "Sec-") || strings.HasPrefix(k, "X-Forwarded") {
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	if refURL != nil {
		req.Header.Set("Referer", refURL.String())
		if r.Header.Get("Origin") != "" {
			req.Header.Set("Origin", refURL.Scheme+"://"+refURL.Host)
		}
	}
	// Cookies a page set from JS live in the browser, on this proxy's
	// origin - pass them along with the jar's (server-set) ones.
	if c := r.Header.Get("Cookie"); c != "" {
		req.Header.Set("Cookie", c)
	}
	if r.ContentLength > 0 {
		req.ContentLength = r.ContentLength
	}

	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	resp, err := client.Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		msg := err.Error()
		var fe forbiddenError
		if errors.As(err, &fe) && fe.private {
			msg = "This address is on your local network. For your safety, pages can't open local-network addresses on their own - type " + target.Host + " into the address bar to open it."
		} else if errors.Is(err, errForbiddenDestination) {
			msg = "This address points at the NivaroOS server itself (localhost), which the browser isn't allowed to open."
		}
		if isNavigation(dest) {
			writeSimplePage(w, http.StatusBadGateway, "This site can't be reached", msg, target.String())
		} else {
			http.Error(w, msg, http.StatusBadGateway)
		}
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(ct)
	if isNavigation(dest) && r.Method == http.MethodGet && resp.StatusCode == http.StatusOK && looksLikeDownload(resp, mediaType) {
		b.captureDownload(w, s, target, resp, refURL, dest)
		return
	}

	h := w.Header()
	for k, vs := range resp.Header {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	for _, k := range droppedResponseHeaders {
		h.Del(k)
	}
	h.Set("Referrer-Policy", "same-origin")
	if loc := resp.Header.Get("Location"); loc != "" {
		h.Set("Location", s.rewriteURL(resp.Request.URL, loc))
	}
	if ref := resp.Header.Get("Refresh"); ref != "" {
		h.Set("Refresh", rewriteRefresh(ref, func(u string) string { return s.rewriteURL(resp.Request.URL, u) }))
	}

	switch {
	case mediaType == "text/html" || mediaType == "application/xhtml+xml":
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 24<<20))
		if err != nil && len(raw) == 0 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		// Drive's "can't scan this file for viruses - Download anyway?" page
		// is just a speed bump in front of the file: capture straight away,
		// as if the user had already clicked through it.
		if isNavigation(dest) && isGoogleDriveHost(resp.Request.URL.Hostname()) {
			if fileURL := parseDownloadForm(raw, resp.Request.URL); fileURL != nil {
				referer := ""
				if refURL != nil {
					referer = refURL.String()
				}
				if c, err := s.CaptureURL(fileURL.String(), referer); err == nil {
					if m := driveNameRe.FindSubmatch(raw); m != nil {
						if name := sanitizeFilename(html.UnescapeString(string(m[1]))); name != "" {
							c.Filename = name
						}
					}
					for _, k := range droppedResponseHeaders {
						h.Del(k)
					}
					h.Del("Content-Type")
					writeCaptured(w, s, c)
					return
				}
			}
		}
		out := b.rewriteHTML(s, resp.Request.URL, raw, isNavigation(dest), adblockOn)
		h.Set("Content-Length", strconv.Itoa(len(out)))
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(resp.StatusCode)
		w.Write(out)
	case mediaType == "text/css":
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		out := rewriteCSS(string(raw), func(u string) string { return s.rewriteURL(resp.Request.URL, u) })
		h.Set("Content-Length", strconv.Itoa(len(out)))
		w.WriteHeader(resp.StatusCode)
		io.WriteString(w, out)
	default:
		if resp.ContentLength >= 0 && !resp.Uncompressed {
			h.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

var inlineTypes = []string{"text/", "image/", "application/json", "application/javascript", "application/x-javascript",
	"application/ecmascript", "application/xml", "application/xhtml+xml", "application/pdf", "application/rss+xml",
	"application/atom+xml", "application/manifest+json", "application/ld+json", "multipart/"}

// looksLikeDownload mirrors what makes IDM grab a click: an explicit
// attachment, or a response type a browser wouldn't display inline anyway
// (archives, installers, disk images, media files...).
func looksLikeDownload(resp *http.Response, mediaType string) bool {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if d, _, err := mime.ParseMediaType(cd); err == nil && d == "attachment" {
			return true
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(cd)), "attachment") {
			return true
		}
	}
	if mediaType == "" {
		return false
	}
	for _, p := range inlineTypes {
		if strings.HasPrefix(mediaType, p) {
			return false
		}
	}
	return true
}

// attachCookies copies the jar's cookies for u onto a capture, so the
// download engine's requests are signed in exactly like the page was.
func (s *BrowserSession) attachCookies(c *Capture, u *url.URL) {
	s.mu.Lock()
	jar := s.jar
	s.mu.Unlock()
	var parts []string
	for _, ck := range jar.Cookies(u) {
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	if len(parts) > 0 {
		c.headers["Cookie"] = strings.Join(parts, "; ")
		c.HasCookies = true
	}
}

func (s *BrowserSession) storeCapture(c *Capture) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captures[c.ID] = c
	// Captures are only needed until the Add window reads them.
	if len(s.captures) > 100 {
		var oldest *Capture
		for _, x := range s.captures {
			if oldest == nil || x.CreatedAt.Before(oldest.CreatedAt) {
				oldest = x
			}
		}
		delete(s.captures, oldest.ID)
	}
}

// CaptureURL records an explicit "download this address" from the
// browser toolbar - same cookies/referer treatment as a clicked link.
func (s *BrowserSession) CaptureURL(raw, referer string) (*Capture, error) {
	u, err := validateDownloadURL(raw)
	if err != nil {
		return nil, err
	}
	c := &Capture{ID: newID(), URL: u.String(), Filename: filenameFromURL(u), Size: -1, CreatedAt: time.Now(), headers: map[string]string{}}
	if referer != "" {
		c.Referer = referer
		c.headers["Referer"] = referer
	}
	s.attachCookies(c, u)
	s.storeCapture(c)
	return c, nil
}

func (b *Browser) captureDownload(w http.ResponseWriter, s *BrowserSession, target *url.URL, resp *http.Response, refURL *url.URL, dest string) {
	finalURL := resp.Request.URL
	size, resumable := resp.ContentLength, strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes")
	c := &Capture{
		ID:          newID(),
		URL:         finalURL.String(),
		Filename:    filenameFromResponse(resp),
		Size:        size,
		Resumable:   resumable,
		ContentType: resp.Header.Get("Content-Type"),
		CreatedAt:   time.Now(),
		headers:     map[string]string{},
	}
	if refURL != nil {
		c.Referer = refURL.String()
		c.headers["Referer"] = c.Referer
	}
	s.attachCookies(c, finalURL)
	s.storeCapture(c)
	writeCaptured(w, s, c)
}

// writeCaptured answers a captured navigation with the small "Sent to
// Download Station" page that notifies the app and steps back.
func writeCaptured(w http.ResponseWriter, s *BrowserSession, c *Capture) {
	msg, _ := json.Marshal(map[string]interface{}{"nvds": 1, "type": "download", "session": s.ID, "capture": c.ID, "filename": c.Filename})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	capturedTmpl.Execute(w, map[string]interface{}{
		"Msg":      template.JS(msg),
		"Filename": c.Filename,
		"Size":     humanSize(c.Size),
	})
}

// The file name Drive prints on its warning page ("<name> (476M) is too
// large for Google to scan...").
var driveNameRe = regexp.MustCompile(`class="uc-name-size"><a[^>]*>([^<]+)</a>`)

// The sidecar's own pages follow the viewer's light/dark preference (the
// Download Station iframe passes the NivaroOS theme down as color-scheme).
// Colours match the NivaroOS tokens; every text colour is >= 4.5:1 on its
// background in both schemes.
const pageThemeCSS = `:root{color-scheme:light dark;--bg:#f8fafc;--fg:#0f172a;--muted:#475569;--chip:#e2e8f0;--accent:#1d4ed8;--accent-soft:#dbeafe;--btn:#2563eb;--btn-fg:#fff}
@media (prefers-color-scheme:dark){:root{--bg:#0a0a0a;--fg:#f4f4f5;--muted:#a1a1aa;--chip:#27272a;--accent:#93c5fd;--accent-soft:#1e3a8a;--btn:#2563eb;--btn-fg:#fff}}`

var capturedTmpl = template.Must(template.New("c").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="color-scheme" content="light dark"><title>Download captured</title>
<style>` + pageThemeCSS + `
html,body{margin:0;height:100%;font-family:system-ui,sans-serif;background:var(--bg);color:var(--fg)}
.w{height:100%;display:flex;align-items:center;justify-content:center}.c{text-align:center;max-width:28rem;padding:2rem}
.i{width:3rem;height:3rem;border-radius:50%;background:var(--accent-soft);color:var(--accent);display:inline-flex;align-items:center;justify-content:center;font-size:1.5rem}
h1{font-size:1rem;margin:1rem 0 .25rem}p{margin:0;color:var(--muted);font-size:.85rem;word-break:break-all}</style></head>
<body><div class="w"><div class="c"><div class="i">&#8595;</div><h1>Sent to Download Station</h1><p>{{.Filename}}{{if .Size}} &middot; {{.Size}}{{end}}</p></div></div>
<script>(function(){var m={{.Msg}};try{window.top.postMessage(m,'*')}catch(e){}
// Stay on the page the link was on, like a real browser does for a download.
if(window.parent===window.top&&history.length>1){setTimeout(function(){history.back()},600)}})();</script></body></html>`))

func humanSize(n int64) string {
	if n < 0 {
		return ""
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

var refreshURLRe = regexp.MustCompile(`(?i)(url\s*=\s*['"]?)([^'"]+)(['"]?)`)

func rewriteRefresh(v string, rw func(string) string) string {
	return refreshURLRe.ReplaceAllStringFunc(v, func(m string) string {
		sm := refreshURLRe.FindStringSubmatch(m)
		return sm[1] + rw(strings.TrimSpace(sm[2])) + sm[3]
	})
}

var (
	cssURLRe    = regexp.MustCompile(`(?i)url\(\s*(?:"([^"]*)"|'([^']*)'|([^)'"\s]*))\s*\)`)
	cssImportRe = regexp.MustCompile(`(?i)@import\s+(?:"([^"]*)"|'([^']*)')`)
)

func rewriteCSS(css string, rw func(string) string) string {
	css = cssURLRe.ReplaceAllStringFunc(css, func(m string) string {
		sm := cssURLRe.FindStringSubmatch(m)
		u := sm[1] + sm[2] + sm[3]
		if u == "" || strings.HasPrefix(strings.ToLower(u), "data:") {
			return m
		}
		return `url("` + strings.ReplaceAll(rw(u), `"`, `%22`) + `")`
	})
	return cssImportRe.ReplaceAllStringFunc(css, func(m string) string {
		sm := cssImportRe.FindStringSubmatch(m)
		return `@import "` + strings.ReplaceAll(rw(sm[1]+sm[2]), `"`, `%22`) + `"`
	})
}

var urlAttrs = map[string]bool{"href": true, "src": true, "action": true, "formaction": true, "poster": true, "background": true, "data": true, "cite": true, "manifest": true, "longdesc": true}

func rewriteSrcset(v string, rw func(string) string) string {
	parts := strings.Split(v, ",")
	for i, p := range parts {
		f := strings.Fields(strings.TrimSpace(p))
		if len(f) == 0 {
			continue
		}
		f[0] = rw(f[0])
		parts[i] = strings.Join(f, " ")
	}
	return strings.Join(parts, ", ")
}

// rewriteHTML rewrites every URL-bearing attribute and inline style, drops
// meta CSP/referrer tags and SRI hashes (which a rewritten stylesheet would
// fail), neutralises elements whose src the blocker rejects, and injects
// the runtime shim plus element-hiding CSS at the top of <head>.
func (b *Browser) rewriteHTML(s *BrowserSession, pageURL *url.URL, raw []byte, inject, adblockOn bool) []byte {
	base := pageURL
	rw := func(u string) string { return s.rewriteURL(base, u) }
	engine := b.adblock.Engine()
	pageHost := strings.ToLower(pageURL.Hostname())
	blocked := func(u string, t resType) bool {
		if !adblockOn || u == "" {
			return false
		}
		ref, err := base.Parse(strings.TrimSpace(u))
		if err != nil || (ref.Scheme != "http" && ref.Scheme != "https") {
			return false
		}
		if engine.Match(RequestInfo{URL: ref.String(), Host: ref.Hostname(), PageHost: pageHost, Type: t}).Blocked {
			b.adblock.CountBlocked()
			s.countBlocked(pageHost)
			return true
		}
		return false
	}

	var out bytes.Buffer
	out.Grow(len(raw) + 4096)
	keys := map[string]bool{}
	injectAt := -1
	z := html.NewTokenizer(bytes.NewReader(raw))
	inStyle := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.TextToken:
			if inStyle {
				out.WriteString(rewriteCSS(string(z.Raw()), rw))
			} else {
				out.Write(z.Raw())
			}
			continue
		case html.EndTagToken:
			name, _ := z.TagName()
			if atom.Lookup(name) == atom.Style {
				inStyle = false
			}
			out.Write(z.Raw())
			continue
		case html.StartTagToken, html.SelfClosingTagToken:
		default:
			out.Write(z.Raw())
			continue
		}

		rawTag := append([]byte(nil), z.Raw()...)
		tok := z.Token()
		changed := false
		drop := false
		switch tok.DataAtom {
		case atom.Style:
			inStyle = tt == html.StartTagToken
		case atom.Base:
			for _, a := range tok.Attr {
				if a.Key == "href" {
					if nb, err := pageURL.Parse(strings.TrimSpace(a.Val)); err == nil {
						base = nb
					}
				}
			}
		case atom.Meta:
			var equiv, name string
			for _, a := range tok.Attr {
				switch a.Key {
				case "http-equiv":
					equiv = strings.ToLower(a.Val)
				case "name":
					name = strings.ToLower(a.Val)
				}
			}
			if equiv == "content-security-policy" || equiv == "x-frame-options" || name == "referrer" {
				drop = true
			}
			if equiv == "refresh" {
				for i, a := range tok.Attr {
					if a.Key == "content" {
						tok.Attr[i].Val = rewriteRefresh(a.Val, rw)
						changed = true
					}
				}
			}
		}
		if drop {
			changed = true
			continue
		}

		var resourceType resType
		switch tok.DataAtom {
		case atom.Script:
			resourceType = typeScript
		case atom.Img, atom.Picture:
			resourceType = typeImage
		case atom.Iframe, atom.Frame:
			resourceType = typeSubdocument
		case atom.Video, atom.Audio, atom.Source, atom.Track:
			resourceType = typeMedia
		case atom.Embed, atom.Object:
			resourceType = typeObject
		}

		attrs := tok.Attr[:0]
		for _, a := range tok.Attr {
			key := strings.ToLower(a.Key)
			switch {
			case key == "class":
				for _, c := range strings.Fields(a.Val) {
					keys["."+c] = true
				}
			case key == "id" && a.Val != "":
				keys["#"+strings.TrimSpace(a.Val)] = true
			case key == "integrity" || key == "nonce":
				changed = true
				continue
			case key == "target" && (tok.DataAtom == atom.A || tok.DataAtom == atom.Form || tok.DataAtom == atom.Area):
				v := strings.ToLower(a.Val)
				if v == "_blank" || v == "_top" || v == "_parent" || v == "_new" {
					a.Val = "_self"
					changed = true
				}
			case key == "style":
				if strings.Contains(strings.ToLower(a.Val), "url(") {
					a.Val = rewriteCSS(a.Val, rw)
					changed = true
				}
			case key == "srcset" || key == "imagesrcset":
				a.Val = rewriteSrcset(a.Val, rw)
				changed = true
			case urlAttrs[key]:
				if key == "src" && resourceType != 0 && blocked(a.Val, resourceType) {
					changed = true
					if tok.DataAtom == atom.Iframe || tok.DataAtom == atom.Frame {
						a.Val = "about:blank"
					} else {
						continue
					}
				} else if key == "href" && tok.DataAtom == atom.Link && blocked(a.Val, typeStylesheet) {
					changed = true
					continue
				} else {
					nv := rw(a.Val)
					if nv != a.Val {
						a.Val = nv
						changed = true
					}
				}
			}
			attrs = append(attrs, a)
		}
		tok.Attr = attrs

		before := out.Len()
		if changed {
			out.WriteString(tok.String())
		} else {
			out.Write(rawTag)
		}
		// Inject right after <head>, or - for markup with no <head> - right
		// before the first real element.
		if injectAt < 0 {
			if tok.DataAtom == atom.Head {
				injectAt = out.Len()
			} else if tok.DataAtom != atom.Html {
				injectAt = before
			}
		}
	}
	if !inject {
		return out.Bytes()
	}
	if injectAt < 0 {
		injectAt = 0
	}

	cfg := map[string]interface{}{
		"sid":     s.ID,
		"prefix":  browserPrefix + s.ID + "/",
		"url":     pageURL.String(),
		"adblock": adblockOn,
		"nav":     s.recordNav(pageURL),
	}
	// json.Marshal escapes <, > and & (\u003c...), so a page URL can't
	// close the <script> this is written into.
	cfgJSON, _ := json.Marshal(cfg)
	var head strings.Builder
	head.WriteString("<script data-nvds>")
	head.WriteString(strings.Replace(injectJS, "__NVDS_CONFIG__", string(cfgJSON), 1))
	head.WriteString("</script>")
	if adblockOn {
		keyList := make([]string, 0, len(keys))
		for k := range keys {
			keyList = append(keyList, k)
		}
		css := CosmeticCSS(engine.SelectorsForPage(pageURL.String(), pageHost, keyList, true))
		if css != "" {
			head.WriteString("<style data-nvds>")
			head.WriteString(strings.ReplaceAll(css, "</", "<\\/"))
			head.WriteString("</style>")
		}
	}
	res := out.Bytes()
	final := make([]byte, 0, len(res)+head.Len())
	final = append(final, res[:injectAt]...)
	final = append(final, head.String()...)
	final = append(final, res[injectAt:]...)
	return final
}

var simpleTmpl = template.Must(template.New("s").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="color-scheme" content="light dark"><title>{{.Title}}</title>
<style>` + pageThemeCSS + `
html,body{margin:0;height:100%;font-family:system-ui,sans-serif;background:var(--bg);color:var(--fg)}
.w{min-height:100%;display:flex;align-items:center;justify-content:center}.c{max-width:34rem;padding:2rem}
h1{font-size:1.15rem;margin:0 0 .5rem}p{margin:0 0 .75rem;color:var(--muted);font-size:.875rem;line-height:1.5;word-break:break-word}
code{font-size:.8rem;background:var(--chip);color:var(--fg);padding:.1rem .35rem;border-radius:4px;word-break:break-all}
.b{display:flex;gap:.5rem;margin-top:1.25rem}a.btn{font-size:.85rem;text-decoration:none;padding:.45rem .9rem;border-radius:8px;background:var(--chip);color:var(--fg)}
a.btn:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
a.btn.p{background:var(--btn);color:var(--btn-fg)}.hide{display:none}</style></head>
<body{{if .Nested}} class="hide"{{end}}><div class="w"><div class="c"><h1>{{.Title}}</h1><p>{{.Message}}</p>
{{if .Detail}}<p><code>{{.Detail}}</code></p>{{end}}
<div class="b">{{if .Proceed}}<a class="btn" href="javascript:history.back()">Go back</a><a class="btn p" href="{{.Proceed}}">Proceed anyway</a>{{end}}</div></div></div>
{{if .Nested}}<script>if(window.parent===window.top){document.body.className=''}</script>{{end}}</body></html>`))

func writeSimplePage(w http.ResponseWriter, code int, title, message, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := simpleTmpl.Execute(w, map[string]interface{}{"Title": title, "Message": message, "Detail": detail}); err != nil {
		log.Printf("render page: %v", err)
	}
}

// writeBlockedPage is uBO's strict-blocking warning. Rendered hidden when
// it lands in a nested ad iframe (nobody needs a warning inside an ad
// slot); shown - with Proceed - when it's the page the user navigated to.
func writeBlockedPage(w http.ResponseWriter, s *BrowserSession, target *url.URL, filter string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	proceed := browserPrefix + s.ID + "/__nvds/proceed?url=" + url.QueryEscape(target.String())
	simpleTmpl.Execute(w, map[string]interface{}{
		"Title":   "Page blocked by the ad blocker",
		"Message": "The ad blocker prevented " + target.Hostname() + " from loading, because it matches this filter:",
		"Detail":  filter,
		"Proceed": template.URL(proceed),
		"Nested":  true,
	})
}
