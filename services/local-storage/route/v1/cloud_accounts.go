package v1

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/gin-gonic/gin"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/operations"
	"github.com/rclone/rclone/fs/rc"
	"go.uber.org/zap"
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
	name      string
	label     string
	reconnect bool
	m         configmap.Simple
	state     string
	expiry    time.Time
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
		if sess.reconnect {
			if err := service.MyService.Storage().UnmountStorage(mountPoint); err != nil {
				logger.Error("reconnect: failed to unmount before remounting", zap.Error(err), zap.String("name", sess.name))
			}
			time.Sleep(500 * time.Millisecond)
		}
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
	// Name, if set, reconnects an existing account in place (re-runs sign-in
	// against its current remote name) instead of creating a new one.
	Name string `json:"name"`
}

// PostICloudStart begins the iCloud interactive flow: Apple ID + password.
// Typically returns a "2fa_do" question for PostICloudVerify.
func PostICloudStart(c *gin.Context) {
	var req icloudStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: err.Error()})
		return
	}
	name := req.Name
	if name == "" {
		name = remoteName(req.Label, "iclouddrive")
	}
	// The iclouddrive backend's own Config function expects an
	// already-obscured password (config.Reveal()s it immediately) - that's
	// normally done automatically by rclone's interactive config machinery
	// before a value ever reaches a backend; since we're seeding this
	// configmap ourselves, we have to obscure it the same way first.
	obscuredPassword, err := obscure.Obscure(req.Password)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}
	m := configmap.Simple{"apple_id": req.AppleID, "password": obscuredPassword}
	// A bare configmap.Simple has no knowledge of the backend's own declared
	// option defaults (that's normally layered in separately by rclone's
	// config-file/flag machinery - see fs.ConfigMap - which we're
	// deliberately not using here since its setter writes straight through
	// to the real rclone.conf on every m.Set(), before we've decided the
	// flow is actually done). Without this, client_id comes back empty and
	// Apple's own authorize/signin endpoint rejects it outright with a bare
	// "HTTP error 400 ... body: \"\"" - seed the declared defaults for
	// anything the caller didn't already supply.
	if ri, err := fs.Find("iclouddrive"); err == nil {
		for _, opt := range ri.Options {
			if _, ok := m[opt.Name]; ok {
				continue
			}
			if opt.Default == nil {
				continue
			}
			if s := fmt.Sprint(opt.Default); s != "" {
				m[opt.Name] = s
			}
		}
	}
	sess := &icloudSession{
		name:      name,
		label:     req.Label,
		reconnect: req.Name != "",
		m:         m,
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

// --- Manage an existing account: rename, reconnect, and a quick speed test.

type renameAccountRequest struct {
	Label string `json:"label" binding:"required"`
}

// PatchCloudAccount renames an account's display label - the only thing
// "editing" an account safely means without re-running its whole auth flow.
func PatchCloudAccount(c *gin.Context) {
	name := c.Param("name")
	var req renameAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: err.Error()})
		return
	}
	if service.MyService.Storage().GetAttributeValueByName(name, "type") == "" {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "no such account"})
		return
	}
	if err := service.MyService.Storage().SetAttributeValue(name, "username", req.Label, false); err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: gin.H{"name": name, "label": req.Label}})
}

type reconnectAccountRequest struct {
	Params map[string]string `json:"params" binding:"required"`
}

// PostCloudAccountReconnect replaces credentials on an existing account
// in place (a fresh token pasted after re-running `rclone authorize`, or
// updated server details for a form-kind account) without losing its name,
// mount point, or display label. Used for token/form-kind providers; iCloud
// has its own reconnect via PostICloudStart with an existing name.
func PostCloudAccountReconnect(c *gin.Context) {
	name := c.Param("name")
	t := service.MyService.Storage().GetAttributeValueByName(name, "type")
	if t == "" {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "no such account"})
		return
	}
	var req reconnectAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: err.Error()})
		return
	}

	ri, err := fs.Find(t)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}
	isPassword := make(map[string]bool, len(ri.Options))
	for _, opt := range ri.Options {
		isPassword[opt.Name] = opt.IsPassword
	}

	mountPoint := "/mnt/" + name
	if err := service.MyService.Storage().UnmountStorage(mountPoint); err != nil {
		logger.Error("reconnect: failed to unmount before remounting", zap.Error(err), zap.String("name", name))
	}
	time.Sleep(500 * time.Millisecond)

	for k, v := range req.Params {
		if err := service.MyService.Storage().SetAttributeValue(name, k, v, isPassword[k]); err != nil {
			c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
			return
		}
	}

	if err := service.MyService.Storage().MountStorage(mountPoint, name); err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: gin.H{"name": name, "mount_point": mountPoint}})
}

// PostCloudAccountSpeedTest uploads a small random test file, times the
// upload, reads it back timed, then deletes it - a quick real-world
// throughput check against the actual remote, not a synthetic benchmark.
func PostCloudAccountSpeedTest(c *gin.Context) {
	name := c.Param("name")
	if service.MyService.Storage().GetAttributeValueByName(name, "type") == "" {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "no such account"})
		return
	}

	ctx := context.Background()
	f, err := fs.NewFs(ctx, name+":")
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}

	const testSize = 8 * 1024 * 1024 // 8MB - enough for a meaningful number, small enough to be quick
	payload := make([]byte, testSize)
	if _, err := rand.Read(payload); err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		return
	}
	testFileName := ".nivaroos-speedtest-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	uploadStart := time.Now()
	obj, err := operations.Rcat(ctx, f, testFileName, io.NopCloser(bytes.NewReader(payload)), time.Now(), nil)
	uploadElapsed := time.Since(uploadStart)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: fmt.Sprintf("upload failed: %v", err)})
		return
	}
	defer func() {
		if err := obj.Remove(ctx); err != nil {
			logger.Error("speedtest: failed to remove test file", zap.Error(err), zap.String("name", name))
		}
	}()

	downloadStart := time.Now()
	rc, err := obj.Open(ctx)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: fmt.Sprintf("download failed: %v", err)})
		return
	}
	written, err := io.Copy(io.Discard, rc)
	rc.Close()
	downloadElapsed := time.Since(downloadStart)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: fmt.Sprintf("download failed: %v", err)})
		return
	}

	mbpsUp := float64(testSize) / uploadElapsed.Seconds() / (1024 * 1024)
	mbpsDown := float64(written) / downloadElapsed.Seconds() / (1024 * 1024)

	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: gin.H{
		"upload_mbps":      mbpsUp,
		"download_mbps":    mbpsDown,
		"upload_seconds":   uploadElapsed.Seconds(),
		"download_seconds": downloadElapsed.Seconds(),
		"test_size_bytes":  testSize,
	}})
}
