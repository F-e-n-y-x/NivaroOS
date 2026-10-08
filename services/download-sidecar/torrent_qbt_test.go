package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A fake qBittorrent WebUI: cookie login, plus the 5.2 API key (and its
// 204 login reply) when hasKey.
func fakeQbt(hasKey bool) (*httptest.Server, *string, *int) {
	key, logins := "", 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(403) // 5.2: the auth scope refuses key requests
			return
		}
		r.ParseForm()
		if r.Form.Get("password") != "p" {
			if hasKey {
				w.WriteHeader(401)
			} else {
				w.Write([]byte("Fails."))
			}
			return
		}
		logins++
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s", Path: "/"})
		if hasKey {
			w.WriteHeader(204)
		} else {
			w.Write([]byte("Ok."))
		}
	})
	authed := func(r *http.Request) bool {
		if a := r.Header.Get("Authorization"); a != "" {
			return hasKey && key != "" && a == "Bearer "+key // a sent key hides the cookie
		}
		c, err := r.Cookie("SID")
		return err == nil && c.Value == "s"
	}
	mux.HandleFunc("/api/v2/", func(w http.ResponseWriter, r *http.Request) {
		if !authed(r) {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/api/v2/app/preferences":
			if hasKey {
				w.Write([]byte(`{"web_ui_api_key":"` + key + `"}`))
			} else {
				w.Write([]byte(`{}`))
			}
		case "/api/v2/app/rotateAPIKey":
			key = "qbt_0123456789012345678901234567"
			w.Write([]byte(`{"apiKey":"` + key + `"}`))
		case "/api/v2/torrents/info":
			w.Write([]byte(`[]`))
		default:
			w.WriteHeader(404)
		}
	})
	return httptest.NewServer(mux), &key, &logins
}

func TestQbtAPIKeyAndCookieFallback(t *testing.T) {
	ctx := context.Background()

	srv, key, logins := fakeQbt(true)
	defer srv.Close()
	q := newQbtEngine(srv.URL, "nivaroos", "p", true, t.TempDir())
	if err := q.login(ctx); err != nil {
		t.Fatalf("5.2 login (204): %v", err)
	}
	if q.key() == "" || q.key() != *key {
		t.Fatalf("5.2 should hand out an API key, got %q", q.key())
	}
	q.hc.Jar = nil // key only: no cookie to lean on
	if _, err := q.List(ctx); err != nil {
		t.Fatalf("API key call: %v", err)
	}

	// A stale key: one fresh login, which picks up the current key.
	*key = "qbt_rotated_elsewhere_000000000"
	q2 := newQbtEngine(srv.URL, "nivaroos", "", true, t.TempDir())
	os.WriteFile(filepath.Join(q2.dataDir, "qbt-webui.secret"), []byte("p\n"), 0o600) // restarted, engine still up
	q2.setKey("qbt_stale")
	before := *logins
	if _, err := q2.List(ctx); err != nil || q2.key() != *key || *logins != before+1 {
		t.Fatalf("stale key: err=%v key=%q logins=%d", err, q2.key(), *logins-before)
	}

	if err := newQbtEngine(srv.URL, "nivaroos", "wrong", true, t.TempDir()).login(ctx); err != errQbtAuth {
		t.Fatalf("5.2 bad password: %v", err)
	}

	old, _, oldLogins := fakeQbt(false)
	defer old.Close()
	q3 := newQbtEngine(old.URL, "nivaroos", "p", true, t.TempDir())
	if _, err := q3.List(ctx); err != nil || *oldLogins != 1 || q3.key() != "" {
		t.Fatalf("5.1 cookie: err=%v logins=%d key=%q", err, *oldLogins, q3.key())
	}
	if err := newQbtEngine(old.URL, "nivaroos", "wrong", true, t.TempDir()).login(ctx); err != errQbtAuth {
		t.Fatalf("5.1 bad password: %v", err)
	}
}
