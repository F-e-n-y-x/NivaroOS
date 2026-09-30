package v1

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	"go.uber.org/zap/zapcore"
)

type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *logBuf) Sync() error                 { return nil }
func (l *logBuf) String() string              { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// Every refused refresh says why in the log, with what the token said
// about itself but never the token; the client gets the reason as
// data.refused (or data.reason for an ended session).
func TestRefreshRefusalIsLoggedWithItsReason(t *testing.T) {
	old := jwt.RevokedSessionsPath
	jwt.RevokedSessionsPath = filepath.Join(t.TempDir(), "revoked_sessions.json")
	t.Cleanup(func() { jwt.RevokedSessionsPath = old })

	f := withAuthRoutes(t)
	setPassword(t, f.alice, "pw for alice")
	loginLimiter.reset("ip:192.168.1.50", "user:alice")
	w := post(f, "/v1/users/login", map[string]string{"username": "alice", "password": "pw for alice"})
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	access, refresh := tokensOf(t, w.Body.Bytes(), false)

	priv, _ := service.MyService.User().GetKeyPair()
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	foreign, _ := jwt.GenerateToken("alice", otherKey, f.alice.Id, "refresh", time.Hour)
	expired, _ := jwt.GenerateToken("alice", priv, f.alice.Id, "refresh", -time.Minute)
	oldGen, _, _ := func() (string, string, error) {
		_, r, err := jwt.IssueSessionTokens("alice", priv, f.alice.Id, "sid-old", 0)
		return r, "", err
	}()

	cases := []struct {
		name, token, want string
		before            func()
	}{
		{"missing", "", refuseMissing, nil},
		{"malformed", "not-a-jwt", refuseMalformed, nil},
		{"bad signature", foreign, refuseBadSignature, nil},
		{"expired", expired, refuseExpired, nil},
		{"access token sent as refresh", access, refuseWrongIssuer, nil},
		{"generation", oldGen, refuseGeneration, func() {
			if _, err := service.MyService.User().RevokeSessions(f.alice.Id, jwt.ReasonSignedOutEverywhere); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.before != nil {
				tc.before()
			}
			buf := &logBuf{}
			logger.LogInitWithWriterSyncers(zapcore.AddSync(buf))
			t.Cleanup(logger.LogInitConsoleOnly)

			w := post(f, "/v1/users/refresh", map[string]string{"refresh_token": tc.token})
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d %s", w.Code, w.Body)
			}
			out := buf.String()
			if !strings.Contains(out, "refresh refused (401)") || !strings.Contains(out, `"reason": "`+tc.want+`"`) {
				t.Fatalf("log doesn't say %q:\n%s", tc.want, out)
			}
			if !strings.Contains(out, `"remote_ip": "192.168.1.50"`) {
				t.Fatalf("log misses the client:\n%s", out)
			}
			for _, tok := range []string{access, refresh, foreign, expired, oldGen} {
				// Not even the signature part of any token.
				if sig := tok[strings.LastIndex(tok, ".")+1:]; strings.Contains(out, sig) {
					t.Fatalf("a token leaked into the log:\n%s", out)
				}
			}
			var res struct {
				Data map[string]string `json:"data"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &res)
			if tc.want == refuseGeneration {
				if res.Data["reason"] != jwt.ReasonSignedOutEverywhere {
					t.Fatalf("an ended session must carry its reason: %s", w.Body)
				}
				if !strings.Contains(out, `"token_generation": 0`) || !strings.Contains(out, `"account_generation": 1`) {
					t.Fatalf("generation refusal must log both generations:\n%s", out)
				}
			} else if res.Data["refused"] != tc.want {
				t.Fatalf("client should get refused=%q: %s", tc.want, w.Body)
			}
		})
	}
}
