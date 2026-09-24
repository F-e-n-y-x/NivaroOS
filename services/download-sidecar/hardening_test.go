package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---- auth / CORS / content type ----

func TestLoopbackBrowserRequestNeedsToken(t *testing.T) {
	h := requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), t.TempDir())

	// curl / another local service: no Origin, no Sec-Fetch-Site.
	req := httptest.NewRequest("GET", "/downloads", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("loopback automation should pass, got %d", rec.Code)
	}

	// Relayed by the gateway (also from loopback): token required.
	req = httptest.NewRequest("GET", "/downloads", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "192.168.1.20")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("gateway-relayed request skipped auth: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/v1/download-station/downloads", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	stripGatewayPrefix(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("gateway-path request skipped auth: %d", rec.Code)
	}

	// A browser tab on the NAS itself (also loopback) must authenticate.
	for _, hdr := range [][2]string{{"Origin", "http://evil.example"}, {"Sec-Fetch-Site", "cross-site"}} {
		req = httptest.NewRequest("POST", "/downloads", strings.NewReader(`{}`))
		req.RemoteAddr = "127.0.0.1:5555"
		req.Header.Set(hdr[0], hdr[1])
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("loopback browser request with %s skipped auth: %d", hdr[0], rec.Code)
		}
	}
}

func fakeToken(claims map[string]interface{}) string {
	raw, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(raw) + ".sig"
}

func TestTokenRole(t *testing.T) {
	cases := []struct {
		claims map[string]interface{}
		ok     bool
	}{
		{map[string]interface{}{"username": "a", "id": 1}, true}, // today's tokens: no role
		{map[string]interface{}{"role": "admin"}, true},
		{map[string]interface{}{"role": "user"}, false},
		{map[string]interface{}{"roles": []interface{}{"user", "admin"}}, true},
		{map[string]interface{}{"roles": []interface{}{"guest"}}, false},
		{map[string]interface{}{"is_admin": false}, false},
	}
	for _, c := range cases {
		if got := tokenRoleAllowed(fakeToken(c.claims)); got != c.ok {
			t.Errorf("%v: got %v want %v", c.claims, got, c.ok)
		}
	}
}

func TestCORSOnlySameHost(t *testing.T) {
	h := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("OPTIONS", "http://nas.local:28642/downloads", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("foreign origin got a CORS grant")
	}
	req = httptest.NewRequest("GET", "http://nas.local:28642/downloads", nil)
	req.Header.Set("Origin", "http://nas.local")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://nas.local" || !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Fatalf("same-host origin not granted: %v", rec.Header())
	}
}

