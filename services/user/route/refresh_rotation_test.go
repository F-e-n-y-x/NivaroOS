package route

import (
	"net/http"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
)

// M5: a refresh used to leave the old refresh token valid for its 7 days,
// and each new one was good for another 7: a stolen copy renewed forever.
// Now each refresh exchanges it, and an exchanged one coming back ends the
// session everywhere.
func TestAReusedRefreshTokenEndsTheSession(t *testing.T) {
	w := newWorld(t)
	w.addUser(t, "alice", "first password")
	t0 := w.login(t, "alice", "first password")
	other := w.login(t, "alice", "first password") // another device

	rec := w.refresh(t, t0.refresh)
	if rec.Code != http.StatusOK {
		t.Fatalf("first refresh: %d %s", rec.Code, rec.Body)
	}
	t1 := tokensIn(t, rec)
	rec = w.refresh(t, t1.refresh)
	if rec.Code != http.StatusOK {
		t.Fatalf("second refresh: %d %s", rec.Code, rec.Body)
	}
	t2 := tokensIn(t, rec)
	w.works(t, "the session's newest access token", t2.access)

	// The thief's copy of the first token comes back.
	if rec := w.refresh(t, t0.refresh); rec.Code != http.StatusUnauthorized || reasonOf(rec) != jwt.ReasonRefreshReused {
		t.Fatalf("exchanged refresh token accepted again: %d %s", rec.Code, rec.Body)
	}
	w.ended(t, "the session's newest access token", t2.access, jwt.ReasonRefreshReused)
	if rec := w.refresh(t, t2.refresh); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the session's newest refresh token still works: %d", rec.Code)
	}
	w.works(t, "another device's session", other.access)
	if rec := w.refresh(t, other.refresh); rec.Code != http.StatusOK {
		t.Fatalf("another device's refresh: %d %s", rec.Code, rec.Body)
	}
}

// Two tabs (or app engines) refreshing with the same token at once, or a
// retry after a lost answer, is not theft: for a short while the token just
// exchanged still gets the session's current one, and the session goes on.
func TestARacingRefreshIsNotReuse(t *testing.T) {
	w := newWorld(t)
	w.addUser(t, "alice", "first password")
	t0 := w.login(t, "alice", "first password")
	a := w.refresh(t, t0.refresh)
	b := w.refresh(t, t0.refresh)
	if a.Code != http.StatusOK || b.Code != http.StatusOK {
		t.Fatalf("racing refreshes: %d %s / %d %s", a.Code, a.Body, b.Code, b.Body)
	}
	ta, tb := tokensIn(t, a), tokensIn(t, b)
	w.works(t, "tab A", ta.access)
	w.works(t, "tab B", tb.access)
	if rec := w.refresh(t, tb.refresh); rec.Code != http.StatusOK {
		t.Fatalf("tab B's refresh token: %d %s", rec.Code, rec.Body)
	}
}
