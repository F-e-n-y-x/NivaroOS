package v1

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/utils/httper"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/gin-gonic/gin"
	"golang.org/x/sys/unix"
)

// statfsUsage reads live block usage for an already-mounted path via
// statfs(2), the same syscall `df` uses - confirmed working against rclone's
// FUSE mounts (they implement statfs from the backend's real quota, e.g.
// Google Drive's About() call) as well as local/cifs mounts. Returns
// ok=false for a path that isn't actually mounted right now, so callers can
// omit usage instead of reporting 0/0.
func statfsUsage(path string) (size, avail, used uint64, ok bool) {
	if path == "" {
		return 0, 0, 0, false
	}
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, 0, false
	}
	bsize := uint64(st.Bsize)
	size = st.Blocks * bsize
	avail = st.Bavail * bsize
	used = size - (st.Bfree * bsize)
	return size, avail, used, true
}

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
		item := httper.MountPoint{
			Fs:         v.Fs,
			Icon:       cloudProviderIcon(t),
			MountPoint: v.MountPoint,
			Name:       name,
			Type:       t,
		}
		if size, avail, used, ok := statfsUsage(v.MountPoint); ok {
			item.Size = strconv.FormatUint(size, 10)
			item.Avail = strconv.FormatUint(avail, 10)
			item.Used = strconv.FormatUint(used, 10)
		}
		list = append(list, item)
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
	// Cloud drives live directly under /mnt; anything else isn't one.
	if clean := filepath.Clean(mountPoint); clean != mountPoint || filepath.Dir(clean) != "/mnt" {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: "not a cloud drive mount point"})
		return
	}
	err := service.MyService.Storage().UnmountStorage(mountPoint)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}
	service.MyService.Storage().DeleteConfigByName(strings.ReplaceAll(mountPoint, "/mnt/", ""))
	if fs, err := os.ReadDir(mountPoint); err == nil && len(fs) == 0 {
		os.Remove(mountPoint)
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: "success"})
}
