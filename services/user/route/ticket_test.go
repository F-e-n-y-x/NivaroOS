package route

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// L5: media links carry a short-lived ticket (?mt=) for one URL instead of
// the access token.
func TestMediaTicketForTheAvatarOnly(t *testing.T) {
	w := newWorld(t)
	w.addUser(t, "owner", "right password")
	tok := w.login(t, "owner", "right password")

	issue := func(auth string, body any) (int, string) {
		rec := w.call(t, w.users, http.MethodPost, "/v1/users/current/ticket", auth, body)
		var res struct {
			Data struct{ Ticket string } `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		return rec.Code, res.Data.Ticket
	}
	if code, _ := issue(tok.access, map[string]string{"kind": "url", "target": "/v1/users/current"}); code == http.StatusOK {
		t.Fatal("a ticket for a non-media URL was issued")
	}
	code, mt := issue(tok.access, map[string]string{"kind": "url", "target": "/v1/users/avatar"})
	if code != http.StatusOK || mt == "" {
		t.Fatalf("issue: %d", code)
	}
	q := "?mt=" + url.QueryEscape(mt)
	if rec := w.call(t, w.users, http.MethodGet, "/v1/users/avatar"+q+"&username=owner", "", nil); rec.Code == http.StatusUnauthorized {
		t.Fatalf("avatar with its ticket: %d %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/v1/users/current" + q, "/v1/users/name" + q} {
		if rec := w.call(t, w.users, http.MethodGet, p, "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s: %d, want 401", p, rec.Code)
		}
	}
	if rec := w.call(t, w.other, http.MethodGet, "/v1/sys/whoami"+q, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("another service took the ticket: %d", rec.Code)
	}
	if code, _ := issue(mt, map[string]string{"kind": "url", "target": "/v1/users/avatar"}); code == http.StatusOK {
		t.Error("a ticket issued a ticket")
	}
	// Signing out everywhere ends the session's tickets.
	if rec := w.call(t, w.users, http.MethodDelete, "/v1/users/current/sessions", tok.access, nil); rec.Code != http.StatusOK {
		t.Fatalf("sign out everywhere: %d %s", rec.Code, rec.Body)
	}
	if rec := w.call(t, w.users, http.MethodGet, "/v1/users/avatar"+q+"&username=owner", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("ticket outlived its session: %d", rec.Code)
	}
}
