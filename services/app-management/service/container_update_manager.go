package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	client2 "github.com/docker/docker/client"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/docker"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
)

type ContainerUpdateInfo struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Image              string `json:"image"`
	ImageID            string `json:"image_id"`
	State              string `json:"state"`
	Status             string `json:"status"`
	HasUpdate          bool   `json:"has_update"`
	CurrentDigest      string `json:"current_digest"`
	LatestDigest       string `json:"latest_digest"`
	AutoUpdateEnabled  bool   `json:"auto_update_enabled"`
	AutoUpdateSchedule string `json:"auto_update_schedule"` // e.g. "0 3 * * *"
	LastCheckedAt      string `json:"last_checked_at"`
	LastUpdatedAt      string `json:"last_updated_at"`
	CreatedAt          string `json:"created_at"`
	IsAppStoreApp      bool   `json:"is_appstore_app"`
	// Where the image comes from (registry / git / local build); decides
	// how it is checked and updated. Filled in when listing.
	Source *ContainerSource `json:"source,omitempty"`
}

type GlobalAutoUpdateConfig struct {
	Enabled  bool   `json:"enabled"`
	Schedule string `json:"schedule"` // e.g. "0 3 * * *" (daily at 3 AM)
}

type ContainerUpdateManager struct {
	mu          sync.RWMutex
	configs     map[string]ContainerUpdateInfo
	global      GlobalAutoUpdateConfig
	dataFile    string
	cron        *cron.Cron
	cronEntryID cron.EntryID
}

var (
	GlobalContainerUpdateMgr *ContainerUpdateManager
	mgrOnce                  sync.Once
)

func getContainerUpdateDataPath() string {
	paths := []string{
		"/var/lib/nivaroos/container_updates.json",
		"/var/lib/casaos/container_updates.json",
	}
	for _, p := range paths {
		dir := filepath.Dir(p)
		if _, err := os.Stat(dir); err == nil {
			return p
		}
	}
	_ = os.MkdirAll("/var/lib/nivaroos", 0755)
	return "/var/lib/nivaroos/container_updates.json"
}

func GetContainerUpdateManager() *ContainerUpdateManager {
	mgrOnce.Do(func() {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		c := cron.New(cron.WithParser(parser))
		c.Start()

		GlobalContainerUpdateMgr = &ContainerUpdateManager{
			configs:  make(map[string]ContainerUpdateInfo),
			global:   GlobalAutoUpdateConfig{Enabled: false, Schedule: "0 3 * * *"},
			dataFile: getContainerUpdateDataPath(),
			cron:     c,
		}
		GlobalContainerUpdateMgr.load()
		GlobalContainerUpdateMgr.reschedule()
	})
	return GlobalContainerUpdateMgr
}

type storedUpdateData struct {
	Global  GlobalAutoUpdateConfig         `json:"global"`
	Configs map[string]ContainerUpdateInfo `json:"configs"`
}

func (m *ContainerUpdateManager) load() {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.dataFile)
	if err != nil {
		return
	}

	var stored storedUpdateData
	if err := json.Unmarshal(data, &stored); err == nil {
		if stored.Global.Schedule != "" {
			m.global = stored.Global
		}
		if stored.Configs != nil {
			m.configs = stored.Configs
		}
	}
	before := len(m.configs)
	m.normalizeKeysLocked()
	if len(m.configs) != before {
		_ = m.saveLocked()
	}
}

var containerIDRe = regexp.MustCompile(`^[0-9a-f]{12}([0-9a-f]{52})?$`)

// normalizeKeysLocked keeps one entry per container name. Settings used to
// be saved under container IDs too, which change on every recreate - so
// the per-container Auto switch was lost after each update and the file
// filled with orphans.
func (m *ContainerUpdateManager) normalizeKeysLocked() {
	for key, cfg := range m.configs {
		if !containerIDRe.MatchString(key) {
			continue
		}
		delete(m.configs, key)
		if cfg.Name == "" {
			continue // orphan of a container that no longer exists
		}
		if existing, ok := m.configs[cfg.Name]; ok {
			// Keep the name entry's data but not lose an opt-in.
			existing.AutoUpdateEnabled = existing.AutoUpdateEnabled || cfg.AutoUpdateEnabled
			m.configs[cfg.Name] = existing
		} else {
			m.configs[cfg.Name] = cfg
		}
	}
}

// nameOf resolves a container ID (or name) to its name.
func containerName(ctx context.Context, nameOrID string) string {
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return nameOrID
	}
	defer cli.Close()
	if inspect, err := cli.ContainerInspect(ctx, nameOrID); err == nil {
		return strings.TrimPrefix(inspect.Name, "/")
	}
	return nameOrID
}

