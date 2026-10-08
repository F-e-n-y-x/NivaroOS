package v2

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/model"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/service"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/labstack/echo/v5"
	"github.com/samber/lo"
	"go.uber.org/zap"
)

func (a *AppManagement) GetAppGrid(ctx *echo.Context) error {
	// v2 Apps
	composeAppsWithStoreInfo, containersByApp, err := composeAppsWithStoreInfoAndContainers(ctx.Request().Context(), composeAppsWithStoreInfoOpts{
		checkIsUpdateAvailable: false,
	})
	if err != nil {
		message := err.Error()
		logger.Error("failed to list compose apps with store info", zap.Error(err))
		return ctx.JSON(http.StatusInternalServerError, codegen.ResponseInternalServerError{Message: &message})
	}

	v2AppGridItems := lo.FilterMap(lo.Values(composeAppsWithStoreInfo), func(app codegen.ComposeAppWithStoreInfo, i int) (codegen.WebAppGridItem, bool) {
		item, err := WebAppGridItemAdapterV2(&app)
		if err != nil {
			logger.Error("failed to adapt web app grid item", zap.Error(err), zap.String("app", app.Compose.Name))
			return codegen.WebAppGridItem{}, false
		}

		return *item, true
	})

	// v1 Apps
	nivaroosApps, containers := service.MyService.Docker().GetContainerAppList(nil, nil, nil)

	v1AppGridItems := lo.Map(*nivaroosApps, func(app model.MyAppList, i int) codegen.WebAppGridItem {
		item, err := WebAppGridItemAdapterV1(&app)
		if err != nil {
			logger.Error("failed to adapt web app grid item", zap.Error(err), zap.String("app", app.Name))
			return codegen.WebAppGridItem{}
		}
		return *item
	})

	// containers from compose apps (fetched above, once per app; an app whose
	// containers could not be listed is just skipped here)
	composeAppContainers := []codegen.ContainerSummary{}
	for _, containers := range containersByApp {
		composeAppContainers = append(composeAppContainers, containers...)
	}

	containerAppGridItems := lo.FilterMap(*containers, func(app model.MyAppList, i int) (codegen.WebAppGridItem, bool) {
		if lo.ContainsBy(composeAppContainers, func(container codegen.ContainerSummary) bool { return container.ID == app.ID }) {
			// already exists as compose app, skipping...
			return codegen.WebAppGridItem{}, false
		}

		// check if this is a replacement container for a compose app when applying new settings or updating.
		//
		// we need this logic so that user does not see the temporary replacement container in the UI.
		{
			container, err := service.MyService.Docker().GetContainerByName(app.Name)
			if err != nil {
				logger.Error("failed to get container by name", zap.Error(err), zap.String("container", app.Name))
				return codegen.WebAppGridItem{}, false
			}

			// see recreateContainer() func from https://github.com/docker/compose/blob/v2/pkg/compose/convergence.go
			if replaceLabel, ok := container.Labels[api.ContainerReplaceLabel]; ok {
				if lo.ContainsBy(
					composeAppContainers,
					func(container codegen.ContainerSummary) bool {
						return container.ID == replaceLabel
					},
				) {
					// this is a replacement container for a compose app, skipping...
					return codegen.WebAppGridItem{}, false
				}
			}
		}

		item, err := WebAppGridItemAdapterContainer(&app)
		if err != nil {
			logger.Error("failed to adapt web app grid item", zap.Error(err), zap.String("app", app.Name))
			return codegen.WebAppGridItem{}, false
		}
		return *item, true
	})

	// merge v1 and v2 apps
	var appGridItems []codegen.WebAppGridItem
	appGridItems = append(appGridItems, v2AppGridItems...)
	appGridItems = append(appGridItems, v1AppGridItems...)
	appGridItems = append(appGridItems, containerAppGridItems...)
	markFailed(appGridItems, listContainers(ctx.Request().Context()))

	return ctx.JSON(http.StatusOK, codegen.GetWebAppGridOK{
		Message: utils.Ptr("This data is for internal use ONLY - will not be supported for public use."),
		Data:    &appGridItems,
	})
}

