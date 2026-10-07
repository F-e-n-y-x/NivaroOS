package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/anacrolix/torrent/types"
	"golang.org/x/time/rate"
)

// The built-in engine: anacrolix/torrent inside this process, for boxes
// without qbittorrent-nox. The client only exists while something needs
// it (Start/Stop); torrents, their options and counters are kept in
// torrents-builtin.json plus each one's .torrent once its metadata is in.
// What anacrolix has no knob for (LSD, preallocation, a global connection
// cap) is listed in engineUnsupported; queueing, seeding limits, the
// incomplete folder and sequential order are done here on a 1 s tick.

type btItem struct {
	Hash       string      `json:"hash"`
	Name       string      `json:"name"`
	Magnet     string      `json:"magnet,omitempty"`
	Dir        string      `json:"dir"`
	Category   string      `json:"category,omitempty"`
	Paused     bool        `json:"paused"`
	Sequential bool        `json:"sequential"`
	FirstLast  bool        `json:"first_last"`
	Prio       map[int]int `json:"prio,omitempty"`
	Added      int64       `json:"added"`
	Completed  int64       `json:"completed,omitempty"`
	SeedSecs   int64       `json:"seed_secs"`
	Uploaded   int64       `json:"uploaded"`   // from earlier sessions
	Downloaded int64       `json:"downloaded"` // from earlier sessions
	Private    bool        `json:"private"`
	InTemp     bool        `json:"in_temp,omitempty"` // still in the incomplete folder
	Size       int64       `json:"size"`              // selected bytes, as of the last tick
	Done       int64       `json:"done"`
	Trackers   []string    `json:"trackers,omitempty"` // added from the public list

	t                  *torrent.Torrent
	queued             bool
	sessUp, sessDown   int64
	lastUp, lastDown   int64
	upSpeed, downSpeed int64
	seqPieces          []int
	err                string
}

type builtinEngine struct {
	dataDir string
	mu      sync.Mutex
	cl      *torrent.Client
	s       TorrentSettings
	dlLim   *rate.Limiter
	upLim   *rate.Limiter
	items   map[string]*btItem
	done    chan struct{}
}

func newBuiltinEngine(dataDir string) *builtinEngine {
	e := &builtinEngine{dataDir: dataDir, items: map[string]*btItem{}}
	if raw, err := os.ReadFile(e.statePath()); err == nil {
		var list []*btItem
		if json.Unmarshal(raw, &list) == nil {
			for _, it := range list {
				e.items[it.Hash] = it
			}
		}
	}
	return e
}

func (e *builtinEngine) statePath() string { return filepath.Join(e.dataDir, "torrents-builtin.json") }
func (e *builtinEngine) metaPath(hash string) string {
	return filepath.Join(e.dataDir, "torrents", hash+".torrent")
}

// The burst must stay >= 1 MiB even when unlimited: anacrolix sizes its
// reads by it, and a zero burst stalls every connection.
const limiterBurst = 1 << 20

func newLimiter(v int64) *rate.Limiter {
	l := rate.NewLimiter(rate.Inf, limiterBurst)
	setLimiter(l, v)
	return l
}

func setLimiter(l *rate.Limiter, v int64) {
	if v <= 0 {
		l.SetLimit(rate.Inf)
		return
	}
	l.SetBurst(int(max(v, limiterBurst)))
	l.SetLimit(rate.Limit(v))
}

// Settings that only take effect on a new client.
func clientKey(s TorrentSettings) string {
	return fmt.Sprint(s.ListenPort, s.UPnP, s.DHT, s.PeX, s.Encryption, s.MaxConnsPer)
}

func (e *builtinEngine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cl != nil
}

