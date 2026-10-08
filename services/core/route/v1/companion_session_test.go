package v1

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/labstack/echo/v5"
)

// companionReqAs runs handler as user uid in login session sid.
func companionReqAs(t *testing.T, h echo.HandlerFunc, method, target string, uid int, sid, id string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, target, bytes.NewReader(b))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := NewEcho().NewContext(req, rec)
	c.Set("user", &jwt.Claims{ID: uid, Username: "u", SessionID: sid})
	if id != "" {
		c.SetPathValues(echo.PathValues{{Name: "id", Value: id}})
	}
	if err := h(c); err != nil {
		t.Fatal(err)
	}
	return rec
}

func deviceNamed(t *testing.T, uid int, id string) CompanionDevice {
	t.Helper()
	rec := companionReq(t, GetCompanionDevices, http.MethodGet, "/v1/companion/devices", uid, "", nil)
	var res struct {
		Data []CompanionDevice `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	for _, d := range res.Data {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("device %s not listed: %s", id, rec.Body)
	return CompanionDevice{}
}

// Removing a phone from the web used to leave its app signed in: it kept
// using the server with its tokens. Now the phone's own sessions are ended
// (every service refuses their tokens), and the web session doing the
// removing - same user - carries on.
func TestRemovingAPhoneSignsOutExactlyThatPhone(t *testing.T) {
	useTempCompanionState(t)
	priv, pub, _ := jwt.GenerateKeyPair()
	key := func() (*ecdsa.PublicKey, error) { return pub, nil }
	phoneSID, webSID, otherPhoneSID := jwt.NewSessionID(), jwt.NewSessionID(), jwt.NewSessionID()
	phoneTok, _, _ := jwt.GetSessionTokens("alice", priv, 7, phoneSID)
	webTok, _, _ := jwt.GetSessionTokens("alice", priv, 7, webSID)
	otherTok, _, _ := jwt.GetSessionTokens("alice", priv, 7, otherPhoneSID)

	phone := CompanionRegistrationDTO{ID: "dev_android_s25", Name: "Galaxy S25", Model: "SM-S938B"}
	tablet := CompanionRegistrationDTO{ID: "dev_android_tab", Name: "Tab S9", Model: "SM-X710"}
	if rec := companionReqAs(t, PostRegisterCompanionDevice, http.MethodPost, "/v1/companion/register", 7, phoneSID, "", phone); rec.Code != http.StatusOK {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	// The heartbeat again: still one session for the phone.
	companionReqAs(t, PostRegisterCompanionDevice, http.MethodPost, "/v1/companion/register", 7, phoneSID, "", phone)
	companionReqAs(t, PostRegisterCompanionDevice, http.MethodPost, "/v1/companion/register", 7, otherPhoneSID, "", tablet)
	if got := companionSessions[phone.ID]; len(got) != 1 || got[0] != phoneSID {
		t.Fatalf("phone sessions %v", got)
	}

	// The binding survives a restart of core.
	companionMu.Lock()
	companionLoaded = false
	companionMu.Unlock()

	rec := companionReqAs(t, DeleteCompanionDevice, http.MethodDelete, "/v1/companion/devices/"+phone.ID, 7, webSID, phone.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	var res struct {
		Data map[string]interface{} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Data["signed_out"] != true {
		t.Fatalf("response doesn't say the phone was signed out: %s", rec.Body)
	}

	ok, _, err := jwt.Validate(phoneTok, key)
	if ok {
		t.Fatal("the removed phone's token still works")
	}
	if reason, _ := jwt.RevocationReason(err); reason != jwt.ReasonCompanionRemoved {
		t.Fatalf("revoked without the companion reason: %v", err)
	}
	if ok, _, err := jwt.Validate(webTok, key); !ok {
		t.Fatalf("the web session doing the removing was signed out too: %v", err)
	}
	if ok, _, err := jwt.Validate(otherTok, key); !ok {
		t.Fatalf("another phone of the same user was signed out: %v", err)
	}
	if _, bound := companionSessions[phone.ID]; bound {
		t.Fatal("removed phone still has sessions bound")
	}

	// No ghost: the web list no longer has it, and its heartbeat (were the
	// token still usable, as with apps signed in before sessions existed)
	// can't bring it back.
	if ids := listIDs(t, 7); has(ids, phone.ID) || !has(ids, tablet.ID) {
		t.Fatalf("list after removal: %v", ids)
	}
	if rec := companionReqAs(t, PostRegisterCompanionDevice, http.MethodPost, "/v1/companion/register", 7, phoneSID, "", phone); rec.Code != http.StatusGone {
		t.Fatalf("heartbeat after removal: %d", rec.Code)
	}

	// Signing in again on the phone (a new session) pairs it again, and
	// that session is the one bound now.
	newSID := jwt.NewSessionID()
	again := phone
	again.Repair = true
	if rec := companionReqAs(t, PostRegisterCompanionDevice, http.MethodPost, "/v1/companion/register", 7, newSID, "", again); rec.Code != http.StatusOK {
		t.Fatalf("pair again after signing in: %d %s", rec.Code, rec.Body)
	}
	if got := companionSessions[phone.ID]; len(got) != 1 || got[0] != newSID {
		t.Fatalf("sessions after pairing again %v", got)
	}
}

// A session belongs to one phone: the same sign-in registering under a new
// device id (app data cleared) moves, and removing the old entry doesn't
// sign the phone out.
func TestASessionMovesWithItsPhone(t *testing.T) {
	useTempCompanionState(t)
	sid := jwt.NewSessionID()
	companionReqAs(t, PostRegisterCompanionDevice, http.MethodPost, "/v1/companion/register", 7, sid, "", CompanionRegistrationDTO{ID: "old_id", Name: "Pixel"})
	companionReqAs(t, PostRegisterCompanionDevice, http.MethodPost, "/v1/companion/register", 7, sid, "", CompanionRegistrationDTO{ID: "new_id", Name: "Pixel"})
	companionReqAs(t, DeleteCompanionDevice, http.MethodDelete, "/v1/companion/devices/old_id", 7, jwt.NewSessionID(), "old_id", nil)
	if _, revoked := jwt.SessionRevoked(sid); revoked {
		t.Fatal("removing the stale entry signed out the phone that moved on")
	}
	if data, err := os.ReadFile(filepath.Join(companionStateDir, "companion_sessions.json")); err != nil || !bytes.Contains(data, []byte(sid)) {
		t.Fatalf("sessions file: %v %s", err, data)
	}
}

// A rename from the web used to last until the phone's next heartbeat: the
// app sent its own locally kept name flagged user_renamed and the server
// took it. Now a user's name - from the web or the app - is kept, and the
// register answer carries it so the app adopts it.
func TestAWebRenameSurvivesTheHeartbeat(t *testing.T) {
	_, base := useTempCompanionState(t)
	phone := CompanionRegistrationDTO{ID: "dev_s25", Name: "Galaxy S25", Model: "SM-S938B"}
	register(t, 7, phone)
	if d := deviceNamed(t, 7, phone.ID); d.NameSource != companionNameDevice {
		t.Fatalf("a new phone's own name: source %q", d.NameSource)
	}

	rec := companionReq(t, PutUpdateCompanionDevice, http.MethodPut, "/v1/companion/devices/dev_s25", 7, "dev_s25", map[string]string{"name": "  Work phone "})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}

	// An old app: its local name, flagged as the user's.
	legacy := phone
	legacy.Name = "Galaxy S25"
	legacy.CustomProps = map[string]interface{}{"user_renamed": true}
	rec = register(t, 7, legacy)
	// A new app: its last known name, as the user's.
	current := phone
	current.Name = "Old name the app held"
	current.NameSource = companionNameUser
	rec = register(t, 7, current)

	var res struct {
		Data struct {
			Device CompanionDevice `json:"device"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Data.Device.Name != "Work phone" {
		t.Fatalf("register answer should carry the server's name, got %q", res.Data.Device.Name)
	}
	d := deviceNamed(t, 7, phone.ID)
	if d.Name != "Work phone" || d.NameSource != companionNameUser || d.NameUpdatedAt.IsZero() {
		t.Fatalf("after heartbeats: %q (%s)", d.Name, d.NameSource)
	}
	if _, flagged := d.CustomProps["user_renamed"]; flagged {
		t.Fatal("user_renamed leaked back into custom_props")
	}
	if d.StoragePath != filepath.Join(base, "Work phone") {
		t.Fatalf("backup folder not renamed with it: %s", d.StoragePath)
	}

	// The app renames (PUT, like the web): that one wins now.
	companionReq(t, PutUpdateCompanionDevice, http.MethodPut, "/v1/companion/devices/dev_s25", 7, "dev_s25", map[string]string{"name": "Pocket"})
	register(t, 7, phone)
	if d := deviceNamed(t, 7, phone.ID); d.Name != "Pocket" {
		t.Fatalf("app rename: %q", d.Name)
	}
}

// While nobody has named the phone, its name follows what the phone
// reports; a name the old app still flags as the user's is adopted once.
func TestAnUnnamedPhoneFollowsThePhone(t *testing.T) {
	useTempCompanionState(t)
	register(t, 7, CompanionRegistrationDTO{ID: "p", Name: "Pixel 8"})
	register(t, 7, CompanionRegistrationDTO{ID: "p", Name: "Pixel 8 Pro"})
	if d := deviceNamed(t, 7, "p"); d.Name != "Pixel 8 Pro" || d.NameSource != companionNameDevice {
		t.Fatalf("got %q %s", d.Name, d.NameSource)
	}
	register(t, 7, CompanionRegistrationDTO{ID: "p", Name: "Kitchen", CustomProps: map[string]interface{}{"user_renamed": true}})
	if d := deviceNamed(t, 7, "p"); d.Name != "Kitchen" || d.NameSource != companionNameUser {
		t.Fatalf("got %q %s", d.Name, d.NameSource)
	}
	register(t, 7, CompanionRegistrationDTO{ID: "p", Name: "Pixel 8 Pro"})
	if d := deviceNamed(t, 7, "p"); d.Name != "Kitchen" {
		t.Fatalf("the phone's own name replaced the user's: %q", d.Name)
	}
}

// Old companion_devices.json: user_renamed moves to name_source.
func TestLegacyUserRenamedFlagIsMigrated(t *testing.T) {
	state, _ := useTempCompanionState(t)
	os.MkdirAll(state, 0o755)
	os.WriteFile(filepath.Join(state, "companion_devices.json"), []byte(`[
	 {"id":"a","name":"Work phone","custom_props":{"user_renamed":true,"x":1}},
	 {"id":"b","name":"Pixel"}
	]`), 0o644)
	companionMu.Lock()
	loadCompanionDevicesLocked()
	a, b := companionDevices["a"], companionDevices["b"]
	companionMu.Unlock()
	if a.NameSource != companionNameUser || b.NameSource != companionNameDevice {
		t.Fatalf("sources %q %q", a.NameSource, b.NameSource)
	}
	if _, ok := a.CustomProps["user_renamed"]; ok || a.CustomProps["x"] == nil {
		t.Fatalf("props %v", a.CustomProps)
	}
}

func TestRenameRejectsOverlongNames(t *testing.T) {
	useTempCompanionState(t)
	register(t, 7, CompanionRegistrationDTO{ID: "p", Name: "Pixel"})
	long := string(bytes.Repeat([]byte("x"), 101))
	if rec := companionReq(t, PutUpdateCompanionDevice, http.MethodPut, "/v1/companion/devices/p", 7, "p", map[string]string{"name": long}); rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d", rec.Code)
	}
}
