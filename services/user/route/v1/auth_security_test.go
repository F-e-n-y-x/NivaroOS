package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/user/pkg/utils/encryption"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/user/service/model"
	"github.com/gin-gonic/gin"
)

func post(f *fixture, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.50:5555"
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func withAuthRoutes(t *testing.T) *fixture {
	f := setup(t)
	f.router.POST("/v1/users/login", PostUserLogin)
	f.router.POST("/v1/users/refresh", PostUserRefreshToken)
	f.router.PUT("/v1/users/current/password", func(c *gin.Context) { c.Request.Header.Set("user_id", c.GetHeader("X-Test-As")) }, PutUserPassword)
	return f
}

func setPassword(t *testing.T, u model2.UserDBModel, pw string) {
	t.Helper()
	u.Password = hashPassword(pw)
	service.MyService.User().UpdateUserPassword(u)
}

// Passwords were unsalted MD5. Old hashes keep working and are upgraded to
// bcrypt the first time they're used.
func TestAnMD5PasswordStillLogsInAndIsUpgraded(t *testing.T) {
	f := withAuthRoutes(t)
	u := f.alice
	u.Password = encryption.GetMD5ByStr("correct horse")
	service.MyService.User().UpdateUserPassword(u)

	if w := post(f, "/v1/users/login", map[string]string{"username": "alice", "password": "correct horse"}); w.Code != http.StatusOK {
		t.Fatalf("login with the old hash failed: %d %s", w.Code, w.Body)
	}
	stored := service.MyService.User().GetUserAllInfoById(strconv.Itoa(u.Id)).Password
	if len(stored) < 4 || stored[:2] != "$2" {
		t.Fatalf("hash not upgraded: %q", stored)
	}
	if w := post(f, "/v1/users/login", map[string]string{"username": "alice", "password": "correct horse"}); w.Code != http.StatusOK {
		t.Fatalf("login with the upgraded hash failed: %d", w.Code)
	}
	if w := post(f, "/v1/users/login", map[string]string{"username": "alice", "password": "wrong"}); w.Code == http.StatusOK {
		t.Fatal("wrong password accepted")
	}
}

// Changing the password didn't end other sessions: their refresh tokens
// kept working for 7 days.
func TestAPasswordChangeEndsOtherSessions(t *testing.T) {
	f := withAuthRoutes(t)
	setPassword(t, f.alice, "old password 1")
	priv, _ := service.MyService.User().GetKeyPair()
	oldRefresh, _ := jwt.GetRefreshToken("alice", priv, f.alice.Id)
	time.Sleep(1100 * time.Millisecond) // tokens carry whole-second times

	w := f.do(t, http.MethodPut, "/v1/users/current/password", f.alice.Id, map[string]string{"old_password": "old password 1", "password": "new password 2"})
	if w.Code != http.StatusOK {
		t.Fatalf("password change: %d %s", w.Code, w.Body)
	}
	if w := post(f, "/v1/users/refresh", map[string]string{"refresh_token": oldRefresh}); w.Code == http.StatusOK {
		t.Fatal("a session from before the password change could still refresh")
	}
}

func TestADeletedUserCantRefresh(t *testing.T) {
	f := withAuthRoutes(t)
	priv, _ := service.MyService.User().GetKeyPair()
	tok, _ := jwt.GetRefreshToken("bob", priv, f.bob.Id)
	service.MyService.User().DeleteUserById(strconv.Itoa(f.bob.Id))
	if w := post(f, "/v1/users/refresh", map[string]string{"refresh_token": tok}); w.Code == http.StatusOK {
		t.Fatal("deleted user got new tokens")
	}
}

// One global limiter: five bad attempts from anyone locked the owner out.
func TestBadLoginsFromOneClientDontLockOutAnother(t *testing.T) {
	f := withAuthRoutes(t)
	setPassword(t, f.alice, "right password")
	for i := 0; i < 8; i++ {
		post(f, "/v1/users/login", map[string]string{"username": "bob", "password": "guess"})
	}
	raw, _ := json.Marshal(map[string]string{"username": "alice", "password": "right password"})
	req := httptest.NewRequest(http.MethodPost, "/v1/users/login", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.77:4444" // someone else
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("alice locked out by another client's failures: %d %s", w.Code, w.Body)
	}
}
