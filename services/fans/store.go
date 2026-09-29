package main

// Persistence: the user's settings (durable, /var/lib/nivaroos/fans) and
// the originals - what each channel looked like before this service took
// it over - kept in /run so they live exactly as long as the boot they
// describe (the firmware re-initialises every fan at power-on).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const (
	modeAuto  = "auto"
	modeFixed = "fixed"
	modeCurve = "curve"

	kindHwmon  = "hwmon"
	kindNVIDIA = "nvidia"
)

type FanConfig struct {
	Label    string  `json:"label,omitempty"`
	Mode     string  `json:"mode"`
	FixedPct int     `json:"fixed_pct,omitempty"`
	Curve    Curve   `json:"curve,omitempty"`
	Source   string  `json:"source,omitempty"`
	MinPct   int     `json:"min_pct,omitempty"`
	HystC    float64 `json:"hysteresis_c,omitempty"`
	// Tach is the fanN_input this pwm drives (set by identify, or pwmN ->
	// fanN by default); TachSeen once it ever read above 0 RPM, which is
	// what makes stall detection meaningful for the channel.
	Tach     int    `json:"tach,omitempty"`
	TachSet  bool   `json:"tach_set,omitempty"`
	TachSeen bool   `json:"tach_seen,omitempty"`
	Alert    string `json:"alert,omitempty"`
	AlertAt  int64  `json:"alert_at,omitempty"`
}

type Settings struct {
	Version int                   `json:"version"`
	CritCPU int                   `json:"critical_cpu_c"`
	CritGPU int                   `json:"critical_gpu_c"`
	Preset  string                `json:"preset"`
	Fans    map[string]*FanConfig `json:"fans"`
}

func defaultSettings() Settings {
	return Settings{Version: 1, CritCPU: DefaultCritCPU, CritGPU: DefaultCritGPU, Preset: modeAuto, Fans: map[string]*FanConfig{}}
}

// original is one channel's state before we took it over.
type original struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	HwmonDir  string `json:"hwmon_dir,omitempty"`
	HwmonName string `json:"hwmon_name,omitempty"`
	DevPath   string `json:"dev_path,omitempty"`
	PWM       int    `json:"pwm,omitempty"`
	Enable    *int64 `json:"enable,omitempty"`
	Value     int64  `json:"value,omitempty"`
	GPUUUID   string `json:"gpu_uuid,omitempty"`
	Fan       int    `json:"fan,omitempty"`
}

type store struct {
	settingsPath  string
	originalsPath string
	mu            sync.Mutex // guards the originals file
}

// writeFileAtomic: temp file, fsync, rename, fsync the directory - the
// originals must survive the crash they exist for.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

func (s *store) loadSettings() Settings {
	st := defaultSettings()
	b, err := os.ReadFile(s.settingsPath)
	if err != nil {
		return st
	}
	if json.Unmarshal(b, &st) != nil {
		return defaultSettings()
	}
	if st.Fans == nil {
		st.Fans = map[string]*FanConfig{}
	}
	st.CritCPU = clampInt(st.CritCPU, MinCritC, MaxCritCPU, DefaultCritCPU)
	st.CritGPU = clampInt(st.CritGPU, MinCritC, MaxCritGPU, DefaultCritGPU)
	for _, fc := range st.Fans {
		sanitizeConfig(fc)
	}
	return st
}

func clampInt(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// sanitizeConfig makes a loaded config safe whatever the file says: a bad
// curve or unknown mode falls back to Auto.
func sanitizeConfig(fc *FanConfig) {
	if fc.MinPct < HardMinPct {
		fc.MinPct = DefaultMinPct
	}
	if fc.MinPct > 100 {
		fc.MinPct = 100
	}
	if fc.HystC < 0 || fc.HystC > MaxHystC {
		fc.HystC = DefaultHystC
	}
	switch fc.Mode {
	case modeFixed:
		fc.FixedPct = clampPct(fc.FixedPct, fc.MinPct)
	case modeCurve:
		if validateCurve(fc.Curve, fc.MinPct) != nil {
			fc.Mode = modeAuto
		}
	default:
		fc.Mode = modeAuto
	}
}

func (s *store) saveSettings(st Settings) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.settingsPath, b, 0o600)
}

func (s *store) loadOriginals() map[string]original {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadOriginalsLocked()
}

func (s *store) loadOriginalsLocked() map[string]original {
	out := map[string]original{}
	b, err := os.ReadFile(s.originalsPath)
	if err != nil {
		return out
	}
	var list []original
	if json.Unmarshal(b, &list) != nil {
		return out
	}
	for _, o := range list {
		out[o.ID] = o
	}
	return out
}

func (s *store) writeOriginalsLocked(m map[string]original) error {
	if len(m) == 0 {
		err := os.Remove(s.originalsPath)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	list := make([]original, 0, len(m))
	for _, o := range m {
		list = append(list, o)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	b, _ := json.MarshalIndent(list, "", "  ")
	return writeFileAtomic(s.originalsPath, b, 0o600)
}

// putOriginal records o unless the channel already has a record (the
// first capture is the real original; a later one would capture our own
// manual setting).
func (s *store) putOriginal(o original) (original, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.loadOriginalsLocked()
	if prev, ok := m[o.ID]; ok {
		return prev, nil
	}
	m[o.ID] = o
	return o, s.writeOriginalsLocked(m)
}

func (s *store) dropOriginal(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.loadOriginalsLocked()
	if _, ok := m[id]; !ok {
		return nil
	}
	delete(m, id)
	return s.writeOriginalsLocked(m)
}
