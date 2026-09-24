// nivaroos-gpu-sidecar exposes GPU stats and GPU driver status/install as
// JSON for the NivaroOS GPU dashboard widget.
//
// It listens on 127.0.0.1:28640 only. The UI reaches it same-origin through
// the gateway at /v1/gpu/<endpoint> (prefix stripped, see
// services/gateway/route/gateway_route.go), and every request must carry
// the user's JWT (Authorization header or ?token=) - the gateway does not
// authenticate proxied requests itself. Same-host automation (a script on
// this box calling 127.0.0.1:28640 directly) may skip the token, see
// requireAuth.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/constants"
)

// cacheTTL reuses the last successful reading for a short window so that
// several near-simultaneous polls (multiple open dashboard tabs, or the
// widget's own poll racing a manual refresh) don't each spawn a fresh pair
// of nvidia-smi subprocesses.
const cacheTTL = 1 * time.Second

var (
	cacheMu   sync.Mutex
	cached    gpuStats
	cachedAt  time.Time
	cacheGood bool
)

// gpuStats is the response of GET /gpu-stats. The top-level fields describe
// the first GPU (the shape the widget has always consumed); GPUCount and
// GPUs describe every GPU found. PowerDrawW/PowerLimitW are null when the
// driver reports them as unavailable ("[N/A]").
type gpuStats struct {
	Index              int          `json:"index"`
	Name               string       `json:"name"`
	DriverVersion      string       `json:"driver_version"`
	UtilizationPercent float64      `json:"utilization_percent"`
	MemoryUsedMiB      float64      `json:"memory_used_mib"`
	MemoryTotalMiB     float64      `json:"memory_total_mib"`
	TemperatureC       float64      `json:"temperature_c"`
	PowerDrawW         *float64     `json:"power_draw_w"`
	PowerLimitW        *float64     `json:"power_limit_w"`
	Processes          []gpuProcess `json:"processes"`
	GPUCount           int          `json:"gpu_count,omitempty"`
	GPUs               []gpuStats   `json:"gpus,omitempty"`
	Error              string       `json:"error,omitempty"`
}

type gpuProcess struct {
	GPU                int     `json:"gpu"`
	PID                int     `json:"pid"`
	Command            string  `json:"command"`
	UtilizationPercent float64 `json:"utilization_percent"`
}

// queryGPU tries each vendor in turn and returns the first one that
// actually has a device present. nvidia-smi returning "command not found"
// on a non-NVIDIA machine fails near-instantly (it's an exec lookup
// failure, not a timeout), so trying it unconditionally first costs
// nothing measurable on AMD/Intel-only systems - see vendor.go for the
// AMD/Intel implementations.
func queryGPU() (gpuStats, error) {
	nvStats, nvErr := queryNVIDIA()
	if nvErr == nil {
		return nvStats, nil
	}
	if stats, err := queryAMD(); err == nil {
		return stats, nil
	}
	if stats, err := queryIntel(); err == nil {
		return stats, nil
	} else if err != errNoDevice {
		return gpuStats{}, err
	}
	// nvidia-smi exists but failed (driver mismatch, parse error, ...):
	// report that rather than a generic "no device".
	if _, lookErr := exec.LookPath("nvidia-smi"); lookErr == nil {
		return gpuStats{}, nvErr
	}
	return gpuStats{}, errNoDevice
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func handleGPUStats(w http.ResponseWriter, r *http.Request) {
	cacheMu.Lock()
	defer cacheMu.Unlock() // also serializes refreshes: one nvidia-smi at a time

	if cacheGood && time.Since(cachedAt) < cacheTTL {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	stats, err := queryGPU()
	if err != nil {
		cacheGood = false
		writeJSON(w, http.StatusServiceUnavailable, gpuStats{Error: err.Error()})
		return
	}
	cached, cachedAt, cacheGood = stats, time.Now(), true
	writeJSON(w, http.StatusOK, stats)
}

// driverScriptPath is where installer/install.sh copies
// installer/gpu-driver-install.sh during setup (install_uninstall_wrapper) -
// shelling out to it here rather than reimplementing detection in Go keeps
// there being exactly one place (that script) that knows how to detect a
// GPU vendor and install its driver across every supported distro.
const driverScriptPath = "/usr/local/bin/nivaroos-gpu-driver-install.sh"

// driverStatusTimeout only needs to cover an lspci call and a couple of
// lsmod/nvidia-smi checks - all fast, local, no network.
const driverStatusTimeout = 10 * time.Second

// driverStatusTTL: the driver status only changes when a driver is
// installed (which invalidates the cache) or the machine is reconfigured,
// so the widget's polling doesn't need to spawn bash+lspci+nvidia-smi each
// time.
const driverStatusTTL = 5 * time.Minute

// driverInstallTimeout has to cover a real package manager install.
const driverInstallTimeout = 5 * time.Minute

var (
	driverMu       sync.Mutex // guards the fields below and serializes refreshes
	driverStatus   []byte
	driverStatusAt time.Time

	installMu sync.Mutex // one driver install at a time
)

func invalidateDriverStatus() {
	driverMu.Lock()
	driverStatus = nil
	driverMu.Unlock()
}

func handleDriverStatus(w http.ResponseWriter, r *http.Request) {
	driverMu.Lock()
	defer driverMu.Unlock()

	if driverStatus == nil || time.Since(driverStatusAt) >= driverStatusTTL || r.URL.Query().Get("refresh") == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), driverStatusTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "bash", driverScriptPath, "--status").Output()
		if err != nil || !json.Valid(out) {
			msg := "invalid status output"
			if err != nil {
				msg = err.Error()
			}
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": msg})
			return
		}
		driverStatus, driverStatusAt = out, time.Now()
	}
	// The script already prints a well-formed {"gpus":[...]} JSON object -
	// pass it straight through.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(driverStatus)
}

var allowedVendors = map[string]bool{"": true, "nvidia": true, "amd": true, "intel": true}

func handleDriverInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}

	var body struct {
		Vendor string `json:"vendor"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) // vendor is optional - empty means auto-detect
	if !allowedVendors[body.Vendor] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "vendor must be one of nvidia, amd, intel (or empty)"})
		return
	}

	if !installMu.TryLock() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a driver install is already running"})
		return
	}
	defer installMu.Unlock()
	defer invalidateDriverStatus()

	ctx, cancel := context.WithTimeout(context.Background(), driverInstallTimeout)
	defer cancel()

	args := []string{driverScriptPath}
	if body.Vendor != "" {
		args = append(args, "--vendor="+body.Vendor)
	}
	out, err := exec.CommandContext(ctx, "bash", args...).CombinedOutput()

	// the install attempt itself always completed - failure is reported in
	// the body, not the transport
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": err == nil,
		"output":  string(out),
	})
}

func main() {
	addr := flag.String("addr", "127.0.0.1:28640", "address to listen on (keep it loopback: the UI reaches this through the gateway at /v1/gpu/)")
	runtimePath := flag.String("runtime-path", constants.DefaultRuntimePath, "NivaroOS runtime directory (for locating user-service's JWKS endpoint)")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/gpu-stats", handleGPUStats)
	mux.HandleFunc("/driver-status", handleDriverStatus)
	mux.HandleFunc("/driver-install", handleDriverInstall)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           requireAuth(mux, *runtimePath),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("nivaroos-gpu-sidecar listening on %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
