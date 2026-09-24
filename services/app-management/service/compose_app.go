package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	v1 "github.com/F-e-n-y-x/NivaroOS/services/app-management/service/v1"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/docker"
	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	portutil "github.com/F-e-n-y-x/NivaroOS/services/common/utils/port"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/random"
	"github.com/compose-spec/compose-go/cli"
	"github.com/compose-spec/compose-go/loader"
	"github.com/compose-spec/compose-go/types"

	"github.com/docker/compose/v2/cmd/formatter"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/go-resty/resty/v2"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type ComposeApp codegen.ComposeApp

// XCasaOS returns the x-casaos extension as a map. ok is false when it is
// missing, null (`x-casaos:` with no value) or not a mapping - never panics.
func (a *ComposeApp) XCasaOS() (map[string]interface{}, bool) {
	if a == nil || a.Extensions == nil {
		return nil, false
	}

	extension, ok := a.Extensions[common.ComposeExtensionNameXCasaOS].(map[string]interface{})
	if !ok || extension == nil {
		return nil, false
	}

	return extension, true
}

// IsUncontrolled reads x-casaos.is_uncontrolled; ok is false if it is not set.
func (a *ComposeApp) IsUncontrolled() (value bool, ok bool) {
	extension, ok := a.XCasaOS()
	if !ok {
		return false, false
	}

	value, ok = extension[common.ComposeExtensionPropertyNameIsUncontrolled].(bool)
	return value, ok
}

func (a *ComposeApp) StoreInfo(includeApps bool) (*codegen.ComposeAppStoreInfo, error) {
	ex, ok := a.XCasaOS()
	if !ok {
		return nil, ErrComposeExtensionNameXCasaOSNotFound
	}

	var storeInfo codegen.ComposeAppStoreInfo
	if err := loader.Transform(ex, &storeInfo); err != nil {
		logger.Error("Transform store info fail", zap.Error(err))
		return nil, err
	}

	// TODO refactor this with ComposeAppWithStoreInfo
	isUncontrolled, ok := a.IsUncontrolled()
	if ok {
		storeInfo.IsUncontrolled = &isUncontrolled
	}

	// locate main app
	if storeInfo.Main == nil || *storeInfo.Main == "" {
		// if main app is not specified, use the first app
		for _, app := range a.Apps() {
			storeInfo.Main = &app.Name
			break
		}
	}

	if storeInfo.Scheme == nil || *storeInfo.Scheme == "" {
		storeInfo.Scheme = lo.ToPtr(codegen.Http)
	}

	if includeApps {
		apps := map[string]codegen.AppStoreInfo{}

		for _, app := range a.Apps() {
			appStoreInfo, err := app.StoreInfo()
			if err != nil {
				if err == ErrComposeExtensionNameXCasaOSNotFound {
					logger.Info("App does not have x-casaos extension - skipping", zap.String("app", app.Name))
					continue
				}

				return nil, err
			}
			apps[app.Name] = appStoreInfo
		}

		storeInfo.Apps = &apps
	}

	return &storeInfo, nil
}

// StoreAppID is x-casaos.store_app_id, falling back to the compose project
// name (which is the store app id by convention at install time, but can
// differ, e.g. after an install under another name).
func (a *ComposeApp) StoreAppID() string {
	if extension, ok := a.XCasaOS(); ok {
		if id, ok := extension[common.ComposeExtensionPropertyNameStoreAppID].(string); ok && id != "" {
			return id
		}
	}

	return a.Name
}

func (a *ComposeApp) AuthorType() codegen.StoreAppAuthorType {
	storeInfo, err := a.StoreInfo(false)
	if err != nil {
		return codegen.Unknown
	}

	if strings.EqualFold(storeInfo.Author, storeInfo.Developer) {
		return codegen.Official
	}
	if strings.EqualFold(storeInfo.Author, common.ComposeAppAuthorCasaOSTeam) {
		return codegen.ByCasaos
	}

	return codegen.Community
}

func (a *ComposeApp) SetStoreAppID(storeAppID string) (string, bool) {
	// set store_app_id (by convention is the same as app name at install time if it does not exist)
	composeAppStoreInfo, ok := a.XCasaOS()
	if !ok {
		logger.Info("compose app does not have valid x-casaos extension - might not be a compose app for CasaOS", zap.String("app", a.Name))
		return "", false
	}

	value, ok := composeAppStoreInfo[common.ComposeExtensionPropertyNameStoreAppID]
	if ok {
		currentStoreAppID, ok := value.(string)
		if ok {
			logger.Info("compose app already has store_app_id", zap.String("app", a.Name), zap.String("storeAppID", currentStoreAppID))
			return currentStoreAppID, true
		}
	}

	composeAppStoreInfo[common.ComposeExtensionPropertyNameStoreAppID] = storeAppID
	return storeAppID, true
}

func (a *ComposeApp) SetTitle(title, lang string) {
	if a.Extensions == nil {
		a.Extensions = make(map[string]interface{})
	}

	extension, ok := a.Extensions[common.ComposeExtensionNameXCasaOS]
	if !ok || extension == nil {
		extension = map[string]interface{}{}
		a.Extensions[common.ComposeExtensionNameXCasaOS] = extension
	}

	composeAppStoreInfo, ok := extension.(map[string]interface{})
	if !ok {
		logger.Info("compose app does not have valid x-casaos extension - might not be a compose app for CasaOS", zap.String("app", a.Name))
		return
	}

	if _, ok := composeAppStoreInfo[common.ComposeExtensionPropertyNameTitle]; !ok {
		composeAppStoreInfo[common.ComposeExtensionPropertyNameTitle] = map[string]string{}
	}

	titleMap, ok := composeAppStoreInfo[common.ComposeExtensionPropertyNameTitle].(map[string]string)
	if !ok {
		logger.Info("compose app does not have valid title map in its x-casaos extension - might not be a compose app for CasaOS", zap.String("app", a.Name))
		return
	}

	if _, ok := titleMap[lang]; !ok {
		titleMap[lang] = title
	}
}

