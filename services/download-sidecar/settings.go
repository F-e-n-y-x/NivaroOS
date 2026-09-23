package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings is everything the user can change from the Download Station
// settings pane - one small JSON file, rewritten whole on every change.
type Settings struct {
	DefaultDir         string `json:"default_dir"`
	DefaultConnections int    `json:"default_connections"`
	MaxConcurrent      int    `json:"max_concurrent"`
	// Bytes per second across every active download combined; 0 = unlimited.
	SpeedLimit int64 `json:"speed_limit"`

	AdblockEnabled bool `json:"adblock_enabled"`
	// Filter list IDs (see builtinFilterLists) the user has switched on.
	EnabledLists []string `json:"enabled_lists"`
	// uBO "My filters" - one filter per line, same syntax as the lists.
	CustomFilters string `json:"custom_filters"`
	// uBO's "trusted sites" / per-site power button: hostnames where the
	// blocker is off entirely.
	AllowedSites []string `json:"allowed_sites"`
	// The lite browser's start page.
	HomePage string `json:"home_page"`
}

const (
	minConnections = 1
	maxConnections = 32
)

func defaultSettings(defaultDir string) Settings {
	return Settings{
		DefaultDir:         defaultDir,
		DefaultConnections: 8,
		MaxConcurrent:      3,
		AdblockEnabled:     true,
		EnabledLists:       defaultEnabledLists(),
		HomePage:           "https://duckduckgo.com/",
	}
}

func (s *Settings) normalize(defaultDir string) {
	if s.DefaultDir == "" {
		s.DefaultDir = defaultDir
	}
	s.DefaultConnections = clampInt(s.DefaultConnections, minConnections, maxConnections)
	if s.MaxConcurrent < 1 {
		s.MaxConcurrent = 1
	}
	if s.MaxConcurrent > 20 {
		s.MaxConcurrent = 20
	}
	if s.SpeedLimit < 0 {
		s.SpeedLimit = 0
	}
	if s.EnabledLists == nil {
		s.EnabledLists = []string{}
	}
	if s.AllowedSites == nil {
		s.AllowedSites = []string{}
	}
}

type SettingsStore struct {
	mu         sync.RWMutex
	path       string
	defaultDir string
	s          Settings
	listeners  []func(Settings)
}

func NewSettingsStore(dataDir, defaultDir string) *SettingsStore {
	st := &SettingsStore{path: filepath.Join(dataDir, "settings.json"), defaultDir: defaultDir, s: defaultSettings(defaultDir)}
	if raw, err := os.ReadFile(st.path); err == nil {
		loaded := defaultSettings(defaultDir)
		if json.Unmarshal(raw, &loaded) == nil {
			st.s = loaded
		}
	}
	st.s.normalize(defaultDir)
	return st
}

func (st *SettingsStore) Get() Settings {
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := st.s
	out.EnabledLists = append([]string(nil), st.s.EnabledLists...)
	out.AllowedSites = append([]string(nil), st.s.AllowedSites...)
	return out
}

// OnChange registers a callback run (outside the lock) after every Update.
func (st *SettingsStore) OnChange(fn func(Settings)) {
	st.mu.Lock()
	st.listeners = append(st.listeners, fn)
	st.mu.Unlock()
}

func (st *SettingsStore) Update(mutate func(*Settings)) (Settings, error) {
	st.mu.Lock()
	next := st.s
	next.EnabledLists = append([]string(nil), st.s.EnabledLists...)
	next.AllowedSites = append([]string(nil), st.s.AllowedSites...)
	mutate(&next)
	next.normalize(st.defaultDir)
	if err := writeJSONAtomic(st.path, next); err != nil {
		st.mu.Unlock()
		return st.Get(), err
	}
	st.s = next
	listeners := append([]func(Settings){}, st.listeners...)
	st.mu.Unlock()
	for _, fn := range listeners {
		fn(next)
	}
	return next, nil
}

// writeJSONAtomic writes via a temp file + rename so a crash or power loss
// mid-write can never leave a truncated state file behind. 0600 because
// downloads.json holds captured cookies.
func writeJSONAtomic(path string, v interface{}) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
