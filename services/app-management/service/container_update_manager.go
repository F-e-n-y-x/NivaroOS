package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	client2 "github.com/docker/docker/client"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

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

	if m.global.Enabled && m.global.Schedule != "" {
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
	m.mu.Lock()
	m.global = cfg
	err := m.saveLocked()
	m.mu.Unlock()

	m.reschedule()
	return err
}

func (m *ContainerUpdateManager) SetContainerAutoUpdate(nameOrID string, enabled bool, schedule string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.configs[nameOrID]
	if !ok {
		cfg = ContainerUpdateInfo{ID: nameOrID}
	}
	cfg.AutoUpdateEnabled = enabled
	if schedule != "" {
		cfg.AutoUpdateSchedule = schedule
	}
	m.configs[nameOrID] = cfg
	return m.saveLocked()
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

	result := make([]ContainerUpdateInfo, 0, len(containers))
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")
		cfg := m.configs[name]
		if cfg.ID == "" {
			cfg = m.configs[c.ID]
		}

		info := ContainerUpdateInfo{
			ID:                 c.ID,
			Name:               name,
			Image:              c.Image,
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

	inspect, err := cli.ContainerInspect(ctx, nameOrID)
	if err != nil {
		return nil, err
	}

	imageName := inspect.Config.Image
	if imageName == "" {
		return nil, fmt.Errorf("container has no image")
	}

	isUpdated, pullErr := MyService.Docker().PullLatestImage(ctx, imageName)

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

	if pullErr == nil {
		cfg.HasUpdate = isUpdated
	}

	m.configs[name] = cfg
	_ = m.saveLocked()

	return &cfg, pullErr
}

func (m *ContainerUpdateManager) UpdateAndRecreateContainer(ctx context.Context, nameOrID string) error {
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	inspect, err := cli.ContainerInspect(ctx, nameOrID)
	if err != nil {
		return err
	}

	if projectName := inspect.Config.Labels["com.docker.compose.project"]; projectName != "" {
		composeApps, err := MyService.Compose().List(ctx)
		if err == nil {
			if composeApp, ok := composeApps[projectName]; ok && composeApp != nil {
				return composeApp.Update(ctx)
			}
		}
	}

	err = MyService.Docker().RecreateContainer(ctx, inspect.ID, true, true)
	if err == nil {
		m.mu.Lock()
		name := strings.TrimPrefix(inspect.Name, "/")
		cfg := m.configs[name]
		cfg.HasUpdate = false
		cfg.LastUpdatedAt = time.Now().Format(time.RFC3339)
		m.configs[name] = cfg
		_ = m.saveLocked()
		m.mu.Unlock()
	}
	return err
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
		if m.global.Enabled || c.AutoUpdateEnabled {
			logger.Info("auto-updating container", zap.String("name", c.Name), zap.String("image", c.Image))
			if err := m.UpdateAndRecreateContainer(ctx, c.ID); err != nil {
				logger.Error("failed to auto-update container", zap.String("name", c.Name), zap.Error(err))
			} else {
				logger.Info("container auto-updated successfully", zap.String("name", c.Name))
			}
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
