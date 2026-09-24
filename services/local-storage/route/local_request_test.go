package route

import (
	"net/http/httptest"
	"testing"
)

func TestIsTrustedLocalRequest(t *testing.T) {
	req := func(remote string, headers map[string]string) bool {
		r := httptest.NewRequest("GET", "/v2/local_storage/mount", nil)
		r.RemoteAddr = remote
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return IsTrustedLocalRequest(r)
	}
	if !req("127.0.0.1:5555", nil) || !req("[::1]:5555", nil) {
		t.Fatal("plain local process not trusted")
	}
	cases := []struct {
		remote  string
		headers map[string]string
	}{
		{"203.0.113.9:1234", map[string]string{"X-Forwarded-For": "127.0.0.1"}}, // the old bypass
		{"203.0.113.9:1234", map[string]string{"X-Real-IP": "127.0.0.1"}},
		{"203.0.113.9:1234", nil},
		{"127.0.0.1:1", map[string]string{"X-Forwarded-For": "203.0.113.9"}}, // via the gateway
		{"127.0.0.1:1", map[string]string{"X-Real-IP": "10.0.0.2"}},
		{"127.0.0.1:1", map[string]string{"Forwarded": "for=10.0.0.2"}},
		{"127.0.0.1:1", map[string]string{"Origin": "http://evil.example"}}, // browser -> localhost
		{"127.0.0.1:1", map[string]string{"Sec-Fetch-Site": "cross-site"}},
		{"garbage", nil},
	}
	for _, c := range cases {
		if req(c.remote, c.headers) {
			t.Errorf("trusted: %s %v", c.remote, c.headers)
		}
	}
}
