package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/labstack/echo/v5"
)

// L5: a folder download (GET /v1/batch?ticket=) needed the access token in
// the URL as well. The ticket is the credential now - for the user who
// made it, only for that GET.
func TestDownloadTicketAuthorizesItsDownload(t *testing.T) {
	e := echo.New()
	post := httptest.NewRequest(http.MethodPost, "/v1/batch/download", strings.NewReader(`{"files":["/DATA/a"]}`))
	post.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c := e.NewContext(post, w)
	c.Set("user", &jwt.Claims{Username: "bob", ID: 3})
	if err := PostDownloadTicket(c); err != nil {
		t.Fatal(err)
	}
	var res struct{ Data string }
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.Data == "" {
		t.Fatalf("no ticket: %s", w.Body.String())
	}

	check := func(method, target string) bool {
		return DownloadTicketAuth(e.NewContext(httptest.NewRequest(method, target, nil), httptest.NewRecorder()))
	}
	if !check(http.MethodGet, "/v1/batch?ticket="+res.Data) {
		t.Error("the ticket's own download was refused")
	}
	for _, bad := range []struct{ m, u string }{
		{http.MethodGet, "/v1/batch?ticket=nope"},
		{http.MethodGet, "/v1/batch"},
		{http.MethodDelete, "/v1/batch?ticket=" + res.Data},
		{http.MethodGet, "/v1/file?ticket=" + res.Data},
	} {
		if check(bad.m, bad.u) {
			t.Errorf("%s %s skipped the token", bad.m, bad.u)
		}
	}

	// A ticket made without a user (same-host automation) is no credential.
	post = httptest.NewRequest(http.MethodPost, "/v1/batch/download", strings.NewReader(`{"files":["/DATA/a"]}`))
	post.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	c = e.NewContext(post, w)
	_ = PostDownloadTicket(c)
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if check(http.MethodGet, "/v1/batch?ticket="+res.Data) {
		t.Error("an anonymous ticket skipped the token")
	}
}
