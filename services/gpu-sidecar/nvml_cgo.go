//go:build linux && cgo

package main

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <string.h>

// NVML resolved with dlsym, like the fan service (services/fans/nvml_cgo.go),
// so the binary builds and runs without the NVIDIA driver. Device handles
// never leave C: every call takes the device index.
typedef void *nvdev;
#define NV_MISSING (-1000)

typedef struct { unsigned int gpu, memory; } nv_util;
typedef struct { unsigned long long total, free, used; } nv_mem;
typedef struct { unsigned int version; unsigned long long total, reserved, free, used; } nv_mem2;
typedef struct { unsigned int pid; unsigned long long used_mem; unsigned int gi, ci; } nv_proc;
typedef struct { unsigned int pid; unsigned long long ts; unsigned int sm, mem, enc, dec; } nv_psample;

static void *nv_lib;
static int nv_ready;
static int (*p_init)(void);
static int (*p_driver)(char *, unsigned int);
static int (*p_count)(unsigned int *);
static int (*p_handle)(unsigned int, nvdev *);
static int (*p_name)(nvdev, char *, unsigned int);
static int (*p_util)(nvdev, nv_util *);
static int (*p_mem)(nvdev, nv_mem *);
static int (*p_mem2)(nvdev, nv_mem2 *);
static int (*p_temp)(nvdev, int, unsigned int *);
static int (*p_power)(nvdev, unsigned int *);
static int (*p_limit)(nvdev, unsigned int *);
static int (*p_compute)(nvdev, unsigned int *, nv_proc *);
static int (*p_graphics)(nvdev, unsigned int *, nv_proc *);
static int (*p_putil)(nvdev, nv_psample *, unsigned int *, unsigned long long);
static int (*p_pname)(unsigned int, char *, unsigned int);

// Opened and initialised once for the life of the process; retried on
// the next call while it fails (no driver yet, or it's being installed).
static int nv_open(void) {
	if (nv_ready) return 0;
	if (!nv_lib) {
		nv_lib = dlopen("libnvidia-ml.so.1", RTLD_NOW | RTLD_LOCAL);
		if (!nv_lib) nv_lib = dlopen("libnvidia-ml.so", RTLD_NOW | RTLD_LOCAL);
		if (!nv_lib) return NV_MISSING;
		p_init = dlsym(nv_lib, "nvmlInit_v2");
		p_driver = dlsym(nv_lib, "nvmlSystemGetDriverVersion");
		p_count = dlsym(nv_lib, "nvmlDeviceGetCount_v2");
		p_handle = dlsym(nv_lib, "nvmlDeviceGetHandleByIndex_v2");
		p_name = dlsym(nv_lib, "nvmlDeviceGetName");
		p_util = dlsym(nv_lib, "nvmlDeviceGetUtilizationRates");
		p_mem = dlsym(nv_lib, "nvmlDeviceGetMemoryInfo");
		p_mem2 = dlsym(nv_lib, "nvmlDeviceGetMemoryInfo_v2");
		p_temp = dlsym(nv_lib, "nvmlDeviceGetTemperature");
		p_power = dlsym(nv_lib, "nvmlDeviceGetPowerUsage");
		p_limit = dlsym(nv_lib, "nvmlDeviceGetPowerManagementLimit");
		p_compute = dlsym(nv_lib, "nvmlDeviceGetComputeRunningProcesses_v3");
		if (!p_compute) p_compute = dlsym(nv_lib, "nvmlDeviceGetComputeRunningProcesses_v2");
		p_graphics = dlsym(nv_lib, "nvmlDeviceGetGraphicsRunningProcesses_v3");
		if (!p_graphics) p_graphics = dlsym(nv_lib, "nvmlDeviceGetGraphicsRunningProcesses_v2");
		p_putil = dlsym(nv_lib, "nvmlDeviceGetProcessUtilization");
		p_pname = dlsym(nv_lib, "nvmlSystemGetProcessName");
	}
	if (!p_init || !p_driver || !p_count || !p_handle || !p_name || !p_util || !p_mem || !p_temp) return NV_MISSING;
	int r = p_init();
	if (r == 0) nv_ready = 1;
	return r;
}

static int nv_driver(char *buf, unsigned int len) { return p_driver(buf, len); }
static int nv_count(unsigned int *n) { return p_count(n); }

typedef struct {
	unsigned int util, temp, power_mw, limit_mw;
	unsigned long long mem_used, mem_total;
	int power_ok, limit_ok;
} nv_stats;

static int nv_stats_get(unsigned int i, char *name, unsigned int len, nv_stats *s) {
	nvdev d; int r = p_handle(i, &d); if (r) return r;
	memset(s, 0, sizeof *s);
	if ((r = p_name(d, name, len))) return r;
	nv_util u; if ((r = p_util(d, &u))) return r;
	s->util = u.gpu;
	// _v2 leaves the driver's reserved memory out of "used", as nvidia-smi does.
	nv_mem2 m2 = { .version = sizeof(nv_mem2) | (2 << 24) };
	if (p_mem2 && p_mem2(d, &m2) == 0) {
		s->mem_used = m2.used; s->mem_total = m2.total;
	} else {
		nv_mem m; if ((r = p_mem(d, &m))) return r;
		s->mem_used = m.used; s->mem_total = m.total;
	}
	if ((r = p_temp(d, 0, &s->temp))) return r; // NVML_TEMPERATURE_GPU
	s->power_ok = p_power && p_power(d, &s->power_mw) == 0;
	s->limit_ok = p_limit && p_limit(d, &s->limit_mw) == 0;
	return 0;
}

