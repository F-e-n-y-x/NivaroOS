package v1

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/common_err"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type CompanionDevice struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Model             string                 `json:"model"`
	Platform          string                 `json:"platform"` // android, ios, macos, windows, linux, web
	OSVersion         string                 `json:"os_version"`
	AppVersion        string                 `json:"app_version"`
	IP                string                 `json:"ip"`
	Port              int                    `json:"port,omitempty"`
	SharesStorage     bool                   `json:"shares_storage"`
	RootPath          string                 `json:"root_path,omitempty"`
	StorageUsed       int64                  `json:"storage_used"`
	StorageTotal      int64                  `json:"storage_total"`
	BatteryLevel      int                    `json:"battery_level"`
	IsOnline          bool                   `json:"is_online"`
	LastSeen          time.Time              `json:"last_seen"`
	CreatedAt         time.Time              `json:"created_at"`
	StoragePath       string                 `json:"storage_path"`
	ServerStorageUsed int64                  `json:"server_storage_used"` // actual size of backed-up files on server
	CustomProps       map[string]interface{} `json:"custom_props,omitempty"`
}

type CompanionRegistrationDTO struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Model         string                 `json:"model"`
	Platform      string                 `json:"platform"`
	OSVersion     string                 `json:"os_version"`
	AppVersion    string                 `json:"app_version"`
	IP            string                 `json:"ip"`
	Port          int                    `json:"port,omitempty"`
	SharesStorage bool                   `json:"shares_storage,omitempty"`
	RootPath      string                 `json:"root_path,omitempty"`
	StorageUsed   int64                  `json:"storage_used"`
	StorageTotal  int64                  `json:"storage_total"`
	BatteryLevel  int                    `json:"battery_level"`
	IsOnline      bool                   `json:"is_online"`
	LastSeen      interface{}            `json:"last_seen,omitempty"`
	CreatedAt     interface{}            `json:"created_at,omitempty"`
	StoragePath   string                 `json:"storage_path,omitempty"`
	CustomProps   map[string]interface{} `json:"custom_props,omitempty"`
}

type CompanionFileItem struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

