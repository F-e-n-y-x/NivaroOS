package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/docker"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/utils/downloadHelper"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/bluele/gcache"
	"github.com/docker/docker/client"
	"github.com/samber/lo"
	"go.uber.org/zap"
)

var (
	ErrAppStoreSourceExists      = fmt.Errorf("appstore source already exists")
	ErrAppStoreSourceRegistering = fmt.Errorf("appstore source is already being registered")
	ErrAppStoreSourceNotFound    = fmt.Errorf("appstore source not found")
	ErrAppStoreInvalidURL        = fmt.Errorf("invalid appstore url - an http(s) link to a .zip file is expected")
)

// how long a failed "is an update available" check is remembered, so a
// broken registry or missing image does not get re-checked on every list
const updateCheckFailureTTL = 10 * time.Minute

type AppStoreManagement struct {
	isAppUpgradable      gcache.Cache
	defaultAppStore      AppStore
	isAppUpgrading       sync.Map
	onAppStoreRegister   []func(string) error
	onAppStoreUnregister []func(string) error

	// url (lowercase) -> struct{} while a register is in flight
	registering sync.Map

	// held while UpdateCatalog runs, so cron runs never overlap
	updatingCatalog sync.Mutex
}

func (a *AppStoreManagement) AppStoreList() []codegen.AppStoreMetadata {
	return lo.Map(config.AppStoreList(), func(appStoreURL string, id int) codegen.AppStoreMetadata {
		appStore, err := AppStoreByURL(appStoreURL)
		if err != nil {
			logger.Error("failed to construct appstore", zap.Error(err), zap.String("appstoreURL", appStoreURL))
			return codegen.AppStoreMetadata{}
		}

		workDir, err := appStore.WorkDir()
		if err != nil {
			logger.Error("failed to get appstore workdir", zap.Error(err), zap.String("appstoreURL", appStoreURL))
			return codegen.AppStoreMetadata{}
		}

		storeRoot, err := StoreRoot(workDir)
		if err != nil {
			logger.Error("failed to get appstore storeRoot", zap.Error(err), zap.String("appstoreURL", appStoreURL))
			storeRoot = "internal error - store root not found"
		}

		return codegen.AppStoreMetadata{
			ID:        &id,
			URL:       &appStoreURL,
			StoreRoot: &storeRoot,
		}
	})
}

func (a *AppStoreManagement) OnAppStoreRegister(fn func(string) error) {
	a.onAppStoreRegister = append(a.onAppStoreRegister, fn)
}

func (a *AppStoreManagement) OnAppStoreUnregister(fn func(string) error) {
	a.onAppStoreUnregister = append(a.onAppStoreUnregister, fn)
}

func (a *AppStoreManagement) ChangeGlobal(key string, value string) error {
	if err := config.SetGlobal(key, value); err != nil {
		return err
	}

	go func() {
		if err := config.SaveGlobal(); err != nil {
			logger.Error("failed to save global env", zap.Error(err), zap.String("key", key))
			return
		}
	}()

	return nil
}

func (a *AppStoreManagement) DeleteGlobal(key string) error {
	config.DeleteGlobal(key)

	go func() {
		if err := config.SaveGlobal(); err != nil {
			logger.Error("failed to delete global env", zap.Error(err), zap.String("key", key))
			return
		}
	}()

	return nil
}

// ValidateAppStoreURL checks a store url synchronously, before any download.
func ValidateAppStoreURL(appstoreURL string) error {
	if err := downloadHelper.ValidateURL(appstoreURL); err != nil {
		return fmt.Errorf("%w: %s", ErrAppStoreInvalidURL, err.Error())
	}

	return nil
}

func isAppStoreRegistered(appstoreURL string) bool {
	return lo.ContainsBy(config.AppStoreList(), func(url string) bool { return strings.EqualFold(url, appstoreURL) })
}

