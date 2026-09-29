package main

// Temperatures the control loop steers by. Only two kinds are trusted: the
// CPU package temperature (k10temp, zenpower, coretemp, the Pi's
// cpu_thermal) and each GPU's own core temperature. Super-I/O chips also
// report "SYSTIN", "AUXTINn" and so on, but those are often unconnected
// inputs that read 0, 109 or 3892313987 °C - steering (or triggering the
// emergency) by them would be wrong, so they are never offered.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// A reading outside this range is a broken or unconnected sensor.
const (
	minSaneC = -20.0
	maxSaneC = 150.0
)

func saneC(c float64) bool { return c > minSaneC && c < maxSaneC }

// readTempC reads a millidegree sysfs file.
func readTempC(path string) (float64, bool) {
	v, err := readInt(path)
	if err != nil {
		return 0, false
	}
	c := float64(v) / 1000
	if !saneC(c) || v == 0 {
		return 0, false
	}
	return c, true
}

type tempInput struct {
	Label string
	Path  string
}

func tempInputs(dir string) []tempInput {
	var out []tempInput
	for n := 1; n <= 32; n++ {
		in := filepath.Join(dir, fmt.Sprintf("temp%d_input", n))
		if !exists(in) {
			continue
		}
		lbl, _ := readTrim(filepath.Join(dir, fmt.Sprintf("temp%d_label", n)))
		out = append(out, tempInput{Label: lbl, Path: in})
	}
	return out
}

// cpuSensor is where the CPU temperature is read from.
type cpuSensor struct {
	Source string   // e.g. "k10temp Tctl"
	Paths  []string // the highest sane value of these is the CPU temperature
}

// findCPUSensor picks the CPU package sensor, most specific first.
func findCPUSensor(sysRoot string, devs []hwmonDev) *cpuSensor {
	byName := map[string][]hwmonDev{}
	for _, d := range devs {
		byName[d.Name] = append(byName[d.Name], d)
	}
	pick := func(name string, labels ...string) *cpuSensor {
		var paths []string
		used := ""
		for _, d := range byName[name] {
			ins := tempInputs(d.Dir)
			for _, want := range labels {
				for _, in := range ins {
					if want == "*" || strings.EqualFold(in.Label, want) || (strings.HasSuffix(want, "*") && strings.HasPrefix(in.Label, strings.TrimSuffix(want, "*"))) {
						paths = append(paths, in.Path)
						if used == "" {
							used = in.Label
						}
					}
				}
				if len(paths) > 0 {
					break
				}
			}
		}
		if len(paths) == 0 {
			return nil
		}
		return &cpuSensor{Source: strings.TrimSpace(name + " " + used), Paths: paths}
	}
	for _, try := range []func() *cpuSensor{
		func() *cpuSensor { return pick("zenpower", "Tdie", "Tctl") },
		func() *cpuSensor { return pick("k10temp", "Tdie", "Tctl", "*") },
		func() *cpuSensor { return pick("coretemp", "Package id*", "*") },
		func() *cpuSensor { return pick("cpu_thermal", "*") },
		func() *cpuSensor { return pick("cpu-thermal", "*") },
		func() *cpuSensor { return pick("soc_thermal", "*") },
	} {
		if s := try(); s != nil {
			return s
		}
	}
	// Thermal zones (ARM boards, some Intel systems without coretemp).
	zones, _ := filepath.Glob(filepath.Join(sysRoot, "class", "thermal", "thermal_zone*"))
	sort.Strings(zones)
	for _, want := range []string{"x86_pkg_temp", "cpu-thermal", "cpu_thermal", "soc-thermal", "cpu"} {
		for _, z := range zones {
			t, _ := readTrim(filepath.Join(z, "type"))
			if t == want {
				return &cpuSensor{Source: "thermal zone " + t, Paths: []string{filepath.Join(z, "temp")}}
			}
		}
	}
	return nil
}

func (s *cpuSensor) read() (float64, bool) {
	if s == nil {
		return 0, false
	}
	best, ok := 0.0, false
	for _, p := range s.Paths {
		if c, good := readTempC(p); good && (!ok || c > best) {
			best, ok = c, true
		}
	}
	return best, ok
}

// amdgpuTemp reads an amdgpu's edge temperature (the one comparable with a
// GPU "core" temperature limit; junction/hotspot runs 10-20 °C higher).
func amdgpuTemp(d hwmonDev) (float64, bool) {
	ins := tempInputs(d.Dir)
	for _, in := range ins {
		if strings.EqualFold(in.Label, "edge") {
			return readTempC(in.Path)
		}
	}
	if len(ins) > 0 {
		return readTempC(ins[0].Path)
	}
	return 0, false
}

func gpuSourceAMD(d hwmonDev) string { return "gpu:amdgpu-" + slug(d.DevBase) }
func gpuSourceNV(uuid string) string { return "gpu:nv-" + slug(uuid) }