func (a *ComposeApp) Update(ctx context.Context) error {
	if len(a.ComposeFiles) <= 0 {
		return ErrComposeFileNotFound
	}

	if len(a.ComposeFiles) > 1 {
		logger.Info("warning: multiple compose files found, only the first one will be used", zap.String("compose files", strings.Join(a.ComposeFiles, ",")))
	}

	storeInfo, err := a.StoreInfo(true)
	if err != nil {
		return err
	}

	if storeInfo == nil || storeInfo.StoreAppID == nil || *storeInfo.StoreAppID == "" {
		return ErrStoreInfoNotFound
	}

	storeComposeApp, err := MyService.AppStoreManagement().ComposeApp(*storeInfo.StoreAppID)
	if err != nil {
		return err
	}

	if storeComposeApp == nil {
		return ErrNotFoundInAppStore
	}

	localComposeAppServices := lo.Map(a.Services, func(service types.ServiceConfig, i int) string { return service.Name })
	storeComposeAppServices := lo.Map(storeComposeApp.Services, func(service types.ServiceConfig, i int) string { return service.Name })

	localAbsentOfStore, storeAbsentOfLocal := lo.Difference(localComposeAppServices, storeComposeAppServices)
	if len(localAbsentOfStore) > 0 {
		logger.Error("local compose app has container apps that are not present in store compose app, thus update is not possible", zap.Strings("absent", localAbsentOfStore))
		return ErrComposeAppNotMatch
	}

	if len(storeAbsentOfLocal) > 0 {
		logger.Error("store compose app has container apps that are not present in local compose app, thus update is not possible", zap.Strings("absent", storeAbsentOfLocal))
		return ErrComposeAppNotMatch
	}

	// build the new compose on a copy: `a` must keep describing what is
	// running now, because a failed update rolls back to it.
	updated := a.withServices(updatedServiceImages(a.Services, storeComposeApp.Services))

	// the code is need by stable diffusion.
	removeRuntime(updated)

	newComposeYAML, err := yaml.Marshal(updated)
	if err != nil {
		return err
	}

	// prepare for message bus events
	eventProperties := common.PropertiesFromContext(ctx)
	if eventProperties != nil {
		eventProperties[common.PropertyTypeAppName.Name] = a.Name
	}

	if err := a.UpdateEventPropertiesFromStoreInfo(eventProperties); err != nil {
		logger.Info("failed to update event properties from store info", zap.Error(err), zap.String("name", a.Name))
	}

	go func(ctx context.Context) {
		go PublishEventWrapper(ctx, common.EventTypeAppUpdateBegin, nil)

		defer PublishEventWrapper(ctx, common.EventTypeAppUpdateEnd, nil)

		MyService.AppStoreManagement().StartUpgrade(a.Name)
		defer MyService.AppStoreManagement().FinishUpgrade(a.Name)

		if err := a.PullAndApply(ctx, newComposeYAML); err != nil {
			go PublishEventWrapper(ctx, common.EventTypeAppUpdateError, map[string]string{
				common.PropertyTypeMessage.Name: err.Error(),
			})

			logger.Error("failed to update compose app", zap.Error(err), zap.String("name", a.Name))
		}
	}(ctx)

	return nil
}

// withServices returns a shallow copy of a with its own Services slice.
func (a *ComposeApp) withServices(services types.Services) *ComposeApp {
	copied := *a
	copied.Services = services
	return &copied
}

// updatedServiceImages returns a copy of local where each service takes the
// store's image, unless the store image uses a tag that is checked by digest
// (e.g. latest), which keeps the local image.
func updatedServiceImages(local, store types.Services) types.Services {
	result := make(types.Services, len(local))
	copy(result, local)

	for _, storeService := range store {
		for i := range result {
			if result[i].Name != storeService.Name {
				continue
			}

			if !lo.SomeBy(common.NeedCheckDigestTags, func(tag string) bool { return strings.HasSuffix(storeService.Image, tag) }) {
				result[i].Image = storeService.Image
			}
		}
	}

	return result
}

// TODO rename the function to service and add error return value
func (a *ComposeApp) App(name string) *App {
	if name == "" {
		return nil
	}

	for i, service := range a.Services {
		if service.Name == name {
			return (*App)(&a.Services[i])
		}
	}

	return nil
}

func (a *ComposeApp) Apps() map[string]*App {
	apps := make(map[string]*App)

	for i, service := range a.Services {
		apps[service.Name] = (*App)(&a.Services[i])
	}

	return apps
}

func (a *ComposeApp) MainService() (*App, error) {
	storeInfo, err := a.StoreInfo(false)
	if err != nil {
		return nil, err
	}

	if storeInfo.Main == nil || *storeInfo.Main == "" {
		return nil, ErrMainServiceNotSpecified
	}

	return a.App(*storeInfo.Main), nil
}

