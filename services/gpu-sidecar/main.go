// recasa-gpu-sidecar exposes NVIDIA GPU stats as JSON for the CasaOS GPU
// dashboard widget, since CasaOS itself has no GPU support. It shells out to
// nvidia-smi on every request rather than polling on a timer, since
// nvidia-smi is fast and this keeps the service stateless.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
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
	out, err := exec.Command("nvidia-smi",
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
	out, err := exec.Command("nvidia-smi", "pmon", "-c", "1", "-s", "u").Output()
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

	stats, err := queryGPU()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(gpuStats{Error: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(stats)
}

func main() {
	addr := flag.String("addr", ":28640", "address to listen on")
	flag.Parse()

	http.HandleFunc("/gpu-stats", handleGPUStats)
	log.Printf("recasa-gpu-sidecar listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