func (e *builtinEngine) Start(ctx context.Context, s TorrentSettings, dl, up int64) error {
	e.mu.Lock()
	restart := e.cl != nil && clientKey(e.s) != clientKey(s)
	e.mu.Unlock()
	if restart {
		_ = e.Stop()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.s = s
	if e.cl != nil {
		setLimiter(e.dlLim, dl)
		setLimiter(e.upLim, up)
		return nil
	}
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = s.SaveDir
	cfg.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{ClientBaseDir: s.SaveDir})
	cfg.ListenPort = s.ListenPort
	cfg.NoDefaultPortForwarding = !s.UPnP
	cfg.NoDHT = !s.DHT
	cfg.DisablePEX = !s.PeX
	cfg.Seed = true
	cfg.Slogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg.HeaderObfuscationPolicy = torrent.HeaderObfuscationPolicy{
		Preferred:        s.Encryption != "allow",
		RequirePreferred: s.Encryption == "require",
	}
	if s.MaxConnsPer > 0 {
		cfg.EstablishedConnsPerTorrent = s.MaxConnsPer
	}
	e.dlLim, e.upLim = newLimiter(dl), newLimiter(up)
	cfg.DownloadRateLimiter, cfg.UploadRateLimiter = e.dlLim, e.upLim
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		return err
	}
	e.cl = cl
	for _, it := range e.items {
		if !it.Paused {
			if err := e.attachLocked(it); err != nil {
				it.err = err.Error()
			}
		}
	}
	e.done = make(chan struct{})
	go e.loop(e.cl, e.done)
	return nil
}

func (e *builtinEngine) Stop() error {
	e.mu.Lock()
	cl, done := e.cl, e.done
	if cl == nil {
		e.mu.Unlock()
		return nil
	}
	for _, it := range e.items {
		e.detachLocked(it)
	}
	e.cl = nil
	close(done)
	e.saveLocked()
	e.mu.Unlock()
	cl.Close()
	return nil
}

// dirFor is where a torrent's data lives right now.
func (e *builtinEngine) dirFor(it *btItem) string {
	if it.InTemp && e.s.IncompleteDir != "" {
		return e.s.IncompleteDir
	}
	return it.Dir
}

func (e *builtinEngine) attachLocked(it *btItem) error {
	if it.t != nil {
		return nil
	}
	var spec *torrent.TorrentSpec
	var err error
	if mi, lerr := metainfo.LoadFromFile(e.metaPath(it.Hash)); lerr == nil {
		spec, err = torrent.TorrentSpecFromMetaInfoErr(mi)
	} else {
		spec, err = torrent.TorrentSpecFromMagnetUri(it.Magnet)
	}
	if err != nil {
		return err
	}
	if !it.Private && len(it.Trackers) > 0 {
		spec.Trackers = append(spec.Trackers, it.Trackers)
	}
	// Part files (name.part until complete) are how completion survives a
	// restart: finished files count as done, partial ones get rechecked.
	spec.Storage = storage.NewFileOpts(storage.NewFileClientOpts{ClientBaseDir: e.dirFor(it)})
	t, _, err := e.cl.AddTorrentSpec(spec)
	if err != nil {
		return err
	}
	it.t, it.err = t, ""
	st := t.Stats()
	it.sessUp, it.sessDown = 0, 0
	it.lastUp, it.lastDown = st.BytesWrittenData.Int64(), st.BytesReadUsefulData.Int64()
	go func(it *btItem, t *torrent.Torrent) {
		select {
		case <-t.GotInfo():
		case <-t.Closed():
			return
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if it.t != t {
			return
		}
		e.gotInfoLocked(it)
	}(it, t)
	return nil
}

func (e *builtinEngine) gotInfoLocked(it *btItem) {
	t := it.t
	info := t.Info()
	it.Name = info.BestName()
	it.Private = info.Private != nil && *info.Private
	if _, err := os.Stat(e.metaPath(it.Hash)); err != nil {
		mi := t.Metainfo()
		if err := os.MkdirAll(filepath.Dir(e.metaPath(it.Hash)), 0o700); err == nil {
			if f, err := os.Create(e.metaPath(it.Hash)); err == nil {
				_ = mi.Write(f)
				f.Close()
			}
		}
	}
	for i, f := range t.Files() {
		p, ok := it.Prio[i]
		if !ok {
			p = prioNormal
		}
		f.SetPriority(btPrio(p))
	}
	e.saveLocked()
}

func btPrio(p int) types.PiecePriority {
	switch p {
	case prioSkip:
		return types.PiecePriorityNone
	case prioHigh:
		return types.PiecePriorityHigh
	case prioMax:
		return types.PiecePriorityNow
	}
	return types.PiecePriorityNormal
}

func (e *builtinEngine) detachLocked(it *btItem) {
	if it.t == nil {
		return
	}
	it.Uploaded += it.sessUp
	it.Downloaded += it.sessDown
	it.sessUp, it.sessDown, it.upSpeed, it.downSpeed = 0, 0, 0, 0
	it.t.Drop()
	it.t = nil
}

func (e *builtinEngine) saveLocked() {
	list := make([]*btItem, 0, len(e.items))
	for _, it := range e.items {
		cp := *it
		cp.Uploaded += it.sessUp
		cp.Downloaded += it.sessDown
		list = append(list, &cp)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Added < list[j].Added })
	if err := writeJSONAtomic(e.statePath(), list); err != nil {
		log.Printf("torrent: saving built-in engine state: %v", err)
	}
}

