package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	libvirt "libvirt.org/go/libvirt"
)

// Every {name} route must refuse a traversal-shaped name before any
// handler (and so any filesystem work keyed on the name) runs.
func TestVMRoutes_RejectInvalidNames(t *testing.T) {
	store := newTestStore(t)
	mux := http.NewServeMux()
	RegisterVMRoutes(mux, store)
	RegisterConsoleRoutes(mux, store)
	RegisterScreenshotRoutes(mux, store)

	for _, target := range []string{
		"/vms/..%2F..%2Fetc/shared-folders",
		"/vms/..%2F..%2F/shared-folders",
		"/vms/a%20b",
		"/vms/..%2F..%2Fetc/console",
		"/vms/x.y/screenshot",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			r := httptest.NewRequest(method, target, strings.NewReader("{}"))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest && w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
				t.Errorf("%s %s: expected 400, got %d", method, target, w.Code)
			}
			if w.Code == http.StatusBadRequest && !strings.Contains(w.Body.String(), "invalid VM name") {
				t.Errorf("%s %s: expected an invalid-name error, got %s", method, target, w.Body.String())
			}
		}
	}
}

func TestPauseResumeRoutes(t *testing.T) {
	store := newTestStore(t)
	mux := http.NewServeMux()
	RegisterVMRoutes(mux, store)

	do := func(path string) int {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		return w.Code
	}
	if code := do("/vms/test/pause"); code != http.StatusNoContent {
		t.Fatalf("pause: expected 204, got %d", code)
	}
	if vm, _ := store.GetVM("test"); vm.State != "paused" {
		t.Fatalf("expected paused, got %q", vm.State)
	}
	if code := do("/vms/test/resume"); code != http.StatusNoContent {
		t.Fatalf("resume: expected 204, got %d", code)
	}
	if code := do("/vms/nope/pause"); code != http.StatusNotFound {
		t.Fatalf("pause of a missing VM: expected 404, got %d", code)
	}
}

func TestErrorStatusAndMessage(t *testing.T) {
	verr := libvirt.Error{Code: libvirt.ERR_OPERATION_INVALID, Message: "Requested operation is not valid: domain is already running"}
	wrapped := fmt.Errorf("start domain: %w", verr)
	if got := errorStatus(wrapped, 500); got != http.StatusConflict {
		t.Errorf("OPERATION_INVALID: expected 409, got %d", got)
	}
	if got := errorMessage(wrapped); got != "start domain: Requested operation is not valid: domain is already running" {
		t.Errorf("expected the bare libvirt message, got %q", got)
	}
	if got := errorStatus(libvirt.Error{Code: libvirt.ERR_NO_DOMAIN_SNAPSHOT}, 500); got != http.StatusNotFound {
		t.Errorf("NO_DOMAIN_SNAPSHOT: expected 404, got %d", got)
	}
	if got := errorStatus(fmt.Errorf("x: %w", conflictf("busy")), 500); got != http.StatusConflict {
		t.Errorf("conflictf: expected 409, got %d", got)
	}
	if got := errorStatus(errors.New("boom"), 500); got != 500 {
		t.Errorf("plain error: expected fallback, got %d", got)
	}
}
