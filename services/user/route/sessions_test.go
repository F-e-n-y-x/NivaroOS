package route

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/user/codegen/message_bus"
	"github.com/F-e-n-y-x/NivaroOS/services/user/pkg/config"
	v1 "github.com/F-e-n-y-x/NivaroOS/services/user/route/v1"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/user/service/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

type repo struct{ user service.UserService }

func (r repo) Gateway() external.ManagementService          { return nil }
func (r repo) User() service.UserService                    { return r.user }
func (r repo) MessageBus() *message_bus.ClientWithResponses { return nil }
func (r repo) Event() service.EventService                  { return nil }

// world is the real user-service router plus "another service" (any of
// core, app-management, local-storage...: the shared echo JWT middleware),
// both on one throwaway database and one session-state file.
type world struct {
	users, other http.Handler
	state        string
	db           *gorm.DB
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := t.TempDir()
	config.AppInfo.DBPath = filepath.Join(dir, "db")
	config.AppInfo.UserDataPath = filepath.Join(dir, "users")
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "user.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model2.UserDBModel{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if s, err := db.DB(); err == nil {
			s.Close()
		}
	})
	state := filepath.Join(dir, "run", jwt.SessionsFilename)
	service.SetSessionsFile(state)
	oldPath := jwt.SetSessionsPath(state)
	oldRevoked := jwt.RevokedSessionsPath
	jwt.RevokedSessionsPath = filepath.Join(dir, "revoked_sessions.json")
	t.Cleanup(func() {
		service.SetSessionsFile("")
		jwt.SetSessionsPath(oldPath)
		jwt.RevokedSessionsPath = oldRevoked
	})
	us := service.NewUserService(db)
	service.MyService = repo{user: us}
	gin.SetMode(gin.TestMode)

	_, pub := us.GetKeyPair()
	e := echo.New()
	e.Use(jwt.JWT(func() (*ecdsa.PublicKey, error) { return pub, nil }))
	e.GET("/v1/sys/whoami", func(c *echo.Context) error { return c.String(http.StatusOK, c.Request().Header.Get("user_id")) })
	return &world{users: InitRouter(), other: e, state: state, db: db}
}

func (w *world) addUser(t *testing.T, name, pw string) model2.UserDBModel {
	t.Helper()
	u := service.MyService.User().CreateUser(model2.UserDBModel{Username: name, Role: "admin"})
	u.Password = v1.HashPassword(pw)
	service.MyService.User().UpdateUserPassword(u)
	return u
}

var peer = 10

