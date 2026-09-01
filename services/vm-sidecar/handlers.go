// handlers.go implements the HTTP layer for VM CRUD/lifecycle - thin
// wrappers around LibvirtStore that translate to/from JSON.
package main

import (
	"encoding/json"
	"errors"
	"net/http"

	libvirt "libvirt.org/go/libvirt"
)

func RegisterVMRoutes(mux *http.ServeMux, store *LibvirtStore) {
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		vms, err := store.ListVMs()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, vms)
	})

	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		vm, err := store.GetVM(r.PathValue("name"))
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, vm)
	})

	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		var req CreateVMRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		vm, err := store.CreateVM(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, vm)
	})

	mux.HandleFunc("PUT /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		var req UpdateVMRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		vm, err := store.UpdateVM(r.PathValue("name"), req)
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, vm)
	})

	mux.HandleFunc("POST /vms/{name}/start", vmAction(store.StartVM))
	mux.HandleFunc("POST /vms/{name}/shutdown", vmAction(store.ShutdownVM))
	mux.HandleFunc("POST /vms/{name}/force-off", vmAction(store.ForceOffVM))
	mux.HandleFunc("POST /vms/{name}/reset", vmAction(store.ResetVM))

	mux.HandleFunc("DELETE /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		wipeDisk := r.URL.Query().Get("wipe_disk") == "true"
		if err := store.DeleteVM(r.PathValue("name"), wipeDisk); err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Hot attach/detach - unlike PUT /vms/{name} (which requires a stopped
	// VM and redefines the whole domain), these apply to a VM in any
	// state: live if it's running, persistent-config-only if it's
	// stopped. Console-side USB/disk attach lives here specifically so it
	// works without powering the VM off first.
	mux.HandleFunc("POST /vms/{name}/usb-devices", func(w http.ResponseWriter, r *http.Request) {
		var spec USBDeviceSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := store.AttachUSBDevice(r.PathValue("name"), spec); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /vms/{name}/usb-devices/{vendor}/{product}", func(w http.ResponseWriter, r *http.Request) {
		spec := USBDeviceSpec{VendorID: r.PathValue("vendor"), ProductID: r.PathValue("product")}
		if err := store.DetachUSBDevice(r.PathValue("name"), spec); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /vms/{name}/pci-devices", func(w http.ResponseWriter, r *http.Request) {
		var spec PCIDeviceSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := store.AttachPCIDevice(r.PathValue("name"), spec); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /vms/{name}/pci-devices/{address}", func(w http.ResponseWriter, r *http.Request) {
		spec := PCIDeviceSpec{Address: r.PathValue("address")}
		if err := store.DetachPCIDevice(r.PathValue("name"), spec); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /vms/{name}/disks", func(w http.ResponseWriter, r *http.Request) {
		var spec DiskSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		disk, err := store.AttachDisk(r.PathValue("name"), spec)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, disk)
	})

	mux.HandleFunc("DELETE /vms/{name}/disks/{target}", func(w http.ResponseWriter, r *http.Request) {
		if err := store.DetachDisk(r.PathValue("name"), r.PathValue("target")); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /vms/{name}/cdrom/eject", func(w http.ResponseWriter, r *http.Request) {
		if err := store.EjectCDROM(r.PathValue("name")); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /vms/{name}/cdrom", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ISOPath string `json:"iso_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := store.InsertCDROM(r.PathValue("name"), req.ISOPath); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /vms/{name}/network/link", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MAC   string `json:"mac"`
			State string `json:"state"` // "up" or "down"
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := store.SetNetworkLinkState(r.PathValue("name"), req.MAC, req.State); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /vms/{name}/network/adapter", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OldMAC string  `json:"old_mac"`
			NIC    NICSpec `json:"nic"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := store.UpdateNetworkAdapter(r.PathValue("name"), req.OldMAC, req.NIC); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// vmAction adapts a LibvirtStore method taking just a VM name into an
// http.HandlerFunc, for the four start/shutdown/force-off/reboot actions
// that share the same request/response shape.
func vmAction(fn func(name string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(r.PathValue("name")); err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func isNotFound(err error) bool {
	return errors.Is(err, libvirt.ERR_NO_DOMAIN)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
