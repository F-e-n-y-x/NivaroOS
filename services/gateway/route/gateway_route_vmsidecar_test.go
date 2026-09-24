package route

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVMSidecarRouting(t *testing.T) {
	for p, want := range map[string]bool{
		"/v1/vm-sidecar":                   true,
		"/v1/vm-sidecar/vms":               true,
		"/v1/vm-sidecar/vms/win11/console": true,
		"/v1/vm-sidecarx":                  false,
		"/v1/vm":                           false,
	} {
		if got := isVMSidecarPath(p); got != want {
			t.Errorf("isVMSidecarPath(%q)=%v want %v", p, got, want)
		}
	}
	for in, want := range map[string]string{
		"/v1/vm-sidecar":              "/",
		"/v1/vm-sidecar/vms":          "/vms",
		"/v1/vm-sidecar/host/console": "/host/console",
	} {
		if got := stripVMSidecarPrefix(in); got != want {
			t.Errorf("stripVMSidecarPrefix(%q)=%q want %q", in, got, want)
		}
	}
}

// Through the gateway's handler to a fake sidecar: plain requests arrive
// with the prefix stripped and marked as proxied (so the sidecar never
// takes them for same-host automation), a forged automation header is
// dropped, and a WebSocket upgrade (the VNC console) is carried both ways.
func TestVMSidecarProxyEndToEnd(t *testing.T) {
	type seen struct{ path, xff, marker string }
	got := make(chan seen, 4)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- seen{r.URL.Path, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Nivaroos-Local-Automation")}
		if r.Header.Get("Upgrade") != "websocket" {
			_, _ = io.WriteString(w, "ok")
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		buf := make([]byte, 4)
		if _, err := io.ReadFull(rw, buf); err == nil {
			_, _ = rw.Write(buf)
			_ = rw.Flush()
		}
	}))
	defer backend.Close()

	old := vmSidecarProxy
	vmSidecarProxy = newVMSidecarProxy(strings.TrimPrefix(backend.URL, "http://"))
	defer func() { vmSidecarProxy = old }()
	gw := httptest.NewServer(NewGatewayRoute(nil).GetRoute())
	defer gw.Close()

	req, _ := http.NewRequest(http.MethodGet, gw.URL+"/v1/vm-sidecar/vms?x=1", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Nivaroos-Local-Automation", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	s := <-got
	if res.StatusCode != 200 || s.path != "/vms" || s.xff == "" || s.marker != "" {
		t.Fatalf("status %d, backend saw %+v", res.StatusCode, s)
	}

	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(gw.URL, "http://"), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.WriteString(conn, "GET /v1/vm-sidecar/vms/win11/console?token=t HTTP/1.1\r\nHost: nivaro.example.com\r\n"+
		"Origin: https://nivaro.example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n"+
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")
	br := bufio.NewReader(conn)
	status, _ := br.ReadString('\n')
	if !strings.Contains(status, "101") {
		t.Fatalf("upgrade through the gateway: %q", status)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" {
			break
		}
	}
	if s := <-got; s.path != "/vms/win11/console" {
		t.Fatalf("console path %q", s.path)
	}
	_, _ = io.WriteString(conn, "ping")
	echo := make([]byte, 4)
	if _, err := io.ReadFull(br, echo); err != nil || string(echo) != "ping" {
		t.Fatalf("websocket data through the gateway: %q %v", echo, err)
	}
}