func (w *world) call(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	// A browser, not same-host automation (which may skip the token).
	req.Header.Set("Origin", "http://nivaroos.example.com")
	peer++
	req.RemoteAddr = "192.168.1." + strconv.Itoa(peer) + ":5000"
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type tokens struct{ access, refresh string }

func tokensIn(t *testing.T, rec *httptest.ResponseRecorder) tokens {
	t.Helper()
	var res struct {
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			Token        struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			} `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	if res.Data.Token.AccessToken != "" {
		return tokens{res.Data.Token.AccessToken, res.Data.Token.RefreshToken}
	}
	return tokens{res.Data.AccessToken, res.Data.RefreshToken}
}

func (w *world) login(t *testing.T, name, pw string) tokens {
	t.Helper()
	rec := w.call(t, w.users, http.MethodPost, "/v1/users/login", "", map[string]string{"username": name, "password": pw})
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: %d %s", name, rec.Code, rec.Body)
	}
	return tokensIn(t, rec)
}

func reasonOf(rec *httptest.ResponseRecorder) string {
	var res struct {
		Data map[string]string `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	return res.Data["reason"]
}

// works: the access token is accepted by the user service and by another
// service; ended: refused by both, with reason.
func (w *world) works(t *testing.T, what, access string) {
	t.Helper()
	if rec := w.call(t, w.users, http.MethodGet, "/v1/users/current", access, nil); rec.Code != http.StatusOK {
		t.Fatalf("%s: user service refused it: %d %s", what, rec.Code, rec.Body)
	}
	if rec := w.call(t, w.other, http.MethodGet, "/v1/sys/whoami", access, nil); rec.Code != http.StatusOK {
		t.Fatalf("%s: another service refused it: %d %s", what, rec.Code, rec.Body)
	}
}

func (w *world) ended(t *testing.T, what, access, reason string) {
	t.Helper()
	for name, h := range map[string]http.Handler{"user service": w.users, "another service": w.other} {
		path := "/v1/users/current"
		if name == "another service" {
			path = "/v1/sys/whoami"
		}
		rec := w.call(t, h, http.MethodGet, path, access, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %s still accepts it: %d", what, name, rec.Code)
		}
		if got := reasonOf(rec); got != reason {
			t.Fatalf("%s: %s gave reason %q, want %q (%s)", what, name, got, reason, rec.Body)
		}
	}
}

func (w *world) refresh(t *testing.T, refresh string) *httptest.ResponseRecorder {
	return w.call(t, w.users, http.MethodPost, "/v1/users/refresh", "", map[string]string{"refresh_token": refresh})
}

func sid(t *testing.T, tok string) string {
	t.Helper()
	_, pub := service.MyService.User().GetKeyPair()
	c, err := jwt.ParseToken(tok, func() (*ecdsa.PublicKey, error) { return pub, nil })
	if err != nil {
		t.Fatal(err)
	}
	return c.SessionID
}

// A password change ends every other session of the account at once - in
// every service, not only the user service (the others used to accept the
// old access tokens for their whole 3 h) - and their refresh tokens; the
// session making the change continues, in the same session.
func TestAPasswordChangeEndsOtherSessionsInEveryService(t *testing.T) {
	w := newWorld(t)
	w.addUser(t, "alice", "first password")
	w.addUser(t, "bob", "bobs password")
	web := w.login(t, "alice", "first password")
	phone := w.login(t, "alice", "first password")
	bob := w.login(t, "bob", "bobs password")
	w.works(t, "web", web.access)
	w.works(t, "phone", phone.access)

	rec := w.call(t, w.users, http.MethodPut, "/v1/users/current/password", web.access, map[string]string{"old_password": "first password", "password": "second password"})
	if rec.Code != http.StatusOK {
		t.Fatalf("password change: %d %s", rec.Code, rec.Body)
	}
	web2 := tokensIn(t, rec)
	if sid(t, web2.access) != sid(t, web.access) {
		t.Fatal("the changing session was moved to a new session id")
	}

	w.ended(t, "phone", phone.access, jwt.ReasonPasswordChanged)
	w.ended(t, "web's old token", web.access, jwt.ReasonPasswordChanged)
	if rec := w.refresh(t, phone.refresh); rec.Code != http.StatusUnauthorized || reasonOf(rec) != jwt.ReasonPasswordChanged {
		t.Fatalf("phone refreshed after the change: %d %s", rec.Code, rec.Body)
	}
	w.works(t, "web's new token", web2.access)
	if rec := w.refresh(t, web2.refresh); rec.Code != http.StatusOK {
		t.Fatalf("new refresh token refused: %d %s", rec.Code, rec.Body)
	}
	w.works(t, "another account", bob.access)
	// A fresh sign-in gets the new generation.
	w.works(t, "new sign-in", w.login(t, "alice", "second password").access)
}

// "Sign out everywhere else" ends every other session, keeps the caller's.
func TestSignOutEverywhereElse(t *testing.T) {
	w := newWorld(t)
	w.addUser(t, "dana", "a password")
	here := w.login(t, "dana", "a password")
	laptop := w.login(t, "dana", "a password")
	phone := w.login(t, "dana", "a password")

	rec := w.call(t, w.users, http.MethodDelete, "/v1/users/current/sessions", here.access, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign out everywhere: %d %s", rec.Code, rec.Body)
	}
	here2 := tokensIn(t, rec)
	w.ended(t, "laptop", laptop.access, jwt.ReasonSignedOutEverywhere)
	w.ended(t, "phone", phone.access, jwt.ReasonSignedOutEverywhere)
	if rec := w.refresh(t, laptop.refresh); rec.Code != http.StatusUnauthorized {
		t.Fatalf("laptop refreshed: %d", rec.Code)
	}
	w.works(t, "this session", here2.access)
	if sid(t, here2.access) != sid(t, here.access) {
		t.Fatal("this session got a new id")
	}

	// Twice in a row: the generation keeps going up, persisted.
	rec = w.call(t, w.users, http.MethodDelete, "/v1/users/current/sessions", here2.access, nil)
	here3 := tokensIn(t, rec)
	w.ended(t, "previous tokens", here2.access, jwt.ReasonSignedOutEverywhere)
	w.works(t, "latest tokens", here3.access)
	var gen int64
	w.db.Model(&model2.UserDBModel{}).Where("username = ?", "dana").Pluck("token_generation", &gen)
	if gen != 2 {
		t.Fatalf("token_generation = %d, want 2", gen)
	}
}

// A deleted account is signed out everywhere at once; and SQLite handing
// its id to the next new account doesn't hand that account its tokens.
func TestADeletedAccountIsSignedOutEverywhere(t *testing.T) {
	w := newWorld(t)
	w.addUser(t, "erin", "a password")
	bob := w.addUser(t, "bob", "bobs password")
	admin := w.login(t, "erin", "a password")
	bobs := w.login(t, "bob", "bobs password")
	w.works(t, "bob", bobs.access)

	if rec := w.call(t, w.users, http.MethodDelete, "/v1/users/"+strconv.Itoa(bob.Id), admin.access, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	w.ended(t, "deleted bob", bobs.access, jwt.ReasonAccountDeleted)
	if rec := w.refresh(t, bobs.refresh); rec.Code != http.StatusUnauthorized || reasonOf(rec) != jwt.ReasonAccountDeleted {
		t.Fatalf("deleted bob refreshed: %d %s", rec.Code, rec.Body)
	}

	// bob's id comes back for the next account.
	priv, _ := service.MyService.User().GetKeyPair()
	oldAccess, _, _ := jwt.IssueSessionTokens("bob", priv, bob.Id, "", 0)
	oldIat := func() int64 {
		_, pub := service.MyService.User().GetKeyPair()
		c, _ := jwt.ParseToken(oldAccess, func() (*ecdsa.PublicKey, error) { return pub, nil })
		return c.IssuedAt.Unix()
	}()
	carol := service.MyService.User().CreateUser(model2.UserDBModel{Username: "carol", Role: "admin", TokensValidAfter: oldIat + 1})
	if carol.Id != bob.Id {
		t.Skipf("SQLite didn't reuse the id (%d vs %d)", carol.Id, bob.Id)
	}
	w.ended(t, "old bob token on carol's id", oldAccess, "")
}

// The published state is what every service reads: every account, its
// generation; rewritten on start (republish) and after each change.
func TestSessionStateIsPublished(t *testing.T) {
	w := newWorld(t)
	a := w.addUser(t, "alice", "a password")
	if _, err := service.MyService.User().RevokeSessions(a.Id, jwt.ReasonSignedOutEverywhere); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(w.state)
	if err != nil {
		t.Fatal(err)
	}
	var st jwt.SessionState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	u, ok := st.Users[strconv.Itoa(a.Id)]
	if !ok || u.Generation != 1 || u.Reason != jwt.ReasonSignedOutEverywhere || u.ValidAfter == 0 {
		t.Fatalf("published %+v", st)
	}
	fi, _ := os.Stat(w.state)
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("state file mode %o", fi.Mode().Perm())
	}
	// Lost (e.g. /run cleared) - the next publish brings it back.
	os.Remove(w.state)
	if err := service.MyService.User().PublishSessions(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.state); err != nil {
		t.Fatal("not republished")
	}
	if _, err := service.MyService.User().RevokeSessions(9999, "x"); err == nil {
		t.Fatal("revoking a missing account succeeded")
	}
}
