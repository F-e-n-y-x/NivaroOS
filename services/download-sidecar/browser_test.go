package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testBrowser(t *testing.T, filters string) (*Browser, *BrowserSession, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	st := NewSettingsStore(dir, dir)
	_, _ = st.Update(func(s *Settings) { s.EnabledLists = nil; s.CustomFilters = filters; s.AdblockEnabled = true })
	ab := NewAdblocker(dir, st, newTransport(true))
	ab.Rebuild()
	br := NewBrowser(newTransport(true), ab, st)
	s := br.NewSession()
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	return br, s, route(br, api)
}

func TestProxyRewritesPageAndInjects(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
			w.Header().Set("Set-Cookie", "sess=abc; Path=/")
			io.WriteString(w, `<!doctype html><html><head><title>T</title><link rel="stylesheet" href="/s.css" integrity="sha384-x"></head>
<body><a href="/file.zip" target="_blank">dl</a><img src="https://ads.test/b.png"><div class="ad-slot"></div>
<script src="https://ads.test/x.js"></script><img srcset="/a.png 1x, /b.png 2x"></body></html>`)
		case "/s.css":
			w.Header().Set("Content-Type", "text/css")
			io.WriteString(w, `body{background:url(/bg.png)} @import "other.css";`)
		case "/file.zip":
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", `attachment; filename="file.zip"`)
			w.Header().Set("Accept-Ranges", "bytes")
			io.WriteString(w, "PK....")
		}
	}))
	defer upstream.Close()
	_, s, h := testBrowser(t, "||ads.test^\n##.ad-slot\n")
	u, _ := url.Parse(upstream.URL)
	pageProxy := s.proxyPath(u)

	req := httptest.NewRequest("GET", pageProxy, nil)
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Header().Get("X-Frame-Options") != "" || rec.Header().Get("Content-Security-Policy") != "" || rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("framing/cookie headers not stripped: %v", rec.Header())
	}
	prefix := "/b/" + s.ID + "/http/" + u.Host
	for _, want := range []string{
		`href="` + prefix + `/s.css"`,
		`href="` + prefix + `/file.zip"`,
		`target="_self"`,
		prefix + `/a.png 1x`,
		`<script data-nvds>`,
		`.ad-slot{display:none!important}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rewritten page missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "ads.test/x.js") || strings.Contains(body, "ads.test/b.png") {
		t.Error("blocked resources still referenced")
	}
	if strings.Contains(body, "integrity") {
		t.Error("SRI attribute kept")
	}
	if s.Stats().BlockedTotal < 2 {
		t.Errorf("blocked count %d", s.Stats().BlockedTotal)
	}
	// The server-set cookie went into the session jar.
	if len(s.jar.Cookies(u)) != 1 {
		t.Error("cookie not stored in jar")
	}

	// CSS rewriting.
	req = httptest.NewRequest("GET", prefix+"/s.css", nil)
	req.Header.Set("Sec-Fetch-Dest", "style")
	req.Header.Set("Referer", "http://nas"+pageProxy)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if css := rec.Body.String(); !strings.Contains(css, `url("`+prefix+`/bg.png")`) || !strings.Contains(css, `@import "`+prefix+`/other.css"`) {
		t.Errorf("css not rewritten: %s", css)
	}

	// Clicking the download link is captured, with the jar's cookie.
	req = httptest.NewRequest("GET", prefix+"/file.zip", nil)
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	req.Header.Set("Referer", "http://nas"+pageProxy)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"type":"download"`) {
		t.Fatalf("download not captured: %s", rec.Body.String())
	}
	if len(s.captures) != 1 {
		t.Fatal("no capture recorded")
	}
	for _, c := range s.captures {
		if c.Filename != "file.zip" || c.headers["Cookie"] != "sess=abc" || !strings.HasPrefix(c.headers["Referer"], upstream.URL) {
			t.Errorf("capture: %+v headers=%v", c, c.headers)
		}
	}
}

func TestRefererRedirectAndBlockedDocument(t *testing.T) {
	_, s, h := testBrowser(t, "||blocked.test^\n")
	page, _ := url.Parse("https://site.test/dir/page.html")
	req := httptest.NewRequest("GET", "/api/list?x=1", nil)
	req.Header.Set("Referer", "http://nas:28642"+s.proxyPath(page))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/b/"+s.ID+"/https/site.test/api/list?x=1" {
		t.Fatalf("referer redirect: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// Without a proxied Referer, the API's auth still runs.
	req = httptest.NewRequest("GET", "/downloads", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("expected API auth, got %d", rec.Code)
	}

	bu, _ := url.Parse("https://blocked.test/")
	req = httptest.NewRequest("GET", s.proxyPath(bu), nil)
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Proceed anyway") {
		t.Fatalf("expected strict-block page, got %s", rec.Body.String())
	}
}

func TestProxyRefusesLoopback(t *testing.T) {
	dir := t.TempDir()
	st := NewSettingsStore(dir, dir)
	ab := NewAdblocker(dir, st, newTransport(false))
	br := NewBrowser(newTransport(false), ab, st)
	s := br.NewSession()
	h := route(br, http.NotFoundHandler())
	u, _ := url.Parse("http://127.0.0.1:28641/vms")
	req := httptest.NewRequest("GET", s.proxyPath(u), nil)
	req.Header.Set("Sec-Fetch-Dest", "document")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "localhost") {
		t.Fatalf("loopback not refused: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnknownSessionRejected(t *testing.T) {
	_, _, h := testBrowser(t, "")
	req := httptest.NewRequest("GET", "/b/nope/https/example.com/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestHistoryDedupesAndSearches(t *testing.T) {
	h := NewHistory(t.TempDir())
	h.Add("https://a.test/", "")
	h.Add("https://a.test/", "Page A") // reload/title update: same entry
	h.Add("https://b.test/x", "Bee")
	if _, ok := h.Add("javascript:alert(1)", "x"); ok {
		t.Error("non-http URL accepted")
	}
	all := h.List("", 0)
	if len(all) != 2 || all[0].URL != "https://b.test/x" || all[1].Title != "Page A" {
		t.Fatalf("history: %+v", all)
	}
	if got := h.List("bee", 0); len(got) != 1 {
		t.Fatalf("search: %+v", got)
	}
	h.Delete(all[0].ID)
	if len(h.List("", 0)) != 1 {
		t.Fatal("delete failed")
	}
	h.Clear()
	if len(h.List("", 0)) != 0 {
		t.Fatal("clear failed")
	}
}

func TestDriveWarningPageIsCapturedDirectly(t *testing.T) {
	_, s, h := testBrowser(t, "")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<form id="download-form" action="https://drive.usercontent.google.com/download" method="get"><input type="hidden" name="id" value="X"><input type="hidden" name="confirm" value="t"></form>`)
	}))
	defer upstream.Close()
	s.client.Transport = rewriteHostTransport{to: upstream.URL, next: s.client.Transport}
	u, _ := url.Parse("https://drive.usercontent.google.com/download?id=X")
	req := httptest.NewRequest("GET", s.proxyPath(u), nil)
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"type":"download"`) {
		t.Fatalf("warning page not captured: %s", rec.Body.String())
	}
	for _, c := range s.captures {
		if !strings.Contains(c.URL, "confirm=t") || !strings.Contains(c.URL, "id=X") {
			t.Errorf("capture url %s", c.URL)
		}
	}
}
