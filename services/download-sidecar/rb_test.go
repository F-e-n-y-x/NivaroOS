package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---- pure helpers ----

func TestFrameHeaderRoundTrip(t *testing.T) {
	b := encodeFrameHeader(rbFrameJPEG, 7, 123456, 2560, 1600, 60, 1280, 800)
	if len(b) != rbFrameHeader {
		t.Fatalf("header is %d bytes", len(b))
	}
	f, ok := decodeFrameHeader(append(b, 0xFF, 0xD8))
	if !ok || f.Kind != rbFrameJPEG || f.Tab != 7 || f.Frame != 123456 || f.W != 2560 || f.H != 1600 || f.Quality != 60 || f.CSSW != 1280 || f.CSSH != 800 {
		t.Fatalf("decoded %+v", f)
	}
	if _, ok := decodeFrameHeader(b[:5]); ok {
		t.Fatal("short header accepted")
	}
}

func TestQualityStepsDownOnBacklogAndBackUp(t *testing.T) {
	var q qualityCtl
	now := time.Unix(1000, 0)
	if q.Quality() != 80 || q.Slow() {
		t.Fatal("starts sharp")
	}
	if !q.OnBacklog(now) || q.Quality() != 60 {
		t.Fatalf("first backlog -> 60, got %d", q.Quality())
	}
	// At most one step a second.
	if q.OnBacklog(now.Add(200*time.Millisecond)) || q.Quality() != 60 {
		t.Fatal("stepped twice within a second")
	}
	q.OnBacklog(now.Add(1500 * time.Millisecond))
	q.OnBacklog(now.Add(3 * time.Second))
	if q.Quality() != 45 {
		t.Fatalf("floor is 45, got %d", q.Quality())
	}
	if q.OnTick(now.Add(4 * time.Second)) {
		t.Fatal("recovered while still backlogged")
	}
	if !q.OnTick(now.Add(9*time.Second)) || q.Quality() != 60 {
		t.Fatalf("5 s without backlog should step up, got %d", q.Quality())
	}
	q.OnTick(now.Add(15 * time.Second))
	if q.Quality() != 80 || q.Slow() {
		t.Fatalf("back to sharp, got %d", q.Quality())
	}
}

func TestKeyEventParams(t *testing.T) {
	p := keyEventParams(rbKey{Type: "down", Key: "a", Code: "KeyA", KeyCode: 65, Text: "a"})
	if p["type"] != "keyDown" || p["text"] != "a" {
		t.Fatalf("typing a: %v", p)
	}
	p = keyEventParams(rbKey{Type: "down", Key: "a", Code: "KeyA", KeyCode: 65, Text: "a", Mods: modCtrl})
	if p["type"] != "rawKeyDown" || p["text"] != nil || p["modifiers"] != modCtrl {
		t.Fatalf("Ctrl+A must not type: %v", p)
	}
	p = keyEventParams(rbKey{Type: "down", Key: "Enter", Code: "Enter", KeyCode: 13})
	if p["type"] != "keyDown" || p["text"] != "\r" {
		t.Fatalf("Enter submits: %v", p)
	}
	p = keyEventParams(rbKey{Type: "down", Key: "ArrowLeft", Code: "ArrowLeft", KeyCode: 37})
	if p["type"] != "rawKeyDown" {
		t.Fatalf("arrow: %v", p)
	}
	p = keyEventParams(rbKey{Type: "up", Key: "a", KeyCode: 65, Text: "a"})
	if p["type"] != "keyUp" {
		t.Fatalf("up: %v", p)
	}
}

