package main

// The Download Station browser's egress proxy. Chromium is started with
// --proxy-server pointing here (and --proxy-bypass-list=<-loopback>, so
// even localhost goes through it), so every connection a page makes is
// dialed by this code and checked by the same netguard rules as the
// download engine:
//
//   - loopback, link-local, 0/8 and multicast are never reachable;
//   - private, CGNAT (Tailscale) and this box's own addresses only for a
//     host the user typed into the address bar;
//   - the check runs on the resolved IP at dial time, so DNS rebinding
//     cannot slip past it.
//
// QUIC is off in Chromium and WebRTC may not use non-proxied UDP, so
// nothing leaves the browser any other way.

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rbEgress struct {
	ln        net.Listener
	srv       *http.Server
	typed     func(host string) bool
	strict    func(ctx context.Context, network, addr string) (net.Conn, error)
	lan       func(ctx context.Context, network, addr string) (net.Conn, error)
	transport *http.Transport
	lanTr     *http.Transport
	resolver  *net.Resolver

	mu    sync.Mutex
	conns map[net.Conn]bool
}

// newRBEgress listens on 127.0.0.1 on a random port. typed reports whether
// a host was typed by the user (and so may be on the LAN).
func newRBEgress(typed func(string) bool) (*rbEgress, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	e := &rbEgress{
		ln:       ln,
		typed:    typed,
		strict:   guardedDialer(false, nil),
		lan:      guardedDialer(true, nil),
		resolver: net.DefaultResolver,
		conns:    map[net.Conn]bool{},
	}
	e.transport = e.newTransport(e.strict)
	e.lanTr = e.newTransport(e.lan)
	e.srv = &http.Server{Handler: e, ReadHeaderTimeout: 30 * time.Second}
	go func() { _ = e.srv.Serve(ln) }()
	return e, nil
}

func (e *rbEgress) newTransport(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *http.Transport {
	t := baseTransport()
	t.Proxy = nil
	t.DialContext = dial
	// Chromium does its own content negotiation; pass bodies through as-is.
	t.DisableCompression = true
	return t
}

func (e *rbEgress) Addr() string { return e.ln.Addr().String() }

func (e *rbEgress) Close() {
	_ = e.srv.Close()
	e.mu.Lock()
	for c := range e.conns {
		_ = c.Close()
	}
	e.mu.Unlock()
	e.transport.CloseIdleConnections()
	e.lanTr.CloseIdleConnections()
}

// dialerFor picks the strict or LAN dialer for a destination host. A name
// like "localhost" is refused before any lookup.
func (e *rbEgress) dialerFor(host string) (func(ctx context.Context, network, addr string) (net.Conn, error), error) {
	h := normalizeHost(host)
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return nil, forbiddenError{}
	}
	if ip := net.ParseIP(h); ip != nil {
		if err := isForbiddenIP(ip, e.typed(h)); err != nil {
			return nil, err
		}
	}
	if e.typed(h) {
		return e.lan, nil
	}
	return e.strict, nil
}

func (e *rbEgress) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		e.serveConnect(w, r)
		return
	}
	if r.URL == nil || !r.URL.IsAbs() || (r.URL.Scheme != "http" && r.URL.Scheme != "ws") {
		http.Error(w, "this is Download Station's browser proxy", http.StatusBadRequest)
		return
	}
	host := r.URL.Hostname()
	if _, err := e.dialerFor(host); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		// Plain ws:// through an HTTP proxy is sent as a normal request
		// with an Upgrade header - tunnel it as raw TCP.
		e.tunnelUpgrade(w, r)
		return
	}
	tr := e.transport
	if e.typed(normalizeHost(host)) {
		tr = e.lanTr
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	removeHopHeaders(out.Header)
	resp, err := tr.RoundTrip(out)
	if err != nil {
		code := http.StatusBadGateway
		if isForbidden(err) {
			code = http.StatusForbidden
		}
		http.Error(w, err.Error(), code)
		return
	}
	defer resp.Body.Close()
	removeHopHeaders(resp.Header)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flushCopy(w, resp.Body)
}

func isForbidden(err error) bool { return errors.Is(err, errForbiddenDestination) }

func (e *rbEgress) serveConnect(w http.ResponseWriter, r *http.Request) {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		http.Error(w, "bad CONNECT target", http.StatusBadRequest)
		return
	}
	dial, err := e.dialerFor(host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	up, err := dial(ctx, "tcp", net.JoinHostPort(normalizeHost(host), port))
	cancel()
	if err != nil {
		code := http.StatusBadGateway
		if isForbidden(err) {
			code = http.StatusForbidden
		}
		http.Error(w, err.Error(), code)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		up.Close()
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	down, rw, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	_, _ = down.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	// Anything Chromium already sent after the CONNECT header.
	if n := rw.Reader.Buffered(); n > 0 {
		buf, _ := rw.Reader.Peek(n)
		_, _ = up.Write(buf)
	}
	e.pipe(down, up)
}

func (e *rbEgress) tunnelUpgrade(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Hostname()
	port := r.URL.Port()
	if port == "" {
		port = "80"
	}
	dial, err := e.dialerFor(host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	up, err := dial(ctx, "tcp", net.JoinHostPort(normalizeHost(host), port))
	cancel()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		up.Close()
		return
	}
	down, rw, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	out := r.Clone(context.Background())
	out.RequestURI = ""
	if err := out.Write(up); err != nil {
		up.Close()
		down.Close()
		return
	}
	if n := rw.Reader.Buffered(); n > 0 {
		buf, _ := rw.Reader.Peek(n)
		_, _ = up.Write(buf)
	}
	e.pipe(down, up)
}

func (e *rbEgress) pipe(a, b net.Conn) {
	e.mu.Lock()
	e.conns[a], e.conns[b] = true, true
	e.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if tc, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = tc.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
	_ = a.Close()
	_ = b.Close()
	e.mu.Lock()
	delete(e.conns, a)
	delete(e.conns, b)
	e.mu.Unlock()
}

var egressHopHeaders = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

func removeHopHeaders(h http.Header) {
	for _, f := range h["Connection"] {
		for _, k := range strings.Split(f, ",") {
			if k = strings.TrimSpace(k); k != "" {
				h.Del(k)
			}
		}
	}
	for _, k := range egressHopHeaders {
		h.Del(k)
	}
}

func flushCopy(w http.ResponseWriter, r io.Reader) {
	fl, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("ds-browser egress: %v", err)
			}
			return
		}
	}
}
