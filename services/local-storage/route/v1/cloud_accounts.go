package v1

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/gin-gonic/gin"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/rc"
)

// cloudProvider describes one online-account type the "Add Account" UI can
// offer. The actual config fields for "form"/"token" providers come from
// rclone's own backend metadata (fs.Find(Type).Options) via
// GetCloudProviderOptions - not hand-maintained here - so this catalog only
// needs to say which providers we support and how to drive them.
type cloudProvider struct {
	Type     string `json:"type"`      // rclone backend type, e.g. "drive", "s3"
	Label    string `json:"label"`     // display name
	Icon     string `json:"icon"`      // mdi icon name (no "mdi-" prefix)
	AuthKind string `json:"auth_kind"` // "form" | "token" | "interactive"
}

// providerCatalog is the launch set. rclone's engine (already vendored in
// this service) supports far more backends than this via backend/all; this
// list is deliberately curated rather than exhaustive - anything else can be
// added later purely by adding a row here, no plumbing changes.
var providerCatalog = []cloudProvider{
	{Type: "drive", Label: "Google Drive", Icon: "google-drive", AuthKind: "token"},
	{Type: "dropbox", Label: "Dropbox", Icon: "dropbox", AuthKind: "token"},
	{Type: "onedrive", Label: "OneDrive", Icon: "microsoft-onedrive", AuthKind: "token"},
	{Type: "iclouddrive", Label: "iCloud Drive", Icon: "apple", AuthKind: "interactive"},
	{Type: "s3", Label: "S3-Compatible Storage", Icon: "aws", AuthKind: "form"},
	{Type: "b2", Label: "Backblaze B2", Icon: "cloud-outline", AuthKind: "form"},
	{Type: "webdav", Label: "WebDAV", Icon: "server-network", AuthKind: "form"},
	{Type: "sftp", Label: "SFTP", Icon: "server-network", AuthKind: "form"},
	{Type: "smb", Label: "SMB / CIFS Share", Icon: "nas", AuthKind: "form"},
}

var providerIcons = func() map[string]string {
	m := make(map[string]string, len(providerCatalog))
	for _, p := range providerCatalog {
		m[p.Type] = p.Icon
	}
	return m
}()

// cloudProviderIcon returns an mdi icon name for a mounted remote's rclone
// type, falling back to a generic cloud glyph for anything outside the
// curated catalog (e.g. a remote added via a future/rarer backend).
func cloudProviderIcon(rcloneType string) string {
	if icon, ok := providerIcons[rcloneType]; ok {
		return icon
	}
	return "cloud-outline"
}

// GetCloudProviders lists the supported online-account types.
func GetCloudProviders(c *gin.Context) {
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: providerCatalog})
}

// GetCloudProviderOptions returns rclone's own config-field metadata for a
// provider type (name/help/required/is_password/advanced/default), so the
// "Add Account" form is generated from the real backend definition instead
// of a hand-maintained field list.
func GetCloudProviderOptions(c *gin.Context) {
	t := c.Param("type")
	if !isKnownProvider(t) {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "unknown provider type"})
		return
	}
	ri, err := fs.Find(t)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: ri.Options})
}

func isKnownProvider(t string) bool {
	for _, p := range providerCatalog {
		if p.Type == t {
			return true
		}
	}
	return false
}

// remoteName turns a user-supplied label into a unique rclone config
// section name, following the same "<slug>_<type>_<unixts>" convention the
// mount engine (service/storage.go, UmountStorage) already assumes.
func remoteName(label, providerType string) string {
	slug := strings.ToLower(strings.TrimSpace(label))
	var b strings.Builder
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('_')
		}
	}
	slug = strings.Trim(b.String(), "_")
	if slug == "" {
		slug = providerType
	}
	return slug + "_" + providerType + "_" + strconv.FormatInt(time.Now().Unix(), 10)
}

type createAccountRequest struct {
	Type   string            `json:"type" binding:"required"`
	Label  string            `json:"label" binding:"required"`
	Params map[string]string `json:"params"`
}

// PostCloudAccount creates and mounts a new online account for any
// non-interactive provider: the "form" providers (S3/B2/WebDAV/SFTP/SMB)
// with plain credentials, and the "token" providers (Drive/Dropbox/OneDrive)
// via a pasted `rclone authorize <type>` token - both just hand their params
// straight to the existing, generic CreateConfig+MountStorage pipeline.
func PostCloudAccount(c *gin.Context) {
	var req createAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: err.Error()})
		return
	}
	if !isKnownProvider(req.Type) {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "unknown provider type"})
		return
	}
	name := remoteName(req.Label, req.Type)
	mountPoint := "/mnt/" + name

	data := rc.Params{}
	for k, v := range req.Params {
		data[k] = v
	}
	data["username"] = req.Label
	data["mount_point"] = mountPoint

	if err := service.MyService.Storage().CreateConfig(data, name, req.Type); err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}
	if err := service.MyService.Storage().MountStorage(mountPoint, name); err != nil {
		service.MyService.Storage().DeleteConfigByName(name)
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: gin.H{"name": name, "mount_point": mountPoint}})
}

