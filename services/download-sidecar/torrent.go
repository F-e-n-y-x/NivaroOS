package main

// Torrents in Download Station. One engine-agnostic manager (this file)
// drives whichever TorrentEngine the user picked in settings:
//
//   - "qbittorrent": libtorrent through qbittorrent-nox, run by NivaroOS as
//     its own sandboxed unit (nivaroos-torrent.service), WebUI on loopback
//     only, started on demand and stopped when idle (torrent_qbt.go).
//   - "external": the user's own qBittorrent (URL + credentials), same
//     client code, never reconfigured by us.
//   - "builtin": anacrolix/torrent embedded in this process - the light
//     fallback for boxes without qbittorrent-nox (torrent_builtin.go).
//
// The manager owns what is the same for all of them: validating adds
// (magnet / .torrent / URL to a .torrent), save-folder root checks,
// categories, the watch folder, the alternative-speed schedule, the
// public-tracker list (never added to private torrents), the idle stop
// and completion notifications.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

// ---- what every engine reports ----

type TorrentInfo struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	State      string  `json:"state"`
	Progress   float64 `json:"progress"` // 0..1 of the selected files
	Size       int64   `json:"size"`     // bytes of the selected files
	Done       int64   `json:"done"`
	Downloaded int64   `json:"downloaded"`
	Uploaded   int64   `json:"uploaded"`
	DlSpeed    int64   `json:"dl_speed"`
	UpSpeed    int64   `json:"up_speed"`
	Seeds      int     `json:"seeds"`
	SeedsTotal int     `json:"seeds_total"`
	Peers      int     `json:"peers"`
	PeersTotal int     `json:"peers_total"`
	Ratio      float64 `json:"ratio"`
	ETA        int64   `json:"eta"` // seconds, -1 = unknown
	Dir        string  `json:"dir"`
	Category   string  `json:"category"`
	// nil while unknown (a magnet still fetching its metadata, or an
	// older qBittorrent that doesn't report it) - treated as private.
	Private     *bool  `json:"private"`
	HasMetadata bool   `json:"has_metadata"`
	Sequential  bool   `json:"sequential"`
	FirstLast   bool   `json:"first_last"`
	AddedAt     int64  `json:"added_at"`
	CompletedAt int64  `json:"completed_at"`
	SeedingTime int64  `json:"seeding_time"`
	Error       string `json:"error,omitempty"`
}

// Torrent states as the UI sees them, whichever engine runs.
const (
	tsDownloading = "downloading"
	tsStalled     = "stalled"
	tsMetadata    = "metadata"
	tsSeeding     = "seeding"
	tsQueued      = "queued"
	tsChecking    = "checking"
	tsMoving      = "moving"
	tsPaused      = "paused"
	tsCompleted   = "completed" // finished and stopped (not seeding)
	tsError       = "error"
)

// active: the engine has work to do for it (the idle stop waits for none).
func (t TorrentInfo) active() bool {
	switch t.State {
	case tsPaused, tsCompleted, tsError:
		return false
	}
	return true
}

func (t TorrentInfo) finished() bool {
	return t.State == tsSeeding || t.State == tsCompleted || (t.Size > 0 && t.Done >= t.Size)
}

type TorrentFile struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

// File priorities (qBittorrent's values).
const (
	prioSkip   = 0
	prioNormal = 1
	prioHigh   = 6
	prioMax    = 7
)

func validPriority(p int) bool {
	return p == prioSkip || p == prioNormal || p == prioHigh || p == prioMax
}

type TorrentTracker struct {
	URL     string `json:"url"`
	Status  string `json:"status"` // working, updating, not_working, not_contacted, disabled
	Tier    int    `json:"tier"`
	Peers   int    `json:"peers"`
	Seeds   int    `json:"seeds"`
	Message string `json:"message,omitempty"`
}

type TorrentDetail struct {
	TorrentInfo
	Files    []TorrentFile    `json:"files"`
	Trackers []TorrentTracker `json:"trackers"`
}

