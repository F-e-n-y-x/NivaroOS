package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

// useTempCompanionState points every companion file (device list, secrets,
// backup folders) at a temp dir and starts with an empty, unloaded device
// map, so tests never read or write this machine's real state.
func useTempCompanionState(t *testing.T) (state, base string) {
	t.Helper()
	logger.LogInitConsoleOnly()
	root := t.TempDir()
	state = filepath.Join(root, "state")
	base = filepath.Join(root, "DATA", "Companion")
	os.MkdirAll(filepath.Dir(base), 0o755)

	companionMu.Lock()
	oldState, oldBase, oldNoData := companionStateDir, companionBaseDir, companionBaseNoDATA
	oldDevs, oldLoaded, oldKept := companionDevices, companionLoaded, companionKeptFolders
	companionStateDir, companionBaseDir, companionBaseNoDATA = state, base, filepath.Join(root, "nodata")
	companionDevices = map[string]*CompanionDevice{}
	companionKeptFolders = map[string]companionKeptFolder{}
	companionLoaded = false
	companionMu.Unlock()

	t.Cleanup(func() {
		companionMu.Lock()
		companionStateDir, companionBaseDir, companionBaseNoDATA = oldState, oldBase, oldNoData
		companionDevices, companionLoaded, companionKeptFolders = oldDevs, oldLoaded, oldKept
		companionMu.Unlock()
	})
	return state, base
}

// companionReq runs handler as user uid (0 = same-host automation, no JWT).
func companionReq(t *testing.T, h echo.HandlerFunc, method, target string, uid int, id string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, rd)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	if uid != 0 {
		c.Set("user", &jwt.Claims{ID: uid, Username: "u"})
	}
	if id != "" {
		c.SetParamNames("id")
		c.SetParamValues(id)
	}
	if err := h(c); err != nil {
		t.Fatal(err)
	}
	return rec
}

func register(t *testing.T, uid int, dto CompanionRegistrationDTO) *httptest.ResponseRecorder {
	t.Helper()
	return companionReq(t, PostRegisterCompanionDevice, http.MethodPost, "/v1/companion/register", uid, "", dto)
}

// registerWithSecret registers as the app does: with the file-server
// secret it holds in X-Companion-Secret ("" = none).
func registerWithSecret(t *testing.T, uid int, secret string, dto CompanionRegistrationDTO) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(dto)
	req := httptest.NewRequest(http.MethodPost, "/v1/companion/register", bytes.NewReader(b))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if secret != "" {
		req.Header.Set("X-Companion-Secret", secret)
	}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	if uid != 0 {
		c.Set("user", &jwt.Claims{ID: uid})
	}
	if err := PostRegisterCompanionDevice(c); err != nil {
		t.Fatal(err)
	}
	return rec
}

func secretOf(rec *httptest.ResponseRecorder) string {
	var res struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	return res.Data.Secret
}

