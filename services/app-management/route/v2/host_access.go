package v2

import (
	"net/http"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/service"
	"github.com/labstack/echo/v5"
	"github.com/samber/lo"
)

// allowHostAccessParam is the admin's OK for an app that gets
// root-equivalent access to this server (ComposeApp.HostAccess).
const allowHostAccessParam = "allow_host_access"

// refuseHostAccess answers 428, with the reasons in data.host_access, when
// app would get host access it didn't have before (nil: a new install) and
// the request doesn't carry ?allow_host_access=true.
func refuseHostAccess(ctx *echo.Context, app, before *service.ComposeApp) (bool, error) {
	why := app.HostAccess()
	if before != nil {
		why = lo.Without(why, before.HostAccess()...)
	}
	if len(why) == 0 || ctx.QueryParam(allowHostAccessParam) == "true" {
		return false, nil
	}
	return true, ctx.JSON(http.StatusPreconditionRequired, map[string]any{
		"message": "This app gets full (root) access to the server (" + strings.Join(why, ", ") + "). Install it only if you trust it.",
		"data":    map[string]any{"host_access": why},
	})
}