func (a *ComposeApp) MainTag() (string, error) {
	mainService, err := a.MainService()
	if err != nil {
		return "", err
	}
	_, newTag := docker.ExtractImageAndTag(mainService.Image)

	return newTag, nil
}

func (a *ComposeApp) Containers(ctx context.Context) (map[string][]api.ContainerSummary, error) {
	service, dockerClient, err := apiService()
	if err != nil {
		return nil, err
	}
	defer dockerClient.Close()

	containers, err := service.Ps(ctx, a.Name, api.PsOptions{
		All: true,
	})
	if err != nil {
		return nil, err
	}

	// it is possible a `service` contains multiple containers.
	// See https://docs.docker.com/compose/compose-file/deploy/#replicas
	return lo.GroupBy(containers, func(container api.ContainerSummary) string {
		return container.Service
	}), nil
}

func (a *ComposeApp) Pull(ctx context.Context) error {
	// pull
	serviceNum := len(a.Services)

	for i, app := range a.Services {
		if err := func() error {
			go PublishEventWrapper(ctx, common.EventTypeImagePullBegin, map[string]string{
				common.PropertyTypeImageName.Name: app.Image,
			})

			defer PublishEventWrapper(ctx, common.EventTypeImagePullEnd, map[string]string{
				common.PropertyTypeImageName.Name: app.Image,
			})

			if err := docker.PullImage(ctx, app.Image, func(out io.ReadCloser) {
				pullImageProgress(ctx, out, "INSTALL", serviceNum, i+1)
			}); err != nil {
				go PublishEventWrapper(ctx, common.EventTypeImagePullError, map[string]string{
					common.PropertyTypeImageName.Name: app.Image,
					common.PropertyTypeMessage.Name:   err.Error(),
				})

				return fmt.Errorf("failed to pull image %s: %w", app.Image, err)
			}

			return nil
		}(); err != nil {
			return err
		}
	}

	return nil
}

// injectEnvVariableToComposeApp sets global settings (config.Global, e.g.
// OPENAI_API_KEY) as environment variables of the services - but only the
// ones the app's compose file actually references ($KEY / ${KEY} or an
// environment entry named KEY). Injecting every global into every container
// leaked e.g. API keys into apps that never asked for them.
func (a *ComposeApp) injectEnvVariableToComposeApp() {
	global := config.GlobalSnapshot()
	if len(global) == 0 {
		return
	}

	var raw []byte
	for _, composeFile := range a.ComposeFiles {
		if content, err := os.ReadFile(composeFile); err == nil {
			raw = append(append(raw, content...), '\n')
		}
	}

	for i := range a.Services {
		keys := referencedGlobalKeys(raw, a.Services[i].Environment, global)
		if len(keys) == 0 {
			continue
		}

		if a.Services[i].Environment == nil {
			a.Services[i].Environment = types.MappingWithEquals{}
		}

		for _, k := range keys {
			// if there is same name var declared in environment in compose yaml
			// we should not reassign a value to it.
			if a.Services[i].Environment[k] == nil {
				a.Services[i].Environment[k] = utils.Ptr(global[k])
			}
		}
	}
}

