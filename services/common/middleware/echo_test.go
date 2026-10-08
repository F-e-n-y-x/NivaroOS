package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

// Pins what echo v4 sent so the v5 migration stays invisible to clients:
// CORS headers and the {"message": ...} error body.
func TestCorsAndErrorShape(t *testing.T) {
	e := echo.New()
	e.Use(Cors())
	e.GET("/bad", func(c *echo.Context) error { return echo.NewHTTPError(http.StatusBadRequest, "nope") })
	e.GET("/boom", func(c *echo.Context) error { return http.ErrAbortHandler })

	do := func(method, path string, hdr map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		return w
	}

	w := do(http.MethodOptions, "/bad", map[string]string{"Origin": "http://x.example", "Access-Control-Request-Method": "GET"})
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "*" ||
		w.Header().Get("Access-Control-Allow-Credentials") != "true" ||
		w.Header().Get("Access-Control-Allow-Methods") != "POST,GET,OPTIONS,PUT,DELETE" ||
		w.Header().Get("Access-Control-Max-Age") != "172800" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}

	for path, want := range map[string]struct {
		code int
		body string
	}{
		"/bad":     {400, `{"message":"nope"}`},
		"/boom":    {500, `{"message":"Internal Server Error"}`},
		"/missing": {404, `{"message":"Not Found"}`},
	} {
		w := do(http.MethodGet, path, map[string]string{"Origin": "http://x.example"})
		if w.Code != want.code || strings.TrimSpace(w.Body.String()) != want.body || w.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("%s: %d %q %v", path, w.Code, w.Body.String(), w.Header())
		}
	}
}