// wanted: bytes of the selected files, and how many of them are done.
func wanted(it *btItem) (size, done int64) {
	if it.t == nil || it.t.Info() == nil {
		return 0, 0
	}
	for i, f := range it.t.Files() {
		if p, ok := it.Prio[i]; ok && p == prioSkip {
			continue
		}
		size += f.Length()
		done += f.BytesCompleted()
	}
	return
}

func (e *builtinEngine) loop(cl *torrent.Client, done chan struct{}) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	n := 0
	for {
		select {
		case <-done:
			return
		case <-tick.C:
		}
		e.mu.Lock()
		if e.cl == cl {
			e.tickLocked()
			if n++; n%10 == 0 {
				e.saveLocked()
			}
		}
		e.mu.Unlock()
	}
}

func (e *builtinEngine) tickLocked() {
	s := e.s
	order := e.orderedLocked()
	activeDl, activeUp, active := 0, 0, 0
	for _, it := range order {
		if it.t == nil {
			continue
		}
		st := it.t.Stats()
		up, down := st.BytesWrittenData.Int64(), st.BytesReadUsefulData.Int64()
		it.upSpeed, it.downSpeed = up-it.lastUp, down-it.lastDown
		it.sessUp += it.upSpeed
		it.sessDown += it.downSpeed
		it.lastUp, it.lastDown = up, down
		if it.t.Info() == nil {
			continue
		}
		size, got := wanted(it)
		it.Size, it.Done = size, got
		complete := size > 0 && got >= size
		if complete && it.Completed == 0 {
			it.Completed = time.Now().Unix()
		}
		if complete && it.InTemp && e.s.IncompleteDir != "" {
			e.moveOutLocked(it)
			continue
		}
		// Queueing: the first N (by add order) may download / seed.
		allowed := true
		if s.Queueing {
			if complete {
				allowed = (s.MaxActiveUploads == 0 || activeUp < s.MaxActiveUploads)
			} else {
				allowed = (s.MaxActiveDownloads == 0 || activeDl < s.MaxActiveDownloads)
			}
			allowed = allowed && (s.MaxActiveTorrents == 0 || active < s.MaxActiveTorrents)
		}
		if allowed {
			active++
			if complete {
				activeUp++
			} else {
				activeDl++
			}
		}
		if allowed != !it.queued {
			if allowed {
				it.t.AllowDataDownload()
				it.t.AllowDataUpload()
			} else {
				it.t.DisallowDataDownload()
				it.t.DisallowDataUpload()
			}
			it.queued = !allowed
		}
		if complete && allowed {
			it.SeedSecs++
			ratio := float64(it.Uploaded+it.sessUp) / float64(size)
			if (s.RatioLimit > 0 && ratio >= s.RatioLimit) || (s.SeedTimeLimit > 0 && it.SeedSecs >= int64(s.SeedTimeLimit)*60) {
				if s.SeedLimitAction == "remove" {
					e.detachLocked(it)
					delete(e.items, it.Hash)
					_ = os.Remove(e.metaPath(it.Hash))
				} else {
					e.detachLocked(it)
					it.Paused = true
				}
				continue
			}
		}
		if !complete {
			e.piecePrioLocked(it)
		}
	}
}

