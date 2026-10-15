package service

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

func testTLS(t *testing.T, h http.Handler) (*TLS, *[]bool) {
	var saved []bool
	tl := &TLS{
		Dir: t.TempDir() + "/tls", Port: "0", Handler: h,
		Save: func(e, r bool) error { saved = append(saved, e, r); return nil },
		SANs: func() ([]string, []net.IP) { return []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")} },
	}
	t.Cleanup(tl.Stop)
	return tl, &saved
}

func trustingClient(t *testing.T, tl *TLS) *tls.Config {
	pool := x509.NewCertPool()
	root, err := x509.ParseCertificate(tl.CA().CADER())
	if err != nil {
		t.Fatal(err)
	}
	pool.AddCert(root)
	return &tls.Config{RootCAs: pool, NextProtos: []string{"h2", "http/1.1"}}
}

// Off by default: nothing listens and no CA files are created.
func TestTLSOffByDefault(t *testing.T) {
	tl, _ := testTLS(t, http.NotFoundHandler())
	if err := tl.Apply(); err != nil || tl.Addr() != "" || tl.CA() != nil {
		t.Fatalf("off: err %v addr %q", err, tl.Addr())
	}
	if st := tl.Status(); st.Enabled || st.Running || st.Fingerprint != "" {
		t.Fatalf("%+v", st)
	}
}

func TestTLSServesSameHandlerHTTP1(t *testing.T) {
	// Backend answers a WebSocket-style upgrade (as the vm-sidecar / message
	// bus do) and normal requests; the gateway proxies to it.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			c, buf, _ := w.(http.Hijacker).Hijack()
			buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			buf.Flush()
			c.Close()
			return
		}
		io.WriteString(w, "proto="+r.Header.Get("X-Forwarded-Proto"))
	}))
	defer backend.Close()
	u, _ := url.Parse(backend.URL)
	tl, saved := testTLS(t, httputil.NewSingleHostReverseProxy(u))

	if err := tl.Set(true, true); err != nil {
		t.Fatal(err)
	}
	if len(*saved) != 2 || !(*saved)[0] || !(*saved)[1] {
		t.Fatalf("saved %v", *saved)
	}
	cfg := trustingClient(t, tl)
	addr := tl.Addr()
	_, port, _ := net.SplitHostPort(addr)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, ForceAttemptHTTP2: true}}
	res, err := client.Get("https://localhost:" + port + "/x")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "proto=https" || res.ProtoMajor != 1 {
		t.Fatalf("body %q proto %s", body, res.Proto)
	}

	// WebSocket upgrade through the TLS listener + reverse proxy.
	conn, err := tls.Dial("tcp", "127.0.0.1:"+port, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p := conn.ConnectionState().NegotiatedProtocol; p == "h2" {
		t.Fatal("h2 negotiated")
	}
	conn.Write([]byte("GET /ws HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"))
	line, _ := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if !strings.Contains(line, "101") {
		t.Fatalf("upgrade: %q", line)
	}

	if p, ok := tl.RedirectPort("127.0.0.1"); !ok || p != port {
		t.Fatalf("redirect port %q %v", p, ok)
	}
	if _, ok := tl.RedirectPort("nas.example.com"); ok {
		t.Fatal("redirect to a name the cert lacks")
	}
	if st := tl.Status(); !st.Running || st.Fingerprint == "" || st.NotAfter == "" {
		t.Fatalf("%+v", st)
	}

	// Off: listener closed, redirect off, CA kept for re-enable.
	fp := tl.Status().Fingerprint
	if err := tl.Set(false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := tls.Dial("tcp", addr, cfg); err == nil {
		t.Fatal("still listening")
	}
	if st := tl.Status(); st.Enabled || st.Redirect || st.Running || st.Fingerprint != fp {
		t.Fatalf("%+v", st)
	}
	if _, ok := tl.RedirectPort("127.0.0.1"); ok {
		t.Fatal("redirect while off")
	}
}

// Port taken: enabling fails, stays off, nothing saved.
func TestTLSPortBusy(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	tl, saved := testTLS(t, http.NotFoundHandler())
	_, tl.Port, _ = net.SplitHostPort(ln.Addr().String())
	if err := tl.Set(true, false); err == nil {
		t.Fatal("enabled on a busy port")
	}
	if st := tl.Status(); st.Enabled || st.Running || len(*saved) != 0 {
		t.Fatalf("%+v saved %v", st, *saved)
	}
}
