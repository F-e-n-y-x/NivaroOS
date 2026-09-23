package ntfsperm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Run keeps a Watcher on every ntfs3 mount that names an owner, picking up
// drives mounted or unmounted later. The first time a mount point is seen
// its whole tree is repaired once (in the background) - that is what
// fixes files written before the watcher existed; statePath remembers
// which mount points are done so a restart doesn't walk terabytes again.
func Run(ctx context.Context, statePath string, logf func(format string, args ...any)) {
	watchers := map[string]*Watcher{}
	defer func() {
		for _, w := range watchers {
			w.Close()
		}
	}()
	repaired := loadState(statePath)
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		current := map[string]Mount{}
		for _, m := range Mounts() {
			current[m.Point] = m
		}
		for point, w := range watchers {
			if _, ok := current[point]; !ok {
				w.Close()
				delete(watchers, point)
			}
		}
		for point, m := range current {
			if watchers[point] != nil {
				continue
			}
			w, err := Start(point, m.Policy)
			if err != nil {
				logf("ntfsperm: can't watch %s: %v", point, err)
				continue
			}
			watchers[point] = w
			logf("ntfsperm: keeping %s owned by %d:%d", point, m.Policy.UID, m.Policy.GID)
			if !repaired[point] {
				repaired[point] = true
				go func(m Mount) {
					n, err := Repair(ctx, m.Point, m.Policy)
					logf("ntfsperm: repaired %d entries on %s (err=%v)", n, m.Point, err)
					if err == nil {
						saveState(statePath, m.Point)
					}
				}(m)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func loadState(path string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		for _, p := range list {
			out[p] = true
		}
	}
	return out
}

func saveState(path, point string) {
	st := loadState(path)
	st[point] = true
	list := make([]string, 0, len(st))
	for p := range st {
		list = append(list, p)
	}
	raw, _ := json.Marshal(list)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}