func WebAppGridItemAdapterV2(composeAppWithStoreInfo *codegen.ComposeAppWithStoreInfo) (*codegen.WebAppGridItem, error) {
	if composeAppWithStoreInfo == nil {
		return nil, fmt.Errorf("v2 compose app is nil")
	}

	// validation
	composeApp := (*service.ComposeApp)(composeAppWithStoreInfo.Compose)
	if composeApp == nil {
		return nil, fmt.Errorf("failed to get compose app")
	}

	item := &codegen.WebAppGridItem{
		AppType: codegen.V2app,
		Name:    &composeApp.Name,
		Title: lo.ToPtr(map[string]string{
			common.DefaultLanguage: composeApp.Name,
		}),
		IsUncontrolled: utils.Ptr(false),
	}

	composeAppStoreInfo := composeAppWithStoreInfo.StoreInfo
	if composeAppStoreInfo != nil {

		// item properties from store info
		item.Hostname = composeAppStoreInfo.Hostname
		item.Icon = &composeAppStoreInfo.Icon
		item.Index = &composeAppStoreInfo.Index
		item.Port = &composeAppStoreInfo.PortMap
		item.Scheme = composeAppStoreInfo.Scheme
		item.Status = composeAppWithStoreInfo.Status
		item.StoreAppID = composeAppStoreInfo.StoreAppID
		item.Title = &composeAppStoreInfo.Title
		item.IsUncontrolled = composeAppStoreInfo.IsUncontrolled

		// A Web UI port published on one specific address (e.g.
		// "192.168.1.10:2283:2283") isn't reachable at whatever name the
		// dashboard was opened with - over Tailscale that's the node's
		// 100.x address or MagicDNS name, and the app link would be
		// refused. Point the link at the bound address instead (reachable
		// remotely through the node's subnet route).
		if item.Hostname == nil || strings.TrimSpace(*item.Hostname) == "" {
			if ip := webUIBoundHostIP(composeApp, composeAppStoreInfo.Main, composeAppStoreInfo.PortMap); ip != "" {
				item.Hostname = &ip
			}
		}

		if composeAppStoreInfo.Main != nil {
			if service, ok := composeApp.Services[*composeAppStoreInfo.Main]; ok {
				item.Image = &service.Image // Hengxin needs this image property for some reason...
			}
		}
	}

	// item type
	itemAuthorType := composeApp.AuthorType()
	item.AuthorType = &itemAuthorType
	if composeAppWithStoreInfo.IsUncontrolled == nil {
		item.IsUncontrolled = utils.Ptr(false)
	} else {
		item.IsUncontrolled = composeAppWithStoreInfo.IsUncontrolled
	}

	return item, nil
}

func WebAppGridItemAdapterV1(app *model.MyAppList) (*codegen.WebAppGridItem, error) {
	if app == nil {
		return nil, fmt.Errorf("v1 app is nil")
	}

	item := &codegen.WebAppGridItem{
		AppType:  codegen.V1app,
		Name:     &app.Name,
		Status:   &app.State,
		Image:    &app.Image,
		Hostname: &app.Host,
		Icon:     &app.Icon,
		Index:    &app.Index,
		Port:     &app.Port,
		Scheme:   (*codegen.Scheme)(&app.Protocol),
		Title: &map[string]string{
			common.DefaultLanguage: app.Name,
		},
		IsUncontrolled: &app.IsUncontrolled,
	}

	return item, nil
}

func WebAppGridItemAdapterContainer(container *model.MyAppList) (*codegen.WebAppGridItem, error) {
	if container == nil {
		return nil, fmt.Errorf("container is nil")
	}

	item := &codegen.WebAppGridItem{
		AppType: codegen.Container,
		Name:    &container.Name,
		Status:  &container.State,
		Image:   &container.Image,
		Title: &map[string]string{
			common.DefaultLanguage: container.Name,
		},
		IsUncontrolled: &container.IsUncontrolled,
	}

	return item, nil
}

// webUIBoundHostIP returns the specific host address the Web UI port
// (portMap, published by the main service - or any service when main is
// unset) is bound to, or "" when it listens on every address.
func webUIBoundHostIP(app *service.ComposeApp, main *string, portMap string) string {
	portMap = strings.TrimSpace(portMap)
	if app == nil || portMap == "" {
		return ""
	}
	for _, svc := range app.Services {
		if main != nil && *main != "" && svc.Name != *main {
			continue
		}
		for _, p := range svc.Ports {
			if strings.TrimSpace(p.Published) != portMap {
				continue
			}
			host := strings.Trim(strings.TrimSpace(p.HostIP), "[]")
			ip := net.ParseIP(host)
			if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
				return ""
			}
			if ip.To4() == nil {
				return "[" + ip.String() + "]"
			}
			return ip.String()
		}
	}
	return ""
}