var (
	companionMu          sync.RWMutex
	companionDevices     = make(map[string]*CompanionDevice)
	companionLoaded      = false
	companionWSMu        sync.RWMutex
	companionWSConns     = make(map[string]*websocket.Conn)
	companionPendingMu   sync.Mutex
	companionPendingReqs = make(map[string]chan map[string]interface{})
	wsUpgrader           = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

func getCompanionStorageBasePath() string {
	base := "/DATA/Companion"
	if _, err := os.Stat("/DATA"); os.IsNotExist(err) {
		base = "/var/lib/nivaroos/companion"
	}
	os.MkdirAll(base, 0755)
	return base
}

func getCompanionConfigPath() string {
	path := "/var/lib/nivaroos/companion_devices.json"
	os.MkdirAll("/var/lib/nivaroos", 0755)
	return path
}

func loadCompanionDevicesLocked() {
	if companionLoaded {
		return
	}
	companionLoaded = true
	filePath := getCompanionConfigPath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	var list []*CompanionDevice
	if err := json.Unmarshal(data, &list); err == nil {
		for _, dev := range list {
			if dev.Port <= 0 {
				dev.Port = 8765
			}
			if dev.RootPath == "" {
				dev.RootPath = "/storage/emulated/0"
			}
			dev.SharesStorage = true
			companionDevices[dev.ID] = dev
		}
	}
}

func deduplicateCompanionDevicesLocked() {
	toDelete := make([]string, 0)
	seenIP := make(map[string]*CompanionDevice)
	seenModelName := make(map[string]*CompanionDevice)

	for id, dev := range companionDevices {
		ip := strings.TrimSpace(dev.IP)
		isRealIP := ip != "" && ip != "Local Device" && ip != "Local" && !strings.HasPrefix(ip, "127.")
		modelNameKey := strings.ToLower(strings.TrimSpace(dev.Model + "_" + dev.Name))

		if isRealIP {
			if existing, found := seenIP[ip]; found {
				if dev.LastSeen.After(existing.LastSeen) {
					toDelete = append(toDelete, existing.ID)
					seenIP[ip] = dev
					if exKey := strings.ToLower(strings.TrimSpace(existing.Model + "_" + existing.Name)); exKey != "" && exKey != "_" {
						seenModelName[exKey] = dev
					}
				} else {
					toDelete = append(toDelete, id)
					continue
				}
			} else {
				seenIP[ip] = dev
			}
		}

		if modelNameKey != "_" && modelNameKey != "" {
			if existing, found := seenModelName[modelNameKey]; found && existing.ID != dev.ID {
				if dev.LastSeen.After(existing.LastSeen) {
					toDelete = append(toDelete, existing.ID)
					seenModelName[modelNameKey] = dev
				} else {
					toDelete = append(toDelete, id)
					continue
				}
			} else {
				seenModelName[modelNameKey] = dev
			}
		}
	}

	for _, delID := range toDelete {
		delete(companionDevices, delID)
		companionWSMu.Lock()
		if ws, ok := companionWSConns[delID]; ok {
			ws.Close()
			delete(companionWSConns, delID)
		}
		companionWSMu.Unlock()
	}
}

func saveCompanionDevicesLocked() error {
	deduplicateCompanionDevicesLocked()
	filePath := getCompanionConfigPath()
	list := make([]*CompanionDevice, 0, len(companionDevices))
	for _, dev := range companionDevices {
		list = append(list, dev)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func sanitizeFilename(name string) string {
	res := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == ' ' || r == '.' {
			return r
		}
		return '_'
	}, name)
	return strings.TrimSpace(res)
}

// folderSize returns the total size in bytes of all files under dir
func folderSize(dir string) int64 {
	var total int64
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// GetCompanionDeviceByStoragePath maps a filesystem path to the owning CompanionDevice and target phone path
func GetCompanionDeviceByStoragePath(p string) (*CompanionDevice, string) {
	companionMu.RLock()
	defer companionMu.RUnlock()
	loadCompanionDevicesLocked()

	cleanP := filepath.Clean(p)
	for _, dev := range companionDevices {
		if dev.StoragePath == "" {
			continue
		}
		cleanDevPath := filepath.Clean(dev.StoragePath)
		root := dev.RootPath
		if root == "" {
			root = "/storage/emulated/0"
		}

		if cleanP == cleanDevPath {
			return dev, root
		}
		if strings.HasPrefix(cleanP, cleanDevPath+"/") {
			rel := strings.TrimPrefix(cleanP, cleanDevPath)
			// Map relative path onto device root
			target := filepath.Join(root, rel)
			return dev, target
		}
	}
	return nil, ""
}

// FetchCompanionFilesFromDevice fetches live file listing from companion device via direct HTTP or WebSocket
func FetchCompanionFilesFromDevice(dev *CompanionDevice, phonePath string) ([]CompanionFileItem, error) {
	if phonePath == "" {
		phonePath = "/storage/emulated/0"
	}

	// 1. Try direct HTTP if device has a reachable LAN IP
	devIP := dev.IP
	if devIP != "" && devIP != "Local Device" && devIP != "Local" && !strings.HasPrefix(devIP, "127.") {
		port := dev.Port
		if port <= 0 {
			port = 8765
		}
		urlStr := fmt.Sprintf("http://%s:%d/files?path=%s", devIP, port, url.QueryEscape(phonePath))
		req, err := http.NewRequest("GET", urlStr, nil)
		if err == nil {
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var body struct {
					Success bool                `json:"success"`
					Files   []CompanionFileItem `json:"files"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Success {
					dev.LastSeen = time.Now()
					dev.IsOnline = true
					return body.Files, nil
				}
			}
		}
	}

	// 2. Try WebSocket reverse tunnel
	companionWSMu.RLock()
	ws, hasWS := companionWSConns[dev.ID]
	companionWSMu.RUnlock()

	if hasWS && ws != nil {
		reqID := fmt.Sprintf("req_%d", time.Now().UnixNano())
		ch := make(chan map[string]interface{}, 1)

		companionPendingMu.Lock()
		companionPendingReqs[reqID] = ch
		companionPendingMu.Unlock()

		defer func() {
			companionPendingMu.Lock()
			delete(companionPendingReqs, reqID)
			companionPendingMu.Unlock()
		}()

		msg := map[string]interface{}{
			"id":     reqID,
			"action": "list",
			"path":   phonePath,
		}
		if err := ws.WriteJSON(msg); err == nil {
			select {
			case res := <-ch:
				if filesRaw, ok := res["files"].([]interface{}); ok {
					var items []CompanionFileItem
					data, _ := json.Marshal(filesRaw)
					json.Unmarshal(data, &items)
					dev.LastSeen = time.Now()
					dev.IsOnline = true
					return items, nil
				}
			case <-time.After(5 * time.Second):
				logger.Info("WS list request timed out", zap.String("dev_id", dev.ID))
			}
		}
	}

	return nil, fmt.Errorf("device %s (%s) is unreachable", dev.Name, dev.IP)
}

// ProxyCompanionFileDownload streams a file from the companion device to the Echo HTTP response
func ProxyCompanionFileDownload(dev *CompanionDevice, phonePath string, ctx echo.Context) error {
	devIP := dev.IP
	if devIP == "" || devIP == "Local Device" || devIP == "Local" || strings.HasPrefix(devIP, "127.") {
		return fmt.Errorf("companion device has no direct LAN IP")
	}

	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	urlStr := fmt.Sprintf("http://%s:%d/download?path=%s", devIP, port, url.QueryEscape(phonePath))
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("device returned status %d", resp.StatusCode)
	}

	dev.LastSeen = time.Now()
	dev.IsOnline = true

	fileName := filepath.Base(phonePath)
	for k, v := range resp.Header {
		if len(v) > 0 {
			ctx.Response().Header().Set(k, v[0])
		}
	}
	if ctx.Response().Header().Get("Content-Disposition") == "" {
		ctx.Response().Header().Set("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(fileName))
	}

	_, err = io.Copy(ctx.Response().Writer, resp.Body)
	return err
}

// ProxyCompanionFileDelete sends a delete request to the companion device
func ProxyCompanionFileDelete(dev *CompanionDevice, phonePath string) error {
	devIP := dev.IP
	if devIP == "" || devIP == "Local Device" || devIP == "Local" || strings.HasPrefix(devIP, "127.") {
		return nil
	}

	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	urlStr := fmt.Sprintf("http://%s:%d/delete?path=%s", devIP, port, url.QueryEscape(phonePath))
	req, err := http.NewRequest("DELETE", urlStr, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	return nil
}

func probeCompanionOnline(dev *CompanionDevice) {
	// 1. Check WebSocket connection
	companionWSMu.RLock()
	ws, hasWS := companionWSConns[dev.ID]
	companionWSMu.RUnlock()
	if hasWS && ws != nil {
		dev.LastSeen = time.Now()
		dev.IsOnline = true
		return
	}

	// 2. If seen recently (under 3 minutes), consider online
	if time.Since(dev.LastSeen) <= 3*time.Minute {
		dev.IsOnline = true
		return
	}

	// 3. Proactive LAN check on port 8765
	devIP := dev.IP
	if devIP != "" && devIP != "Local Device" && devIP != "Local" && !strings.HasPrefix(devIP, "127.") {
		port := dev.Port
		if port <= 0 {
			port = 8765
		}
		client := &http.Client{Timeout: 800 * time.Millisecond}
		resp, err := client.Get(fmt.Sprintf("http://%s:%d/status", devIP, port))
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			dev.LastSeen = time.Now()
			dev.IsOnline = true
			return
		}
	}

	dev.IsOnline = false
}

// GET /v1/companion/devices
func GetCompanionDevices(ctx echo.Context) error {
	companionMu.Lock()
	defer companionMu.Unlock()
	loadCompanionDevicesLocked()
	deduplicateCompanionDevicesLocked()

	list := make([]*CompanionDevice, 0, len(companionDevices))

	var wg sync.WaitGroup
	for _, dev := range companionDevices {
		if dev.Port <= 0 {
			dev.Port = 8765
		}
		if dev.RootPath == "" {
			dev.RootPath = "/storage/emulated/0"
		}
		dev.SharesStorage = true

		if dev.StoragePath != "" {
			dev.ServerStorageUsed = folderSize(dev.StoragePath)
		}

		wg.Add(1)
		go func(d *CompanionDevice) {
			defer wg.Done()
			probeCompanionOnline(d)
		}(dev)

		list = append(list, dev)
	}
	wg.Wait()

	saveCompanionDevicesLocked()

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "success",
		Data:    list,
	})
}

// POST /v1/companion/register
func PostRegisterCompanionDevice(ctx echo.Context) error {
	var input CompanionRegistrationDTO
	if err := ctx.Bind(&input); err != nil {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "invalid device registration payload: " + err.Error(),
		})
	}

	realIP := ctx.RealIP()
	if input.ID == "" {
		input.ID = "dev_" + sanitizeFilename(realIP)
	}

	companionMu.Lock()
	defer companionMu.Unlock()
	loadCompanionDevicesLocked()

	if input.Name == "" {
		input.Name = input.Model
		if input.Name == "" {
			input.Name = "Companion Device"
		}
	}

	sanitized := sanitizeFilename(input.Name)
	if sanitized == "" {
		sanitized = input.ID
	}
	devStoragePath := filepath.Join(getCompanionStorageBasePath(), sanitized)
	os.MkdirAll(devStoragePath, 0755)

	resolvedIP := input.IP
	if resolvedIP == "" || resolvedIP == "Local Device" || resolvedIP == "Local" || strings.HasPrefix(resolvedIP, "127.") {
		resolvedIP = realIP
	}

	resolvedPort := input.Port
	if resolvedPort <= 0 {
		resolvedPort = 8765
	}

	rootPath := input.RootPath
	if rootPath == "" {
		rootPath = "/storage/emulated/0"
	}

	var matchedDev *CompanionDevice
	var oldKey string
	now := time.Now()

	if existing, exists := companionDevices[input.ID]; exists {
		matchedDev = existing
	} else {
		// Look for existing device with matching IP or matching Model + Name
		isRealIP := resolvedIP != "" && resolvedIP != "Local Device" && resolvedIP != "Local" && !strings.HasPrefix(resolvedIP, "127.")
		for id, dev := range companionDevices {
			if isRealIP && strings.TrimSpace(dev.IP) == strings.TrimSpace(resolvedIP) {
				matchedDev = dev
				oldKey = id
				break
			}
			if input.Model != "" && input.Name != "" &&
				strings.EqualFold(strings.TrimSpace(dev.Model), strings.TrimSpace(input.Model)) &&
				strings.EqualFold(strings.TrimSpace(dev.Name), strings.TrimSpace(input.Name)) {
				matchedDev = dev
				oldKey = id
				break
			}
		}
	}

	if matchedDev != nil {
		if oldKey != "" && oldKey != input.ID {
			delete(companionDevices, oldKey)
			matchedDev.ID = input.ID
			companionDevices[input.ID] = matchedDev
		}
		if input.Name != "" {
			matchedDev.Name = input.Name
		}
		if input.Model != "" {
			matchedDev.Model = input.Model
		}
		if input.Platform != "" {
			matchedDev.Platform = input.Platform
		}
		if input.OSVersion != "" {
			matchedDev.OSVersion = input.OSVersion
		}
		if input.AppVersion != "" {
			matchedDev.AppVersion = input.AppVersion
		}
		if resolvedIP != "" {
			matchedDev.IP = resolvedIP
		}
		matchedDev.Port = resolvedPort
		matchedDev.SharesStorage = true
		matchedDev.RootPath = rootPath
		if input.StorageUsed > 0 {
			matchedDev.StorageUsed = input.StorageUsed
		}
		if input.StorageTotal > 0 {
			matchedDev.StorageTotal = input.StorageTotal
		}
		if input.BatteryLevel > 0 {
			matchedDev.BatteryLevel = input.BatteryLevel
		}
		matchedDev.IsOnline = true
		matchedDev.LastSeen = now
		matchedDev.StoragePath = devStoragePath
		if input.CustomProps != nil {
			matchedDev.CustomProps = input.CustomProps
		}
	} else {
		newDev := &CompanionDevice{
			ID:            input.ID,
			Name:          input.Name,
			Model:         input.Model,
			Platform:      input.Platform,
			OSVersion:     input.OSVersion,
			AppVersion:    input.AppVersion,
			IP:            resolvedIP,
			Port:          resolvedPort,
			SharesStorage: true,
			RootPath:      rootPath,
			StorageUsed:   input.StorageUsed,
			StorageTotal:  input.StorageTotal,
			BatteryLevel:  input.BatteryLevel,
			IsOnline:      true,
			LastSeen:      now,
			CreatedAt:     now,
			StoragePath:   devStoragePath,
			CustomProps:   input.CustomProps,
		}
		companionDevices[input.ID] = newDev
	}

	saveCompanionDevicesLocked()

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "companion device registered",
		Data:    companionDevices[input.ID],
	})
}

// PUT /v1/companion/devices/:id
func PutUpdateCompanionDevice(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "device id required",
		})
	}

	var update CompanionRegistrationDTO
	if err := ctx.Bind(&update); err != nil {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "invalid update payload",
		})
	}

	companionMu.Lock()
	defer companionMu.Unlock()
	loadCompanionDevicesLocked()

	dev, exists := companionDevices[id]
	if !exists {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: "companion device not found",
		})
	}

	// Rename storage path if name changed
	if update.Name != "" && update.Name != dev.Name {
		oldPath := dev.StoragePath
		newSanitized := sanitizeFilename(update.Name)
		newPath := filepath.Join(getCompanionStorageBasePath(), newSanitized)
		if oldPath != "" && oldPath != newPath {
			os.Rename(oldPath, newPath)
		}
		os.MkdirAll(newPath, 0755)
		dev.Name = update.Name
		dev.StoragePath = newPath
	}

	if update.StorageTotal > 0 {
		dev.StorageTotal = update.StorageTotal
	}
	if update.StorageUsed > 0 {
		dev.StorageUsed = update.StorageUsed
	}
	if update.BatteryLevel > 0 {
		dev.BatteryLevel = update.BatteryLevel
	}
	if update.CustomProps != nil {
		dev.CustomProps = update.CustomProps
	}

	saveCompanionDevicesLocked()

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "companion device updated",
		Data:    dev,
	})
}

// DELETE /v1/companion/devices/:id
func DeleteCompanionDevice(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "device id required",
		})
	}

	companionMu.Lock()
	defer companionMu.Unlock()
	loadCompanionDevicesLocked()

	if _, exists := companionDevices[id]; !exists {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: "companion device not found",
		})
	}

	delete(companionDevices, id)
	saveCompanionDevicesLocked()

	// Close any active WS connection
	companionWSMu.Lock()
	if ws, ok := companionWSConns[id]; ok {
		ws.Close()
		delete(companionWSConns, id)
	}
	companionWSMu.Unlock()

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "companion device removed",
	})
}

// GET /v1/companion/devices/:id/storage
func GetCompanionDeviceStorage(ctx echo.Context) error {
	id := ctx.Param("id")
	companionMu.RLock()
	dev, exists := companionDevices[id]
	companionMu.RUnlock()

	if !exists {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: "device not found",
		})
	}

	probeCompanionOnline(dev)

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "success",
		Data: echo.Map{
			"id":            dev.ID,
			"name":          dev.Name,
			"storage_path":  dev.StoragePath,
			"storage_total": dev.StorageTotal,
			"storage_used":  dev.StorageUsed,
			"storage_free":  dev.StorageTotal - dev.StorageUsed,
			"is_online":     dev.IsOnline,
		},
	})
}

// GET /v1/companion/devices/:id/files
func GetCompanionDeviceFiles(ctx echo.Context) error {
	id := ctx.Param("id")
	subPath := ctx.QueryParam("path")

	companionMu.RLock()
	dev, exists := companionDevices[id]
	companionMu.RUnlock()

	if !exists {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: "companion device not found",
		})
	}

	phonePath := subPath
	if phonePath == "" || phonePath == "/" {
		phonePath = dev.RootPath
		if phonePath == "" {
			phonePath = "/storage/emulated/0"
		}
	}

	items, err := FetchCompanionFilesFromDevice(dev, phonePath)
	if err == nil {
		return ctx.JSON(http.StatusOK, model.Result{
			Success: common_err.SUCCESS,
			Message: "success",
			Data: echo.Map{
				"device": dev,
				"path":   phonePath,
				"files":  items,
			},
		})
	}

	// Fallback to reading server local directory
	targetDir := dev.StoragePath
	if targetDir == "" {
		targetDir = filepath.Join(getCompanionStorageBasePath(), sanitizeFilename(dev.Name))
	}
	os.MkdirAll(targetDir, 0755)

	if subPath != "" && subPath != "/" && !strings.HasPrefix(subPath, "/storage/") {
		cleaned := filepath.Clean(subPath)
		if strings.HasPrefix(cleaned, "/") {
			cleaned = cleaned[1:]
		}
		targetDir = filepath.Join(targetDir, cleaned)
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return ctx.JSON(http.StatusOK, model.Result{
			Success: common_err.SUCCESS,
			Message: "empty or inaccessible directory",
			Data:    []CompanionFileItem{},
		})
	}

	items = make([]CompanionFileItem, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		fullPath := filepath.Join(targetDir, entry.Name())
		items = append(items, CompanionFileItem{
			Name:     entry.Name(),
			Path:     fullPath,
			IsDir:    entry.IsDir(),
			Size:     info.Size(),
			Modified: info.ModTime(),
		})
	}

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "success",
		Data: echo.Map{
			"device": dev,
			"path":   targetDir,
			"files":  items,
		},
	})
}

// GET /v1/companion/devices/:id/file
func GetCompanionDeviceDownload(ctx echo.Context) error {
	id := ctx.Param("id")
	filePath := ctx.QueryParam("path")

	companionMu.RLock()
	dev, exists := companionDevices[id]
	companionMu.RUnlock()

	if !exists {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: "companion device not found",
		})
	}

	if filePath == "" {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "path parameter is required",
		})
	}

	isOnline := time.Since(dev.LastSeen) <= 3*time.Minute
	if isOnline && (strings.HasPrefix(filePath, "/storage/") || strings.HasPrefix(filePath, "/sdcard")) {
		if err := ProxyCompanionFileDownload(dev, filePath, ctx); err == nil {
			return nil
		}
	}

	// Fallback to server local companion storage
	cleanPath := filepath.Clean(filePath)
	base := getCompanionStorageBasePath()
	if !strings.HasPrefix(cleanPath, dev.StoragePath) && !strings.HasPrefix(cleanPath, base) {
		return ctx.JSON(http.StatusForbidden, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "access outside companion device storage is denied",
		})
	}

	return ctx.Attachment(cleanPath, filepath.Base(cleanPath))
}

// POST /v1/companion/devices/:id/upload
func PostCompanionDeviceUpload(ctx echo.Context) error {
	id := ctx.Param("id")
	destSubPath := ctx.QueryParam("path")

	companionMu.RLock()
	dev, exists := companionDevices[id]
	companionMu.RUnlock()

	if !exists {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: "companion device not found",
		})
	}

	targetDir := dev.StoragePath
	if targetDir == "" {
		targetDir = filepath.Join(getCompanionStorageBasePath(), sanitizeFilename(dev.Name))
	}
	os.MkdirAll(targetDir, 0755)

	if destSubPath != "" && destSubPath != "/" {
		cleaned := filepath.Clean(destSubPath)
		if strings.HasPrefix(cleaned, "/") {
			cleaned = cleaned[1:]
		}
		targetDir = filepath.Join(targetDir, cleaned)
		os.MkdirAll(targetDir, 0755)
	}

	file, err := ctx.FormFile("file")
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "missing upload file form data: " + err.Error(),
		})
	}

	src, err := file.Open()
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: err.Error(),
		})
	}
	defer src.Close()

	destPath := filepath.Join(targetDir, file.Filename)
	dst, err := os.Create(destPath)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: "failed to create destination file: " + err.Error(),
		})
	}
	defer dst.Close()

	if _, err = io.Copy(dst, src); err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: "failed to write file content: " + err.Error(),
		})
	}

	logger.Info("Uploaded file to companion device storage", zap.String("device_id", id), zap.String("file", destPath))

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "file uploaded successfully to companion storage",
		Data: echo.Map{
			"path": destPath,
			"name": file.Filename,
			"size": file.Size,
		},
	})
}

// GET /v1/companion/devices/:id/ws
func GetCompanionDeviceWS(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return ctx.JSON(http.StatusBadRequest, echo.Map{"error": "id required"})
	}

	ws, err := wsUpgrader.Upgrade(ctx.Response(), ctx.Request(), nil)
	if err != nil {
		logger.Error("WS upgrade failed", zap.Error(err))
		return err
	}
	defer ws.Close()

	companionWSMu.Lock()
	companionWSConns[id] = ws
	companionWSMu.Unlock()

	defer func() {
		companionWSMu.Lock()
		delete(companionWSConns, id)
		companionWSMu.Unlock()
	}()

	companionMu.Lock()
	loadCompanionDevicesLocked()
	if dev, ok := companionDevices[id]; ok {
		dev.IsOnline = true
		dev.LastSeen = time.Now()
		realIP := ctx.RealIP()
		if realIP != "" && !strings.HasPrefix(realIP, "127.") {
			dev.IP = realIP
		}
	}
	companionMu.Unlock()

	logger.Info("Companion WebSocket tunnel connected", zap.String("id", id))

	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			break
		}
		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err == nil {
			action, _ := msg["action"].(string)
			reqID, _ := msg["id"].(string)

			if action == "register" {
				companionMu.Lock()
				if dev, ok := companionDevices[id]; ok {
					if port, ok := msg["port"].(float64); ok && port > 0 {
						dev.Port = int(port)
					}
					if ip, ok := msg["ip"].(string); ok && ip != "" && ip != "Local Device" {
						dev.IP = ip
					}
					dev.SharesStorage = true
					dev.IsOnline = true
					dev.LastSeen = time.Now()
				}
				companionMu.Unlock()
			} else if reqID != "" {
				companionPendingMu.Lock()
				if ch, ok := companionPendingReqs[reqID]; ok {
					select {
					case ch <- msg:
					default:
					}
				}
				companionPendingMu.Unlock()
			}
		}
	}

	return nil
}