// The PIDs using GPU i (compute, then graphics; one PID may be in both).
static int nv_procs(unsigned int i, nv_proc *buf, unsigned int cap) {
	nvdev d; if (p_handle(i, &d)) return 0;
	unsigned int got = 0, n;
	if (p_compute) { n = cap; if (p_compute(d, &n, buf) == 0) got = n; }
	if (p_graphics && got < cap) { n = cap - got; if (p_graphics(d, &n, buf + got) == 0) got += n; }
	return got;
}

// SM utilisation samples since ts (µs); the count is 0 when there are none.
static int nv_putil(unsigned int i, nv_psample *buf, unsigned int cap, unsigned long long ts) {
	nvdev d; if (!p_putil || p_handle(i, &d)) return 0;
	unsigned int n = cap;
	return p_putil(d, buf, &n, ts) == 0 ? (int)n : 0;
}

static int nv_pname(unsigned int pid, char *buf, unsigned int len) {
	return p_pname ? p_pname(pid, buf, len) : NV_MISSING;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

var errNoNVML = errors.New("NVML not available")

// queryNVML reads every NVIDIA GPU through NVML: no process started per
// request. Callers are serialised by handleGPUStats' cacheMu.
func queryNVML() ([]gpuStats, error) {
	if r := C.nv_open(); r == C.NV_MISSING {
		return nil, errNoNVML
	} else if r != 0 {
		return nil, fmt.Errorf("NVML error %d", int(r))
	}
	var buf [96]C.char
	if r := C.nv_driver(&buf[0], C.uint(len(buf))); r != 0 {
		return nil, fmt.Errorf("NVML error %d", int(r))
	}
	driver := C.GoString(&buf[0])
	var n C.uint
	if r := C.nv_count(&n); r != 0 {
		return nil, fmt.Errorf("NVML error %d", int(r))
	}
	if n == 0 {
		return nil, errNoDevice
	}
	gpus := make([]gpuStats, 0, int(n))
	for i := C.uint(0); i < n; i++ {
		var s C.nv_stats
		if r := C.nv_stats_get(i, &buf[0], C.uint(len(buf)), &s); r != 0 {
			return nil, fmt.Errorf("NVML error %d on GPU %d", int(r), int(i))
		}
		g := gpuStats{
			Index:              int(i),
			Name:               C.GoString(&buf[0]),
			DriverVersion:      driver,
			UtilizationPercent: float64(s.util),
			MemoryUsedMiB:      math.Round(float64(s.mem_used) / (1 << 20)),
			MemoryTotalMiB:     math.Round(float64(s.mem_total) / (1 << 20)),
			TemperatureC:       float64(s.temp),
		}
		if s.power_ok != 0 {
			w := math.Round(float64(s.power_mw)/10) / 100
			g.PowerDrawW = &w
		}
		if s.limit_ok != 0 {
			w := float64(s.limit_mw) / 1000
			g.PowerLimitW = &w
		}
		g.Processes = nvmlProcesses(i)
		gpus = append(gpus, g)
	}
	return gpus, nil
}

// nvmlProcesses lists GPU i's processes with their latest SM utilisation
// (the same rows `nvidia-smi pmon -s u` printed).
func nvmlProcesses(i C.uint) []gpuProcess {
	var procs [128]C.nv_proc
	np := int(C.nv_procs(i, &procs[0], C.uint(len(procs))))
	if np == 0 {
		return nil
	}
	var samples [256]C.nv_psample
	// ponytail: a fixed 2 s window, not per-poll timestamps; fine for a widget.
	since := C.ulonglong(time.Now().Add(-2 * time.Second).UnixMicro())
	ns := int(C.nv_putil(i, &samples[0], C.uint(len(samples)), since))
	sm := map[int]float64{}
	latest := map[int]C.ulonglong{}
	for _, s := range samples[:ns] {
		if pid := int(s.pid); s.ts >= latest[pid] {
			latest[pid], sm[pid] = s.ts, float64(s.sm)
		}
	}
	seen := map[int]bool{}
	var out []gpuProcess
	var name [256]C.char
	for _, p := range procs[:np] {
		pid := int(p.pid)
		if seen[pid] {
			continue
		}
		seen[pid] = true
		cmd := ""
		if C.nv_pname(p.pid, &name[0], C.uint(len(name))) == 0 {
			cmd = pmonName(C.GoString(&name[0]))
		}
		out = append(out, gpuProcess{GPU: int(i), PID: pid, Command: cmd, UtilizationPercent: sm[pid]})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].PID < out[b].PID })
	return out
}
