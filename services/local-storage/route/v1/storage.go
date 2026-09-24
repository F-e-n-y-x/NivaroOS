/*@Author: LinkLeong link@icewhale.com
 *@Date: 2022-07-11 16:02:29
 *@LastEditors: LinkLeong
 *@LastEditTime: 2022-08-17 19:14:50
 *@FilePath: /CasaOS/route/v1/storage.go
 *@Description:
 *Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package v1

import (
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"go.uber.org/zap"

	model1 "github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/local-storage/service/model"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/gin-gonic/gin"
)

func GetStorageList(c *gin.Context) {
	system := c.Query("system")

	blkList := service.MyService.Disk().LSBLK(false)
	foundSystem := false

	storages := []model1.Storages{}
	df, err := service.MyService.Disk().GetSystemDf()
	for _, currentDisk := range blkList {
		// if currentDisk.Tran == "usb" {
		// 	continue
		// }

		tempSystemDisk := false
		children := 1
		// currentDisk.Tran only ever says "usb" (from lsblk/udev's ID_BUS) -
		// it can't tell a USB2 drive from a USB3 one. classifyUSBTransport
		// reads the real negotiated link speed from sysfs and reports "usb3"
		// when it's SuperSpeed or faster, so the Files app can show the
		// right port-speed icon; falls back to plain "usb" if that can't be
		// determined, and passes every non-USB transport through unchanged.
		diskType := currentDisk.Tran
		if diskType == "usb" {
			diskType = service.MyService.Disk().ClassifyUSBTransport(currentDisk.Path)
		}
		tempDisk := model1.Storages{
			DiskName: currentDisk.Model,
			Path:     currentDisk.Path,
			Size:     currentDisk.Size,
			Type:     diskType,
		}

		storageArr := []model1.Storage{}
		temp := service.MyService.Disk().SmartCTL(currentDisk.Path)
		if reflect.DeepEqual(temp, model1.SmartctlA{}) {
			temp.SmartStatus.Passed = true
		}
		if len(currentDisk.Children) == 0 && service.IsDiskSupported(currentDisk) {
			currentDisk.Children = append(currentDisk.Children, currentDisk)
		}
		for _, blkChild := range currentDisk.Children {
			if err == nil {
				if blkChild.Path == df.FileSystem {
					tempDisk.DiskName = "System"
					foundSystem = true
					tempSystemDisk = true
					logger.Info("found system disk", zap.String("disk", blkChild.Path))
				}
			}
			if blkChild.MountPoint == "" {
				continue
			}
			if !foundSystem {
				if blkChild.MountPoint == "/" {
					tempDisk.DiskName = "System"
					foundSystem = true
					tempSystemDisk = true
				} else {
					for _, c := range blkChild.Children {
						if c.MountPoint == "/" {
							tempDisk.DiskName = "System"
							foundSystem = true
							tempSystemDisk = true
							break
						}
					}
				}
			}
			stor := model1.Storage{
				UUID:        blkChild.UUID,
				MountPoint:  blkChild.MountPoint,
				Size:        blkChild.FSSize.String(),
				Avail:       blkChild.FSAvail.String(),
				Used:        blkChild.FSUsed.String(),
				Path:        blkChild.Path,
				Type:        blkChild.FsType,
				DriveName:   blkChild.Name,
				PersistedIn: service.MyService.Disk().GetPersistentTypeByUUID(blkChild.UUID),
			}
			if len(blkChild.Label) == 0 {
				if stor.MountPoint == "/" {
					stor.Label = "System"
				} else {
					stor.Label = filepath.Base(stor.MountPoint)
				}

				children++
			} else {
				stor.Label = blkChild.Label
			}
			//if _, ok := mapdb[stor.MountPoint]; ok || stor.Label == "System" {
			storageArr = append(storageArr, stor)
			//}

		}

		if len(storageArr) == 0 {
			continue
		}

		if tempSystemDisk && len(system) > 0 {
			tempStorageArr := []model1.Storage{}
			for i := 0; i < len(storageArr); i++ {
				if storageArr[i].MountPoint != "/boot/efi" && storageArr[i].Type != "swap" {
					tempStorageArr = append(tempStorageArr, storageArr[i])
				}
			}
			tempDisk.Children = tempStorageArr
			logger.Info("system disk", zap.Any("disk", tempDisk))
			storages = append(storages, tempDisk)
			logger.Info("system disk", zap.Any("storages", storages))
		} else if !tempSystemDisk {
			tempDisk.Children = storageArr
			storages = append(storages, tempDisk)
		}
	}

	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: storages})
}

type addStorageRequest struct {
	Path   string `json:"path" form:"path"`
	Name   string `json:"name" form:"name"`
	Format bool   `json:"format" form:"format"`
}

type formatStorageRequest struct {
	Path   string `json:"path" form:"path"`
	Volume string `json:"volume" form:"volume"`
}

type deleteStorageRequest struct {
	Path   string `json:"path" form:"path"`
	Volume string `json:"volume" form:"volume"`
}

func badRequest(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: msg})
}

// respondGuardError maps a guard/validation error to 400 (bad input) or 409
// (refused because of the disk's state). Returns false if err is neither.
func respondGuardError(c *gin.Context, err error) bool {
	switch {
	case err == nil:
		return false
	case service.IsInvalidDeviceError(err):
		badRequest(c, err.Error())
	case service.IsGuardError(err):
		c.JSON(http.StatusConflict, model.Result{Success: common_err.COMMAND_ERROR_INVALID_OPERATION, Message: err.Error()})
	default:
		return false
	}
	return true
}

func respondBusy(c *gin.Context, disk string) {
	c.JSON(http.StatusConflict, model.Result{Success: common_err.DISK_BUSYING, Message: "another operation is already running on " + disk})
}

// wantSync: ?sync=1 keeps the old blocking behaviour (200 when finished).
func wantSync(c *gin.Context) bool {
	v := c.Query("sync")
	return v == "1" || v == "true"
}

// startOrRun runs a storage job synchronously (sync=1) or in the background
// (202 {job_id}). release is always called exactly once when run finishes.
func startOrRun(c *gin.Context, kind, path string, run service.JobRun, release func()) {
	if wantSync(c) {
		mp, err := run(func(step string) { logger.Info("storage "+kind, zap.String("path", path), zap.String("step", step)) })
		release()
		if err != nil {
			c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
			return
		}
		c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: map[string]string{"mount_point": mp}})
		return
	}
	job := service.StorageJobs.Start(kind, path, run, release)
	c.JSON(http.StatusAccepted, model.Result{Success: common_err.SUCCESS, Message: "accepted", Data: map[string]interface{}{"job_id": job.ID, "job": job}})
}

// @Summary create storage: partition+format a whole available disk (format=true) or mount it
// @Router /storage [post]
// 202 {job_id} (or 200 with ?sync=1); 400 invalid input; 409 refused/busy.
func PostAddStorage(c *gin.Context) {
	var req addStorageRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS), Data: err.Error()})
		return
	}
	if req.Path == "" {
		badRequest(c, "path is required")
		return
	}
	if err := model1.ValidateStorageName(req.Name); err != nil {
		badRequest(c, err.Error())
		return
	}

	op := service.DiskOpMount
	if req.Format {
		op = service.DiskOpCreate
	}
	disk, _, err := service.MyService.Disk().GuardDiskOperation(req.Path, op)
	if respondGuardError(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}
	if !service.DiskBusy.TryAcquire(disk.Path, "create") {
		respondBusy(c, disk.Path)
		return
	}
	path, name, format := req.Path, req.Name, req.Format
	startOrRun(c, "create", path, func(progress func(string)) (string, error) {
		return createStorage(path, name, format, progress)
	}, func() {
		service.DiskBusy.Release(disk.Path)
		service.MyService.Disk().RemoveLSBLKCache()
	})
}

// waitFor polls cond every second until it is true or timeout passes.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Second)
	}
}

func createStorage(path, name string, format bool, progress func(string)) (string, error) {
	currentDisk := service.MyService.Disk().GetDiskInfo(path)
	if format {
		progress("unmounting")
		if err := service.MyService.Disk().UmountPointAndRemoveDir(currentDisk); err != nil {
			logger.Error("error when trying to umount storage", zap.Error(err), zap.String("path", path))
			return "", err
		}

		progress("deleting partitions")
		logger.Info("deleting storage...", zap.String("path", path))
		if err := service.MyService.Disk().DeletePartition(path); err != nil {
			logger.Error("error when trying to delete partition", zap.Error(err), zap.String("path", path))
			return "", err
		}

		progress("creating partition and filesystem")
		logger.Info("formatting storage...", zap.String("path", path))
		if err := service.MyService.Disk().AddPartition(path); err != nil {
			return "", err
		}
	}
	currentDisk = service.MyService.Disk().GetDiskInfo(path)
	if len(currentDisk.Children) == 0 && service.IsDiskSupported(currentDisk) {
		currentDisk.Children = append(currentDisk.Children, currentDisk)
	}

	var mounted []string
	for _, blkChild := range currentDisk.Children {
		mountPoint, err := blkChild.GetMountPoint(name)
		if err != nil {
			return strings.Join(mounted, ","), err
		}

		progress("mounting " + blkChild.Path)
		if output, err := service.MyService.Disk().MountDisk(blkChild.Path, mountPoint); err != nil {
			logger.Error("err", zap.Error(err), zap.String("output", output), zap.String("mount point", mountPoint))
			if strings.TrimSpace(output) != "" {
				return strings.Join(mounted, ","), fmt.Errorf("%s: %s", err.Error(), strings.TrimSpace(output))
			}
			return strings.Join(mounted, ","), err
		}

		// lsblk may not report the new UUID right away
		var b model1.LSBLKModel
		waitFor(10*time.Second, func() bool {
			b = service.MyService.Disk().GetDiskInfo(blkChild.Path)
			return b.UUID != ""
		})
		if b.MountPoint != "" {
			mountPoint = b.MountPoint
		}

		m := model2.Volume{
			MountPoint: mountPoint,
			UUID:       b.UUID,
			CreatedAt:  time.Now().Unix(),
		}
		if err := service.MyService.Disk().SaveMountPointToDB(m); err != nil {
			return strings.Join(mounted, ","), err
		}
		mounted = append(mounted, mountPoint)

		// send notify to client
		go func(blkChild model1.LSBLKModel) {
			message := map[string]interface{}{
				"data": StorageMessage{
					Action: "ADDED",
					Path:   blkChild.Path,
					Volume: "/mnt/",
					Size:   blkChild.Size,
					Type:   blkChild.Tran,
				},
			}

			if err := service.MyService.Notify().SendNotify(messagePathStorageStatus, message); err != nil {
				logger.Error("error when sending notification", zap.Error(err), zap.String("message path", messagePathStorageStatus), zap.Any("message", message))
			}
		}(blkChild)
	}
	return strings.Join(mounted, ","), nil
}

// @Summary reformat one storage partition and mount it again
// @Param  path formData string true "partition, e.g. /dev/sdb1"
// @Param  volume formData string false "mount point (/mnt|/media|/DATA/<name>); generated if empty"
// @Router /storage [put]
// 202 {job_id} (or 200 with ?sync=1); 400 invalid input; 409 refused/busy.
func PutFormatStorage(c *gin.Context) {
	var req formatStorageRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS), Data: err.Error()})
		return
	}
	if req.Path == "" {
		badRequest(c, "path is required")
		return
	}
	if req.Volume != "" {
		if err := model1.ValidateMountPoint(req.Volume); err != nil {
			badRequest(c, err.Error())
			return
		}
	}

	disk, _, err := service.MyService.Disk().GuardDiskOperation(req.Path, service.DiskOpFormat)
	if respondGuardError(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}
	if !service.DiskBusy.TryAcquire(disk.Path, "format") {
		respondBusy(c, disk.Path)
		return
	}
	path, mountPoint := req.Path, req.Volume
	startOrRun(c, "format", path, func(progress func(string)) (string, error) {
		return formatStorage(path, mountPoint, progress)
	}, func() {
		service.DiskBusy.Release(disk.Path)
		service.MyService.Disk().RemoveLSBLKCache()
	})
}

func formatStorage(path, mountPoint string, progress func(string)) (string, error) {
	diskInfo := service.MyService.Disk().GetDiskInfo(path)
	progress("unmounting")
	if err := service.MyService.Disk().UmountPointAndRemoveDir(diskInfo); err != nil {
		return "", err
	}

	progress("formatting")
	if err := service.MyService.Disk().FormatDisk(path); err != nil {
		// used to fall through (no return) and then spin forever waiting
		// for a UUID change that never came
		return "", fmt.Errorf("%s: %w", common_err.GetMsg(common_err.FORMAT_ERROR), err)
	}

	progress("waiting for the new filesystem")
	var currentDisk model1.LSBLKModel
	if !waitFor(60*time.Second, func() bool {
		currentDisk = service.MyService.Disk().GetDiskInfo(path)
		return currentDisk.UUID != "" && currentDisk.UUID != diskInfo.UUID
	}) {
		return "", fmt.Errorf("%s was formatted, but its new filesystem did not show up within 60s", path)
	}
	if mountPoint == "" {
		mp, err := currentDisk.GetMountPoint("")
		if err != nil {
			return "", err
		}
		mountPoint = mp
	}

	progress("mounting")
	if output, err := service.MyService.Disk().MountDisk(path, mountPoint); err != nil {
		if strings.TrimSpace(output) != "" {
			return "", fmt.Errorf("%s: %s", err.Error(), strings.TrimSpace(output))
		}
		return "", err
	}
	if after := service.MyService.Disk().GetDiskInfo(path); after.MountPoint != "" {
		mountPoint = after.MountPoint
	}

	m := model2.Volume{
		MountPoint: mountPoint,
		UUID:       currentDisk.UUID,
		CreatedAt:  time.Now().Unix(),
	}
	if err := service.MyService.Disk().SaveMountPointToDB(m); err != nil {
		return "", err
	}
	return mountPoint, nil
}

// @Summary status of an async storage job (create/format)
// @Router /storage/jobs/{id} [get]
func GetStorageJob(c *gin.Context) {
	job, ok := service.StorageJobs.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.CLIENT_ERROR, Message: "job not found (finished jobs are kept for an hour)"})
		return
	}
	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: job})
}

func DeleteStorage(c *gin.Context) {
	var req deleteStorageRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS), Data: err.Error()})
		return
	}

	path := req.Path
	mountPoint := req.Volume

	if len(path) == 0 || len(mountPoint) == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}

	disk, _, err := service.MyService.Disk().GuardDiskOperation(path, service.DiskOpUmount)
	if respondGuardError(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}
	if !service.DiskBusy.TryAcquire(disk.Path, "umount") {
		respondBusy(c, disk.Path)
		return
	}
	defer service.DiskBusy.Release(disk.Path)

	diskInfo := service.MyService.Disk().GetDiskInfo(path)
	if err := service.MyService.Disk().UmountPointAndRemoveDir(diskInfo); err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.REMOVE_MOUNT_POINT_ERROR, Message: err.Error()})
		return
	}

	// delete data
	defer func() {
		if err := service.MyService.Disk().DeleteMountPointFromDB(path, mountPoint); err != nil {
			logger.Error("error when deleting mount point from database", zap.Error(err))
		}
	}()
	defer service.MyService.Disk().RemoveLSBLKCache()

	// send notify to client
	go func() {
		message := map[string]interface{}{
			"data": StorageMessage{
				Action: "REMOVED",
				Path:   path,
				Volume: mountPoint,
				Size:   0,
				Type:   "",
			},
		}

		if err := service.MyService.Notify().SendNotify(messagePathStorageStatus, message); err != nil {
			logger.Error("error when sending notification", zap.Error(err), zap.String("message path", messagePathStorageStatus), zap.Any("message", message))
		}
	}()

	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}
