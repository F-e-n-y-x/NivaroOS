package route

import (
	"crypto/ecdsa"
	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware/oapi"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	codegen "github.com/F-e-n-y-x/NivaroOS/services/user/codegen/user_service"
	v2 "github.com/F-e-n-y-x/NivaroOS/services/user/route/v2"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	echojwt "github.com/labstack/echo-jwt/v5"
	"github.com/labstack/echo/v5"
	echo_middleware "github.com/labstack/echo/v5/middleware"
)

var (
	_swagger *openapi3.T

	V2APIPath string
	V2DocPath string
)

func init() {
	swagger, err := codegen.GetSwagger()
	if err != nil {
		panic(err)
	}

	_swagger = swagger

	u, err := url.Parse(_swagger.Servers[0].URL)
	if err != nil {
		panic(err)
	}

	V2APIPath = strings.TrimRight(u.Path, "/")
	V2DocPath = "/doc" + V2APIPath
}

func InitV2Router() http.Handler {
	UserService := v2.NewUserService()

	e := echo.New()

	e.Use(nivaroos_middleware.Cors())

	e.Use(echo_middleware.Gzip())

	// Logs the path only: tokens can ride in query strings.
	e.Use(nivaroos_middleware.RequestLogger())

	e.Use(echojwt.WithConfig(echojwt.Config{
		// Same-host automation only (socket peer loopback, no proxy or
		// browser headers) - c.RealIP() trusted spoofable X-Forwarded-For.
		Skipper: nivaroos_middleware.LocalAutomationSkipper(),
		ParseTokenFunc: func(c *echo.Context, token string) (interface{}, error) {
			valid, claims, err := jwt.Validate(
				token,
				func() (*ecdsa.PublicKey, error) {
					_, publicKey := service.MyService.User().GetKeyPair()
					return publicKey, nil
				})
			if err != nil || !valid {
				return nil, echo.ErrUnauthorized
			}

			c.Request().Header.Set("user_id", strconv.Itoa(claims.ID))

			return claims, nil
		},
		TokenLookupFuncs: []echo_middleware.ValuesExtractor{
			func(c *echo.Context) ([]string, echo_middleware.ExtractorSource, error) {
				return []string{c.Request().Header.Get(echo.HeaderAuthorization)}, echo_middleware.ExtractorSourceHeader, nil
			},
		},
	}))

	e.Use(oapi.RequestValidator(_swagger, oapi.Options{Options: openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}))

	codegen.RegisterHandlersWithBaseURL(e, UserService, V2APIPath)

	return e
}

func InitV2DocRouter(docHTML string, docYAML string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == V2DocPath {
			if _, err := w.Write([]byte(docHTML)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		if r.URL.Path == V2DocPath+"/openapi.yaml" {
			if _, err := w.Write([]byte(docYAML)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}
	})
}
