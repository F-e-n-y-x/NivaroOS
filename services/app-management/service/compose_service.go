package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	timeutils "github.com/F-e-n-y-x/NivaroOS/services/common/utils/time"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
	"github.com/docker/docker/client"

	"go.uber.org/zap"
)

type ComposeService struct {
	installationInProgress sync.Map
}

func (s *ComposeService) PrepareWorkingDirectory(name string) (string, error) {
	workingDirectory := filepath.Join(config.AppInfo.AppsPath, name)

	if err := file.IsNotExistMkDir(workingDirectory); err != nil {
		logger.Error("failed to create working dir", zap.Error(err), zap.String("path", workingDirectory))
		return "", err
	}

	return workingDirectory, nil
}

func (s *ComposeService) IsInstalling(appName string) bool {
	_, ok := s.installationInProgress.Load(appName)
	return ok
}

// Install writes the compose file of a new app and installs it in the
// background. It fails with ErrComposeAppAlreadyInstalling when an install of
// the same name is in flight and ErrComposeAppAlreadyInstalled when a compose
// project with that name exists (use apply settings / update to change it).
func (s *ComposeService) Install(ctx context.Context, composeApp *ComposeApp) (err error) {
	name := composeApp.Name

	// reserve the name before touching any file, so two concurrent installs
	// of the same app can not both write compose.yaml
	if _, loaded := s.installationInProgress.LoadOrStore(name, true); loaded {
		return ErrComposeAppAlreadyInstalling
	}

	started := false
	defer func() {
		if !started {
			s.installationInProgress.Delete(name)
		}
	}()

	if exists, existsErr := s.Exists(ctx, name); existsErr != nil {
		return existsErr
	} else if exists {
		return ErrComposeAppAlreadyInstalled
	}

	// set store_app_id (by convention is the same as app name at install time if it does not exist)
	_, isStoreApp := composeApp.SetStoreAppID(composeApp.Name)
	if !isStoreApp {
		logger.Info("the compose app getting installed is not a store app, skipping store app id setting.")
	}

	logger.Info("installing compose app", zap.String("name", composeApp.Name))

	composeYAMLInterpolated, err := yaml.Marshal(composeApp)
	if err != nil {
		return err
	}

	workingDirectory, err := s.PrepareWorkingDirectory(composeApp.Name)
	if err != nil {
		return err
	}

	yamlFilePath := filepath.Join(workingDirectory, common.ComposeYAMLFileName)

	if err := os.WriteFile(yamlFilePath, composeYAMLInterpolated, 0o600); err != nil {
		logger.Error("failed to save compose file", zap.Error(err), zap.String("path", yamlFilePath))

		if err := file.RMDir(workingDirectory); err != nil {
			logger.Error("failed to cleanup working dir after failing to save compose file", zap.Error(err), zap.String("path", workingDirectory))
		}
		return err
	}

	// load project
	composeApp, err = LoadComposeAppFromConfigFile(name, yamlFilePath)

	if err != nil {
		logger.Error("failed to install compose app", zap.Error(err), zap.String("name", name))
		cleanup(workingDirectory)
		return err
	}

	// prepare for message bus events
	eventProperties := common.PropertiesFromContext(ctx)
	if eventProperties != nil {
		eventProperties[common.PropertyTypeAppName.Name] = composeApp.Name
	}

	if err := composeApp.UpdateEventPropertiesFromStoreInfo(eventProperties); err != nil {
		logger.Info("failed to update event properties from store info", zap.Error(err), zap.String("name", composeApp.Name))
	}

	started = true

	go func(ctx context.Context) {
		defer func() {
			s.installationInProgress.Delete(name)
		}()

		go PublishEventWrapper(ctx, common.EventTypeAppInstallBegin, nil)

		defer PublishEventWrapper(ctx, common.EventTypeAppInstallEnd, nil)

		if err := composeApp.PullAndInstall(ctx); err != nil {
			go PublishEventWrapper(ctx, common.EventTypeAppInstallError, map[string]string{
				common.PropertyTypeMessage.Name: err.Error(),
			})

			logger.Error("failed to install compose app", zap.Error(err), zap.String("name", composeApp.Name))
		}
	}(ctx)

	return nil
}

func (s *ComposeService) Uninstall(ctx context.Context, composeApp *ComposeApp, deleteConfigFolder bool) error {
	// prepare for message bus events
	eventProperties := common.PropertiesFromContext(ctx)
	if eventProperties != nil {
		eventProperties[common.PropertyTypeAppName.Name] = composeApp.Name
	}

	if err := composeApp.UpdateEventPropertiesFromStoreInfo(eventProperties); err != nil {
		logger.Info("failed to update event properties from store info", zap.Error(err), zap.String("name", composeApp.Name))
	}

	go func(ctx context.Context) {
		go PublishEventWrapper(ctx, common.EventTypeAppUninstallBegin, nil)

		defer PublishEventWrapper(ctx, common.EventTypeAppUninstallEnd, nil)

		if err := composeApp.Uninstall(ctx, deleteConfigFolder); err != nil {
			go PublishEventWrapper(ctx, common.EventTypeAppUninstallError, map[string]string{
				common.PropertyTypeMessage.Name: err.Error(),
			})

			logger.Error("failed to uninstall compose app", zap.Error(err), zap.String("name", composeApp.Name))
		}
	}(ctx)

	return nil
}