func TestJSONEndpointsRejectSimpleRequests(t *testing.T) {
	m, _ := testManager(t)
	dir := t.TempDir()
	st := NewSettingsStore(dir, dir)
	mux := http.NewServeMux()
	RegisterRoutes(mux, m, st, NewAdblocker(dir, st, newTransport(true)), NewBrowser(newTransport(true), NewAdblocker(dir, st, newTransport(true)), st), NewHistory(dir))
	req := httptest.NewRequest("POST", "/downloads", strings.NewReader(`{"url":"https://example.com/x","dir":"/etc/cron.d"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "application/json") {
		t.Fatalf("text/plain body accepted: %d %s", rec.Code, rec.Body.String())
	}
	// Even as JSON, /etc is outside the storage roots.
	req = httptest.NewRequest("POST", "/downloads", strings.NewReader(`{"url":"https://example.com/x","dir":"/etc/cron.d","start":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "save folder") {
		t.Fatalf("/etc/cron.d accepted: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("PUT", "/settings", strings.NewReader(`{"default_dir":"/root/.ssh"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("default_dir outside roots accepted: %d", rec.Code)
	}
}

func TestGatewayPrefixNeverServesBrowser(t *testing.T) {
	var got string
	h := stripGatewayPrefix(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.URL.Path }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/download-station/downloads", nil))
	if got != "/downloads" {
		t.Fatalf("prefix not stripped: %q", got)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/download-station/b/sid/https/example.com/", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("proxy served through the gateway path: %d", rec.Code)
	}
}

// ---- storage roots ----

func TestPathPolicy(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"/etc/cron.d", "/root/.ssh", "/", "/usr/lib/x", "relative/dir"} {
		if _, err := pathPolicy.Check(p); err == nil {
			t.Errorf("%s allowed", p)
		}
	}
	if _, err := pathPolicy.Check(filepath.Join(root, "new", "sub")); err != nil {
		t.Errorf("non-existent folder under a root refused: %v", err)
	}
	// A symlink inside a root that points out of it is refused.
	link := filepath.Join(root, "escape")
	if err := os.Symlink("/etc", link); err != nil {
		t.Skip(err)
	}
	if _, err := pathPolicy.Check(filepath.Join(link, "cron.d")); err == nil {
		t.Error("symlink escape allowed")
	}
	if pathPolicy.Allowed("/etc/passwd") {
		t.Error("delete outside roots allowed")
	}
}

// ---- outbound address guard ----

func TestPrivateAddressesNeedExplicitHost(t *testing.T) {
	for _, s := range []string{"10.1.2.3", "172.17.0.1", "192.168.1.1", "100.100.1.1", "fd00::1"} {
		ip := net.ParseIP(s)
		if isForbiddenIP(ip, false) == nil {
			t.Errorf("%s reachable without the user choosing it", s)
		}
		if isForbiddenIP(ip, true) != nil {
			t.Errorf("%s refused although the user typed it", s)
		}
	}
	for _, s := range []string{"127.0.0.1", "::1", "169.254.169.254", "0.0.0.0", "0.1.2.3"} {
		if isForbiddenIP(net.ParseIP(s), true) == nil {
			t.Errorf("%s reachable", s)
		}
	}
	if isForbiddenIP(net.ParseIP("93.184.216.34"), false) != nil {
		t.Error("public address refused")
	}
	ctx := withPrivateHosts(context.Background(), "NAS.local.")
	if !privateAllowed(ctx, "nas.local") || privateAllowed(ctx, "router.local") {
		t.Error("typed-host set wrong")
	}
}

// With an outbound proxy configured, the dialer only sees the proxy - the
// real target must still be checked.
func TestProxyEnvChecksRealTarget(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		w.WriteHeader(200)
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	proxyFn := func(*http.Request) (*url.URL, error) { return pu, nil }
	addrs := envProxyAddrs(proxyFn)
	strict, lan := baseTransport(), baseTransport()
	strict.Proxy, lan.Proxy = proxyFn, proxyFn
	strict.DialContext, lan.DialContext = guardedDialer(false, addrs), guardedDialer(true, addrs)
	g := &guardedTransport{strict: strict, lan: lan, proxy: proxyFn, proxyAddrs: addrs, resolver: net.DefaultResolver}
	c := &http.Client{Transport: g}

	for _, target := range []string{"http://127.0.0.1:28641/vms", "http://192.168.1.1/", "http://localhost/"} {
		resp, err := c.Get(target)
		if err == nil {
			resp.Body.Close()
			t.Errorf("%s reached through the proxy", target)
		} else if !errors.Is(err, errForbiddenDestination) {
			t.Errorf("%s: unexpected error %v", target, err)
		}
	}
	// The proxy's own address (loopback here) is reachable as the proxy.
	req, _ := http.NewRequestWithContext(withPrivateHosts(context.Background(), "192.168.1.1"), "GET", "http://192.168.1.1/", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("typed LAN host via proxy refused: %v", err)
	}
	resp.Body.Close()
	if proxied.Load() != 1 {
		t.Fatalf("proxy hits = %d", proxied.Load())
	}
	// ...but not as a direct target.
	if resp, err := c.Get(proxy.URL); err == nil {
		resp.Body.Close()
		t.Error("proxy address reachable as a target")
	}
}

// ---- browser ----

func TestCosmeticSelectorsCannotBreakOutOfStyle(t *testing.T) {
	css := CosmeticCSS([]string{".ad", `</style><script>alert(1)</script>`, "a{color:red}", ".x;y", "div > .banner"})
	if strings.Contains(css, "<") || strings.Contains(css, "alert") || strings.Contains(css, "color:red") {
		t.Fatalf("unsafe selector written: %q", css)
	}
	if !strings.Contains(css, ".ad{") || !strings.Contains(css, "div > .banner{") {
		t.Fatalf("safe selectors dropped: %q", css)
	}
}

func TestCaptureCookiesOnlyForTheirHost(t *testing.T) {
	_, s, _ := testBrowser(t, "")
	c := &Capture{URL: "https://files.example.com/a.zip", Referer: "https://example.com/", headers: map[string]string{"Cookie": "sid=secret", "Referer": "https://example.com/"}}
	if h := s.HeadersFor(c, "https://files.example.com/a.zip"); h["Cookie"] != "sid=secret" {
		t.Fatalf("same host lost cookies: %v", h)
	}
	h := s.HeadersFor(c, "https://attacker.example.net/steal")
	if h["Cookie"] != "" {
		t.Fatalf("captured cookie sent to another host: %v", h)
	}
	if h["Referer"] != "https://example.com/" {
		t.Fatalf("referer dropped: %v", h)
	}
}

func TestNavRecordsWhatWasServed(t *testing.T) {
	_, s, _ := testBrowser(t, "")
	u, _ := url.Parse("https://example.com/page")
	id := s.recordNav(u)
	got, ok := s.Nav(id)
	if !ok || got != "https://example.com/page" {
		t.Fatalf("nav %q %v", got, ok)
	}
	if _, ok := s.Nav("forged"); ok {
		t.Fatal("unknown nav id resolved")
	}
}

func TestHistoryTitleTruncationIsRuneSafe(t *testing.T) {
	title := strings.Repeat("日本", 400)
	got := truncateRunes(title, 300)
	if len([]rune(got)) != 300 || !strings.HasPrefix(title, got) {
		t.Fatalf("bad truncation: %d runes", len([]rune(got)))
	}
	h := NewHistory(t.TempDir())
	e, _ := h.Add("https://example.com/", title)
	if !strings.HasPrefix(title, e.Title) || len([]rune(e.Title)) != 300 {
		t.Fatal("history title cut mid-rune")
	}
}

func TestEventEpoch(t *testing.T) {
	a, b := newEventLog(10), newEventLog(10)
	if a.Epoch() == "" || a.Epoch() == b.Epoch() {
		t.Fatal("epochs must differ per process")
	}
}

// ---- engine state machine and integrity ----

func TestPauseThenImmediateResumeRuns(t *testing.T) {
	m, dlDir := testManager(t)
	content := randomBytes(4 << 20)
	srv, _ := rangeServer(content, "pr.bin", 2*time.Millisecond)
	defer srv.Close()
	v, _ := m.Add(AddRequest{URL: srv.URL + "/pr.bin", Connections: 4})
	waitState(t, m, v.ID, StateDownloading, 5*time.Second)
	time.Sleep(50 * time.Millisecond)
	// Resume lands while the paused run is still winding down.
	_ = m.Pause(v.ID)
	_ = m.Resume(v.ID)
	waitState(t, m, v.ID, StateCompleted, 30*time.Second)
	got, _ := os.ReadFile(filepath.Join(dlDir, "pr.bin"))
	if !bytes.Equal(got, content) {
		t.Fatal("content differs")
	}
}

// A resumed download whose server now serves a different file (size or
// ETag changed) restarts from zero instead of mixing the two.
func TestResumeDetectsChangedFile(t *testing.T) {
	m, dlDir := testManager(t)
	oldContent := randomBytes(3 << 20)
	newContent := randomBytes(3<<20 + 777)
	var version atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, etag := oldContent, `"v1"`
		if version.Load() > 0 {
			content, etag = newContent, `"v2"`
		}
		w.Header().Set("ETag", etag)
		time.Sleep(time.Millisecond)
		http.ServeContent(w, r, "c.bin", time.Time{}, &slowReader{ReadSeeker: bytes.NewReader(content), delay: 2 * time.Millisecond})
	}))
	defer srv.Close()
	v, _ := m.Add(AddRequest{URL: srv.URL + "/c.bin", Connections: 2})
	waitState(t, m, v.ID, StateDownloading, 5*time.Second)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cur, _ := m.Get(v.ID); cur.Downloaded > 128<<10 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = m.Pause(v.ID)
	waitState(t, m, v.ID, StatePaused, 5*time.Second)
	version.Store(1)
	_ = m.Resume(v.ID)
	done := waitState(t, m, v.ID, StateCompleted, 30*time.Second)
	got, _ := os.ReadFile(filepath.Join(dlDir, done.Filename))
	if !bytes.Equal(got, newContent) {
		t.Fatalf("mixed/old content after the file changed (len %d)", len(got))
	}
}

