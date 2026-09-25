package v1

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
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
	// OwnerUserID is the NivaroOS user (JWT "id" claim) that registered the
	// device. List/rename/browse/delete/upload/download are limited to that
	// user. Devices saved before this field existed have none until their
	// phone's next authenticated heartbeat (every 30 s) claims them - see
	// PostRegisterCompanionDevice; until then they stay visible to everyone,
	// as before, so an old phone that never comes back can still be removed.
	OwnerUserID string `json:"owner_user_id,omitempty"`
	// Connection is how the server can reach the phone right now, filled
	// by GetCompanionDevices: "lan" (direct - files open and copy), "remote"
	// (online through the tunnel/heartbeat only - files can be listed, not
	// opened) or "offline". Not meaningful once persisted; reset on load.
	Connection string `json:"connection,omitempty"`
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
	// Repair is true only when the user tapped "Pair again" on a phone the
	// server removed; the app's automatic heartbeat never sets it.
	Repair bool `json:"repair,omitempty"`
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
	companionWSConns     = make(map[string]*companionTunnel)
	companionPendingMu   sync.Mutex
	companionPendingReqs = make(map[string]chan map[string]interface{})
	wsUpgrader           = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     nivaroos_middleware.CheckWebSocketOrigin,
	}
)

// companionTunnel is a phone's reverse WebSocket. gorilla/websocket allows
// one concurrent writer only, and several web UI tabs can list the same
// phone at once, so every write goes through send.
type companionTunnel struct {
	conn *websocket.Conn
	wmu  sync.Mutex
}

func (t *companionTunnel) send(v interface{}) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_ = t.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return t.conn.WriteJSON(v)
}

func companionPendingKey(deviceID, reqID string) string {
	return deviceID + "\x00" + reqID
}

func companionTunnelFor(id string) *companionTunnel {
	companionWSMu.RLock()
	defer companionWSMu.RUnlock()
	return companionWSConns[id]
}

// notifyCompanionRemoved sends {"type":"removed"} over the phone's tunnel,
// if it has one open, so the app unpairs right away instead of at its next
// heartbeat.
func notifyCompanionRemoved(id string) {
	companionWSMu.Lock()
	t, ok := companionWSConns[id]
	companionWSMu.Unlock()
	if !ok {
		return
	}
	_ = t.send(map[string]string{"type": "removed"})
}

func closeCompanionTunnel(id string) {
	companionWSMu.Lock()
	if t, ok := companionWSConns[id]; ok {
		t.conn.Close()
		delete(companionWSConns, id)
	}
	companionWSMu.Unlock()
}

// companionHasLANIP reports whether ip is an address the server could dial
// the phone's file server on (the heartbeat sends the phone's own Wi-Fi
// address).
func companionHasLANIP(ip string) bool {
	ip = strings.TrimSpace(ip)
	return ip != "" && ip != "Local Device" && ip != "Local" && !strings.HasPrefix(ip, "127.")
}

// Direct server->phone calls go to a private LAN address the server may not
// be able to route to at all (phone on mobile data or another Wi-Fi). The
// default dialer waits for the kernel's TCP connect timeout (~2 minutes);
// a few seconds is plenty on a LAN.
var companionTransport = &http.Transport{
	DialContext:         (&net.Dialer{Timeout: 4 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout: 10 * time.Second,
	MaxIdleConnsPerHost: 4,
	IdleConnTimeout:     90 * time.Second,
}

func companionClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: companionTransport}
}

// errCompanionNotOnLAN: the phone is online (tunnel up or heartbeat within
// the last 75 s) but the server can't open a connection to it - it isn't on
// the same network. The reverse tunnel only carries directory listings (the
// app implements "list" and "ping" only), so opening, downloading or
// uploading a file needs the direct connection.
type errCompanionNotOnLAN struct {
	name  string
	cause error
}

func (e *errCompanionNotOnLAN) Error() string {
	return companionNotOnLANMessage(e.name)
}

func (e *errCompanionNotOnLAN) Unwrap() error { return e.cause }

func companionNotOnLANMessage(name string) string {
	if strings.TrimSpace(name) == "" {
		name = "The phone"
	}
	return name + " isn't on the same network as the server - open it from the phone, or when both are on the same network"
}

// companionOnlineRemotely: the phone is talking to the server (reverse
// tunnel connected, or a heartbeat in the last two sync intervals).
func companionOnlineRemotely(dev *CompanionDevice) bool {
	return companionTunnelFor(dev.ID) != nil || time.Since(dev.LastSeen) <= 75*time.Second
}

