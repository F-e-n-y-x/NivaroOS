package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// History is the lite browser's visit history, kept server-side (like the
// downloads list) so it survives reloads and is the same from every device
// that opens Download Station.
type HistoryEntry struct {
	ID    string    `json:"id"`
	URL   string    `json:"url"`
	Title string    `json:"title"`
	Time  time.Time `json:"time"`
}

const maxHistory = 5000

type History struct {
	mu      sync.Mutex
	path    string
	entries []HistoryEntry // newest first
	dirty   bool
}

func NewHistory(dataDir string) *History {
	h := &History{path: filepath.Join(dataDir, "history.json")}
	if raw, err := os.ReadFile(h.path); err == nil {
		_ = json.Unmarshal(raw, &h.entries)
	}
	go func() {
		for range time.Tick(5 * time.Second) {
			h.flush()
		}
	}()
	return h
}

func (h *History) flush() {
	h.mu.Lock()
	if !h.dirty {
		h.mu.Unlock()
		return
	}
	snapshot := append([]HistoryEntry(nil), h.entries...)
	h.dirty = false
	h.mu.Unlock()
	_ = writeJSONAtomic(h.path, snapshot)
}

// Add records a visit. A revisit of the page that's already the most
// recent entry (a reload, a title update arriving after load) just
// refreshes that entry instead of adding a duplicate.
func (h *History) Add(rawURL, title string) (HistoryEntry, bool) {
	u, err := validateDownloadURL(rawURL)
	if err != nil {
		return HistoryEntry{}, false
	}
	title = truncateRunes(strings.ToValidUTF8(title, ""), 300)
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if len(h.entries) > 0 && h.entries[0].URL == u.String() && now.Sub(h.entries[0].Time) < 30*time.Minute {
		if title != "" {
			h.entries[0].Title = title
		}
		h.entries[0].Time = now
		h.dirty = true
		return h.entries[0], true
	}
	e := HistoryEntry{ID: newID(), URL: u.String(), Title: title, Time: now}
	h.entries = append([]HistoryEntry{e}, h.entries...)
	if len(h.entries) > maxHistory {
		h.entries = h.entries[:maxHistory]
	}
	h.dirty = true
	return e, true
}

func (h *History) List(query string, limit int) []HistoryEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []HistoryEntry{}
	for _, e := range h.entries {
		if q != "" && !strings.Contains(strings.ToLower(e.URL), q) && !strings.Contains(strings.ToLower(e.Title), q) {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (h *History) Delete(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, e := range h.entries {
		if e.ID == id {
			h.entries = append(h.entries[:i], h.entries[i+1:]...)
			h.dirty = true
			return
		}
	}
}

func (h *History) Clear() {
	h.mu.Lock()
	h.entries = nil
	h.dirty = true
	h.mu.Unlock()
	h.flush()
}

// truncateRunes cuts s to at most n characters without splitting a
// multi-byte UTF-8 sequence (a byte slice would leave a broken rune at the
// end of, say, a CJK or emoji title).
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}
