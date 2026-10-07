package v1

import (
	"html/template"
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
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
)

// expiryToDuration maps the fixed expiry choices the apps offer - there is
// deliberately no free-form duration input.
func expiryToDuration(expiry string) (time.Duration, bool) {
	switch expiry {
	case "1h":
		return time.Hour, true
	case "1d":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	case "never":
		return 0, true
	default:
		return 0, false
	}
}

type quickShareItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	URL          string `json:"url"`
	ExpiresAt    int64  `json:"expires_at"`
	Created      int64  `json:"created"`
	MaxDownloads int    `json:"max_downloads"`
	Downloads    int    `json:"downloads"`
	HasPassword  bool   `json:"has_password"`
	IsDir        bool   `json:"is_dir"`
}

func newQuickShareItem(ctx echo.Context, s model2.QuickShareDBModel) quickShareItem {
	fi, err := os.Stat(s.Path)
	return quickShareItem{
		ID:           s.ID,
		Name:         s.Name,
		Path:         s.Path,
		URL:          quickShareURL(ctx, s.ID),
		ExpiresAt:    s.ExpiresAt,
		Created:      s.Created,
		MaxDownloads: s.MaxDownloads,
		Downloads:    s.Downloads,
		HasPassword:  s.PasswordHash != "",
		IsDir:        err == nil && fi.IsDir(),
	}
}

func quickShareURL(ctx echo.Context, id string) string {
	scheme := "http"
	if ctx.Request().TLS != nil || ctx.Request().Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + ctx.Request().Host + "/v1/qs/" + id
}