// --- iCloud: rclone's iclouddrive backend needs a real interactive,
// multi-step flow (Apple ID + password, then a 2FA code - sometimes a third
// SMS-fallback step) rather than a single flat form. We drive rclone's own
// generic interactive-config state machine (fs.RegInfo.Config /
// fs.ConfigIn / fs.ConfigOut) directly, which is the same mechanism
// `rclone config create` and rclone's own web GUI use - no bespoke Apple
// API/2FA handling of our own.

type icloudSession struct {
	name   string
	label  string
	m      configmap.Simple
	state  string
	expiry time.Time
}

var (
	icloudSessionsMu sync.Mutex
	icloudSessions   = map[string]*icloudSession{}
)

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func pruneICloudSessions() {
	now := time.Now()
	for id, s := range icloudSessions {
		if now.After(s.expiry) {
			delete(icloudSessions, id)
		}
	}
}

type icloudStepResponse struct {
	SessionID string     `json:"session_id,omitempty"`
	Done      bool       `json:"done"`
	Question  *fs.Option `json:"question,omitempty"`
	Error     string     `json:"error,omitempty"`
	Name      string     `json:"name,omitempty"`
	MountPath string     `json:"mount_point,omitempty"`
}

// runICloudConfig calls the iclouddrive backend's Config function and either
// returns the next question to ask, or - when the flow is complete -
// finishes the account the same way PostCloudAccount does (CreateConfig +
// MountStorage) using whatever fields Config accumulated into m.
func runICloudConfig(c *gin.Context, sess *icloudSession, in fs.ConfigIn) {
	ri, err := fs.Find("iclouddrive")
	if err != nil || ri.Config == nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: "iclouddrive backend is unavailable"})
		return
	}
	out, err := ri.Config(context.Background(), sess.name, sess.m, in)
	if err != nil {
		c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: icloudStepResponse{Error: err.Error()}})
		return
	}
	if out == nil || out.State == "" {
		// Flow finished - m now holds everything the backend needs
		// (cookies/trust token/etc alongside apple_id). Finish the account
		// exactly like the non-interactive path does.
		mountPoint := "/mnt/" + sess.name
		data := rc.Params{"username": sess.label, "mount_point": mountPoint}
		for k, v := range sess.m {
			data[k] = v
		}
		if err := service.MyService.Storage().CreateConfig(data, sess.name, "iclouddrive"); err != nil {
			c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
			return
		}
		if err := service.MyService.Storage().MountStorage(mountPoint, sess.name); err != nil {
			service.MyService.Storage().DeleteConfigByName(sess.name)
			c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
			return
		}
		icloudSessionsMu.Lock()
		delete(icloudSessions, sess.name)
		icloudSessionsMu.Unlock()
		c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: icloudStepResponse{Done: true, Name: sess.name, MountPath: mountPoint}})
		return
	}
	if out.Error != "" {
		c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: icloudStepResponse{SessionID: sess.name, Error: out.Error}})
		return
	}
	sess.state = out.State
	sess.expiry = time.Now().Add(10 * time.Minute)
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: icloudStepResponse{SessionID: sess.name, Question: out.Option}})
}

type icloudStartRequest struct {
	Label    string `json:"label" binding:"required"`
	AppleID  string `json:"apple_id" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// PostICloudStart begins the iCloud interactive flow: Apple ID + password.
// Typically returns a "2fa_do" question for PostICloudVerify.
func PostICloudStart(c *gin.Context) {
	var req icloudStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: err.Error()})
		return
	}
	name := remoteName(req.Label, "iclouddrive")
	sess := &icloudSession{
		name:  name,
		label: req.Label,
		m:     configmap.Simple{"apple_id": req.AppleID, "password": req.Password},
	}
	icloudSessionsMu.Lock()
	pruneICloudSessions()
	icloudSessions[name] = sess
	icloudSessionsMu.Unlock()

	runICloudConfig(c, sess, fs.ConfigIn{State: ""})
}

type icloudVerifyRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
}

// PostICloudVerify continues the flow with the 2FA code (or "sms" to
// request a text-message code instead, per the backend's own support for
// that).
func PostICloudVerify(c *gin.Context) {
	var req icloudVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: err.Error()})
		return
	}
	icloudSessionsMu.Lock()
	sess, ok := icloudSessions[req.SessionID]
	icloudSessionsMu.Unlock()
	if !ok {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: fmt.Sprintf("no such session %q (it may have expired - start again)", req.SessionID)})
		return
	}
	runICloudConfig(c, sess, fs.ConfigIn{State: sess.state, Result: req.Code})
}