func TestMouseEventParams(t *testing.T) {
	p, ok := mouseEventParams(rbMouse{Type: "down", X: 200, Y: 100, Button: 2, Buttons: 2, Clicks: 1}, 2)
	if !ok || p["type"] != "mousePressed" || p["button"] != "right" || p["x"] != 100.0 || p["y"] != 50.0 {
		t.Fatalf("right click at zoom 2: %v", p)
	}
	p, ok = mouseEventParams(rbMouse{Type: "wheel", X: 10, Y: 10, DY: 120}, 1)
	if !ok || p["type"] != "mouseWheel" || p["deltaY"] != 120.0 {
		t.Fatalf("wheel: %v", p)
	}
	if _, ok := mouseEventParams(rbMouse{Type: "down", Button: 9}, 1); ok {
		t.Fatal("unknown button accepted")
	}
	if _, ok := mouseEventParams(rbMouse{Type: "bogus"}, 1); ok {
		t.Fatal("unknown type accepted")
	}
}

func TestCheckNavURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com/a?b": "https://example.com/a?b",
		"http://192.168.1.1:8080": "http://192.168.1.1:8080",
		"":                        "about:blank",
		"about:blank":             "about:blank",
		"https://u:p@example.com": "https://example.com",
	} {
		got, err := rbCheckNavURL(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, bad := range []string{"file:///etc/passwd", "chrome://settings", "javascript:alert(1)", "data:text/html,x", "devtools://devtools", "view-source:https://a.com", "ftp://x.com"} {
		if _, err := rbCheckNavURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSafeCursorAndZoomSteps(t *testing.T) {
	if safeCursor("pointer") != "pointer" || safeCursor(`url("https://evil/x.cur"), auto`) != "default" || safeCursor(`url(x.cur), text`) != "text" {
		t.Fatal("cursor filter")
	}
	z := 1.0
	for _, want := range []float64{1.1, 1.25, 1.5} {
		z = zoomStep(z, 1.1)
		if z != want {
			t.Fatalf("zoom in -> %v, want %v", z, want)
		}
	}
	if zoomStep(1, 0.9) != 0.9 || zoomStep(1.5, 0) != 1 || zoomStep(0.5, 0.9) != 0.5 || zoomStep(2, 1.1) != 2 {
		t.Fatal("zoom bounds")
	}
}

// tinyJPEG is a JPEG header with a baseline SOF0 for a 32x16 picture.
func tinyJPEG() []byte {
	return []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00, 0xFF, 0xC0, 0x00, 0x11, 0x08, 0x00, 0x10, 0x00, 0x20, 0x03, 0x01, 0x22, 0x00, 0x02, 0x11, 0x01, 0x03, 0x11, 0x01, 0xFF, 0xD9}
}

func TestJPEGSize(t *testing.T) {
	if w, h := jpegSize(tinyJPEG()); w != 32 || h != 16 {
		t.Fatalf("got %dx%d", w, h)
	}
	if w, h := jpegSize([]byte("not a jpeg")); w != 0 || h != 0 {
		t.Fatal("garbage parsed")
	}
}

func TestProviderCookieOnlyExportsThatDomain(t *testing.T) {
	p := rbProviders["terabox"]
	all := []cdpCookie{
		{Name: "BAIDUID", Value: "b1", Domain: ".1024terabox.com"},
		{Name: "ndus", Value: "SECRET", Domain: ".1024terabox.com", HTTPOnly: true},
		{Name: "lang", Value: "en", Domain: "www.1024terabox.com"},
		{Name: "SID", Value: "google-session", Domain: ".google.com"},
		{Name: "csrf", Value: "x", Domain: ".terabox.com"},
		{Name: "bad", Value: "a;b", Domain: ".1024terabox.com"},
	}
	cookie, domain, auth := providerCookie(p, all)
	if domain != "1024terabox.com" || auth != "SECRET" {
		t.Fatalf("domain %q auth %q", domain, auth)
	}
	if cookie != "ndus=SECRET; BAIDUID=b1; lang=en" {
		t.Fatalf("cookie %q", cookie)
	}
	if strings.Contains(cookie, "google") || strings.Contains(cookie, "csrf") {
		t.Fatal("cookies from other domains leaked")
	}
	if c, _, _ := providerCookie(p, all[:1]); c != "" {
		t.Fatal("no auth cookie must mean not signed in")
	}
	if valuedCookie(cookie) != cookie {
		t.Fatal("export must be in the form the TeraBox backend accepts as-is")
	}
}

// valuedCookie mirrors local-storage/backend/terabox/util.go: a multi-part
// cookie string is used as-is.
func valuedCookie(cookie string) string {
	parts := strings.Split(cookie, ";")
	if len(parts) == 1 && !strings.Contains(parts[0], "=") {
		return "ndus=" + parts[0] + "; lang=en"
	}
	return cookie
}

func TestFindAccountName(t *testing.T) {
	var m map[string]interface{}
	_ = json.Unmarshal([]byte(`{"errno":0,"data":{"display_name":"  Ada  ","uk":1}}`), &m)
	if findAccountName(m) != "Ada" {
		t.Fatalf("got %q", findAccountName(m))
	}
	m = nil
	_ = json.Unmarshal([]byte(`{"errno":0,"records":{"user":{"uname":"bob"}}}`), &m)
	if findAccountName(m) != "bob" {
		t.Fatalf("nested: %q", findAccountName(m))
	}
}

// ---- the instance against a fake Chrome ----

func testInstance(t *testing.T, filters string, allowed ...string) (*rbInstance, *fakeChrome) {
	t.Helper()
	h := &rbHost{stateDir: t.TempDir(), chromeV: "154.0.8037.57", branded: true, instances: map[string]*rbInstance{}}
	h.adblock.Store(&rbAdblockState{enabled: true, allowed: allowed, engine: BuildFilterEngine(filters)})
	in := newRBInstance(h, "1")
	conn, f := newFakeChrome(t, in.onEvent)
	in.cdp = conn
	return in, f
}

func testViewer(in *rbInstance) *rbViewer {
	v := &rbViewer{inst: in, out: make(chan rbOut, 256), done: make(chan struct{}), w: 1280, h: 800, dpr: 1, mode: "browse"}
	in.mu.Lock()
	in.viewers[v] = true
	in.mu.Unlock()
	return v
}

func attachPage(f *fakeChrome, session, target, opener string) {
	f.emit("", "Target.attachedToTarget", map[string]interface{}{
		"sessionId":          session,
		"waitingForDebugger": true,
		"targetInfo":         map[string]interface{}{"targetId": target, "type": "page", "url": "about:blank", "openerId": opener, "browserContextId": "DEFAULT"},
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func nextMsg(t *testing.T, v *rbViewer, pred func(m map[string]interface{}, bin []byte) bool) (map[string]interface{}, []byte) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case o := <-v.out:
			if o.bin {
				if pred(nil, o.data) {
					return nil, o.data
				}
				continue
			}
			var m map[string]interface{}
			_ = json.Unmarshal(o.data, &m)
			if pred(m, nil) {
				return m, nil
			}
		case <-deadline:
			t.Fatal("expected viewer message never came")
		}
	}
}

func TestAttachSetsUpPageBeforeItRuns(t *testing.T) {
	in, f := testInstance(t, "")
	attachPage(f, "P1", "T1", "")
	run := f.waitCall("P1", "Runtime.runIfWaitingForDebugger")
	// Everything that must apply to the very first request is sent before
	// the page is released.
	var ua cdpMessage
	for _, m := range []string{"Page.enable", "Network.enable", "Emulation.setUserAgentOverride", "Fetch.enable", "Page.setInterceptFileChooserDialog", "Target.setAutoAttach"} {
		c := f.waitCall("P1", m)
		if c.ID > run.ID {
			t.Errorf("%s sent after the page was released", m)
		}
		if m == "Emulation.setUserAgentOverride" {
			ua = c
		}
	}
	var p struct {
		UserAgent string `json:"userAgent"`
		Meta      struct {
			Brands []struct{ Brand string } `json:"brands"`
		} `json:"userAgentMetadata"`
	}
	_ = json.Unmarshal(ua.Params, &p)
	if strings.Contains(p.UserAgent, "Headless") || !strings.Contains(p.UserAgent, "Chrome/154.0.0.0") {
		t.Fatalf("UA %q", p.UserAgent)
	}
	if len(p.Meta.Brands) == 0 || p.Meta.Brands[0].Brand != "Google Chrome" {
		t.Fatalf("brands %+v", p.Meta.Brands)
	}
	waitFor(t, "tab", func() bool { return in.tabCount() == 1 })
}

func TestPopupOpensNextToOpenerAndIsShown(t *testing.T) {
	in, f := testInstance(t, "")
	v := testViewer(in)
	attachPage(f, "P1", "T1", "")
	attachPage(f, "P2", "T2", "")
	waitFor(t, "two tabs", func() bool { return in.tabCount() == 2 })
	v.mu.Lock()
	v.active = in.tabForTarget("T1").ID
	v.mu.Unlock()
	attachPage(f, "P3", "T3", "T1") // window.open from T1
	waitFor(t, "popup", func() bool { return in.tabCount() == 3 })
	tabs := in.tabsFor("")
	if tabs[1].ID != in.tabForTarget("T3").ID || tabs[1].Opener != tabs[0].ID {
		t.Fatalf("popup not placed after its opener: %+v", tabs)
	}
	waitFor(t, "viewer switched to the popup", func() bool { return v.activeID() == in.tabForTarget("T3").ID })
}

func pause(f *fakeChrome, session, id, typ, frame, url string) {
	f.emit(session, "Fetch.requestPaused", map[string]interface{}{"requestId": id, "resourceType": typ, "frameId": frame, "request": map[string]string{"url": url}})
}

func TestFetchBlockingUsesFilterEngine(t *testing.T) {
	in, f := testInstance(t, "||ads.example^\n||tracker.test^$third-party\n", "trusted.example")
	attachPage(f, "P1", "T1", "")
	f.waitCall("P1", "Runtime.runIfWaitingForDebugger")
	in.mu.Lock()
	in.bySession["P1"].URL = "https://news.example/story"
	in.mu.Unlock()

	pause(f, "P1", "r1", "Script", "T1", "https://ads.example/x.js")
	pause(f, "P1", "r2", "Image", "T1", "https://cdn.news.example/logo.png")
	// A page the user navigates to is never blocked by a network filter.
	pause(f, "P1", "r3", "Document", "T1", "https://ads.example/")
	pause(f, "P1", "r4", "XHR", "T1", "https://tracker.test/p")
	waitFor(t, "4 answers", func() bool {
		return len(f.callsOf("Fetch.failRequest"))+len(f.callsOf("Fetch.continueRequest")) >= 4
	})
	failed := map[string]bool{}
	for _, c := range f.callsOf("Fetch.failRequest") {
		var p map[string]string
		_ = json.Unmarshal(c.Params, &p)
		if p["errorReason"] != "BlockedByClient" {
			t.Errorf("wrong reason %v", p)
		}
		failed[p["requestId"]] = true
	}
	if !failed["r1"] || failed["r2"] || failed["r3"] || !failed["r4"] {
		t.Fatalf("blocked %v", failed)
	}

	// Trusted site: nothing blocked.
	in.mu.Lock()
	in.bySession["P1"].URL = "https://www.trusted.example/"
	in.mu.Unlock()
	pause(f, "P1", "r5", "Script", "T1", "https://ads.example/x.js")
	waitFor(t, "r5", func() bool { return len(f.callsOf("Fetch.continueRequest")) >= 3 })
	for _, c := range f.callsOf("Fetch.failRequest") {
		if strings.Contains(string(c.Params), "r5") {
			t.Fatal("blocked on a trusted site")
		}
	}

	// Blocked requests (ours and uBO Lite's) are counted for the shield.
	f.emit("P1", "Network.loadingFailed", map[string]interface{}{"requestId": "r1", "errorText": "net::ERR_BLOCKED_BY_CLIENT"})
	f.emit("P1", "Network.loadingFailed", map[string]interface{}{"requestId": "x", "errorText": "net::ERR_CONNECTION_RESET"})
	waitFor(t, "count", func() bool { return in.tabsFor("")[0].Blocked == 1 })
}

func TestIframeRequestsAreFilteredForTheirTab(t *testing.T) {
	in, f := testInstance(t, "||ads.example^\n")
	attachPage(f, "P1", "T1", "")
	f.waitCall("P1", "Runtime.runIfWaitingForDebugger")
	f.emit("P1", "Target.attachedToTarget", map[string]interface{}{"sessionId": "C1", "waitingForDebugger": true,
		"targetInfo": map[string]interface{}{"targetId": "F1", "type": "iframe", "url": "https://frame.example/"}})
	f.waitCall("C1", "Fetch.enable")
	f.waitCall("C1", "Runtime.runIfWaitingForDebugger")
	pause(f, "C1", "i1", "Script", "F1", "https://ads.example/in-frame.js")
	f.waitCall("C1", "Fetch.failRequest")
	f.emit("C1", "Network.loadingFailed", map[string]interface{}{"requestId": "i1", "errorText": "net::ERR_BLOCKED_BY_CLIENT"})
	waitFor(t, "iframe block counted on the tab", func() bool { return in.tabsFor("")[0].Blocked == 1 })
}

func TestDownloadHandedToDownloadStationWithCookies(t *testing.T) {
	in, f := testInstance(t, "")
	f.setHandler(func(m cdpMessage) (interface{}, *cdpError) {
		if m.Method == "Network.getCookies" {
			return map[string]interface{}{"cookies": []cdpCookie{{Name: "sid", Value: "1"}, {Name: "x", Value: "2"}}}, nil
		}
		return nil, nil
	})
	v := testViewer(in)
	attachPage(f, "P1", "T1", "")
	f.waitCall("P1", "Runtime.runIfWaitingForDebugger")
	in.mu.Lock()
	in.bySession["P1"].URL = "https://files.example/page"
	in.mu.Unlock()
	f.emit("", "Browser.downloadWillBegin", map[string]interface{}{"frameId": "T1", "guid": "g1", "url": "https://files.example/big.iso", "suggestedFilename": "big.iso"})
	c := f.waitCall("", "Browser.cancelDownload")
	if !strings.Contains(string(c.Params), "g1") {
		t.Fatalf("cancelled %s", c.Params)
	}
	m, _ := nextMsg(t, v, func(m map[string]interface{}, _ []byte) bool { return m != nil && m["t"] == "download" })
	if m["url"] != "https://files.example/big.iso" || m["filename"] != "big.iso" || m["captured"] == "" {
		t.Fatalf("download message %v", m)
	}
	cp, ok := in.capture(m["captured"].(string))
	if !ok || cp.Headers["Cookie"] != "sid=1; x=2" || cp.Headers["Referer"] != "https://files.example/page" || !strings.Contains(cp.Headers["User-Agent"], "Chrome/154") {
		t.Fatalf("capture %+v", cp)
	}
	// Only the tab's cookies for that URL were asked for.
	gc := f.waitCall("P1", "Network.getCookies")
	if !strings.Contains(string(gc.Params), "https://files.example/big.iso") {
		t.Fatalf("getCookies %s", gc.Params)
	}

	// blob: downloads finish inside the browser, into staging.
	f.emit("", "Browser.downloadWillBegin", map[string]interface{}{"frameId": "T1", "guid": "g2", "url": "blob:https://files.example/uuid", "suggestedFilename": "../../etc/x.txt"})
	f.emit("", "Browser.downloadProgress", map[string]interface{}{"guid": "g2", "receivedBytes": 5, "totalBytes": 5, "state": "completed"})
	m, _ = nextMsg(t, v, func(m map[string]interface{}, _ []byte) bool { return m != nil && m["t"] == "download" && m["staged"] == "g2" })
	d, ok := in.staged("g2")
	if !ok || d.Filename == "" || strings.Contains(d.Filename, "/") || filepath.Dir(d.Path) != in.staging {
		t.Fatalf("staged %+v", d)
	}
}

func TestFrameFlowControl(t *testing.T) {
	in, f := testInstance(t, "")
	v := testViewer(in)
	attachPage(f, "P1", "T1", "")
	f.waitCall("P1", "Runtime.runIfWaitingForDebugger")
	id := in.tabForTarget("T1").ID
	v.mu.Lock()
	v.active = id
	v.mu.Unlock()
	data := base64.StdEncoding.EncodeToString(tinyJPEG())
	frame := func(n int) {
		f.emit("P1", "Page.screencastFrame", map[string]interface{}{"data": data, "sessionId": n, "metadata": map[string]float64{"deviceWidth": 1280, "deviceHeight": 800}})
	}
	acks := func() []int {
		var out []int
		for _, c := range f.callsOf("Page.screencastFrameAck") {
			var p map[string]int
			_ = json.Unmarshal(c.Params, &p)
			out = append(out, p["sessionId"])
		}
		return out
	}
	frame(1)
	frame(2)
	_, b := nextMsg(t, v, func(_ map[string]interface{}, bin []byte) bool { return bin != nil })
	hdr, _ := decodeFrameHeader(b)
	if hdr.Tab != id || hdr.W != 32 || hdr.H != 16 || hdr.CSSW != 1280 {
		t.Fatalf("frame header %+v", hdr)
	}
	waitFor(t, "two acks", func() bool { return len(acks()) == 2 })
	// The viewer has 2 unacknowledged pictures: the third waits, and Chrome
	// is not told to send more.
	frame(3)
	time.Sleep(150 * time.Millisecond)
	if len(acks()) != 2 {
		t.Fatalf("acked Chrome with no room: %v", acks())
	}
	v.onAck()
	waitFor(t, "deferred ack", func() bool { return len(acks()) == 3 && acks()[2] == 3 })
}

func TestTabsPersistAndRestoreLazily(t *testing.T) {
	in, f := testInstance(t, "")
	attachPage(f, "P1", "T1", "")
	attachPage(f, "P2", "T2", "")
	waitFor(t, "tabs", func() bool { return in.tabCount() == 2 })
	in.mu.Lock()
	in.byTarget["T1"].URL, in.byTarget["T1"].Title = "https://a.example/", "A"
	in.byTarget["T2"].URL, in.byTarget["T2"].Title = "https://b.example/", "B"
	in.mu.Unlock()
	in.saveTabsNow()

	again := newRBInstance(in.host, "1")
	again.restoreTabs()
	tabs := again.tabsFor("")
	if len(tabs) != 2 || tabs[0].URL != "https://a.example/" || tabs[1].Title != "B" {
		t.Fatalf("restored %+v", tabs)
	}
	for _, tt := range again.tabs {
		if !tt.Discarded {
			t.Fatal("restored tabs must not load until opened")
		}
	}
}

func TestTypedHostsPersist(t *testing.T) {
	in, _ := testInstance(t, "")
	in.allowTyped("NAS.local.")
	if !in.isTyped("nas.local") {
		t.Fatal("typed host not allowed")
	}
	again := newRBInstance(in.host, "1")
	again.loadTyped()
	if !again.isTyped("nas.local") || again.isTyped("other.local") {
		t.Fatal("typed hosts not persisted per profile")
	}
}

// ---- egress proxy (netguard) ----

func connectVia(t *testing.T, proxy, target string) int {
	t.Helper()
	c, err := net.DialTimeout("tcp", proxy, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatalf("CONNECT %s: %v", target, err)
	}
	return resp.StatusCode
}

func TestEgressRefusesLoopbackMetadataAndUntypedLAN(t *testing.T) {
	typed := map[string]bool{}
	eg, err := newRBEgress(func(h string) bool { return typed[normalizeHost(h)] })
	if err != nil {
		t.Fatal(err)
	}
	defer eg.Close()
	// A local service a page must never reach (every NivaroOS API trusts
	// some loopback callers).
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("secret")) }))
	defer local.Close()
	_, port, _ := net.SplitHostPort(local.Listener.Addr().String())
	for _, target := range []string{"127.0.0.1:" + port, "localhost:" + port, "[::1]:" + port, "169.254.169.254:80", "0.0.0.0:" + port, "foo.localhost:80"} {
		if code := connectVia(t, eg.Addr(), target); code != http.StatusForbidden {
			t.Errorf("CONNECT %s -> %d, want 403", target, code)
		}
	}
	// Even a host the user typed cannot be loopback.
	typed["127.0.0.1"] = true
	if code := connectVia(t, eg.Addr(), "127.0.0.1:"+port); code != http.StatusForbidden {
		t.Errorf("typed loopback -> %d", code)
	}
	// Plain-HTTP proxying goes through the same check.
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(mustURL("http://" + eg.Addr()))}}).Get(local.URL)
	if err == nil {
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("GET via proxy -> %d", resp.StatusCode)
		}
		resp.Body.Close()
	}

	// A LAN address (this box's own, here) only when typed.
	own := ownPrivateIPv4()
	if own == "" {
		t.Skip("no private IPv4 address on this machine")
	}
	ln, err := net.Listen("tcp", own+":0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if code := connectVia(t, eg.Addr(), ln.Addr().String()); code != http.StatusForbidden {
		t.Errorf("untyped LAN -> %d", code)
	}
	typed[own] = true
	if code := connectVia(t, eg.Addr(), ln.Addr().String()); code != http.StatusOK {
		t.Errorf("typed LAN -> %d, want 200", code)
	}
}

