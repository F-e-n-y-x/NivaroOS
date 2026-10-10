package v1

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type pairWorld struct {
	f      *fixture
	r      *gin.Engine
	clock  *time.Time
	paired []map[string]string
}

// newPairWorld: fresh code store with a hand-driven clock, fresh limiters,
// the notification captured instead of sent.
func newPairWorld(t *testing.T) *pairWorld {
	t.Helper()
	f := setup(t)
	now := time.Unix(1_800_000_000, 0)
	w := &pairWorld{f: f, clock: &now}
	oldStore, oldIP, oldIssue, oldNotify := pairings, pairLimiter, issueLimiter, notifyPaired
	pairings = &pairStore{now: func() time.Time { return *w.clock }}
	pairLimiter = &loginLimits{buckets: map[string]*rate.Limiter{}, seen: map[string]time.Time{}}
	issueLimiter = &loginLimits{buckets: map[string]*rate.Limiter{}, seen: map[string]time.Time{}}
	notifyPaired = func(u, d, ip string) { w.paired = append(w.paired, pairedProps(u, d, ip)) }
	t.Cleanup(func() { pairings, pairLimiter, issueLimiter, notifyPaired = oldStore, oldIP, oldIssue, oldNotify })
	r := gin.New()
	as := func(c *gin.Context) { c.Request.Header.Set("user_id", c.GetHeader("X-Test-As")) }
	r.POST("/v1/users/current/pair", as, PostPairCode)
	r.POST("/v1/users/pair", PostPairRedeem)
	w.r = r
	return w
}

func (w *pairWorld) post(path, ip string, asID int, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":40000"
	if asID != 0 {
		req.Header.Set("X-Test-As", strconv.Itoa(asID))
	}
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, req)
	return rec
}

