package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type State string

const (
	StateQueued      State = "queued"
	StateDownloading State = "downloading"
	StatePaused      State = "paused"
	StateCompleted   State = "completed"
	StateFailed      State = "failed"
)

const (
	// A segment is only ever split in two (IDM-style dynamic segmentation:
	// a connection that finishes early takes over half of the slowest
	// remaining piece) when both halves would still be at least this big -
	// below that, the cost of a fresh request outweighs the parallelism.
	minSplitSize = 1 << 20
	readBufSize  = 128 << 10
	maxRetries   = 8
	partSuffix   = ".part"
)

// A var, not a const, so the tests can shorten it.
var stallTimeout = 30 * time.Second

// Segment is one byte range [Start, End] of the file. End is inclusive, or
// -1 while the total size is still unknown (a server that sent no length).
type Segment struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  int64 `json:"done"`
	// Set once an unknown-length stream hits EOF (End can't express an
	// empty range, so a zero-byte body needs this to count as complete).
	Finished bool `json:"finished,omitempty"`

	active bool
}

func (s *Segment) pos() int64 { return s.Start + s.Done }

func (s *Segment) remaining() int64 {
	if s.End < 0 {
		return -1
	}
	return s.End - s.pos() + 1
}

func (s *Segment) complete() bool { return s.Finished || (s.End >= 0 && s.pos() > s.End) }