func ownPrivateIPv4() string {
	as, _ := net.InterfaceAddrs()
	for _, a := range as {
		if n, ok := a.(*net.IPNet); ok {
			if ip := n.IP.To4(); ip != nil && ip.IsPrivate() {
				return ip.String()
			}
		}
	}
	return ""
}

// ---- sidecar side ----

func TestMoveStagedRefusesLinksAndStrayPaths(t *testing.T) {
	staging := t.TempDir()
	dest := t.TempDir()
	c := &rbClient{stagingDir: staging}
	root := filepath.Join(staging, "1")
	_ = os.MkdirAll(root, 0o700)
	good := filepath.Join(root, "guid-1")
	_ = os.WriteFile(good, []byte("hello"), 0o600)
	p, err := c.moveStaged("1", rbStaged{Path: good, Filename: "../report.pdf"}, dest)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != dest || filepath.Base(p) == ".." {
		t.Fatalf("moved to %s", p)
	}
	if b, _ := os.ReadFile(p); string(b) != "hello" {
		t.Fatal("content")
	}
	if _, err := os.Stat(good); !os.IsNotExist(err) {
		t.Fatal("staged file left behind")
	}
	// Same name again gets a new one.
	_ = os.WriteFile(good, []byte("2"), 0o600)
	p2, err := c.moveStaged("1", rbStaged{Path: good, Filename: "report.pdf"}, dest)
	if err != nil || p2 == p {
		t.Fatalf("second move %s %v", p2, err)
	}

	secret := filepath.Join(t.TempDir(), "shadow")
	_ = os.WriteFile(secret, []byte("root only"), 0o600)
	link := filepath.Join(root, "guid-link")
	_ = os.Symlink(secret, link)
	if _, err := c.moveStaged("1", rbStaged{Path: link, Filename: "x"}, dest); err == nil {
		t.Fatal("followed a symlink out of staging")
	}
	hard := filepath.Join(root, "guid-hard")
	if os.Link(secret, hard) == nil {
		if _, err := c.moveStaged("1", rbStaged{Path: hard, Filename: "x"}, dest); err == nil {
			t.Fatal("moved a hard link to another file")
		}
	}
	if _, err := c.moveStaged("1", rbStaged{Path: secret, Filename: "x"}, dest); err == nil {
		t.Fatal("moved a file outside staging")
	}
	if _, err := c.moveStaged("2", rbStaged{Path: good, Filename: "x"}, dest); err == nil {
		t.Fatal("moved another user's file")
	}
}

