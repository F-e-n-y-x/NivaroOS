package update

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Everything lives on disk under Dir (never /tmp, which is RAM here), so a
// crash or power loss resumes or reverts deterministically. Core reads
// state.json, settings.json and history.jsonl and writes request.json.
var (
	Dir         = "/var/lib/nivaroos/update"
	ReleaseFile = "/usr/share/nivaroos/release.json"
)

// States (state.json "state").
const (
	StateIdle        = "idle"        // up to date (or never checked)
	StateAvailable   = "available"   // a newer release can be installed
	StateWaiting     = "waiting"     // "Update when they finish": backups still running
	StateDownloading = "downloading" // then verifying
	StateVerifying   = "verifying"
	StateApplying    = "applying" // apply.sh steps: stopping, migrating, installing, health
	StateProbation   = "probation"
	StateRollingBack = "rolling_back"
)

type Settings struct {
	Channel string `json:"channel"` // stable | beta
	Auto    string `json:"auto"`    // install (at night) | notify
	Window  string `json:"window"`  // HH:MM, local time: automatic installs only near it
}

type Result struct {
	At     time.Time `json:"at"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Result string    `json:"result"` // installed | rolled_back | failed | stuck
	Reason string    `json:"reason,omitempty"`
}

type State struct {
	State       string     `json:"state"`
	Step        string     `json:"step,omitempty"`
	Progress    int        `json:"progress,omitempty"` // download percent
	Current     string     `json:"current"`            // "" = a from-source install
	Available   *Available `json:"available,omitempty"`
	LastCheck   time.Time  `json:"last_check,omitzero"`
	LastError   string     `json:"last_error,omitempty"`
	Skipped     string     `json:"skipped,omitempty"` // why tonight's automatic install didn't run
	LastResult  *Result    `json:"last_result,omitempty"`
	BadVersions []string   `json:"bad_versions,omitempty"`
	Notified    string     `json:"notified,omitempty"` // version last announced as available
	Unsupported string     `json:"unsupported,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Request is what core (or the CLI) asks nivaroos-update.service to do.
type Request struct {
	Action      string `json:"action"`         // check | apply | rollback
	Mode        string `json:"mode,omitempty"` // apply: wait (default) | cancel_backups
	RestoreData bool   `json:"restore_data,omitempty"`
}

func p(name string) string { return filepath.Join(Dir, name) }

func readJSON(name string, v any) error {
	b, err := os.ReadFile(p(name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// writeJSON writes atomically (temp file + rename).
func writeJSON(name string, v any) error {
	if err := os.MkdirAll(Dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := p(name + ".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p(name))
}

// LoadSettings: the box's channel defaults to its release's, automatic
// installs at about 04:00 are the default (owner decision 2026-10-16).
func LoadSettings() Settings {
	s := Settings{}
	_ = readJSON("settings.json", &s)
	if s.Channel != "stable" && s.Channel != "beta" {
		s.Channel = currentRelease().Channel
		if s.Channel != "beta" {
			s.Channel = "stable"
		}
	}
	if s.Auto != "notify" {
		s.Auto = "install"
	}
	if _, err := time.Parse("15:04", s.Window); err != nil {
		s.Window = "04:00"
	}
	return s
}

func SaveSettings(s Settings) error { return writeJSON("settings.json", s) }

func LoadState() State {
	var s State
	_ = readJSON("state.json", &s)
	if s.State == "" {
		s.State = StateIdle
	}
	s.Current = currentRelease().Version
	return s
}

func saveState(s *State) error {
	s.UpdatedAt = time.Now()
	return writeJSON("state.json", s)
}

func appendHistory(r Result) {
	f, err := os.OpenFile(p("history.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(r)
	_, _ = f.Write(append(b, '\n'))
}

// History, newest first.
func History() []Result {
	f, err := os.Open(p("history.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Result
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r Result
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append([]Result{r}, out...)
		}
	}
	return out
}

type release struct{ Version, Channel, Commit, Built string }

// currentRelease is release.json ("" version: a from-source install).
func currentRelease() release {
	var r release
	if b, err := os.ReadFile(ReleaseFile); err == nil {
		_ = json.Unmarshal(b, &r)
	}
	return r
}

// lock: one update at a time (UI + CLI + timer). The lock is released when
// the process exits, however it exits.
// ErrBusy: another update (manual, or from Settings) holds the lock.
var ErrBusy = errors.New("an update is already running")

func lock() (*os.File, error) {
	if err := os.MkdirAll(Dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p(".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, ErrBusy
	}
	return f, nil
}
