// nivaroos-gpu-sidecar exposes NVIDIA GPU stats as JSON for the NivaroOS GPU
// dashboard widget, since NivaroOS itself has no GPU support. It shells out to
// nvidia-smi on every request rather than polling on a timer, since
// nvidia-smi is fast and this keeps the service stateless.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// nvidiaSMITimeout bounds each nvidia-smi invocation so a hung/slow driver
// can't block an HTTP request (and the goroutine serving it) indefinitely.
const nvidiaSMITimeout = 3 * time.Second

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

type gpuStats struct {
	Name               string       `json:"name"`
	DriverVersion      string       `json:"driver_version"`
	UtilizationPercent float64      `json:"utilization_percent"`
	MemoryUsedMiB      float64      `json:"memory_used_mib"`
	MemoryTotalMiB     float64      `json:"memory_total_mib"`
	TemperatureC       float64      `json:"temperature_c"`
	PowerDrawW         float64      `json:"power_draw_w"`
	PowerLimitW        float64      `json:"power_limit_w"`
	Processes          []gpuProcess `json:"processes"`
	Error              string       `json:"error,omitempty"`
}

type gpuProcess struct {
	PID                int     `json:"pid"`
	Command            string  `json:"command"`
	UtilizationPercent float64 `json:"utilization_percent"`
}

func queryGPU() (gpuStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSMITimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,driver_version,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return gpuStats{}, err
	}
	fields := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(fields) != 8 {
		return gpuStats{}, err
	}
	parse := func(s string) float64 {
		v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v
	}
	stats := gpuStats{
		Name:               strings.TrimSpace(fields[0]),
		DriverVersion:      strings.TrimSpace(fields[1]),
		UtilizationPercent: parse(fields[2]),
		MemoryUsedMiB:      parse(fields[3]),
		MemoryTotalMiB:     parse(fields[4]),
		TemperatureC:       parse(fields[5]),
		PowerDrawW:         parse(fields[6]),
		PowerLimitW:        parse(fields[7]),
	}
	stats.Processes = queryProcesses()
	return stats, nil
}

// queryProcesses uses `nvidia-smi pmon` (per-process monitoring) rather than
// query-compute-apps, since pmon reports per-process utilization % directly
// (query-compute-apps only reports memory, not utilization).
func queryProcesses() []gpuProcess {
	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSMITimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi", "pmon", "-c", "1", "-s", "u").Output()
	if err != nil {
		return nil
	}
	var procs []gpuProcess
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		// gpu pid type sm mem enc dec jpg ofa command
		if len(fields) < 10 || fields[1] == "-" {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		util, _ := strconv.ParseFloat(fields[3], 64)
		procs = append(procs, gpuProcess{
			PID:                pid,
			Command:            fields[9],
			UtilizationPercent: util,
		})
	}
	return procs
}

func handleGPUStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	cacheMu.Lock()
	if cacheGood && time.Since(cachedAt) < cacheTTL {
		stats := cached
		cacheMu.Unlock()
		json.NewEncoder(w).Encode(stats)
		return
	}
	cacheMu.Unlock()

	stats, err := queryGPU()
	if err != nil {
		cacheMu.Lock()
		cacheGood = false
		cacheMu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(gpuStats{Error: err.Error()})
		return
	}

	cacheMu.Lock()
	cached = stats
	cachedAt = time.Now()
	cacheGood = true
	cacheMu.Unlock()

	json.NewEncoder(w).Encode(stats)
}

// driverScriptPath is where installer/install.sh copies
// installer/gpu-driver-install.sh during setup (install_uninstall_wrapper) -
// shelling out to it here rather than reimplementing detection in Go keeps
// there being exactly one place (that script) that knows how to detect a
// GPU vendor and install its driver across every supported distro.
const driverScriptPath = "/usr/local/bin/nivaroos-gpu-driver-install.sh"

// driverStatusTimeout only needs to cover an lspci call and a couple of
// lsmod/nvidia-smi checks - all fast, local, no network - so this stays
// short deliberately, unlike driverInstallTimeout.
const driverStatusTimeout = 10 * time.Second

// driverInstallTimeout has to cover a real package manager install (apt
// update + install a driver package, possibly pulling in a fair amount of
// data) - this is the one gpu-sidecar request that's expected to take a
// while, not something to make snappy.
const driverInstallTimeout = 5 * time.Minute

func handleDriverStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(context.Background(), driverStatusTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "bash", driverScriptPath, "--status").Output()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	// The script already prints a well-formed {"gpus":[...]} JSON object -
	// pass it straight through rather than re-modeling it into a Go struct
	// just to re-serialize the same shape back out.
	w.Write(out)
}

func handleDriverInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST required"})
		return
	}

	var body struct {
		Vendor string `json:"vendor"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // vendor is optional - empty means auto-detect

	ctx, cancel := context.WithTimeout(context.Background(), driverInstallTimeout)
	defer cancel()

	args := []string{driverScriptPath}
	if body.Vendor != "" {
		args = append(args, "--vendor="+body.Vendor)
	}
	out, err := exec.CommandContext(ctx, "bash", args...).CombinedOutput()

	success := err == nil
	w.WriteHeader(http.StatusOK) // the install attempt itself always completed - failure is reported in the body, not the transport
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": success,
		"output":  string(out),
	})
}

func main() {
	addr := flag.String("addr", ":28640", "address to listen on")
	flag.Parse()

	http.HandleFunc("/gpu-stats", handleGPUStats)
	http.HandleFunc("/driver-status", handleDriverStatus)
	http.HandleFunc("/driver-install", handleDriverInstall)
	log.Printf("nivaroos-gpu-sidecar listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
