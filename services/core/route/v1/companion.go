package v1

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
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
	// Secret authenticates every direct server->phone HTTP call this device's
	// embedded CompanionFileServer receives (download/upload/delete/files) -
	// that server has no other way to verify a LAN caller. Generated once at
	// registration (over the already-JWT-authenticated /companion/register
	// call) and handed back to the phone in that one response only -
	// json:"-" keeps it out of every other response (GetCompanionDevices,
	// GetCompanionDeviceStorage, etc.) that any authenticated NivaroOS user
	// browsing the sidebar can see - and, as a side effect, out of the
	// companion_devices.json persistence file too (saveCompanionDevicesLocked
	// marshals this same struct), so a core service restart clears every
	// device's secret. That's fine: the phone re-registers every 30s
	// (device_sync_service.dart's heartbeat) and gets a freshly generated one
	// then - direct phone calls just fail for up to that long after a
	// restart, not silently or insecurely.
	Secret string `json:"-"`
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

func getCompanionSecretsPath() string {
	path := "/var/lib/nivaroos/companion_secrets.json"
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
	// Restore credentials securely persisted to companion_secrets.json
	secData, secErr := os.ReadFile(getCompanionSecretsPath())
	if secErr == nil {
		var secrets map[string]string
		if err := json.Unmarshal(secData, &secrets); err == nil {
			for id, sec := range secrets {
				if dev, ok := companionDevices[id]; ok && sec != "" {
					dev.Secret = sec
				}
			}
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
	secrets := make(map[string]string)
	for _, dev := range companionDevices {
		list = append(list, dev)
		if dev.Secret != "" {
			secrets[dev.ID] = dev.Secret
		}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if len(secrets) > 0 {
		if secData, err := json.MarshalIndent(secrets, "", "  "); err == nil {
			_ = os.WriteFile(getCompanionSecretsPath(), secData, 0600)
		}
	}
	return os.WriteFile(filePath, data, 0644)
}

// generateCompanionSecret returns a random 32-byte hex string for a new
// device's Secret field. crypto/rand, not math/rand - this is a credential,
// not a display ID (unlike CompanionDevice.ID, which does appear in URLs
// and logs and must stay separate from this).
func generateCompanionSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// relocateDeviceFolder gives dev a backup folder named after newName and
// moves its existing backups there. A folder another device uses (or any
// existing folder) is never taken - " (2)", " (3)"... is appended. If the
// move fails, dev keeps its old folder and the error is returned (it used
// to be ignored, leaving the device on a new empty folder).
func relocateDeviceFolder(base string, devs map[string]*CompanionDevice, dev *CompanionDevice, newName string) error {
	stem := sanitizeFilename(newName)
	if stem == "" {
		stem = dev.ID
	}
	oldPath := filepath.Clean(dev.StoragePath)
	taken := func(p string) bool {
		if dev.StoragePath != "" && p == oldPath {
			return false
		}
		for id, other := range devs {
			if id != dev.ID && other.StoragePath != "" && filepath.Clean(other.StoragePath) == p {
				return true
			}
		}
		_, err := os.Lstat(p)
		return err == nil
	}
	newPath := filepath.Join(base, stem)
	for i := 2; taken(newPath); i++ {
		newPath = filepath.Join(base, fmt.Sprintf("%s (%d)", stem, i))
	}
	if dev.StoragePath != "" && newPath == oldPath {
		return nil
	}
	if dev.StoragePath != "" {
		if _, err := os.Stat(oldPath); err == nil {
			if err := os.Rename(oldPath, newPath); err != nil {
				return fmt.Errorf("moving the backups to %s: %w", filepath.Base(newPath), err)
			}
		}
	}
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		return err
	}
	dev.StoragePath = newPath
	return nil
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
			req.Header.Set("X-Companion-Secret", dev.Secret)
			client := &http.Client{Timeout: 15 * time.Second} // a large folder (DCIM) can take seconds to list on the phone
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
			case <-time.After(15 * time.Second):
				logger.Info("WS list request timed out", zap.String("dev_id", dev.ID))
			}
		}
	}

	return nil, fmt.Errorf("device %s (%s) is unreachable", dev.Name, dev.IP)
}