// @Summary create a Quick Share link for a file or folder
// @Produce  application/json
// @Accept  application/json
// @Tags quickshare
// @Security ApiKeyAuth
// @Param path body string true "path of the file to share"
// @Param expiry body string true "one of: 1h, 1d, 7d, never"
// @Param max_downloads body int false "0 = unlimited, 1 = one-time link"
// @Param password body string false "asked before the download when set"
// @Success 200 {string} string "ok"
// @Router /quickshare [post]
func PostCreateQuickShare(ctx echo.Context) error {
	req := struct {
		Path         string `json:"path"`
		Expiry       string `json:"expiry"`
		MaxDownloads int    `json:"max_downloads"`
		Password     string `json:"password"`
	}{}
	if err := ctx.Bind(&req); err != nil || len(req.Path) == 0 || req.MaxDownloads < 0 || len(req.Password) > 72 {
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

	share, err := service.MyService.QuickShares().CreateQuickShare(req.Path, path.Base(req.Path), expiresIn, req.MaxDownloads, req.Password)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: common_err.GetMsg(common_err.SERVICE_ERROR),
		})
	}

	return ctx.JSON(common_err.SUCCESS, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    newQuickShareItem(ctx, share),
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
		list = append(list, newQuickShareItem(ctx, s))
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
// opaque, unguessable :id is the only credential (never a raw path), so
// this is safe to register outside the JWT-protected route group. Anyone
// with the link gets the file; nothing else on this instance is reachable
// through it.
//
// A one-time (limited) or password link answers GET with a small page
// whose Download button POSTs (PostQuickShareRedeem): chat apps fetch
// every link they see for a preview, and that must neither use up a
// one-time link nor try a password.
func GetQuickShareRedeem(ctx echo.Context) error {
	share, ok := liveQuickShare(ctx)
	if !ok {
		return nil
	}
	if share.MaxDownloads > 0 || share.PasswordHash != "" {
		return quickSharePage(ctx, http.StatusOK, share, "")
	}
	return serveQuickShare(ctx, share)
}

// PostQuickShareRedeem is the landing page's Download button: checks the
// password, counts the download, then serves it.
func PostQuickShareRedeem(ctx echo.Context) error {
	share, ok := liveQuickShare(ctx)
	if !ok {
		return nil
	}
	if !service.CheckQuickSharePassword(share, ctx.FormValue("password")) {
		// ponytail: a flat delay against guessing; per-IP limits if links get hammered.
		time.Sleep(time.Second)
		return quickSharePage(ctx, http.StatusForbidden, share, "Wrong password. Try again.")
	}
	if !service.MyService.QuickShares().ClaimQuickShareDownload(share.ID) {
		return ctx.JSON(http.StatusGone, model.Result{
			Success: common_err.QUICKSHARE_EXPIRED,
			Message: common_err.GetMsg(common_err.QUICKSHARE_EXPIRED),
		})
	}
	return serveQuickShare(ctx, share)
}

// liveQuickShare finds :id; false when it is gone, expired or its file is,
// and the 404/410 has been sent.
func liveQuickShare(ctx echo.Context) (model2.QuickShareDBModel, bool) {
	fail := func(status, code int) (model2.QuickShareDBModel, bool) {
		_ = ctx.JSON(status, model.Result{Success: code, Message: common_err.GetMsg(code)})
		return model2.QuickShareDBModel{}, false
	}
	id := ctx.Param("id")
	share, found := service.MyService.QuickShares().GetQuickShareByID(id)
	if !found {
		return fail(http.StatusNotFound, common_err.QUICKSHARE_NOT_FOUND)
	}
	if share.ExpiresAt > 0 && share.ExpiresAt < time.Now().Unix() {
		service.MyService.QuickShares().DeleteQuickShare(id)
		return fail(http.StatusGone, common_err.QUICKSHARE_EXPIRED)
	}
	if !file.Exists(share.Path) {
		return fail(http.StatusNotFound, common_err.FILE_DOES_NOT_EXIST)
	}
	return share, true
}

var quickSharePageTmpl = template.Must(template.New("qs").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex"><title>{{.Name}}</title>
<style>body{font:16px system-ui,sans-serif;margin:0;display:grid;place-items:center;min-height:100vh;background:#f4f4f5;color:#18181b}
main{background:#fff;padding:24px;border-radius:12px;max-width:360px;width:calc(100% - 32px);box-sizing:border-box}
h1{font-size:18px;word-break:break-all;margin:0 0 4px}p{margin:0 0 16px;color:#52525b}.err{color:#b91c1c}
input,button{font:inherit;width:100%;box-sizing:border-box;padding:10px;border-radius:8px;border:1px solid #d4d4d8;margin-bottom:12px}
button{background:#18181b;color:#fff;border:0;cursor:pointer}
@media(prefers-color-scheme:dark){body{background:#000;color:#fafafa}main{background:#18181b}p{color:#a1a1aa}input{background:#000;color:#fafafa;border-color:#3f3f46}button{background:#fafafa;color:#18181b}}</style></head>
<body><main><h1>{{.Name}}</h1><p>Shared from NivaroOS{{if .OneTime}} · This link works once{{end}}</p>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post">{{if .Password}}<input type="password" name="password" placeholder="Password" autocomplete="off" required autofocus>{{end}}<button type="submit">Download</button></form>
</main></body></html>`))

func quickSharePage(ctx echo.Context, status int, share model2.QuickShareDBModel, errText string) error {
	name := share.Name
	if fi, err := os.Stat(share.Path); err == nil && fi.IsDir() {
		name += ".zip"
	}
	h := ctx.Response().Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex")
	h.Set("Referrer-Policy", "no-referrer")
	ctx.Response().WriteHeader(status)
	return quickSharePageTmpl.Execute(ctx.Response().Writer, map[string]interface{}{
		"Name":     name,
		"OneTime":  share.MaxDownloads == 1,
		"Password": share.PasswordHash != "",
		"Error":    errText,
	})
}

// serveQuickShare sends the shared file, or a folder as a ZIP stream.
func serveQuickShare(ctx echo.Context, share model2.QuickShareDBModel) error {
	notFound := func() error {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.FILE_DOES_NOT_EXIST,
			Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		})
	}
	node, err := os.Stat(share.Path)
	if err != nil {
		return notFound()
	}
	if node.IsDir() {
		_, ar, _ := file.GetCompressionAlgorithm("zip")
		h := ctx.Response().Header()
		h.Set("Content-Type", "application/zip")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(path.Base(share.Path)+".zip"))
		if err := ar.Create(ctx.Response().Writer); err != nil {
			return err
		}
		defer ar.Close()
		return file.AddFileReport(ar, share.Path, share.Path, func(string, error) {})
	}

	fi, err := os.Open(share.Path)
	if err != nil {
		return notFound()
	}
	defer fi.Close()

	fileName := path.Base(share.Path)
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
