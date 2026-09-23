package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Job history survives restarts, so a job the service was killed in the
// middle of shows up as "interrupted" (with Retry) instead of vanishing or
// looking finished.

func (m *Manager) save() {
	if m.opts.StatePath == "" {
		return
	}
	jobs := m.List()
	raw, err := json.Marshal(jobs)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(m.opts.StatePath), 0o755)
	tmp := m.opts.StatePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, m.opts.StatePath)
}

func (m *Manager) load() {
	if m.opts.StatePath == "" {
		return
	}
	raw, err := os.ReadFile(m.opts.StatePath)
	if err != nil {
		return
	}
	var jobs []Job
	if json.Unmarshal(raw, &jobs) != nil {
		return
	}
	now := time.Now()
	for _, s := range jobs {
		if !s.State.Terminal() {
			s.State = StateInterrupted
			s.FinishedAt = &now
			s.Speed = 0
			s.Current = ""
		}
		if s.Failures == nil {
			s.Failures = []Failure{}
		}
		j := &job{Job: s}
		m.jobs[s.ID] = j
		m.order = append(m.order, s.ID)
	}
}

// removeLocalTree removes a local file or folder (used after a remote
// transfer reported full success).
func removeLocalTree(p string) error {
	return os.RemoveAll(p)
}
