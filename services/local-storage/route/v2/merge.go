package v2

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/sys/mountinfo"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/constants"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/utils/merge"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/local-storage/service/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service/v2/fs"

	"github.com/labstack/echo/v4"
)

var MessageMergerFSNotEnabled = "mergerfs is not enabled - either it is not enabled in configuration file; merge point is not empty before mounting; or mergerfs is not installed"

func (s *LocalStorage) GetMerges(ctx echo.Context, params codegen.GetMergesParams) error {
	if strings.ToLower(config.ServerInfo.EnableMergerFS) != "true" {
		return ctx.JSON(http.StatusServiceUnavailable, codegen.ResponseServiceUnavailable{Message: &MessageMergerFSNotEnabled})
	}

	merges, err := service.MyService.LocalStorage().GetMerges(params.MountPoint)
	if err != nil {
		message := err.Error()
		return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
	}
	data := make([]codegen.Merge, 0, len(merges))
	for _, merge := range merges {
		data = append(data, MergeAdapterOut(merge))
	}
	message := "ok"
	return ctx.JSON(http.StatusOK, codegen.GetMergesResponseOK{Data: &data, Message: &message})

}

func (s *LocalStorage) SetMerge(ctx echo.Context) error {
	var m codegen.Merge
	if err := ctx.Bind(&m); err != nil {
		message := err.Error()
		return ctx.JSON(http.StatusBadRequest, codegen.ResponseBadRequest{Message: &message})
	}

	// default to mergerfs if fstype is not specified
	fstype := fs.MergerFSFullName
	if m.Fstype != nil {
		fstype = *m.Fstype
	}

	// expand source volume paths to source volumes
	var sourceVolumes []*model2.Volume
	if m.SourceVolumeUuids != nil {
		volumesFromDB, err := service.MyService.Disk().GetSerialAllFromDB()
		if err != nil {
			message := err.Error()
			return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
		}

		sourceVolumes = make([]*model2.Volume, 0, len(*m.SourceVolumeUuids))
		for _, volumeUUID := range *m.SourceVolumeUuids {
			volumeFound := false
			for i := range volumesFromDB {
				if volumeUUID == volumesFromDB[i].UUID {
					volumeFound = true
					sourceVolumes = append(sourceVolumes, &volumesFromDB[i])
					break
				}
			}

			if !volumeFound {
				message := "volume " + volumeUUID + " not found, or it is not a NivaroOS storage. Consider adding it to NivaroOS first."
				return ctx.JSON(http.StatusBadRequest, codegen.ResponseBadRequest{Message: &message})
			}
		}
	}

	merge, err := service.MyService.LocalStorage().GetFirstMergeFromDB(m.MountPoint)
	if err != nil {
		message := err.Error()
		return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
	}

	if merge == nil {
		merge = &model2.Merge{
			FSType:         fstype,
			MountPoint:     m.MountPoint,
			SourceBasePath: m.SourceBasePath,
			SourceVolumes:  sourceVolumes,
		}

		if err := service.MyService.LocalStorage().CreateMerge(merge); err != nil {
			message := err.Error()
			return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
		}

		if err := service.MyService.LocalStorage().CreateMergeInDB(merge); err != nil {
			message := err.Error()
			return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
		}
	} else {
		if m.SourceBasePath != nil {
			merge.SourceBasePath = m.SourceBasePath
		}

		if m.SourceVolumeUuids != nil {
			merge.SourceVolumes = sourceVolumes // which come from m.SourceVolumeUuids
		}

		if err := service.MyService.LocalStorage().UpdateMerge(merge); err != nil {
			message := err.Error()
			return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
		}

		if err := service.MyService.LocalStorage().UpdateMergeSourcesInDB(merge); err != nil {
			message := err.Error()
			return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
		}
	}

	result := MergeAdapterOut(*merge)

	return ctx.JSON(http.StatusOK, codegen.SetMergeResponseOK{
		Data: &result,
	})
}
func (s *LocalStorage) GetMergeInitStatus(ctx echo.Context) error {
	if strings.ToLower(config.ServerInfo.EnableMergerFS) != "true" {
		status := codegen.Uninitialized
		return ctx.JSON(http.StatusOK, codegen.GetMergeInitStatusResponseOK{Data: &status})
	} else {
		status := codegen.Initialized
		return ctx.JSON(http.StatusOK, codegen.GetMergeInitStatusResponseOK{Data: &status})
	}
}

// initMergeState is what InitMerge looks at before touching anything.
type initMergeState struct {
	MergerFSInstalled bool
	MountPoint        string // from the client - must be "/DATA"
	DataIsMountpoint  bool
	DataExists        bool
	DataIsRealDir     bool // a directory, not a symlink/file
	DataEmpty         bool
	BaseExists        bool // constants.DefaultFilePath
	BaseEmpty         bool
}

type initMergePlan struct {
	MoveDataToBase  bool // rename /DATA -> DefaultFilePath (its content becomes the pool's base branch)
	RemoveEmptyBase bool // DefaultFilePath exists but is empty: rmdir it first
}

