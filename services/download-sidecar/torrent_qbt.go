package main

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// qBittorrent through its WebUI API (v2). Two flavours:
//
//   - bundled: qbittorrent-nox as nivaroos-torrent.service, a sandboxed
//     unit that is never enabled - this service starts it when a torrent
//     needs it and stops it when nothing is active. Its WebUI listens on
//     127.0.0.1 only, with a random password only this service knows
//     (written into its profile before the first start), so nothing else
//     on the box or the LAN can drive it. Every start re-applies our
//     settings, including the ones that keep it locked down (no external
//     programs, no UPnP for the WebUI, no localhost auth bypass).
//   - external: the user's own qBittorrent. We log in and drive torrents,
//     but never change its preferences.
//
// Auth: a cookie login, then (bundled, qBittorrent 5.2+) its API key for
// every call - no session to expire. Older versions keep the cookie, and a
// refused key falls back to a fresh login.

const (
	qbtUnit    = "nivaroos-torrent.service"
	qbtBinary  = "/opt/nivaroos/qbittorrent/qbittorrent-nox" // install-qbittorrent.sh's link
	qbtUnitDir = "/usr/lib/systemd/system"
)

// Where the bundled instance keeps its profile (--profile=...); a var for tests.
var qbtProfileDir = "/var/lib/nivaroos/torrent"

func qbtInstalled() bool {
	_, e1 := os.Stat(qbtBinary)
	_, e2 := os.Stat(filepath.Join(qbtUnitDir, qbtUnit))
	return e1 == nil && e2 == nil
}

// systemctl is a var so the tests never touch the real one.
var systemctl = func(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

type qbtEngine struct {
	base, user, pass string
	bundled          bool
	dataDir          string
	hc               *http.Client
	loginMu          sync.Mutex
	apiKey           string // guarded by loginMu
}

func newQbtEngine(base, user, pass string, bundled bool, dataDir string) *qbtEngine {
	jar, _ := cookiejar.New(nil)
	return &qbtEngine{
		base:    strings.TrimRight(strings.TrimSpace(base), "/"),
		user:    user,
		pass:    pass,
		bundled: bundled,
		dataDir: dataDir,
		// Not the netguard transport: the bundled one is on loopback, the
		// external one is an address the admin entered. No env proxy.
		hc: &http.Client{Jar: jar, Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}},
	}
}

var errQbtAuth = errors.New("qBittorrent refused the username or password")

type qbtStatusError struct {
	code int
	body string
}

func (e qbtStatusError) Error() string {
	if e.body != "" {
		return fmt.Sprintf("qBittorrent: %s (HTTP %d)", e.body, e.code)
	}
	return fmt.Sprintf("qBittorrent: HTTP %d", e.code)
}

func (q *qbtEngine) key() string {
	q.loginMu.Lock()
	defer q.loginMu.Unlock()
	return q.apiKey
}

func (q *qbtEngine) setKey(k string) {
	q.loginMu.Lock()
	q.apiKey = k
	q.loginMu.Unlock()
}

