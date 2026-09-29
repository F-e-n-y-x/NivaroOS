//go:build linux && cgo

package main

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>

// NVML's C API, resolved with dlsym so the binary runs (and builds) on a
// machine without the NVIDIA driver. Device handles never leave C: every
// call takes the device index and looks the handle up itself.
typedef void *nvdev;
#define NV_MISSING (-1000)

static void *nv_lib;
static int (*p_init)(void);
static int (*p_shutdown)(void);
static int (*p_count)(unsigned int *);
static int (*p_handle)(unsigned int, nvdev *);
static int (*p_uuid)(nvdev, char *, unsigned int);
static int (*p_name)(nvdev, char *, unsigned int);
static int (*p_numfans)(nvdev, unsigned int *);
static int (*p_fan_v2)(nvdev, unsigned int, unsigned int *);
static int (*p_fan_v1)(nvdev, unsigned int *);
static int (*p_setfan)(nvdev, unsigned int, unsigned int);
static int (*p_deffan)(nvdev, unsigned int);
static int (*p_minmax)(nvdev, unsigned int *, unsigned int *);
static int (*p_temp)(nvdev, int, unsigned int *);

static int nv_open(void) {
	if (!nv_lib) {
		nv_lib = dlopen("libnvidia-ml.so.1", RTLD_NOW | RTLD_LOCAL);
		if (!nv_lib) nv_lib = dlopen("libnvidia-ml.so", RTLD_NOW | RTLD_LOCAL);
		if (!nv_lib) return NV_MISSING;
		p_init = dlsym(nv_lib, "nvmlInit_v2");
		p_shutdown = dlsym(nv_lib, "nvmlShutdown");
		p_count = dlsym(nv_lib, "nvmlDeviceGetCount_v2");
		p_handle = dlsym(nv_lib, "nvmlDeviceGetHandleByIndex_v2");
		p_uuid = dlsym(nv_lib, "nvmlDeviceGetUUID");
		p_name = dlsym(nv_lib, "nvmlDeviceGetName");
		p_numfans = dlsym(nv_lib, "nvmlDeviceGetNumFans");
		p_fan_v2 = dlsym(nv_lib, "nvmlDeviceGetFanSpeed_v2");
		p_fan_v1 = dlsym(nv_lib, "nvmlDeviceGetFanSpeed");
		p_setfan = dlsym(nv_lib, "nvmlDeviceSetFanSpeed_v2");
		p_deffan = dlsym(nv_lib, "nvmlDeviceSetDefaultFanSpeed_v2");
		p_minmax = dlsym(nv_lib, "nvmlDeviceGetMinMaxFanSpeed");
		p_temp = dlsym(nv_lib, "nvmlDeviceGetTemperature");
	}
	if (!p_init || !p_shutdown || !p_count || !p_handle) return NV_MISSING;
	return p_init();
}

static int nv_close(void) { return p_shutdown ? p_shutdown() : 0; }
static int nv_can_control(void) { return p_setfan && p_deffan && p_numfans ? 1 : 0; }

static int nv_count(unsigned int *n) { return p_count(n); }

static int nv_info(unsigned int i, char *uuid, char *name, unsigned int len) {
	nvdev d; int r = p_handle(i, &d); if (r) return r;
	if (p_uuid) { r = p_uuid(d, uuid, len); if (r) return r; }
	if (p_name) p_name(d, name, len);
	return 0;
}

static int nv_numfans(unsigned int i, unsigned int *n) {
	nvdev d; int r = p_handle(i, &d); if (r) return r;
	if (!p_numfans) { *n = p_fan_v1 ? 1 : 0; return 0; }
	return p_numfans(d, n);
}

static int nv_minmax(unsigned int i, unsigned int *mn, unsigned int *mx) {
	nvdev d; int r = p_handle(i, &d); if (r) return r;
	if (!p_minmax) return NV_MISSING;
	return p_minmax(d, mn, mx);
}

static int nv_temp(unsigned int i, unsigned int *t) {
	nvdev d; int r = p_handle(i, &d); if (r) return r;
	if (!p_temp) return NV_MISSING;
	return p_temp(d, 0, t); // NVML_TEMPERATURE_GPU
}

static int nv_fan(unsigned int i, unsigned int fan, unsigned int *pct) {
	nvdev d; int r = p_handle(i, &d); if (r) return r;
	if (p_fan_v2) return p_fan_v2(d, fan, pct);
	if (p_fan_v1 && fan == 0) return p_fan_v1(d, pct);
	return NV_MISSING;
}

