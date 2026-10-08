package v1

import "github.com/labstack/echo/v5"

// NewEcho is echo.New() with the echo v4 behaviour these handlers rely on.
func NewEcho() *echo.Echo {
	e := echo.New()
	// ctx.File/Attachment take absolute paths (echo v4 os.Open'd them; v5
	// defaults to a filesystem rooted at the working directory).
	e.Filesystem = echo.NewDefaultFS("/")
	// echo v4 RealIP(): the companion routes record the phone's address
	// from the gateway's X-Forwarded-For (data only, never an auth decision).
	e.IPExtractor = echo.LegacyIPExtractor()
	return e
}
