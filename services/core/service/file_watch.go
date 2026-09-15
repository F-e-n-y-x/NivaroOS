package service

import (
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// The Files app used to only ever refresh a folder's listing after an
// action the current browser tab itself initiated (upload/paste/delete, via
// the RELOAD_FILE_LIST event, or the nivaroos:file:operate broadcast for
// in-flight copy/move tasks) - a change made any other way (another device
// over Samba, a scheduled backup task, a companion sync, a second browser
// tab) never showed up until the user manually reloaded. This watches
// whatever directories are actually being viewed and broadcasts a change
// notice so open Files windows can refresh themselves automatically.
//
// Deliberately not a recursive watch of /DATA (or of every path anyone has
// ever visited): only the single directory currently open in some Files
// window is watched, one level deep, evicting the least-recently-viewed
// entry once maxWatchedDirs is hit. inotify watches are cheap but not
// free, and a long-running server that's ever browsed thousands of distinct
// folders over months must not slowly exhaust the kernel's
// fs.inotify.max_user_watches and start breaking every other inotify
// consumer on the box (this one included).
const (
	maxWatchedDirs  = 256
	changeDebounce  = 300 * time.Millisecond
	changedEvent    = "nivaroos:file:changed"
	watchedFsEvents = fsnotify.Create | fsnotify.Remove | fsnotify.Rename | fsnotify.Write
)

type dirWatcher struct {
	mu       sync.Mutex
	watcher  *fsnotify.Watcher
	lastUsed map[string]time.Time
	timers   map[string]*time.Timer
	started  bool
}

var fileWatcher = &dirWatcher{
	lastUsed: make(map[string]time.Time),
	timers:   make(map[string]*time.Timer),
}

// EnsureDirWatch registers path for live-change notifications if it isn't
// already watched, and marks it as most-recently-used either way. Always
// best-effort: a path fsnotify can't watch (a FUSE cloud-drive mount that
// doesn't implement inotify, a permissions issue, a path that's since
// disappeared) just means that folder doesn't get live updates - it must
// never fail or slow down the directory listing itself.
func EnsureDirWatch(path string) {
	if path == "" {
		return
	}
	fileWatcher.mu.Lock()
	defer fileWatcher.mu.Unlock()

	if !fileWatcher.started {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			logger.Error("file watch: failed to start fsnotify watcher", zap.Error(err))
			return
		}
		fileWatcher.watcher = w
		fileWatcher.started = true
		go fileWatcher.run()
	}

	now := time.Now()
	if _, ok := fileWatcher.lastUsed[path]; ok {
		fileWatcher.lastUsed[path] = now
		return
	}

	if len(fileWatcher.lastUsed) >= maxWatchedDirs {
		fileWatcher.evictOldestLocked()
	}

	if err := fileWatcher.watcher.Add(path); err != nil {
		// Not watchable - fine, just don't track it (a retry on every
		// listing request for an unwatchable path would be pure overhead).
		return
	}
	fileWatcher.lastUsed[path] = now
}

func (d *dirWatcher) evictOldestLocked() {
	var oldestPath string
	var oldestAt time.Time
	first := true
	for p, t := range d.lastUsed {
		if first || t.Before(oldestAt) {
			oldestPath, oldestAt = p, t
			first = false
		}
	}
	if oldestPath == "" {
		return
	}
	_ = d.watcher.Remove(oldestPath)
	delete(d.lastUsed, oldestPath)
	if timer, ok := d.timers[oldestPath]; ok {
		timer.Stop()
		delete(d.timers, oldestPath)
	}
}

func (d *dirWatcher) run() {
	for {
		select {
		case event, ok := <-d.watcher.Events:
			if !ok {
				return
			}
			if event.Op&watchedFsEvents == 0 {
				continue
			}
			d.scheduleBroadcast(watchDirOf(event.Name))
		case err, ok := <-d.watcher.Errors:
			if !ok {
				return
			}
			logger.Error("file watch: fsnotify error", zap.Error(err))
		}
	}
}

// scheduleBroadcast coalesces a burst of events on the same directory (a
// bulk copy/extract can fire dozens of Create/Write events in a few
// milliseconds) into a single broadcast, changeDebounce after the last one.
func (d *dirWatcher) scheduleBroadcast(dir string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// The directory may have been evicted between the fsnotify event
	// arriving and this running - only debounce-track/broadcast for
	// directories still actually being watched.
	if _, ok := d.lastUsed[dir]; !ok {
		return
	}

	if timer, ok := d.timers[dir]; ok {
		timer.Reset(changeDebounce)
		return
	}
	d.timers[dir] = time.AfterFunc(changeDebounce, func() {
		d.mu.Lock()
		delete(d.timers, dir)
		d.mu.Unlock()
		MyService.Notify().SendNotify(changedEvent, map[string]interface{}{"path": dir})
	})
}

// watchDirOf returns the directory a watched path's event belongs to.
// fsnotify.Watcher watches are non-recursive and report the exact path
// added, so event.Name is already that directory's own child - the
// directory itself is everything but the final path segment.
func watchDirOf(eventPath string) string {
	idx := -1
	for i := len(eventPath) - 1; i >= 0; i-- {
		if eventPath[i] == '/' {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return "/"
	}
	return eventPath[:idx]
}