// referencedGlobalKeys returns the keys of global that the raw compose text
// references as $KEY or ${KEY...}, or that are declared in environment.
func referencedGlobalKeys(raw []byte, environment types.MappingWithEquals, global map[string]string) []string {
	referenced := map[string]bool{}
	for _, match := range composeVariablePattern.FindAllSubmatch(raw, -1) {
		name := string(match[1])
		if name == "" {
			name = string(match[2])
		}
		if name != "" { // "" is an escaped $$
			referenced[name] = true
		}
	}

	keys := []string{}
	for k := range global {
		if _, declared := environment[k]; declared || referenced[k] {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)
	return keys
}

// $$ is an escaped dollar; $NAME and ${NAME[:-default...]} are references
var composeVariablePattern = regexp.MustCompile(`\$\$|\$\{([A-Za-z_][A-Za-z0-9_]*)|\$([A-Za-z_][A-Za-z0-9_]*)`)

func (a *ComposeApp) Up(ctx context.Context, service api.Service) error {
	a.injectEnvVariableToComposeApp()

	if err := service.Up(ctx, (*codegen.ComposeApp)(a), api.UpOptions{
		Start: api.StartOptions{
			CascadeStop: true,
			Wait:        true,
		},
	}); err != nil {
		logger.Error("failed to start original compose app", zap.Error(err), zap.String("name", a.Name))
		return err
	}
	return nil
}

// bindSourceToCreate returns the host path to create for a service volume,
// or "" when nothing should be created: named volumes (declared in the
// project), anonymous volumes and tmpfs (no source) and non-bind mounts.
func bindSourceToCreate(volume types.ServiceVolumeConfig, projectVolumes types.Volumes) string {
	if volume.Source == "" {
		return ""
	}

	if _, ok := projectVolumes[volume.Source]; ok {
		// this is a internal volume, so skip.
		return ""
	}

	if volume.Type != "" && volume.Type != types.VolumeTypeBind {
		return ""
	}

	if !filepath.IsAbs(volume.Source) {
		// compose resolves relative binds against the working dir; a bare
		// name here is a volume that is not declared - let compose handle it
		return ""
	}

	return volume.Source
}

func (a *ComposeApp) UpWithCheckRequire(ctx context.Context, service api.Service) error {
	// prepare source path for volumes if not exist
	for i, app := range a.Services {
		for _, volume := range app.Volumes {
			path := bindSourceToCreate(volume, a.Volumes)
			if path == "" {
				continue
			}

			if err := file.IsNotExistMkDir(path); err != nil {
				go PublishEventWrapper(ctx, common.EventTypeContainerStartError, map[string]string{
					common.PropertyTypeMessage.Name: err.Error(),
				})
				return err
			}
		}

		// check if each required device exists
		deviceMapFiltered := []string{}
		for _, deviceMap := range app.Devices {
			devicePath := strings.SplitN(deviceMap, ":", 2)[0]
			if file.CheckNotExist(devicePath) {
				logger.Info("device not found", zap.String("device", devicePath))
				continue
			}
			deviceMapFiltered = append(deviceMapFiltered, deviceMap)
		}
		a.Services[i].Devices = deviceMapFiltered
	}

	if err := a.Up(ctx, service); err != nil {
		go PublishEventWrapper(ctx, common.EventTypeContainerStartError, map[string]string{
			common.PropertyTypeMessage.Name: err.Error(),
		})
		return err
	}
	return nil
}

func (a *ComposeApp) PullAndApply(ctx context.Context, newComposeYAML []byte) error {
	// backup current compose file
	currentComposeFile := a.ComposeFiles[0]

	backupComposeFile := currentComposeFile + "." + "bak"
	if err := file.CopySingleFile(currentComposeFile, backupComposeFile, ""); err != nil {
		return err
	}

	// start compose app
	service, dockerClient, err := apiService()
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	success := false

	defer func() {
		if !success {
			if err := file.CopySingleFile(backupComposeFile, currentComposeFile, ""); err != nil {
				logger.Error("failed to restore original compose file", zap.Error(err), zap.String("src", backupComposeFile), zap.String("dst", currentComposeFile))
				return
			}

			// start what was running before from the restored file, not from
			// `a` (which callers may have changed)
			original, err := LoadComposeAppFromConfigFiles(a.Name, a.ComposeFiles)
			if err != nil {
				logger.Error("failed to load original compose app for rollback", zap.Error(err), zap.String("name", a.Name))
				return
			}

			if err := original.Up(ctx, service); err != nil {
				logger.Error("failed to start original compose app", zap.Error(err), zap.String("name", a.Name))
				return
			}

			logger.Info("rolled back compose app to its previous compose file", zap.String("name", a.Name))
		}
	}()

	// save new compose file
	if err := file.WriteToFullPath(newComposeYAML, currentComposeFile, 0o600); err != nil {
		return err
	}

	newComposeApp, err := LoadComposeAppFromConfigFiles(a.Name, a.ComposeFiles)
	if err != nil {
		return err
	}

	if err := newComposeApp.Pull(ctx); err != nil {
		return err
	}

	go PublishEventWrapper(ctx, common.EventTypeContainerStartBegin, nil)

	defer PublishEventWrapper(ctx, common.EventTypeContainerStartEnd, nil)

	err = newComposeApp.UpWithCheckRequire(ctx, service)

	success = err == nil

	return err
}

func (a *ComposeApp) Create(ctx context.Context, options api.CreateOptions, service api.Service) error {
	a.injectEnvVariableToComposeApp()
	return service.Create(ctx, (*codegen.ComposeApp)(a), api.CreateOptions{})
}

func (a *ComposeApp) PullAndInstall(ctx context.Context) error {
	service, dockerClient, err := apiService()
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	// pull
	if err := a.Pull(ctx); err != nil {
		return err
	}

	// create
	if err := func() error {
		go PublishEventWrapper(ctx, common.EventTypeContainerCreateBegin, nil)

		defer PublishEventWrapper(ctx, common.EventTypeContainerCreateEnd, nil)

		for i, app := range a.Services {
			// prepare source path for volumes if not exist
			for _, volume := range app.Volumes {
				path := bindSourceToCreate(volume, a.Volumes)
				if path == "" {
					continue
				}

				if err := file.IsNotExistMkDir(path); err != nil {
					go PublishEventWrapper(ctx, common.EventTypeContainerCreateError, map[string]string{
						common.PropertyTypeMessage.Name: err.Error(),
					})
					return err
				}
			}

			// check if each required device exists
			deviceMapFiltered := []string{}
			for _, deviceMap := range app.Devices {
				devicePath := strings.SplitN(deviceMap, ":", 2)[0]
				if file.CheckNotExist(devicePath) {
					logger.Info("device not found", zap.String("device", devicePath))
					continue
				}
				deviceMapFiltered = append(deviceMapFiltered, deviceMap)
			}
			a.Services[i].Devices = deviceMapFiltered
		}

		if err := a.Create(ctx, api.CreateOptions{}, service); err != nil {
			go PublishEventWrapper(ctx, common.EventTypeContainerCreateError, map[string]string{
				common.PropertyTypeMessage.Name: err.Error(),
			})
			return err
		}

		return nil
	}(); err != nil {
		return err
	}

	go PublishEventWrapper(ctx, common.EventTypeContainerStartBegin, nil)

	defer PublishEventWrapper(ctx, common.EventTypeContainerStartEnd, nil)

	if err := service.Start(ctx, a.Name, api.StartOptions{
		CascadeStop: true,
		Wait:        true,
	}); err != nil {
		go PublishEventWrapper(ctx, common.EventTypeContainerStartError, map[string]string{
			common.PropertyTypeMessage.Name: err.Error(),
		})
		return err
	}

	return nil
}

func (a *ComposeApp) Uninstall(ctx context.Context, deleteConfigFolder bool) error {
	service, dockerClient, err := apiService()
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	// stop
	if err := func() error {
		go PublishEventWrapper(ctx, common.EventTypeContainerStopBegin, nil)

		defer PublishEventWrapper(ctx, common.EventTypeContainerStopEnd, nil)

		if err := service.Stop(ctx, a.Name, api.StopOptions{}); err != nil {
			go PublishEventWrapper(ctx, common.EventTypeContainerStopError, map[string]string{
				common.PropertyTypeMessage.Name: err.Error(),
			})

			return err
		}

		return nil
	}(); err != nil {
		return err
	}

	// remove
	go PublishEventWrapper(ctx, common.EventTypeContainerRemoveBegin, nil)

	defer PublishEventWrapper(ctx, common.EventTypeContainerRemoveEnd, nil)

	if err := service.Down(ctx, a.Name, uninstallDownOptions(deleteConfigFolder)); err != nil {
		go PublishEventWrapper(ctx, common.EventTypeImageRemoveError, map[string]string{
			common.PropertyTypeMessage.Name: err.Error(),
		})

		return err
	}

	// the compose working dir (holding compose.yaml) is only ours to delete
	// when it is the app's folder under AppsPath - a compose project started
	// elsewhere (Portainer, a user's own folder) keeps its files
	if IsManagedWorkingDir(a.Name, a.WorkingDir) {
		if err := file.RMDir(a.WorkingDir); err != nil {
			go PublishEventWrapper(ctx, common.EventTypeImageRemoveError, map[string]string{
				common.PropertyTypeMessage.Name: err.Error(),
			})
		}
	} else {
		logger.Info("not removing compose working dir outside of the apps folder", zap.String("name", a.Name), zap.String("workingDir", a.WorkingDir))
	}

	if !deleteConfigFolder {
		return nil
	}

	sources := []string{}
	for _, app := range a.Services {
		for _, volume := range app.Volumes {
			if volume.Type != "" && volume.Type != types.VolumeTypeBind {
				continue // named volumes were removed by Down(Volumes: true)
			}
			sources = append(sources, volume.Source)
		}
	}

	for _, path := range AppDataPathsToRemove(a.Name, sources, a.WorkingDir) {
		logger.Info("removing compose app data folder", zap.String("name", a.Name), zap.String("path", path))
		if err := file.RMDir(path); err != nil {
			logger.Error("failed to remove compose app config folder", zap.Error(err), zap.String("path", path))

			go PublishEventWrapper(ctx, common.EventTypeImageRemoveError, map[string]string{
				common.PropertyTypeMessage.Name: err.Error(),
			})
		}
	}

	return nil
}

// uninstallDownOptions: "keep data" (deleteConfigFolder=false) must not
// remove named volumes - they are the data. Images are only removed on a
// full removal; docker refuses (and compose ignores) removing an image that
// another container still uses, so images shared with other apps survive.
// When data is kept the images are kept too, so a reinstall is instant.
func uninstallDownOptions(deleteConfigFolder bool) api.DownOptions {
	options := api.DownOptions{
		RemoveOrphans: true,
		Volumes:       deleteConfigFolder,
	}

	if deleteConfigFolder {
		options.Images = "all"
	}

	return options
}

// AppDataRoot is where store apps keep their data: /DATA/AppData/<app>/...
var AppDataRoot = "/DATA/AppData"

// AppDataPathsToRemove decides which host folders are deleted when an app is
// uninstalled with "delete data". Only the app's own folder under
// AppDataRoot (/DATA/AppData/<name>, for any bind source inside it) and the
// app's compose working dir are ever returned. Everything else - media
// folders that merely contain the name (/DATA/Media/tv for app "tv"),
// /DATA itself, /mnt/data, relative paths, other apps' folders - is kept.
func AppDataPathsToRemove(appName string, sources []string, workingDir string) []string {
	result := []string{}

	name := strings.TrimPrefix(appName, "/") // v1 container names start with "/"
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return result
	}

	appDir := filepath.Join(filepath.Clean(AppDataRoot), name)

	add := func(path string) {
		if !lo.Contains(result, path) {
			result = append(result, path)
		}
	}

	for _, source := range sources {
		if source == "" || !filepath.IsAbs(source) {
			continue
		}

		if isPathWithin(appDir, filepath.Clean(source)) {
			add(appDir)
		}
	}

	if IsManagedWorkingDir(name, workingDir) {
		add(filepath.Clean(workingDir))
	}

	return result
}

