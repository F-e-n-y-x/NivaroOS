package middleware

import (
	"github.com/labstack/echo/v4"
	echo_middleware "github.com/labstack/echo/v4/middleware"
)

func Cors() echo.MiddlewareFunc {
	return echo_middleware.CORSWithConfig(echo_middleware.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{echo.POST, echo.GET, echo.OPTIONS, echo.PUT, echo.DELETE},
		AllowHeaders:     []string{echo.HeaderAuthorization, echo.HeaderContentLength, echo.HeaderXCSRFToken, echo.HeaderContentType, echo.HeaderAccessControlAllowOrigin, echo.HeaderAccessControlAllowHeaders, echo.HeaderAccessControlAllowMethods, echo.HeaderConnection, echo.HeaderOrigin, echo.HeaderXRequestedWith},
		ExposeHeaders:    []string{echo.HeaderContentLength, echo.HeaderAccessControlAllowOrigin, echo.HeaderAccessControlAllowHeaders},
		MaxAge:           172800,
		AllowCredentials: true,
	})
}

// requestLogFormat is echo's DefaultLoggerConfig format with "uri"
// (path + query string) replaced by "path": WebSocket endpoints carry the
// JWT as ?token=, and logging the full URI put live access tokens into the
// journal.
const requestLogFormat = `{"time":"${time_rfc3339_nano}","id":"${id}","remote_ip":"${remote_ip}",` +
	`"host":"${host}","method":"${method}","path":"${path}","user_agent":"${user_agent}",` +
	`"status":${status},"error":"${error}","latency":${latency},"latency_human":"${latency_human}"` +
	`,"bytes_in":${bytes_in},"bytes_out":${bytes_out}}` + "\n"

// RequestLogger is a drop-in replacement for echo_middleware.Logger() that
// never logs the query string.
func RequestLogger() echo.MiddlewareFunc {
	return echo_middleware.LoggerWithConfig(echo_middleware.LoggerConfig{
		Format: requestLogFormat,
	})
}
