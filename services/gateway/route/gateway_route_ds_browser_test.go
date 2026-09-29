package route

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Download Station's full browser: its API and picture-stream WebSocket
// live under /v1/download-station/rb/ and must be proxied (unlike the lite
// browser's /b/ pages, which never are).
func TestDownloadStationBrowserRoutes(t *testing.T) {
	for _, p := range []string{"/v1/download-station/rb/ws", "/v1/download-station/rb/status", "/v1/download-station/rb/cookies/export"} {
		if !isDownloadStationPath(p) || isDownloadStationBrowserPath(p) {
			t.Errorf("%s must be proxied to the sidecar", p)
		}
	}
}

// The proxy must carry a WebSocket upgrade both ways (the browser stream).
func TestDownloadStationProxyCarriesWebSocketUpgrade(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/download-station/rb/ws" || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "not an upgrade", 400)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		rw.Flush()
		// Echo whatever the client sends.
		buf := make([]byte, 64)
		n, _ := rw.Read(buf)
		_, _ = conn.Write(buf[:n])
	}))
	defer backend.Close()
	proxy := newDSProxy(strings.TrimPrefix(backend.URL, "http://"))
	front := httptest.NewServer(proxy)
	defer front.Close()

	c, err := net.Dial("tcp", strings.TrimPrefix(front.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(c, "GET /v1/download-station/rb/ws?token=x HTTP/1.1\r\nHost: nas\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	fmt.Fprint(c, "frame")
	got := make([]byte, 5)
	if _, err := io.ReadFull(br, got); err != nil || string(got) != "frame" {
		t.Fatalf("echo %q %v", got, err)
	}
}