func (m *ContainerUpdateManager) anyOptedInLocked() bool {
	for _, cfg := range m.configs {
		if cfg.AutoUpdateEnabled {
			return true
		}
	}
	return false
}

func (m *ContainerUpdateManager) saveLocked() error {
	dir := filepath.Dir(m.dataFile)
	_ = os.MkdirAll(dir, 0755)

	stored := storedUpdateData{
		Global:  m.global,
		Configs: m.configs,
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.dataFile, data, 0644)
}

func (m *ContainerUpdateManager) reschedule() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cronEntryID != 0 {
		m.cron.Remove(m.cronEntryID)
		m.cronEntryID = 0
	}

	// Run when everything is auto-updated, or when at least one container
	// opted in on its own (those were never updated without the global
	// switch).
	if (m.global.Enabled || m.anyOptedInLocked()) && m.global.Schedule != "" {
		entryID, err := m.cron.AddFunc(m.global.Schedule, func() {
			m.RunAutoUpdates()
		})
		if err == nil {
			m.cronEntryID = entryID
		}
	}
}

func (m *ContainerUpdateManager) GetGlobalConfig() GlobalAutoUpdateConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.global
}

func (m *ContainerUpdateManager) SetGlobalConfig(cfg GlobalAutoUpdateConfig) error {
	// An invalid schedule used to be saved (and reported as saved) while
	// the timer silently stopped running.
	if cfg.Schedule == "" {
		cfg.Schedule = "0 3 * * *"
	}
	if _, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor).Parse(cfg.Schedule); err != nil {
		return fmt.Errorf("invalid schedule %q: %w", cfg.Schedule, err)
	}
	m.mu.Lock()
	m.global = cfg
	err := m.saveLocked()
	m.mu.Unlock()

	m.reschedule()
	return err
}

func (m *ContainerUpdateManager) SetContainerAutoUpdate(nameOrID string, enabled bool, schedule string) error {
	name := containerName(context.Background(), nameOrID)
	m.mu.Lock()
	cfg := m.configs[name]
	cfg.Name = name
	cfg.AutoUpdateEnabled = enabled
	if schedule != "" {
		cfg.AutoUpdateSchedule = schedule
	}
	m.configs[name] = cfg
	err := m.saveLocked()
	m.mu.Unlock()
	m.reschedule() // the timer depends on whether anything opted in
	return err
}

func (m *ContainerUpdateManager) GetAllContainersWithUpdates(ctx context.Context) ([]ContainerUpdateInfo, error) {
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	toHost := dockerPathMapper(ctx)
	result := make([]ContainerUpdateInfo, 0, len(containers))
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		cfg := m.configs[name]

		displayImage := c.Image
		if strings.HasPrefix(displayImage, "sha256:") || len(displayImage) == 12 {
			if cfg.Image != "" && !strings.HasPrefix(cfg.Image, "sha256:") {
				displayImage = cfg.Image
			} else {
				if inspect, err := cli.ContainerInspect(ctx, c.ID); err == nil && inspect.Config != nil && inspect.Config.Image != "" {
					displayImage = inspect.Config.Image
				}
			}
		}

		info := ContainerUpdateInfo{
			ID:                 c.ID,
			Name:               name,
			Image:              displayImage,
			ImageID:            c.ImageID,
			State:              c.State,
			Status:             c.Status,
			HasUpdate:          cfg.HasUpdate,
			CurrentDigest:      cfg.CurrentDigest,
			LatestDigest:       cfg.LatestDigest,
			AutoUpdateEnabled:  cfg.AutoUpdateEnabled,
			AutoUpdateSchedule: cfg.AutoUpdateSchedule,
			LastCheckedAt:      cfg.LastCheckedAt,
			LastUpdatedAt:      cfg.LastUpdatedAt,
			CreatedAt:          time.Unix(c.Created, 0).Format(time.RFC3339),
			IsAppStoreApp:      c.Labels["casaos.app"] != "" || c.Labels["com.docker.compose.project"] != "",
		}
		src := detectSource(c.Labels, toHost)
		info.Source = &src
		if src.Kind == "local" {
			info.HasUpdate = false // nothing upstream to compare with
		}
		result = append(result, info)
	}

	return result, nil
}