// planInitMerge decides what InitMerge may do. It used to trust the client's
// mount_point, RemoveAll the base dir and rename whatever path was sent,
// and only then check that mergerfs was even installed.
func planInitMerge(st initMergeState) (initMergePlan, int, string) {
	if !st.MergerFSInstalled {
		return initMergePlan{}, http.StatusBadRequest, "mergerfs is not installed"
	}
	if st.MountPoint != "/DATA" {
		return initMergePlan{}, http.StatusBadRequest, "only /DATA can be initialised as the storage pool"
	}
	if st.DataIsMountpoint {
		return initMergePlan{}, http.StatusConflict, "/DATA is already a mount point - unmount it first"
	}
	if st.DataExists && !st.DataIsRealDir {
		return initMergePlan{}, http.StatusConflict, "/DATA is not a regular directory (symlink or file) - refusing to move it"
	}
	if !st.DataExists || st.DataEmpty {
		return initMergePlan{}, 0, ""
	}
	if st.BaseExists && !st.BaseEmpty {
		return initMergePlan{}, http.StatusConflict, "both /DATA and " + constants.DefaultFilePath + " contain files - move one of them out of the way first"
	}
	return initMergePlan{MoveDataToBase: true, RemoveEmptyBase: st.BaseExists}, 0, ""
}

func dirState(path string) (exists, realDir, empty bool) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, false, false
	}
	if !fi.IsDir() {
		return true, false, false
	}
	entries, err := os.ReadDir(path)
	return true, true, err == nil && len(entries) == 0
}

func (s *LocalStorage) InitMerge(ctx echo.Context) error {
	var m codegen.MountPoint
	if err := ctx.Bind(&m); err != nil {
		message := err.Error()
		return ctx.JSON(http.StatusBadRequest, codegen.ResponseBadRequest{Message: &message})
	}

	if strings.ToLower(config.ServerInfo.EnableMergerFS) == "true" {
		status := codegen.Initialized
		return ctx.JSON(http.StatusOK, codegen.InitMergeResponseOK{Data: &status})
	}

	const dataPath = "/DATA"
	base := constants.DefaultFilePath

	st := initMergeState{MergerFSInstalled: merge.IsMergerFSInstalled(), MountPoint: m.MountPoint}
	if mounted, err := mountinfo.Mounted(dataPath); err == nil {
		st.DataIsMountpoint = mounted
	}
	st.DataExists, st.DataIsRealDir, st.DataEmpty = dirState(dataPath)
	var baseReal bool
	st.BaseExists, baseReal, st.BaseEmpty = dirState(base)
	if st.BaseExists && !baseReal {
		st.BaseEmpty = false
	}

	plan, code, message := planInitMerge(st)
	if code != 0 {
		if !st.MergerFSInstalled {
			config.ServerInfo.EnableMergerFS = "false"
		}
		if code == http.StatusConflict {
			return ctx.JSON(code, codegen.ResponseConflict{Message: &message})
		}
		return ctx.JSON(code, codegen.ResponseBadRequest{Message: &message})
	}

	moved := false
	if plan.MoveDataToBase {
		if plan.RemoveEmptyBase {
			// os.Remove only removes an empty directory (was RemoveAll)
			if err := os.Remove(base); err != nil {
				message := "couldn't remove the empty " + base + ": " + err.Error()
				return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
			}
		}
		if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
			message := err.Error()
			return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
		}
		if err := os.Rename(dataPath, base); err != nil {
			message := "move " + dataPath + " to " + base + " failed: " + err.Error()
			return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
		}
		moved = true
	}
	rollback := func() {
		if moved {
			if err := os.Remove(dataPath); err == nil || os.IsNotExist(err) {
				if err := os.Rename(base, dataPath); err != nil {
					fmt.Println("InitMerge rollback failed:", err)
				}
			}
		}
	}

	if err := file.MkDir(dataPath); err != nil {
		rollback()
		message := "create " + dataPath + " failed"
		return ctx.JSON(http.StatusInternalServerError, codegen.BaseResponse{Message: &message})
	}

	if !service.MyService.Disk().EnsureDefaultMergePoint() {
		rollback()
		config.ServerInfo.EnableMergerFS = "false"
		message := "default merge point is not empty"
		return ctx.JSON(http.StatusConflict, codegen.ResponseConflict{Message: &message})
	}

	service.MyService.LocalStorage().CheckMergeMount()

	config.Cfg.Section("server").Key("EnableMergerFS").SetValue("true")
	config.ServerInfo.EnableMergerFS = "true"

	config.Cfg.SaveTo(config.LocalStorageConfigFilePath)

	status := codegen.Initialized
	return ctx.JSON(http.StatusOK, codegen.InitMergeResponseOK{Data: &status})
}

func MergeAdapterOut(m model2.Merge) codegen.Merge {
	id := int(m.ID)

	sourceVolumeUUIDs := make([]string, 0, len(m.SourceVolumes))
	for _, volume := range m.SourceVolumes {
		sourceVolumeUUIDs = append(sourceVolumeUUIDs, volume.UUID)
	}

	return codegen.Merge{
		Id:                &id,
		Fstype:            &m.FSType,
		MountPoint:        m.MountPoint,
		SourceBasePath:    m.SourceBasePath,
		SourceVolumeUuids: &sourceVolumeUUIDs,
		CreatedAt:         &m.CreatedAt,
		UpdatedAt:         &m.UpdatedAt,
	}
}
