package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveShareURL(t *testing.T) {
	cases := map[string]string{
		"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOp/view?usp=sharing":   "https://drive.usercontent.google.com/download?confirm=t&export=download&id=1AbCdEfGhIjKlMnOp",
		"https://drive.google.com/file/u/0/d/1AbCdEfGhIjKlMnOp/edit":           "https://drive.usercontent.google.com/download?confirm=t&export=download&id=1AbCdEfGhIjKlMnOp",
		"https://drive.google.com/uc?id=1AbCdEfGhIjKlMnOp&export=download":     "https://drive.usercontent.google.com/download?confirm=t&export=download&id=1AbCdEfGhIjKlMnOp",
		"https://drive.google.com/open?id=1AbCdEfGhIjKlMnOp&resourcekey=0-xyz": "https://drive.usercontent.google.com/download?confirm=t&export=download&id=1AbCdEfGhIjKlMnOp&resourcekey=0-xyz",
		"https://example.com/file/d/1AbCdEfGhIjKlMnOp/view":                    "https://example.com/file/d/1AbCdEfGhIjKlMnOp/view",
	}
	for in, want := range cases {
		u, _ := url.Parse(in)
		if got := resolveShareURL(u).String(); got != want {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}

func TestParseDriveWarningForm(t *testing.T) {
	page := []byte(`<html><body><form id="download-form" action="https://drive.usercontent.google.com/download" method="get">
<input type="submit" value="Download anyway"/><input type="hidden" name="id" value="ABC"><input type="hidden" name="confirm" value="t"><input type="hidden" name="uuid" value="u-1"></form></body></html>`)
	base, _ := url.Parse("https://drive.usercontent.google.com/download?id=ABC")
	u := parseDownloadForm(page, base)
	if u == nil || u.Host != "drive.usercontent.google.com" || u.Query().Get("uuid") != "u-1" || u.Query().Get("confirm") != "t" {
		t.Fatalf("got %v", u)
	}
}

// End to end: an HTML warning page on a Drive-looking host is followed to
// the real file, which then downloads over multiple connections.
func TestEngineFollowsDriveWarningPage(t *testing.T) {
	m, dlDir := testManager(t)
	content := randomBytes(4 << 20)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("uuid") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<form id="download-form" action="%s/download" method="get"><input type="hidden" name="id" value="X"><input type="hidden" name="uuid" value="u"></form>`, srv.URL)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="model.npz"`)
		http.ServeContent(w, r, "model.npz", time.Time{}, bytes.NewReader(content))
	}))
	defer srv.Close()
	// followInterstitial only trusts Google's hosts; route that name to the
	// test server for this test.
	m.client.Transport = rewriteHostTransport{to: srv.URL, next: m.client.Transport}

	v, err := m.Add(AddRequest{URL: "https://drive.usercontent.google.com/download?id=X&confirm=t", Connections: 4})
	if err != nil {
		t.Fatal(err)
	}
	v = waitState(t, m, v.ID, StateCompleted, 20*time.Second)
	if v.Filename != "model.npz" || !v.Resumable {
		t.Fatalf("filename=%q resumable=%v", v.Filename, v.Resumable)
	}
	got, _ := os.ReadFile(filepath.Join(dlDir, "model.npz"))
	if !bytes.Equal(got, content) {
		t.Fatal("content differs")
	}
}

// rewriteHostTransport sends every request to a test server while keeping
// the original URL visible on resp.Request (as a real redirect-free fetch
// would).
type rewriteHostTransport struct {
	to   string
	next http.RoundTripper
}

func (rt rewriteHostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	target, _ := url.Parse(rt.to)
	orig := r.URL
	r2 := r.Clone(r.Context())
	r2.URL.Scheme, r2.URL.Host = target.Scheme, target.Host
	r2.Host = target.Host
	resp, err := rt.next.RoundTrip(r2)
	if resp != nil {
		resp.Request = r.Clone(r.Context())
		resp.Request.URL = orig
	}
	return resp, err
}
