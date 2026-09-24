package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func call(h gin.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h(c)
	return w
}

func TestRespondGuardErrorStatusCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{
		{&service.InvalidDeviceError{Msg: "bad"}, http.StatusBadRequest},
		{&service.GuardError{Msg: "system disk"}, http.StatusConflict},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		if !respondGuardError(c, tc.err) || w.Code != tc.code {
			t.Errorf("%T -> %d", tc.err, w.Code)
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if respondGuardError(c, nil) {
		t.Error("nil handled")
	}
}

// Input is rejected before any disk is looked at.
func TestStorageHandlersValidateInput(t *testing.T) {
	cases := []struct {
		h    gin.HandlerFunc
		m    string
		body string
	}{
		{PostAddStorage, "POST", `{"path":"","name":"","format":true}`},
		{PostAddStorage, "POST", `{"path":"/dev/sdb","name":"../../etc","format":true}`},
		{PostAddStorage, "POST", `{"path":"/dev/sdb","name":"a/b","format":false}`},
		{PostAddStorage, "POST", `{"path":123}`}, // wrong type used to panic (unchecked assertion)
		{PostAddStorage, "POST", `{"path":"/dev/sdb","format":"yes"}`},
		{PutFormatStorage, "PUT", `{"path":"/dev/sdb1","volume":"/mnt/x; reboot"}`},
		{PutFormatStorage, "PUT", `{"path":"/dev/sdb1","volume":"/etc"}`},
		{PutFormatStorage, "PUT", `{"path":"","volume":""}`},
	}
	for _, tc := range cases {
		if w := call(tc.h, tc.m, "/v1/storage", tc.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s -> %d %s", tc.m, tc.body, w.Code, w.Body.String())
		}
	}
}

func TestWantSyncAndWaitFor(t *testing.T) {
	for q, want := range map[string]bool{"": false, "?sync=1": true, "?sync=true": true, "?sync=0": false} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/storage"+q, nil)
		if wantSync(c) != want {
			t.Errorf("%q", q)
		}
	}
	start := time.Now()
	if waitFor(1500*time.Millisecond, func() bool { return false }) {
		t.Fatal("waitFor true without condition")
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("waitFor not capped")
	}
	if !waitFor(time.Second, func() bool { return true }) {
		t.Fatal("waitFor false")
	}
}