// ProxyCompanionStream streams a file from the companion device to http.ResponseWriter with Range and inline preview support
func ProxyCompanionStream(dev *CompanionDevice, phonePath string, w http.ResponseWriter, r *http.Request) error {
	devIP := dev.IP
	if devIP == "" || devIP == "Local Device" || devIP == "Local" || strings.HasPrefix(devIP, "127.") {
		return fmt.Errorf("companion device has no direct LAN IP")
	}

	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	urlStr := fmt.Sprintf("http://%s:%d/download?path=%s", devIP, port, url.QueryEscape(phonePath))
	if r != nil && r.URL.Query().Get("download") == "1" {
		urlStr += "&download=1"
	}

	var req *http.Request
	var err error
	if r != nil {
		req, err = http.NewRequestWithContext(r.Context(), "GET", urlStr, nil)
	} else {
		req, err = http.NewRequest("GET", urlStr, nil)
	}
	if err != nil {
		return err
	}
	req.Header.Set("X-Companion-Secret", dev.Secret)

	// Forward Range header for video/media seeking and partial content
	if r != nil {
		if rangeH := r.Header.Get("Range"); rangeH != "" {
			req.Header.Set("Range", rangeH)
		}
	}

	client := &http.Client{Timeout: 60 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("companion device returned status %d", resp.StatusCode)
	}

	dev.LastSeen = time.Now()
	dev.IsOnline = true

	// Forward critical media/streaming headers
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if val := resp.Header.Get(h); val != "" {
			w.Header().Set(h, val)
		}
	}

	fileName := filepath.Base(phonePath)
	disposition := "inline; filename*=utf-8''" + url.PathEscape(fileName)
	if r != nil && r.URL.Query().Get("download") == "1" {
		disposition = "attachment; filename*=utf-8''" + url.PathEscape(fileName)
	}
	w.Header().Set("Content-Disposition", disposition)

	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	return err
}

// ProxyCompanionFileDownload streams a file from the companion device to the Echo HTTP response
func ProxyCompanionFileDownload(dev *CompanionDevice, phonePath string, ctx echo.Context) error {
	return ProxyCompanionStream(dev, phonePath, ctx.Response().Writer, ctx.Request())
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
	req.Header.Set("X-Companion-Secret", dev.Secret)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	return nil
}

// ProxyCompanionFileRename sends a rename request to the companion device with fallback to streaming
func ProxyCompanionFileRename(dev *CompanionDevice, oldPath, newPath string) error {
	devIP := dev.IP
	if devIP == "" || devIP == "Local Device" || devIP == "Local" || strings.HasPrefix(devIP, "127.") {
		return fmt.Errorf("companion device has no direct LAN IP")
	}
	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	// Try /rename endpoint first
	urlStr := fmt.Sprintf("http://%s:%d/rename?old_path=%s&new_path=%s", devIP, port, url.QueryEscape(oldPath), url.QueryEscape(newPath))
	req, err := http.NewRequest("POST", urlStr, nil)
	if err == nil {
		req.Header.Set("X-Companion-Secret", dev.Secret)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				dev.LastSeen = time.Now()
				dev.IsOnline = true
				return nil
			}
		}
	}

	// Fallback: download old -> upload new -> delete old
	tmpFile, err := os.CreateTemp("", "nivaroos-comp-rename-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	dlUrl := fmt.Sprintf("http://%s:%d/download?path=%s&download=1", devIP, port, url.QueryEscape(oldPath))
	dlReq, err := http.NewRequest("GET", dlUrl, nil)
	if err != nil {
		tmpFile.Close()
		return err
	}
	dlReq.Header.Set("X-Companion-Secret", dev.Secret)
	client := &http.Client{Timeout: 60 * time.Minute}
	dlResp, err := client.Do(dlReq)
	if err != nil {
		tmpFile.Close()
		return err
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("failed to read companion file for rename (status %d)", dlResp.StatusCode)
	}
	if _, err := io.Copy(tmpFile, dlResp.Body); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	ulSrc, err := os.Open(tmpName)
	if err != nil {
		return err
	}
	defer ulSrc.Close()
	ulStat, _ := ulSrc.Stat()

	ulUrl := fmt.Sprintf("http://%s:%d/upload?path=%s", devIP, port, url.QueryEscape(newPath))
	ulReq, err := http.NewRequest("POST", ulUrl, ulSrc)
	if err != nil {
		return err
	}
	ulReq.ContentLength = ulStat.Size()
	ulReq.Header.Set("X-Companion-Secret", dev.Secret)
	ulReq.Header.Set("Content-Type", "application/octet-stream")
	ulResp, err := client.Do(ulReq)
	if err != nil {
		return err
	}
	defer ulResp.Body.Close()
	if ulResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to write companion file for rename (status %d)", ulResp.StatusCode)
	}

	_ = ProxyCompanionFileDelete(dev, oldPath)
	dev.LastSeen = time.Now()
	dev.IsOnline = true
	return nil
}