// piecePrioLocked: sequential order (a moving window of the next pieces
// at top priority - ponytail: an approximation, the rest still fetch
// rarest-first in the background) and first/last piece of every file.
func (e *builtinEngine) piecePrioLocked(it *btItem) {
	t := it.t
	for _, p := range it.seqPieces {
		t.Piece(p).SetPriority(types.PiecePriorityNone)
	}
	it.seqPieces = it.seqPieces[:0]
	for i, f := range t.Files() {
		if p, ok := it.Prio[i]; ok && p == prioSkip {
			continue
		}
		begin, end := f.BeginPieceIndex(), f.EndPieceIndex()
		if it.FirstLast && end > begin {
			it.seqPieces = append(it.seqPieces, begin, end-1)
		}
		if it.Sequential && len(it.seqPieces) < 34 {
			for p := begin; p < end && len(it.seqPieces) < 34; p++ {
				if !t.PieceState(p).Complete {
					it.seqPieces = append(it.seqPieces, p)
				}
			}
		}
	}
	for _, p := range it.seqPieces {
		if !t.PieceState(p).Complete {
			t.Piece(p).SetPriority(types.PiecePriorityNow)
		}
	}
}

// moveOutLocked moves a finished torrent from the incomplete folder to its
// save folder. Its files no longer have .part names, so the re-added
// torrent counts them as complete without a recheck.
func (e *builtinEngine) moveOutLocked(it *btItem) {
	name := it.t.Info().BestName()
	from, to := filepath.Join(e.s.IncompleteDir, name), filepath.Join(it.Dir, name)
	if !safeName(name) {
		it.err = "unsafe torrent name"
		return
	}
	e.detachLocked(it)
	if err := moveTree(from, to); err != nil {
		it.err = "could not move to the save folder: " + err.Error()
	} else {
		it.InTemp = false
	}
	if err := e.attachLocked(it); err != nil {
		it.err = err.Error()
	}
}

func safeName(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, "/\\") && filepath.Base(n) == n
}

