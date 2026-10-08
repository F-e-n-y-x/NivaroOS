package middleware

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
	echo_middleware "github.com/labstack/echo/v5/middleware"
)

// CorsConfig is the CORS policy every NivaroOS echo service uses. Echo v5
// refuses AllowOrigins "*" together with AllowCredentials, so the wildcard
// is answered through UnsafeAllowOriginFunc - same headers as echo v4 sent
// (Access-Control-Allow-Origin: * plus Allow-Credentials: true).
func CorsConfig() echo_middleware.CORSConfig {
	return echo_middleware.CORSConfig{
		UnsafeAllowOriginFunc: func(*echo.Context, string) (string, bool, error) { return "*", true, nil },
		AllowMethods:          []string{http.MethodPost, http.MethodGet, http.MethodOptions, http.MethodPut, http.MethodDelete},
		AllowHeaders:          []string{echo.HeaderAuthorization, echo.HeaderContentLength, echo.HeaderXCSRFToken, echo.HeaderContentType, echo.HeaderAccessControlAllowOrigin, echo.HeaderAccessControlAllowHeaders, echo.HeaderAccessControlAllowMethods, echo.HeaderConnection, echo.HeaderOrigin, echo.HeaderXRequestedWith},
		ExposeHeaders:         []string{echo.HeaderContentLength, echo.HeaderAccessControlAllowOrigin, echo.HeaderAccessControlAllowHeaders},
		MaxAge:                172800,
		AllowCredentials:      true,
	}
}

func Cors() echo.MiddlewareFunc { return echo_middleware.CORSWithConfig(CorsConfig()) }

// requestLogLine keeps the field order of echo v4's DefaultLoggerConfig
// format, with "uri" (path + query string) replaced by "path": WebSocket
// endpoints carry the JWT as ?token=, and logging the full URI put live
// access tokens into the journal.
type requestLogLine struct {
	Time         string `json:"time"`
	ID           string `json:"id"`
	RemoteIP     string `json:"remote_ip"`
	Host         string `json:"host"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	UserAgent    string `json:"user_agent"`
	Status       int    `json:"status"`
	Error        string `json:"error"`
	Latency      int64  `json:"latency"`
	LatencyHuman string `json:"latency_human"`
	BytesIn      int64  `json:"bytes_in"`
	BytesOut     int64  `json:"bytes_out"`
}

// RequestLogger writes one JSON line per request to stdout (the journal),
// like echo v4's Logger() did, and never logs the query string. Errors are
// handed to the error handler first so the logged status is the one sent.
func RequestLogger() echo.MiddlewareFunc {
	return echo_middleware.RequestLoggerWithConfig(echo_middleware.RequestLoggerConfig{
		HandleError:      true,
		LogLatency:       true,
		LogRemoteIP:      true,
		LogHost:          true,
		LogMethod:        true,
		LogURIPath:       true,
		LogRequestID:     true,
		LogUserAgent:     true,
		LogStatus:        true,
		LogContentLength: true,
		LogResponseSize:  true,
		LogValuesFunc: func(_ *echo.Context, v echo_middleware.RequestLoggerValues) error {
			line := requestLogLine{
				Time: time.Now().Format(time.RFC3339Nano), ID: v.RequestID, RemoteIP: v.RemoteIP,
				Host: v.Host, Method: v.Method, Path: v.URIPath, UserAgent: v.UserAgent,
				Status: v.Status, Latency: int64(v.Latency), LatencyHuman: v.Latency.String(),
				BytesOut: max(v.ResponseSize, 0),
			}
			if v.Error != nil {
				line.Error = v.Error.Error()
			}
			line.BytesIn, _ = strconv.ParseInt(v.ContentLength, 10, 64)
			b, _ := json.Marshal(line)
			_, _ = os.Stdout.Write(append(b, '\n'))
			return nil
		},
	})
}