// beginRegister validates the url and reserves it, so two concurrent
// registers of the same url cannot both run. The returned func releases it.
func (a *AppStoreManagement) beginRegister(appstoreURL string) (func(), error) {
	if err := ValidateAppStoreURL(appstoreURL); err != nil {
		return nil, err
	}

	if isAppStoreRegistered(appstoreURL) {
		return nil, ErrAppStoreSourceExists
	}

	key := strings.ToLower(appstoreURL)
	if _, loaded := a.registering.LoadOrStore(key, struct{}{}); loaded {
		return nil, ErrAppStoreSourceRegistering
	}

	return func() { a.registering.Delete(key) }, nil
}

func (a *AppStoreManagement) registerAppStore(ctx context.Context, appstoreURL string, callbacks []func(*codegen.AppStoreMetadata)) (err error) {
	// every event of this register carries the store url
	eventProperties := map[string]string{common.PropertyTypeAppStoreURL.Name: appstoreURL}

	go PublishEventWrapper(ctx, common.EventTypeAppStoreRegisterBegin, lo.Assign(eventProperties))

	defer func() {
		if err != nil {
			// error first, then end - both carry the url; end also carries the
			// error message so a listener of only "-end" can tell it failed
			PublishEventWrapper(ctx, common.EventTypeAppStoreRegisterError, lo.Assign(eventProperties, map[string]string{
				common.PropertyTypeMessage.Name: err.Error(),
			}))
			PublishEventWrapper(ctx, common.EventTypeAppStoreRegisterEnd, lo.Assign(eventProperties, map[string]string{
				common.PropertyTypeMessage.Name: err.Error(),
			}))
			return
		}

		PublishEventWrapper(ctx, common.EventTypeAppStoreRegisterEnd, lo.Assign(eventProperties))
	}()

	appstore, err := AppStoreByURL(appstoreURL)
	if err != nil {
		return err
	}

	if err = appstore.UpdateCatalog(); err != nil {
		logger.Error("failed to update appstore catalog", zap.Error(err), zap.String("appstoreURL", appstoreURL))
		return err
	}

	// if everything is good, add to the list
	added, err := config.AddAppStore(appstoreURL)
	if err != nil {
		logger.Error("failed to save appstore list", zap.Error(err), zap.String("appstoreURL", appstoreURL))
		return err
	}

	if !added {
		return ErrAppStoreSourceExists
	}

	for _, fn := range a.onAppStoreRegister {
		if err := fn(appstoreURL); err != nil {
			logger.Error("failed to run onAppStoreRegister", zap.Error(err), zap.String("appstoreURL", appstoreURL))
		}
	}

	list := config.AppStoreList()
	id := lo.IndexOf(list, appstoreURL)

	appStoreMetadata := &codegen.AppStoreMetadata{
		ID:  utils.Ptr(id),
		URL: &appstoreURL,
	}

	for _, callback := range callbacks {
		callback(appStoreMetadata)
	}

	return nil
}

// RegisterAppStore validates the url synchronously (ErrAppStoreInvalidURL,
// ErrAppStoreSourceExists, ErrAppStoreSourceRegistering) and then downloads
// and registers the store in the background. The outcome is published as
// app-store:register-end / app-store:register-error, both with the url.
func (a *AppStoreManagement) RegisterAppStore(ctx context.Context, appstoreURL string, callbacks ...func(*codegen.AppStoreMetadata)) error {
	release, err := a.beginRegister(appstoreURL)
	if err != nil {
		return err
	}

	go func() {
		defer release()

		_ = a.registerAppStore(ctx, appstoreURL, callbacks)
	}()

	return nil
}

func (a *AppStoreManagement) RegisterAppStoreSync(ctx context.Context, appstoreURL string, callbacks ...func(*codegen.AppStoreMetadata)) error {
	release, err := a.beginRegister(appstoreURL)
	if err != nil {
		return err
	}
	defer release()

	return a.registerAppStore(ctx, appstoreURL, callbacks)
}

