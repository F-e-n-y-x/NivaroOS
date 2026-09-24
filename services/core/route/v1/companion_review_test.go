package v1

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Regression tests from the adversarial review of the S-02/S-03/S-04 fixes
// (all three findings fixed).

// Review finding: a legacy (unowned) device is visible - id included - to
// every user, and the first authenticated register carrying its id claims
// it and is handed the phone's existing file-server secret. Another user
// can therefore take over a legacy phone before its own heartbeat does
// (e.g. while the phone is off), lock the real owner out (403), repoint
// its IP/port and use the secret against the phone's LAN file server.
func TestReviewLegacyDeviceCannotBeClaimedByAnotherUser(t *testing.T) {
	useTempCompanionState(t)
	companionMu.Lock()
	companionLoaded = true
	companionDevices["alice_old"] = &CompanionDevice{ID: "alice_old", Name: "Alice phone", IP: "192.168.1.9", Secret: "phone-secret"}
	companionMu.Unlock()

	// Bob sees the id in his device list...
	if !has(listIDs(t, 2), "alice_old") {
		t.Fatal("precondition: legacy device visible to user 2")
	}
	// ...and registers it as his.
	rec := register(t, 2, CompanionRegistrationDTO{ID: "alice_old", Name: "Alice phone", IP: "192.168.1.66"})
	var res struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Data.Secret == "phone-secret" {
		t.Errorf("another user was handed the legacy phone's existing secret")
	}
	companionMu.Lock()
	owner, ip := companionDevices["alice_old"].OwnerUserID, companionDevices["alice_old"].IP
	companionMu.Unlock()
	if owner == "2" || ip == "192.168.1.66" {
		t.Errorf("legacy device taken over by another user: owner=%q ip=%q", owner, ip)
	}
	// The real phone's next heartbeat is now refused.
	if rec := register(t, 1, CompanionRegistrationDTO{ID: "alice_old", Name: "Alice phone"}); rec.Code == http.StatusForbidden {
		t.Errorf("real owner locked out of their own phone (403)")
	}
}

// Review finding: assignCompanionFolder reuses <base>/<name> whenever no
// *current* device uses it. A phone removed with "keep backups" leaves its
// folder behind; any NEW phone with the same (often default) name - of any
// user - is given that folder and can list and download the old phone's
// backups through the server-backup fallback, and its uploads overwrite
// same-named files in it.
func TestReviewNewPhoneDoesNotInheritAnotherUsersKeptBackups(t *testing.T) {
	_, base := useTempCompanionState(t)
	register(t, 1, CompanionRegistrationDTO{ID: "alice_pixel", Name: "Pixel 8", IP: "Local"})
	companionMu.Lock()
	aliceDir := companionDevices["alice_pixel"].StoragePath
	companionMu.Unlock()
	os.WriteFile(filepath.Join(aliceDir, "passport.jpg"), []byte("private"), 0o644)
	// Removed, backups kept (the default).
	if rec := companionReq(t, DeleteCompanionDevice, http.MethodDelete, "/", 1, "alice_pixel", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}

	register(t, 2, CompanionRegistrationDTO{ID: "bob_pixel", Name: "Pixel 8", IP: "Local"})
	companionMu.Lock()
	bobDir := companionDevices["bob_pixel"].StoragePath
	companionMu.Unlock()
	if bobDir == aliceDir {
		t.Errorf("user 2's new phone was given user 1's kept backup folder %s", filepath.Base(bobDir))
	}
	_ = base
}

// Review finding: handlers keep using the *CompanionDevice after
// lookupCompanionDevice released companionMu - they write LastSeen/IsOnline
// and JSON-encode the device (CustomProps map included) with no lock, while
// the phone's 30 s heartbeat (PostRegisterCompanionDevice) writes the same
// fields and map under the lock. A concurrent map read/write is a fatal,
// unrecoverable runtime error that kills the core service. Run with -race.
func TestReviewCompanionHandlersDoNotRaceTheHeartbeat(t *testing.T) {
	useTempCompanionState(t)
	register(t, 1, CompanionRegistrationDTO{ID: "p", Name: "P", IP: "0.0.0.0", Port: 1, CustomProps: map[string]interface{}{"k": 0}})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			register(t, 1, CompanionRegistrationDTO{ID: "p", Name: "P", IP: "0.0.0.0", Port: 1, CustomProps: map[string]interface{}{"k": i, "battery": i}})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			companionReq(t, GetCompanionDeviceFiles, http.MethodGet, "/v1/companion/devices/p/files?path=/", 1, "p", nil)
		}
	}()
	wg.Wait()
}