func (w *pairWorld) issue(t *testing.T, id int) string {
	t.Helper()
	rec := w.post("/v1/users/current/pair", "192.168.1.2", id, nil)
	var res struct {
		Data struct {
			Code      string `json:"code"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("issue: %d %s", rec.Code, rec.Body)
	}
	if res.Data.ExpiresAt != w.clock.Add(PairCodeLifetime).Unix() {
		t.Fatalf("expires_at %d", res.Data.ExpiresAt)
	}
	return res.Data.Code
}

var redeemIP = 10

func (w *pairWorld) redeem(code string) *httptest.ResponseRecorder {
	redeemIP++ // a new address each time, unless a test wants one
	return w.post("/v1/users/pair", "10.0.0."+strconv.Itoa(redeemIP), 0, map[string]string{"code": code, "device": "Pixel 9"})
}

func reason(rec *httptest.ResponseRecorder) string {
	var res struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	s, _ := res.Data["reason"].(string)
	return s
}

func refused(t *testing.T, rec *httptest.ResponseRecorder, status int, why string) {
	t.Helper()
	if rec.Code != status || reason(rec) != why {
		t.Fatalf("want %d %q, got %d %s", status, why, rec.Code, rec.Body)
	}
}

// The code turns into a normal session of the user who showed it - not
// anyone else - and only once.
func TestPairCodeGivesTheIssuersSessionOnce(t *testing.T) {
	w := newPairWorld(t)
	code := w.issue(t, w.f.bob.Id)
	if len(code) != pairCodeLen || strings.Trim(code, pairAlphabet) != "" {
		t.Fatalf("code %q", code)
	}
	// Typed the way it is shown, in lower case.
	rec := w.redeem(strings.ToLower(code[:4] + "-" + code[4:]))
	if rec.Code != http.StatusOK {
		t.Fatalf("redeem: %d %s", rec.Code, rec.Body)
	}
	var res struct {
		Data struct {
			Token struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			} `json:"token"`
			User struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"user"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	_, pub := service.MyService.User().GetKeyPair()
	for _, tok := range []string{res.Data.Token.AccessToken, res.Data.Token.RefreshToken} {
		c, err := jwt.ParseToken(tok, func() (*ecdsa.PublicKey, error) { return pub, nil })
		if err != nil || c.ID != w.f.bob.Id || c.SessionID == "" {
			t.Fatalf("token for %+v (%v), want bob", c, err)
		}
	}
	if res.Data.User.Username != "bob" || res.Data.User.Password != "" {
		t.Fatalf("user %+v", res.Data.User)
	}
	if len(w.paired) != 1 || w.paired[0]["title"] != "A phone was paired" || !strings.Contains(w.paired[0]["message"], "Pixel 9") {
		t.Fatalf("notification %v", w.paired)
	}
	refused(t, w.redeem(code), http.StatusGone, PairUsed)
	if len(w.paired) != 1 {
		t.Fatal("a refused redeem notified")
	}
}

func TestPairCodeExpires(t *testing.T) {
	w := newPairWorld(t)
	code := w.issue(t, w.f.alice.Id)
	*w.clock = w.clock.Add(PairCodeLifetime)
	refused(t, w.redeem(code), http.StatusGone, PairExpired)
	// Long dead: just wrong.
	*w.clock = w.clock.Add(pairRemember)
	refused(t, w.redeem(code), http.StatusBadRequest, PairInvalid)
}

// A new code replaces the user's previous one; other users' stay.
func TestNewPairCodeReplacesTheOld(t *testing.T) {
	w := newPairWorld(t)
	old := w.issue(t, w.f.alice.Id)
	bobs := w.issue(t, w.f.bob.Id)
	fresh := w.issue(t, w.f.alice.Id)
	refused(t, w.redeem(old), http.StatusGone, PairUsed)
	for _, c := range []string{bobs, fresh} {
		if rec := w.redeem(c); rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
}

func TestPairCodeWrongAndMalformed(t *testing.T) {
	w := newPairWorld(t)
	code := w.issue(t, w.f.alice.Id)
	for _, c := range []string{"", "ABC", code + "X", strings.Repeat("A", pairCodeLen)} {
		refused(t, w.redeem(c), http.StatusBadRequest, PairInvalid)
	}
	if rec := w.redeem(code); rec.Code != http.StatusOK {
		t.Fatalf("the right code after a few wrong ones: %d %s", rec.Code, rec.Body)
	}
}

// Guesses from many addresses burn the live code: after pairMaxFailures
// wrong ones even the right code is refused.
func TestWrongGuessesBurnTheCode(t *testing.T) {
	w := newPairWorld(t)
	code := w.issue(t, w.f.alice.Id)
	wrong := "22222222"
	if wrong == code {
		wrong = "33333333"
	}
	for i := 0; i < pairMaxFailures; i++ {
		refused(t, w.redeem(wrong), http.StatusBadRequest, PairInvalid)
	}
	refused(t, w.redeem(code), http.StatusTooManyRequests, PairTooMany)
	refused(t, w.redeem(code), http.StatusGone, PairUsed)
	// A new code starts clean.
	if rec := w.redeem(w.issue(t, w.f.alice.Id)); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// One address gets 5 tries at once - the right code included.
func TestPairRedeemIsLimitedPerAddress(t *testing.T) {
	w := newPairWorld(t)
	code := w.issue(t, w.f.alice.Id)
	for i := 0; i < 5; i++ {
		refused(t, w.post("/v1/users/pair", "10.9.9.9", 0, map[string]string{"code": "22222223"}), http.StatusBadRequest, PairInvalid)
	}
	refused(t, w.post("/v1/users/pair", "10.9.9.9", 0, map[string]string{"code": code}), http.StatusTooManyRequests, PairTooMany)
	// The limit refused it before it was looked at: still good elsewhere.
	if rec := w.redeem(code); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestPairIssueIsLimitedPerUser(t *testing.T) {
	w := newPairWorld(t)
	for i := 0; i < 5; i++ {
		w.issue(t, w.f.alice.Id)
	}
	refused(t, w.post("/v1/users/current/pair", "192.168.1.2", w.f.alice.Id, nil), http.StatusTooManyRequests, PairTooMany)
	w.issue(t, w.f.bob.Id)
	if rec := w.post("/v1/users/current/pair", "192.168.1.2", 9999, nil); rec.Code == http.StatusOK {
		t.Fatal("a code for an account that doesn't exist")
	}
}

// Only the code's hash is kept.
func TestPairCodeIsStoredHashed(t *testing.T) {
	w := newPairWorld(t)
	code := w.issue(t, w.f.alice.Id)
	for _, c := range pairings.codes {
		if bytes.Contains(c.hash[:], []byte(code)) {
			t.Fatal("plain code in the store")
		}
	}
}
