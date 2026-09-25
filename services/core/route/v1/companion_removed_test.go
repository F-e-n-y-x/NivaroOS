package v1

import (
	"net/http"
	"testing"
)

// Removing a phone used to last 30 seconds: its heartbeat registered it
// again. Now it stays removed until the user pairs it again from the app,
// and the removal survives a restart.
func TestARemovedPhoneStaysRemovedUntilPairedAgain(t *testing.T) {
	useTempCompanionState(t)
	dto := CompanionRegistrationDTO{ID: "dev_android_s25", Name: "S25", Model: "SM-S938B", Platform: "Android"}
	if rec := register(t, 7, dto); rec.Code != http.StatusOK {
		t.Fatalf("first register: %d %s", rec.Code, rec.Body)
	}
	if rec := companionReq(t, DeleteCompanionDevice, http.MethodDelete, "/v1/companion/devices/dev_android_s25", 7, "dev_android_s25", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}

	// The heartbeat, even after a restart (state read back from disk).
	companionMu.Lock()
	companionLoaded = false
	companionMu.Unlock()
	if rec := register(t, 7, dto); rec.Code != http.StatusGone {
		t.Fatalf("heartbeat after removal should get 410, got %d %s", rec.Code, rec.Body)
	}
	if ids := listIDs(t, 7); len(ids) != 0 {
		t.Fatalf("removed phone came back: %v", ids)
	}

	// Another account can't undo the owner's removal.
	other := dto
	other.Repair = true
	if rec := register(t, 8, other); rec.Code != http.StatusGone {
		t.Fatalf("another account re-pairing should get 410, got %d", rec.Code)
	}

	// The owner taps "Pair again".
	if rec := register(t, 7, other); rec.Code != http.StatusOK {
		t.Fatalf("pair again: %d %s", rec.Code, rec.Body)
	}
	if ids := listIDs(t, 7); len(ids) != 1 {
		t.Fatalf("paired-again phone missing: %v", ids)
	}
	// And from then on the ordinary heartbeat works again.
	if rec := register(t, 7, dto); rec.Code != http.StatusOK {
		t.Fatalf("heartbeat after pairing again: %d", rec.Code)
	}
}
