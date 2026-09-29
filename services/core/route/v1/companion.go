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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	ID   string `json:"id"`
	Name string `json:"name"`
	// NameSource says who chose Name: companionNameUser once someone renamed
	// the device (from the web or from the app - both go through
	// PutUpdateCompanionDevice), companionNameDevice while it is the name the
	// phone reported. The server is the source of truth for a user's name:
	// the phone's heartbeat never changes it, and the app shows what the
	// server says. (It used to be custom_props.user_renamed, which the app
	// also sent with its own locally kept name on every heartbeat - so a
	// rename from the web was undone within a minute.)
	NameSource    string    `json:"name_source,omitempty"`
	NameUpdatedAt time.Time `json:"name_updated_at,omitempty"`
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
	// by GetCompanionDevices: "lan" (on the home network), "remote" (away
	// from home - see Route) or "offline". Not meaningful once persisted;
	// reset on load.
	Connection string `json:"connection,omitempty"`
	// Route says how files travel right now (S-04): "lan" and "tailscale"
	// are direct connections, "tunnel" goes through the phone's connection
	// to the server (slower), "tunnel_list" is an older app's tunnel that
	// only lists folders, "" not reachable. Reset on load.
	Route string `json:"route,omitempty"`
	// Addresses are every address the phone reported it can be reached on
	// (Wi-Fi LAN, Tailscale IPv4/IPv6, MagicDNS name), filtered to the
	// allowed ranges - see companionCandidates.
	Addresses []string `json:"addresses,omitempty"`
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
	Addresses     []string               `json:"addresses,omitempty"` // every address the file server listens on (S-04)
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
	// Repair is true only for the first registration after the user signed
	// in to the app again (or, in older apps, tapped "Pair again") on a
	// phone the server removed; the automatic heartbeat never sets it.
	Repair bool `json:"repair,omitempty"`
	// NameSource "user" says Name is a name the user gave this phone (the
	// app's last known name from the server), used only when the server has
	// no name of a user's for the device - a phone paired again after it
	// was removed gets its name back. The phone can't override a name the
	// user set; renaming goes through PUT /companion/devices/:id.
	NameSource string `json:"name_source,omitempty"`
}

const (
	companionNameUser   = "user"
	companionNameDevice = "device"
)

// companionUserRenamed reports whether a user chose dev's name.
func companionUserRenamed(dev *CompanionDevice) bool {
	return dev.NameSource == companionNameUser
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
	// mux carries file streams (companion_tunnel_mux.go); set once the
	// phone's register message says its app supports them, nil for an
	// older app (listing only).
	mux atomic.Pointer[tunnelMux]
}

func (t *companionTunnel) send(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return t.sendRaw(false, data)
}

// sendRaw writes one text (JSON) or binary (stream frame) message.
func (t *companionTunnel) sendRaw(binary bool, data []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_ = t.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	kind := websocket.TextMessage
	if binary {
		kind = websocket.BinaryMessage
	}
	return t.conn.WriteMessage(kind, data)
}

// streams returns the tunnel's stream mux, or nil when the phone's app
// can only list folders through the tunnel.
func (t *companionTunnel) streams() *tunnelMux { return t.mux.Load() }

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

// companionOnlineRemotely: the phone is talking to the server (reverse
// tunnel connected, or a heartbeat in the last two sync intervals).
func companionOnlineRemotely(dev *CompanionDevice) bool {
	return companionTunnelFor(dev.ID) != nil || time.Since(dev.LastSeen) <= 75*time.Second
}

