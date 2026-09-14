package v1

import (
	"net/http"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/h2non/filetype"
	"github.com/labstack/echo/v4"

	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
)

// expiryToDuration maps the three options the Files app UI actually offers -
// there is deliberately no free-form duration input, so this only ever needs
// to recognize these three literal values.
func expiryToDuration(expiry string) (time.Duration, bool) {
	switch expiry {
	case "1h":
		return time.Hour, true
	case "1d":
		return 24 * time.Hour, true
	case "never":
		return 0, true
	default:
		return 0, false
	}
}

type quickShareItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
	Created   int64  `json:"created"`
}

func quickShareURL(ctx echo.Context, id string) string {
	scheme := "http"
	if ctx.Request().TLS != nil || ctx.Request().Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + ctx.Request().Host + "/v1/qs/" + id
}

// @Summary create a Quick Share link for a single file
// @Produce  application/json
// @Accept  application/json
// @Tags quickshare
// @Security ApiKeyAuth
// @Param path body string true "path of the file to share"
// @Param expiry body string true "one of: 1h, 1d, never"
// @Success 200 {string} string "ok"
// @Router /quickshare [post]
func PostCreateQuickShare(ctx echo.Context) error {
	req := struct {
		Path   string `json:"path"`
		Expiry string `json:"expiry"`
	}{}
	if err := ctx.Bind(&req); err != nil || len(req.Path) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}
	if !file.Exists(req.Path) {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.FILE_DOES_NOT_EXIST,
			Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		})
	}
	expiresIn, ok := expiryToDuration(req.Expiry)
	if !ok {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}

	share, err := service.MyService.QuickShares().CreateQuickShare(req.Path, path.Base(req.Path), expiresIn)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: common_err.GetMsg(common_err.SERVICE_ERROR),
		})
	}

	return ctx.JSON(common_err.SUCCESS, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data: quickShareItem{
			ID:        share.ID,
			Name:      share.Name,
			Path:      share.Path,
			URL:       quickShareURL(ctx, share.ID),
			ExpiresAt: share.ExpiresAt,
			Created:   share.Created,
		},
	})
}

// @Summary list this instance's active Quick Share links
// @Produce  application/json
// @Tags quickshare
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /quickshare [get]
func GetQuickSharesList(ctx echo.Context) error {
	service.MyService.QuickShares().PurgeExpiredQuickShares()
	shares := service.MyService.QuickShares().GetQuickSharesList()
	list := make([]quickShareItem, 0, len(shares))
	for _, s := range shares {
		list = append(list, quickShareItem{
			ID:        s.ID,
			Name:      s.Name,
			Path:      s.Path,
			URL:       quickShareURL(ctx, s.ID),
			ExpiresAt: s.ExpiresAt,
			Created:   s.Created,
		})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: list})
}

// @Summary revoke a Quick Share link
// @Produce  application/json
// @Tags quickshare
// @Security ApiKeyAuth
// @Param id path string true "share id"
// @Success 200 {string} string "ok"
// @Router /quickshare/{id} [delete]
func DeleteQuickShare(ctx echo.Context) error {
	id := ctx.Param("id")
	if len(id) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}
	service.MyService.QuickShares().DeleteQuickShare(id)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// GetQuickShareRedeem serves the shared file without authentication - the
// opaque, unguessable :token is the only credential (never a raw path), so
// this is safe to register outside the JWT-protected route group. Anyone
// with the link gets the file; nothing else on this instance is reachable
// through it.
func GetQuickShareRedeem(ctx echo.Context) error {
	id := ctx.Param("id")
	share, found := service.MyService.QuickShares().GetQuickShareByID(id)
	if !found {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.QUICKSHARE_NOT_FOUND,
			Message: common_err.GetMsg(common_err.QUICKSHARE_NOT_FOUND),
		})
	}
	if share.ExpiresAt > 0 && share.ExpiresAt < time.Now().Unix() {
		service.MyService.QuickShares().DeleteQuickShare(id)
		return ctx.JSON(http.StatusGone, model.Result{
			Success: common_err.QUICKSHARE_EXPIRED,
			Message: common_err.GetMsg(common_err.QUICKSHARE_EXPIRED),
		})
	}
	if !file.Exists(share.Path) {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.FILE_DOES_NOT_EXIST,
			Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		})
	}

	fi, err := os.Open(share.Path)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.FILE_DOES_NOT_EXIST,
			Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		})
	}
	defer fi.Close()

	fileName := path.Base(share.Path)
	node, err := os.Stat(share.Path)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.FILE_DOES_NOT_EXIST,
			Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		})
	}

	buffer := make([]byte, 261)
	_, _ = fi.Read(buffer)
	_, _ = fi.Seek(0, 0)
	if kind, _ := filetype.Match(buffer); kind != filetype.Unknown {
		ctx.Response().Header().Set("Content-Type", kind.MIME.Value)
	}
	ctx.Response().Header().Set("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(fileName))

	http.ServeContent(ctx.Response().Writer, ctx.Request(), fileName, node.ModTime(), fi)
	return nil
}