type Download struct {
	ID          string            `json:"id"`
	URL         string            `json:"url"`
	FinalURL    string            `json:"final_url,omitempty"`
	Filename    string            `json:"filename"`
	Dir         string            `json:"dir"`
	Size        int64             `json:"size"`
	Resumable   bool              `json:"resumable"`
	ContentType string            `json:"content_type,omitempty"`
	State       State             `json:"state"`
	Error       string            `json:"error,omitempty"`
	Connections int               `json:"connections"`
	Segments    []*Segment        `json:"segments"`
	Headers     map[string]string `json:"headers,omitempty"`
	Source      string            `json:"source,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	// Whether the user explicitly chose Filename (a probe then never
	// overrides it with the server's Content-Disposition name).
	FilenameFixed bool `json:"filename_fixed,omitempty"`

	cancel      context.CancelFunc
	running     bool
	speed       float64
	lastBytes   int64
	activeConns int
	// Set once the server rejected a connection as too many (429/503);
	// no more segments get split off for the rest of this run.
	throttled bool
	// Set by Pause/Delete right before cancelling, so the run loop knows the
	// cancellation was deliberate and which state to land in.
	stopReason State
}

func (d *Download) downloaded() int64 {
	var n int64
	for _, s := range d.Segments {
		n += s.Done
	}
	return n
}

func (d *Download) partPath() string  { return filepath.Join(d.Dir, d.Filename+partSuffix) }
func (d *Download) finalPath() string { return filepath.Join(d.Dir, d.Filename) }

// DownloadView is the JSON shape the UI sees - persisted fields plus the
// live, computed ones.
type DownloadView struct {
	ID                string     `json:"id"`
	URL               string     `json:"url"`
	FinalURL          string     `json:"final_url,omitempty"`
	Filename          string     `json:"filename"`
	Dir               string     `json:"dir"`
	Path              string     `json:"path"`
	Size              int64      `json:"size"`
	Downloaded        int64      `json:"downloaded"`
	Resumable         bool       `json:"resumable"`
	ContentType       string     `json:"content_type,omitempty"`
	State             State      `json:"state"`
	Error             string     `json:"error,omitempty"`
	Connections       int        `json:"connections"`
	ActiveConnections int        `json:"active_connections"`
	Speed             float64    `json:"speed"`
	ETA               int64      `json:"eta"`
	Segments          []Segment  `json:"segments"`
	Source            string     `json:"source,omitempty"`
	HasCookies        bool       `json:"has_cookies"`
	CreatedAt         time.Time  `json:"created_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

type AddRequest struct {
	URL         string            `json:"url"`
	Filename    string            `json:"filename"`
	Dir         string            `json:"dir"`
	Connections int               `json:"connections"`
	Headers     map[string]string `json:"headers"`
	// false = add to the list paused ("Download later").
	Start  *bool  `json:"start"`
	Source string `json:"source"`
}

type Manager struct {
	mu        sync.Mutex
	downloads map[string]*Download
	statePath string
	settings  *SettingsStore
	client    *http.Client
	limiter   *rateLimiter
	dirty     bool
	// Invoked (outside the lock) whenever a download finishes or fails, for
	// the UI's notification polling.
	events *eventLog
	// Closed once the ticker has done its final save after shutdown.
	stopped chan struct{}
}

func NewManager(dataDir string, settings *SettingsStore, transport http.RoundTripper) *Manager {
	m := &Manager{
		downloads: map[string]*Download{},
		statePath: filepath.Join(dataDir, "downloads.json"),
		settings:  settings,
		client:    &http.Client{Transport: transport},
		limiter:   newRateLimiter(settings.Get().SpeedLimit),
		events:    newEventLog(200),
		stopped:   make(chan struct{}),
	}
	settings.OnChange(func(s Settings) {
		m.limiter.SetRate(s.SpeedLimit)
		m.schedule()
	})
	m.load()
	return m
}

func (m *Manager) load() {
	raw, err := os.ReadFile(m.statePath)
	if err != nil {
		return
	}
	var list []*Download
	if err := json.Unmarshal(raw, &list); err != nil {
		log.Printf("downloads.json unreadable, starting empty: %v", err)
		return
	}
	for _, d := range list {
		// Anything that was mid-transfer when the service stopped just goes
		// back in the queue - its segments already record how far each
		// connection got.
		if d.State == StateDownloading {
			d.State = StateQueued
		}
		m.downloads[d.ID] = d
	}
}

// Wait blocks until the ticker started by Start has shut down.
func (m *Manager) Wait() { <-m.stopped }

func (m *Manager) Start(ctx context.Context) {
	m.schedule()
	go m.ticker(ctx)
}

// ticker recomputes per-download speed once a second and flushes state to
// disk every few seconds while anything changed.
func (m *Manager) ticker(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	defer close(m.stopped)
	n := 0
	for {
		select {
		case <-ctx.Done():
			m.save()
			return
		case <-t.C:
		}
		n++
		m.mu.Lock()
		for _, d := range m.downloads {
			cur := d.downloaded()
			if d.running {
				delta := float64(cur - d.lastBytes)
				if delta < 0 {
					delta = 0
				}
				// Light smoothing - raw per-second samples jump around too
				// much to read, heavy smoothing lags visibly after a pause.
				if d.speed == 0 {
					d.speed = delta
				} else {
					d.speed = d.speed*0.6 + delta*0.4
				}
				m.dirty = true
			} else {
				d.speed = 0
			}
			d.lastBytes = cur
		}
		dirty := m.dirty
		m.mu.Unlock()
		if dirty && n%3 == 0 {
			m.save()
		}
	}
}

func (m *Manager) save() {
	m.mu.Lock()
	list := make([]*Download, 0, len(m.downloads))
	for _, d := range m.downloads {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	raw, err := json.Marshal(list)
	m.dirty = false
	m.mu.Unlock()
	if err != nil {
		return
	}
	var v []json.RawMessage
	_ = json.Unmarshal(raw, &v)
	if err := writeJSONAtomic(m.statePath, v); err != nil {
		log.Printf("saving download state: %v", err)
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func validateDownloadURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, errors.New("not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("only http:// and https:// links are supported")
	}
	return u, nil
}

func (m *Manager) Add(req AddRequest) (DownloadView, error) {
	u, err := validateDownloadURL(req.URL)
	if err != nil {
		return DownloadView{}, err
	}
	s := m.settings.Get()
	dir := strings.TrimSpace(req.Dir)
	if dir == "" {
		dir = s.DefaultDir
	}
	if !filepath.IsAbs(dir) {
		return DownloadView{}, errors.New("save folder must be an absolute path")
	}
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return DownloadView{}, fmt.Errorf("cannot create save folder: %w", err)
	}
	conns := req.Connections
	if conns == 0 {
		conns = s.DefaultConnections
	}
	d := &Download{
		ID:          newID(),
		URL:         u.String(),
		Dir:         dir,
		Size:        -1,
		State:       StateQueued,
		Connections: clampInt(conns, minConnections, maxConnections),
		Headers:     cleanHeaders(req.Headers),
		Source:      req.Source,
		CreatedAt:   time.Now(),
	}
	if name := sanitizeFilename(req.Filename); name != "" {
		d.Filename = name
		d.FilenameFixed = true
	} else {
		d.Filename = filenameFromURL(u)
	}
	if req.Start != nil && !*req.Start {
		d.State = StatePaused
	}
	m.mu.Lock()
	d.Filename = m.uniqueFilenameLocked(d.Dir, d.Filename, d.ID)
	m.downloads[d.ID] = d
	m.dirty = true
	view := m.viewLocked(d)
	m.mu.Unlock()
	m.save()
	m.schedule()
	return view, nil
}

// cleanHeaders keeps only the request headers that make sense to replay on
// every segment request - notably never Range, Host or Content-Length.
func cleanHeaders(in map[string]string) map[string]string {
	allowed := map[string]bool{"cookie": true, "referer": true, "user-agent": true, "authorization": true, "accept": true, "accept-language": true, "origin": true}
	out := map[string]string{}
	for k, v := range in {
		if allowed[strings.ToLower(k)] && v != "" && !strings.ContainsAny(v, "\r\n") {
			out[http.CanonicalHeaderKey(k)] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *Manager) List() []DownloadView {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]DownloadView, 0, len(m.downloads))
	for _, d := range m.downloads {
		out = append(out, m.viewLocked(d))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (m *Manager) Get(id string) (DownloadView, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.downloads[id]
	if !ok {
		return DownloadView{}, false
	}
	return m.viewLocked(d), true
}

func (m *Manager) viewLocked(d *Download) DownloadView {
	segs := make([]Segment, len(d.Segments))
	for i, s := range d.Segments {
		segs[i] = *s
	}
	v := DownloadView{
		ID: d.ID, URL: d.URL, FinalURL: d.FinalURL, Filename: d.Filename, Dir: d.Dir,
		Size: d.Size, Downloaded: d.downloaded(), Resumable: d.Resumable, ContentType: d.ContentType,
		State: d.State, Error: d.Error, Connections: d.Connections, ActiveConnections: d.activeConns,
		Speed: d.speed, ETA: -1, Segments: segs, Source: d.Source, HasCookies: d.Headers["Cookie"] != "",
		CreatedAt: d.CreatedAt, CompletedAt: d.CompletedAt,
	}
	if d.State == StateCompleted {
		v.Path = d.finalPath()
		if v.Size < 0 {
			v.Size = v.Downloaded
		}
	} else {
		v.Path = d.partPath()
	}
	if d.speed > 1 && d.Size > 0 {
		v.ETA = int64(float64(d.Size-v.Downloaded) / d.speed)
	}
	return v
}

var errNotFound = errors.New("download not found")

func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.downloads[id]
	if !ok {
		return errNotFound
	}
	switch d.State {
	case StateDownloading:
		d.stopReason = StatePaused
		d.State = StatePaused
		if d.cancel != nil {
			d.cancel()
		}
	case StateQueued:
		d.State = StatePaused
	}
	m.dirty = true
	return nil
}

func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	d, ok := m.downloads[id]
	if !ok {
		m.mu.Unlock()
		return errNotFound
	}
	if d.State == StatePaused || d.State == StateFailed {
		d.State = StateQueued
		d.Error = ""
		m.dirty = true
	}
	m.mu.Unlock()
	m.schedule()
	return nil
}

// Redownload starts a completed (or any) download over from byte zero.
func (m *Manager) Redownload(id string) error {
	m.mu.Lock()
	d, ok := m.downloads[id]
	if !ok {
		m.mu.Unlock()
		return errNotFound
	}
	if d.running {
		m.mu.Unlock()
		return errors.New("pause the download first")
	}
	d.Segments = nil
	d.Size = -1
	d.CompletedAt = nil
	d.Error = ""
	d.State = StateQueued
	m.dirty = true
	m.mu.Unlock()
	m.schedule()
	return nil
}

func (m *Manager) Delete(id string, deleteFile bool) error {
	m.mu.Lock()
	d, ok := m.downloads[id]
	if !ok {
		m.mu.Unlock()
		return errNotFound
	}
	delete(m.downloads, id)
	d.stopReason = "deleted"
	if d.cancel != nil {
		d.cancel()
	}
	part, final, completed := d.partPath(), d.finalPath(), d.State == StateCompleted
	m.dirty = true
	m.mu.Unlock()
	// Always drop the incomplete .part - it's useless without its entry.
	// The finished file is only removed when explicitly asked.
	go func() {
		// Give a cancelled run loop a moment to close its file handle.
		time.Sleep(300 * time.Millisecond)
		_ = os.Remove(part)
		if deleteFile && completed {
			_ = os.Remove(final)
		}
	}()
	m.save()
	m.schedule()
	return nil
}

// ClearCompleted removes finished entries from the list (files are kept).
func (m *Manager) ClearCompleted() int {
	m.mu.Lock()
	n := 0
	for id, d := range m.downloads {
		if d.State == StateCompleted {
			delete(m.downloads, id)
			n++
		}
	}
	m.dirty = true
	m.mu.Unlock()
	m.save()
	return n
}

type UpdateRequest struct {
	URL         *string `json:"url"`
	Connections *int    `json:"connections"`
	Filename    *string `json:"filename"`
}

// Update changes a download's properties. URL is IDM's "Refresh download
// address": point a paused/failed download at a fresh link (an expired
// signed URL, a new mirror) and keep every byte already fetched.
func (m *Manager) Update(id string, req UpdateRequest) (DownloadView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.downloads[id]
	if !ok {
		return DownloadView{}, errNotFound
	}
	if req.Connections != nil {
		d.Connections = clampInt(*req.Connections, minConnections, maxConnections)
	}
	if req.URL != nil || req.Filename != nil {
		if d.running {
			return DownloadView{}, errors.New("pause the download before changing its link or name")
		}
	}
	if req.URL != nil {
		u, err := validateDownloadURL(*req.URL)
		if err != nil {
			return DownloadView{}, err
		}
		d.URL = u.String()
		d.FinalURL = ""
		d.Error = ""
	}
	if req.Filename != nil && d.State != StateCompleted {
		name := sanitizeFilename(*req.Filename)
		if name == "" {
			return DownloadView{}, errors.New("invalid file name")
		}
		if name != d.Filename {
			name = m.uniqueFilenameLocked(d.Dir, name, d.ID)
			oldPart := d.partPath()
			d.Filename = name
			d.FilenameFixed = true
			if _, err := os.Stat(oldPart); err == nil {
				_ = os.Rename(oldPart, d.partPath())
			}
		}
	}
	m.dirty = true
	return m.viewLocked(d), nil
}

// schedule starts queued downloads (oldest first) until MaxConcurrent are
// running.
func (m *Manager) schedule() {
	maxC := m.settings.Get().MaxConcurrent
	m.mu.Lock()
	defer m.mu.Unlock()
	running := 0
	var queued []*Download
	for _, d := range m.downloads {
		if d.running {
			running++
		} else if d.State == StateQueued {
			queued = append(queued, d)
		}
	}
	sort.Slice(queued, func(i, j int) bool { return queued[i].CreatedAt.Before(queued[j].CreatedAt) })
	for _, d := range queued {
		if running >= maxC {
			break
		}
		ctx, cancel := context.WithCancel(context.Background())
		d.cancel = cancel
		d.running = true
		d.stopReason = ""
		d.State = StateDownloading
		d.Error = ""
		d.lastBytes = d.downloaded()
		d.throttled = false
		running++
		go m.run(ctx, d)
	}
}

func (m *Manager) run(ctx context.Context, d *Download) {
	err := m.transfer(ctx, d)
	m.mu.Lock()
	d.running = false
	d.activeConns = 0
	d.speed = 0
	d.cancel = nil
	var ev *Event
	switch {
	case d.stopReason == "deleted":
	case d.stopReason == StatePaused:
		d.State = StatePaused
	case err == nil:
		now := time.Now()
		d.State = StateCompleted
		d.CompletedAt = &now
		d.Error = ""
		ev = &Event{Kind: "completed", ID: d.ID, Filename: d.Filename, Path: d.finalPath()}
	default:
		d.State = StateFailed
		d.Error = err.Error()
		ev = &Event{Kind: "failed", ID: d.ID, Filename: d.Filename, Message: d.Error}
	}
	m.dirty = true
	m.mu.Unlock()
	if ev != nil {
		m.events.Push(*ev)
	}
	m.save()
	m.schedule()
}

func (m *Manager) newRequest(ctx context.Context, d *Download, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "*/*")
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}
	// Transparent gzip would make byte offsets meaningless.
	req.Header.Set("Accept-Encoding", "identity")
	return req, nil
}

