package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// L1: any Origin was echoed back with Allow-Credentials, so any website
// could read the answers. Only this same host (any port) may now.
func TestCorsOnlyForThisHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ginCors())
	r.GET("/x", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	for origin, want := range map[string]string{
		"https://evil.example":       "",
		"http://nas.local.evil.test": "",
		"http://nas.local:5173":      "http://nas.local:5173",
		"http://nas.local":           "http://nas.local",
	} {
		req := httptest.NewRequest(http.MethodGet, "http://nas.local/x", nil)
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("Origin %s: Allow-Origin %q, want %q", origin, got, want)
		}
	}
}