// ProxyCompanionMkdir creates a directory on the companion device
func ProxyCompanionMkdir(dev *CompanionDevice, phonePath string) error {
	devIP := dev.IP
	if devIP == "" || devIP == "Local Device" || devIP == "Local" || strings.HasPrefix(devIP, "127.") {
		return fmt.Errorf("companion device has no direct LAN IP")
	}
	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	// Try /mkdir endpoint
	urlStr := fmt.Sprintf("http://%s:%d/mkdir?path=%s", devIP, port, url.QueryEscape(phonePath))
	req, err := http.NewRequest("POST", urlStr, nil)
	if err == nil {
		req.Header.Set("X-Companion-Secret", dev.Secret)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				dev.LastSeen = time.Now()
				dev.IsOnline = true
				return nil
			}
		}
	}

	// Fallback: upload placeholder into path, which forces parent directory creation on phone
	dummyPath := filepath.Join(phonePath, ".init")
	ulUrl := fmt.Sprintf("http://%s:%d/upload?path=%s", devIP, port, url.QueryEscape(dummyPath))
	ulReq, err := http.NewRequest("POST", ulUrl, strings.NewReader(""))
	if err != nil {
		return err
	}
	ulReq.Header.Set("X-Companion-Secret", dev.Secret)
	client := &http.Client{Timeout: 5 * time.Second}
	ulResp, err := client.Do(ulReq)
	if err != nil {
		return err
	}
	defer ulResp.Body.Close()
	_ = ProxyCompanionFileDelete(dev, dummyPath)
	dev.LastSeen = time.Now()
	dev.IsOnline = true
	return nil
}

// ProxyCompanionUploadStream streams content to the companion device at phonePath
func ProxyCompanionUploadStream(dev *CompanionDevice, phonePath string, reader io.Reader, size int64) error {
	devIP := dev.IP
	if devIP == "" || devIP == "Local Device" || devIP == "Local" || strings.HasPrefix(devIP, "127.") {
		return fmt.Errorf("companion device has no direct LAN IP")
	}
	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	urlStr := fmt.Sprintf("http://%s:%d/upload?path=%s", devIP, port, url.QueryEscape(phonePath))
	req, err := http.NewRequest("POST", urlStr, reader)
	if err != nil {
		return err
	}
	if size >= 0 {
		req.ContentLength = size
	}
	req.Header.Set("X-Companion-Secret", dev.Secret)
	req.Header.Set("Content-Type", "application/octet-stream")

	client := &http.Client{Timeout: 60 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("companion upload failed with status %d", resp.StatusCode)
	}
	// The phone answers {"success": true} only after the file is fully
	// written on its side.
	var ack struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10)); len(body) > 0 && json.Unmarshal(body, &ack) == nil && ack.Success != nil && !*ack.Success {
		if ack.Error == "" {
			ack.Error = "the device rejected the upload"
		}
		return errors.New(ack.Error)
	}
	dev.LastSeen = time.Now()
	dev.IsOnline = true
	return nil
}

// ProxyCompanionUploadFile uploads a local file to the companion device at phonePath
func ProxyCompanionUploadFile(dev *CompanionDevice, localPath, phonePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	return ProxyCompanionUploadStream(dev, phonePath, f, st.Size())
}

