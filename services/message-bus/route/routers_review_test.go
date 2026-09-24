package route

import (
	"net/http"
	"net/url"
	"testing"

	"gotest.tools/assert"
)

// Review finding (fixed; SubscriptionOriginAllowed): the
// subscription origin guard compares Origin with the Host header. An outer
// reverse proxy on the same box that rewrites Host to its upstream (nginx's
// default `proxy_pass http://127.0.0.1:80;` sends Host: 127.0.0.1 and no
// X-Forwarded-Host) makes every signed-in browser's socket.io handshake a
// 403, although it carries a valid token - widgets, notifications, file
// operation and backup progress all go dead behind such a proxy. The token
// (never a cookie) already stops cross-site pages, so the Origin check adds
// no protection for token-carrying subscriptions.
func TestReviewSubscriptionBehindHostRewritingProxy(t *testing.T) {
	f := newAuthFixture(t)
	req, err := http.NewRequest(http.MethodGet, f.server.URL+"/v2/message_bus/socket.io/?EIO=3&transport=polling&token="+url.QueryEscape(f.token), nil)
	assert.NilError(t, err)
	req.Host = "127.0.0.1" // what nginx sends upstream by default
	req.Header.Set("Origin", "https://nas.example.com")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	resp.Body.Close()
	assert.Equal(t, resp.StatusCode, http.StatusOK)
}
