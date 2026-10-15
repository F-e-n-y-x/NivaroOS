package service

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/gateway/pkg"
)

// TLS is the optional https listener for the home network (default off,
// docs/specs/2026-10-16-lan-https.md). It serves the same handler as the
// http listener, which keeps running regardless.
type TLS struct {
	Dir     string                             // CA and server cert, created on first enable
	Port    string                             // "443"; "0" in tests
	Handler http.Handler                       // the gateway mux, set before Apply
	Save    func(enabled, redirect bool) error // persists to gateway.ini
	SANs    func() ([]string, []net.IP)        // pkg.LocalSANs; tests replace it

	mu       sync.Mutex
	enabled  bool
	redirect bool
	ca       *pkg.LocalCA
	srv      *http.Server
	addr     string
	lastErr  string
}

type TLSStatus struct {
	Enabled     bool   `json:"enabled"`
	Running     bool   `json:"running"`
	Port        string `json:"port"`
	Redirect    bool   `json:"redirect"`
	Fingerprint string `json:"fingerprint,omitempty"` // CA SHA-256
	NotAfter    string `json:"not_after,omitempty"`   // server cert expiry, RFC 3339
	Error       string `json:"error,omitempty"`
}

// Load sets the saved state without persisting (startup); Apply starts it.
func (t *TLS) Load(enabled, redirect bool) {
	t.mu.Lock()
	t.enabled, t.redirect = enabled, redirect
	t.mu.Unlock()
}

// Set changes the switches, applies them and saves them. Enabling fails
// (and nothing is saved) when the listener can't start.
func (t *TLS) Set(enabled, redirect bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	redirect = redirect && enabled
	prevE, prevR := t.enabled, t.redirect
	t.enabled, t.redirect = enabled, redirect
	if err := t.applyLocked(); err != nil {
		t.enabled, t.redirect = prevE, prevR
		_ = t.applyLocked()
		return err
	}
	if t.Save != nil {
		return t.Save(enabled, redirect)
	}
	return nil
}

// Apply starts or stops the listener to match the switches.
func (t *TLS) Apply() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.applyLocked()
}

func (t *TLS) applyLocked() error {
	if !t.enabled {
		t.stopLocked()
		t.lastErr = ""
		return nil
	}
	if err := t.refreshLocked(); err != nil {
		t.lastErr = err.Error()
		return err
	}
	if t.srv != nil {
		return nil
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("", t.Port))
	if err != nil {
		t.lastErr = "Port " + t.Port + " is not available: " + err.Error()
		return errors.New(t.lastErr)
	}
	handler := t.Handler
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-Forwarded-Proto", "https")
			handler.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: t.ca.GetCertificate,
			NextProtos:     []string{"http/1.1"},
		},
		// HTTP/1.1 only, like the http listener: WebSocket upgrades go
		// through the reverse proxies unchanged (no h2 extended CONNECT).
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){},
	}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.srv, t.addr, t.lastErr = srv, ln.Addr().String(), ""
	return nil
}

func (t *TLS) stopLocked() {
	if t.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = t.srv.Shutdown(ctx)
		cancel()
		t.srv, t.addr = nil, ""
	}
}

func (t *TLS) refreshLocked() error {
	if t.ca == nil {
		host, _ := os.Hostname()
		ca, err := pkg.OpenLocalCA(t.Dir, host)
		if err != nil {
			return err
		}
		t.ca = ca
	}
	sans := t.SANs
	if sans == nil {
		sans = pkg.LocalSANs
	}
	names, ips := sans()
	_, err := t.ca.Refresh(names, ips, time.Now())
	return err
}

// Refresh reissues the server cert when the box's names/IPs changed or it
// nears expiry; run it periodically. A no-op while off.
func (t *TLS) Refresh() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.enabled {
		return nil
	}
	return t.refreshLocked()
}

// Stop closes the listener (shutdown).
func (t *TLS) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopLocked()
}

// Addr is the running listener's address, "" when not running.
func (t *TLS) Addr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.addr
}

// CA is the local CA once it exists: opened on enable, or from disk.
func (t *TLS) CA() *pkg.LocalCA {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ca == nil {
		// Never created here: only an owner's enable creates files.
		if _, err := os.Stat(t.Dir + "/ca.crt"); err == nil {
			t.ca, _ = pkg.OpenLocalCA(t.Dir, "")
		}
	}
	return t.ca
}

func (t *TLS) Status() TLSStatus {
	ca := t.CA()
	t.mu.Lock()
	defer t.mu.Unlock()
	st := TLSStatus{Enabled: t.enabled, Running: t.srv != nil, Port: t.Port, Redirect: t.redirect, Error: t.lastErr}
	if ca != nil {
		st.Fingerprint = ca.Fingerprint()
		if l := ca.Leaf(); l != nil && l.Leaf != nil {
			st.NotAfter = l.Leaf.NotAfter.UTC().Format(time.RFC3339)
		}
	}
	return st
}

// RedirectPort returns the https port to send a browser to for host (no
// port), when the redirect is on, the listener runs and the cert covers it.
func (t *TLS) RedirectPort(host string) (string, bool) {
	t.mu.Lock()
	on, ca := t.redirect && t.srv != nil, t.ca
	port := t.Port
	if _, p, err := net.SplitHostPort(t.addr); err == nil {
		port = p
	}
	t.mu.Unlock()
	if !on || ca == nil || !ca.Covers(strings.Trim(host, "[]")) {
		return "", false
	}
	return port, true
}