func (m *ContainerUpdateManager) CheckContainerUpdate(ctx context.Context, nameOrID string) (*ContainerUpdateInfo, error) {
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	inspect, raw, err := cli.ContainerInspectWithRaw(ctx, nameOrID, false)
	if err != nil {
		return nil, err
	}

	// Built from a compose project: its image is in no registry. A git
	// checkout (e.g. from GitHub) has an update when upstream has commits
	// it doesn't; a plain folder has nothing to compare with.
	if src := detectSource(inspect.Config.Labels, dockerPathMapper(ctx)); src.Kind != "registry" {
		name := strings.TrimPrefix(inspect.Name, "/")
		behind := 0
		if src.Kind == "git" {
			n, err := gitBehind(ctx, src)
			if err != nil {
				return nil, err
			}
			behind = n
		}
		src.Behind = behind
		m.mu.Lock()
		cfg := m.configs[name]
		cfg.ID, cfg.Name, cfg.Image, cfg.State = inspect.ID, name, inspect.Config.Image, inspect.State.Status
		cfg.HasUpdate = behind > 0
		cfg.LastCheckedAt = time.Now().Format(time.RFC3339)
		m.configs[name] = cfg
		_ = m.saveLocked()
		m.mu.Unlock()
		cfg.Source = &src
		return &cfg, nil
	}

	var rawInspect struct {
		ImageManifestDescriptor *struct {
			Digest string `json:"digest"`
		} `json:"ImageManifestDescriptor"`
	}
	_ = json.Unmarshal(raw, &rawInspect)

	imageName := inspect.Config.Image
	if imageName == "" {
		return nil, fmt.Errorf("container has no image")
	}

	hasUpdate := false
	var currentDigest, latestDigest string

	// Build image reference with tag (e.g. portainer/portainer-ce:latest)
	imageRef := imageName
	if !strings.Contains(imageRef, ":") && !strings.Contains(imageRef, "@") && !strings.HasPrefix(imageRef, "sha256:") {
		imageRef = imageRef + ":latest"
	}

	// Inspect local image for this container to get repo digests and image ID
	imageInfo, _, imgErr := cli.ImageInspectWithRaw(ctx, inspect.Image)
	if imgErr == nil && len(imageInfo.RepoDigests) > 0 {
		parts := strings.Split(imageInfo.RepoDigests[0], "@")
		if len(parts) > 1 {
			currentDigest = parts[1]
		} else {
			currentDigest = imageInfo.RepoDigests[0]
		}
	}
	if currentDigest == "" {
		if rawInspect.ImageManifestDescriptor != nil && rawInspect.ImageManifestDescriptor.Digest != "" {
			currentDigest = rawInspect.ImageManifestDescriptor.Digest
		} else if inspect.Image != "" {
			currentDigest = inspect.Image
		}
	}

	var digestsToCompare []string
	if imgErr == nil && len(imageInfo.RepoDigests) > 0 {
		digestsToCompare = append(digestsToCompare, imageInfo.RepoDigests...)
	}
	if currentDigest != "" {
		digestsToCompare = append(digestsToCompare, currentDigest)
	}
	if inspect.Image != "" {
		digestsToCompare = append(digestsToCompare, inspect.Image)
	}
	if rawInspect.ImageManifestDescriptor != nil && rawInspect.ImageManifestDescriptor.Digest != "" {
		digestsToCompare = append(digestsToCompare, rawInspect.ImageManifestDescriptor.Digest)
	}

	// If image is pinned by sha256, it cannot be updated from remote registry
	if strings.Contains(imageName, "@sha256:") || strings.HasPrefix(imageName, "sha256:") {
		hasUpdate = false
	} else {
		// 1. Check if host Docker already has a newer image ID pulled for this tag
		localLatest, _, localErr := cli.ImageInspectWithRaw(ctx, imageRef)
		if localErr == nil && localLatest.ID != "" && inspect.Image != "" && localLatest.ID != inspect.Image {
			hasUpdate = true
			if len(localLatest.RepoDigests) > 0 {
				parts := strings.Split(localLatest.RepoDigests[0], "@")
				if len(parts) > 1 {
					latestDigest = parts[1]
				} else {
					latestDigest = localLatest.RepoDigests[0]
				}
			}
			if latestDigest == "" {
				latestDigest = localLatest.ID
			}
		}

		// 2. Query remote registry for the latest manifest digest
		match, remoteDigest, checkErr := docker.CompareDigestWithResult(imageRef, digestsToCompare)
		if checkErr == nil {
			if !match && remoteDigest != "" {
				hasUpdate = true
				latestDigest = remoteDigest
			} else if match {
				// Remote registry matched one of our local digests
				// Only clear update if local image is also not newer
				if localErr != nil || localLatest.ID == inspect.Image {
					hasUpdate = false
				}
			}
		} else {
			logger.Info("registry digest compare failed for image", zap.String("image", imageRef), zap.Error(checkErr))
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	name := strings.TrimPrefix(inspect.Name, "/")
	cfg := m.configs[name]
	cfg.ID = inspect.ID
	cfg.Name = name
	cfg.Image = imageName
	cfg.ImageID = inspect.Image
	cfg.State = inspect.State.Status
	cfg.LastCheckedAt = time.Now().Format(time.RFC3339)
	cfg.HasUpdate = hasUpdate
	if currentDigest != "" {
		cfg.CurrentDigest = currentDigest
	}
	if latestDigest != "" {
		cfg.LatestDigest = latestDigest
	}

	m.configs[name] = cfg
	_ = m.saveLocked()

	return &cfg, nil
}

func (m *ContainerUpdateManager) CheckAllContainersUpdate(ctx context.Context) ([]ContainerUpdateInfo, error) {
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}

	type job struct {
		id string
	}
	jobs := make(chan job, len(containers))
	for _, c := range containers {
		jobs <- job{id: c.ID}
	}
	close(jobs)

	workerCount := 4
	if len(containers) < workerCount {
		workerCount = len(containers)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				checkCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
				_, _ = m.CheckContainerUpdate(checkCtx, j.id)
				cancel()
			}
		}()
	}
	wg.Wait()

	return m.GetAllContainersWithUpdates(ctx)
}