// UnregisterAppStore removes the store at index appStoreID of AppStoreList().
func (a *AppStoreManagement) UnregisterAppStore(appStoreID uint) error {
	list := config.AppStoreList()
	if appStoreID >= uint(len(list)) {
		return fmt.Errorf("appstore id %d out of range", appStoreID)
	}

	return a.UnregisterAppStoreByURL(list[appStoreID])
}

// UnregisterAppStoreByURL removes a store by its url, which (unlike its
// index) does not shift when the list changes concurrently.
func (a *AppStoreManagement) UnregisterAppStoreByURL(appStoreURL string) error {
	// remove appstore from list
	removed, err := config.RemoveAppStore(appStoreURL)
	if err != nil {
		return err
	}

	if !removed {
		return ErrAppStoreSourceNotFound
	}

	// remove appstore workdir
	{
		appStore, err := AppStoreByURL(appStoreURL)
		if err != nil {
			return err
		}

		workdir, err := appStore.WorkDir()
		if err != nil {
			logger.Error("error while getting appstore workdir", zap.Error(err), zap.String("url", appStoreURL))
		}

		if len(workdir) != 0 {
			if err := file.RMDir(workdir); err != nil {
				logger.Error("error while removing appstore workdir", zap.Error(err), zap.String("workdir", workdir))
			}
		}

		forgetAppStore(appStoreURL)
	}

	for _, fn := range a.onAppStoreUnregister {
		if err := fn(appStoreURL); err != nil {
			return err
		}
	}
	return nil
}

func (a *AppStoreManagement) AppStoreMap() (map[string]AppStore, error) {
	appStoreMap := lo.SliceToMap(config.AppStoreList(), func(appStoreURL string) (string, AppStore) {
		appStore, err := AppStoreByURL(appStoreURL)
		if err != nil {
			return "", nil
		}
		return appStoreURL, appStore
	})

	delete(appStoreMap, "")

	return appStoreMap, nil
}

// AppStore interface
func (a *AppStoreManagement) CategoryMap() (map[string]codegen.CategoryInfo, error) {
	appStoreMap, err := a.AppStoreMap()
	if err != nil {
		return nil, err
	}

	allFailed := true

	categoryMap := map[string]codegen.CategoryInfo{}
	for _, appStore := range appStoreMap {
		c, err := appStore.CategoryMap()
		if err != nil {
			logger.Error("error while loading category map", zap.Error(err))
			continue
		}

		allFailed = false

		for name, category := range c {
			categoryMap[name] = category
		}
	}

	if allFailed {
		logger.Info("all appstores failed to load category map, using default")

		categoryMap, err = a.defaultAppStore.CategoryMap()
		if err != nil {
			return nil, err
		}
	}

	for name, category := range categoryMap {
		category.Count = utils.Ptr(0)
		categoryMap[name] = category
	}

	catalog, err := a.Catalog()
	if err != nil {
		return nil, err
	}

	for _, app := range catalog {
		storeInfo, err := app.StoreInfo(false)
		if err != nil {
			continue
		}

		category, ok := categoryMap[storeInfo.Category]
		if !ok {
			continue
		}

		category.Count = lo.ToPtr(*category.Count + 1)

		categoryMap[storeInfo.Category] = category
	}

	return categoryMap, nil
}

func (a *AppStoreManagement) Recommend() ([]string, error) {
	appStoreMap, err := a.AppStoreMap()
	if err != nil {
		logger.Error("error while loading appstore map", zap.Error(err))
		return nil, err
	}

	allFailed := true

	recommend := []string{}
	for _, appStore := range appStoreMap {
		r, err := appStore.Recommend()
		if err != nil {
			logger.Error("error while getting appstore recommend", zap.Error(err))
			continue
		}

		allFailed = false
		recommend = lo.Union(recommend, r)
	}

	if !allFailed {
		return recommend, nil
	}

	logger.Info("No appstore registered")
	if a.defaultAppStore == nil {
		logger.Info("WARNING - no default appstore")
		return nil, nil
	}

	logger.Info("Using default appstore")
	recommend, err = a.defaultAppStore.Recommend()
	if err != nil {
		logger.Error("error while getting default appstore recommend list", zap.Error(err))
		return nil, err
	}

	return recommend, nil
}