// companionUnreachableMessage is what a failed phone request shows the user.
func companionUnreachableMessage(dev *CompanionDevice, err error) string {
	var ue *errCompanionUnreachable
	if errors.As(err, &ue) {
		return ue.Error()
	}
	return companionDisplayName(dev) + " can't be reached right now - make sure the NivaroOS app is open on it with file sharing on"
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

// companionSessions binds each phone to the login sessions (the JWT "sid"
// claim) its app registered with (companion_sessions.json, by device id).
// Removing the phone revokes exactly those sessions, so the app is signed
// out - its access and refresh tokens stop working in every service - while
// the user's other sessions (the web UI doing the removing) carry on.
var companionSessions = map[string][]string{}

// companionMaxSessions: a phone keeps a handful of recent sessions (a new
// one per sign-in; a refresh keeps its session).
const companionMaxSessions = 8

func getCompanionSessionsPath() string {
	os.MkdirAll(companionStateDir, 0755)
	return filepath.Join(companionStateDir, "companion_sessions.json")
}

func saveCompanionSessionsLocked() {
	data, err := json.MarshalIndent(companionSessions, "", "  ")
	if err == nil {
		err = os.WriteFile(getCompanionSessionsPath(), data, 0600)
	}
	if err != nil {
		logger.Error("companion: saving device sessions failed", zap.Error(err))
	}
}

// bindCompanionSessionLocked records that session sid belongs to device id.
// A session belongs to one phone only: the same sign-in registering as
// another device id (the app reinstalled with a new id) moves it.
func bindCompanionSessionLocked(id, sid string) {
	if sid == "" {
		return
	}
	for _, have := range companionSessions[id] {
		if have == sid {
			return
		}
	}
	for other, sids := range companionSessions {
		if other == id {
			continue
		}
		for i, have := range sids {
			if have == sid {
				companionSessions[other] = append(sids[:i:i], sids[i+1:]...)
				if len(companionSessions[other]) == 0 {
					delete(companionSessions, other)
				}
				break
			}
		}
	}
	sids := append(companionSessions[id], sid)
	if len(sids) > companionMaxSessions {
		sids = sids[len(sids)-companionMaxSessions:]
	}
	companionSessions[id] = sids
	saveCompanionSessionsLocked()
}

// endCompanionSessionsLocked signs the removed phone's app out: its
// sessions are revoked for every service. The caller's own session is
// among them only when a phone removes itself.
func endCompanionSessionsLocked(id string) {
	sids := companionSessions[id]
	delete(companionSessions, id)
	saveCompanionSessionsLocked()
	if len(sids) == 0 {
		return
	}
	if err := jwt.RevokeSessions(sids, jwt.ReasonCompanionRemoved); err != nil {
		logger.Error("companion: signing out the removed phone failed", zap.String("device", id), zap.Error(err))
	}
}

// companionSessionOf returns the caller's login session id ("" for local
// automation or a token from before sessions had ids).
func companionSessionOf(ctx echo.Context) string {
	if claims, ok := ctx.Get("user").(*jwt.Claims); ok && claims != nil {
		return claims.SessionID
	}
	return ""
}

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
	companionSessions = map[string][]string{}
	if data, err := os.ReadFile(getCompanionSessionsPath()); err == nil {
		var sessions map[string][]string
		if json.Unmarshal(data, &sessions) == nil {
			for id, sids := range sessions {
				if len(sids) > 0 {
					companionSessions[id] = sids
				}
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
			dev.Route = ""
			migrateCompanionNameSource(dev)
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
		migrateCompanionNameSource(keep)
		migrateCompanionNameSource(other)
		if companionUserRenamed(other) && !companionUserRenamed(keep) {
			// The user's chosen name (and its folder) beats a default one.
			keep.Name, keep.StoragePath = other.Name, other.StoragePath
			keep.NameSource, keep.NameUpdatedAt = other.NameSource, other.NameUpdatedAt
		}
		out[i] = keep
	}
	return out
}

// migrateCompanionNameSource moves the old custom_props.user_renamed flag
// to NameSource (once; idempotent). Whoever set it - the web or the app -
// it was a user's rename.
func migrateCompanionNameSource(dev *CompanionDevice) {
	if ur, _ := dev.CustomProps["user_renamed"].(bool); ur && dev.NameSource == "" {
		dev.NameSource = companionNameUser
	}
	delete(dev.CustomProps, "user_renamed")
	if dev.NameSource == "" {
		dev.NameSource = companionNameDevice
	}
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

// FetchCompanionFilesFromDevice fetches the live file listing from the
// phone: directly (LAN or Tailscale) or streamed through its tunnel
// (companionDo), else - an older app's tunnel - with the tunnel's "list"
// message.
func FetchCompanionFilesFromDevice(dev *CompanionDevice, phonePath string) ([]CompanionFileItem, error) {
	if phonePath == "" {
		phonePath = "/storage/emulated/0"
	}

	// A large folder (DCIM) can take seconds to list on the phone.
	resp, err := companionDo(context.Background(), dev, companionRequest{
		Method: http.MethodGet, Path: "/files", Query: url.Values{"path": {phonePath}}, Timeout: 30 * time.Second,
	})
	if err == nil {
		defer resp.Body.Close()
		var body struct {
			Success bool                `json:"success"`
			Files   []CompanionFileItem `json:"files"`
			Message string              `json:"message"`
		}
		derr := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&body)
		if resp.StatusCode == http.StatusOK && derr == nil && body.Success {
			return body.Files, nil
		}
		if derr == nil && !body.Success {
			// The phone answered: a file, a missing folder, a refused path
			// - not an empty folder.
			if body.Message == "" {
				body.Message = "the device couldn't list " + phonePath
			}
			return nil, errors.New(body.Message)
		}
		return nil, fmt.Errorf("the device couldn't list %s (status %d)", phonePath, resp.StatusCode)
	}

	// An app from before tunnel streams: its tunnel still lists folders.
	if ws := companionTunnelFor(dev.ID); ws != nil && ws.streams() == nil {
		if items, lerr := companionTunnelList(ws, dev, phonePath); lerr == nil || !errors.Is(lerr, errTunnelListTimeout) {
			return items, lerr
		}
	}
	return nil, err
}

var errTunnelListTimeout = errors.New("the phone didn't answer in time")

// companionTunnelList is the legacy tunnel "list" request.
func companionTunnelList(ws *companionTunnel, dev *CompanionDevice, phonePath string) ([]CompanionFileItem, error) {
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

	if err := ws.send(map[string]interface{}{"id": reqID, "action": "list", "path": phonePath}); err != nil {
		return nil, err
	}
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
		var items []CompanionFileItem
		if filesRaw, ok := res["files"].([]interface{}); ok {
			data, _ := json.Marshal(filesRaw)
			json.Unmarshal(data, &items)
		}
		markCompanionSeen(dev)
		return items, nil
	case <-time.After(15 * time.Second):
		logger.Info("WS list request timed out", zap.String("dev_id", dev.ID))
		return nil, errTunnelListTimeout
	}
}

// companionAck reads the phone's {"success": false, "error"/"message"}
// answer, if it is one.
func companionAck(r io.Reader) error {
	var ack struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if body, _ := io.ReadAll(io.LimitReader(r, 64<<10)); len(body) > 0 && json.Unmarshal(body, &ack) == nil && ack.Success != nil && !*ack.Success {
		if ack.Error == "" {
			ack.Error = ack.Message
		}
		if ack.Error == "" {
			ack.Error = "the device rejected the request"
		}
		return errors.New(ack.Error)
	}
	return nil
}

// companionSimple sends a body-less request and checks the phone's answer.
func companionSimple(dev *CompanionDevice, method, path string, q url.Values, timeout time.Duration) error {
	resp, err := companionDo(context.Background(), dev, companionRequest{Method: method, Path: path, Query: q, Timeout: timeout})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if ackErr := companionAck(resp.Body); ackErr != nil {
		return ackErr
	}
	if resp.StatusCode != http.StatusOK {
		return &companionStatusErr{status: resp.StatusCode}
	}
	return nil
}

// companionStatusErr: the phone answered with an HTTP error status.
type companionStatusErr struct{ status int }

func (e *companionStatusErr) Error() string {
	return fmt.Sprintf("companion device returned status %d", e.status)
}

// ProxyCompanionStream streams a file from the companion device to
// http.ResponseWriter with Range and inline preview support - over any
// route (LAN, Tailscale, tunnel).
func ProxyCompanionStream(dev *CompanionDevice, phonePath string, w http.ResponseWriter, r *http.Request) error {
	q := url.Values{"path": {phonePath}}
	download := r != nil && r.URL.Query().Get("download") == "1"
	if download {
		q.Set("download", "1")
	}
	ctx := context.Background()
	h := http.Header{}
	if r != nil {
		ctx = r.Context()
		// Forward Range for video/media seeking and partial content.
		for _, k := range []string{"Range", "If-Range"} {
			if v := r.Header.Get(k); v != "" {
				h.Set(k, v)
			}
		}
	}
	resp, err := companionDo(ctx, dev, companionRequest{Method: http.MethodGet, Path: "/download", Query: q, Header: h, Timeout: 6 * time.Hour})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			w.Header().Set("Content-Range", cr)
		}
		w.WriteHeader(resp.StatusCode)
		return nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return &companionStatusErr{status: resp.StatusCode}
	}

	// Forward critical media/streaming headers
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if val := resp.Header.Get(k); val != "" {
			w.Header().Set(k, val)
		}
	}
	w.Header().Set("X-Companion-Route", resp.Route)

	fileName := filepath.Base(phonePath)
	disposition := "inline; filename*=utf-8''" + url.PathEscape(fileName)
	if download {
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

// ProxyCompanionFileDelete deletes phonePath on the companion device. The
// error says when the phone couldn't be reached or refused - a move from
// the phone must not look done when the original is still there.
func ProxyCompanionFileDelete(dev *CompanionDevice, phonePath string) error {
	return companionSimple(dev, http.MethodDelete, "/delete", url.Values{"path": {phonePath}}, 30*time.Second)
}

// ProxyCompanionFileRename renames on the companion device, with a copy
// fallback for apps without /rename.
func ProxyCompanionFileRename(dev *CompanionDevice, oldPath, newPath string) error {
	err := companionSimple(dev, http.MethodPost, "/rename", url.Values{"old_path": {oldPath}, "new_path": {newPath}}, 30*time.Second)
	var se *companionStatusErr
	if err == nil || !errors.As(err, &se) || se.status != http.StatusNotFound {
		return err
	}

	// Fallback: download old -> upload new -> delete old
	tmpFile, err := os.CreateTemp("", "nivaroos-comp-rename-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	dl, err := companionDo(context.Background(), dev, companionRequest{Method: http.MethodGet, Path: "/download", Query: url.Values{"path": {oldPath}, "download": {"1"}}, Timeout: 60 * time.Minute})
	if err != nil {
		tmpFile.Close()
		return err
	}
	defer dl.Body.Close()
	if dl.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("failed to read companion file for rename (status %d)", dl.StatusCode)
	}
	if _, err := io.Copy(tmpFile, dl.Body); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	if err := ProxyCompanionUploadFile(dev, tmpName, newPath); err != nil {
		return err
	}
	return ProxyCompanionFileDelete(dev, oldPath)
}

// ProxyCompanionMkdir creates a directory on the companion device
func ProxyCompanionMkdir(dev *CompanionDevice, phonePath string) error {
	err := companionSimple(dev, http.MethodPost, "/mkdir", url.Values{"path": {phonePath}}, 30*time.Second)
	var se *companionStatusErr
	if err == nil || !errors.As(err, &se) || se.status != http.StatusNotFound {
		return err
	}
	// Fallback: upload placeholder into path, which forces parent directory creation on phone
	dummyPath := filepath.Join(phonePath, ".init")
	if err := ProxyCompanionUploadStream(dev, dummyPath, strings.NewReader(""), 0); err != nil {
		return err
	}
	_ = ProxyCompanionFileDelete(dev, dummyPath)
	return nil
}

// ProxyCompanionUploadStream streams content to the companion device at phonePath
func ProxyCompanionUploadStream(dev *CompanionDevice, phonePath string, reader io.Reader, size int64) error {
	return companionUpload(context.Background(), dev, phonePath, reader, size)
}

// companionUpload streams reader to phonePath; size -1 when unknown. The
// phone answers {"success": true} only after the file is fully written.
func companionUpload(ctx context.Context, dev *CompanionDevice, phonePath string, reader io.Reader, size int64) error {
	resp, err := companionDo(ctx, dev, companionRequest{
		Method: http.MethodPost, Path: "/upload", Query: url.Values{"path": {phonePath}},
		Header: http.Header{"Content-Type": {"application/octet-stream"}},
		Body:   reader, Size: size, Timeout: 6 * time.Hour,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if ackErr := companionAck(resp.Body); ackErr != nil {
		return ackErr
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("companion upload failed with status %d", resp.StatusCode)
	}
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

	// 2. A verified direct address (cached; callers may hold companionMu,
	// so wait briefly - the probe finishes in the background)
	pctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if companionDirectEndpoint(pctx, dev) != nil {
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

// probeCompanionConnection fills dev.Connection, dev.Route (and IsOnline)
// for the device list: Connection "lan" on the home network, "remote" away
// from home (Route says whether directly over Tailscale or through the
// tunnel), else "offline".
func probeCompanionConnection(dev *CompanionDevice) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dev.Route = companionCurrentRoute(ctx, dev)
	switch {
	case dev.Route == companionRouteLAN:
		dev.Connection = "lan"
		dev.LastSeen = time.Now()
		dev.IsOnline = true
	case dev.Route != "" || companionOnlineRemotely(dev):
		dev.Connection = "remote"
		dev.IsOnline = true
		if dev.Route == companionRouteTailscale {
			dev.LastSeen = time.Now()
		}
	default:
		dev.Connection = "offline"
		dev.IsOnline = false
	}
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
	// Older apps flagged their locally kept name as the user's on every
	// heartbeat; it only counts where NameSource would (see below).
	if ur, _ := input.CustomProps["user_renamed"].(bool); ur && input.NameSource == "" {
		input.NameSource = companionNameUser
	}
	delete(input.CustomProps, "user_renamed")
	input.Name = strings.TrimSpace(input.Name)

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
		// The server keeps the name. A heartbeat changes it only while no
		// user has named the device - then it follows what the phone reports
		// (its default name, or a name the app still holds from before the
		// server tracked who chose it). A user's name, set from the web or
		// the app, is never overwritten by the phone.
		if !companionUserRenamed(matchedDev) && input.Name != "" && input.Name != matchedDev.Name {
			if err := relocateDeviceFolder(getCompanionStorageBasePath(), companionDevices, matchedDev, input.Name); err != nil {
				// Keep the old name with its folder rather than split them.
				logger.Error("companion: renaming backup folder failed", zap.String("device", matchedDev.ID), zap.Error(err))
			} else {
				matchedDev.Name = input.Name
				if input.NameSource == companionNameUser {
					matchedDev.NameSource, matchedDev.NameUpdatedAt = companionNameUser, now
				}
			}
		} else if !companionUserRenamed(matchedDev) && input.NameSource == companionNameUser && input.Name == matchedDev.Name {
			matchedDev.NameSource, matchedDev.NameUpdatedAt = companionNameUser, now
		}
		if matchedDev.StoragePath == "" {
			assignCompanionFolder(getCompanionStorageBasePath(), companionDevices, matchedDev, matchedDev.Name)
		}
		os.MkdirAll(matchedDev.StoragePath, 0755)

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
		if input.Addresses != nil {
			matchedDev.Addresses = filterCompanionAddresses(input.Addresses)
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
			Addresses:     filterCompanionAddresses(input.Addresses),
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
			NameSource:    companionNameDevice,
		}
		if input.NameSource == companionNameUser {
			// Paired again after a removal: the name the user gave it.
			newDev.NameSource, newDev.NameUpdatedAt = companionNameUser, now
		}
		// Its own folder: a second phone with the same default name gets
		// "Pixel 8 (2)", never the first one's backups.
		assignCompanionFolder(getCompanionStorageBasePath(), companionDevices, newDev, input.Name)
		companionDevices[input.ID] = newDev
	}

	registered := companionDevices[input.ID]
	// This sign-in is now the phone's: removing the phone ends it.
	bindCompanionSessionLocked(registered.ID, companionSessionOf(ctx))
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
	delete(update.CustomProps, "user_renamed")
	update.Name = strings.TrimSpace(update.Name)
	if len(update.Name) > 100 {
		return ctx.JSON(http.StatusBadRequest, model.Result{
			Success: common_err.CLIENT_ERROR,
			Message: "device name is too long (100 characters at most)",
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

	// A rename - from the web or from the app - is the user's: it sticks
	// until the next rename (the phone's heartbeat can't undo it), and the
	// app adopts it from its next registration.
	if update.Name != "" {
		if update.Name != dev.Name {
			if err := relocateDeviceFolder(getCompanionStorageBasePath(), companionDevices, dev, update.Name); err != nil {
				return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
			}
			dev.Name = update.Name
		}
		dev.NameSource, dev.NameUpdatedAt = companionNameUser, time.Now()
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

	if err := saveCompanionDevicesLocked(); err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: "couldn't save the device: " + err.Error()})
	}

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "companion device updated",
		Data:    dev.snapshot(),
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

	// 3. Sign the phone's app out: its sessions end in every service, so
	// it can't keep using the server (or quietly re-register) with the
	// tokens it holds. The user's other sessions are untouched.
	signedOut := len(companionSessions[id]) > 0
	endCompanionSessionsLocked(id)

	// 4. Tell a connected phone it was removed (it signs out at once
	// instead of on its next request), then close its tunnel.
	notifyCompanionRemoved(id)
	closeCompanionTunnel(id)

	return ctx.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: "companion device removed",
		Data:    map[string]interface{}{"kept_backups": keptData, "signed_out": signedOut},
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

	// A folder listing of thousands of files is a big message; stream
	// frames are 64 KiB.
	conn.SetReadLimit(64 << 20)
	defer func() {
		if m := tunnel.streams(); m != nil {
			m.closeAll(errTunnelClosed)
		}
	}()

	for {
		kind, message, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if kind == websocket.BinaryMessage {
			if m := tunnel.streams(); m != nil {
				m.handleFrame(message)
			}
			continue
		}
		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err == nil {
			action, _ := msg["action"].(string)
			reqID, _ := msg["id"].(string)

			if typ, _ := msg["type"].(string); typ == "http_head" {
				if m := tunnel.streams(); m != nil {
					m.handleHead(message)
				}
				continue
			}

			if action == "register" {
				// An app that can carry files through the tunnel says so
				// (streams: 1) with its receive window.
				if v, _ := msg["streams"].(float64); v >= 1 && tunnel.streams() == nil {
					w, _ := msg["window"].(float64)
					tunnel.mux.Store(newTunnelMux(tunnel.sendRaw, int(w)))
				}
				companionMu.Lock()
				if dev, ok := companionDevices[id]; ok {
					if port, ok := msg["port"].(float64); ok && port > 0 && port < 65536 {
						dev.Port = int(port)
					}
					if ip, ok := msg["ip"].(string); ok && companionHasLANIP(ip) {
						dev.IP = ip
					}
					if raw, ok := msg["addresses"].([]interface{}); ok {
						addrs := make([]string, 0, len(raw))
						for _, a := range raw {
							if s, ok := a.(string); ok {
								addrs = append(addrs, s)
							}
						}
						dev.Addresses = filterCompanionAddresses(addrs)
					}
					dev.SharesStorage = true
					dev.IsOnline = true
					dev.LastSeen = time.Now()
				}
				companionMu.Unlock()
				forgetCompanionRoute(id) // addresses may have changed
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

// filterCompanionAddresses keeps the reported addresses the server may
// dial (LAN, Tailscale, MagicDNS), at most 8.
func filterCompanionAddresses(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" || seen[a] || companionAddrKind(a) == "" {
			continue
		}
		seen[a] = true
		out = append(out, a)
		if len(out) == 8 {
			break
		}
	}
	return out
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
	if dev == nil {
		return 0, fmt.Errorf("companion device not found or offline")
	}
	items, err := FetchCompanionFilesFromDevice(dev, phonePath)
	if err == nil && len(items) > 0 {
		var total int64 = 0
		for _, it := range items {
			total += it.Size
		}
		return total, nil
	}
	size, _, err := companionFileSize(ctx, dev, phonePath)
	return size, err
}

// companionFileSize asks the phone for one byte of phonePath: ok is false
// when it isn't a file there.
func companionFileSize(ctx context.Context, dev *CompanionDevice, phonePath string) (int64, bool, error) {
	resp, err := companionDo(ctx, dev, companionRequest{
		Method: http.MethodGet, Path: "/download", Query: url.Values{"path": {phonePath}},
		Header: http.Header{"Range": {"bytes=0-0"}}, Timeout: 15 * time.Second,
	})
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, false, nil
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if idx := strings.LastIndex(cr, "/"); idx != -1 {
			if s, err := strconv.ParseInt(cr[idx+1:], 10, 64); err == nil {
				return s, true, nil
			}
		}
	}
	if resp.ContentLength > 0 {
		return resp.ContentLength, true, nil
	}
	return 0, true, nil
}

func isCompanionFile(dev *CompanionDevice, phonePath string) bool {
	_, ok, _ := companionFileSize(context.Background(), dev, phonePath)
	return ok
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
					// The phone can't be reached at all: say so once
					// instead of once per file.
					var ue *errCompanionUnreachable
					if errors.As(err, &ue) {
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
	resp, err := companionDo(ctx, dev, companionRequest{
		Method: http.MethodGet, Path: "/download", Query: url.Values{"path": {phonePath}, "download": {"1"}}, Timeout: 6 * time.Hour,
	})
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

	var sent int64
	body := &progressReader{r: srcFile, fn: func(n int64) {
		sent += n
		if onProgress != nil {
			onProgress(sent)
		}
	}}
	return companionUpload(ctx, dev, targetPhonePath, body, fileSize)
}

// progressReader reports every read, so a copy to the phone shows progress
// on any route.
type progressReader struct {
	r  io.Reader
	fn func(int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.fn(int64(n))
	}
	return n, err
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