func listIDs(t *testing.T, uid int) []string {
	t.Helper()
	rec := companionReq(t, GetCompanionDevices, http.MethodGet, "/v1/companion/devices", uid, "", nil)
	var res struct {
		Data []CompanionDevice `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("list: %v: %s", err, rec.Body.String())
	}
	ids := []string{}
	for _, d := range res.Data {
		ids = append(ids, d.ID)
	}
	return ids
}

func has(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// S-02: two phones with the same private LAN address (different home
// networks, or a reused DHCP lease) and two identical phones with default
// names used to delete each other.
func TestRegisterKeepsDevicesWithSameIPOrSameModelAndName(t *testing.T) {
	_, base := useTempCompanionState(t)
	a := CompanionRegistrationDTO{ID: "dev_a", Name: "Pixel 8", Model: "Pixel 8", IP: "192.168.1.23"}
	b := CompanionRegistrationDTO{ID: "dev_b", Name: "Pixel 8", Model: "Pixel 8", IP: "192.168.1.23"}
	for _, d := range []CompanionRegistrationDTO{a, b, a, b} {
		if rec := register(t, 1, d); rec.Code != http.StatusOK {
			t.Fatalf("register %s: %d %s", d.ID, rec.Code, rec.Body.String())
		}
	}
	ids := listIDs(t, 1)
	if len(ids) != 2 || !has(ids, "dev_a") || !has(ids, "dev_b") {
		t.Fatalf("want both devices, got %v", ids)
	}
	companionMu.Lock()
	pa, pb := companionDevices["dev_a"].StoragePath, companionDevices["dev_b"].StoragePath
	companionMu.Unlock()
	if pa == pb {
		t.Fatalf("both devices share backup folder %s", pa)
	}
	if pa != filepath.Join(base, "Pixel 8") || pb != filepath.Join(base, "Pixel 8 (2)") {
		t.Fatalf("folders %q %q", pa, pb)
	}

	// And they survive a reload from disk.
	companionMu.Lock()
	companionDevices = map[string]*CompanionDevice{}
	companionLoaded = false
	companionMu.Unlock()
	if ids := listIDs(t, 1); len(ids) != 2 {
		t.Fatalf("after reload: %v", ids)
	}
}

func TestRegisterRequiresADeviceID(t *testing.T) {
	useTempCompanionState(t)
	if rec := register(t, 1, CompanionRegistrationDTO{Name: "x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestRegisterNeverStoresTheSecretInCustomProps(t *testing.T) {
	useTempCompanionState(t)
	register(t, 1, CompanionRegistrationDTO{ID: "d", Name: "P", CustomProps: map[string]interface{}{"secret": "s3cret", "x": 1}})
	rec := companionReq(t, GetCompanionDevices, http.MethodGet, "/v1/companion/devices", 1, "", nil)
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatalf("secret leaked in the device list: %s", rec.Body.String())
	}
}

// The one cleanup that stays: the same id written twice to the file.
func TestMergeDuplicateCompanionDevices(t *testing.T) {
	now := time.Now()
	list := []*CompanionDevice{
		{ID: "a", Name: "Pixel", IP: "192.168.1.5", StoragePath: "/c/Pixel", LastSeen: now.Add(-time.Hour), CreatedAt: now.Add(-48 * time.Hour), OwnerUserID: "1"},
		{ID: "b", Name: "Pixel", Model: "Pixel", IP: "192.168.1.5", StoragePath: "/c/Pixel (2)"},
		{ID: " a ", Name: "Pixel", IP: "10.0.0.2", LastSeen: now},
		{ID: ""},
		nil,
	}
	out := mergeDuplicateCompanionDevices(list)
	if len(out) != 2 || out[0].ID != "a" || out[1].ID != "b" {
		t.Fatalf("got %+v", out)
	}
	a := out[0]
	if a.IP != "10.0.0.2" || a.StoragePath != "/c/Pixel" || a.OwnerUserID != "1" || !a.CreatedAt.Equal(now.Add(-48*time.Hour)) {
		t.Fatalf("merge kept the wrong fields: %+v", a)
	}

	// A user's rename beats the default name of a newer duplicate.
	out = mergeDuplicateCompanionDevices([]*CompanionDevice{
		{ID: "x", Name: "Work phone", StoragePath: "/c/Work phone", CustomProps: map[string]interface{}{"user_renamed": true}},
		{ID: "x", Name: "Pixel", StoragePath: "/c/Pixel", LastSeen: now},
	})
	if len(out) != 1 || out[0].Name != "Work phone" || out[0].StoragePath != "/c/Work phone" {
		t.Fatalf("got %+v", out[0])
	}
}

// Old companion_devices.json files (no owner field, duplicate ids) load.
func TestLoadLegacyCompanionFile(t *testing.T) {
	state, _ := useTempCompanionState(t)
	os.MkdirAll(state, 0o755)
	legacy := `[
	 {"id":"p1","name":"Pixel","ip":"192.168.1.23","storage_path":"/x/Pixel","last_seen":"2026-01-01T00:00:00Z"},
	 {"id":"p2","name":"Pixel","ip":"192.168.1.23","storage_path":"/x/Pixel (2)"},
	 {"id":"p1","name":"Pixel","ip":"192.168.1.40","last_seen":"2026-02-01T00:00:00Z","connection":"lan"}
	]`
	os.WriteFile(filepath.Join(state, "companion_devices.json"), []byte(legacy), 0o644)
	companionMu.Lock()
	loadCompanionDevicesLocked()
	defer companionMu.Unlock()
	if len(companionDevices) != 2 {
		t.Fatalf("want 2 devices, got %d", len(companionDevices))
	}
	p1 := companionDevices["p1"]
	if p1.IP != "192.168.1.40" || p1.StoragePath != "/x/Pixel" || p1.OwnerUserID != "" || p1.Connection != "" {
		t.Fatalf("p1 = %+v", p1)
	}
}

// S-03: devices belong to the user who registered them.
func TestCompanionDevicesAreScopedToTheirOwner(t *testing.T) {
	useTempCompanionState(t)
	if rec := register(t, 1, CompanionRegistrationDTO{ID: "alice_phone", Name: "Alice", IP: "192.168.1.2"}); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if rec := register(t, 2, CompanionRegistrationDTO{ID: "bob_phone", Name: "Bob", IP: "192.168.1.3"}); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	companionMu.Lock()
	owner := companionDevices["alice_phone"].OwnerUserID
	companionMu.Unlock()
	if owner != "1" {
		t.Fatalf("owner_user_id = %q", owner)
	}

	if ids := listIDs(t, 2); has(ids, "alice_phone") || !has(ids, "bob_phone") {
		t.Fatalf("user 2 sees %v", ids)
	}
	if ids := listIDs(t, 0); len(ids) != 2 {
		t.Fatalf("same-host automation should see every device, got %v", ids)
	}

	// Every per-device endpoint answers "not found" to another user.
	for name, run := range map[string]func() *httptest.ResponseRecorder{
		"rename": func() *httptest.ResponseRecorder {
			return companionReq(t, PutUpdateCompanionDevice, http.MethodPut, "/", 2, "alice_phone", map[string]string{"name": "mine"})
		},
		"delete": func() *httptest.ResponseRecorder {
			return companionReq(t, DeleteCompanionDevice, http.MethodDelete, "/", 2, "alice_phone", nil)
		},
		"storage": func() *httptest.ResponseRecorder {
			return companionReq(t, GetCompanionDeviceStorage, http.MethodGet, "/", 2, "alice_phone", nil)
		},
		"browse": func() *httptest.ResponseRecorder {
			return companionReq(t, GetCompanionDeviceFiles, http.MethodGet, "/?path=/", 2, "alice_phone", nil)
		},
		"download": func() *httptest.ResponseRecorder {
			return companionReq(t, GetCompanionDeviceDownload, http.MethodGet, "/?path=/x", 2, "alice_phone", nil)
		},
		"ws": func() *httptest.ResponseRecorder {
			return companionReq(t, GetCompanionDeviceWS, http.MethodGet, "/", 2, "alice_phone", nil)
		},
		"upload": func() *httptest.ResponseRecorder {
			var body bytes.Buffer
			mw := multipart.NewWriter(&body)
			fw, _ := mw.CreateFormFile("file", "a.txt")
			fw.Write([]byte("x"))
			mw.Close()
			req := httptest.NewRequest(http.MethodPost, "/", &body)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(req, rec)
			c.Set("user", &jwt.Claims{ID: 2})
			c.SetParamNames("id")
			c.SetParamValues("alice_phone")
			if err := PostCompanionDeviceUpload(c); err != nil {
				t.Fatal(err)
			}
			return rec
		},
	} {
		if rec := run(); rec.Code != http.StatusNotFound {
			t.Errorf("%s by another user: status %d %s", name, rec.Code, rec.Body.String())
		}
	}
	companionMu.Lock()
	_, still := companionDevices["alice_phone"]
	name := companionDevices["alice_phone"].Name
	companionMu.Unlock()
	if !still || name != "Alice" {
		t.Fatal("another user changed the device")
	}

	// Registering someone else's device id doesn't take it over.
	if rec := register(t, 2, CompanionRegistrationDTO{ID: "alice_phone", Name: "Mine now"}); rec.Code != http.StatusForbidden {
		t.Fatalf("takeover: status %d", rec.Code)
	}
	// The owner still can.
	if rec := companionReq(t, PutUpdateCompanionDevice, http.MethodPut, "/", 1, "alice_phone", map[string]string{"name": "Alice's Pixel"}); rec.Code != http.StatusOK {
		t.Fatalf("owner rename: %d %s", rec.Code, rec.Body.String())
	}
	if rec := companionReq(t, DeleteCompanionDevice, http.MethodDelete, "/", 1, "alice_phone", nil); rec.Code != http.StatusOK {
		t.Fatalf("owner delete: %d", rec.Code)
	}
}

// Migration: a device saved before owners existed is claimed by the next
// authenticated heartbeat from it, once.
func TestLegacyDeviceIsClaimedByItsNextRegister(t *testing.T) {
	useTempCompanionState(t)
	companionMu.Lock()
	companionLoaded = true
	companionDevices["old"] = &CompanionDevice{ID: "old", Name: "Old phone", IP: "192.168.1.9"}
	companionMu.Unlock()

	// Unclaimed: still visible to everyone, as before.
	if !has(listIDs(t, 1), "old") || !has(listIDs(t, 2), "old") {
		t.Fatal("legacy device should be visible until claimed")
	}
	// Same-host automation registering it doesn't claim it (it is given
	// the secret the server now mints for it, as the phone would be).
	secret := secretOf(register(t, 0, CompanionRegistrationDTO{ID: "old", Name: "Old phone"}))
	companionMu.Lock()
	owner := companionDevices["old"].OwnerUserID
	companionMu.Unlock()
	if owner != "" || secret == "" {
		t.Fatalf("claimed by automation: %q (secret %q)", owner, secret)
	}

	for i := 0; i < 2; i++ { // idempotent
		if rec := registerWithSecret(t, 1, secret, CompanionRegistrationDTO{ID: "old", Name: "Old phone"}); rec.Code != http.StatusOK {
			t.Fatal(rec.Body.String())
		}
	}
	companionMu.Lock()
	owner = companionDevices["old"].OwnerUserID
	companionMu.Unlock()
	if owner != "1" {
		t.Fatalf("owner = %q", owner)
	}
	if has(listIDs(t, 2), "old") {
		t.Fatal("claimed device still visible to another user")
	}
	if rec := register(t, 2, CompanionRegistrationDTO{ID: "old"}); rec.Code != http.StatusForbidden {
		t.Fatalf("second claim: %d", rec.Code)
	}

	// Persisted: survives a restart.
	companionMu.Lock()
	companionDevices = map[string]*CompanionDevice{}
	companionLoaded = false
	loadCompanionDevicesLocked()
	owner = companionDevices["old"].OwnerUserID
	companionMu.Unlock()
	if owner != "1" {
		t.Fatalf("owner after reload = %q", owner)
	}
}

// A phone removed and paired again finds its kept backups, and so does a
// new phone of the same user; a folder whose owner isn't known is never
// handed to a new phone.
func TestReRegisteredPhoneReusesItsKeptFolder(t *testing.T) {
	state, base := useTempCompanionState(t)
	folder := func(id string) string {
		companionMu.Lock()
		defer companionMu.Unlock()
		return companionDevices[id].StoragePath
	}
	register(t, 1, CompanionRegistrationDTO{ID: "p", Name: "Pixel"})
	if got := folder("p"); got != filepath.Join(base, "Pixel") {
		t.Fatalf("folder %q", got)
	}
	if rec := companionReq(t, DeleteCompanionDevice, http.MethodDelete, "/", 1, "p", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	// The record survives a restart.
	companionMu.Lock()
	companionDevices, companionKeptFolders, companionLoaded = map[string]*CompanionDevice{}, map[string]companionKeptFolder{}, false
	loadCompanionDevicesLocked()
	companionMu.Unlock()
	register(t, 1, CompanionRegistrationDTO{ID: "p", Name: "Pixel"})
	if got := folder("p"); got != filepath.Join(base, "Pixel") {
		t.Fatalf("re-paired phone got %q, not its kept folder", got)
	}

	// Same user, new phone (app reinstalled: new id), same name.
	companionReq(t, DeleteCompanionDevice, http.MethodDelete, "/", 1, "p", nil)
	register(t, 1, CompanionRegistrationDTO{ID: "p2", Name: "Pixel"})
	if got := folder("p2"); got != filepath.Join(base, "Pixel") {
		t.Fatalf("same user's new phone got %q", got)
	}

	// A folder left from before the records existed: owner unknown.
	os.MkdirAll(filepath.Join(base, "Galaxy"), 0o755)
	register(t, 1, CompanionRegistrationDTO{ID: "g", Name: "Galaxy"})
	if got := folder("g"); got != filepath.Join(base, "Galaxy (2)") {
		t.Fatalf("unrecorded existing folder handed out: %q", got)
	}
	if _, err := os.Stat(filepath.Join(state, "companion_kept_folders.json")); err != nil {
		t.Fatal(err)
	}
}

// Review finding 1 (fixed): a legacy phone is claimed by a register that
// proves it is that phone - it carries the secret the phone was given.
func TestLegacyDeviceWithSecretIsClaimedOnlyWithProof(t *testing.T) {
	useTempCompanionState(t)
	companionMu.Lock()
	companionLoaded = true
	companionDevices["old"] = &CompanionDevice{ID: "old", Name: "Old phone", IP: "192.168.1.9", Secret: "phone-secret"}
	companionMu.Unlock()

	withSecret := func(uid int, secret string, dto CompanionRegistrationDTO) *httptest.ResponseRecorder {
		return registerWithSecret(t, uid, secret, dto)
	}

	// A wrong secret proves nothing, and doesn't replace the real one.
	if rec := withSecret(2, "guess", CompanionRegistrationDTO{ID: "old", IP: "192.168.1.66"}); rec.Code != http.StatusOK || secretOf(rec) != "" {
		t.Fatalf("wrong secret: %d %q", rec.Code, secretOf(rec))
	}
	companionMu.Lock()
	d := companionDevices["old"]
	owner, ip, sec := d.OwnerUserID, d.IP, d.Secret
	companionMu.Unlock()
	if owner != "" || ip != "192.168.1.9" || sec != "phone-secret" {
		t.Fatalf("after wrong secret: owner=%q ip=%q secret=%q", owner, ip, sec)
	}
	// The unowned device's tunnel isn't open to users either.
	if rec := companionReq(t, GetCompanionDeviceWS, http.MethodGet, "/", 2, "old", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("tunnel of an unclaimed device: %d", rec.Code)
	}
	// Nor is deleting its backups.
	if rec := companionReq(t, DeleteCompanionDevice, http.MethodDelete, "/?delete_data=true", 2, "old", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("delete_data of an unclaimed device: %d", rec.Code)
	}

	// The phone itself, with its secret, claims it and keeps its secret.
	rec := withSecret(1, "phone-secret", CompanionRegistrationDTO{ID: "old", Name: "Old phone", IP: "192.168.1.10"})
	if rec.Code != http.StatusOK || secretOf(rec) != "phone-secret" {
		t.Fatalf("proof: %d %q", rec.Code, secretOf(rec))
	}
	companionMu.Lock()
	owner, ip = companionDevices["old"].OwnerUserID, companionDevices["old"].IP
	companionMu.Unlock()
	if owner != "1" || ip != "192.168.1.10" {
		t.Fatalf("after proof: owner=%q ip=%q", owner, ip)
	}
}

// A legacy device the server holds no secret for has nothing to prove
// against (and no secret to leak): the first user register claims it and
// gets a freshly minted secret, never one it sent.
func TestLegacyDeviceWithoutSecretIsClaimedWithAFreshSecret(t *testing.T) {
	useTempCompanionState(t)
	companionMu.Lock()
	companionLoaded = true
	companionDevices["old"] = &CompanionDevice{ID: "old", Name: "Old phone", IP: "192.168.1.9"}
	companionMu.Unlock()
	rec := registerWithSecret(t, 1, "chosen-by-caller", CompanionRegistrationDTO{ID: "old", Name: "Old phone"})
	companionMu.Lock()
	owner, sec := companionDevices["old"].OwnerUserID, companionDevices["old"].Secret
	companionMu.Unlock()
	if rec.Code != http.StatusOK || owner != "1" || sec == "" || sec == "chosen-by-caller" || secretOf(rec) != sec {
		t.Fatalf("code=%d owner=%q secret=%q returned=%q", rec.Code, owner, sec, secretOf(rec))
	}
}

// A caller never chooses a device's file-server secret.
func TestRegisterMintsTheSecret(t *testing.T) {
	useTempCompanionState(t)
	b, _ := json.Marshal(CompanionRegistrationDTO{ID: "n", Name: "N"})
	req := httptest.NewRequest(http.MethodPost, "/v1/companion/register", bytes.NewReader(b))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("X-Companion-Secret", "chosen")
	c := echo.New().NewContext(req, httptest.NewRecorder())
	c.Set("user", &jwt.Claims{ID: 1})
	if err := PostRegisterCompanionDevice(c); err != nil {
		t.Fatal(err)
	}
	companionMu.Lock()
	sec := companionDevices["n"].Secret
	companionMu.Unlock()
	if sec == "" || sec == "chosen" {
		t.Fatalf("secret %q", sec)
	}
}

// S-04: a phone that is online but not on the server's network can be
// listed through the tunnel but its files can't be opened - say so.
func TestCompanionDownloadOffLANSaysWhy(t *testing.T) {
	useTempCompanionState(t)
	companionMu.Lock()
	companionLoaded = true
	companionDevices["remote"] = &CompanionDevice{ID: "remote", Name: "Anna's Pixel", IP: "", LastSeen: time.Now(), OwnerUserID: "1"}
	companionDevices["gone"] = &CompanionDevice{ID: "gone", Name: "Old", IP: "", LastSeen: time.Now().Add(-2 * time.Minute), OwnerUserID: "1"}
	companionMu.Unlock()

	rec := companionReq(t, GetCompanionDeviceDownload, http.MethodGet, "/?path=/storage/emulated/0/DCIM/a.jpg", 1, "remote", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
	var res struct{ Message string }
	json.Unmarshal(rec.Body.Bytes(), &res)
	want := "Anna's Pixel isn't on the same network as the server - open it from the phone, or when both are on the same network"
	if res.Message != want {
		t.Fatalf("message %q", res.Message)
	}

	rec = companionReq(t, GetCompanionDeviceDownload, http.MethodGet, "/?path=/storage/emulated/0/a.jpg", 1, "gone", nil)
	json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(res.Message, "isn't on the same network") {
		t.Fatalf("offline phone: %d %q", rec.Code, res.Message)
	}
}

func TestCompanionDirectErr(t *testing.T) {
	online := &CompanionDevice{ID: "on", Name: "P", LastSeen: time.Now()}
	offline := &CompanionDevice{ID: "off", Name: "P"}
	dial := errors.New("dial tcp 192.168.1.5:8765: i/o timeout")
	var nl *errCompanionNotOnLAN
	if err := companionDirectErr(online, dial); !errors.As(err, &nl) || !errors.Is(err, dial) {
		t.Fatalf("online phone: %v", err)
	}
	if err := companionDirectErr(offline, dial); errors.As(err, &nl) {
		t.Fatalf("offline phone: %v", err)
	}
	if companionDirectErr(online, nil) != nil {
		t.Fatal("nil error changed")
	}
	// Upload to a phone without a LAN address.
	if err := ProxyCompanionUploadStream(online, "/storage/emulated/0/a", strings.NewReader("x"), 1); !errors.As(err, &nl) {
		t.Fatalf("upload: %v", err)
	}
}

// Local fallback download: only this device's backup folder, not another
// phone's under the same base.
func TestCompanionBackupDownloadStaysInTheDevicesFolder(t *testing.T) {
	_, base := useTempCompanionState(t)
	mine, theirs := filepath.Join(base, "Mine"), filepath.Join(base, "Theirs")
	writeBackup(t, mine, "a.jpg")
	writeBackup(t, theirs, "b.jpg")
	companionMu.Lock()
	companionLoaded = true
	companionDevices["m"] = &CompanionDevice{ID: "m", Name: "Mine", StoragePath: mine, OwnerUserID: "1"}
	companionDevices["t"] = &CompanionDevice{ID: "t", Name: "Theirs", StoragePath: theirs, OwnerUserID: "2"}
	companionMu.Unlock()
	if rec := companionReq(t, GetCompanionDeviceDownload, http.MethodGet, "/?path="+filepath.Join(mine, "a.jpg"), 1, "m", nil); rec.Code != http.StatusOK {
		t.Fatalf("own backup: %d", rec.Code)
	}
	if rec := companionReq(t, GetCompanionDeviceDownload, http.MethodGet, "/?path="+filepath.Join(theirs, "b.jpg"), 1, "m", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("other device's backup through mine: %d", rec.Code)
	}
}

// The reverse tunnel: owner-only, writes serialised, answers only from
// the phone they were sent to, and success:false is an error (not an empty
// folder).
func TestCompanionTunnelList(t *testing.T) {
	useTempCompanionState(t)
	companionMu.Lock()
	companionLoaded = true
	companionDevices["p"] = &CompanionDevice{ID: "p", Name: "P", OwnerUserID: "1"}
	companionDevices["q"] = &CompanionDevice{ID: "q", Name: "Q", OwnerUserID: "1"}
	companionMu.Unlock()

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user", &jwt.Claims{ID: 1})
			return next(c)
		}
	})
	e.GET("/v1/companion/devices/:id/ws", GetCompanionDeviceWS)
	srv := httptest.NewServer(e)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/companion/devices/"

	phone := func(id string, answer func(req map[string]interface{}) map[string]interface{}) *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL+id+"/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			for {
				var req map[string]interface{}
				if err := conn.ReadJSON(&req); err != nil {
					return
				}
				if res := answer(req); res != nil {
					conn.WriteJSON(res)
				}
			}
		}()
		return conn
	}
	cp := phone("p", func(req map[string]interface{}) map[string]interface{} {
		if req["path"] == "/storage/emulated/0/a.jpg" {
			return map[string]interface{}{"id": req["id"], "success": false, "is_file": true, "message": "Path is a file", "files": []interface{}{}}
		}
		return map[string]interface{}{"id": req["id"], "success": true, "files": []interface{}{map[string]interface{}{"name": "DCIM", "is_dir": true}}}
	})
	defer cp.Close()
	var stolen []string
	cq := phone("q", func(req map[string]interface{}) map[string]interface{} {
		stolen = append(stolen, req["id"].(string))
		return nil
	})
	defer cq.Close()

	deadline := time.Now().Add(3 * time.Second)
	for (companionTunnelFor("p") == nil || companionTunnelFor("q") == nil) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	companionMu.Lock()
	p := companionDevices["p"]
	companionMu.Unlock()

	items, err := FetchCompanionFilesFromDevice(p, "/storage/emulated/0")
	if err != nil || len(items) != 1 || items[0].Name != "DCIM" {
		t.Fatalf("list: %v %v", items, err)
	}
	if _, err := FetchCompanionFilesFromDevice(p, "/storage/emulated/0/a.jpg"); err == nil {
		t.Fatal("a file listed as an empty folder")
	}

	// Another user can't open (or replace) the tunnel.
	h := http.Header{}
	e2 := echo.New()
	e2.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user", &jwt.Claims{ID: 2})
			return next(c)
		}
	})
	e2.GET("/v1/companion/devices/:id/ws", GetCompanionDeviceWS)
	srv2 := httptest.NewServer(e2)
	defer srv2.Close()
	_, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv2.URL, "http")+"/v1/companion/devices/p/ws", h)
	if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other user's tunnel: %v %v", err, resp)
	}
	if len(stolen) != 0 {
		t.Fatalf("q received p's requests: %v", stolen)
	}
}
