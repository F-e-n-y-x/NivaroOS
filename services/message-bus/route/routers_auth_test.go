package route

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/repository"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/service"
	"github.com/gobwas/ws"
	"go.uber.org/zap/zapcore"
	"gotest.tools/assert"
)

type authFixture struct {
	server       *httptest.Server
	handler      http.Handler
	token        string
	refreshToken string
}

// newAuthFixture serves the real message-bus router (JWT, origin guard,
// CORS, OpenAPI validation, socket.io) on a loopback test server, with a
// throwaway key pair standing in for the user service's JWKS.
func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	logger.LogInitWithWriterSyncers(zapcore.AddSync(io.Discard))

	privateKey, publicKey, err := jwt.GenerateKeyPair()
	assert.NilError(t, err)
	token, err := jwt.GetAccessToken("admin", privateKey, 1)
	assert.NilError(t, err)
	refreshToken, err := jwt.GetRefreshToken("admin", privateKey, 1)
	assert.NilError(t, err)

	repo, err := repository.NewDatabaseRepositoryInMemory()
	assert.NilError(t, err)
	t.Cleanup(func() { repo.Close() })

	services := service.NewServices(&repo)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	services.Start(&ctx)

	swagger, err := codegen.GetSwagger()
	assert.NilError(t, err)

	handler, err := newAPIRouter(swagger, &services, func() (*ecdsa.PublicKey, error) { return publicKey, nil })
	assert.NilError(t, err)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	f := &authFixture{server: server, handler: handler, token: token, refreshToken: refreshToken}

	// Register event and action types as same-host automation does.
	f.mustPost(t, "/v2/message_bus/event_type", `[{"sourceID":"Foo","name":"Bar","propertyTypeList":[]}]`)
	f.mustPost(t, "/v2/message_bus/action_type", `[{"sourceID":"Foo","name":"Baz","propertyTypeList":[]}]`)

	return f
}

func (f *authFixture) mustPost(t *testing.T, path, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.server.URL+path, bytes.NewBufferString(body))
	assert.NilError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	assert.Equal(t, resp.StatusCode, http.StatusOK, path+": "+string(b))
}

func (f *authFixture) wsURL(path string) string {
	return "ws" + strings.TrimPrefix(f.server.URL, "http") + path
}

// dial opens a WebSocket and returns the handshake status (101 on success).
func (f *authFixture) dial(t *testing.T, path string, header http.Header) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dialer := ws.Dialer{Header: ws.HandshakeHeaderHTTP(header)}
	conn, _, _, err := dialer.Dial(ctx, f.wsURL(path))
	if err == nil {
		conn.Close()
		return http.StatusSwitchingProtocols
	}
	var status ws.StatusError
	if errors.As(err, &status) {
		return int(status)
	}
	t.Fatalf("dial %s: %v", path, err)
	return 0
}

// viaGateway is what the gateway forwards for a remote (LAN / mobile app)
// client: loopback peer, X-Forwarded-For set, no local-automation mark.
func viaGateway(extra ...string) http.Header {
	h := http.Header{"X-Forwarded-For": {"203.0.113.9"}}
	for i := 0; i+1 < len(extra); i += 2 {
		h.Set(extra[i], extra[i+1])
	}
	return h
}