// TorrentAdd is a validated add: exactly one of Magnet / File is set and
// Dir is already root-checked (empty for the external engine).
type TorrentAdd struct {
	Magnet     string
	File       []byte
	Hash       string
	Dir        string
	Category   string
	Paused     bool
	Sequential bool
	FirstLast  bool
}

// TorrentEngine is the backend interface - the UI and the manager are the
// same whichever one runs.
type TorrentEngine interface {
	// Start brings the engine up if it isn't (idempotent) and applies s,
	// with dl/up as the speed limits in force right now.
	Start(ctx context.Context, s TorrentSettings, dl, up int64) error
	Stop() error
	Running() bool
	SetLimits(ctx context.Context, dl, up int64) error
	List(ctx context.Context) ([]TorrentInfo, error)
	Detail(ctx context.Context, hash string) (TorrentDetail, error)
	Add(ctx context.Context, a TorrentAdd) error
	Act(ctx context.Context, hash, action string) error // pause, resume, recheck
	Remove(ctx context.Context, hash string, deleteFiles bool) error
	SetFilePriority(ctx context.Context, hash string, ids []int, prio int) error
	SetOptions(ctx context.Context, hash string, sequential, firstLast bool) error
	AddTrackers(ctx context.Context, hash string, urls []string) error
}

// Settings sections an engine can't honour (the UI greys them out).
var engineUnsupported = map[string][]string{
	"builtin":  {"lsd", "preallocate", "max_connections"},
	"external": {"all"}, // the user's own client is configured in its own UI
}

// ---- settings ----

type TorrentCategory struct {
	Name string `json:"name"`
	Dir  string `json:"dir"` // empty = <save_dir>/<name>
}

type TorrentSettings struct {
	// "" = the default: qbittorrent when it's installed, else builtin.
	Engine              string `json:"engine"`
	ExternalURL         string `json:"external_url"`
	ExternalUser        string `json:"external_user"`
	ExternalPassword    string `json:"external_password,omitempty"`
	ExternalPasswordSet bool   `json:"external_password_set"`

	SaveDir       string            `json:"save_dir"`
	IncompleteDir string            `json:"incomplete_dir"` // "" = off
	Categories    []TorrentCategory `json:"categories"`
	WatchDir      string            `json:"watch_dir"` // "" = off

	// Bytes per second, 0 = unlimited.
	DlLimit    int64 `json:"dl_limit"`
	UpLimit    int64 `json:"up_limit"`
	AltDlLimit int64 `json:"alt_dl_limit"`
	AltUpLimit int64 `json:"alt_up_limit"`
	// Manual "use alternative limits now", plus the schedule.
	AltSpeed     bool   `json:"alt_speed"`
	Schedule     bool   `json:"schedule"`
	ScheduleFrom string `json:"schedule_from"` // "HH:MM"
	ScheduleTo   string `json:"schedule_to"`
	ScheduleDays string `json:"schedule_days"` // every, weekdays, weekends

	// 0 = unlimited.
	Queueing           bool `json:"queueing"`
	MaxActiveDownloads int  `json:"max_active_downloads"`
	MaxActiveUploads   int  `json:"max_active_uploads"`
	MaxActiveTorrents  int  `json:"max_active_torrents"`

	RatioLimit      float64 `json:"ratio_limit"`     // 0 = off
	SeedTimeLimit   int     `json:"seed_time_limit"` // minutes, 0 = off
	SeedLimitAction string  `json:"seed_limit_action"`

	ListenPort  int    `json:"listen_port"`
	UPnP        bool   `json:"upnp"`
	DHT         bool   `json:"dht"`
	PeX         bool   `json:"pex"`
	LSD         bool   `json:"lsd"`
	Encryption  string `json:"encryption"` // allow, prefer, require
	MaxConns    int    `json:"max_connections"`
	MaxConnsPer int    `json:"max_connections_per_torrent"`
	Preallocate bool   `json:"preallocate"`

	TrackersAuto     bool   `json:"trackers_auto"`
	TrackersURL      string `json:"trackers_url"`
	TrackersInterval int    `json:"trackers_interval_hours"`
}

