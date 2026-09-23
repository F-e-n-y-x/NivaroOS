package v1

import (
	"net/http"

	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/trash"
	"github.com/labstack/echo/v4"
)

type trashIDs struct {
	IDs []string `json:"ids"`
}

func trashOK(ctx echo.Context, data interface{}) error {
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

// GetTrash lists everything in the Trash (every drive that's present).
func GetTrash(ctx echo.Context) error {
	items := service.Trash.List()
	var bytes int64
	for _, it := range items {
		bytes += it.Size
	}
	return trashOK(ctx, map[string]interface{}{"items": items, "count": len(items), "bytes": bytes, "retention_days": int(service.TrashRetention.Hours() / 24)})
}

func PostTrashRestore(ctx echo.Context) error {
	var req trashIDs
	if err := ctx.Bind(&req); err != nil || len(req.IDs) == 0 {
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: "no items given"})
	}
	return trashOK(ctx, service.Trash.Restore(req.IDs))
}

// DeleteTrashItems removes items from the Trash for good.
func DeleteTrashItems(ctx echo.Context) error {
	var req trashIDs
	if err := ctx.Bind(&req); err != nil || len(req.IDs) == 0 {
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: "no items given"})
	}
	return trashOK(ctx, map[string]interface{}{"failed": service.Trash.Delete(req.IDs)})
}

func DeleteTrashAll(ctx echo.Context) error {
	return trashOK(ctx, map[string]interface{}{"failed": service.Trash.Empty()})
}

// GetTrashSupport tells the UI whether deleting in a folder goes to the
// Trash or is permanent (cloud drives, phones, network shares).
func GetTrashSupport(ctx echo.Context) error {
	p := ctx.QueryParam("path")
	dev, _ := GetCompanionDeviceByStoragePath(p)
	supported := p != "" && dev == nil && !trash.IsTrashPath(p) && trash.SupportsTrash(p)
	return trashOK(ctx, map[string]bool{"supported": supported})
}
