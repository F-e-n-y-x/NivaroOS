package route

import (
	"net"
	"net/http/httptest"
	"testing"
)

// cloudflared dials the gateway from the box itself, so every internet
// client looked like 192.168.10.10 and shared one login-limit bucket. Its
// Cf-Connecting-Ip is the real client, but only a same-host peer may set it.
func TestCfConnectingIPTrustedOnlyFromThisHost(t *testing.T) {
	peers := map[string]string{"127.0.0.1:4000": "203.0.113.9", "[::1]:4000": "203.0.113.9", "198.51.100.7:4000": ""}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			peers[n.IP.String()+":4000"] = "203.0.113.9" // the LAN address cloudflared dials
			break
		}
	}
	for peer, want := range peers {
		r := httptest.NewRequest("GET", "/v1/users/login", nil)
		r.RemoteAddr = peer
		r.Header.Set("Cf-Connecting-Ip", "203.0.113.9")
		r.Header.Set("X-Forwarded-For", "10.0.0.1")
		rewriteRequestSourceIP(r)
		if got := r.Header.Get("X-Forwarded-For"); got != want {
			t.Errorf("peer %s: X-Forwarded-For %q, want %q", peer, got, want)
		}
	}
}
