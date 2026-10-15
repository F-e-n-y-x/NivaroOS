package route

import (
	_ "embed"
	"html/template"
	"net"
	"net/http"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/gateway/service"
	"github.com/labstack/echo/v5"
)

// HTTPS on the home network (docs/specs/2026-10-16-lan-https.md).
// Status, the CA download and the trust page are public: a device needs
// them before it can sign in safely. Changing the switches needs a token.

const TLSRoutePath = "/v1/gateway/tls"

//go:embed trust.html
var trustHTML string

var trustTmpl = template.Must(template.New("trust").Parse(trustHTML))

func (m *ManagementRoute) buildTLSRoutes(g *echo.Group) {
	tlsOf := func() *service.TLS { return m.management.State.TLS }
	unavailable := func(c *echo.Context) error {
		return c.JSON(http.StatusNotFound, model.Result{Success: common_err.SERVICE_ERROR, Message: "https is not available on this gateway"})
	}

	g.GET("/tls", func(c *echo.Context) error {
		t := tlsOf()
		if t == nil {
			return unavailable(c)
		}
		return c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: t.Status()})
	})

	g.PUT("/tls", func(c *echo.Context) error {
		t := tlsOf()
		if t == nil {
			return unavailable(c)
		}
		var req struct {
			Enabled  bool `json:"enabled"`
			Redirect bool `json:"redirect"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, model.Result{Success: common_err.CLIENT_ERROR, Message: err.Error()})
		}
		if err := t.Set(req.Enabled, req.Redirect); err != nil {
			return c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error(), Data: t.Status()})
		}
		return c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: t.Status()})
	}, m.jwtAuth())

	g.GET("/tls/ca.crt", func(c *echo.Context) error {
		t := tlsOf()
		if t == nil || t.CA() == nil {
			return unavailable(c)
		}
		h := c.Response().Header()
		h.Set("Content-Disposition", `attachment; filename="nivaroos-ca.crt"`)
		h.Set("Cache-Control", "no-store")
		return c.Blob(http.StatusOK, "application/x-x509-ca-cert", t.CA().CADER())
	})

	g.GET("/tls/trust", func(c *echo.Context) error {
		t := tlsOf()
		if t == nil {
			return unavailable(c)
		}
		st := t.Status()
		c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
		c.Response().Header().Set("X-Frame-Options", "DENY")
		c.Response().WriteHeader(http.StatusOK)
		return trustTmpl.Execute(c.Response(), st)
	})
}

// HTTPSRedirect sends browsers loading a page over http to the https
// listener once the owner switched that on. Only top-level page loads
// from other devices: API calls, the app, WebSockets, iframes, scripts
// and relays on this box (cloudflared, tailscale serve) are untouched,
// and it is a 302 - nothing a browser remembers after the switch is off.
func HTTPSRedirect(next http.Handler, t *service.TLS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t != nil && isPageLoad(r) && !isHostAddr(peerIP(r.RemoteAddr)) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			if port, ok := t.RedirectPort(host); ok {
				target := "https://" + net.JoinHostPort(strings.Trim(host, "[]"), port)
				if port == "443" {
					target = "https://" + bracket(host)
				}
				http.Redirect(w, r, target+r.URL.RequestURI(), http.StatusFound)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isPageLoad(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	dest := r.Header.Get("Sec-Fetch-Dest")
	return r.Header.Get("Sec-Fetch-Mode") == "navigate" && (dest == "" || dest == "document")
}

func peerIP(remoteAddr string) string {
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return h
	}
	return remoteAddr
}

func bracket(host string) string {
	host = strings.Trim(host, "[]")
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}