// IsManagedWorkingDir reports whether dir is <AppsPath>/<name>, i.e. a
// compose folder NivaroOS created at install time.
func IsManagedWorkingDir(name, dir string) bool {
	if name == "" || dir == "" || !filepath.IsAbs(dir) {
		return false
	}

	appsPath := filepath.Clean(config.AppInfo.AppsPath)
	if appsPath == "" || appsPath == "." || appsPath == "/" {
		return false
	}

	return filepath.Clean(dir) == filepath.Join(appsPath, name)
}

// isPathWithin reports whether path is dir or inside it, by path segments
// (so /DATA/AppData/tv2 is not within /DATA/AppData/tv).
func isPathWithin(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (a *ComposeApp) Apply(ctx context.Context, newComposeYAML []byte) error {
	// compare new ComposeApp with current ComposeApp
	if getNameFrom(newComposeYAML) != a.Name {
		return ErrComposeAppNotMatch
	}

	newComposeApp, err := NewComposeAppFromYAML(newComposeYAML, true, true)
	if err != nil {
		return err
	}

	if len(a.ComposeFiles) <= 0 {
		return ErrComposeFileNotFound
	}

	if len(a.ComposeFiles) > 1 {
		logger.Info("warning: multiple compose files found, only the first one will be used", zap.String("compose files", strings.Join(a.ComposeFiles, ",")))
	}

	// prepare for message bus events
	eventProperties := common.PropertiesFromContext(ctx)
	if eventProperties != nil {
		eventProperties[common.PropertyTypeAppName.Name] = a.Name
	}

	// prepare for message bus events
	if err := newComposeApp.UpdateEventPropertiesFromStoreInfo(eventProperties); err != nil {
		logger.Info("failed to update event properties from store info", zap.Error(err), zap.String("name", a.Name))
	}

	go func(ctx context.Context) {
		go PublishEventWrapper(ctx, common.EventTypeAppApplyChangesBegin, nil)

		defer PublishEventWrapper(ctx, common.EventTypeAppApplyChangesEnd, nil)

		if err := a.PullAndApply(ctx, newComposeYAML); err != nil {
			go PublishEventWrapper(ctx, common.EventTypeAppApplyChangesError, map[string]string{
				common.PropertyTypeMessage.Name: err.Error(),
			})

			logger.Error("failed to apply changes to compose app", zap.Error(err), zap.String("name", a.Name))
		}
	}(ctx)

	return nil
}

func (a *ComposeApp) SetStatus(ctx context.Context, status codegen.RequestComposeAppStatus) error {
	service, dockerClient, err := apiService()
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	eventProperties := common.PropertiesFromContext(ctx)
	if eventProperties != nil {
		eventProperties[common.PropertyTypeAppName.Name] = a.Name
	}

	switch status {
	case codegen.RequestComposeAppStatusStart:
		go func(ctx context.Context) {
			go PublishEventWrapper(ctx, common.EventTypeAppStartBegin, nil)

			defer PublishEventWrapper(ctx, common.EventTypeAppStartEnd, nil)

			// to make sure the container is stopped
			// timeout is 20s
			for index := 0; index < 10; index++ {
				containerSummarys, err := service.Ps(ctx, a.Name, api.PsOptions{
					All: true,
				})
				if err != nil {
					logger.Error("failed to get compose app info", zap.Error(err), zap.String("name", a.Name))
				}
				isContainerExited := true
				for _, containerSummary := range containerSummarys {
					// to make sure every service of the container is stopped
					// I think "exited" can be replace by constant value.
					isContainerExited = isContainerExited && (containerSummary.State == "exited")
				}
				if isContainerExited {
					break
				}
				time.Sleep(2 * time.Second)
			}

			if err := service.Start(ctx, a.Name, api.StartOptions{
				CascadeStop: true,
				Wait:        true,
			}); err != nil {
				go PublishEventWrapper(ctx, common.EventTypeAppStartError, map[string]string{
					common.PropertyTypeMessage.Name: err.Error(),
				})

				logger.Error("failed to start compose app", zap.Error(err), zap.String("name", a.Name))
			}
		}(ctx)
	case codegen.RequestComposeAppStatusStop:
		go func(ctx context.Context) {
			go PublishEventWrapper(ctx, common.EventTypeAppStopBegin, nil)

			defer PublishEventWrapper(ctx, common.EventTypeAppStopEnd, nil)

			if err := service.Stop(ctx, a.Name, api.StopOptions{}); err != nil {
				go PublishEventWrapper(ctx, common.EventTypeAppStopError, map[string]string{
					common.PropertyTypeMessage.Name: err.Error(),
				})

				logger.Error("failed to stop compose app", zap.Error(err), zap.String("name", a.Name))
			}
		}(ctx)
	case codegen.RequestComposeAppStatusRestart:
		go func(ctx context.Context) {
			go PublishEventWrapper(ctx, common.EventTypeAppRestartBegin, nil)

			defer PublishEventWrapper(ctx, common.EventTypeAppRestartEnd, nil)

			if err := service.Restart(ctx, a.Name, api.RestartOptions{}); err != nil {
				go PublishEventWrapper(ctx, common.EventTypeAppRestartError, map[string]string{
					common.PropertyTypeMessage.Name: err.Error(),
				})

				logger.Error("failed to restart compose app", zap.Error(err), zap.String("name", a.Name))
			}
		}(ctx)
	default:
		return ErrInvalidComposeAppStatus
	}

	return nil
}

func (a *ComposeApp) Logs(ctx context.Context, lines int) ([]byte, error) {
	service, dockerClient, err := apiService()
	if err != nil {
		return nil, err
	}
	defer dockerClient.Close()

	var buf bytes.Buffer

	consumer := formatter.NewLogConsumer(ctx, &buf, &buf, false, true, false)

	if err := service.Logs(ctx, a.Name, consumer, api.LogOptions{
		Project:  (*codegen.ComposeApp)(a),
		Services: lo.Map(a.Services, func(s types.ServiceConfig, i int) string { return s.Name }),
		Follow:   false,
		Tail:     lo.If(lines < 0, "all").Else(strconv.Itoa(lines)),
	}); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (a *ComposeApp) GetPortsInUse() (*codegen.ComposeAppValidationErrorsPortsInUse, error) {
	tcpPorts, udpPorts, err := portutil.ListPortsInUse()
	if err != nil {
		return nil, err
	}

	allPortsInUse := lo.Union(tcpPorts, udpPorts)

	tcpPortInUse := []string{}
	udpPortInUse := []string{}

	for _, s := range a.Services {
		for _, p := range s.Ports {
			if lo.ContainsBy(allPortsInUse, func(portInUse int) bool { return strconv.Itoa(portInUse) == p.Published }) {
				switch strings.ToLower(p.Protocol) {
				case "tcp":
					tcpPortInUse = append(tcpPortInUse, p.Published)
				case "udp":
					udpPortInUse = append(udpPortInUse, p.Published)
				}
			}
		}
	}

	if len(tcpPortInUse) == 0 && len(udpPortInUse) == 0 {
		return nil, nil
	}

	portsInUse := struct {
		TCP *codegen.PortList "json:\"TCP,omitempty\""
		UDP *codegen.PortList "json:\"UDP,omitempty\""
	}{TCP: &tcpPortInUse, UDP: &udpPortInUse}

	return &codegen.ComposeAppValidationErrorsPortsInUse{PortsInUse: &portsInUse}, nil
}

// Try to update AppIcon and AppTitle in given event properties from store info
func (a *ComposeApp) UpdateEventPropertiesFromStoreInfo(eventProperties map[string]string) error {
	if eventProperties == nil {
		return fmt.Errorf("event properties is nil")
	}

	storeInfo, err := a.StoreInfo(false)
	if err != nil {
		return err
	}

	eventProperties[common.PropertyTypeAppIcon.Name] = storeInfo.Icon

	if storeInfo.Title == nil {
		return fmt.Errorf("compose app title not found in store info")
	}

	titles, err := json.Marshal(storeInfo.Title)
	if err != nil {
		return err
	}

	eventProperties[common.PropertyTypeAppTitle.Name] = string(titles)

	return nil
}

func (a *ComposeApp) HealthCheck() (bool, error) {
	storeInfo, err := a.StoreInfo(false)
	if err != nil {
		return false, err
	}

	scheme := "http"
	if storeInfo.Scheme != nil && *storeInfo.Scheme != "" {
		scheme = string(*storeInfo.Scheme)
	}

	hostname := common.Localhost
	if storeInfo.Hostname != nil && *storeInfo.Hostname != "" {
		hostname = *storeInfo.Hostname
	}

	url := fmt.Sprintf(
		"%s://%s:%s/%s",
		scheme,
		hostname,
		storeInfo.PortMap,
		strings.TrimLeft(storeInfo.Index, "/"),
	)

	logger.Info("checking compose app health at the specified web port...", zap.String("name", a.Name), zap.Any("url", url))

	client := resty.New()
	client.SetTimeout(30 * time.Second)
	client.SetHeader("Accept", "text/html")
	// ignore ssl error
	client.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})
	response, err := client.R().Get(url)
	if err != nil {
		logger.Error("failed to check container health", zap.Error(err), zap.String("name", a.Name))
		return false, err
	}
	if response.StatusCode() == http.StatusOK || response.StatusCode() == http.StatusUnauthorized {
		return true, nil
	}

	logger.Error("compose app health check failed at the specified web port", zap.Any("name", a.Name), zap.Any("url", url), zap.String("status", fmt.Sprint(response.StatusCode())))
	return false, nil
}

