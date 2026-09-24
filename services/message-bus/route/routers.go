package route

import (
	"crypto/ecdsa"
	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/config"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/service"
	"github.com/deepmap/oapi-codegen/pkg/middleware"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/labstack/echo/v4"
	echo_middleware "github.com/labstack/echo/v4/middleware"
)

func NewAPIRouter(swagger *openapi3.T, services *service.Services) (http.Handler, error) {
	return newAPIRouter(swagger, services, func() (*ecdsa.PublicKey, error) {
		return external.GetPublicKey(config.CommonInfo.RuntimePath)
	})
}

// isSubscription reports whether r opens (or continues) an event stream:
// a WebSocket upgrade on /event/{source_id} or /action/{source_id}, or any
// socket.io request (its websocket transport and its HTTP long-polling
// transport alike).
func isSubscription(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get(echo.HeaderUpgrade), "websocket") {
		return true
	}
	return strings.Contains(r.URL.Path, "/socket.io")
}

// tokenFromRequest finds the caller's access token: the Authorization
// header (raw or "Bearer <token>"), else a `token` query parameter. Browsers
// can't set headers on a WebSocket, so the web UI's socket.io client
// sends ?token= (engine.io repeats the query on every polling and
// websocket request of the session).
func tokenFromRequest(c echo.Context) ([]string, error) {
	if h := c.Request().Header.Get(echo.HeaderAuthorization); h != "" {
		return []string{strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))}, nil
	}
	return []string{c.QueryParam("token")}, nil
}

func newAPIRouter(swagger *openapi3.T, services *service.Services, publicKeyFunc func() (*ecdsa.PublicKey, error)) (http.Handler, error) {
	apiRoute := NewAPIRoute(services)

	e := echo.New()

	// CORS only for pages served from this same host (the web UI reaches
	// the bus same-origin through the gateway; apps on other ports of this
	// host still qualify). A cross-site page gets no CORS headers, so its
	// browser won't let it read anything.
	e.Use((echo_middleware.CORSWithConfig(echo_middleware.CORSConfig{
		Skipper: func(c echo.Context) bool {
			return !nivaroos_middleware.SameHostOrigin(c.Request())
		},
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{echo.POST, echo.GET, echo.OPTIONS, echo.PUT, echo.DELETE},
		AllowHeaders:     []string{echo.HeaderAuthorization, echo.HeaderContentLength, echo.HeaderXCSRFToken, echo.HeaderContentType, echo.HeaderAccessControlAllowOrigin, echo.HeaderAccessControlAllowHeaders, echo.HeaderAccessControlAllowMethods, echo.HeaderConnection, echo.HeaderOrigin, echo.HeaderXRequestedWith},
		ExposeHeaders:    []string{echo.HeaderContentLength, echo.HeaderAccessControlAllowOrigin, echo.HeaderAccessControlAllowHeaders},
		MaxAge:           172800,
		AllowCredentials: true,
	})))

	// Cross-site WebSocket hijacking guard for subscriptions that carry no
	// token (see service.SubscriptionOriginAllowed): those must come from
	// a non-browser client or a page on this same host. The /event and
	// /action upgrades (gobwas/ws) do no origin check of their own.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if isSubscription(c.Request()) && !service.SubscriptionOriginAllowed(c.Request()) {
				return echo.NewHTTPError(http.StatusForbidden, "cross-origin subscription refused")
			}
			return next(c)
		}
	})

	e.Use(echo_middleware.Gzip())

	// Logs the path only: tokens can ride in query strings.
	e.Use(nivaroos_middleware.RequestLogger())

	// Every request - including every event/action/socket.io subscription -
	// needs a valid access token, except same-host automation (other
	// NivaroOS services, nivaroos-cli) as decided by IsLocalAutomation.
	e.Use(echo_middleware.JWTWithConfig(echo_middleware.JWTConfig{
		// Same-host automation only (c.RealIP() trusted spoofable
		// X-Forwarded-For headers).
		Skipper: func(c echo.Context) bool {
			return nivaroos_middleware.IsLocalAutomation(c.Request())
		},
		ParseTokenFunc: func(token string, c echo.Context) (interface{}, error) {
			if token == "" {
				return nil, echo.ErrUnauthorized
			}
			valid, claims, err := jwt.Validate(token, publicKeyFunc)
			if err != nil || !valid {
				return nil, echo.ErrUnauthorized
			}

			c.Request().Header.Set("user_id", strconv.Itoa(claims.ID))

			return claims, nil
		},
		TokenLookupFuncs: []echo_middleware.ValuesExtractor{tokenFromRequest},
	}))

	e.Use(middleware.OapiRequestValidatorWithOptions(swagger, &middleware.Options{Options: openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}))

	apiPath, err := getAPIPath(getSwaggerURL(swagger))
	if err != nil {
		return nil, err
	}

	codegen.RegisterHandlersWithBaseURL(e, apiRoute, apiPath)

	return e, nil
}

func NewDocRouter(swagger *openapi3.T, docHTML string, docYAML string) (http.Handler, error) {
	apiPath, err := getAPIPath(getSwaggerURL(swagger))
	if err != nil {
		return nil, err
	}

	docPath := "/doc" + apiPath

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == docPath {
			if _, err := w.Write([]byte(docHTML)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		if r.URL.Path == docPath+"/openapi.yaml" {
			if _, err := w.Write([]byte(docYAML)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}
	}), nil
}

func getSwaggerURL(swagger *openapi3.T) string {
	return swagger.Servers[0].URL
}

func getAPIPath(swaggerURL string) (string, error) {
	u, err := url.Parse(swaggerURL)
	if err != nil {
		return "", err
	}

	return strings.TrimRight(u.Path, "/"), nil
}
