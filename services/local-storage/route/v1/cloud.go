package v1

import (
	"os"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/utils/httper"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/gin-gonic/gin"
)

func ListStorages(c *gin.Context) {
	// var req model.PageReq
	// if err := c.ShouldBind(&req); err != nil {
	// 	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: err.Error()})
	// 	return
	// }
	// req.Validate()

	//logger.Info("ListStorages", zap.Any("req", req))
	//storages, total, err := service.MyService.Storage().GetStorages(req.Page, req.PerPage)
	// if err != nil {
	// 	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	// 	return
	// }
	// c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: model.PageResp{
	// 	Content: storages,
	// 	Total:   total,
	// }})
	r, err := service.MyService.Storage().GetStorages()

	if err != nil {
		c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}

	list := []httper.MountPoint{}

	for _, v := range r.MountPoints {
		t := service.MyService.Storage().GetAttributeValueByName(v.Fs, "type")
		name := service.MyService.Storage().GetAttributeValueByName(v.Fs, "username")
		list = append(list, httper.MountPoint{
			Fs:         v.Fs,
			Icon:       cloudProviderIcon(t),
			MountPoint: v.MountPoint,
			Name:       name,
			Type:       t,
		})
	}

	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: list})
}

func UmountStorage(c *gin.Context) {
	json := make(map[string]string)
	c.ShouldBind(&json)
	mountPoint := json["mount_point"]
	if mountPoint == "" {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "mount_point is empty"})
		return
	}
	err := service.MyService.Storage().UnmountStorage(mountPoint)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}
	service.MyService.Storage().DeleteConfigByName(strings.ReplaceAll(mountPoint, "/mnt/", ""))
	if fs, err := os.ReadDir(mountPoint); err == nil && len(fs) == 0 {
		os.RemoveAll(mountPoint)
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: "success"})
}
