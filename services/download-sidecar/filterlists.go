package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// FilterList is one of uBlock Origin's stock filter lists - the same set,
// from the same URLs, uBO itself ships enabled by default (plus a couple of
// optional ones it offers).
type FilterList struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	URL         string `json:"url"`
	DefaultOn   bool   `json:"default_on"`
	Description string `json:"description"`
}

var builtinFilterLists = []FilterList{
	{ID: "ublock-filters", Name: "uBlock filters – Ads, trackers, and more", Group: "Built-in", URL: "https://ublockorigin.github.io/uAssets/filters/filters.min.txt", DefaultOn: true, Description: "uBlock Origin's own core list."},
	{ID: "ublock-privacy", Name: "uBlock filters – Privacy", Group: "Built-in", URL: "https://ublockorigin.github.io/uAssets/filters/privacy.min.txt", DefaultOn: true, Description: "Trackers and fingerprinting."},
	{ID: "ublock-badware", Name: "uBlock filters – Badware risks", Group: "Built-in", URL: "https://ublockorigin.github.io/uAssets/filters/badware.min.txt", DefaultOn: true, Description: "Fake download buttons, scam and malware sites."},
	{ID: "ublock-unbreak", Name: "uBlock filters – Unbreak", Group: "Built-in", URL: "https://ublockorigin.github.io/uAssets/filters/unbreak.min.txt", DefaultOn: true, Description: "Exceptions that fix sites other lists break."},
	{ID: "ublock-quick-fixes", Name: "uBlock filters – Quick fixes", Group: "Built-in", URL: "https://ublockorigin.github.io/uAssets/filters/quick-fixes.min.txt", DefaultOn: true, Description: "Short-lived fixes pushed between releases."},
	{ID: "easylist", Name: "EasyList", Group: "Ads", URL: "https://ublockorigin.github.io/uAssets/thirdparties/easylist.txt", DefaultOn: true, Description: "The most widely used ad-blocking list."},
	{ID: "easyprivacy", Name: "EasyPrivacy", Group: "Privacy", URL: "https://ublockorigin.github.io/uAssets/thirdparties/easyprivacy.txt", DefaultOn: true, Description: "Tracking scripts and pixels."},
	{ID: "urlhaus", Name: "Online Malicious URL Blocklist", Group: "Malware protection", URL: "https://malware-filter.gitlab.io/malware-filter/urlhaus-filter-ag-online.txt", DefaultOn: true, Description: "Active malware distribution URLs (abuse.ch URLhaus)."},
	{ID: "peter-lowe", Name: "Peter Lowe's Ad and tracking server list", Group: "Multipurpose", URL: "https://pgl.yoyo.org/adservers/serverlist.php?hostformat=hosts&showintro=0&mimetype=plaintext", DefaultOn: true, Description: "Compact hostname blocklist."},
	{ID: "ublock-annoyances", Name: "uBlock filters – Annoyances", Group: "Annoyances", URL: "https://ublockorigin.github.io/uAssets/filters/annoyances.min.txt", DefaultOn: false, Description: "Cookie notices, pop-ups, newsletter prompts."},
	{ID: "easylist-cookie", Name: "EasyList – Cookie Notices", Group: "Annoyances", URL: "https://ublockorigin.github.io/uAssets/thirdparties/easylist-cookies.txt", DefaultOn: false, Description: "Hides cookie consent banners."},
}

func defaultEnabledLists() []string {
	var ids []string
	for _, l := range builtinFilterLists {
		if l.DefaultOn {
			ids = append(ids, l.ID)
		}
	}
	return ids
}

func findList(id string) (FilterList, bool) {
	for _, l := range builtinFilterLists {
		if l.ID == id {
			return l, true
		}
	}
	return FilterList{}, false
}

// Lists are refreshed on this cadence - uBO's own default is every few
// days for most lists.
const listMaxAge = 4 * 24 * time.Hour