// httpStatusError keeps the status around so the retry loop can tell a
// hopeless 404/403 from a transient 503.
type httpStatusError struct{ code int }

func (e httpStatusError) Error() string {
	switch e.code {
	case 401, 403:
		return fmt.Sprintf("server refused the request (HTTP %d) - the link may have expired; use Refresh link", e.code)
	case 404, 410:
		return fmt.Sprintf("file not found on server (HTTP %d) - the link may have expired; use Refresh link", e.code)
	case 416:
		return "server rejected the byte range (HTTP 416) - the file changed on the server; restart the download"
	}
	return fmt.Sprintf("server returned HTTP %d", e.code)
}

func (e httpStatusError) throttle() bool {
	return e.code == http.StatusTooManyRequests || e.code == http.StatusServiceUnavailable
}

func (e httpStatusError) Is(target error) bool { return target == errThrottled && e.throttle() }

var errThrottled = errors.New("server is limiting connections")

func (e httpStatusError) permanent() bool {
	return e.code == 401 || e.code == 403 || e.code == 404 || e.code == 410 || e.code == 416
}

// probe issues the first request of a fresh download - "bytes=0-" so a
// range-capable server answers 206 with the total size in Content-Range.
// The open response is handed back so its body can become the first
// segment's stream instead of being thrown away.
func (m *Manager) probe(ctx context.Context, d *Download) (*http.Response, error) {
	target := d.URL
	if u, err := url.Parse(target); err == nil {
		target = resolveShareURL(u).String()
	}
	// A couple of hops at most: share link -> warning page -> file.
	for hop := 0; hop < 3; hop++ {
		req, err := m.newRequest(ctx, d, target)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Range", "bytes=0-")
		resp, err := m.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return nil, httpStatusError{resp.StatusCode}
		}
		next, err := followInterstitial(resp)
		if err != nil {
			return nil, permanentError{err}
		}
		if next == nil {
			return resp, nil
		}
		target = next.String()
	}
	return nil, permanentError{errors.New("the server kept answering with a web page instead of the file")}
}