func TestEventWebSocketAuth(t *testing.T) {
	f := newAuthFixture(t)
	const path = "/v2/message_bus/event/Foo"

	cases := []struct {
		name   string
		path   string
		header http.Header
		want   int
	}{
		{"via gateway, no token", path, viaGateway(), http.StatusUnauthorized},
		{"via gateway, bad token", path + "?token=not-a-jwt", viaGateway(), http.StatusUnauthorized},
		{"via gateway, refresh token is not an access token", path + "?token=" + url.QueryEscape(f.refreshToken), viaGateway(), http.StatusUnauthorized},
		{"via gateway, ?token=", path + "?token=" + url.QueryEscape(f.token), viaGateway(), http.StatusSwitchingProtocols},
		{"via gateway, Authorization header", path, viaGateway("Authorization", f.token), http.StatusSwitchingProtocols},
		{"via gateway, Authorization Bearer", path, viaGateway("Authorization", "Bearer "+f.token), http.StatusSwitchingProtocols},
		{"same-host automation, no token", path, http.Header{}, http.StatusSwitchingProtocols},
		{"same-host automation vouched by the gateway", path, viaGateway(nivaroos_middleware.LocalAutomationHeader, "1"), http.StatusSwitchingProtocols},
		{"browser tab on this host, no token", path, http.Header{"Origin": {"http://127.0.0.1"}}, http.StatusUnauthorized},
		{"browser tab on this host, token", path + "?token=" + url.QueryEscape(f.token), http.Header{"Origin": {"http://127.0.0.1"}}, http.StatusSwitchingProtocols},
		{"cross-site page, no token", path, http.Header{"Origin": {"https://evil.example"}}, http.StatusForbidden},
		// The token is the protection (never a cookie, so a cross-site page
		// can't have one); Origin vs Host is not compared once one is sent,
		// or a Host-rewriting outer proxy breaks every browser.
		{"foreign Origin (Host-rewriting proxy), token", path + "?token=" + url.QueryEscape(f.token), http.Header{"Origin": {"https://nas.example.com"}, "X-Forwarded-For": {"203.0.113.9"}}, http.StatusSwitchingProtocols},
		{"foreign Origin, bad token", path + "?token=not-a-jwt", http.Header{"Origin": {"https://evil.example"}}, http.StatusUnauthorized},
		{"action stream via gateway, no token", "/v2/message_bus/action/Foo", viaGateway(), http.StatusUnauthorized},
		{"action stream via gateway, token", "/v2/message_bus/action/Foo?token=" + url.QueryEscape(f.token), viaGateway(), http.StatusSwitchingProtocols},
		{"socket.io websocket via gateway, no token", "/v2/message_bus/socket.io/?EIO=3&transport=websocket", viaGateway(), http.StatusUnauthorized},
		{"socket.io websocket via gateway, token", "/v2/message_bus/socket.io/?EIO=3&transport=websocket&token=" + url.QueryEscape(f.token), viaGateway(), http.StatusSwitchingProtocols},
		{"socket.io websocket, cross-site page, no token", "/v2/message_bus/socket.io/?EIO=3&transport=websocket", http.Header{"Origin": {"https://evil.example"}}, http.StatusForbidden},
		{"socket.io websocket, foreign Origin (Host-rewriting proxy), token", "/v2/message_bus/socket.io/?EIO=3&transport=websocket&token=" + url.QueryEscape(f.token), http.Header{"Origin": {"https://nas.example.com"}, "X-Forwarded-For": {"203.0.113.9"}}, http.StatusSwitchingProtocols},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, f.dial(t, tc.path, tc.header), tc.want)
		})
	}
}

// A client that isn't on this host at all (no gateway in between) needs a
// token too.
func TestEventWebSocketRemotePeerNeedsToken(t *testing.T) {
	f := newAuthFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/v2/message_bus/event/Foo", nil)
	req.RemoteAddr = "192.0.2.10:5555"
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	rec := httptest.NewRecorder()

	f.handler.ServeHTTP(rec, req)
	assert.Equal(t, rec.Code, http.StatusUnauthorized)
}

func TestSocketIOPollingAuth(t *testing.T) {
	f := newAuthFixture(t)

	get := func(query string, header http.Header) *http.Response {
		req, err := http.NewRequest(http.MethodGet, f.server.URL+"/v2/message_bus/socket.io/?EIO=3&transport=polling"+query, nil)
		assert.NilError(t, err)
		for k, v := range header {
			req.Header[k] = v
		}
		resp, err := http.DefaultClient.Do(req)
		assert.NilError(t, err)
		resp.Body.Close()
		return resp
	}

	assert.Equal(t, get("", viaGateway()).StatusCode, http.StatusUnauthorized)
	assert.Equal(t, get("&token=garbage", viaGateway()).StatusCode, http.StatusUnauthorized)
	assert.Equal(t, get("&token="+url.QueryEscape(f.token), viaGateway()).StatusCode, http.StatusOK)
	assert.Equal(t, get("", http.Header{}).StatusCode, http.StatusOK) // same-host automation

	crossSite := get("", http.Header{"Origin": {"https://evil.example"}})
	assert.Equal(t, crossSite.StatusCode, http.StatusForbidden)
	// With a token the JWT decides; a cross-site page still gets no CORS
	// headers, so its browser can't read the answer.
	withToken := get("&token="+url.QueryEscape(f.token), http.Header{"Origin": {"https://evil.example"}})
	assert.Equal(t, withToken.Header.Get("Access-Control-Allow-Origin"), "")
	assert.Equal(t, get("&token=garbage", http.Header{"Origin": {"https://evil.example"}}).StatusCode, http.StatusUnauthorized)

	sameHost := get("&token="+url.QueryEscape(f.token), http.Header{"Origin": {"http://127.0.0.1:8080"}})
	assert.Equal(t, sameHost.StatusCode, http.StatusOK)
	assert.Equal(t, sameHost.Header.Get("Access-Control-Allow-Origin"), "http://127.0.0.1:8080")
}

func TestCORSOnlyForSameHost(t *testing.T) {
	f := newAuthFixture(t)

	get := func(origin string) *http.Response {
		req, err := http.NewRequest(http.MethodGet, f.server.URL+"/v2/message_bus/event_type", nil)
		assert.NilError(t, err)
		req.Header.Set("Origin", origin)
		req.Header.Set("Authorization", f.token)
		resp, err := http.DefaultClient.Do(req)
		assert.NilError(t, err)
		resp.Body.Close()
		return resp
	}

	assert.Equal(t, get("https://evil.example").Header.Get("Access-Control-Allow-Origin"), "")
	assert.Assert(t, get("http://127.0.0.1").Header.Get("Access-Control-Allow-Origin") != "")
}
