package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

// L1: the echo services answered "*" to every site; only this same host
// (any port) gets CORS headers now.
func TestEchoCorsOnlyForThisHost(t *testing.T) {
	e := echo.New()
	e.Use(Cors())
	e.GET("/x", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })
	for origin, want := range map[string]string{"https://evil.example": "", "http://nas.local:5173": "http://nas.local:5173"} {
		req := httptest.NewRequest(http.MethodGet, "http://nas.local/x", nil)
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != want || w.Code != http.StatusOK {
			t.Errorf("Origin %s: %d, Allow-Origin %q, want %q", origin, w.Code, got, want)
		}
	}
}
