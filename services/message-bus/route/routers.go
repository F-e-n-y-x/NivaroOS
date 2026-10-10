package route

import (
	"crypto/ecdsa"
	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware/oapi"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/config"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/service"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	echojwt "github.com/labstack/echo-jwt/v5"
	"github.com/labstack/echo/v5"
	echo_middleware "github.com/labstack/echo/v5/middleware"
)

func NewAPIRouter(swagger *openapi3.T, services *service.Services) (http.Handler, error) {
	return newAPIRouter(swagger, services, func() (*ecdsa.PublicKey, error) {
		return external.GetPublicKey(config.CommonInfo.RuntimePath)
	})
}

// isSubscription reports whether r opens an event stream: a WebSocket
// upgrade on /event/{source_id} or /action/{source_id}.
func isSubscription(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get(echo.HeaderUpgrade), "websocket")
}

// tokenFromRequest finds the caller's access token: the Authorization
// header (raw or "Bearer <token>"), else a `token` query parameter. Browsers
// can't set headers on a WebSocket, so the web UI's message-bus sockets
// send ?token=.
func tokenFromRequest(c *echo.Context) ([]string, echo_middleware.ExtractorSource, error) {
	if h := c.Request().Header.Get(echo.HeaderAuthorization); h != "" {
		return []string{strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))}, echo_middleware.ExtractorSourceHeader, nil
	}
	return []string{c.QueryParam("token")}, echo_middleware.ExtractorSourceQuery, nil
}

func newAPIRouter(swagger *openapi3.T, services *service.Services, publicKeyFunc func() (*ecdsa.PublicKey, error)) (http.Handler, error) {
	apiRoute := NewAPIRoute(services)

	e := echo.New()

	// CORS only for pages served from this same host (the web UI reaches
	// the bus same-origin through the gateway; apps on other ports of this
	// host still qualify). A cross-site page gets no CORS headers, so its
	// browser won't let it read anything.
	cors := nivaroos_middleware.CorsConfig()
	cors.Skipper = func(c *echo.Context) bool {
		return !nivaroos_middleware.SameHostOrigin(c.Request())
	}
	e.Use(echo_middleware.CORSWithConfig(cors))

	// Cross-site WebSocket hijacking guard for subscriptions that carry no
	// token (see service.SubscriptionOriginAllowed): those must come from
	// a non-browser client or a page on this same host. The /event and
	// /action upgrades (gobwas/ws) do no origin check of their own.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if isSubscription(c.Request()) && !service.SubscriptionOriginAllowed(c.Request()) {
				return echo.NewHTTPError(http.StatusForbidden, "cross-origin subscription refused")
			}
			return next(c)
		}
	})

	e.Use(echo_middleware.Gzip())

	// Logs the path only: tokens can ride in query strings. The half-second
	// live readings (nivaroos:system:utilization:live, only while a phone
	// is in Real time) aren't logged - they'd be two lines a second.
	requestLog := nivaroos_middleware.RequestLogger()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		logged := requestLog(next)
		return func(c *echo.Context) error {
			if quietRequest(c.Request()) {
				return next(c)
			}
			return logged(c)
		}
	})

	// Every request - including every event/action subscription -
	// needs a valid access token, except same-host automation (other
	// NivaroOS services, nivaroos-cli) as decided by IsLocalAutomation.
	e.Use(echojwt.WithConfig(echojwt.Config{
		// Same-host automation only (c.RealIP() trusted spoofable
		// X-Forwarded-For headers).
		Skipper: func(c *echo.Context) bool {
			return nivaroos_middleware.IsLocalAutomation(c.Request())
		},
		ParseTokenFunc: func(c *echo.Context, token string) (interface{}, error) {
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

	e.Use(oapi.RequestValidator(swagger, oapi.Options{Options: openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}))

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

// quietRequest: requests too frequent to be worth a log line.
func quietRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/nivaroos:system:utilization:live")
}