func (m *ContainerUpdateManager) GetContainerInfo(ctx context.Context, nameOrID string) (*ContainerUpdateInfo, error) {
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	inspect, err := cli.ContainerInspect(ctx, nameOrID)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	name := strings.TrimPrefix(inspect.Name, "/")
	cfg := m.configs[name]

	info := &ContainerUpdateInfo{
		ID:                 inspect.ID,
		Name:               name,
		Image:              inspect.Config.Image,
		ImageID:            inspect.Image,
		State:              inspect.State.Status,
		Status:             inspect.State.Status,
		HasUpdate:          cfg.HasUpdate,
		CurrentDigest:      cfg.CurrentDigest,
		LatestDigest:       cfg.LatestDigest,
		AutoUpdateEnabled:  cfg.AutoUpdateEnabled,
		AutoUpdateSchedule: cfg.AutoUpdateSchedule,
		LastCheckedAt:      cfg.LastCheckedAt,
		LastUpdatedAt:      cfg.LastUpdatedAt,
		CreatedAt:          inspect.Created,
		IsAppStoreApp:      inspect.Config.Labels["casaos.app"] != "" || inspect.Config.Labels["com.docker.compose.project"] != "",
	}
	return info, nil
}

// UpdateAndRecreateContainer pulls the container's image and recreates the
// container only if that brought a newer image. updated reports whether it
// did; an image that can't be pulled is an error, never a silent success.
func (m *ContainerUpdateManager) UpdateAndRecreateContainer(ctx context.Context, nameOrID string) (info *ContainerUpdateInfo, updated bool, err error) {
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return nil, false, err
	}
	defer cli.Close()

	inspect, err := cli.ContainerInspect(ctx, nameOrID)
	if err != nil {
		return nil, false, err
	}

	if common.PropertiesFromContext(ctx) == nil {
		ctx = common.WithProperties(ctx, make(map[string]string))
	}

	name := strings.TrimPrefix(inspect.Name, "/")

	appIcon := ""
	if icon, ok := inspect.Config.Labels["casaos.icon"]; ok {
		appIcon = icon
	}
	go PublishEventWrapper(ctx, common.EventTypeAppApplyChangesBegin, map[string]string{
		common.PropertyTypeAppName.Name:  name,
		common.PropertyTypeAppTitle.Name: name,
		common.PropertyTypeAppIcon.Name:  appIcon,
	})

	var updatedViaCompose bool
	if projectName := inspect.Config.Labels["com.docker.compose.project"]; projectName != "" {
		composeApps, err := MyService.Compose().List(ctx)
		if err == nil {
			if composeApp, ok := composeApps[projectName]; ok && composeApp != nil {
				if storeInfo, _ := composeApp.StoreInfo(true); storeInfo != nil && storeInfo.StoreAppID != nil && *storeInfo.StoreAppID != "" {
					if updateErr := composeApp.Update(ctx); updateErr == nil {
						updatedViaCompose = true
					} else {
						logger.Info("compose app update failed, falling back to recreate container", zap.Error(updateErr), zap.String("name", projectName))
					}
				}
			}
		}
	}

	updated = updatedViaCompose
	if !updatedViaCompose {
		// pull=true, force=false: recreate only when the pull brought a
		// newer image (the nightly run used to recreate every container).
		if err := MyService.Docker().RecreateContainer(ctx, inspect.ID, true, false); err != nil {
			go PublishEventWrapper(ctx, common.EventTypeAppApplyChangesError, map[string]string{
				common.PropertyTypeAppName.Name: name,
				common.PropertyTypeMessage.Name: err.Error(),
			})
			return nil, false, err
		}
		updated = common.PropertiesFromContext(ctx)[common.PropertyTypeImageUpdated.Name] == "true"
	}

	go PublishEventWrapper(ctx, common.EventTypeAppApplyChangesEnd, map[string]string{
		common.PropertyTypeAppName.Name: name,
	})

	m.mu.Lock()
	cfg := m.configs[name]
	// Either way the pull succeeded, so what's running is the latest.
	cfg.HasUpdate = false
	cfg.LastCheckedAt = time.Now().Format(time.RFC3339)
	if updated {
		cfg.LastUpdatedAt = cfg.LastCheckedAt
	}
	if cfg.LatestDigest != "" {
		cfg.CurrentDigest = cfg.LatestDigest
	}
	if newInspect, err := cli.ContainerInspect(ctx, name); err == nil {
		cfg.ID = newInspect.ID
		cfg.ImageID = newInspect.Image
		cfg.Image = newInspect.Config.Image
		cfg.State = newInspect.State.Status
	}
	m.configs[name] = cfg
	_ = m.saveLocked()
	m.mu.Unlock()

	return &cfg, updated, nil
}