func (a *AppStoreManagement) Catalog() (map[string]*ComposeApp, error) {
	catalog := map[string]*ComposeApp{}

	appStoreMap, err := a.AppStoreMap()
	if err != nil {
		return nil, err
	}

	allFailed := true

	for _, appStore := range appStoreMap {

		c, err := appStore.Catalog()
		if err != nil {
			logger.Error("error while getting appstore catalog", zap.Error(err))
			continue
		}

		allFailed = false
		for storeAppID, composeApp := range c {
			catalog[storeAppID] = composeApp
		}
	}

	if !allFailed {
		return catalog, nil
	}

	logger.Info("No appstore registered")
	if a.defaultAppStore == nil {
		logger.Info("WARNING - no default appstore")
		return map[string]*ComposeApp{}, nil
	}

	logger.Info("Using default appstore")
	catalog, err = a.defaultAppStore.Catalog()
	if err != nil {
		return map[string]*ComposeApp{}, err
	}

	return catalog, nil
}

func (a *AppStoreManagement) UpdateCatalog() error {
	// never run two catalog updates at once (startup run, 10 min cron, ...):
	// if one is still running, skip this one
	if !a.updatingCatalog.TryLock() {
		logger.Info("previous appstore catalog update is still running - skipping this run")
		return nil
	}
	defer a.updatingCatalog.Unlock()

	// reload config.
	// the appstore may be change in runtime.
	config.ReloadConfig()

	appStoreMap, err := a.AppStoreMap()
	if err != nil {
		return err
	}

	for url, appStore := range appStoreMap {
		if err := updateStoreCatalogSafely(appStore); err != nil {
			logger.Error("error while updating catalog for app store", zap.Error(err), zap.String("url", url))
		}
	}

	// clean cache
	a.isAppUpgradable.Purge()

	return nil
}

// updateStoreCatalogSafely updates one store; a panic is logged as an error
// so one broken store never takes the process down.
func updateStoreCatalogSafely(appStore AppStore) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while updating appstore catalog: %v", r)
		}
	}()

	return appStore.UpdateCatalog()
}

func (a *AppStoreManagement) ComposeApp(id string) (*ComposeApp, error) {
	appStoreMap, err := a.AppStoreMap()
	if err != nil {
		return nil, err
	}

	for _, appStore := range appStoreMap {
		composeApp, appErr := appStore.ComposeApp(id)
		if appErr != nil {
			logger.Error("error while getting appstore compose app", zap.Error(appErr))
			continue
		}

		if composeApp != nil {
			return composeApp, nil
		}
	}

	logger.Info("app not found in any appstore", zap.String("id", id))

	if a.defaultAppStore == nil {
		logger.Info("WARNING - no default appstore")
		return nil, nil
	}

	logger.Info("Using default appstore")

	composeApp, err := a.defaultAppStore.ComposeApp(id)
	if err != nil {
		return nil, err
	}

	return composeApp, nil
}

func (a *AppStoreManagement) WorkDir() (string, error) {
	panic("not implemented and will never be implemented - this is a virtual appstore")
}

func (a *AppStoreManagement) IsUpdateAvailable(composeApp *ComposeApp) bool {
	storeID := composeApp.Name
	if value, err := a.isAppUpgradable.Get(storeID); err == nil {
		switch value := value.(type) {
		case bool:
			return value
		default:
			logger.Error("invalid type in cache", zap.String("storeID", storeID), zap.Any("value", value))
			return false
		}
	}

	isUpdate, err := a.isUpdateAvailable(composeApp)
	if err != nil {
		logger.Error("failed to check if update is available", zap.Error(err))
		// remember the failure for a while too, so a broken check is not
		// repeated (registry round trips) on every list request
		_ = a.isAppUpgradable.SetWithExpire(storeID, false, updateCheckFailureTTL)
		return false
	}
	_ = a.isAppUpgradable.Set(storeID, isUpdate)
	return isUpdate
}

