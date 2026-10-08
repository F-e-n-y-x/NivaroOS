package route

import (
	"crypto/ecdsa"
	"net/http"
	"strconv"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/gateway/service"
	echojwt "github.com/labstack/echo-jwt/v5"
	"github.com/labstack/echo/v5"
	echo_middleware "github.com/labstack/echo/v5/middleware"
)

type ManagementRoute struct {
	management *service.Management
}

func NewManagementRoute(management *service.Management) *ManagementRoute {
	return &ManagementRoute{
		management: management,
	}
}

func (m *ManagementRoute) GetRoute() http.Handler {
	e := echo.New()

	e.Use(nivaroos_middleware.Cors())

	e.Use(echo_middleware.Gzip())

	e.GET("/ping", func(ctx *echo.Context) error {
		return ctx.JSON(http.StatusOK, map[string]any{
			"message": "pong from management service",
		})
	})

	m.buildV1Group(e)

	return e
}

func (m *ManagementRoute) buildV1Group(e *echo.Echo) {
	v1Group := e.Group("/v1")

	v1Group.Use()
	{
		m.buildV1RouteGroup(v1Group)
	}
}

func (m *ManagementRoute) buildV1RouteGroup(v1Group *echo.Group) {
	v1GatewayGroup := v1Group.Group("/gateway")

	v1GatewayGroup.Use()
	{
		v1GatewayGroup.GET("/routes", func(ctx *echo.Context) error {
			return ctx.JSON(http.StatusOK, m.management.GetRoutes())
		})

		v1GatewayGroup.POST("/routes",
			func(ctx *echo.Context) error {
				var route *model.Route
				err := ctx.Bind(&route)
				if err != nil {
					return ctx.JSON(http.StatusBadRequest, model.Result{
						Success: common_err.CLIENT_ERROR,
						Message: err.Error(),
					})
				}

				if err := m.management.CreateRoute(route); err != nil {
					return ctx.JSON(http.StatusInternalServerError, model.Result{
						Success: common_err.SERVICE_ERROR,
						Message: err.Error(),
					})
				}

				return ctx.NoContent(http.StatusCreated)
			},
			echojwt.WithConfig(echojwt.Config{
				// socket-peer loopback check; c.RealIP() trusted spoofable
				// X-Forwarded-For/X-Real-IP headers
				Skipper: nivaroos_middleware.LocalAutomationSkipper(),
				ParseTokenFunc: func(c *echo.Context, token string) (interface{}, error) {
					valid, claims, err := jwt.Validate(token, func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(m.management.State.GetRuntimePath()) })
					if err != nil || !valid {
						return nil, echo.ErrUnauthorized
					}
					c.Request().Header.Set("user_id", strconv.Itoa(claims.ID))

					return claims, nil
				},
				TokenLookupFuncs: []echo_middleware.ValuesExtractor{
					func(c *echo.Context) ([]string, echo_middleware.ExtractorSource, error) {
						if len(c.Request().Header.Get(echo.HeaderAuthorization)) > 0 {
							return []string{c.Request().Header.Get(echo.HeaderAuthorization)}, echo_middleware.ExtractorSourceHeader, nil
						}
						return []string{c.QueryParam("token")}, echo_middleware.ExtractorSourceQuery, nil
					},
				},
			}))

		v1GatewayGroup.GET("/port", func(ctx *echo.Context) error {
			return ctx.JSON(http.StatusOK, model.Result{
				Success: common_err.SUCCESS,
				Message: common_err.GetMsg(common_err.SUCCESS),
				Data:    m.management.GetGatewayPort(),
			})
		})

		v1GatewayGroup.PUT("/port",
			func(ctx *echo.Context) error {
				var request *model.ChangePortRequest

				if err := ctx.Bind(&request); err != nil {
					return ctx.JSON(http.StatusBadRequest, model.Result{
						Success: common_err.CLIENT_ERROR,
						Message: err.Error(),
					})
				}

				if err := m.management.SetGatewayPort(request.Port); err != nil {
					return ctx.JSON(http.StatusInternalServerError, model.Result{
						Success: common_err.SERVICE_ERROR,
						Message: err.Error(),
					})
				}

				return ctx.JSON(http.StatusOK, model.Result{
					Success: common_err.SUCCESS,
					Message: common_err.GetMsg(common_err.SUCCESS),
				})
			},
			echojwt.WithConfig(echojwt.Config{
				// socket-peer loopback check; c.RealIP() trusted spoofable
				// X-Forwarded-For/X-Real-IP headers
				Skipper: nivaroos_middleware.LocalAutomationSkipper(),
				ParseTokenFunc: func(c *echo.Context, token string) (interface{}, error) {
					valid, claims, err := jwt.Validate(token, func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(m.management.State.GetRuntimePath()) })
					if err != nil || !valid {
						return nil, echo.ErrUnauthorized
					}
					c.Request().Header.Set("user_id", strconv.Itoa(claims.ID))

					return claims, nil
				},
				TokenLookupFuncs: []echo_middleware.ValuesExtractor{
					func(c *echo.Context) ([]string, echo_middleware.ExtractorSource, error) {
						if len(c.Request().Header.Get(echo.HeaderAuthorization)) > 0 {
							return []string{c.Request().Header.Get(echo.HeaderAuthorization)}, echo_middleware.ExtractorSourceHeader, nil
						}
						return []string{c.QueryParam("token")}, echo_middleware.ExtractorSourceQuery, nil
					},
				},
			}))
	}
}
