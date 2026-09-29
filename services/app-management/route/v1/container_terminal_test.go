package v1

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
)

// The session routes sit next to /:id routes; make sure neither shadows
// the other, and that the JWT skipper never lets them through untokened.
func TestContainerTerminalSessionRoutesDontClashWithContainerRoutes(t *testing.T) {
	e := echo.New()
	g := e.Group("/v1/container")
	hit := ""
	g.GET("/:id", func(c echo.Context) error { hit = "container:" + c.Param("id"); return nil })
	g.GET("/:id/terminal", func(c echo.Context) error { hit = "legacy:" + c.Param("id"); return nil })
	g.DELETE("/:id", func(c echo.Context) error { hit = "uninstall:" + c.Param("id"); return nil })
	ContainerTerminalAPI().Register(g.Group("/terminal-sessions"))

	do := func(method, path string) int {
		hit = ""
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}
	if code := do("GET", "/v1/container/terminal-sessions"); code != http.StatusUnauthorized || hit != "" {
		t.Fatalf("list: %d %q", code, hit)
	}
	if code := do("DELETE", "/v1/container/terminal-sessions/abc"); code != http.StatusUnauthorized || hit != "" {
		t.Fatalf("kill: %d %q", code, hit)
	}
	if do("GET", "/v1/container/jellyfin"); hit != "container:jellyfin" {
		t.Fatalf("container route: %q", hit)
	}
	if do("GET", "/v1/container/jellyfin/terminal"); hit != "legacy:jellyfin" {
		t.Fatalf("legacy terminal route: %q", hit)
	}
	if do("DELETE", "/v1/container/jellyfin"); hit != "uninstall:jellyfin" {
		t.Fatalf("uninstall route: %q", hit)
	}
	for _, p := range []string{ContainerTerminalBase, ContainerTerminalBase + "/abc/attach"} {
		if !nivaroos_middleware.MatchRoute(ContainerTerminalBase+"/*", p) {
			t.Errorf("%s not covered by the never-skip pattern", p)
		}
	}
}