// permanentError marks a failure retrying can't fix (a private file, an
// exhausted quota) so the retry loop gives up at once.
type permanentError struct{ error }

func (e permanentError) Unwrap() error { return e.error }

// ProbeResult is what the Add Download window shows before starting.
type ProbeResult struct {
	URL         string `json:"url"`
	FinalURL    string `json:"final_url"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	Resumable   bool   `json:"resumable"`
	ContentType string `json:"content_type"`
}

func (m *Manager) Probe(ctx context.Context, rawURL string, headers map[string]string) (ProbeResult, error) {
	u, err := validateDownloadURL(rawURL)
	if err != nil {
		return ProbeResult{}, err
	}
	d := &Download{URL: u.String(), Headers: cleanHeaders(headers)}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := m.probe(ctx, d)
	if err != nil {
		return ProbeResult{}, err
	}
	resp.Body.Close()
	size, resumable := sizeFromResponse(resp)
	return ProbeResult{
		URL:         d.URL,
		FinalURL:    resp.Request.URL.String(),
		Filename:    filenameFromResponse(resp),
		Size:        size,
		Resumable:   resumable,
		ContentType: resp.Header.Get("Content-Type"),
	}, nil
}

func sizeFromResponse(resp *http.Response) (int64, bool) {
	if resp.StatusCode == http.StatusPartialContent {
		if _, _, total, ok := parseContentRange(resp.Header.Get("Content-Range")); ok && total > 0 {
			return total, true
		}
		return -1, false
	}
	return resp.ContentLength, false
}

// parseContentRange parses "bytes START-END/TOTAL" (TOTAL may be "*").
func parseContentRange(h string) (start, end, total int64, ok bool) {
	h = strings.TrimSpace(h)
	if !strings.HasPrefix(h, "bytes ") {
		return 0, 0, 0, false
	}
	h = strings.TrimPrefix(h, "bytes ")
	slash := strings.IndexByte(h, '/')
	dash := strings.IndexByte(h, '-')
	if slash < 0 || dash < 0 || dash > slash {
		return 0, 0, 0, false
	}
	var err error
	if start, err = strconv.ParseInt(h[:dash], 10, 64); err != nil {
		return 0, 0, 0, false
	}
	if end, err = strconv.ParseInt(h[dash+1:slash], 10, 64); err != nil {
		return 0, 0, 0, false
	}
	total = -1
	if t := h[slash+1:]; t != "*" {
		if total, err = strconv.ParseInt(t, 10, 64); err != nil {
			return 0, 0, 0, false
		}
	}
	return start, end, total, true
}

func (m *Manager) transfer(ctx context.Context, d *Download) error {
	var first *http.Response
	m.mu.Lock()
	fresh := len(d.Segments) == 0
	// A non-resumable download can only ever restart from zero.
	if !fresh && !d.Resumable {
		d.Segments = nil
		fresh = true
	}
	m.mu.Unlock()

	if fresh {
		resp, err := m.retryProbe(ctx, d)
		if err != nil {
			return err
		}
		first = resp
		size, resumable := sizeFromResponse(resp)
		m.mu.Lock()
		d.Size = size
		d.Resumable = resumable
		d.FinalURL = resp.Request.URL.String()
		d.ContentType = resp.Header.Get("Content-Type")
		if !d.FilenameFixed {
			if name := filenameFromResponse(resp); name != "" && name != d.Filename {
				d.Filename = m.uniqueFilenameLocked(d.Dir, name, d.ID)
			}
		}
		d.Segments = planSegments(size, resumable, d.Connections)
		m.dirty = true
		m.mu.Unlock()
		_ = os.Remove(d.partPath())
	}

	f, err := os.OpenFile(d.partPath(), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		if first != nil {
			first.Body.Close()
		}
		return fmt.Errorf("cannot write to save folder: %w", err)
	}
	if fresh && d.Size > 0 {
		// Sparse preallocation - reserves the length without writing zeros,
		// and makes an out-of-space failure show up late instead of at 99%
		// only on filesystems that support it; harmless elsewhere.
		_ = f.Truncate(d.Size)
	}

	err = m.runWorkers(ctx, d, f, first)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	m.mu.Lock()
	size := d.Size
	if size < 0 {
		size = d.downloaded()
		d.Size = size
	}
	part, final := d.partPath(), d.finalPath()
	m.mu.Unlock()
	if fi, err := os.Stat(part); err == nil && fi.Size() > size {
		_ = os.Truncate(part, size)
	}
	if _, err := os.Stat(final); err == nil {
		// Someone created a file with this name while we were downloading.
		m.mu.Lock()
		d.Filename = m.uniqueFilenameLocked(d.Dir, d.Filename, d.ID)
		final = d.finalPath()
		m.mu.Unlock()
	}
	return os.Rename(part, final)
}

func (m *Manager) retryProbe(ctx context.Context, d *Download) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		resp, err := m.probe(ctx, d)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		var se httpStatusError
		var pe permanentError
		if (errors.As(err, &se) && se.permanent()) || errors.As(err, &pe) {
			return nil, err
		}
		if !sleepCtx(ctx, backoff(attempt)) {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

// planSegments splits a fresh download into its initial ranges.
func planSegments(size int64, resumable bool, conns int) []*Segment {
	if size == 0 {
		return []*Segment{{Start: 0, End: -1}}
	}
	if !resumable || size < 0 {
		end := int64(-1)
		if size > 0 {
			end = size - 1
		}
		return []*Segment{{Start: 0, End: end}}
	}
	n := int64(conns)
	if maxN := size / minSplitSize; n > maxN {
		n = maxN
	}
	if n < 1 {
		n = 1
	}
	chunk := size / n
	segs := make([]*Segment, 0, n)
	for i := int64(0); i < n; i++ {
		start := i * chunk
		end := start + chunk - 1
		if i == n-1 {
			end = size - 1
		}
		segs = append(segs, &Segment{Start: start, End: end})
	}
	return segs
}

// claimNext picks the next piece of work for a free connection: an idle
// incomplete segment if there is one, otherwise (resumable downloads only)
// the back half of whichever active segment has the most left.
func (m *Manager) claimNextLocked(d *Download) *Segment {
	for _, s := range d.Segments {
		if s.active || s.complete() {
			continue
		}
		s.active = true
		return s
	}
	if !d.Resumable || d.throttled {
		return nil
	}
	var best *Segment
	for _, s := range d.Segments {
		if s.active && s.remaining() >= 2*minSplitSize && (best == nil || s.remaining() > best.remaining()) {
			best = s
		}
	}
	if best == nil {
		return nil
	}
	mid := best.pos() + best.remaining()/2
	ns := &Segment{Start: mid, End: best.End, active: true}
	best.End = mid - 1
	// Keep Segments ordered by offset for the UI's progress bar.
	idx := len(d.Segments)
	for i, s := range d.Segments {
		if s == best {
			idx = i + 1
			break
		}
	}
	d.Segments = append(d.Segments, nil)
	copy(d.Segments[idx+1:], d.Segments[idx:])
	d.Segments[idx] = ns
	return ns
}

func (m *Manager) runWorkers(ctx context.Context, d *Download, f *os.File, first *http.Response) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)
	fail := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
		cancel()
	}

	worker := func(seg *Segment, resp *http.Response) {
		defer wg.Done()
		for seg != nil {
			err := m.fetchSegment(ctx, d, f, seg, resp)
			resp = nil
			m.mu.Lock()
			seg.active = false
			// The server is limiting connections (429/503): like IDM, drop
			// this one and let the others carry on - its segment goes back
			// to the pool for whichever connection frees up next.
			if errors.Is(err, errThrottled) && d.activeConns > 1 {
				d.throttled = true
				m.mu.Unlock()
				break
			}
			if err != nil {
				m.mu.Unlock()
				fail(err)
				break
			}
			seg = m.claimNextLocked(d)
			m.mu.Unlock()
		}
		m.mu.Lock()
		d.activeConns--
		m.mu.Unlock()
	}

	m.mu.Lock()
	limit := d.Connections
	if !d.Resumable {
		limit = 1
	}
	var starts []*Segment
	for len(starts) < limit {
		s := m.claimNextLocked(d)
		if s == nil {
			break
		}
		starts = append(starts, s)
	}
	d.activeConns = len(starts)
	m.mu.Unlock()

	for _, s := range starts {
		wg.Add(1)
		var resp *http.Response
		if first != nil && s.Start == 0 && s.Done == 0 {
			resp, first = first, nil
		}
		go worker(s, resp)
	}
	if first != nil {
		first.Body.Close()
	}
	wg.Wait()

	if err := ctx.Err(); err != nil && firstErr == nil {
		return err
	}
	if firstErr != nil {
		return firstErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range d.Segments {
		if !s.complete() && s.End >= 0 {
			return errors.New("download ended with missing data")
		}
	}
	return nil
}

// fetchSegment downloads one segment to completion, retrying transient
// failures from wherever it got to. resp, when non-nil, is an already-open
// response positioned at the segment's current offset (the probe).
func (m *Manager) fetchSegment(ctx context.Context, d *Download, f *os.File, seg *Segment, resp *http.Response) error {
	attempt := 0
	for {
		if ctx.Err() != nil {
			if resp != nil {
				resp.Body.Close()
			}
			return ctx.Err()
		}
		progressed, err := m.streamSegment(ctx, d, f, seg, resp)
		resp = nil
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var se httpStatusError
		if errors.As(err, &se) && se.permanent() {
			return err
		}
		if errors.As(err, &se) && se.throttle() {
			m.mu.Lock()
			others := d.activeConns > 1
			m.mu.Unlock()
			if others {
				return errThrottled
			}
		}
		if progressed {
			attempt = 0
		}
		attempt++
		if attempt > maxRetries {
			return fmt.Errorf("connection kept failing: %w", err)
		}
		m.mu.Lock()
		// A non-resumable stream can't pick up mid-file - start over.
		if !d.Resumable {
			seg.Done = 0
		}
		m.mu.Unlock()
		if !sleepCtx(ctx, backoff(attempt)) {
			return ctx.Err()
		}
	}
}

func (m *Manager) streamSegment(ctx context.Context, d *Download, f *os.File, seg *Segment, resp *http.Response) (bool, error) {
	m.mu.Lock()
	pos, end := seg.pos(), seg.End
	target := d.FinalURL
	if target == "" {
		target = d.URL
	}
	resumable := d.Resumable
	m.mu.Unlock()
	if end >= 0 && pos > end {
		if resp != nil {
			resp.Body.Close()
		}
		return false, nil
	}

	// Stall watchdog: a connection that delivers nothing for stallTimeout is
	// torn down and retried from where it stopped. Without this, a server
	// that silently stops sending (common with throttling mirrors) would
	// hang that segment forever - ResponseHeaderTimeout only covers the
	// headers.
	reqCtx, cancelReq := context.WithCancel(ctx)
	defer cancelReq()
	watchdog := time.AfterFunc(stallTimeout, cancelReq)
	defer watchdog.Stop()
	if resp != nil {
		// The probe's response was opened under the run context; tie it to
		// this watchdog too by closing it when the watchdog fires.
		body := resp.Body
		stop := context.AfterFunc(reqCtx, func() { body.Close() })
		defer stop()
	}

	if resp == nil {
		req, err := m.newRequest(reqCtx, d, target)
		if err != nil {
			return false, err
		}
		if resumable {
			if end >= 0 {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", pos, end))
			} else {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", pos))
			}
		}
		resp, err = m.client.Do(req)
		if err != nil {
			return false, err
		}
		if resumable {
			if resp.StatusCode != http.StatusPartialContent {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return false, errors.New("server stopped honouring byte ranges")
				}
				return false, httpStatusError{resp.StatusCode}
			}
			if s, _, _, ok := parseContentRange(resp.Header.Get("Content-Range")); !ok || s != pos {
				resp.Body.Close()
				return false, errors.New("server returned the wrong byte range")
			}
		} else if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return false, httpStatusError{resp.StatusCode}
		}
	}
	defer resp.Body.Close()

	buf := make([]byte, readBufSize)
	progressed := false
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(stallTimeout)
			m.mu.Lock()
			cur, end := seg.pos(), seg.End
			m.mu.Unlock()
			chunk := int64(n)
			if end >= 0 && cur+chunk-1 > end {
				chunk = end - cur + 1
			}
			if chunk > 0 {
				if _, err := f.WriteAt(buf[:chunk], cur); err != nil {
					return progressed, fmt.Errorf("writing file: %w", err)
				}
				progressed = true
				if err := m.limiter.Wait(ctx, int(chunk)); err != nil {
					return progressed, err
				}
				m.mu.Lock()
				// End may have been pulled in by a split while this chunk
				// was in flight - never count past it.
				seg.Done += chunk
				if seg.End >= 0 && seg.pos() > seg.End+1 {
					seg.Done = seg.End - seg.Start + 1
				}
				done := seg.complete()
				m.mu.Unlock()
				if done {
					return true, nil
				}
			} else {
				return progressed, nil
			}
		}
		if rerr == io.EOF {
			m.mu.Lock()
			defer m.mu.Unlock()
			if seg.End < 0 {
				// Unknown-length stream: EOF is the end.
				if seg.Done > 0 {
					seg.End = seg.pos() - 1
				}
				seg.Finished = true
				return progressed, nil
			}
			if seg.complete() {
				return progressed, nil
			}
			return progressed, io.ErrUnexpectedEOF
		}
		if rerr != nil {
			if ctx.Err() == nil && reqCtx.Err() != nil {
				return progressed, errStalled
			}
			return progressed, rerr
		}
	}
}

var errStalled = errors.New("connection stalled")

func backoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * 500 * time.Millisecond
	if d > 15*time.Second {
		d = 15 * time.Second
	}
	return d
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ---- filenames ----

func filenameFromResponse(resp *http.Response) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			// mime.ParseMediaType already decodes RFC 5987 filename*=.
			if name := sanitizeFilename(params["filename"]); name != "" {
				return name
			}
		}
	}
	name := filenameFromURL(resp.Request.URL)
	if filepath.Ext(name) == "" {
		name += extensionForType(resp.Header.Get("Content-Type"))
	}
	return name
}

// mime.ExtensionsByType returns every known extension in alphabetical
// order (".zip" and ".zipx", ".jfif" before ".jpg"), so the usual one for
// common types is spelled out here.
var preferredExt = map[string]string{
	"application/zip": ".zip", "image/jpeg": ".jpg", "text/plain": ".txt", "video/mp4": ".mp4",
	"audio/mpeg": ".mp3", "application/x-7z-compressed": ".7z", "application/vnd.rar": ".rar",
	"application/x-rar-compressed": ".rar", "application/gzip": ".gz", "application/x-tar": ".tar",
	"application/pdf": ".pdf", "application/x-iso9660-image": ".iso", "application/vnd.android.package-archive": ".apk",
	"application/x-msdownload": ".exe", "application/vnd.debian.binary-package": ".deb", "text/html": ".html",
}

func extensionForType(ct string) string {
	mt := strings.TrimSpace(strings.ToLower(strings.Split(ct, ";")[0]))
	if mt == "" || mt == "application/octet-stream" {
		return ""
	}
	if e, ok := preferredExt[mt]; ok {
		return e
	}
	if exts, _ := mime.ExtensionsByType(mt); len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func filenameFromURL(u *url.URL) string {
	base := path.Base(u.Path)
	if unescaped, err := url.PathUnescape(base); err == nil {
		base = unescaped
	}
	if name := sanitizeFilename(base); name != "" && name != "/" {
		return name
	}
	return "download"
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return -1
		case r == '/' || r == '\\':
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" {
		return ""
	}
	// Keep well under the usual 255-byte limit, leaving room for " (12)"
	// and the .part suffix.
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		cut := 200 - len(ext)
		for cut > 0 && !utf8Start(name[cut]) {
			cut--
		}
		name = name[:cut] + ext
	}
	return name
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// uniqueFilenameLocked appends " (n)" until the name clashes with neither a
// file on disk nor another download's target in the same folder.
func (m *Manager) uniqueFilenameLocked(dir, name, selfID string) string {
	taken := func(candidate string) bool {
		for id, other := range m.downloads {
			if id != selfID && other.Dir == dir && other.Filename == candidate && other.State != StateCompleted {
				return true
			}
		}
		if _, err := os.Stat(filepath.Join(dir, candidate)); err == nil {
			return true
		}
		if self, ok := m.downloads[selfID]; !ok || self.Filename != candidate {
			if _, err := os.Stat(filepath.Join(dir, candidate+partSuffix)); err == nil {
				return true
			}
		}
		return false
	}
	if !taken(name) {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if strings.HasSuffix(strings.ToLower(stem), ".tar") {
		ext = stem[len(stem)-4:] + ext
		stem = stem[:len(stem)-4]
	}
	for i := 1; i < 10000; i++ {
		c := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !taken(c) {
			return c
		}
	}
	return newID() + ext
}
