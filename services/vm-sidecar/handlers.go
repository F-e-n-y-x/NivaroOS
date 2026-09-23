// handlers.go implements the HTTP layer for VM CRUD/lifecycle - thin
// wrappers around LibvirtStore that translate to/from JSON.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

func RegisterVMRoutes(mux *http.ServeMux, store *LibvirtStore) {
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		vms, err := store.ListVMs()
		if err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, vms)
	})

	handleVM(mux, "GET /vms/{name}", func(w http.ResponseWriter, r *http.Request, name string) {
		vm, err := store.GetVM(name)
		if err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, vm)
	})

	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		var req CreateVMRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		vm, err := store.CreateVM(req)
		if err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, vm)
	})

	handleVM(mux, "PUT /vms/{name}", func(w http.ResponseWriter, r *http.Request, name string) {
		var req UpdateVMRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		vm, err := store.UpdateVM(name, req)
		if err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, vm)
	})

	handleVM(mux, "POST /vms/{name}/start", vmAction(store.StartVM))
	handleVM(mux, "POST /vms/{name}/shutdown", vmAction(store.ShutdownVM))
	handleVM(mux, "POST /vms/{name}/force-off", vmAction(store.ForceOffVM))
	handleVM(mux, "POST /vms/{name}/reset", vmAction(store.ResetVM))
	handleVM(mux, "POST /vms/{name}/pause", vmAction(store.PauseVM))
	handleVM(mux, "POST /vms/{name}/resume", vmAction(store.ResumeVM))

	handleVM(mux, "DELETE /vms/{name}", func(w http.ResponseWriter, r *http.Request, name string) {
		wipeDisk := r.URL.Query().Get("wipe_disk") == "true"
		if err := store.DeleteVM(name, wipeDisk); err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Hot attach/detach - unlike PUT /vms/{name} (which requires a stopped
	// VM and redefines the whole domain), these apply to a VM in any
	// state: live if it's running, persistent-config-only if it's
	// stopped. Console-side USB/disk attach lives here specifically so it
	// works without powering the VM off first.
	handleVM(mux, "POST /vms/{name}/usb-devices", func(w http.ResponseWriter, r *http.Request, name string) {
		var spec USBDeviceSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		if err := store.AttachUSBDevice(name, spec); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handleVM(mux, "DELETE /vms/{name}/usb-devices/{vendor}/{product}", func(w http.ResponseWriter, r *http.Request, name string) {
		spec := USBDeviceSpec{VendorID: r.PathValue("vendor"), ProductID: r.PathValue("product")}
		if err := store.DetachUSBDevice(name, spec); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handleVM(mux, "POST /vms/{name}/pci-devices", func(w http.ResponseWriter, r *http.Request, name string) {
		var spec PCIDeviceSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		if err := store.AttachPCIDevice(name, spec); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handleVM(mux, "DELETE /vms/{name}/pci-devices/{address}", func(w http.ResponseWriter, r *http.Request, name string) {
		spec := PCIDeviceSpec{Address: r.PathValue("address")}
		if err := store.DetachPCIDevice(name, spec); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handleVM(mux, "POST /vms/{name}/disks", func(w http.ResponseWriter, r *http.Request, name string) {
		var spec DiskSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		disk, err := store.AttachDisk(name, spec)
		if err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, disk)
	})

	handleVM(mux, "DELETE /vms/{name}/disks/{target}", func(w http.ResponseWriter, r *http.Request, name string) {
		if err := store.DetachDisk(name, r.PathValue("target")); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handleVM(mux, "POST /vms/{name}/cdrom/eject", func(w http.ResponseWriter, r *http.Request, name string) {
		if err := store.EjectCDROM(name); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handleVM(mux, "POST /vms/{name}/cdrom", func(w http.ResponseWriter, r *http.Request, name string) {
		var req struct {
			ISOPath string `json:"iso_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		if err := store.InsertCDROM(name, req.ISOPath); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handleVM(mux, "POST /vms/{name}/network/link", func(w http.ResponseWriter, r *http.Request, name string) {
		var req struct {
			MAC   string `json:"mac"`
			State string `json:"state"` // "up" or "down"
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		if err := store.SetNetworkLinkState(name, req.MAC, req.State); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handleVM(mux, "POST /vms/{name}/network/adapter", func(w http.ResponseWriter, r *http.Request, name string) {
		var req struct {
			OldMAC string  `json:"old_mac"`
			NIC    NICSpec `json:"nic"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		if err := store.UpdateNetworkAdapter(name, req.OldMAC, req.NIC); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Live Shared Folders (VirtIO-FS Host-to-Guest Direct Directory Pass-Through)
	handleVM(mux, "GET /vms/{name}/shared-folders", func(w http.ResponseWriter, r *http.Request, name string) {
		shares, err := store.ListSharedFolders(name)
		if err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, shares)
	})

	handleVM(mux, "POST /vms/{name}/shared-folders", func(w http.ResponseWriter, r *http.Request, name string) {
		var spec SharedFolderSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		warning, err := store.AttachSharedFolder(name, spec)
		if err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, struct {
			SharedFolderSpec
			Warning string `json:"warning,omitempty"`
		}{spec, warning})
	})

	handleVM(mux, "DELETE /vms/{name}/shared-folders/{tag}", func(w http.ResponseWriter, r *http.Request, name string) {
		tag := r.PathValue("tag")
		if err := store.DetachSharedFolder(name, tag); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handleVM(mux, "POST /vms/{name}/insert-virtio-win", func(w http.ResponseWriter, r *http.Request, name string) {
		virtioWinPath := filepath.Join(defaultISODir, "virtio-win.iso")
		if _, err := os.Stat(virtioWinPath); err != nil {
			writeError(w, http.StatusNotFound, fmt.Errorf("virtio-win.iso not found at %s: %w", virtioWinPath, err))
			return
		}
		if err := store.InsertCDROM(name, virtioWinPath); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// VM Snapshots (Create, List, Get, Revert, Delete)
	handleVM(mux, "GET /vms/{name}/snapshots", func(w http.ResponseWriter, r *http.Request, name string) {
		snaps, err := store.ListSnapshots(name)
		if err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, snaps)
	})

	handleVM(mux, "POST /vms/{name}/snapshots", func(w http.ResponseWriter, r *http.Request, name string) {
		var req CreateSnapshotRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		snap, err := store.CreateSnapshot(name, req)
		if err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, snap)
	})

	handleVM(mux, "GET /vms/{name}/snapshots/{snap}", func(w http.ResponseWriter, r *http.Request, name string) {
		snap, err := store.GetSnapshot(name, r.PathValue("snap"))
		if err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, snap)
	})

	handleVM(mux, "POST /vms/{name}/snapshots/{snap}/revert", func(w http.ResponseWriter, r *http.Request, name string) {
		if err := store.RevertSnapshot(name, r.PathValue("snap")); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	handleVM(mux, "DELETE /vms/{name}/snapshots/{snap}", func(w http.ResponseWriter, r *http.Request, name string) {
		children := r.URL.Query().Get("children") == "true"
		if err := store.DeleteSnapshot(name, r.PathValue("snap"), children); err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// handleVM registers a route whose pattern has a {name} wildcard,
// rejecting (400) any name that isn't a valid VM name before h runs. The
// name ends up in host filesystem paths (the VM's own folder and its
// auto-generated disk/NVRAM paths, its legacy share directory on delete)
// as well as libvirt lookups, so an encoded "..%2F.." must never get past
// here - it once made the share directory "/" and unmounted host mounts.
func handleVM(mux *http.ServeMux, pattern string, h func(w http.ResponseWriter, r *http.Request, name string)) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		name, ok := vmNameFromPath(w, r)
		if !ok {
			return
		}
		h(w, r, name)
	})
}

// vmNameFromPath reads and validates the {name} path value, writing a 400
// itself (and returning ok=false) when it's not a valid VM name.
func vmNameFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("name")
	if !vmNameRe.MatchString(name) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid VM name %q: only letters, digits, - and _ are allowed", name))
		return "", false
	}
	return name, true
}

// vmAction adapts a LibvirtStore method taking just a VM name into a
// handleVM handler, for the start/shutdown/force-off/reset/pause/resume
// actions that share the same request/response shape.
func vmAction(fn func(name string) error) func(w http.ResponseWriter, r *http.Request, name string) {
	return func(w http.ResponseWriter, r *http.Request, name string) {
		if err := fn(name); err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// statusError carries the HTTP status an error should be reported with -
// for the cases (conflicts, bad input) the handler can't tell apart from
// a generic failure by the error value alone.
type statusError struct {
	status int
	msg    string
}

func (e *statusError) Error() string { return e.msg }

// conflictf builds a 409 Conflict error: the request is well-formed but
// can't be applied to the resource's current state (e.g. a running VM).
func conflictf(format string, args ...interface{}) error {
	return &statusError{status: http.StatusConflict, msg: fmt.Sprintf(format, args...)}
}

// badRequestf builds a 400 Bad Request error for invalid client input.
func badRequestf(format string, args ...interface{}) error {
	return &statusError{status: http.StatusBadRequest, msg: fmt.Sprintf(format, args...)}
}

func isNotFound(err error) bool {
	return errors.Is(err, libvirt.ERR_NO_DOMAIN) || errors.Is(err, libvirt.ERR_NO_DOMAIN_SNAPSHOT)
}

// errorStatus picks the HTTP status for err: an explicit statusError's own
// status, 404 for a missing domain/snapshot, 409 for libvirt refusing an
// operation in the domain's current state ("domain is already running",
// "domain is not running"), or fallback otherwise.
func errorStatus(err error, fallback int) int {
	var se *statusError
	if errors.As(err, &se) {
		return se.status
	}
	if isNotFound(err) {
		return http.StatusNotFound
	}
	if errors.Is(err, libvirt.ERR_OPERATION_INVALID) {
		return http.StatusConflict
	}
	return fallback
}

// writeStoreError writes err with the status errorStatus picks for it.
func writeStoreError(w http.ResponseWriter, fallback int, err error) {
	writeError(w, errorStatus(err, fallback), err)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": errorMessage(err)})
}

// errorMessage is err's text with any libvirt error inside it reduced to
// just its human-readable Message - libvirt.Error's own Error() is a
// "virError(Code=55, Domain=10, Message='...')" dump that's meaningless
// to show a user.
func errorMessage(err error) string {
	msg := err.Error()
	var verr libvirt.Error
	if errors.As(err, &verr) && verr.Message != "" {
		msg = strings.Replace(msg, verr.Error(), verr.Message, 1)
	}
	return msg
}