const (
	defaultTrackersURL = "https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt"
	qbtWebUIPort       = 28646
	defaultTorrentPort = 51413
)

func defaultTorrentSettings(defaultDir string) TorrentSettings {
	return TorrentSettings{
		SaveDir:            defaultDir,
		ScheduleFrom:       "08:00",
		ScheduleTo:         "20:00",
		ScheduleDays:       "every",
		Queueing:           true,
		MaxActiveDownloads: 3,
		MaxActiveUploads:   3,
		MaxActiveTorrents:  5,
		SeedLimitAction:    "pause",
		ListenPort:         defaultTorrentPort,
		UPnP:               true,
		DHT:                true,
		PeX:                true,
		LSD:                true,
		Encryption:         "prefer",
		MaxConns:           500,
		MaxConnsPer:        100,
		TrackersAuto:       true,
		TrackersURL:        defaultTrackersURL,
		TrackersInterval:   24,
		Categories:         []TorrentCategory{},
	}
}

// normalize fills what an older settings file lacks; validate refuses bad
// input from the API.
func (t *TorrentSettings) normalize(defaultDir string) {
	d := defaultTorrentSettings(defaultDir)
	if t.SaveDir == "" {
		t.SaveDir = d.SaveDir
	}
	if t.ScheduleDays == "" {
		t.ScheduleDays = d.ScheduleDays
	}
	if t.SeedLimitAction == "" {
		t.SeedLimitAction = d.SeedLimitAction
	}
	if t.Encryption == "" {
		t.Encryption = d.Encryption
	}
	if t.ListenPort == 0 {
		t.ListenPort = d.ListenPort
	}
	if t.TrackersURL == "" {
		t.TrackersURL = d.TrackersURL
	}
	if t.TrackersInterval <= 0 {
		t.TrackersInterval = d.TrackersInterval
	}
	if t.Categories == nil {
		t.Categories = []TorrentCategory{}
	}
	t.ExternalPasswordSet = t.ExternalPassword != ""
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func (t *TorrentSettings) validate() error {
	switch t.Engine {
	case "", "qbittorrent", "builtin", "external":
	default:
		return fmt.Errorf("unknown torrent engine %q", t.Engine)
	}
	if t.Engine == "external" {
		u, err := url.Parse(strings.TrimSpace(t.ExternalURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("your qBittorrent's address must be an http:// or https:// URL")
		}
	}
	for _, v := range []int64{t.DlLimit, t.UpLimit, t.AltDlLimit, t.AltUpLimit} {
		if v < 0 {
			return errors.New("speed limits can't be negative")
		}
	}
	if !hhmm.MatchString(t.ScheduleFrom) || !hhmm.MatchString(t.ScheduleTo) {
		return errors.New("schedule times must be HH:MM")
	}
	switch t.ScheduleDays {
	case "every", "weekdays", "weekends":
	default:
		return errors.New("schedule days must be every, weekdays or weekends")
	}
	for _, v := range []int{t.MaxActiveDownloads, t.MaxActiveUploads, t.MaxActiveTorrents} {
		if v < 0 || v > 1000 {
			return errors.New("queue limits must be between 0 (unlimited) and 1000")
		}
	}
	if t.RatioLimit < 0 || t.RatioLimit > 1000 {
		return errors.New("ratio limit must be between 0 (off) and 1000")
	}
	if t.SeedTimeLimit < 0 || t.SeedTimeLimit > 525600 {
		return errors.New("seeding time limit must be between 0 (off) and 525600 minutes")
	}
	if t.SeedLimitAction != "pause" && t.SeedLimitAction != "remove" {
		return errors.New("when a seeding limit is reached the torrent is paused or removed")
	}
	if t.ListenPort < 1024 || t.ListenPort > 65535 || t.ListenPort == qbtWebUIPort || t.ListenPort == 28642 {
		return errors.New("listening port must be between 1024 and 65535 and not one NivaroOS uses")
	}
	switch t.Encryption {
	case "allow", "prefer", "require":
	default:
		return errors.New("encryption must be allow, prefer or require")
	}
	if t.MaxConns < 0 || t.MaxConns > 65535 || t.MaxConnsPer < 0 || t.MaxConnsPer > 65535 {
		return errors.New("connection limits must be between 0 (unlimited) and 65535")
	}
	if t.TrackersAuto {
		u, err := url.Parse(strings.TrimSpace(t.TrackersURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("the trackers list must be an http:// or https:// URL")
		}
	}
	if t.TrackersInterval < 1 || t.TrackersInterval > 720 {
		return errors.New("trackers refresh must be every 1 to 720 hours")
	}
	seen := map[string]bool{}
	for i, c := range t.Categories {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" || len(c.Name) > 64 || strings.ContainsAny(c.Name, "/\\\r\n") {
			return errors.New("category names must be 1-64 characters without slashes")
		}
		if seen[strings.ToLower(c.Name)] {
			return fmt.Errorf("category %q is listed twice", c.Name)
		}
		seen[strings.ToLower(c.Name)] = true
		if c.Dir != "" && !filepath.IsAbs(c.Dir) {
			return errors.New("category folders must be absolute paths")
		}
		t.Categories[i] = c
	}
	for _, d := range []string{t.SaveDir, t.IncompleteDir, t.WatchDir} {
		if d != "" && !filepath.IsAbs(d) {
			return errors.New("folders must be absolute paths")
		}
	}
	return nil
}

// folders returns every folder the settings name, for the root checks.
func (t *TorrentSettings) folders() []string {
	out := []string{t.SaveDir}
	if t.IncompleteDir != "" {
		out = append(out, t.IncompleteDir)
	}
	if t.WatchDir != "" {
		out = append(out, t.WatchDir)
	}
	for _, c := range t.Categories {
		if c.Dir != "" {
			out = append(out, c.Dir)
		}
	}
	return out
}

func (t *TorrentSettings) categoryDir(name string) (string, bool) {
	for _, c := range t.Categories {
		if strings.EqualFold(c.Name, name) {
			if c.Dir != "" {
				return c.Dir, true
			}
			return filepath.Join(t.SaveDir, c.Name), true
		}
	}
	return "", false
}

// altActive: whether the alternative speed limits apply at now.
func (t *TorrentSettings) altActive(now time.Time) bool {
	if t.AltSpeed {
		return true
	}
	if !t.Schedule {
		return false
	}
	wd := now.Weekday()
	weekend := wd == time.Saturday || wd == time.Sunday
	if (t.ScheduleDays == "weekdays" && weekend) || (t.ScheduleDays == "weekends" && !weekend) {
		return false
	}
	cur := now.Format("15:04")
	if t.ScheduleFrom <= t.ScheduleTo {
		return cur >= t.ScheduleFrom && cur < t.ScheduleTo
	}
	return cur >= t.ScheduleFrom || cur < t.ScheduleTo // across midnight
}

func (t *TorrentSettings) limitsAt(now time.Time) (int64, int64) {
	if t.altActive(now) {
		return t.AltDlLimit, t.AltUpLimit
	}
	return t.DlLimit, t.UpLimit
}

// ---- parsing adds ----

type torrentMeta struct {
	Hash    string
	Name    string
	HasInfo bool
	Private bool
}

var hashRe = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

func parseMagnet(s string) (torrentMeta, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(strings.ToLower(s), "magnet:?") {
		return torrentMeta{}, errors.New("not a magnet link")
	}
	m, err := metainfo.ParseMagnetV2Uri(s)
	if err != nil {
		return torrentMeta{}, fmt.Errorf("invalid magnet link: %w", err)
	}
	var hash string
	if m.InfoHash.Ok {
		hash = m.InfoHash.Value.HexString()
	} else if m.V2InfoHash.Ok {
		hash = m.V2InfoHash.Value.HexString()
	} else {
		return torrentMeta{}, errors.New("magnet link has no info hash")
	}
	return torrentMeta{Hash: hash, Name: m.DisplayName}, nil
}

const maxTorrentFile = 16 << 20

func parseTorrentFile(b []byte) (torrentMeta, error) {
	if len(b) == 0 || len(b) > maxTorrentFile {
		return torrentMeta{}, errors.New("not a .torrent file")
	}
	mi, err := metainfo.Load(bytes.NewReader(b))
	if err != nil {
		return torrentMeta{}, errors.New("not a valid .torrent file")
	}
	info, err := mi.UnmarshalInfo()
	if err != nil || info.PieceLength <= 0 {
		return torrentMeta{}, errors.New("the .torrent file has no valid info section")
	}
	return torrentMeta{
		Hash:    mi.HashInfoBytes().HexString(),
		Name:    info.BestName(),
		HasInfo: true,
		Private: info.Private != nil && *info.Private,
	}, nil
}

// ---- tracker list (the one feature taken from qBittorrent Enhanced) ----

type TrackerList struct {
	URL       string    `json:"url"`
	Trackers  []string  `json:"trackers"`
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
	Version   int       `json:"version"`
}

const maxTrackers = 1000

func parseTrackerList(r io.Reader) []string {
	raw, _ := io.ReadAll(io.LimitReader(r, 2<<20))
	seen := map[string]bool{}
	out := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || seen[line] {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Host == "" {
			continue
		}
		switch u.Scheme {
		case "udp", "http", "https", "ws", "wss":
		default:
			continue
		}
		seen[line] = true
		out = append(out, line)
		if len(out) == maxTrackers {
			break
		}
	}
	return out
}

// trackersFor is the private-flag rule: the public list is only ever
// added to torrents whose metadata says they're public.
func trackersFor(t TorrentInfo, list []string) []string {
	if !t.HasMetadata || t.Private == nil || *t.Private {
		return nil
	}
	return list
}

// ---- the manager ----

type Torrents struct {
	dataDir  string
	st       *SettingsStore
	fetch    *http.Client // netguard transport: .torrent URLs, tracker lists
	events   *eventLog
	newEng   func(kind string, s TorrentSettings) TorrentEngine
	idleWait time.Duration

	mu         sync.Mutex
	kind       string
	eng        TorrentEngine
	cache      []TorrentInfo
	lastUse    time.Time
	limits     [2]int64
	trackers   TrackerList
	applied    map[string]int // hash -> tracker list version added
	refreshing bool
}

func NewTorrents(dataDir string, st *SettingsStore, transport http.RoundTripper, events *eventLog) *Torrents {
	t := &Torrents{
		dataDir:  dataDir,
		st:       st,
		fetch:    &http.Client{Transport: transport, Timeout: 60 * time.Second},
		events:   events,
		idleWait: 2 * time.Minute,
		applied:  map[string]int{},
	}
	t.newEng = func(kind string, s TorrentSettings) TorrentEngine {
		switch kind {
		case "external":
			return newQbtEngine(s.ExternalURL, s.ExternalUser, s.ExternalPassword, false, dataDir)
		case "qbittorrent":
			return newQbtEngine(fmt.Sprintf("http://127.0.0.1:%d", qbtWebUIPort), "nivaroos", "", true, dataDir)
		}
		return newBuiltinEngine(dataDir)
	}
	if raw, err := os.ReadFile(t.path("torrents-cache.json")); err == nil {
		_ = json.Unmarshal(raw, &t.cache)
	}
	if raw, err := os.ReadFile(t.path("torrent-trackers.json")); err == nil {
		_ = json.Unmarshal(raw, &t.trackers)
	}
	st.OnChange(func(Settings) { go t.applySettings() })
	return t
}

func (t *Torrents) path(name string) string { return filepath.Join(t.dataDir, name) }

// engineKind resolves "" to the default engine.
func engineKind(s TorrentSettings) string {
	if s.Engine != "" {
		return s.Engine
	}
	if qbtInstalled() {
		return "qbittorrent"
	}
	return "builtin"
}

// engine returns the current engine, swapping it when the setting
// changed and, with wake, starting it.
func (t *Torrents) engine(ctx context.Context, wake bool) (TorrentEngine, error) {
	s := t.st.Get().Torrent
	kind := engineKind(s)
	t.mu.Lock()
	if t.eng == nil || t.kind != kind {
		if t.eng != nil {
			old := t.eng
			go func() { _ = old.Stop() }()
		}
		t.eng, t.kind, t.cache = t.newEng(kind, s), kind, nil
		t.limits = [2]int64{-1, -1}
	}
	eng := t.eng
	if wake {
		t.lastUse = time.Now()
	}
	t.mu.Unlock()
	if wake && !eng.Running() {
		dl, up := s.limitsAt(time.Now())
		if err := eng.Start(ctx, s, dl, up); err != nil {
			return eng, fmt.Errorf("the torrent engine could not start: %w", err)
		}
		t.mu.Lock()
		t.limits = [2]int64{dl, up}
		t.mu.Unlock()
	}
	return eng, nil
}

func (t *Torrents) applySettings() {
	s := t.st.Get().Torrent
	eng, _ := t.engine(serverCtx, false)
	if eng == nil || !eng.Running() {
		return
	}
	dl, up := s.limitsAt(time.Now())
	if err := eng.Start(serverCtx, s, dl, up); err != nil {
		log.Printf("torrent: applying settings: %v", err)
	}
	t.mu.Lock()
	t.limits = [2]int64{dl, up}
	t.mu.Unlock()
}

// List never starts the engine: while it's stopped the last known list is
// shown (nothing in it is moving).
func (t *Torrents) List(ctx context.Context) ([]TorrentInfo, bool, error) {
	eng, _ := t.engine(ctx, false)
	if eng.Running() {
		list, err := eng.List(ctx)
		if err == nil {
			t.remember(list)
			return list, true, nil
		}
		if eng.Running() {
			return nil, true, err
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TorrentInfo, len(t.cache))
	for i, c := range t.cache {
		c.DlSpeed, c.UpSpeed, c.Seeds, c.Peers = 0, 0, 0, 0
		out[i] = c
	}
	return out, false, nil
}

// remember keeps the list (for while the engine is stopped) and raises a
// notification for every torrent that just finished.
func (t *Torrents) remember(list []TorrentInfo) {
	t.mu.Lock()
	prev := map[string]TorrentInfo{}
	for _, c := range t.cache {
		prev[c.Hash] = c
	}
	changed := len(prev) != len(list)
	var done []TorrentInfo
	for _, c := range list {
		p, ok := prev[c.Hash]
		if !ok || p.State != c.State {
			changed = true
		}
		if ok && !p.finished() && c.finished() {
			done = append(done, c)
		}
	}
	t.cache = list
	t.mu.Unlock()
	for _, c := range done {
		t.events.Push(Event{Kind: "completed", ID: c.Hash, Filename: c.Name, Path: c.Dir})
	}
	if changed {
		if err := writeJSONAtomic(t.path("torrents-cache.json"), list); err != nil {
			log.Printf("torrent: saving list: %v", err)
		}
	}
}

func (t *Torrents) Detail(ctx context.Context, hash string) (TorrentDetail, error) {
	eng, err := t.engine(ctx, true)
	if err != nil {
		return TorrentDetail{}, err
	}
	return eng.Detail(ctx, hash)
}

// AddRequest from the UI: source is a magnet link or an http(s) URL to a
// .torrent; torrent is a .torrent file's bytes.
type TorrentAddRequest struct {
	Source     string `json:"source"`
	Torrent    []byte `json:"torrent"` // base64 in JSON
	Dir        string `json:"dir"`
	Category   string `json:"category"`
	Paused     bool   `json:"paused"`
	Sequential bool   `json:"sequential"`
	FirstLast  bool   `json:"first_last"`
}

func (t *Torrents) Add(ctx context.Context, req TorrentAddRequest) (torrentMeta, error) {
	s := t.st.Get().Torrent
	a := TorrentAdd{Paused: req.Paused, Sequential: req.Sequential, FirstLast: req.FirstLast}
	var meta torrentMeta
	var err error
	src := strings.TrimSpace(req.Source)
	switch {
	case len(req.Torrent) > 0:
		a.File = req.Torrent
	case strings.HasPrefix(strings.ToLower(src), "magnet:"):
		a.Magnet = src
	case src != "":
		if a.File, err = t.fetchTorrent(ctx, src); err != nil {
			return meta, err
		}
	default:
		return meta, errors.New("add a magnet link, a .torrent file or a link to one")
	}
	if a.File != nil {
		meta, err = parseTorrentFile(a.File)
	} else {
		meta, err = parseMagnet(a.Magnet)
	}
	if err != nil {
		return meta, err
	}
	a.Hash = meta.Hash
	a.Category = strings.TrimSpace(req.Category)
	dir := strings.TrimSpace(req.Dir)
	if a.Category != "" {
		cdir, ok := s.categoryDir(a.Category)
		if !ok {
			return meta, fmt.Errorf("there is no category %q", a.Category)
		}
		if dir == "" {
			dir = cdir
		}
	}
	if engineKind(s) != "external" {
		if dir == "" {
			dir = s.SaveDir
		}
		if !filepath.IsAbs(dir) {
			return meta, errors.New("save folder must be an absolute path")
		}
		if _, err := pathPolicy.MkdirAll(filepath.Clean(dir)); err != nil {
			return meta, err
		}
		a.Dir = filepath.Clean(dir)
	}
	eng, err := t.engine(ctx, true)
	if err != nil {
		return meta, err
	}
	return meta, eng.Add(ctx, a)
}

func (t *Torrents) fetchTorrent(ctx context.Context, raw string) ([]byte, error) {
	u, err := validateDownloadURL(raw)
	if err != nil {
		return nil, errors.New("add a magnet link, a .torrent file or an http(s) link to one")
	}
	// The user typed this address, so a LAN indexer (Jackett, Prowlarr) is fine.
	ctx = withPrivateHosts(ctx, u.Hostname())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := t.fetch.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not fetch the .torrent: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("could not fetch the .torrent: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentFile+1))
	if err != nil {
		return nil, fmt.Errorf("could not fetch the .torrent: %w", err)
	}
	if len(b) > maxTorrentFile {
		return nil, errors.New("that .torrent file is too large")
	}
	return b, nil
}

func (t *Torrents) do(ctx context.Context, fn func(TorrentEngine) error) error {
	eng, err := t.engine(ctx, true)
	if err != nil {
		return err
	}
	return fn(eng)
}

// ---- background work ----

func (t *Torrents) Start(ctx context.Context) {
	t.mu.Lock()
	wake := false
	for _, c := range t.cache {
		wake = wake || c.active()
	}
	t.mu.Unlock()
	if wake {
		if _, err := t.engine(ctx, true); err != nil {
			log.Printf("torrent: %v", err)
		}
	}
	go t.loop(ctx)
}

// Shutdown stops an in-process engine (saving its state) on the way out;
// qBittorrent runs in its own unit and keeps going.
func (t *Torrents) Shutdown() {
	t.mu.Lock()
	eng, kind := t.eng, t.kind
	t.mu.Unlock()
	if eng != nil && kind == "builtin" {
		_ = eng.Stop()
	}
}

func (t *Torrents) loop(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if n%2 == 0 {
			t.scanWatchDir(ctx)
		}
		if n%120 == 0 {
			t.maybeRefreshTrackers(ctx, false)
		}
		n++
		t.tick(ctx)
	}
}

func (t *Torrents) tick(ctx context.Context) {
	eng, _ := t.engine(ctx, false)
	if eng == nil || !eng.Running() {
		return
	}
	s := t.st.Get().Torrent
	dl, up := s.limitsAt(time.Now())
	t.mu.Lock()
	limitsChanged := t.limits != [2]int64{dl, up}
	t.mu.Unlock()
	if limitsChanged && eng.SetLimits(ctx, dl, up) == nil {
		t.mu.Lock()
		t.limits = [2]int64{dl, up}
		t.mu.Unlock()
	}
	list, err := eng.List(ctx)
	if err != nil {
		return
	}
	t.remember(list)
	t.syncTrackers(ctx, eng, s, list)

	busy := false
	for _, c := range list {
		busy = busy || c.active()
	}
	t.mu.Lock()
	kind := t.kind
	idle := !busy && time.Since(t.lastUse) > t.idleWait && kind != "external"
	t.mu.Unlock()
	if idle {
		log.Printf("torrent: nothing active, stopping the %s engine", kind)
		_ = eng.Stop()
	}
}

func (t *Torrents) syncTrackers(ctx context.Context, eng TorrentEngine, s TorrentSettings, list []TorrentInfo) {
	t.mu.Lock()
	tl := t.trackers
	t.mu.Unlock()
	if !s.TrackersAuto || len(tl.Trackers) == 0 {
		return
	}
	for _, c := range list {
		add := trackersFor(c, tl.Trackers)
		t.mu.Lock()
		done := t.applied[c.Hash] == tl.Version
		t.mu.Unlock()
		if add == nil || done {
			continue
		}
		if err := eng.AddTrackers(ctx, c.Hash, add); err != nil {
			continue
		}
		t.mu.Lock()
		t.applied[c.Hash] = tl.Version
		t.mu.Unlock()
	}
}

func (t *Torrents) Trackers() TrackerList {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.trackers
	out.Trackers = append([]string(nil), t.trackers.Trackers...)
	return out
}

func (t *Torrents) maybeRefreshTrackers(ctx context.Context, force bool) {
	s := t.st.Get().Torrent
	t.mu.Lock()
	due := force || (s.TrackersAuto && (t.trackers.URL != s.TrackersURL ||
		time.Since(t.trackers.UpdatedAt) > time.Duration(s.TrackersInterval)*time.Hour))
	if !due || t.refreshing {
		t.mu.Unlock()
		return
	}
	t.refreshing = true
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.refreshing = false
		t.mu.Unlock()
	}()
	list, err := t.fetchTrackers(ctx, s.TrackersURL)
	t.mu.Lock()
	next := t.trackers
	if err != nil {
		next.Error = err.Error()
		if next.URL != s.TrackersURL {
			// Don't retry a dead URL every tick; the interval applies.
			next.URL, next.UpdatedAt = s.TrackersURL, time.Now()
		}
	} else {
		if strings.Join(list, "\n") != strings.Join(next.Trackers, "\n") {
			next.Version++
		}
		next.URL, next.Trackers, next.UpdatedAt, next.Error = s.TrackersURL, list, time.Now(), ""
	}
	t.trackers = next
	t.mu.Unlock()
	_ = writeJSONAtomic(t.path("torrent-trackers.json"), next)
}

func (t *Torrents) fetchTrackers(ctx context.Context, raw string) ([]string, error) {
	u, err := validateDownloadURL(raw)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(withPrivateHosts(ctx, u.Hostname()), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := t.fetch.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("trackers list: HTTP %d", resp.StatusCode)
	}
	list := parseTrackerList(resp.Body)
	if len(list) == 0 {
		return nil, errors.New("the trackers list has no tracker URLs in it")
	}
	return list, nil
}

// scanWatchDir adds every .torrent dropped in the watch folder, then
// renames it to .added (or .invalid) the way qBittorrent does.
func (t *Torrents) scanWatchDir(ctx context.Context) {
	dir := t.st.Get().Torrent.WatchDir
	if dir == "" {
		return
	}
	if _, err := pathPolicy.Check(dir); err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(strings.ToLower(e.Name()), ".torrent") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if _, err := parseTorrentFile(b); err != nil {
			_ = os.Rename(p, p+".invalid")
			continue
		}
		if _, err := t.Add(ctx, TorrentAddRequest{Torrent: b}); err != nil {
			// Engine trouble, not a bad file: left in place for the next scan.
			log.Printf("torrent: watch folder %s: %v", e.Name(), err)
			continue
		}
		_ = os.Rename(p, p+".added")
	}
}