// DownloadCompanionItemToLocal downloads a companion device file or folder into a local filesystem path
func DownloadCompanionItemToLocal(ctx context.Context, companionSrc, localDst string) error {
	h := &companionIOHandlerImpl{}
	return h.CopyFromCompanion(ctx, companionSrc, localDst, "overwrite", nil)
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

	// 2. Proactive LAN check on port 8765
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

	// 3. If seen recently (under 75 seconds - two sync intervals), consider online
	if time.Since(dev.LastSeen) <= 75*time.Second {
		dev.IsOnline = true
		return
	}

	dev.IsOnline = false
}

// IsCompanionFolderVisible returns true if the folder corresponds to an online companion device.
// If the companion device is offline or the folder does not belong to any active companion, returns false.
func IsCompanionFolderVisible(folderPath string) bool {
	companionMu.Lock()
	defer companionMu.Unlock()
	loadCompanionDevicesLocked()

	cleanFolder := filepath.Clean(folderPath)
	base := filepath.Clean(getCompanionStorageBasePath())
	if cleanFolder == base {
		return true
	}

	for _, dev := range companionDevices {
		if dev.StoragePath != "" && filepath.Clean(dev.StoragePath) == cleanFolder {
			probeCompanionOnline(dev)
			return dev.IsOnline
		}
		if dev.Name != "" && filepath.Clean(filepath.Join(base, sanitizeFilename(dev.Name))) == cleanFolder {
			probeCompanionOnline(dev)
			return dev.IsOnline
		}
	}
	return false
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

		inputIsUserRenamed := false
		if input.CustomProps != nil {
			if ur, ok := input.CustomProps["user_renamed"].(bool); ok && ur {
				inputIsUserRenamed = true
			}
		}

		devIsUserRenamed := false
		if matchedDev.CustomProps != nil {
			if ur, ok := matchedDev.CustomProps["user_renamed"].(bool); ok && ur {
				devIsUserRenamed = true
			}
		}

		// Only rename the existing device if:
		// 1) The incoming request was explicitly user_renamed from client, OR
		// 2) The existing device was never user-renamed and incoming has a different non-empty name
		shouldRename := false
		if inputIsUserRenamed && input.Name != "" && input.Name != matchedDev.Name {
			shouldRename = true
		} else if !devIsUserRenamed && input.Name != "" && input.Name != matchedDev.Name {
			shouldRename = true
		}

		if shouldRename {
			if err := relocateDeviceFolder(getCompanionStorageBasePath(), companionDevices, matchedDev, input.Name); err != nil {
				// Keep the old name with its folder rather than split them.
				logger.Error("companion: renaming backup folder failed", zap.String("device", matchedDev.ID), zap.Error(err))
			} else {
				matchedDev.Name = input.Name
			}
			if inputIsUserRenamed {
				if matchedDev.CustomProps == nil {
					matchedDev.CustomProps = make(map[string]interface{})
				}
				matchedDev.CustomProps["user_renamed"] = true
			}
		} else {
			// Device name is preserved! Ensure its existing storage path exists.
			if matchedDev.StoragePath == "" {
				sanitized := sanitizeFilename(matchedDev.Name)
				if sanitized == "" {
					sanitized = matchedDev.ID
				}
				matchedDev.StoragePath = filepath.Join(getCompanionStorageBasePath(), sanitized)
			}
			os.MkdirAll(matchedDev.StoragePath, 0755)
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
		if input.CustomProps != nil {
			if matchedDev.CustomProps == nil {
				matchedDev.CustomProps = make(map[string]interface{})
			}
			for k, v := range input.CustomProps {
				matchedDev.CustomProps[k] = v
			}
		}
	} else {
		newSanitized := sanitizeFilename(input.Name)
		if newSanitized == "" {
			newSanitized = input.ID
		}
		devStoragePath := filepath.Join(getCompanionStorageBasePath(), newSanitized)
		os.MkdirAll(devStoragePath, 0755)

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

	registered := companionDevices[input.ID]
	if registered.Secret == "" {
		if headerSecret := ctx.Request().Header.Get("X-Companion-Secret"); headerSecret != "" {
			registered.Secret = headerSecret
		} else if input.CustomProps != nil {
			if propSecret, ok := input.CustomProps["secret"].(string); ok && propSecret != "" {
				registered.Secret = propSecret
			}
		}
		if registered.Secret == "" {
			secret, err := generateCompanionSecret()
			if err != nil {
				logger.Error("failed to generate companion device secret", zap.Error(err))
			} else {
				registered.Secret = secret
			}
		}
	}

	saveCompanionDevicesLocked()

	// The secret rides alongside (not inside) the device object - Secret's
	// own json:"-" tag means `registered` itself never serializes it, so
	// every OTHER endpoint returning a CompanionDevice (list, storage, etc.)
	// stays safe even though this handler needs to hand it to the phone.
	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "companion device registered",
		Data: map[string]interface{}{
			"device": registered,
			"secret": registered.Secret,
		},
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
		if err := relocateDeviceFolder(getCompanionStorageBasePath(), companionDevices, dev, update.Name); err != nil {
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}
		dev.Name = update.Name
		if dev.CustomProps == nil {
			dev.CustomProps = make(map[string]interface{})
		}
		dev.CustomProps["user_renamed"] = true
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
		if dev.CustomProps == nil {
			dev.CustomProps = make(map[string]interface{})
		}
		for k, v := range update.CustomProps {
			dev.CustomProps[k] = v
		}
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

	dev, exists := companionDevices[id]
	if !exists {
		return ctx.JSON(http.StatusNotFound, model.Result{
			Success: common_err.SERVICE_ERROR,
			Message: "companion device not found",
		})
	}

	// 1. The device's backups stay unless the user explicitly asked to
	// delete them (removing a phone used to silently wipe everything it had
	// backed up - and a second folder guessed from its name, which could
	// belong to another device).
	keptData := ""
	base := filepath.Clean(getCompanionStorageBasePath())
	storage := filepath.Clean(dev.StoragePath)
	inBase := dev.StoragePath != "" && storage != base && strings.HasPrefix(storage, base+"/")
	shared := false
	for otherID, other := range companionDevices {
		if otherID != id && other.StoragePath != "" && filepath.Clean(other.StoragePath) == storage {
			shared = true
		}
	}
	if inBase && !shared && ctx.QueryParam("delete_data") == "true" {
		if err := os.RemoveAll(storage); err != nil {
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: "couldn't delete the backups: " + err.Error()})
		}
	} else if inBase {
		keptData = storage
	}

	// 2. Remove device from map and save
	delete(companionDevices, id)
	saveCompanionDevicesLocked()

	// 3. Close any active WS connection
	companionWSMu.Lock()
	if ws, ok := companionWSConns[id]; ok {
		ws.Close()
		delete(companionWSConns, id)
	}
	companionWSMu.Unlock()

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "companion device removed",
		Data:    map[string]string{"kept_backups": keptData},
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

	// Explicitly labelled: this is the backup copy kept on this server,
	// not what's on the device right now.
	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "device unreachable - showing the backup copy on this server",
		Data: echo.Map{
			"device":  dev,
			"path":    targetDir,
			"files":   items,
			"offline": true,
			"source":  "server_backup",
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

	if strings.HasPrefix(filePath, "/storage/") || strings.HasPrefix(filePath, "/sdcard") {
		// A path on the phone itself: only the phone can serve it.
		var err error = errors.New("device offline")
		if time.Since(dev.LastSeen) <= 3*time.Minute {
			if err = ProxyCompanionFileDownload(dev, filePath, ctx); err == nil {
				return nil
			}
		}
		return companionUnreachable(ctx, dev, err)
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

type companionIOHandlerImpl struct{}

func (h *companionIOHandlerImpl) IsCompanionPath(p string) bool {
	cleanP := filepath.Clean(p)
	base := getCompanionStorageBasePath()
	if cleanP == base || strings.HasPrefix(cleanP, base+"/") {
		return true
	}
	dev, _ := GetCompanionDeviceByStoragePath(cleanP)
	return dev != nil
}

func (h *companionIOHandlerImpl) GetSize(ctx context.Context, p string) (int64, error) {
	dev, phonePath := GetCompanionDeviceByStoragePath(p)
	if dev == nil || dev.IP == "" {
		return 0, fmt.Errorf("companion device not found or offline")
	}
	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	items, err := FetchCompanionFilesFromDevice(dev, phonePath)
	if err == nil && len(items) > 0 {
		var total int64 = 0
		for _, it := range items {
			total += it.Size
		}
		return total, nil
	}
	urlStr := fmt.Sprintf("http://%s:%d/download?path=%s", dev.IP, port, url.QueryEscape(phonePath))
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Companion-Secret", dev.Secret)
	req.Header.Set("Range", "bytes=0-0")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if idx := strings.LastIndex(cr, "/"); idx != -1 {
			if s, err := strconv.ParseInt(cr[idx+1:], 10, 64); err == nil {
				return s, nil
			}
		}
	}
	if resp.ContentLength > 0 {
		return resp.ContentLength, nil
	}
	return 0, nil
}

func isCompanionFile(dev *CompanionDevice, phonePath string) bool {
	devIP := dev.IP
	if devIP == "" || devIP == "Local Device" || devIP == "Local" || strings.HasPrefix(devIP, "127.") {
		return false
	}
	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	urlStr := fmt.Sprintf("http://%s:%d/download?path=%s", devIP, port, url.QueryEscape(phonePath))
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-Companion-Secret", dev.Secret)
	req.Header.Set("Range", "bytes=0-0")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}

func (h *companionIOHandlerImpl) CopyFromCompanion(ctx context.Context, companionSrc, dst, style string, onProgress func(int64)) error {
	dev, phonePath := GetCompanionDeviceByStoragePath(companionSrc)
	if dev == nil {
		return fmt.Errorf("device not found for path: %s", companionSrc)
	}

	// 1. Check if source path is a file on the companion device
	if isCompanionFile(dev, phonePath) {
		targetFile := dst
		dinfo, statErr := os.Stat(dst)
		if (statErr == nil && dinfo.IsDir()) || strings.HasSuffix(dst, "/") {
			targetFile = filepath.Join(dst, filepath.Base(companionSrc))
		}
		return h.downloadFileFromCompanion(ctx, dev, phonePath, targetFile, style, onProgress)
	}

	// 2. Otherwise treat as directory
	files, err := FetchCompanionFilesFromDevice(dev, phonePath)
	if err == nil {
		targetDir := dst
		dinfo, statErr := os.Stat(dst)
		if (statErr == nil && dinfo.IsDir()) || strings.HasSuffix(dst, "/") {
			targetDir = filepath.Join(dst, filepath.Base(companionSrc))
		}
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return err
		}
		// Every child's failure is returned (they used to be logged and
		// dropped, so a folder copy from a phone reported success with
		// files missing - and a move then deleted them on the phone).
		var errs []error
		for _, it := range files {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			childSrc := filepath.Join(companionSrc, it.Name)
			if it.IsDir {
				if err := h.CopyFromCompanion(ctx, childSrc, targetDir, style, onProgress); err != nil {
					errs = append(errs, err)
				}
			} else {
				childDst := filepath.Join(targetDir, it.Name)
				if err := h.downloadFileFromCompanion(ctx, dev, it.Path, childDst, style, onProgress); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", it.Name, err))
				}
			}
		}
		return errors.Join(errs...)
	}

	// 3. Fallback: attempt single file download
	targetFile := dst
	dinfo, statErr := os.Stat(dst)
	if (statErr == nil && dinfo.IsDir()) || strings.HasSuffix(dst, "/") {
		targetFile = filepath.Join(dst, filepath.Base(companionSrc))
	}
	return h.downloadFileFromCompanion(ctx, dev, phonePath, targetFile, style, onProgress)
}

