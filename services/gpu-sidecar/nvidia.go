package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// nvidiaSMITimeout bounds each nvidia-smi invocation so a hung/slow driver
// can't block an HTTP request (and the goroutine serving it) indefinitely.
const nvidiaSMITimeout = 3 * time.Second

const nvidiaQueryFields = "index,name,driver_version,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit"

func runNvidiaSMI(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSMITimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("nvidia-smi: %v: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("nvidia-smi: %w", err)
	}
	return out, nil
}

func queryNVIDIA() (gpuStats, error) {
	out, err := runNvidiaSMI("--query-gpu="+nvidiaQueryFields, "--format=csv,noheader,nounits")
	if err != nil {
		return gpuStats{}, err
	}
	gpus, err := parseNvidiaQuery(string(out))
	if err != nil {
		return gpuStats{}, err
	}
	if pmon, err := runNvidiaSMI("pmon", "-c", "1", "-s", "u"); err == nil {
		procs := parsePmon(string(pmon))
		for i := range gpus {
			for _, p := range procs {
				if p.GPU == gpus[i].Index {
					gpus[i].Processes = append(gpus[i].Processes, p)
				}
			}
		}
	}
	return summarizeGPUs(gpus), nil
}

// summarizeGPUs returns the first GPU's stats at top level (the shape the
// widget consumes) plus the full list and count.
func summarizeGPUs(gpus []gpuStats) gpuStats {
	top := gpus[0]
	top.GPUCount = len(gpus)
	top.GPUs = gpus
	return top
}

// parseNvidiaQuery parses `nvidia-smi --query-gpu=<nvidiaQueryFields>
// --format=csv,noheader,nounits` output: one line per GPU.
func parseNvidiaQuery(out string) ([]gpuStats, error) {
	r := csv.NewReader(strings.NewReader(out))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	want := len(strings.Split(nvidiaQueryFields, ","))
	var gpus []gpuStats
	for line := 1; ; line++ {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("nvidia-smi: unparsable output: %w", err)
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		if len(rec) != want {
			return nil, fmt.Errorf("nvidia-smi: line %d has %d fields, want %d: %q", line, len(rec), want, strings.Join(rec, ","))
		}
		for i := range rec {
			rec[i] = strings.TrimSpace(rec[i])
		}
		idx, err := strconv.Atoi(rec[0])
		if err != nil {
			idx = len(gpus)
		}
		gpus = append(gpus, gpuStats{
			Index:              idx,
			Name:               rec[1],
			DriverVersion:      rec[2],
			UtilizationPercent: numOrZero(rec[3]),
			MemoryUsedMiB:      numOrZero(rec[4]),
			MemoryTotalMiB:     numOrZero(rec[5]),
			TemperatureC:       numOrZero(rec[6]),
			PowerDrawW:         numOrNil(rec[7]),
			PowerLimitW:        numOrNil(rec[8]),
		})
	}
	if len(gpus) == 0 {
		return nil, fmt.Errorf("nvidia-smi reported no GPUs")
	}
	return gpus, nil
}

// numOrNil parses a nvidia-smi number; "[N/A]", "N/A", "[Not Supported]"
// and the like become nil (JSON null).
func numOrNil(s string) *float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &v
}

func numOrZero(s string) float64 {
	if v := numOrNil(s); v != nil {
		return *v
	}
	return 0
}

// parsePmon parses `nvidia-smi pmon -c 1 -s u`. Column positions are taken
// from the "# gpu pid type sm ..." header, since they differ between driver
// versions (older ones have no jpg/ofa columns, newer ones add more).
func parsePmon(out string) []gpuProcess {
	col := map[string]int{}
	var procs []gpuProcess
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			fields := strings.Fields(strings.TrimPrefix(line, "#"))
			if len(col) == 0 && len(fields) > 0 && strings.EqualFold(fields[0], "gpu") {
				for i, f := range fields {
					col[strings.ToLower(f)] = i
				}
			}
			continue
		}
		gpuIdx, okG := col["gpu"]
		pidIdx, okP := col["pid"]
		cmdIdx, okC := col["command"]
		if !okG || !okP || !okC {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) <= cmdIdx {
			continue
		}
		pid, err := strconv.Atoi(fields[pidIdx])
		if err != nil { // "-" = no process on that GPU
			continue
		}
		gpu, _ := strconv.Atoi(fields[gpuIdx])
		p := gpuProcess{GPU: gpu, PID: pid, Command: strings.Join(fields[cmdIdx:], " ")}
		if smIdx, ok := col["sm"]; ok && smIdx < len(fields) {
			p.UtilizationPercent = numOrZero(fields[smIdx])
		}
		procs = append(procs, p)
	}
	return procs
}
