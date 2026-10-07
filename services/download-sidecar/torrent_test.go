package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// fakeEngine records what the manager asks of an engine.
type fakeEngine struct {
	mu       sync.Mutex
	running  bool
	starts   int
	stops    int
	list     []TorrentInfo
	added    []TorrentAdd
	trackers map[string][]string
	limits   [2]int64
}

func (f *fakeEngine) Start(ctx context.Context, s TorrentSettings, dl, up int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running, f.limits = true, [2]int64{dl, up}
	f.starts++
	return nil
}
func (f *fakeEngine) Stop() error {
	f.mu.Lock()
	f.running = false
	f.stops++
	f.mu.Unlock()
	return nil
}
func (f *fakeEngine) Running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}
func (f *fakeEngine) SetLimits(ctx context.Context, dl, up int64) error {
	f.mu.Lock()
	f.limits = [2]int64{dl, up}
	f.mu.Unlock()
	return nil
}
func (f *fakeEngine) List(ctx context.Context) ([]TorrentInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TorrentInfo(nil), f.list...), nil
}
func (f *fakeEngine) Detail(ctx context.Context, h string) (TorrentDetail, error) {
	return TorrentDetail{}, errNotFound
}
func (f *fakeEngine) Add(ctx context.Context, a TorrentAdd) error {
	f.mu.Lock()
	f.added = append(f.added, a)
	f.mu.Unlock()
	return nil
}
func (f *fakeEngine) Act(ctx context.Context, h, a string) error           { return nil }
func (f *fakeEngine) Remove(ctx context.Context, h string, del bool) error { return nil }
func (f *fakeEngine) SetFilePriority(ctx context.Context, h string, ids []int, p int) error {
	return nil
}
func (f *fakeEngine) SetOptions(ctx context.Context, h string, s, fl bool) error { return nil }
func (f *fakeEngine) AddTrackers(ctx context.Context, h string, urls []string) error {
	f.mu.Lock()
	f.trackers[h] = urls
	f.mu.Unlock()
	return nil
}

func newTestTorrents(t *testing.T) (*Torrents, *fakeEngine, string) {
	t.Helper()
	dir := t.TempDir()
	st := NewSettingsStore(dir, dir)
	_, _ = st.Update(func(s *Settings) { s.Torrent.Engine = "builtin"; s.Torrent.SaveDir = filepath.Join(dir, "dl") })
	tm := NewTorrents(dir, st, newTransport(true), newEventLog(10))
	fe := &fakeEngine{trackers: map[string][]string{}}
	tm.newEng = func(string, TorrentSettings) TorrentEngine { return fe }
	return tm, fe, dir
}

const testMagnet = "magnet:?xt=urn:btih:7acf8fb590b2060dd9c3146ef770169d593433b0&dn=debian.iso"

