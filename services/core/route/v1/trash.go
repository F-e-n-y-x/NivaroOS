package v1

import (
	"net/http"

	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/trash"
	"github.com/labstack/echo/v5"
)

type trashIDs struct {
	IDs []string `json:"ids"`
}

func trashOK(ctx *echo.Context, data interface{}) error {
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

// GetTrash lists everything in the Trash: every drive that's present,
// network shares and cloud drives, and the caller's phones (an offline
// phone's items marked unavailable). bytes counts this server's own disks
// only - a share's, cloud drive's or phone's trash takes no space here.
func GetTrash(ctx *echo.Context) error {
	items := append(service.Trash.List(), companionTrashList(companionCaller(ctx))...)
	sortTrash(items)
	var bytes int64
	for _, it := range items {
		if it.Kind == trash.KindDisk {
			bytes += it.Size
		}
	}
	return trashOK(ctx, map[string]interface{}{"items": items, "count": len(items), "bytes": bytes, "retention_days": int(service.TrashRetention.Hours() / 24)})
}

func PostTrashRestore(ctx *echo.Context) error {
	var req trashIDs
	if err := ctx.Bind(&req); err != nil || len(req.IDs) == 0 {
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: "no items given"})
	}
	uid := companionCaller(ctx)
	phone, rest := pickCompanionTrash(req.IDs, uid)
	res := trash.RestoreResult{Restored: []trash.Restored{}, Failed: []trash.Failure{}}
	if len(rest) > 0 {
		res = service.Trash.Restore(rest)
	}
	for _, rec := range phone {
		if p, err := restoreCompanionTrash(rec, uid); err != nil {
			res.Failed = append(res.Failed, trash.Failure{ID: rec.ID, Name: rec.Name, Error: err.Error()})
		} else {
			res.Restored = append(res.Restored, trash.Restored{ID: rec.ID, Path: p})
		}
	}
	return trashOK(ctx, res)
}

func deleteFromTrash(uid string, ids []string) []trash.Failure {
	phone, rest := pickCompanionTrash(ids, uid)
	failed := []trash.Failure{}
	if len(rest) > 0 {
		failed = service.Trash.Delete(rest)
	}
	for _, rec := range phone {
		if err := deleteCompanionTrash(rec, uid); err != nil {
			failed = append(failed, trash.Failure{ID: rec.ID, Name: rec.Name, Error: err.Error()})
		}
	}
	return failed
}

// DeleteTrashItems removes items from the Trash for good.
func DeleteTrashItems(ctx *echo.Context) error {
	var req trashIDs
	if err := ctx.Bind(&req); err != nil || len(req.IDs) == 0 {
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: "no items given"})
	}
	return trashOK(ctx, map[string]interface{}{"failed": deleteFromTrash(companionCaller(ctx), req.IDs)})
}

func DeleteTrashAll(ctx *echo.Context) error {
	uid := companionCaller(ctx)
	failed := service.Trash.Empty()
	var ids []string
	for _, it := range companionTrashList(uid) {
		ids = append(ids, it.ID)
	}
	if len(ids) > 0 {
		failed = append(failed, deleteFromTrash(uid, ids)...)
	}
	return trashOK(ctx, map[string]interface{}{"failed": failed})
}

// GetTrashSupport tells the UI what deleting in a folder does: into the
// Trash, into a cloud provider's own trash, or permanently (and why).
func GetTrashSupport(ctx *echo.Context) error {
	p := ctx.QueryParam("path")
	if p == "" {
		return trashOK(ctx, trash.Support{Kind: trash.KindDisk, Reason: trash.ReasonNoTrash})
	}
	if dev, phonePath := GetCompanionDeviceByStoragePath(p); dev != nil {
		return trashOK(ctx, trash.Support{Supported: !trash.IsTrashPath(phonePath), Kind: trash.KindPhone})
	}
	return trashOK(ctx, trash.SupportsTrash(p))
}