func (h *companionIOHandlerImpl) downloadFileFromCompanion(ctx context.Context, dev *CompanionDevice, phonePath, targetFile, style string, onProgress func(int64)) error {
	if style == "skip" && file.Exists(targetFile) {
		return nil
	}
	// If a directory with the exact same name was erroneously created previously, remove it
	if fi, statErr := os.Stat(targetFile); statErr == nil && fi.IsDir() {
		_ = os.RemoveAll(targetFile)
	}
	if err := os.MkdirAll(filepath.Dir(targetFile), 0755); err != nil {
		return err
	}
	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	urlStr := fmt.Sprintf("http://%s:%d/download?path=%s&download=1", dev.IP, port, url.QueryEscape(phonePath))
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Companion-Secret", dev.Secret)
	client := &http.Client{Timeout: 60 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("companion download failed with status %d", resp.StatusCode)
	}

	// Temp name + fsync + size check + rename: a dropped Wi-Fi connection
	// mid-download never leaves a truncated file under the real name.
	tmp := filepath.Join(filepath.Dir(targetFile), "."+filepath.Base(targetFile)+".nvtmp-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			out.Close()
			_ = os.Remove(tmp)
		}
	}()

	var written int64
	buf := make([]byte, 64*1024)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			written += int64(n)
			if onProgress != nil {
				onProgress(written)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}
	if resp.ContentLength >= 0 && written != resp.ContentLength {
		return fmt.Errorf("download incomplete: got %d of %d bytes", written, resp.ContentLength)
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	done = true
	if err := os.Rename(tmp, targetFile); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	dev.LastSeen = time.Now()
	dev.IsOnline = true
	return nil
}