func makeTorrent(t *testing.T, private bool) []byte {
	t.Helper()
	info := metainfo.Info{Name: "file.bin", PieceLength: 16384, Pieces: make([]byte, 20), Length: 10}
	if private {
		info.Private = &private
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib, Announce: "http://tracker.example/announce"}
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseMagnetAndTorrent(t *testing.T) {
	m, err := parseMagnet(testMagnet)
	if err != nil || m.Hash != "7acf8fb590b2060dd9c3146ef770169d593433b0" || m.Name != "debian.iso" || m.HasInfo {
		t.Fatalf("magnet: %+v %v", m, err)
	}
	// base32 info hash
	if m, err := parseMagnet("magnet:?xt=urn:btih:PLHY7NMQWIDA3WODCRXPO4AWTVMTIM5Q"); err != nil || m.Hash != "7acf8fb590b2060dd9c3146ef770169d593433b0" {
		t.Fatalf("base32 magnet: %+v %v", m, err)
	}
	for _, bad := range []string{"", "http://x/y.torrent", "magnet:?dn=nohash", "magnet:?xt=urn:btih:zz"} {
		if _, err := parseMagnet(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	pub, err := parseTorrentFile(makeTorrent(t, false))
	if err != nil || pub.Private || !pub.HasInfo || pub.Name != "file.bin" || !hashRe.MatchString(pub.Hash) {
		t.Fatalf("public torrent: %+v %v", pub, err)
	}
	priv, err := parseTorrentFile(makeTorrent(t, true))
	if err != nil || !priv.Private {
		t.Fatalf("private torrent: %+v %v", priv, err)
	}
	if priv.Hash == pub.Hash {
		t.Fatal("private flag is part of the info dict, hashes must differ")
	}
	for _, bad := range [][]byte{nil, []byte("not bencode"), []byte("d4:infod4:name1:xee")} {
		if _, err := parseTorrentFile(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTorrentSettingsValidate(t *testing.T) {
	ok := defaultTorrentSettings("/DATA/Downloads")
	if err := ok.validate(); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}
	cases := map[string]func(*TorrentSettings){
		"engine":        func(s *TorrentSettings) { s.Engine = "transmission" },
		"external url":  func(s *TorrentSettings) { s.Engine = "external"; s.ExternalURL = "ftp://x" },
		"negative":      func(s *TorrentSettings) { s.DlLimit = -1 },
		"schedule":      func(s *TorrentSettings) { s.ScheduleFrom = "25:00" },
		"days":          func(s *TorrentSettings) { s.ScheduleDays = "mondays" },
		"queue":         func(s *TorrentSettings) { s.MaxActiveDownloads = -2 },
		"ratio":         func(s *TorrentSettings) { s.RatioLimit = -1 },
		"action":        func(s *TorrentSettings) { s.SeedLimitAction = "delete" },
		"low port":      func(s *TorrentSettings) { s.ListenPort = 80 },
		"webui port":    func(s *TorrentSettings) { s.ListenPort = qbtWebUIPort },
		"encryption":    func(s *TorrentSettings) { s.Encryption = "force" },
		"conns":         func(s *TorrentSettings) { s.MaxConns = -5 },
		"trackers url":  func(s *TorrentSettings) { s.TrackersURL = "file:///etc/passwd" },
		"interval":      func(s *TorrentSettings) { s.TrackersInterval = 0 },
		"category name": func(s *TorrentSettings) { s.Categories = []TorrentCategory{{Name: "a/b"}} },
		"category dup":  func(s *TorrentSettings) { s.Categories = []TorrentCategory{{Name: "TV"}, {Name: "tv"}} },
		"relative dir":  func(s *TorrentSettings) { s.IncompleteDir = "incomplete" },
	}
	for name, mutate := range cases {
		s := defaultTorrentSettings("/DATA/Downloads")
		mutate(&s)
		if err := s.validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	s := defaultTorrentSettings("/DATA/Downloads")
	s.Engine, s.ExternalURL = "external", "http://192.168.1.5:8181"
	s.Categories = []TorrentCategory{{Name: " Movies ", Dir: "/DATA/Movies"}}
	if err := s.validate(); err != nil || s.Categories[0].Name != "Movies" {
		t.Fatalf("valid settings rejected: %v %+v", err, s.Categories)
	}
	if d, ok := s.categoryDir("movies"); !ok || d != "/DATA/Movies" {
		t.Fatal("category lookup", d, ok)
	}
	s.Categories = []TorrentCategory{{Name: "TV"}}
	if d, _ := s.categoryDir("TV"); d != "/DATA/Downloads/TV" {
		t.Fatal("category default folder", d)
	}
}

func TestAltSpeedSchedule(t *testing.T) {
	s := defaultTorrentSettings("/x")
	s.DlLimit, s.AltDlLimit = 100, 10
	s.Schedule, s.ScheduleFrom, s.ScheduleTo = true, "22:00", "06:00"
	at := func(day, hm string) time.Time {
		tm, _ := time.ParseInLocation("2006-01-02 15:04", day+" "+hm, time.Local)
		return tm
	}
	mon, sat := "2026-10-05", "2026-10-10"
	if dl, _ := s.limitsAt(at(mon, "23:30")); dl != 10 {
		t.Error("23:30 should be in the overnight window")
	}
	if dl, _ := s.limitsAt(at(mon, "05:59")); dl != 10 {
		t.Error("05:59 should be in the overnight window")
	}
	if dl, _ := s.limitsAt(at(mon, "12:00")); dl != 100 {
		t.Error("noon is outside it")
	}
	s.ScheduleDays = "weekends"
	if s.altActive(at(mon, "23:30")) || !s.altActive(at(sat, "23:30")) {
		t.Error("weekends only")
	}
	s.Schedule, s.AltSpeed = false, true
	if !s.altActive(at(mon, "12:00")) {
		t.Error("manual alt speed")
	}
}

func TestTrackerListFetchAndPrivateRule(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "udp://tracker.one:1337/announce\n\n# comment\nhttps://tracker.two/announce\nudp://tracker.one:1337/announce\nfile:///etc/passwd\njavascript:alert(1)\nwss://tracker.three\n")
	}))
	defer srv.Close()
	tm, fe, _ := newTestTorrents(t)
	_, _ = tm.st.Update(func(s *Settings) { s.Torrent.TrackersURL = srv.URL })
	tm.maybeRefreshTrackers(context.Background(), false)
	tl := tm.Trackers()
	want := []string{"udp://tracker.one:1337/announce", "https://tracker.two/announce", "wss://tracker.three"}
	if strings.Join(tl.Trackers, ",") != strings.Join(want, ",") || tl.Version != 1 || tl.UpdatedAt.IsZero() {
		t.Fatalf("list: %+v", tl)
	}
	// Not due again until the interval passes.
	tm.maybeRefreshTrackers(context.Background(), false)
	if tm.Trackers().Version != 1 {
		t.Fatal("refetched before the interval")
	}

	yes, no := true, false
	fe.running = true
	fe.list = []TorrentInfo{
		{Hash: strings.Repeat("a", 40), HasMetadata: true, Private: &no, State: tsDownloading},
		{Hash: strings.Repeat("b", 40), HasMetadata: true, Private: &yes, State: tsDownloading},
		{Hash: strings.Repeat("c", 40), HasMetadata: false, Private: nil, State: tsMetadata},
		{Hash: strings.Repeat("d", 40), HasMetadata: true, Private: nil, State: tsDownloading},
	}
	tm.tick(context.Background())
	if len(fe.trackers) != 1 || len(fe.trackers[strings.Repeat("a", 40)]) != 3 {
		t.Fatalf("trackers must go to the public torrent only: %v", fe.trackers)
	}
	// Once per list version.
	delete(fe.trackers, strings.Repeat("a", 40))
	tm.tick(context.Background())
	if len(fe.trackers) != 0 {
		t.Fatal("re-added the same list version")
	}
	// Auto-add off: nothing.
	_, _ = tm.st.Update(func(s *Settings) { s.Torrent.TrackersAuto = false })
	fe.list[0].Hash = strings.Repeat("e", 40)
	tm.tick(context.Background())
	if len(fe.trackers) != 0 {
		t.Fatal("auto-add is off")
	}

	// A list with nothing usable keeps the old one and reports why.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>nope</html>") }))
	defer bad.Close()
	_, _ = tm.st.Update(func(s *Settings) { s.Torrent.TrackersURL = bad.URL })
	tm.maybeRefreshTrackers(context.Background(), true)
	if tl := tm.Trackers(); tl.Error == "" || len(tl.Trackers) != 3 {
		t.Fatalf("bad list: %+v", tl)
	}
}

func TestTorrentManagerAddAndRoots(t *testing.T) {
	tm, fe, dir := newTestTorrents(t)
	ctx := context.Background()
	if _, err := tm.Add(ctx, TorrentAddRequest{Source: testMagnet, Paused: true}); err != nil {
		t.Fatal(err)
	}
	if fe.starts != 1 || len(fe.added) != 1 || fe.added[0].Dir != filepath.Join(dir, "dl") || !fe.added[0].Paused || fe.added[0].Hash == "" {
		t.Fatalf("add: starts=%d %+v", fe.starts, fe.added)
	}
	if _, err := os.Stat(filepath.Join(dir, "dl")); err != nil {
		t.Fatal("save folder not created")
	}
	// A folder outside the storage roots is refused before the engine sees it.
	if _, err := tm.Add(ctx, TorrentAddRequest{Source: testMagnet, Dir: "/etc/cron.d"}); err == nil {
		t.Fatal("/etc accepted")
	}
	// Category -> its folder.
	_, _ = tm.st.Update(func(s *Settings) {
		s.Torrent.Categories = []TorrentCategory{{Name: "Linux", Dir: filepath.Join(dir, "iso")}}
	})
	if _, err := tm.Add(ctx, TorrentAddRequest{Torrent: makeTorrent(t, false), Category: "Linux"}); err != nil {
		t.Fatal(err)
	}
	if a := fe.added[len(fe.added)-1]; a.Dir != filepath.Join(dir, "iso") || a.Category != "Linux" || a.File == nil {
		t.Fatalf("category add: %+v", a)
	}
	if _, err := tm.Add(ctx, TorrentAddRequest{Source: testMagnet, Category: "Nope"}); err == nil {
		t.Fatal("unknown category accepted")
	}
	// A link to a .torrent is fetched by the sidecar.
	tf := makeTorrent(t, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(tf) }))
	defer srv.Close()
	if m, err := tm.Add(ctx, TorrentAddRequest{Source: srv.URL + "/x.torrent"}); err != nil || !m.Private {
		t.Fatalf("url add: %+v %v", m, err)
	}
	for _, bad := range []TorrentAddRequest{{}, {Source: "ftp://x/y.torrent"}, {Torrent: []byte("junk")}} {
		if _, err := tm.Add(ctx, bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestTorrentManagerIdleAndCache(t *testing.T) {
	tm, fe, dir := newTestTorrents(t)
	ctx := context.Background()
	tm.idleWait = 0
	fe.list = []TorrentInfo{{Hash: strings.Repeat("a", 40), Name: "x", State: tsDownloading, DlSpeed: 5, Size: 10, Done: 5}}
	if _, err := tm.engine(ctx, true); err != nil {
		t.Fatal(err)
	}
	tm.tick(ctx)
	if !fe.running {
		t.Fatal("stopped while a torrent is active")
	}
	// It finishes: one notification, then the engine stops once idle.
	fe.list[0].State, fe.list[0].Done = tsCompleted, 10
	tm.tick(ctx)
	if ev, _ := tm.events.Since(0); len(ev) != 1 || ev[0].Kind != "completed" || ev[0].Filename != "x" {
		t.Fatalf("events: %+v", ev)
	}
	if fe.running || fe.stops != 1 {
		t.Fatal("not stopped when idle")
	}
	// Stopped: List serves the cache (speeds zeroed) and doesn't start it.
	list, running, err := tm.List(ctx)
	if err != nil || running || len(list) != 1 || list[0].DlSpeed != 0 || fe.starts != 1 {
		t.Fatalf("cached list: %+v running=%v starts=%d", list, running, fe.starts)
	}
	// The cache survives a restart; an active torrent in it wakes the engine.
	raw, _ := os.ReadFile(filepath.Join(dir, "torrents-cache.json"))
	var cached []TorrentInfo
	if json.Unmarshal(raw, &cached) != nil || len(cached) != 1 {
		t.Fatalf("cache file: %s", raw)
	}
	cached[0].State = tsDownloading
	_ = writeJSONAtomic(filepath.Join(dir, "torrents-cache.json"), cached)
	tm2 := NewTorrents(dir, tm.st, newTransport(true), newEventLog(10))
	fe2 := &fakeEngine{trackers: map[string][]string{}}
	tm2.newEng = func(string, TorrentSettings) TorrentEngine { return fe2 }
	cctx, cancel := context.WithCancel(ctx)
	tm2.Start(cctx)
	cancel()
	if fe2.starts != 1 {
		t.Fatal("an active torrent at startup must wake the engine")
	}
	// Speed limits follow the alternative switch on the next tick.
	_, _ = tm.st.Update(func(s *Settings) { s.Torrent.AltSpeed = true; s.Torrent.AltDlLimit = 7 })
	fe.running = true
	fe.list[0].State = tsDownloading
	tm.tick(ctx)
	if fe.limits[0] != 7 {
		t.Fatalf("alt limit not applied: %v", fe.limits)
	}
}

func TestWatchFolder(t *testing.T) {
	tm, fe, dir := newTestTorrents(t)
	watch := filepath.Join(dir, "watch")
	_ = os.MkdirAll(watch, 0o755)
	_, _ = tm.st.Update(func(s *Settings) { s.Torrent.WatchDir = watch })
	_ = os.WriteFile(filepath.Join(watch, "good.torrent"), makeTorrent(t, false), 0o644)
	_ = os.WriteFile(filepath.Join(watch, "bad.torrent"), []byte("junk"), 0o644)
	_ = os.WriteFile(filepath.Join(watch, "notes.txt"), []byte("x"), 0o644)
	tm.scanWatchDir(context.Background())
	if len(fe.added) != 1 {
		t.Fatalf("added %d", len(fe.added))
	}
	for _, f := range []string{"good.torrent.added", "bad.torrent.invalid", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(watch, f)); err != nil {
			t.Error(f, "missing")
		}
	}
}

func TestSettingsPutTorrent(t *testing.T) {
	dir := t.TempDir()
	st := NewSettingsStore(dir, dir)
	_, _ = st.Update(func(s *Settings) { s.Torrent.ExternalPassword = "secret" })
	mux := http.NewServeMux()
	RegisterRoutes(mux, &Manager{}, st, nil, nil, nil)
	put := func(body string) (int, map[string]interface{}) {
		req := httptest.NewRequest("PUT", "/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, out := put(`{"torrent":{"dl_limit":1000}}`)
	tor, _ := out["torrent"].(map[string]interface{})
	if code != 200 || tor["dl_limit"].(float64) != 1000 || tor["listen_port"].(float64) != defaultTorrentPort {
		t.Fatalf("partial torrent patch: %d %v", code, out)
	}
	if _, leaked := tor["external_password"]; leaked || tor["external_password_set"] != true || st.Get().Torrent.ExternalPassword != "secret" {
		t.Fatalf("password must stay on the server: %v", tor)
	}
	if code, _ := put(`{"torrent":{"listen_port":22}}`); code != 400 {
		t.Fatal("bad port accepted", code)
	}
	if code, _ := put(`{"torrent":{"incomplete_dir":"/etc/x"}}`); code != 400 {
		t.Fatal("folder outside the roots accepted", code)
	}
}

// A minimal qBittorrent WebUI: login, 5.x stop/start names, add.
func TestQbtEngineAgainstFakeAPI(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/api/v2/auth/login" {
			_ = r.ParseForm()
			if r.Form.Get("password") != "pw" {
				io.WriteString(w, "Fails.")
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s", Path: "/"})
			io.WriteString(w, "Ok.")
			return
		}
		if c, err := r.Cookie("SID"); err != nil || c.Value != "s" {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/api/v2/app/version":
			io.WriteString(w, "v4.6.0")
		case "/api/v2/torrents/pause": // a 4.x server: no stop/start
			w.WriteHeader(200)
		case "/api/v2/torrents/add":
			if err := r.ParseMultipartForm(1 << 20); err != nil || r.FormValue("urls") == "" || r.FormValue("savepath") != "/DATA/x" {
				io.WriteString(w, "Fails.")
				return
			}
			io.WriteString(w, "Ok.")
		case "/api/v2/torrents/info":
			io.WriteString(w, `[{"hash":"aa","name":"n","state":"stalledUP","size":10,"completed":10,"progress":1,"eta":8640000,"num_seeds":1,"num_complete":9}]`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	q := newQbtEngine(srv.URL, "admin", "pw", false, t.TempDir())
	ctx := context.Background()
	if err := q.Start(ctx, TorrentSettings{}, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := q.Add(ctx, TorrentAdd{Magnet: testMagnet, Dir: "/DATA/x"}); err != nil {
		t.Fatal(err)
	}
	if err := q.Act(ctx, "aa", "pause"); err != nil {
		t.Fatal("stop -> pause fallback:", err)
	}
	l, err := q.List(ctx)
	if err != nil || len(l) != 1 || l[0].State != tsSeeding || l[0].ETA != -1 || l[0].Private != nil || !l[0].HasMetadata || l[0].SeedsTotal != 9 {
		t.Fatalf("list: %+v %v", l, err)
	}
	bad := newQbtEngine(srv.URL, "admin", "wrong", false, t.TempDir())
	if err := bad.Start(ctx, TorrentSettings{}, 0, 0); err != errQbtAuth {
		t.Fatal("wrong password:", err)
	}
	// Bundled prefs always carry the lock-down.
	p := qbtPrefs(defaultTorrentSettings("/DATA"), 0, 0)
	if p["web_ui_address"] != "127.0.0.1" || p["autorun_enabled"] != false || p["web_ui_upnp"] != false || p["bypass_local_auth"] != false || p["add_trackers_enabled"] != false {
		t.Fatalf("lock-down prefs: %v", p)
	}
}

// The bundled profile: a PBKDF2 password only we know, WebUI on loopback.
func TestQbtProfile(t *testing.T) {
	old := qbtProfileDir
	qbtProfileDir = t.TempDir()
	defer func() { qbtProfileDir = old }()
	q := newQbtEngine("http://127.0.0.1:1", "nivaroos", "", true, t.TempDir())
	if err := q.ensureProfile(false); err != nil {
		t.Fatal(err)
	}
	conf, _ := os.ReadFile(filepath.Join(qbtProfileDir, "qBittorrent", "config", "qBittorrent.conf"))
	if !strings.Contains(string(conf), `WebUI\Address=127.0.0.1`) || !strings.Contains(string(conf), "Password_PBKDF2=\"@ByteArray(") || strings.Contains(string(conf), q.pass) || len(q.pass) < 32 {
		t.Fatalf("conf:\n%s", conf)
	}
	pass := q.pass
	_ = q.ensureProfile(false)
	if q.pass != pass {
		t.Fatal("password regenerated without need")
	}
}
