package v2

import (
	"net/http"
	"sort"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/service"
	"github.com/labstack/echo/v5"
	"github.com/samber/lo"
)

// allowHostAccessParam is the admin's OK for an app that gets
// root-equivalent access to this server (ComposeApp.HostAccess).
const allowHostAccessParam = "allow_host_access"

// refuseHostAccess answers 428, with the reasons in data.host_access, when
// app (parsed from raw) would get host access it didn't have before (nil:
// a new install) and the request doesn't carry ?allow_host_access=true.
func refuseHostAccess(ctx *echo.Context, raw []byte, app, before *service.ComposeApp) (bool, error) {
	why := lo.Uniq(append(app.HostAccess(), service.HostAccessAsDeployed(raw, app.Name)...))
	sort.Strings(why)
	if before != nil {
		why = lo.Without(why, before.HostAccess()...)
	}
	if len(why) == 0 || ctx.QueryParam(allowHostAccessParam) == "true" {
		return false, nil
	}
	return true, needHostAccessOK(ctx, "This app gets full (root) access to the server ("+strings.Join(why, ", ")+"). Install it only if you trust it.", why)
}

// needHostAccessOK: 428 with the reasons in data.host_access - the web and
// phone apps ask the admin and repeat the request with ?allow_host_access=true.
func needHostAccessOK(ctx *echo.Context, message string, why []string) error {
	return ctx.JSON(http.StatusPreconditionRequired, map[string]any{
		"message": message,
		"data":    map[string]any{"host_access": why},
	})
}