func (m *ContainerUpdateManager) RunAutoUpdates() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	containers, err := m.GetAllContainersWithUpdates(ctx)
	if err != nil {
		logger.Error("failed to list containers for auto-update", zap.Error(err))
		return
	}

	for _, c := range containers {
		if !m.global.Enabled && !c.AutoUpdateEnabled {
			continue
		}
		// Stopped containers are left alone: the owner turned them off, and
		// an app-store app updates through compose, which would start it.
		if c.State != "running" {
			logger.Info("auto-update skipped: container is not running", zap.String("name", c.Name), zap.String("state", c.State))
			continue
		}
		if c.Source != nil && c.Source.Kind != "registry" {
			// Rebuild only a git checkout that is behind upstream; a plain
			// local folder has nothing new to build.
			if c.Source.Kind != "git" {
				continue
			}
			if n, err := gitBehind(ctx, *c.Source); err != nil || n == 0 {
				continue
			}
			job, err := m.StartUpdate(c.Name)
			if err == nil {
				logger.Info("auto-update: rebuilding from new commits", zap.String("name", c.Name), zap.String("job", job.State))
			}
			continue
		}
		_, updated, err := m.UpdateAndRecreateContainer(ctx, c.ID)
		switch {
		case err != nil:
			logger.Error("auto-update failed", zap.String("name", c.Name), zap.Error(err))
		case updated:
			logger.Info("auto-update: container recreated with a newer image", zap.String("name", c.Name), zap.String("image", c.Image))
		default:
			logger.Info("auto-update: already up to date", zap.String("name", c.Name))
		}
	}
}

func DemuxDockerLogs(raw []byte) string {
	if len(raw) < 8 {
		return string(raw)
	}
	if (raw[0] == 1 || raw[0] == 2) && raw[1] == 0 && raw[2] == 0 && raw[3] == 0 {
		var sb strings.Builder
		idx := 0
		for idx+8 <= len(raw) {
			size := int(raw[idx+4])<<24 | int(raw[idx+5])<<16 | int(raw[idx+6])<<8 | int(raw[idx+7])
			idx += 8
			if idx+size > len(raw) {
				sb.Write(raw[idx:])
				break
			}
			sb.Write(raw[idx : idx+size])
			idx += size
		}
		return sb.String()
	}
	return string(raw)
}

func GetFormattedContainerLogs(ctx context.Context, nameOrID string, tail string, timestamps bool) (string, error) {
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return "", err
	}
	defer cli.Close()

	if tail == "" {
		tail = "500"
	}

	body, err := cli.ContainerLogs(ctx, nameOrID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: timestamps,
		Tail:       tail,
	})
	if err != nil {
		return "", err
	}
	defer body.Close()

	raw, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}

	return DemuxDockerLogs(raw), nil
}