func (a *AppStoreManagement) isUpdateAvailable(composeApp *ComposeApp) (bool, error) {
	// handle no tag logic and for easy to test
	storeInfo, err := composeApp.StoreInfo(false)
	if err != nil {
		logger.Error("failed to get store info of compose app, thus no update available", zap.Error(err))
		return false, nil
	}

	if storeInfo == nil || storeInfo.StoreAppID == nil || *storeInfo.StoreAppID == "" {
		return false, nil
	}

	// if app is uncontrolled, no update available
	if storeInfo.IsUncontrolled != nil && *storeInfo.IsUncontrolled {
		return false, nil
	}

	storeComposeApp, err := a.ComposeApp(*storeInfo.StoreAppID)
	if err != nil {
		logger.Error("failed to get store compose app, thus no update available", zap.Error(err))
		return false, err
	}

	if storeComposeApp == nil {
		logger.Error("store compose app not found, thus no update available", zap.String("storeAppID", *storeInfo.StoreAppID))
		return false, nil
	}

	return a.IsUpdateAvailableWith(composeApp, storeComposeApp)
}

// the patch is have no choice
// the digest compare is not work for these images
// I don't know why, but I have to do this
// I will remove the patch after I rewrite the digest compare
var NoUpdateBlacklist = []string{
	"johnguan/stable-diffusion-webui:latest",
}

func (a *AppStoreManagement) IsUpdateAvailableWith(composeApp *ComposeApp, storeComposeApp *ComposeApp) (bool, error) {
	currentTag, err := composeApp.MainTag()
	if err != nil {
		logger.Error("failed to get current tag", zap.Error(err))
		return false, err
	}
	mainService, err := composeApp.MainService()
	if err != nil {
		logger.Error("failed to get main service", zap.Error(err))
		return false, err
	}
	if lo.Contains(common.NeedCheckDigestTags, currentTag) {
		ctx := context.Background()
		cli, clientErr := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if clientErr != nil {
			logger.Error("failed to create docker client", zap.Error(clientErr))
			return false, clientErr
		}
		defer cli.Close()

		if lo.Contains(NoUpdateBlacklist, mainService.Image) {
			return false, nil
		}

		image, _ := docker.ExtractImageAndTag(mainService.Image)

		imageInfo, _, clientErr := cli.ImageInspectWithRaw(ctx, image)
		if clientErr != nil {
			logger.Error("failed to inspect image", zap.Error(clientErr))
			return false, clientErr
		}

		match, clientErr := docker.CompareDigest(mainService.Image, imageInfo.RepoDigests)
		if clientErr != nil {
			logger.Error("failed to compare digest", zap.Error(clientErr))
			return false, clientErr
		}
		// match means no update available
		return !match, nil
	}
	storeTag, err := storeComposeApp.MainTag()
	return currentTag != storeTag, err
}

func (a *AppStoreManagement) IsUpdating(appID string) bool {
	_, ok := a.isAppUpgrading.Load(appID)
	return ok
}

func (a *AppStoreManagement) StartUpgrade(appID string) {
	a.isAppUpgrading.Store(appID, struct{}{})
}

func (a *AppStoreManagement) FinishUpgrade(appID string) {
	a.isAppUpgrading.Delete(appID)
	a.isAppUpgradable.Remove(appID)
}

func NewAppStoreManagement() *AppStoreManagement {
	defaultAppStore, err := NewDefaultAppStore()
	if err != nil {
		fmt.Printf("error while loading default appstore: %s\n", err.Error())
	}

	appStoreManagement := &AppStoreManagement{
		defaultAppStore: defaultAppStore,
		isAppUpgradable: gcache.New(100).LRU().Expiration(1 * time.Hour).Build(),
		isAppUpgrading:  sync.Map{},
	}

	return appStoreManagement
}