func LoadComposeAppFromConfigFile(appID string, configFile string) (*ComposeApp, error) {
	return LoadComposeAppFromConfigFiles(appID, []string{configFile})
}

// interpolationEnv is what ${VAR} in an installed compose file can resolve
// to: AppID, the base variables (PUID, PGID, TZ, DefaultUserName, ...) and
// the global settings. The process environment of this service is NOT
// included - it used to be, which leaked host variables into apps.
func interpolationEnv(appID string) []string {
	env := []string{}
	for k, v := range config.GlobalSnapshot() {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	for k, v := range baseInterpolationMap() {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return append(env, fmt.Sprintf("%s=%s", "AppID", appID))
}

// LoadComposeAppFromConfigFiles loads an installed compose project from its
// compose file(s) (a project can be made of several, e.g. an override file).
func LoadComposeAppFromConfigFiles(appID string, configFiles []string) (*ComposeApp, error) {
	configFiles = lo.Filter(configFiles, func(f string, _ int) bool { return strings.TrimSpace(f) != "" })
	if len(configFiles) == 0 {
		return nil, ErrComposeFileNotFound
	}

	projectDir := filepath.Dir(configFiles[0])

	// this mirrors composeCmd.ProjectOptions.ToProject, which always adds
	// cli.WithOsEnv - that is exactly what must not happen here.
	options, err := cli.NewProjectOptions(
		configFiles,
		cli.WithWorkingDirectory(projectDir), // this has to be the first option, otherwise it will assume the dir where this program is running is the working directory.
		cli.WithEnv(interpolationEnv(appID)),
		cli.WithDotEnv,
		cli.WithName(appID),
	)
	if err != nil {
		return nil, err
	}

	project, err := cli.ProjectFromOptions(options)
	if err != nil {
		return nil, err
	}

	for i, s := range project.Services {
		s.CustomLabels = map[string]string{
			api.ProjectLabel:     project.Name,
			api.ServiceLabel:     s.Name,
			api.VersionLabel:     api.ComposeVersion,
			api.WorkingDirLabel:  project.WorkingDir,
			api.ConfigFilesLabel: strings.Join(project.ComposeFiles, ","),
			api.OneoffLabel:      "False",
		}
		project.Services[i] = s
	}

	project.WithoutUnnecessaryResources()

	return (*ComposeApp)(project), nil
}

var gpuCache *([]external.NvidiaGPUInfo) = nil

func removeRuntime(a *ComposeApp) {
	if config.RemoveRuntimeIfNoNvidiaGPUFlag {

		// if gpuCache is nil, it means it is first time fetching gpu info
		if gpuCache == nil {
			value, err := external.NvidiaGPUInfoList()
			if err != nil {
				gpuCache = &([]external.NvidiaGPUInfo{})
			} else {
				gpuCache = &value
			}

			// without nvidia-smi 	// no gpu or first time fetching gpu info failed
		}
		if len(*gpuCache) == 0 {
			for i := range a.Services {
				a.Services[i].Runtime = ""
			}
		}
	}
}

func NewComposeAppFromYAML(yaml []byte, skipInterpolation, skipValidation bool) (*ComposeApp, error) {
	return newComposeAppFromYAML(yaml, skipInterpolation, skipValidation, false)
}

// catalogWorkingDir is a working dir that is never created: parsing a store
// catalog entry only needs some absolute dir to resolve relative paths.
var catalogWorkingDir = filepath.Join(os.TempDir(), "nivaroos-compose-catalog")

func newComposeAppFromYAML(yaml []byte, skipInterpolation, skipValidation, forCatalog bool) (*ComposeApp, error) {
	tmpWorkingDir := catalogWorkingDir
	if !forCatalog {
		dir, err := os.MkdirTemp("", "nivaroos-compose-app-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		tmpWorkingDir = dir
	}

	// the WEBUI_PORT interpolate will tiger twice. In `pulished` and `port-map`.
	// So we need to promise multiple WEBUI_PORT interpolate is a same value.
	// Only look for a free port when WEBUI_PORT is actually used.
	port := 0
	webUIPort := func() int {
		if port == 0 {
			port, _ = portutil.GetAvailablePort("tcp")
		}
		return port
	}

	project, err := loader.Load(
		types.ConfigDetails{
			ConfigFiles: []types.ConfigFile{
				{
					Content: []byte(yaml),
				},
			},
			Environment: map[string]string{},

			// need to set a working dir because loader/normalize.go from github.com/compose-spec/compose-go makes
			// wrong assumption that the working dir is the same as the dir where this program is launched.
			WorkingDir: tmpWorkingDir,
		},
		func(o *loader.Options) {
			o.SkipInterpolation = skipInterpolation
			o.SkipValidation = skipValidation

			o.Interpolate.LookupValue = func(key string) (string, bool) {
				switch key {
				case "WEBUI_PORT":
					p := webUIPort()
					fmt.Printf("WEBUI_PORT is not specified, using %d\n", p)
					return strconv.Itoa(p), true
				}

				// everything else is kept as a reference (TZ => $TZ) and is
				// resolved when the installed compose file is loaded, from
				// AppID, the base variables and the global settings only (see
				// interpolationEnv). The process environment is never used.
				return fmt.Sprintf("$%s", key), true
			}

			if getNameFrom(yaml) != "" {
				return
			}

			// fix compose app name
			logger.Info("compose app name is not specified, getting a name from one of our contributors :)")
			projectName := random.Name(nil)
			logger.Info("compose app name is given", zap.String("name", projectName))
			o.SetProjectName(projectName, false)
		},
	)
	if err != nil {
		return nil, err
	}

	composeApp := (*ComposeApp)(project)

	if composeApp.Extensions == nil {
		composeApp.Extensions = map[string]interface{}{}
	}

	storeInfo, err := composeApp.StoreInfo(false)

	if err != nil || storeInfo == nil || storeInfo.Title == nil {
		logger.Info("compose app does not have store info with title set, re-using app name as title", zap.String("app", composeApp.Name))
		composeApp.SetTitle(composeApp.Name, common.DefaultLanguage)
	}

	removeRuntime(composeApp)

	// pass icon information to v1 label for backward compatibility, because we are
	// still using `func getContainerStats()` from `container.go` to get container stats
	// (we are being lazy to upgrade that v1 API to v2 - please help if you can :D)
	if err == nil && storeInfo != nil && storeInfo.Icon != "" {
		for i := range composeApp.Services {
			if composeApp.Services[i].Labels == nil {
				composeApp.Services[i].Labels = map[string]string{}
			}
			composeApp.Services[i].Labels[v1.V1LabelIcon] = storeInfo.Icon
		}
	}

	return composeApp, nil
}

func getNameFrom(composeYAML []byte) string {
	var baseStructure struct {
		Name string `yaml:"name"`
	}

	if err := yaml.Unmarshal(composeYAML, &baseStructure); err != nil {
		return ""
	}

	return baseStructure.Name
}

func (a *ComposeApp) SetUncontrolled(uncontrolled bool) error {
	xCasaosMap, ok := a.XCasaOS()

	// set to controlled app
	if !ok {
		logger.Error("failed to get map compose app extensions", zap.String("composeAppID", a.Name))
		return ErrComposeExtensionNameXCasaOSNotFound
	} else {
		xCasaosMap[common.ComposeExtensionPropertyNameIsUncontrolled] = uncontrolled
		a.Extensions[common.ComposeExtensionNameXCasaOS] = xCasaosMap
	}

	return nil
}