func TestRBUserIDFromToken(t *testing.T) {
	tok := func(claims string) string {
		return "x." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".sig"
	}
	r := httptest.NewRequest("GET", "/rb/ws?token="+tok(`{"id":7,"username":"ada"}`), nil)
	if rbUserID(r) != "7" {
		t.Fatalf("got %s", rbUserID(r))
	}
	r = httptest.NewRequest("GET", "/rb/status", nil)
	r.Header.Set("Authorization", "Bearer "+tok(`{"username":"ada"}`))
	if rbUserID(r) != "u-ada" {
		t.Fatalf("got %s", rbUserID(r))
	}
	r.Header.Set("Authorization", tok(`{"id":"../../etc"}`))
	if rbUserID(r) != "local" {
		t.Fatalf("unsafe id accepted: %s", rbUserID(r))
	}
}

func TestApplyRBCaptureFillsHeadersForSameURLOnly(t *testing.T) {
	old := rbCaptureLookup
	defer func() { rbCaptureLookup = old }()
	rbCaptureLookup = func(ctx context.Context, uid, id string) (*rbCapture, error) {
		return &rbCapture{ID: id, URL: "https://f.example/a.zip", Filename: "a.zip", Headers: map[string]string{"Cookie": "s=1"}}, nil
	}
	r := httptest.NewRequest("POST", "/downloads", nil)
	req := AddRequest{}
	if err := applyRBCapture(r, "c1", &req); err != nil {
		t.Fatal(err)
	}
	if req.URL != "https://f.example/a.zip" || req.Headers["Cookie"] != "s=1" || req.Source != "browser" || req.Filename != "a.zip" {
		t.Fatalf("%+v", req)
	}
	// The dialog changed the URL to something else: that site must not get
	// these cookies.
	req = AddRequest{URL: "https://other.example/b.zip"}
	_ = applyRBCapture(r, "c1", &req)
	if req.Headers["Cookie"] != "" {
		t.Fatal("cookies sent to a different URL")
	}
}