func moveTree(from, to string) error {
	if !pathPolicy.Allowed(to) {
		return errPathNotAllowed
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	// Another filesystem: copy, then remove.
	err := filepath.Walk(from, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if fi.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(from)
}

func (e *builtinEngine) orderedLocked() []*btItem {
	out := make([]*btItem, 0, len(e.items))
	for _, it := range e.items {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Added < out[j].Added })
	return out
}

func (e *builtinEngine) SetLimits(ctx context.Context, dl, up int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cl != nil {
		setLimiter(e.dlLim, dl)
		setLimiter(e.upLim, up)
	}
	return nil
}

func (e *builtinEngine) infoLocked(it *btItem) TorrentInfo {
	out := TorrentInfo{
		Hash: it.Hash, Name: it.Name, Dir: it.Dir, Category: it.Category,
		Sequential: it.Sequential, FirstLast: it.FirstLast, AddedAt: it.Added, CompletedAt: it.Completed,
		SeedingTime: it.SeedSecs, Uploaded: it.Uploaded + it.sessUp, Downloaded: it.Downloaded + it.sessDown,
		UpSpeed: it.upSpeed, DlSpeed: it.downSpeed, ETA: -1, Error: it.err,
	}
	if out.Name == "" {
		out.Name = it.Hash
	}
	hasInfo := it.t != nil && it.t.Info() != nil
	if _, err := os.Stat(e.metaPath(it.Hash)); err == nil {
		hasInfo = true
	}
	out.HasMetadata = hasInfo
	if hasInfo {
		priv := it.Private
		out.Private = &priv
	}
	out.Size, out.Done = it.Size, it.Done
	if it.t != nil {
		st := it.t.Stats()
		out.Peers, out.Seeds = st.ActivePeers-st.ConnectedSeeders, st.ConnectedSeeders
		out.PeersTotal, out.SeedsTotal = st.TotalPeers, st.ConnectedSeeders
	}
	if out.Size > 0 {
		out.Progress = float64(out.Done) / float64(out.Size)
		out.Ratio = float64(out.Uploaded) / float64(out.Size)
		if out.DlSpeed > 0 {
			out.ETA = (out.Size - out.Done) / out.DlSpeed
		}
	}
	complete := it.Completed > 0 && (out.Size == 0 || out.Done >= out.Size)
	switch {
	case it.err != "":
		out.State = tsError
	case it.Paused && complete:
		out.State = tsCompleted
	case it.Paused:
		out.State = tsPaused
	case it.t == nil:
		out.State = tsQueued
	case !hasInfo:
		out.State = tsMetadata
	case it.queued:
		out.State = tsQueued
	case complete:
		out.State = tsSeeding
	case out.DlSpeed > 0:
		out.State = tsDownloading
	default:
		out.State = tsStalled
	}
	return out
}

func (e *builtinEngine) List(ctx context.Context) ([]TorrentInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []TorrentInfo{}
	for _, it := range e.orderedLocked() {
		out = append(out, e.infoLocked(it))
	}
	return out, nil
}

func (e *builtinEngine) item(hash string) (*btItem, error) {
	it := e.items[hash]
	if it == nil {
		return nil, errNotFound
	}
	return it, nil
}

func (e *builtinEngine) Detail(ctx context.Context, hash string) (TorrentDetail, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, err := e.item(hash)
	if err != nil {
		return TorrentDetail{}, err
	}
	d := TorrentDetail{TorrentInfo: e.infoLocked(it), Files: []TorrentFile{}, Trackers: []TorrentTracker{}}
	var mi *metainfo.MetaInfo
	if it.t != nil && it.t.Info() != nil {
		for i, f := range it.t.Files() {
			p, ok := it.Prio[i]
			if !ok {
				p = prioNormal
			}
			prog := 0.0
			if f.Length() > 0 {
				prog = float64(f.BytesCompleted()) / float64(f.Length())
			}
			d.Files = append(d.Files, TorrentFile{Index: i, Name: f.DisplayPath(), Size: f.Length(), Progress: prog, Priority: p})
		}
		m := it.t.Metainfo()
		mi = &m
	} else if m, err := metainfo.LoadFromFile(e.metaPath(hash)); err == nil {
		mi = m
		if info, err := m.UnmarshalInfo(); err == nil {
			for i, f := range info.UpvertedFiles() {
				p, ok := it.Prio[i]
				if !ok {
					p = prioNormal
				}
				d.Files = append(d.Files, TorrentFile{Index: i, Name: f.DisplayPath(&info), Size: f.Length, Priority: p})
			}
		}
	}
	if mi != nil {
		for tier, urls := range mi.UpvertedAnnounceList() {
			for _, u := range urls {
				st := "not_contacted"
				if it.t != nil {
					st = "working"
				}
				d.Trackers = append(d.Trackers, TorrentTracker{URL: u, Status: st, Tier: tier})
			}
		}
	}
	return d, nil
}

func (e *builtinEngine) Add(ctx context.Context, a TorrentAdd) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.items[a.Hash] != nil {
		return errors.New("that torrent is already in the list")
	}
	it := &btItem{Hash: a.Hash, Magnet: a.Magnet, Dir: a.Dir, Category: a.Category, Paused: a.Paused,
		Sequential: a.Sequential, FirstLast: a.FirstLast, Added: time.Now().Unix(), InTemp: e.s.IncompleteDir != ""}
	if a.File != nil {
		if err := os.MkdirAll(filepath.Dir(e.metaPath(a.Hash)), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(e.metaPath(a.Hash), a.File, 0o600); err != nil {
			return err
		}
		if m, err := parseTorrentFile(a.File); err == nil {
			it.Name, it.Private = m.Name, m.Private
		}
	} else if m, err := parseMagnet(a.Magnet); err == nil {
		it.Name = m.Name
	}
	e.items[a.Hash] = it
	if !a.Paused && e.cl != nil {
		if err := e.attachLocked(it); err != nil {
			delete(e.items, a.Hash)
			return err
		}
	}
	e.saveLocked()
	return nil
}

func (e *builtinEngine) Act(ctx context.Context, hash, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, err := e.item(hash)
	if err != nil {
		return err
	}
	switch action {
	case "pause":
		e.detachLocked(it)
		it.Paused = true
	case "resume":
		it.Paused, it.err = false, ""
		if e.cl != nil {
			if err := e.attachLocked(it); err != nil {
				return err
			}
		}
		// A torrent that hit its seeding limit starts counting again.
		it.SeedSecs = 0
	case "recheck":
		if it.t == nil || it.t.Info() == nil {
			return errors.New("resume the torrent first - it has to be running to be checked")
		}
		t := it.t
		go func() { _ = t.VerifyData() }()
	default:
		return fmt.Errorf("unknown action %q", action)
	}
	e.saveLocked()
	return nil
}

func (e *builtinEngine) Remove(ctx context.Context, hash string, deleteFiles bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, err := e.item(hash)
	if err != nil {
		return err
	}
	name := it.Name
	if it.t != nil && it.t.Info() != nil {
		name = it.t.Info().BestName()
	}
	dir := e.dirFor(it)
	e.detachLocked(it)
	delete(e.items, hash)
	_ = os.Remove(e.metaPath(hash))
	e.saveLocked()
	if deleteFiles && safeName(name) {
		p := filepath.Join(dir, name)
		if pathPolicy.Allowed(p) {
			_ = os.Remove(p + ".part") // a single-file torrent still downloading
			return os.RemoveAll(p)
		}
	}
	return nil
}

func (e *builtinEngine) SetFilePriority(ctx context.Context, hash string, ids []int, prio int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, err := e.item(hash)
	if err != nil {
		return err
	}
	if it.Prio == nil {
		it.Prio = map[int]int{}
	}
	var files []*torrent.File
	if it.t != nil && it.t.Info() != nil {
		files = it.t.Files()
	}
	for _, id := range ids {
		if id < 0 || (files != nil && id >= len(files)) {
			return fmt.Errorf("no file %d in this torrent", id)
		}
		it.Prio[id] = prio
		if files != nil {
			files[id].SetPriority(btPrio(prio))
		}
	}
	e.saveLocked()
	return nil
}

func (e *builtinEngine) SetOptions(ctx context.Context, hash string, sequential, firstLast bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, err := e.item(hash)
	if err != nil {
		return err
	}
	it.Sequential, it.FirstLast = sequential, firstLast
	e.saveLocked()
	return nil
}

func (e *builtinEngine) AddTrackers(ctx context.Context, hash string, urls []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	it, err := e.item(hash)
	if err != nil {
		return err
	}
	if it.t == nil {
		return errors.New("not running")
	}
	if it.Private { // belt and braces: the manager already checks
		return nil
	}
	it.t.AddTrackers([][]string{urls})
	it.Trackers = urls
	e.saveLocked()
	return nil
}