// companionDirectErr turns a failed direct connection to the phone into
// errCompanionNotOnLAN when the phone is otherwise online, so every caller
// can tell the user why instead of a raw "dial tcp ... i/o timeout".
// Errors the phone itself answered with (HTTP status) are not passed here.
func companionDirectErr(dev *CompanionDevice, err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	var nl *errCompanionNotOnLAN
	if errors.As(err, &nl) {
		return err
	}
	if companionOnlineRemotely(dev) {
		return &errCompanionNotOnLAN{name: dev.Name, cause: err}
	}
	return err
}

// companionNoLANIPErr is returned when the device never reported a LAN
// address at all.
func companionNoLANIPErr(dev *CompanionDevice) error {
	return companionDirectErr(dev, errors.New("companion device has no direct LAN IP"))
}

// companionUnreachableMessage is what a failed phone download shows the user.
func companionUnreachableMessage(dev *CompanionDevice, err error) string {
	var nl *errCompanionNotOnLAN
	if errors.As(err, &nl) {
		return nl.Error()
	}
	return dev.Name + " can't be reached right now - make sure the NivaroOS app is open on it and it's on the same network"
}

// Where companion state lives. Variables only so tests can point them at a
// temporary directory; nothing else changes them.
var (
	companionStateDir   = "/var/lib/nivaroos"
	companionBaseDir    = "/DATA/Companion"
	companionBaseNoDATA = "/var/lib/nivaroos/companion"
)

func getCompanionStorageBasePath() string {
	base := companionBaseDir
	if _, err := os.Stat(filepath.Dir(base)); os.IsNotExist(err) {
		base = companionBaseNoDATA
	}
	os.MkdirAll(base, 0755)
	return base
}

func getCompanionConfigPath() string {
	os.MkdirAll(companionStateDir, 0755)
	return filepath.Join(companionStateDir, "companion_devices.json")
}

// companionKeptFolder records whose backups a folder holds after its
// device was removed with "keep backups" (companion_kept_folders.json,
// keyed by the folder's clean path). Only assignCompanionFolder reads it.
type companionKeptFolder struct {
	DeviceID    string    `json:"device_id"`
	OwnerUserID string    `json:"owner_user_id,omitempty"`
	KeptAt      time.Time `json:"kept_at"`
}

var companionKeptFolders = map[string]companionKeptFolder{}

// companionRemoved remembers phones a user removed (companion_removed.json,
// by device id), so the app's automatic re-registration can't silently add
// them back: it gets 410 until the user pairs the phone again from the app.
// Entries expire after companionRemovedTTL.
type companionRemovedRec struct {
	OwnerUserID string    `json:"owner_user_id,omitempty"`
	RemovedAt   time.Time `json:"removed_at"`
}

var companionRemoved = map[string]companionRemovedRec{}

const companionRemovedTTL = 180 * 24 * time.Hour

func getCompanionRemovedPath() string {
	os.MkdirAll(companionStateDir, 0755)
	return filepath.Join(companionStateDir, "companion_removed.json")
}

func saveCompanionRemovedLocked() {
	data, err := json.MarshalIndent(companionRemoved, "", "  ")
	if err == nil {
		err = os.WriteFile(getCompanionRemovedPath(), data, 0600)
	}
	if err != nil {
		logger.Error("companion: saving removed devices failed", zap.Error(err))
	}
}

func getCompanionKeptFoldersPath() string {
	os.MkdirAll(companionStateDir, 0755)
	return filepath.Join(companionStateDir, "companion_kept_folders.json")
}

func saveCompanionKeptFoldersLocked() {
	data, err := json.MarshalIndent(companionKeptFolders, "", "  ")
	if err == nil {
		err = os.WriteFile(getCompanionKeptFoldersPath(), data, 0600)
	}
	if err != nil {
		logger.Error("companion: saving kept backup folders failed", zap.Error(err))
	}
}

func getCompanionSecretsPath() string {
	os.MkdirAll(companionStateDir, 0755)
	return filepath.Join(companionStateDir, "companion_secrets.json")
}