static int nv_setfan(unsigned int i, unsigned int fan, unsigned int pct) {
	nvdev d; int r = p_handle(i, &d); if (r) return r;
	if (!p_setfan) return NV_MISSING;
	return p_setfan(d, fan, pct);
}

static int nv_deffan(unsigned int i, unsigned int fan) {
	nvdev d; int r = p_handle(i, &d); if (r) return r;
	if (!p_deffan) return NV_MISSING;
	return p_deffan(d, fan);
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"
)

var nvmlMu sync.Mutex // NVML's init/shutdown refcount is process-wide

type nvmlError int

func (e nvmlError) Error() string {
	switch int(e) {
	case -1000:
		return "not supported by this NVIDIA driver version"
	case 1:
		return "NVML not initialised"
	case 2:
		return "invalid argument"
	case 3:
		return "not supported by this GPU"
	case 4:
		return "no permission (the fan service must run as root)"
	case 6:
		return "not found"
	case 9:
		return "NVIDIA driver not loaded"
	case 12:
		return "NVIDIA driver library not found"
	case 15:
		return "GPU is lost (fell off the bus)"
	case 18:
		return "driver/library version mismatch (reboot after a driver update)"
	}
	return fmt.Sprintf("NVML error %d", int(e))
}

func nvErr(r C.int) error {
	if r == 0 {
		return nil
	}
	return nvmlError(int(r))
}

type nvmlAPI struct{}

func openNVML() (gpuAPI, error) {
	nvmlMu.Lock()
	defer nvmlMu.Unlock()
	r := C.nv_open()
	if r == C.NV_MISSING {
		return nil, errNoNVML
	}
	if err := nvErr(r); err != nil {
		return nil, err
	}
	return nvmlAPI{}, nil
}

func (nvmlAPI) Close() {
	nvmlMu.Lock()
	defer nvmlMu.Unlock()
	C.nv_close()
}

func (nvmlAPI) Devices() ([]gpuDevice, error) {
	nvmlMu.Lock()
	defer nvmlMu.Unlock()
	var n C.uint
	if err := nvErr(C.nv_count(&n)); err != nil {
		return nil, err
	}
	const buflen = 96
	uuid := (*C.char)(C.calloc(buflen, 1))
	name := (*C.char)(C.calloc(buflen, 1))
	defer C.free(unsafe.Pointer(uuid))
	defer C.free(unsafe.Pointer(name))
	var out []gpuDevice
	for i := C.uint(0); i < n; i++ {
		if C.nv_info(i, uuid, name, buflen) != 0 {
			continue
		}
		d := gpuDevice{Index: int(i), UUID: C.GoString(uuid), Name: C.GoString(name), MinPct: 0, MaxPct: 100}
		var fans C.uint
		if C.nv_numfans(i, &fans) == 0 {
			d.NumFans = int(fans)
		}
		var mn, mx C.uint
		if C.nv_minmax(i, &mn, &mx) == 0 && mx > 0 {
			d.MinPct, d.MaxPct = int(mn), int(mx)
		}
		var t C.uint
		if C.nv_temp(i, &t) == 0 {
			d.TempC, d.TempOK = float64(t), saneC(float64(t))
		}
		if C.nv_can_control() == 1 {
			d.CanControl = true
		} else {
			d.Reason = "This NVIDIA driver is too old for fan control (needs driver 520 or newer)."
		}
		out = append(out, d)
	}
	return out, nil
}

func (nvmlAPI) FanSpeed(index, fan int) (int, error) {
	nvmlMu.Lock()
	defer nvmlMu.Unlock()
	var p C.uint
	if err := nvErr(C.nv_fan(C.uint(index), C.uint(fan), &p)); err != nil {
		return 0, err
	}
	return int(p), nil
}

func (nvmlAPI) SetFanSpeed(index, fan, pct int) error {
	nvmlMu.Lock()
	defer nvmlMu.Unlock()
	return nvErr(C.nv_setfan(C.uint(index), C.uint(fan), C.uint(pct)))
}

func (nvmlAPI) SetDefaultFanSpeed(index, fan int) error {
	nvmlMu.Lock()
	defer nvmlMu.Unlock()
	return nvErr(C.nv_deffan(C.uint(index), C.uint(fan)))
}
