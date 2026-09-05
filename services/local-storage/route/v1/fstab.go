package v1

import (
	"net/http"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"go.uber.org/zap"

	model1 "github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"

	"github.com/gin-gonic/gin"
)

// fstabError unwraps a *service.FstabAPIError (if that's what err is) into the
// common_err code/message pair it carries; any other error falls back to a generic
// 500/SERVICE_ERROR so a handler never has to repeat this switch itself.
func fstabError(c *gin.Context, err error) {
	if apiErr, ok := err.(*service.FstabAPIError); ok {
		status := http.StatusBadRequest
		switch apiErr.Code {
		case common_err.FSTAB_ENTRY_NOT_FOUND, common_err.FSTAB_DEVICE_NOT_FOUND:
			status = http.StatusNotFound
		case common_err.FSTAB_ENTRY_EXISTS:
			status = http.StatusConflict
		}
		c.JSON(status, model.Result{Success: apiErr.Code, Message: apiErr.Message})
		return
	}

	logger.Error("fstab request failed", zap.Error(err))
	c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
}

// GET /v1/storage/fstab - managed entries plus, for context, read-only system entries.
func GetFstabMounts(c *gin.Context) {
	managed, err := service.MyService.Disk().ListFstabMounts()
	if err != nil {
		fstabError(c, err)
		return
	}

	system, err := service.MyService.Disk().ListFstabSystemEntries()
	if err != nil {
		fstabError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data: gin.H{
			"managed": managed,
			"system":  system,
		},
	})
}

// GET /v1/storage/fstab/candidates - drives that can be added.
func GetFstabCandidates(c *gin.Context) {
	candidates, err := service.MyService.Disk().ListFstabCandidates()
	if err != nil {
		fstabError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: candidates})
}

// POST /v1/storage/fstab
func PostFstabMount(c *gin.Context) {
	var req model1.AddFstabMountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS), Data: err.Error()})
		return
	}

	mount, err := service.MyService.Disk().AddFstabMount(req)
	if err != nil {
		fstabError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: mount})
}

// PUT /v1/storage/fstab
func PutFstabMount(c *gin.Context) {
	var req model1.UpdateFstabMountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS), Data: err.Error()})
		return
	}

	mount, err := service.MyService.Disk().UpdateFstabMount(req)
	if err != nil {
		fstabError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: mount})
}

// DELETE /v1/storage/fstab?mount_point=...
func DeleteFstabMount(c *gin.Context) {
	mountPoint := c.Query("mount_point")
	if mountPoint == "" {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}

	if err := service.MyService.Disk().RemoveFstabMount(mountPoint); err != nil {
		fstabError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// PUT /v1/storage/fstab/enabled
func PutFstabMountEnabled(c *gin.Context) {
	var req model1.SetFstabMountEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS), Data: err.Error()})
		return
	}

	if req.MountPoint == "" {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}

	if err := service.MyService.Disk().SetFstabMountEnabled(req.MountPoint, req.Enabled); err != nil {
		fstabError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

type fstabActionRequest struct {
	MountPoint string `json:"mount_point"`
}

// POST /v1/storage/fstab/mount
func PostFstabMountAction(c *gin.Context) {
	var req fstabActionRequest
	_ = c.ShouldBindJSON(&req)
	if req.MountPoint == "" {
		req.MountPoint = c.Query("mount_point")
	}
	if req.MountPoint == "" {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}

	if err := service.MyService.Disk().MountFstabEntry(req.MountPoint); err != nil {
		fstabError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// POST /v1/storage/fstab/umount
func PostFstabUmountAction(c *gin.Context) {
	var req fstabActionRequest
	_ = c.ShouldBindJSON(&req)
	if req.MountPoint == "" {
		req.MountPoint = c.Query("mount_point")
	}
	if req.MountPoint == "" {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}

	if err := service.MyService.Disk().UmountFstabEntry(req.MountPoint); err != nil {
		fstabError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// POST /v1/storage/fstab/adopt
func PostFstabAdoptAction(c *gin.Context) {
	var req fstabActionRequest
	_ = c.ShouldBindJSON(&req)
	if req.MountPoint == "" {
		req.MountPoint = c.Query("mount_point")
	}
	if req.MountPoint == "" {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}

	mount, err := service.MyService.Disk().AdoptFstabEntry(req.MountPoint)
	if err != nil {
		fstabError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: mount})
}

