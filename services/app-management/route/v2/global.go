package v2

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/service"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/samber/lo"
	"go.uber.org/zap"

	"github.com/labstack/echo/v4"
)

func (a *AppManagement) GetGlobalSettings(ctx echo.Context) error {
	global := config.GlobalSnapshot()

	keys := lo.Keys(global)
	sort.Strings(keys)

	result := make([]codegen.GlobalSetting, 0, len(keys))
	for _, key := range keys {
		result = append(result, codegen.GlobalSetting{
			Key:         utils.Ptr(key),
			Value:       global[key],
			Description: utils.Ptr(key),
		})
	}

	return ctx.JSON(http.StatusOK, codegen.GlobalSettingsOK{
		Data: &result,
	})
}

func (a *AppManagement) GetGlobalSetting(ctx echo.Context, key codegen.GlobalSettingKey) error {
	value, ok := config.GetGlobal(string(key))
	if !ok {
		message := "the key is not exist"
		return ctx.JSON(http.StatusNotFound, codegen.ResponseNotFound{Message: &message})
	}

	return ctx.JSON(http.StatusOK, codegen.GlobalSettingOK{
		Data: &codegen.GlobalSetting{
			Key:         utils.Ptr(key),
			Value:       value,
			Description: utils.Ptr(key),
		},
	})
}

func (a *AppManagement) UpdateGlobalSetting(ctx echo.Context, key codegen.GlobalSettingKey) error {
	var action codegen.GlobalSetting
	if err := ctx.Bind(&action); err != nil {
		message := err.Error()
		return ctx.JSON(http.StatusBadRequest, codegen.ResponseBadRequest{Message: &message})
	}
	if err := updateGlobalEnv(ctx, key, action.Value); err != nil {
		message := err.Error()
		return ctx.JSON(http.StatusBadRequest, codegen.ResponseBadRequest{Message: &message})
	}

	return ctx.JSON(http.StatusOK, codegen.GlobalSettingOK{
		Data: &codegen.GlobalSetting{
			Key:         utils.Ptr(key),
			Value:       action.Value,
			Description: utils.Ptr(key),
		},
	})
}

func updateGlobalEnv(ctx echo.Context, key string, value string) error {
	if key == "" {
		return fmt.Errorf("openai api key is required")
	}

	// rejects keys that are not env var names and values with line breaks
	if err := config.ValidateGlobal(key, value); err != nil {
		return err
	}

	if err := service.MyService.AppStoreManagement().ChangeGlobal(key, value); err != nil {
		return err
	}

	// the echo context is recycled once the handler returns - copy what the
	// goroutine needs now
	go reapplyGlobalEnv(PropertiesFromQueryParams(ctx))

	return nil
}

func deleteGlobalEnv(ctx echo.Context, key string) error {
	if err := service.MyService.AppStoreManagement().DeleteGlobal(key); err != nil {
		return err
	}

	go reapplyGlobalEnv(PropertiesFromQueryParams(ctx))

	return nil
}

// isRunning reports whether any container of a compose app is running.
func isRunning(containers []api.ContainerSummary) bool {
	return lo.SomeBy(containers, func(c api.ContainerSummary) bool { return c.State == "running" })
}

// reapplyGlobalEnv recreates apps so a changed global setting takes effect -
// only apps that are running now: an app the user stopped stays stopped.
func reapplyGlobalEnv(properties map[string]string) {
	backgroundCtx := common.WithProperties(context.Background(), properties)

	composeApps, err := service.MyService.Compose().List(backgroundCtx)
	if err != nil {
		logger.Error("Failed to get composeAppsWithStoreInfo", zap.Any("error", err))
		return
	}

	apiService, dockerClient, err := service.ApiService()
	if err != nil {
		logger.Error("Failed to get Api Service", zap.Any("error", err))
		return
	}
	defer dockerClient.Close()

	for name, project := range composeApps {
		containers, err := apiService.Ps(backgroundCtx, name, api.PsOptions{All: true})
		if err != nil {
			logger.Error("failed to get compose app containers", zap.Error(err), zap.String("name", name))
			continue
		}

		if !isRunning(containers) {
			continue
		}

		if err := project.UpWithCheckRequire(backgroundCtx, apiService); err != nil {
			logger.Error("failed to re-apply global settings to compose app", zap.Error(err), zap.String("name", name))
		}
	}
}

func (a *AppManagement) DeleteGlobalSetting(ctx echo.Context, key codegen.GlobalSettingKey) error {
	var action codegen.GlobalSetting
	if err := ctx.Bind(&action); err != nil {
		message := err.Error()
		return ctx.JSON(http.StatusBadRequest, codegen.ResponseBadRequest{Message: &message})
	}

	if err := deleteGlobalEnv(ctx, key); err != nil {
		message := err.Error()
		return ctx.JSON(http.StatusBadRequest, codegen.ResponseBadRequest{Message: &message})
	}

	return ctx.JSON(http.StatusOK, codegen.ResponseOK{})
}