func loadCompanionDevicesLocked() {
	if companionLoaded {
		return
	}
	companionLoaded = true
	companionRemoved = map[string]companionRemovedRec{}
	if data, err := os.ReadFile(getCompanionRemovedPath()); err == nil {
		var removed map[string]companionRemovedRec
		if json.Unmarshal(data, &removed) == nil {
			for id, rec := range removed {
				if time.Since(rec.RemovedAt) < companionRemovedTTL {
					companionRemoved[id] = rec
				}
			}
		}
	}
	companionKeptFolders = map[string]companionKeptFolder{}
	if data, err := os.ReadFile(getCompanionKeptFoldersPath()); err == nil {
		var kept map[string]companionKeptFolder
		if json.Unmarshal(data, &kept) == nil {
			for p, rec := range kept {
				companionKeptFolders[filepath.Clean(p)] = rec
			}
		}
	}
	filePath := getCompanionConfigPath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	var list []*CompanionDevice
	if err := json.Unmarshal(data, &list); err == nil {
		for _, dev := range mergeDuplicateCompanionDevices(list) {
			if dev.Port <= 0 {
				dev.Port = 8765
			}
			if dev.RootPath == "" {
				dev.RootPath = "/storage/emulated/0"
			}
			dev.SharesStorage = true
			dev.Connection = ""
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

// mergeDuplicateCompanionDevices folds entries of companion_devices.json
// that carry the SAME device id (older builds could write one twice) into
// one: the most recently seen entry wins, and fields only the other entry
// has (backup folder, owner, created time, user rename) are kept. Devices are
// never merged by IP address or by model + name any more: the IP is the
// phone's private LAN address, so two phones on different home networks (or
// one phone given another's old DHCP lease), or two identical phones with
// default names, used to delete each other and their folder mapping.
// Entries without an id are dropped. Order of first appearance is kept.
func mergeDuplicateCompanionDevices(list []*CompanionDevice) []*CompanionDevice {
	out := make([]*CompanionDevice, 0, len(list))
	byID := make(map[string]int, len(list))
	for _, dev := range list {
		if dev == nil {
			continue
		}
		dev.ID = strings.TrimSpace(dev.ID)
		if dev.ID == "" {
			continue
		}
		i, dup := byID[dev.ID]
		if !dup {
			byID[dev.ID] = len(out)
			out = append(out, dev)
			continue
		}
		keep, other := out[i], dev
		if dev.LastSeen.After(keep.LastSeen) {
			keep, other = dev, out[i]
		}
		if keep.StoragePath == "" {
			keep.StoragePath = other.StoragePath
		}
		if keep.OwnerUserID == "" {
			keep.OwnerUserID = other.OwnerUserID
		}
		if keep.CreatedAt.IsZero() || (!other.CreatedAt.IsZero() && other.CreatedAt.Before(keep.CreatedAt)) {
			keep.CreatedAt = other.CreatedAt
		}
		if ur, _ := other.CustomProps["user_renamed"].(bool); ur {
			if kr, _ := keep.CustomProps["user_renamed"].(bool); !kr {
				// The user's chosen name (and its folder) beats a default one.
				keep.Name, keep.StoragePath = other.Name, other.StoragePath
				if keep.CustomProps == nil {
					keep.CustomProps = map[string]interface{}{}
				}
				keep.CustomProps["user_renamed"] = true
			}
		}
		out[i] = keep
	}
	return out
}

func saveCompanionDevicesLocked() error {
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

// companionCaller returns the id of the NivaroOS user making the request,
// from the JWT claims the /v1 group's middleware verified and stored in the
// echo context (never from a header a client could send). "" means the
// request was same-host automation that skipped the token
// (nivaroos_middleware.LocalAutomationSkipper) - it is not scoped to a user.
func companionCaller(ctx echo.Context) string {
	if claims, ok := ctx.Get("user").(*jwt.Claims); ok && claims != nil {
		return strconv.Itoa(claims.ID)
	}
	return ""
}

// companionVisibleTo: a user sees the devices they registered, plus legacy
// devices no one has claimed yet. Unscoped callers (local automation) see all.
func companionVisibleTo(dev *CompanionDevice, uid string) bool {
	return uid == "" || dev.OwnerUserID == "" || dev.OwnerUserID == uid
}

// lookupCompanionDevice returns a snapshot of device id if the caller may
// use it. A device that belongs to someone else is reported exactly like a
// missing one.
func lookupCompanionDevice(ctx echo.Context, id string) (*CompanionDevice, bool) {
	uid := companionCaller(ctx)
	companionMu.Lock()
	defer companionMu.Unlock()
	loadCompanionDevicesLocked()
	dev, ok := companionDevices[id]
	if !ok || !companionVisibleTo(dev, uid) {
		return nil, false
	}
	return dev.snapshot(), true
}

// snapshot copies a device out of companionDevices. Everything that works
// on a device after companionMu is released (proxying to the phone, JSON
// encoding it) must use a snapshot: the live entry - its CustomProps map
// included - is rewritten under the lock by every 30 s heartbeat, and an
// unlocked map read during that write is a fatal runtime error that kills
// core, not a recoverable panic. Must be called with companionMu held.
func (d *CompanionDevice) snapshot() *CompanionDevice {
	c := *d
	if d.CustomProps != nil {
		// Values are only ever replaced whole (never mutated in place), so
		// copying the top level is enough.
		c.CustomProps = make(map[string]interface{}, len(d.CustomProps))
		for k, v := range d.CustomProps {
			c.CustomProps[k] = v
		}
	}
	return &c
}

// markCompanionSeen records that the phone just answered: on dev (a
// snapshot the caller holds) and on the live entry, under companionMu.
// Never call it with companionMu held.
func markCompanionSeen(dev *CompanionDevice) {
	now := time.Now()
	dev.LastSeen, dev.IsOnline = now, true
	companionMu.Lock()
	if live, ok := companionDevices[dev.ID]; ok && live != dev && now.After(live.LastSeen) {
		live.LastSeen, live.IsOnline = now, true
	}
	companionMu.Unlock()
}

func companionNotFound(ctx echo.Context) error {
	return ctx.JSON(http.StatusNotFound, model.Result{
		Success: common_err.SERVICE_ERROR,
		Message: "companion device not found",
	})
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

// companionSubdir resolves a client-supplied sub path ("", "/", "Photos",
// "/Photos/2026") inside root. filepath.Clean keeps a leading "..", so a
// plain Join let "?path=../../etc" escape the device's folder - with core
// running as root, that was a write (upload) or listing anywhere on the
// box. Anything that would leave root is refused.
func companionSubdir(root, sub string) (string, error) {
	if sub == "" || sub == "/" {
		return root, nil
	}
	p := filepath.Join(root, filepath.Clean("/"+sub))
	if !withinDir(root, p) {
		return "", errors.New("path outside companion device storage")
	}
	return p, nil
}

// withinDir reports whether p is root or inside it, by path components
// (so "/data/companion-evil" is not inside "/data/companion"). An empty
// root contains nothing.
func withinDir(root, p string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
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

// GetCompanionDeviceByStoragePath maps a filesystem path to (a snapshot of)
// the owning CompanionDevice and the target phone path.
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
			return dev.snapshot(), root
		}
		if strings.HasPrefix(cleanP, cleanDevPath+"/") {
			rel := strings.TrimPrefix(cleanP, cleanDevPath)
			// Map relative path onto device root
			target := filepath.Join(root, rel)
			return dev.snapshot(), target
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
	if companionHasLANIP(devIP) {
		port := dev.Port
		if port <= 0 {
			port = 8765
		}
		urlStr := fmt.Sprintf("http://%s:%d/files?path=%s", devIP, port, url.QueryEscape(phonePath))
		req, err := http.NewRequest("GET", urlStr, nil)
		if err == nil {
			req.Header.Set("X-Companion-Secret", dev.Secret)
			client := companionClient(15 * time.Second) // a large folder (DCIM) can take seconds to list on the phone
			resp, err := client.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				var body struct {
					Success bool                `json:"success"`
					Files   []CompanionFileItem `json:"files"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Success {
					markCompanionSeen(dev)
					return body.Files, nil
				}
			}
		}
	}

	// 2. Try WebSocket reverse tunnel
	if ws := companionTunnelFor(dev.ID); ws != nil {
		reqID := fmt.Sprintf("req_%d", time.Now().UnixNano())
		ch := make(chan map[string]interface{}, 1)
		// Keyed by device too: only this phone's tunnel can answer it.
		pendingKey := companionPendingKey(dev.ID, reqID)

		companionPendingMu.Lock()
		companionPendingReqs[pendingKey] = ch
		companionPendingMu.Unlock()

		defer func() {
			companionPendingMu.Lock()
			delete(companionPendingReqs, pendingKey)
			companionPendingMu.Unlock()
		}()

		msg := map[string]interface{}{
			"id":     reqID,
			"action": "list",
			"path":   phonePath,
		}
		if err := ws.send(msg); err == nil {
			select {
			case res := <-ch:
				// The phone answers success:false for a file or a missing
				// folder - that is not an empty folder (a copy of a file
				// through the tunnel used to "succeed" with nothing copied).
				if ok, isBool := res["success"].(bool); isBool && !ok {
					m, _ := res["message"].(string)
					if m == "" {
						m = "the device couldn't list " + phonePath
					}
					return nil, errors.New(m)
				}
				if filesRaw, ok := res["files"].([]interface{}); ok {
					var items []CompanionFileItem
					data, _ := json.Marshal(filesRaw)
					json.Unmarshal(data, &items)
					markCompanionSeen(dev)
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
	if !companionHasLANIP(devIP) {
		return companionNoLANIPErr(dev)
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

	resp, err := companionClient(60 * time.Minute).Do(req)
	if err != nil {
		return companionDirectErr(dev, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("companion device returned status %d", resp.StatusCode)
	}

	markCompanionSeen(dev)

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
	if !companionHasLANIP(devIP) {
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

	resp, err := companionClient(5 * time.Second).Do(req)
	if err == nil {
		resp.Body.Close()
	}
	return nil
}

// ProxyCompanionFileRename sends a rename request to the companion device with fallback to streaming
func ProxyCompanionFileRename(dev *CompanionDevice, oldPath, newPath string) error {
	devIP := dev.IP
	if !companionHasLANIP(devIP) {
		return companionNoLANIPErr(dev)
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
		resp, err := companionClient(5 * time.Second).Do(req)
		if err != nil {
			// Not reachable at all: the fallback below would fail the same way.
			return companionDirectErr(dev, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			markCompanionSeen(dev)
			return nil
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
	client := companionClient(60 * time.Minute)
	dlResp, err := client.Do(dlReq)
	if err != nil {
		tmpFile.Close()
		return companionDirectErr(dev, err)
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
		return companionDirectErr(dev, err)
	}
	defer ulResp.Body.Close()
	if ulResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to write companion file for rename (status %d)", ulResp.StatusCode)
	}

	_ = ProxyCompanionFileDelete(dev, oldPath)
	markCompanionSeen(dev)
	return nil
}

// ProxyCompanionMkdir creates a directory on the companion device
func ProxyCompanionMkdir(dev *CompanionDevice, phonePath string) error {
	devIP := dev.IP
	if !companionHasLANIP(devIP) {
		return companionNoLANIPErr(dev)
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
		resp, err := companionClient(5 * time.Second).Do(req)
		if err != nil {
			return companionDirectErr(dev, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			markCompanionSeen(dev)
			return nil
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
	ulResp, err := companionClient(5 * time.Second).Do(ulReq)
	if err != nil {
		return companionDirectErr(dev, err)
	}
	defer ulResp.Body.Close()
	_ = ProxyCompanionFileDelete(dev, dummyPath)
	markCompanionSeen(dev)
	return nil
}

// ProxyCompanionUploadStream streams content to the companion device at phonePath
func ProxyCompanionUploadStream(dev *CompanionDevice, phonePath string, reader io.Reader, size int64) error {
	devIP := dev.IP
	if !companionHasLANIP(devIP) {
		return companionNoLANIPErr(dev)
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

	resp, err := companionClient(60 * time.Minute).Do(req)
	if err != nil {
		return companionDirectErr(dev, err)
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
	markCompanionSeen(dev)
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
	if companionTunnelFor(dev.ID) != nil {
		dev.LastSeen = time.Now()
		dev.IsOnline = true
		return
	}

	// 2. Proactive LAN check on port 8765
	if probeCompanionLAN(dev) {
		dev.LastSeen = time.Now()
		dev.IsOnline = true
		return
	}

	// 3. If seen recently (under 75 seconds - two sync intervals), consider online
	if time.Since(dev.LastSeen) <= 75*time.Second {
		dev.IsOnline = true
		return
	}

	dev.IsOnline = false
}

// probeCompanionLAN reports whether the phone's file server answers directly.
func probeCompanionLAN(dev *CompanionDevice) bool {
	if !companionHasLANIP(dev.IP) {
		return false
	}
	port := dev.Port
	if port <= 0 {
		port = 8765
	}
	client := &http.Client{Timeout: 800 * time.Millisecond, Transport: companionTransport}
	resp, err := client.Get(fmt.Sprintf("http://%s:%d/status", dev.IP, port))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// probeCompanionConnection fills dev.Connection (and IsOnline) for the device
// list: "lan" when the server can open files on the phone, "remote" when the
// phone is online but only through the tunnel/heartbeat, else "offline".
func probeCompanionConnection(dev *CompanionDevice) {
	if probeCompanionLAN(dev) {
		dev.Connection = "lan"
		dev.LastSeen = time.Now()
		dev.IsOnline = true
		return
	}
	if companionOnlineRemotely(dev) {
		dev.Connection = "remote"
		dev.IsOnline = true
		return
	}
	dev.Connection = "offline"
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
	uid := companionCaller(ctx)
	companionMu.Lock()
	defer companionMu.Unlock()
	loadCompanionDevicesLocked()

	list := make([]*CompanionDevice, 0, len(companionDevices))

	var wg sync.WaitGroup
	for _, dev := range companionDevices {
		if !companionVisibleTo(dev, uid) {
			continue
		}
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
			probeCompanionConnection(d)
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

// assignCompanionFolder gives a device without a backup folder
// <base>/<name>. A folder another device uses is never shared, and an
// existing folder is reused only when companionKeptFolders says it holds
// the kept backups of this same device, or of another phone of the same
// user - so a phone removed and paired again finds its backups, but a new
// "Pixel 8" of another user never gets (reads, overwrites) the kept backups
// of a removed "Pixel 8". Any other existing folder - including one kept
// before these records existed, whose owner is unknown - counts as taken:
// "<name> (2)" etc. instead.
func assignCompanionFolder(base string, devs map[string]*CompanionDevice, dev *CompanionDevice, name string) {
	stem := sanitizeFilename(name)
	if stem == "" {
		stem = sanitizeFilename(dev.ID)
	}
	candidate := filepath.Join(base, stem)
	reuse := true
	for id, other := range devs {
		if id != dev.ID && other.StoragePath != "" && filepath.Clean(other.StoragePath) == candidate {
			reuse = false
		}
	}
	if reuse {
		if _, err := os.Lstat(candidate); err == nil {
			rec, ok := companionKeptFolders[candidate]
			reuse = ok && (rec.DeviceID == dev.ID || (rec.OwnerUserID != "" && rec.OwnerUserID == dev.OwnerUserID))
		}
	}
	if !reuse {
		if err := relocateDeviceFolder(base, devs, dev, name); err != nil {
			logger.Error("companion: creating backup folder failed", zap.String("device", dev.ID), zap.Error(err))
		}
		return
	}
	if _, ok := companionKeptFolders[candidate]; ok {
		delete(companionKeptFolders, candidate)
		saveCompanionKeptFoldersLocked()
	}
	dev.StoragePath = candidate
	if err := os.MkdirAll(candidate, 0o755); err != nil {
		logger.Error("companion: creating backup folder failed", zap.String("device", dev.ID), zap.Error(err))
	}
}

// POST /v1/companion/register
//
// Devices are identified by their id only (S-02). The caller becomes the
// device's owner (S-03); a device saved before owners existed is claimed by
// the first authenticated register call carrying its id - the phone's own
// heartbeat, every 30 s - which is the whole (idempotent) migration: it only
// ever fills an empty owner. A device owned by another user is refused.
func PostRegisterCompanionDevice(ctx echo.Context) error {
	var input CompanionRegistrationDTO
	if err := ctx.Bind(&input); err != nil {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "invalid device registration payload: " + err.Error(),
		})
	}

	input.ID = strings.TrimSpace(input.ID)
	if input.ID == "" {
		// Used to become "dev_<caller IP>": every phone behind one NAT (or
		// on one mobile carrier gateway) then shared - and overwrote - one
		// device. The app always sends its persistent id.
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "device id required",
		})
	}
	// The pairing secret travels in the response only; never keep it in
	// custom_props, which every device list returns.
	delete(input.CustomProps, "secret")

	uid := companionCaller(ctx)
	realIP := ctx.RealIP()

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
	if !companionHasLANIP(resolvedIP) {
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

	now := time.Now()
	matchedDev := companionDevices[input.ID]

	// A phone the user removed stays removed: the heartbeat gets 410 and the
	// app stops, until the user chooses "Pair again" (repair=true). Another
	// account can't use that to undo the removal of someone else's phone.
	if rec, removed := companionRemoved[input.ID]; removed && matchedDev == nil {
		if !input.Repair || (rec.OwnerUserID != "" && uid != "" && rec.OwnerUserID != uid) {
			return ctx.JSON(http.StatusGone, model.Result{
				Success: http.StatusGone,
				Message: "this phone was removed from NivaroOS - pair it again from the app to reconnect",
			})
		}
		delete(companionRemoved, input.ID)
		saveCompanionRemovedLocked()
	}

	if matchedDev != nil && uid != "" && matchedDev.OwnerUserID != "" && matchedDev.OwnerUserID != uid {
		return ctx.JSON(http.StatusForbidden, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "this device is paired with another NivaroOS account - remove it there first, or sign in to the app with that account",
		})
	}

	if matchedDev != nil && matchedDev.OwnerUserID == "" && uid != "" {
		// A device saved before owners existed. Its id is visible to every
		// user, so knowing it proves nothing: the phone proves itself with
		// the file-server secret it was given at its last registration
		// (the app sends it as X-Companion-Secret). Only a device the
		// server holds no secret for - there is nothing to prove against,
		// and nothing to leak - is claimed by the first user register.
		proof := ctx.Request().Header.Get("X-Companion-Secret")
		if matchedDev.Secret != "" && (proof == "" || subtle.ConstantTimeCompare([]byte(proof), []byte(matchedDev.Secret)) != 1) {
			// Unproven: an app too old to send the secret, or someone else.
			// Keep the phone listed as online, but change nothing an
			// attacker could use (address, port, name, props) and hand out
			// no secret; the owner's phone claims it once its app is updated.
			matchedDev.IsOnline, matchedDev.LastSeen = true, now
			if input.BatteryLevel > 0 {
				matchedDev.BatteryLevel = input.BatteryLevel
			}
			if input.StorageUsed > 0 {
				matchedDev.StorageUsed = input.StorageUsed
			}
			if input.StorageTotal > 0 {
				matchedDev.StorageTotal = input.StorageTotal
			}
			saveCompanionDevicesLocked()
			return ctx.JSON(http.StatusOK, model.Result{
				Success: common_err.SUCCESS,
				Message: "companion device seen - update the NivaroOS app on this phone to link it to your account",
				Data:    map[string]interface{}{"device": matchedDev.snapshot(), "secret": ""},
			})
		}
		// Proven, or no secret yet - then one is minted below (never taken
		// from the request).
		matchedDev.OwnerUserID = uid
		logger.Info("companion: legacy device claimed by its owner", zap.String("device", matchedDev.ID), zap.String("user_id", uid), zap.Bool("proved_with_secret", matchedDev.Secret != ""))
	}

	if matchedDev != nil {

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
				assignCompanionFolder(getCompanionStorageBasePath(), companionDevices, matchedDev, matchedDev.Name)
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
			CustomProps:   input.CustomProps,
			OwnerUserID:   uid,
		}
		// Its own folder: a second phone with the same default name gets
		// "Pixel 8 (2)", never the first one's backups.
		assignCompanionFolder(getCompanionStorageBasePath(), companionDevices, newDev, input.Name)
		companionDevices[input.ID] = newDev
	}

	registered := companionDevices[input.ID]
	if registered.Secret == "" {
		// Always minted here, never taken from the request's
		// X-Companion-Secret: a caller must not choose the credential the
		// server sends to the phone's file server.
		secret, err := generateCompanionSecret()
		if err != nil {
			logger.Error("failed to generate companion device secret", zap.Error(err))
		} else {
			registered.Secret = secret
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
			"device": registered.snapshot(),
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
	delete(update.CustomProps, "secret")

	uid := companionCaller(ctx)
	companionMu.Lock()
	defer companionMu.Unlock()
	loadCompanionDevicesLocked()

	dev, exists := companionDevices[id]
	if !exists || !companionVisibleTo(dev, uid) {
		return companionNotFound(ctx)
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

	uid := companionCaller(ctx)
	companionMu.Lock()
	defer companionMu.Unlock()
	loadCompanionDevicesLocked()

	dev, exists := companionDevices[id]
	if !exists || !companionVisibleTo(dev, uid) {
		return companionNotFound(ctx)
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
	if ctx.QueryParam("delete_data") == "true" && dev.OwnerUserID == "" && uid != "" {
		// Not linked to an account yet, so this caller's claim to it is only
		// that they can see it - every user can.
		return ctx.JSON(http.StatusForbidden, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "this phone isn't linked to an account yet, so its backups can't be deleted from here - remove it and keep its backups, then delete the folder in Files",
		})
	}
	if inBase && !shared && ctx.QueryParam("delete_data") == "true" {
		if err := os.RemoveAll(storage); err != nil {
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: "couldn't delete the backups: " + err.Error()})
		}
		if _, ok := companionKeptFolders[storage]; ok {
			delete(companionKeptFolders, storage)
			saveCompanionKeptFoldersLocked()
		}
	} else if inBase {
		keptData = storage
		if !shared {
			// Whose backups these are, so only this phone (or another phone
			// of the same user) is ever given the folder again.
			companionKeptFolders[storage] = companionKeptFolder{DeviceID: dev.ID, OwnerUserID: dev.OwnerUserID, KeptAt: time.Now()}
			saveCompanionKeptFoldersLocked()
		}
	}

	// 2. Remove device from map and save; remember the removal so its
	// automatic re-registration doesn't add it straight back.
	delete(companionDevices, id)
	saveCompanionDevicesLocked()
	companionRemoved[id] = companionRemovedRec{OwnerUserID: dev.OwnerUserID, RemovedAt: time.Now()}
	saveCompanionRemovedLocked()

	// 3. Tell a connected phone it was removed (it unpairs itself at once),
	// then close its tunnel.
	notifyCompanionRemoved(id)
	closeCompanionTunnel(id)

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "companion device removed",
		Data:    map[string]string{"kept_backups": keptData},
	})
}

// GET /v1/companion/devices/:id/storage
func GetCompanionDeviceStorage(ctx echo.Context) error {
	dev, exists := lookupCompanionDevice(ctx, ctx.Param("id"))
	if !exists {
		return companionNotFound(ctx)
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
	subPath := ctx.QueryParam("path")

	dev, exists := lookupCompanionDevice(ctx, ctx.Param("id"))
	if !exists {
		return companionNotFound(ctx)
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

	if !strings.HasPrefix(subPath, "/storage/") {
		dir, err := companionSubdir(targetDir, subPath)
		if err != nil {
			return ctx.JSON(http.StatusForbidden, model.Result{
				Success: common_err.CLIENT_ERROR,
				Message: err.Error(),
			})
		}
		targetDir = dir
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
	filePath := ctx.QueryParam("path")

	dev, exists := lookupCompanionDevice(ctx, ctx.Param("id"))
	if !exists {
		return companionNotFound(ctx)
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

	// Fallback to server local companion storage - this device's backup
	// folder only (any folder under the companion base used to pass, i.e.
	// every other phone's backups).
	cleanPath := filepath.Clean(filePath)
	devDir := dev.StoragePath
	if devDir == "" {
		devDir = filepath.Join(getCompanionStorageBasePath(), sanitizeFilename(dev.Name))
	}
	if !withinDir(devDir, cleanPath) {
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

	dev, exists := lookupCompanionDevice(ctx, id)
	if !exists {
		return companionNotFound(ctx)
	}

	targetDir := dev.StoragePath
	if targetDir == "" {
		targetDir = filepath.Join(getCompanionStorageBasePath(), sanitizeFilename(dev.Name))
	}
	os.MkdirAll(targetDir, 0755)

	dir, err := companionSubdir(targetDir, destSubPath)
	if err != nil {
		return ctx.JSON(http.StatusForbidden, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: err.Error(),
		})
	}
	targetDir = dir
	os.MkdirAll(targetDir, 0755)

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

	// The multipart name is the client's too: only its last element.
	name := filepath.Base(filepath.Clean("/" + file.Filename))
	if name == "/" || name == "." || name == ".." {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "invalid file name",
		})
	}
	destPath := filepath.Join(targetDir, name)
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
//
// The phone's reverse tunnel. Only the device's owner may open it (another
// user holding the id could otherwise replace the tunnel and answer the
// owner's listings), and only for a registered device - the phone registers
// before (and retries this every 15 s after) so that costs nothing.
func GetCompanionDeviceWS(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return ctx.JSON(http.StatusBadRequest, echo.Map{"error": "id required"})
	}
	dev, ok := lookupCompanionDevice(ctx, id)
	if !ok {
		return companionNotFound(ctx)
	}
	// Not for a legacy device no one has claimed yet either: whoever holds
	// the tunnel answers the owner's listings and can repoint the device
	// (its "register" message). The phone claims it with its next
	// heartbeat (PostRegisterCompanionDevice) and reconnects in 15 s.
	if uid := companionCaller(ctx); uid != "" && dev.OwnerUserID != uid {
		return companionNotFound(ctx)
	}

	conn, err := wsUpgrader.Upgrade(ctx.Response(), ctx.Request(), nil)
	if err != nil {
		logger.Error("WS upgrade failed", zap.Error(err))
		return err
	}
	defer conn.Close()
	tunnel := &companionTunnel{conn: conn}

	companionWSMu.Lock()
	if prev, ok := companionWSConns[id]; ok {
		prev.conn.Close() // a reconnect replaces a half-dead tunnel
	}
	companionWSConns[id] = tunnel
	companionWSMu.Unlock()

	defer func() {
		companionWSMu.Lock()
		// Only our own entry: the replaced tunnel's exit must not remove
		// the reconnect that replaced it.
		if companionWSConns[id] == tunnel {
			delete(companionWSConns, id)
		}
		companionWSMu.Unlock()
	}()

	companionMu.Lock()
	loadCompanionDevicesLocked()
	if dev, ok := companionDevices[id]; ok {
		dev.IsOnline = true
		dev.LastSeen = time.Now()
		// ctx.RealIP() is the phone's public side when it isn't on the LAN;
		// keep the LAN address the heartbeat reported.
		if realIP := ctx.RealIP(); dev.IP == "" && companionHasLANIP(realIP) {
			dev.IP = realIP
		}
	}
	companionMu.Unlock()

	logger.Info("Companion WebSocket tunnel connected", zap.String("id", id))

	for {
		_, message, err := conn.ReadMessage()
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
					if ip, ok := msg["ip"].(string); ok && companionHasLANIP(ip) {
						dev.IP = ip
					}
					dev.SharesStorage = true
					dev.IsOnline = true
					dev.LastSeen = time.Now()
				}
				companionMu.Unlock()
			} else if reqID != "" {
				companionPendingMu.Lock()
				if ch, ok := companionPendingReqs[companionPendingKey(id, reqID)]; ok {
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
	resp, err := companionClient(5 * time.Second).Do(req)
	if err != nil {
		return 0, companionDirectErr(dev, err)
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
	if !companionHasLANIP(devIP) {
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
	resp, err := companionClient(3 * time.Second).Do(req)
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
					// Listing works through the tunnel, file transfer doesn't:
					// say so once instead of once per file.
					var nl *errCompanionNotOnLAN
					if errors.As(err, &nl) {
						return err
					}
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
	resp, err := companionClient(60 * time.Minute).Do(req)
	if err != nil {
		return companionDirectErr(dev, err)
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
	markCompanionSeen(dev)
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

	resp, err := companionClient(60 * time.Minute).Do(req)
	if err != nil {
		return companionDirectErr(dev, err)
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
	markCompanionSeen(dev)
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
