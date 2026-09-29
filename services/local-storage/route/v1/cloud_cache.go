package v1

import (
	"errors"
	"net/http"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/cloudcache"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/gin-gonic/gin"
)

// Settings > Online storage > Cache.

func cacheOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

// cacheFail answers with the reason as Data (what the web UI shows):
// 400 for settings that can't be used, 409 while uploads/open files make
// it unsafe, 500 otherwise.
func cacheFail(c *gin.Context, err error) {
	code := common_err.SERVICE_ERROR
	var busy *service.ErrCacheBusy
	switch {
	case cloudcache.IsValidation(err):
		code = common_err.CLIENT_ERROR
	case errors.As(err, &busy):
		code = http.StatusConflict
	}
	c.JSON(code, model.Result{Success: code, Message: common_err.GetMsg(code), Data: err.Error()})
}

// GET /v1/cloud/cache
func GetCloudCache(c *gin.Context) {
	cacheOK(c, service.GetCloudCacheStatus())
}

// PUT /v1/cloud/cache[?dry_run=1] {mode, max_size, max_age, dir}
func PutCloudCache(c *gin.Context) {
	var req service.CacheSettingsWire
	if err := c.ShouldBindJSON(&req); err != nil {
		cacheFail(c, &cloudcache.ValidationError{Msg: "Bad request: " + err.Error()})
		return
	}
	dry := c.Query("dry_run") == "1" || c.Query("dry_run") == "true"
	warnings, err := service.ApplyCloudCache(req, dry)
	if err != nil {
		cacheFail(c, err)
		return
	}
	if warnings == nil {
		warnings = []string{}
	}
	cacheOK(c, gin.H{"warnings": warnings, "status": service.GetCloudCacheStatus()})
}

type cacheClearReq struct {
	Name string `json:"name"` // account; empty = all
}

// POST /v1/cloud/cache/clear {name?}
func PostCloudCacheClear(c *gin.Context) {
	var req cacheClearReq
	_ = c.ShouldBindJSON(&req)
	res, err := service.ClearCloudCache(req.Name)
	if err != nil {
		cacheFail(c, err)
		return
	}
	cacheOK(c, res)
}

type stuckReq struct {
	Account string `json:"account" binding:"required"`
	File    string `json:"file" binding:"required"`
	Folder  string `json:"folder"`
}

// POST /v1/cloud/cache/stuck/:action {account, file, folder?}
// action: retry | discard | save
func PostCloudCacheStuck(c *gin.Context) {
	var req stuckReq
	if err := c.ShouldBindJSON(&req); err != nil || !cloudcache.SafeRel(req.File) {
		cacheFail(c, &cloudcache.ValidationError{Msg: "Bad request"})
		return
	}
	switch c.Param("action") {
	case "retry":
		if err := service.RetryStuckUpload(req.Account, req.File); err != nil {
			cacheFail(c, err)
			return
		}
		cacheOK(c, gin.H{})
	case "discard":
		if err := service.DiscardStuckUpload(req.Account, req.File); err != nil {
			cacheFail(c, err)
			return
		}
		cacheOK(c, gin.H{})
	case "save":
		res, err := service.SaveStuckUpload(req.Account, req.File, req.Folder)
		if err != nil {
			cacheFail(c, err)
			return
		}
		cacheOK(c, res)
	default:
		cacheFail(c, &cloudcache.ValidationError{Msg: "Unknown action"})
	}
}