type ListStatus struct {
	FilterList
	Enabled   bool       `json:"enabled"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	Size      int64      `json:"size"`
	Error     string     `json:"error,omitempty"`
}

type AdblockStats struct {
	Enabled       bool         `json:"enabled"`
	NetworkRules  int          `json:"network_rules"`
	CosmeticRules int          `json:"cosmetic_rules"`
	BlockedTotal  int64        `json:"blocked_total"`
	Updating      bool         `json:"updating"`
	BuiltAt       *time.Time   `json:"built_at,omitempty"`
	Lists         []ListStatus `json:"lists"`
}

// Adblocker owns the cached list files and the live FilterEngine.
type Adblocker struct {
	dir      string
	settings *SettingsStore
	client   *http.Client

	engine   atomic.Pointer[FilterEngine]
	blocked  atomic.Int64
	mu       sync.Mutex
	updating bool
	builtAt  *time.Time
	errors   map[string]string
}

func NewAdblocker(dataDir string, settings *SettingsStore, transport http.RoundTripper) *Adblocker {
	a := &Adblocker{
		dir:      filepath.Join(dataDir, "filters"),
		settings: settings,
		client:   &http.Client{Transport: transport, Timeout: 90 * time.Second},
		errors:   map[string]string{},
	}
	_ = os.MkdirAll(a.dir, 0o755)
	a.engine.Store(newFilterEngine())
	settings.OnChange(func(Settings) { go a.Rebuild() })
	return a
}

func (a *Adblocker) Engine() *FilterEngine { return a.engine.Load() }

func (a *Adblocker) Enabled() bool { return a.settings.Get().AdblockEnabled }

func (a *Adblocker) CountBlocked() { a.blocked.Add(1) }

func (a *Adblocker) SiteAllowed(host string) bool {
	host = strings.ToLower(host)
	for _, s := range a.settings.Get().AllowedSites {
		if hostMatchesDomain(host, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

func (a *Adblocker) listPath(id string) string { return filepath.Join(a.dir, id+".txt") }

// Start builds from whatever is cached right away (so blocking works
// immediately after a restart, even offline), then fetches any missing or
// stale lists in the background and keeps them fresh.
func (a *Adblocker) Start(ctx context.Context) {
	a.Rebuild()
	go func() {
		a.UpdateLists(ctx, false)
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.UpdateLists(ctx, false)
			}
		}
	}()
}

// UpdateLists downloads enabled lists that are missing, stale, or (force)
// all of them, then rebuilds the engine once.
func (a *Adblocker) UpdateLists(ctx context.Context, force bool) {
	a.mu.Lock()
	if a.updating {
		a.mu.Unlock()
		return
	}
	a.updating = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.updating = false
		a.mu.Unlock()
	}()

	changed := false
	for _, id := range a.settings.Get().EnabledLists {
		l, ok := findList(id)
		if !ok {
			continue
		}
		if !force {
			if fi, err := os.Stat(a.listPath(id)); err == nil && time.Since(fi.ModTime()) < listMaxAge {
				continue
			}
		}
		err := a.fetchList(ctx, l)
		a.mu.Lock()
		if err != nil {
			a.errors[id] = err.Error()
			log.Printf("filter list %s: %v", id, err)
		} else {
			delete(a.errors, id)
			changed = true
		}
		a.mu.Unlock()
	}
	if changed {
		a.Rebuild()
	}
}

func (a *Adblocker) fetchList(ctx context.Context, l FilterList) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if len(body) < 64 {
		return fmt.Errorf("list is suspiciously empty")
	}
	tmp := a.listPath(l.ID) + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.listPath(l.ID))
}

// Rebuild re-parses every enabled cached list plus the user's own filters.
func (a *Adblocker) Rebuild() {
	s := a.settings.Get()
	var texts []string
	for _, id := range s.EnabledLists {
		if raw, err := os.ReadFile(a.listPath(id)); err == nil {
			texts = append(texts, string(raw))
		}
	}
	texts = append(texts, s.CustomFilters)
	started := time.Now()
	e := BuildFilterEngine(texts...)
	a.engine.Store(e)
	now := time.Now()
	a.mu.Lock()
	a.builtAt = &now
	a.mu.Unlock()
	log.Printf("adblock engine built: %d network, %d cosmetic filters in %s", e.NetworkCount, e.CosmeticCount, time.Since(started).Round(time.Millisecond))
}

func (a *Adblocker) Stats() AdblockStats {
	s := a.settings.Get()
	enabled := map[string]bool{}
	for _, id := range s.EnabledLists {
		enabled[id] = true
	}
	e := a.Engine()
	a.mu.Lock()
	defer a.mu.Unlock()
	st := AdblockStats{
		Enabled:       s.AdblockEnabled,
		NetworkRules:  e.NetworkCount,
		CosmeticRules: e.CosmeticCount,
		BlockedTotal:  a.blocked.Load(),
		Updating:      a.updating,
		BuiltAt:       a.builtAt,
	}
	for _, l := range builtinFilterLists {
		ls := ListStatus{FilterList: l, Enabled: enabled[l.ID], Error: a.errors[l.ID]}
		if fi, err := os.Stat(a.listPath(l.ID)); err == nil {
			t := fi.ModTime()
			ls.UpdatedAt = &t
			ls.Size = fi.Size()
		}
		st.Lists = append(st.Lists, ls)
	}
	return st
}