func (q *qbtEngine) login(ctx context.Context) error {
	q.loginMu.Lock()
	defer q.loginMu.Unlock()
	if q.bundled && q.pass == "" {
		// We restarted while the engine kept running: no Start read it yet.
		if b, err := os.ReadFile(filepath.Join(q.dataDir, "qbt-webui.secret")); err == nil {
			q.pass = strings.TrimSpace(string(b))
		}
	}
	form := url.Values{"username": {q.user}, "password": {q.pass}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", q.base)
	resp, err := q.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	// Up to 5.1: 200 "Ok." or 200 "Fails."; 5.2: 204, or 401.
	if (resp.StatusCode != 200 && resp.StatusCode != 204) || strings.TrimSpace(string(b)) == "Fails." {
		return errQbtAuth
	}
	if q.bundled {
		q.apiKey = q.fetchAPIKey(ctx)
	}
	return nil
}

// fetchAPIKey returns qBittorrent 5.2+'s API key, creating one if the
// profile has none; "" on older versions. Runs right after the cookie login.
func (q *qbtEngine) fetchAPIKey(ctx context.Context) string {
	get := func(method, path string) []byte {
		req, _ := http.NewRequestWithContext(ctx, method, q.base+"/api/v2/"+path, nil)
		req.Header.Set("Referer", q.base)
		resp, err := q.hc.Do(req)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return b
	}
	var p struct {
		Key *string `json:"web_ui_api_key"`
	}
	if json.Unmarshal(get(http.MethodGet, "app/preferences"), &p) != nil || p.Key == nil {
		return ""
	}
	if *p.Key != "" {
		return *p.Key
	}
	var r struct {
		APIKey string `json:"apiKey"`
	}
	_ = json.Unmarshal(get(http.MethodPost, "app/rotateAPIKey"), &r)
	return r.APIKey
}

// call POSTs (or GETs, with form == nil) an API path, logging in once on 403.
func (q *qbtEngine) call(ctx context.Context, path string, form url.Values) ([]byte, error) {
	return q.send(ctx, path, func() (io.Reader, string) {
		if form == nil {
			return nil, ""
		}
		return strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"
	})
}

func (q *qbtEngine) send(ctx context.Context, path string, body func() (io.Reader, string)) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		r, ct := body()
		method := http.MethodGet
		if r != nil {
			method = http.MethodPost
		}
		req, err := http.NewRequestWithContext(ctx, method, q.base+"/api/v2/"+path, r)
		if err != nil {
			return nil, err
		}
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		req.Header.Set("Referer", q.base)
		key := q.key()
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := q.hc.Do(req)
		if err != nil {
			return nil, err
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden && attempt == 0 {
			if key != "" {
				// Refused key (a sent key also hides the cookie): cookie from here on.
				q.setKey("")
			}
			if err := q.login(ctx); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode != 200 {
			return nil, qbtStatusError{resp.StatusCode, strings.TrimSpace(string(b))}
		}
		return b, nil
	}
}

func (q *qbtEngine) Running() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, q.base+"/api/v2/app/version", nil)
	resp, err := q.hc.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func (q *qbtEngine) Start(ctx context.Context, s TorrentSettings, dl, up int64) error {
	if !q.bundled {
		if !q.Running() {
			return fmt.Errorf("your qBittorrent at %s is not reachable", q.base)
		}
		return q.login(ctx)
	}
	if !qbtInstalled() {
		return errors.New("qbittorrent-nox is not installed - re-run the NivaroOS installer, or switch to the built-in engine")
	}
	if err := q.ensureProfile(false); err != nil {
		return err
	}
	if err := q.up(ctx); err != nil {
		return err
	}
	if err := q.login(ctx); errors.Is(err, errQbtAuth) {
		// Profile from an older install (or a lost secret): write a fresh one.
		_ = systemctl("stop", qbtUnit)
		if err := q.ensureProfile(true); err != nil {
			return err
		}
		if err := q.up(ctx); err != nil {
			return err
		}
		err = q.login(ctx)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return q.setPrefs(ctx, qbtPrefs(s, dl, up))
}

func (q *qbtEngine) up(ctx context.Context) error {
	if !q.Running() {
		if err := systemctl("start", qbtUnit); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	for !q.Running() {
		if time.Now().After(deadline) {
			return errors.New("qBittorrent did not come up within 30 seconds (journalctl -u " + qbtUnit + ")")
		}
		if !sleepCtx(ctx, 300*time.Millisecond) {
			return ctx.Err()
		}
	}
	return nil
}

func (q *qbtEngine) Stop() error {
	if !q.bundled {
		return nil
	}
	return systemctl("stop", qbtUnit)
}

// ensureProfile writes the bundled instance's config (WebUI on loopback,
// our password) unless it's there already and fresh is false.
func (q *qbtEngine) ensureProfile(fresh bool) error {
	secretPath := filepath.Join(q.dataDir, "qbt-webui.secret")
	conf := filepath.Join(qbtProfileDir, "qBittorrent", "config", "qBittorrent.conf")
	secret, err := os.ReadFile(secretPath)
	cur, confErr := os.ReadFile(conf)
	// Rewritten too when it isn't ours (an older layout, another port).
	ours := confErr == nil && strings.Contains(string(cur), fmt.Sprintf("WebUI\\Port=%d\n", qbtWebUIPort))
	if err == nil && len(secret) >= 32 && ours && !fresh {
		q.pass = strings.TrimSpace(string(secret))
		return nil
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	pass := hex.EncodeToString(b)
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key, err := pbkdf2.Key(sha512.New, pass, salt, 100000, 64)
	if err != nil {
		return err
	}
	ini := fmt.Sprintf(`[LegalNotice]
Accepted=true

[BitTorrent]
Session\Port=%d

[Preferences]
WebUI\Address=127.0.0.1
WebUI\Port=%d
WebUI\Username=nivaroos
WebUI\Password_PBKDF2="@ByteArray(%s:%s)"
WebUI\LocalHostAuth=true
WebUI\UseUPnP=false
WebUI\CSRFProtection=true
WebUI\HostHeaderValidation=true
`, defaultTorrentPort, qbtWebUIPort, base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(key))
	if err := os.MkdirAll(filepath.Dir(conf), 0o700); err != nil {
		return fmt.Errorf("torrent engine profile: %w", err)
	}
	if err := os.WriteFile(conf+".tmp", []byte(ini), 0o600); err != nil {
		return fmt.Errorf("torrent engine profile: %w", err)
	}
	if err := os.Rename(conf+".tmp", conf); err != nil {
		return err
	}
	if err := os.WriteFile(secretPath, []byte(pass), 0o600); err != nil {
		return err
	}
	q.pass = pass
	return nil
}

func unlimited(v int) int {
	if v <= 0 {
		return -1
	}
	return v
}

// qbtPrefs maps our settings onto qBittorrent's preferences. The first
// block is the lock-down, re-applied on every start.
func qbtPrefs(s TorrentSettings, dl, up int64) map[string]interface{} {
	enc := 0 // libtorrent "enabled": accepts both, tries encrypted first (allow and prefer)
	if s.Encryption == "require" {
		enc = 1
	}
	act := 0 // stop
	if s.SeedLimitAction == "remove" {
		act = 1
	}
	return map[string]interface{}{
		"web_ui_address":                       "127.0.0.1",
		"web_ui_port":                          qbtWebUIPort,
		"web_ui_upnp":                          false,
		"bypass_local_auth":                    false,
		"bypass_auth_subnet_whitelist_enabled": false,
		"autorun_enabled":                      false,
		"autorun_on_torrent_added_enabled":     false,
		"mail_notification_enabled":            false,
		"add_trackers_enabled":                 false, // we add the public list ourselves, never to private torrents
		"scan_dirs":                            map[string]int{},

		"save_path":                s.SaveDir,
		"temp_path_enabled":        s.IncompleteDir != "",
		"temp_path":                s.IncompleteDir,
		"dl_limit":                 dl,
		"up_limit":                 up,
		"scheduler_enabled":        false, // the schedule is ours (SetLimits), same for every engine
		"queueing_enabled":         s.Queueing,
		"max_active_downloads":     unlimited(s.MaxActiveDownloads),
		"max_active_uploads":       unlimited(s.MaxActiveUploads),
		"max_active_torrents":      unlimited(s.MaxActiveTorrents),
		"max_ratio_enabled":        s.RatioLimit > 0,
		"max_ratio":                s.RatioLimit,
		"max_seeding_time_enabled": s.SeedTimeLimit > 0,
		"max_seeding_time":         s.SeedTimeLimit,
		"max_ratio_act":            act,
		"listen_port":              s.ListenPort,
		"random_port":              false,
		"upnp":                     s.UPnP,
		"dht":                      s.DHT,
		"pex":                      s.PeX,
		"lsd":                      s.LSD,
		"encryption":               enc,
		"max_connec":               unlimited(s.MaxConns),
		"max_connec_per_torrent":   unlimited(s.MaxConnsPer),
		"preallocate_all":          s.Preallocate,
	}
}

func (q *qbtEngine) setPrefs(ctx context.Context, p map[string]interface{}) error {
	raw, _ := json.Marshal(p)
	_, err := q.call(ctx, "app/setPreferences", url.Values{"json": {string(raw)}})
	return err
}

func (q *qbtEngine) SetLimits(ctx context.Context, dl, up int64) error {
	if !q.bundled {
		return nil
	}
	return q.setPrefs(ctx, map[string]interface{}{"dl_limit": dl, "up_limit": up})
}

type qbtTorrent struct {
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	State         string  `json:"state"`
	Progress      float64 `json:"progress"`
	Size          int64   `json:"size"`
	Completed     int64   `json:"completed"`
	Downloaded    int64   `json:"downloaded"`
	Uploaded      int64   `json:"uploaded"`
	DlSpeed       int64   `json:"dlspeed"`
	UpSpeed       int64   `json:"upspeed"`
	NumSeeds      int     `json:"num_seeds"`
	NumComplete   int     `json:"num_complete"`
	NumLeechs     int     `json:"num_leechs"`
	NumIncomplete int     `json:"num_incomplete"`
	Ratio         float64 `json:"ratio"`
	ETA           int64   `json:"eta"`
	SavePath      string  `json:"save_path"`
	Category      string  `json:"category"`
	Private       *bool   `json:"private"`
	HasMetadata   *bool   `json:"has_metadata"`
	SeqDl         bool    `json:"seq_dl"`
	FLPiecePrio   bool    `json:"f_l_piece_prio"`
	AddedOn       int64   `json:"added_on"`
	CompletionOn  int64   `json:"completion_on"`
	SeedingTime   int64   `json:"seeding_time"`
}

func qbtState(s string) string {
	switch s {
	case "downloading", "forcedDL":
		return tsDownloading
	case "stalledDL":
		return tsStalled
	case "metaDL", "forcedMetaDL":
		return tsMetadata
	case "uploading", "stalledUP", "forcedUP":
		return tsSeeding
	case "queuedDL", "queuedUP":
		return tsQueued
	case "checkingDL", "checkingUP", "checkingResumeData", "allocating":
		return tsChecking
	case "moving":
		return tsMoving
	case "pausedDL", "stoppedDL":
		return tsPaused
	case "pausedUP", "stoppedUP":
		return tsCompleted
	case "error", "missingFiles":
		return tsError
	}
	return tsStalled
}

func (t qbtTorrent) info() TorrentInfo {
	eta := t.ETA
	if eta >= 8640000 || eta < 0 {
		eta = -1
	}
	hasMeta := t.State != "metaDL" && t.State != "forcedMetaDL"
	if t.HasMetadata != nil {
		hasMeta = *t.HasMetadata
	}
	out := TorrentInfo{
		Hash: t.Hash, Name: t.Name, State: qbtState(t.State), Progress: t.Progress,
		Size: t.Size, Done: t.Completed, Downloaded: t.Downloaded, Uploaded: t.Uploaded,
		DlSpeed: t.DlSpeed, UpSpeed: t.UpSpeed,
		Seeds: t.NumSeeds, SeedsTotal: t.NumComplete, Peers: t.NumLeechs, PeersTotal: t.NumIncomplete,
		Ratio: t.Ratio, ETA: eta, Dir: t.SavePath, Category: t.Category,
		Private: t.Private, HasMetadata: hasMeta, Sequential: t.SeqDl, FirstLast: t.FLPiecePrio,
		AddedAt: t.AddedOn, SeedingTime: t.SeedingTime,
	}
	if t.CompletionOn > 0 {
		out.CompletedAt = t.CompletionOn
	}
	if out.State == tsError {
		out.Error = "qBittorrent reports " + t.State
	}
	return out
}

func (q *qbtEngine) list(ctx context.Context, hash string) ([]TorrentInfo, error) {
	path := "torrents/info"
	if hash != "" {
		path += "?hashes=" + hash
	}
	b, err := q.call(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	var raw []qbtTorrent
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("qBittorrent: %w", err)
	}
	out := make([]TorrentInfo, 0, len(raw))
	for _, t := range raw {
		out = append(out, t.info())
	}
	return out, nil
}

func (q *qbtEngine) List(ctx context.Context) ([]TorrentInfo, error) { return q.list(ctx, "") }

func (q *qbtEngine) one(ctx context.Context, hash string) (TorrentInfo, error) {
	l, err := q.list(ctx, hash)
	if err != nil {
		return TorrentInfo{}, err
	}
	if len(l) == 0 {
		return TorrentInfo{}, errNotFound
	}
	return l[0], nil
}

func (q *qbtEngine) Detail(ctx context.Context, hash string) (TorrentDetail, error) {
	info, err := q.one(ctx, hash)
	if err != nil {
		return TorrentDetail{}, err
	}
	d := TorrentDetail{TorrentInfo: info, Files: []TorrentFile{}, Trackers: []TorrentTracker{}}
	if b, err := q.call(ctx, "torrents/files?hash="+hash, nil); err == nil {
		_ = json.Unmarshal(b, &d.Files)
	}
	var tr []struct {
		URL      string `json:"url"`
		Status   int    `json:"status"`
		Tier     int    `json:"tier"`
		NumPeers int    `json:"num_peers"`
		NumSeeds int    `json:"num_seeds"`
		Msg      string `json:"msg"`
	}
	if b, err := q.call(ctx, "torrents/trackers?hash="+hash, nil); err == nil {
		_ = json.Unmarshal(b, &tr)
	}
	status := []string{"disabled", "not_contacted", "working", "updating", "not_working"}
	for _, t := range tr {
		if strings.HasPrefix(t.URL, "** [") { // DHT/PeX/LSD pseudo-entries
			continue
		}
		st := "not_contacted"
		if t.Status >= 0 && t.Status < len(status) {
			st = status[t.Status]
		}
		d.Trackers = append(d.Trackers, TorrentTracker{URL: t.URL, Status: st, Tier: t.Tier, Peers: t.NumPeers, Seeds: t.NumSeeds, Message: t.Msg})
	}
	return d, nil
}

func (q *qbtEngine) Add(ctx context.Context, a TorrentAdd) error {
	if a.Category != "" {
		form := url.Values{"category": {a.Category}}
		if q.bundled && a.Dir != "" {
			form.Set("savePath", a.Dir)
		}
		_, _ = q.call(ctx, "torrents/createCategory", form) // 409 = exists already
	}
	fields := map[string]string{
		"category":           a.Category,
		"paused":             strconv.FormatBool(a.Paused), // 4.x
		"stopped":            strconv.FormatBool(a.Paused), // 5.x
		"sequentialDownload": strconv.FormatBool(a.Sequential),
		"firstLastPiecePrio": strconv.FormatBool(a.FirstLast),
	}
	if a.Dir != "" {
		fields["savepath"] = a.Dir
		fields["autoTMM"] = "false"
	}
	if a.Magnet != "" {
		fields["urls"] = a.Magnet
	}
	b, err := q.send(ctx, "torrents/add", func() (io.Reader, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for k, v := range fields {
			_ = mw.WriteField(k, v)
		}
		if a.File != nil {
			fw, _ := mw.CreateFormFile("torrents", a.Hash+".torrent")
			_, _ = fw.Write(a.File)
		}
		_ = mw.Close()
		return &buf, mw.FormDataContentType()
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(b)) == "Fails." {
		return errors.New("qBittorrent did not add it (is it already in the list?)")
	}
	return nil
}

func (q *qbtEngine) Act(ctx context.Context, hash, action string) error {
	names := map[string][]string{"pause": {"stop", "pause"}, "resume": {"start", "resume"}, "recheck": {"recheck"}}[action]
	if names == nil {
		return fmt.Errorf("unknown action %q", action)
	}
	var err error
	for _, n := range names { // 5.x renamed pause/resume to stop/start
		_, err = q.call(ctx, "torrents/"+n, url.Values{"hashes": {hash}})
		var se qbtStatusError
		if !errors.As(err, &se) || se.code != 404 {
			return err
		}
	}
	return err
}

func (q *qbtEngine) Remove(ctx context.Context, hash string, deleteFiles bool) error {
	_, err := q.call(ctx, "torrents/delete", url.Values{"hashes": {hash}, "deleteFiles": {strconv.FormatBool(deleteFiles)}})
	return err
}

func (q *qbtEngine) SetFilePriority(ctx context.Context, hash string, ids []int, prio int) error {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.Itoa(id)
	}
	_, err := q.call(ctx, "torrents/filePrio", url.Values{"hash": {hash}, "id": {strings.Join(s, "|")}, "priority": {strconv.Itoa(prio)}})
	return err
}

func (q *qbtEngine) SetOptions(ctx context.Context, hash string, sequential, firstLast bool) error {
	cur, err := q.one(ctx, hash)
	if err != nil {
		return err
	}
	// The API only has toggles.
	if cur.Sequential != sequential {
		if _, err := q.call(ctx, "torrents/toggleSequentialDownload", url.Values{"hashes": {hash}}); err != nil {
			return err
		}
	}
	if cur.FirstLast != firstLast {
		_, err = q.call(ctx, "torrents/toggleFirstLastPiecePrio", url.Values{"hashes": {hash}})
	}
	return err
}

func (q *qbtEngine) AddTrackers(ctx context.Context, hash string, urls []string) error {
	_, err := q.call(ctx, "torrents/addTrackers", url.Values{"hash": {hash}, "urls": {strings.Join(urls, "\n")}})
	return err
}