func (s *ComposeService) stacks(ctx context.Context) ([]api.Stack, error) {
	service, dockerClient, err := apiService()
	if err != nil {
		return nil, err
	}
	defer dockerClient.Close()

	return service.List(ctx, api.ListOptions{
		All: true,
	})
}

func (s *ComposeService) Status(ctx context.Context, appID string) (string, error) {
	stackList, err := s.stacks(ctx)
	if err != nil {
		return "", err
	}

	for _, stack := range stackList {
		if stack.ID == appID {
			return stack.Status, nil
		}
	}

	return "", ErrComposeAppNotFound
}

// Exists reports whether a compose project with this name exists (it has
// containers), without parsing any compose file.
func (s *ComposeService) Exists(ctx context.Context, appID string) (bool, error) {
	stackList, err := s.stacks(ctx)
	if err != nil {
		return false, err
	}

	return lo.ContainsBy(stackList, func(stack api.Stack) bool { return stack.ID == appID }), nil
}

// stackConfigFiles splits the comma separated config files of a stack
// (a project can be made of several compose files).
func stackConfigFiles(configFiles string) []string {
	return lo.FilterMap(strings.Split(configFiles, ","), func(f string, _ int) (string, bool) {
		f = strings.TrimSpace(f)
		return f, f != ""
	})
}

func stackFilesExist(stack api.Stack) bool {
	files := stackConfigFiles(stack.ConfigFiles)
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			return false
		}
	}
	return true
}

func loadStack(stack api.Stack) (*ComposeApp, error) {
	return LoadComposeAppFromConfigFiles(stack.ID, stackConfigFiles(stack.ConfigFiles))
}

// Get loads one installed compose app by name; only its own compose files
// are parsed (List parses every installed app). It returns
// ErrComposeAppNotFound when there is no such project.
func (s *ComposeService) Get(ctx context.Context, appID string) (*ComposeApp, error) {
	stackList, err := s.stacks(ctx)
	if err != nil {
		return nil, err
	}

	for _, stack := range stackList {
		if stack.ID != appID {
			continue
		}

		composeApp, err := loadStack(stack)
		if err != nil {
			logger.Error("failed to load compose file", zap.Error(err), zap.String("path", stack.ConfigFiles))
			return nil, err
		}

		return composeApp, nil
	}

	return nil, ErrComposeAppNotFound
}

func (s *ComposeService) List(ctx context.Context) (map[string]*ComposeApp, error) {
	stackList, err := s.stacks(ctx)
	if err != nil {
		return nil, err
	}

	result := map[string]*ComposeApp{}

	for _, stack := range stackList {
		// Projects whose compose files aren't on this host (Portainer keeps
		// its stacks under its own container's /data) aren't NivaroOS
		// apps: skip them quietly instead of logging an error per stack on
		// every list call.
		if !stackFilesExist(stack) {
			continue
		}

		composeApp, err := loadStack(stack)
		if err != nil {
			logger.Error("failed to load compose file", zap.Error(err), zap.String("path", stack.ConfigFiles))
			continue
		}

		result[stack.ID] = composeApp
	}

	return result, nil
}

func NewComposeService() *ComposeService {
	return &ComposeService{
		installationInProgress: sync.Map{},
	}
}

func baseInterpolationMap() map[string]string {
	return map[string]string{
		"DefaultUserName": common.DefaultUserName,
		"DefaultPassword": common.DefaultPassword,
		"PUID":            common.DefaultPUID,
		"PGID":            common.DefaultPGID,
		"TZ":              timeutils.GetSystemTimeZoneName(),
	}
}

// sharedDockerClient is handed out by apiService: callers `defer Close()`
// it, which must not close the process-wide client.
type sharedDockerClient struct {
	client.APIClient
}

func (sharedDockerClient) Close() error { return nil }

var (
	apiServiceMu     sync.Mutex
	apiServiceCached api.Service
	apiClientCached  client.APIClient
)

// apiService returns the process-wide compose service. Building a DockerCli
// (config file, context store, API version negotiation) on every request was
// costly; the compose service and docker client are safe for concurrent use.
// A failed initialisation is not cached, so it is retried on the next call.
func apiService() (api.Service, client.APIClient, error) {
	apiServiceMu.Lock()
	defer apiServiceMu.Unlock()

	if apiServiceCached != nil {
		return apiServiceCached, sharedDockerClient{apiClientCached}, nil
	}

	dockerCli, err := command.NewDockerCli()
	if err != nil {
		return nil, nil, err
	}

	if err := dockerCli.Initialize(&flags.ClientOptions{}); err != nil {
		return nil, nil, err
	}

	apiServiceCached = compose.NewComposeService(dockerCli)
	apiClientCached = dockerCli.Client()

	return apiServiceCached, sharedDockerClient{apiClientCached}, nil
}

func ApiService() (api.Service, client.APIClient, error) {
	return apiService()
}

func cleanup(workDir string) {
	logger.Info("cleaning up working dir", zap.String("path", workDir))
	if err := file.RMDir(workDir); err != nil {
		logger.Error("failed to cleanup working dir", zap.Error(err), zap.String("path", workDir))
	}
}