func TestChecksumVerified(t *testing.T) {
	m, dlDir := testManager(t)
	content := randomBytes(1 << 20)
	srv, _ := rangeServer(content, "sum.bin", 0)
	defer srv.Close()
	sum := sha256.Sum256(content)
	good := hex.EncodeToString(sum[:])

	v, err := m.Add(AddRequest{URL: srv.URL + "/sum.bin", Checksum: "sha256:" + strings.ToUpper(good)})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, v.ID, StateCompleted, 10*time.Second)

	bad := strings.Repeat("0", 64)
	v2, _ := m.Add(AddRequest{URL: srv.URL + "/sum.bin", Checksum: bad})
	f := waitState(t, m, v2.ID, StateFailed, 10*time.Second)
	if !strings.Contains(f.Error, "checksum mismatch") {
		t.Fatalf("error %q", f.Error)
	}
	if _, err := os.Stat(filepath.Join(dlDir, f.Filename)); !os.IsNotExist(err) {
		t.Fatal("file with wrong checksum was kept")
	}
	if _, err := m.Add(AddRequest{URL: srv.URL + "/x", Checksum: "nothex"}); err == nil {
		t.Fatal("malformed checksum accepted")
	}
}

func TestRedownloadReplacesFile(t *testing.T) {
	m, dlDir := testManager(t)
	var gen atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := []byte("version " + strconv.Itoa(int(gen.Load())))
		w.Header().Set("Content-Disposition", `attachment; filename="r.txt"`)
		http.ServeContent(w, r, "r.txt", time.Time{}, bytes.NewReader(body))
	}))
	defer srv.Close()
	v, _ := m.Add(AddRequest{URL: srv.URL + "/r"})
	waitState(t, m, v.ID, StateCompleted, 10*time.Second)
	gen.Store(1)
	if err := m.Redownload(v.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	done := waitState(t, m, v.ID, StateCompleted, 10*time.Second)
	if done.Filename != "r.txt" {
		t.Fatalf("redownload saved as %q", done.Filename)
	}
	got, _ := os.ReadFile(filepath.Join(dlDir, "r.txt"))
	if string(got) != "version 1" {
		t.Fatalf("file not replaced: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dlDir, "r (1).txt")); !os.IsNotExist(err) {
		t.Fatal("a duplicate copy was saved")
	}
}

func TestRedownloadWhileRunning(t *testing.T) {
	m, dlDir := testManager(t)
	content := randomBytes(3 << 20)
	srv, _ := rangeServer(content, "rr.bin", 2*time.Millisecond)
	defer srv.Close()
	v, _ := m.Add(AddRequest{URL: srv.URL + "/rr.bin", Connections: 2})
	waitState(t, m, v.ID, StateDownloading, 5*time.Second)
	if err := m.Redownload(v.ID); err != nil {
		t.Fatalf("redownload of a running download: %v", err)
	}
	waitState(t, m, v.ID, StateCompleted, 30*time.Second)
	got, _ := os.ReadFile(filepath.Join(dlDir, "rr.bin"))
	if !bytes.Equal(got, content) {
		t.Fatal("content differs")
	}
}

func TestDeleteWithFileOnlyInsideRoots(t *testing.T) {
	m, dlDir := testManager(t)
	srv, _ := rangeServer([]byte("hello"), "d.txt", 0)
	defer srv.Close()
	v, _ := m.Add(AddRequest{URL: srv.URL + "/d"})
	waitState(t, m, v.ID, StateCompleted, 10*time.Second)
	if err := m.Delete(v.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dlDir, "d.txt")); !os.IsNotExist(err) {
		t.Fatal("file not deleted")
	}
}