func (h *companionIOHandlerImpl) CopyToCompanion(ctx context.Context, src, companionDst, style string, onProgress func(int64)) error {
	dev, phoneDestDir := GetCompanionDeviceByStoragePath(companionDst)
	if dev == nil {
		return fmt.Errorf("companion device not found for target %s", companionDst)
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if srcInfo.IsDir() {
		baseDirName := filepath.Base(src)
		return filepath.Walk(src, func(currentPath string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || ctx.Err() != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(src, currentPath)
			if err != nil {
				return err
			}
			targetPhonePath := filepath.Join(phoneDestDir, baseDirName, rel)
			return h.uploadFileToCompanion(ctx, dev, currentPath, targetPhonePath, onProgress)
		})
	}
	targetPhonePath := filepath.Join(phoneDestDir, filepath.Base(src))
	return h.uploadFileToCompanion(ctx, dev, src, targetPhonePath, onProgress)
}

func (h *companionIOHandlerImpl) uploadFileToCompanion(ctx context.Context, dev *CompanionDevice, localSrc, targetPhonePath string, onProgress func(int64)) error {
	srcFile, err := os.Open(localSrc)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcStat, _ := srcFile.Stat()
	fileSize := srcStat.Size()

	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	urlStr := fmt.Sprintf("http://%s:%d/upload?path=%s", dev.IP, port, url.QueryEscape(targetPhonePath))

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		buf := make([]byte, 64*1024)
		var sent int64
		for {
			if ctx.Err() != nil {
				pw.CloseWithError(ctx.Err())
				return
			}
			n, rErr := srcFile.Read(buf)
			if n > 0 {
				if _, wErr := pw.Write(buf[:n]); wErr != nil {
					return
				}
				sent += int64(n)
				if onProgress != nil {
					onProgress(sent)
				}
			}
			if rErr != nil {
				// A read error must abort the upload, not look like a
				// clean end of file to the phone.
				if rErr != io.EOF {
					pw.CloseWithError(rErr)
				}
				return
			}
		}
	}()

	req, err := http.NewRequestWithContext(ctx, "POST", urlStr, pr)
	if err != nil {
		return err
	}
	req.ContentLength = fileSize
	req.Header.Set("X-Companion-Secret", dev.Secret)
	req.Header.Set("Content-Type", "application/octet-stream")

	client := &http.Client{Timeout: 60 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("companion upload failed with status %d", resp.StatusCode)
	}
	var ack struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10)); len(body) > 0 && json.Unmarshal(body, &ack) == nil && ack.Success != nil && !*ack.Success {
		if ack.Error == "" {
			ack.Error = "the device rejected the upload"
		}
		return errors.New(ack.Error)
	}
	dev.LastSeen = time.Now()
	dev.IsOnline = true
	return nil
}

func (h *companionIOHandlerImpl) DeleteCompanionPath(ctx context.Context, p string) error {
	dev, phonePath := GetCompanionDeviceByStoragePath(p)
	if dev == nil {
		return fmt.Errorf("companion device not found for path: %s", p)
	}
	return ProxyCompanionFileDelete(dev, phonePath)
}

func init() {
	service.CompanionHandler = &companionIOHandlerImpl{}
}
