package route

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// `tailscale serve` relays https://box.tail1.ts.net to the gateway from
// 127.0.0.1 with X-Forwarded-*: the service behind must see the browser's
// host and scheme and the tailnet client's IP, and never a local-automation
// mark (that would skip JWT auth for every tailnet device).
func TestTailscaleServeRelayThroughGateway(t *testing.T) {
	got := make(chan http.Header, 1)
	var host string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		got <- r.Header.Clone()
		_, _ = io.WriteString(w, "ok")
	}))
	defer backend.Close()
	old := vmSidecarProxy
	vmSidecarProxy = newVMSidecarProxy(strings.TrimPrefix(backend.URL, "http://"))
	defer func() { vmSidecarProxy = old }()
	gw := httptest.NewServer(NewGatewayRoute(nil).GetRoute()) // listens on 127.0.0.1, like tailscaled dials
	defer gw.Close()

	req, _ := http.NewRequest(http.MethodGet, gw.URL+"/v1/vm-sidecar/vms", nil)
	req.Host = "box.tail1.ts.net"
	for k, v := range map[string]string{
		"X-Forwarded-For": "100.101.102.103", "X-Forwarded-Proto": "https", "X-Forwarded-Host": "box.tail1.ts.net",
		"Tailscale-User-Login": "owner@example.com", "X-Nivaroos-Local-Automation": "1",
	} {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	h := <-got
	if res.StatusCode != 200 || host != "box.tail1.ts.net" || h.Get("X-Forwarded-Proto") != "https" ||
		h.Get("X-Forwarded-Host") != "box.tail1.ts.net" || !strings.HasPrefix(h.Get("X-Forwarded-For"), "100.101.102.103") ||
		h.Get("X-Nivaroos-Local-Automation") != "" {
		t.Fatalf("status %d, host %q, backend headers %v", res.StatusCode, host, h)
	}
}
