package v1

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
)

func GetSchedules(ctx echo.Context) error {
	tasks := service.MyService.Schedule().GetTasks()
	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    tasks,
	})
}

func GetSchedule(ctx echo.Context) error {
	id := ctx.Param("id")
	task, err := service.MyService.Schedule().GetTask(id)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: err.Error(),
		})
	}
	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    task,
	})
}

func CreateSchedule(ctx echo.Context) error {
	var task service.ScheduleTask
	if err := ctx.Bind(&task); err != nil {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: err.Error(),
		})
	}

	created, err := service.MyService.Schedule().CreateTask(task)
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    created,
	})
}

func UpdateSchedule(ctx echo.Context) error {
	id := ctx.Param("id")
	var task service.ScheduleTask
	if err := ctx.Bind(&task); err != nil {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: err.Error(),
		})
	}

	updated, err := service.MyService.Schedule().UpdateTask(id, task)
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    updated,
	})
}

func DeleteSchedule(ctx echo.Context) error {
	id := ctx.Param("id")
	if err := service.MyService.Schedule().DeleteTask(id); err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: err.Error(),
		})
	}
	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
	})
}

type toggleReq struct {
	Enabled bool `json:"enabled"`
}

func ToggleSchedule(ctx echo.Context) error {
	id := ctx.Param("id")
	var req toggleReq
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: err.Error(),
		})
	}

	task, err := service.MyService.Schedule().ToggleTask(id, req.Enabled)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    task,
	})
}

func RunScheduleNow(ctx echo.Context) error {
	id := ctx.Param("id")
	msg, err := service.MyService.Schedule().RunTaskNow(id)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: msg,
	})
}

func GetScheduleTargets(ctx echo.Context) error {
	targets, err := service.MyService.Schedule().GetTargets()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: err.Error(),
		})
	}

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    targets,
	})
}