func TestRBStatusWithoutService(t *testing.T) {
	c := newRBClient(filepath.Join(t.TempDir(), "none.sock"), t.TempDir(), nil, nil)
	st := c.status(context.Background(), "1")
	if st["available"] != false || st["reason"] != "not_installed" || st["engine"] != "lite" {
		t.Fatalf("%v", st)
	}
}

func TestSidecarRelaysToHostOverUnixSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "b.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 4)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Method + " " + r.URL.Path + " user=" + r.Header.Get(rbUserHeader) + " auth=" + r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/status":
			writeJSON(w, 200, map[string]interface{}{"available": true, "running": false})
		default:
			w.WriteHeader(404)
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()
	c := newRBClient(sock, dir, nil, nil)
	st := c.status(context.Background(), "9")
	if st["available"] != true || st["engine"] != "chromium" {
		t.Fatalf("%v", st)
	}
	if got := <-seen; got != "GET /status user=9 auth=" {
		t.Fatalf("host saw %q", got)
	}
}

func mustURL(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}

func TestHostQuietPeriodCountsFromLastViewer(t *testing.T) {
	h := &rbHost{stateDir: t.TempDir(), instances: map[string]*rbInstance{}, idle: 10 * time.Minute}
	left := time.Now().Add(-11 * time.Minute)
	h.lastUse = left.Add(-time.Minute)
	in := newRBInstance(h, "1")
	in.lastView = left
	h.instances["1"] = in
	if in.idleFor(time.Now()) < h.idle {
		t.Fatal("instance should be idle")
	}
	h.dropInstance(in)
	if time.Since(h.lastUse) < h.idle {
		t.Fatalf("host would wait another %s after closing an idle browser", h.idle-time.Since(h.lastUse))
	}
}
