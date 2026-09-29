package main

// `nivaroos-download-sidecar browser-host`: the Download Station browser.
//
// It runs in its own unit, nivaroos-ds-browser.service, as the unprivileged
// user nivaroos-browser, because Chromium cannot run under the sidecar's
// hardening (MemoryDenyWriteExecute breaks V8's JIT, RestrictNamespaces
// breaks Chrome's sandbox) and refuses to sandbox itself as root. The unit
// is socket-activated: the sidecar connects to /run/nivaroos/ds-browser.sock
// (root-only, mode 0600), systemd starts this process on the first
// connection, and it exits after a quiet period - so an unused browser costs
// no memory at all.
//
// The socket speaks HTTP. Only the sidecar can connect, and it has already
// checked the user's NivaroOS token; it passes the user id in X-Nv-User. Each
// NivaroOS user gets their own Chromium (own profile, so their sign-ins
// persist and nobody else's cookies are visible).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	rbDefaultSocket  = "/run/nivaroos/ds-browser.sock"
	rbDefaultState   = "/var/lib/nivaroos/ds-browser"
	rbDefaultUBOL    = "/usr/share/nivaroos/ds-browser/ubol"
	rbMaxInstances   = 2
	rbUserHeader     = "X-Nv-User"
	rbHostIdle       = 10 * time.Minute
	rbChromeMinMajor = 120
)

// rbChromeCandidates, in order of preference. The installer's pinned
// Chrome for Testing comes first: it is the one NivaroOS put there itself.
var rbChromeCandidates = []string{
	"/opt/nivaroos/chromium/chrome",
	"/usr/bin/google-chrome-stable",
	"/usr/bin/google-chrome",
	"/opt/google/chrome/chrome",
	"/usr/bin/chromium",
	"/usr/lib/chromium/chromium",
	"/usr/bin/chromium-browser",
}

var errRBBusy = errors.New("2 people are using the browser right now - try again soon")

type rbAdblockConfig struct {
	Version      string   `json:"version"`
	Enabled      bool     `json:"enabled"`
	AllowedSites []string `json:"allowed_sites"`
	Texts        []string `json:"texts,omitempty"`
}

type rbAdblockState struct {
	version string
	enabled bool
	allowed []string
	engine  *FilterEngine
}

func (s *rbAdblockState) siteTrusted(host string) bool {
	host = strings.ToLower(host)
	for _, a := range s.allowed {
		if hostMatchesDomain(host, strings.ToLower(a)) {
			return true
		}
	}
	return false
}

type rbHost struct {
	stateDir string
	ubolDir  string
	chrome   string
	chromeV  string // "154.0.8037.57"
	branded  bool   // Google Chrome (vs Chromium)
	idle     time.Duration

	adblock atomic.Pointer[rbAdblockState]
	ubolMu  sync.Mutex

	mu        sync.Mutex
	instances map[string]*rbInstance
	lastUse   time.Time
	// launchErr is the last reason Chromium could not start ("sandbox",
	// "no_chrome", ...), shown by /status so the UI can fall back to Lite
	// mode with an explanation.
	launchErr string
	stopping  bool
}

// runBrowserHost is the browser-host subcommand's main.
func runBrowserHost(args []string) {
	fs := flag.NewFlagSet("browser-host", flag.ExitOnError)
	listen := fs.String("listen", "", "unix socket to listen on when not socket-activated (default "+rbDefaultSocket+")")
	stateDir := fs.String("state-dir", rbDefaultState, "profiles and staging live here")
	ubolDir := fs.String("ubol", rbDefaultUBOL, "unpacked uBlock Origin Lite to load (skipped if missing)")
	chrome := fs.String("chrome", "", "Chromium/Chrome binary (default: first one found)")
	idle := fs.Duration("idle", rbHostIdle, "exit after this long with nobody using the browser")
	_ = fs.Parse(args)

	h := &rbHost{stateDir: *stateDir, ubolDir: *ubolDir, idle: *idle, instances: map[string]*rbInstance{}, lastUse: time.Now()}
	h.adblock.Store(&rbAdblockState{enabled: true})
	h.chrome = *chrome
	if h.chrome == "" {
		h.chrome = findChrome()
	}
	if h.chrome != "" {
		h.chromeV, h.branded = chromeVersion(h.chrome)
		if major(h.chromeV) < rbChromeMinMajor {
			log.Printf("ds-browser: %s is version %q - at least %d is needed", h.chrome, h.chromeV, rbChromeMinMajor)
			h.launchErr = "no_chrome"
		}
	} else {
		h.launchErr = "no_chrome"
	}
	for _, d := range []string{"profiles", "staging"} {
		_ = os.MkdirAll(filepath.Join(h.stateDir, d), 0o700)
	}

	ln, err := rbListener(*listen)
	if err != nil {
		log.Fatalf("ds-browser: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{Handler: h.routes(), ReadHeaderTimeout: 20 * time.Second}
	go h.reaper(ctx, stop)
	go func() {
		<-ctx.Done()
		h.shutdown()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Printf("ds-browser host listening (chrome %s %s)", h.chrome, h.chromeV)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// rbListener takes the socket systemd passed in (LISTEN_FDS), or listens on
// path itself (development, tests).
func rbListener(path string) (net.Listener, error) {
	if path == "" {
		if n, _ := strconv.Atoi(os.Getenv("LISTEN_FDS")); n >= 1 && os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()) {
			f := os.NewFile(3, "listen-fd")
			ln, err := net.FileListener(f)
			f.Close()
			return ln, err
		}
		path = rbDefaultSocket
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return ln, nil
}

func findChrome() string {
	for _, c := range rbChromeCandidates {
		fi, err := os.Stat(c)
		if err != nil || fi.IsDir() || fi.Mode()&0o111 == 0 {
			continue
		}
		// Ubuntu's chromium-browser is a shim that installs the snap; a
		// snap cannot run under this unit's user.
		if raw, err := os.ReadFile(c); err == nil && len(raw) < 64<<10 && strings.Contains(string(raw), "snap") {
			continue
		}
		return c
	}
	return ""
}

var chromeVersionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)\.(\d+)`)

func chromeVersion(bin string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", false
	}
	s := string(out)
	return chromeVersionRe.FindString(s), strings.Contains(s, "Google Chrome")
}

func major(v string) int {
	n, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	return n
}

// userAgent is a normal desktop Chrome UA for this version: no
// "HeadlessChrome" (Google answers that with its bot check), and the
// reduced form every Chrome sends today.
func (h *rbHost) userAgent() string {
	m := major(h.chromeV)
	if m == 0 {
		m = 140
	}
	return fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36", m)
}

// uaMetadata is the matching client-hints data (Sec-CH-UA and
// navigator.userAgentData), with normal brands.
func (h *rbHost) uaMetadata() map[string]interface{} {
	m := strconv.Itoa(major(h.chromeV))
	brands := []map[string]string{{"brand": "Chromium", "version": m}, {"brand": "Not.A/Brand", "version": "99"}}
	full := []map[string]string{{"brand": "Chromium", "version": h.chromeV}, {"brand": "Not.A/Brand", "version": "99.0.0.0"}}
	if h.branded {
		brands = append([]map[string]string{{"brand": "Google Chrome", "version": m}}, brands...)
		full = append([]map[string]string{{"brand": "Google Chrome", "version": h.chromeV}}, full...)
	}
	return map[string]interface{}{
		"brands": brands, "fullVersionList": full, "fullVersion": h.chromeV,
		"platform": "Linux", "platformVersion": "", "architecture": "x86", "model": "", "mobile": false, "bitness": "64",
	}
}

func (h *rbHost) touch() {
	h.mu.Lock()
	h.lastUse = time.Now()
	h.mu.Unlock()
}

// instance returns uid's running browser, starting it if needed.
func (h *rbHost) instance(uid string, start bool) (*rbInstance, error) {
	h.mu.Lock()
	if h.stopping {
		h.mu.Unlock()
		return nil, errors.New("the browser is shutting down")
	}
	if in := h.instances[uid]; in != nil {
		h.mu.Unlock()
		return in, in.waitReady()
	}
	if !start {
		h.mu.Unlock()
		return nil, nil
	}
	if h.chrome == "" || h.launchErr == "no_chrome" {
		h.mu.Unlock()
		return nil, errors.New("no_chrome")
	}
	if len(h.instances) >= rbMaxInstances {
		h.mu.Unlock()
		return nil, errRBBusy
	}
	in := newRBInstance(h, uid)
	h.instances[uid] = in
	h.mu.Unlock()
	go in.run()
	if err := in.waitReady(); err != nil {
		h.mu.Lock()
		if h.instances[uid] == in {
			delete(h.instances, uid)
		}
		h.mu.Unlock()
		return nil, err
	}
	return in, nil
}

func (h *rbHost) dropInstance(in *rbInstance) {
	in.mu.Lock()
	lastView := in.lastView
	in.mu.Unlock()
	h.mu.Lock()
	if h.instances[in.uid] == in {
		delete(h.instances, in.uid)
	}
	// The host's own quiet period counts from when the last viewer left,
	// not from when its browser was closed for being idle (that would
	// double the time an unused browser service stays up).
	if lastView.After(h.lastUse) {
		h.lastUse = lastView
	}
	h.mu.Unlock()
}

func (h *rbHost) setLaunchErr(reason string) {
	h.mu.Lock()
	h.launchErr = reason
	h.mu.Unlock()
}

// reaper stops a user's browser after `idle` with no viewer, and the whole
// host after `idle` with no browser running.
func (h *rbHost) reaper(ctx context.Context, stop func()) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			h.mu.Lock()
			list := make([]*rbInstance, 0, len(h.instances))
			for _, in := range h.instances {
				list = append(list, in)
			}
			idleSince := h.lastUse
			h.mu.Unlock()
			for _, in := range list {
				in.housekeep(now)
				if in.idleFor(now) >= h.idle {
					log.Printf("ds-browser: closing %s's browser after %s unused", in.uid, h.idle)
					in.close()
				}
			}
			h.mu.Lock()
			empty := len(h.instances) == 0
			h.mu.Unlock()
			if empty && now.Sub(idleSince) >= h.idle {
				log.Printf("ds-browser: nobody is using the browser - exiting")
				stop()
				return
			}
		}
	}
}

func (h *rbHost) shutdown() {
	h.mu.Lock()
	h.stopping = true
	list := make([]*rbInstance, 0, len(h.instances))
	for _, in := range h.instances {
		list = append(list, in)
	}
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, in := range list {
		wg.Add(1)
		go func(in *rbInstance) { defer wg.Done(); in.close() }(in)
	}
	wg.Wait()
}

// memoryMB is this unit's whole memory use (the cgroup's memory.current:
// the host plus every Chromium process).
func memoryMB() int64 {
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "0::") {
			p := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(line, "0::"), "memory.current")
			if b, err := os.ReadFile(p); err == nil {
				n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
				return n >> 20
			}
		}
	}
	return 0
}

func rbUser(r *http.Request) (string, bool) {
	return rbSafeUID(r.Header.Get(rbUserHeader))
}

func (h *rbHost) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		h.touch()
		uid, _ := rbUser(r)
		h.mu.Lock()
		out := map[string]interface{}{
			"available": h.launchErr == "",
			"reason":    h.launchErr,
			"chrome":    h.chromeV,
			"branded":   h.branded,
			"instances": len(h.instances),
			"max":       rbMaxInstances,
		}
		in := h.instances[uid]
		h.mu.Unlock()
		out["mem_mb"] = memoryMB()
		if in != nil {
			out["running"] = true
			out["tabs"] = in.tabCount()
			out["adblock_engine"] = in.blockerName()
		} else {
			out["running"] = false
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("PUT /adblock", func(w http.ResponseWriter, r *http.Request) {
		var cfg rbAdblockConfig
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&cfg); err != nil {
			writeErr(w, 400, err)
			return
		}
		h.applyAdblock(cfg)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /adblock/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"version": h.adblock.Load().version})
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		uid, ok := rbUser(r)
		if !ok {
			writeErr(w, 400, errors.New("no user"))
			return
		}
		h.touch()
		h.serveViewer(w, r, uid)
	})
	withInstance := func(fn func(w http.ResponseWriter, r *http.Request, in *rbInstance)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			uid, ok := rbUser(r)
			if !ok {
				writeErr(w, 400, errors.New("no user"))
				return
			}
			h.touch()
			in, err := h.instance(uid, false)
			if err != nil || in == nil {
				writeErr(w, 409, errors.New("the browser is not running"))
				return
			}
			fn(w, r, in)
		}
	}
	mux.HandleFunc("POST /typed", withInstance(func(w http.ResponseWriter, r *http.Request, in *rbInstance) {
		var body struct {
			Host string `json:"host"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || body.Host == "" {
			writeErr(w, 400, errors.New("host required"))
			return
		}
		in.allowTyped(body.Host)
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /captures/{id}", withInstance(func(w http.ResponseWriter, r *http.Request, in *rbInstance) {
		c, ok := in.capture(r.PathValue("id"))
		if !ok {
			writeErr(w, 404, errors.New("capture not found"))
			return
		}
		writeJSON(w, 200, c)
	}))
	mux.HandleFunc("GET /staged/{guid}", withInstance(func(w http.ResponseWriter, r *http.Request, in *rbInstance) {
		d, ok := in.staged(r.PathValue("guid"))
		if !ok {
			writeErr(w, 404, errors.New("download not found"))
			return
		}
		writeJSON(w, 200, d)
	}))
	mux.HandleFunc("DELETE /staged/{guid}", withInstance(func(w http.ResponseWriter, r *http.Request, in *rbInstance) {
		in.forgetStaged(r.PathValue("guid"))
		w.WriteHeader(204)
	}))
	mux.HandleFunc("POST /uploads/{id}", withInstance(func(w http.ResponseWriter, r *http.Request, in *rbInstance) {
		n, err := in.acceptUpload(r.PathValue("id"), r)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]int{"files": n})
	}))
	mux.HandleFunc("POST /cookies/export", withInstance(func(w http.ResponseWriter, r *http.Request, in *rbInstance) {
		var body struct {
			Provider string `json:"provider"`
			Context  string `json:"context"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			writeErr(w, 400, err)
			return
		}
		res, err := in.exportCookies(r.Context(), body.Provider, body.Context)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, 200, res)
	}))
	mux.HandleFunc("DELETE /profile", func(w http.ResponseWriter, r *http.Request) {
		uid, ok := rbUser(r)
		if !ok {
			writeErr(w, 400, errors.New("no user"))
			return
		}
		if in, _ := h.instance(uid, false); in != nil {
			in.close()
		}
		if err := os.RemoveAll(filepath.Join(h.stateDir, "profiles", uid)); err != nil {
			writeErr(w, 500, err)
			return
		}
		_ = os.RemoveAll(filepath.Join(h.stateDir, "staging", uid))
		w.WriteHeader(204)
	})
	return mux
}

// ubolCopy returns a writable copy of the vendored uBO Lite: Chromium
// writes an unpacked extension's indexed filter rules into the extension's
// own folder, which /usr/share is not (for this user). The copy is redone
// whenever the vendored version changes.
func (h *rbHost) ubolCopy() (string, error) {
	h.ubolMu.Lock()
	defer h.ubolMu.Unlock()
	src := h.ubolDir
	if src == "" {
		return "", os.ErrNotExist
	}
	want, err := os.ReadFile(filepath.Join(src, "manifest.json"))
	if err != nil {
		return "", err
	}
	dst := filepath.Join(h.stateDir, "ubol")
	if have, err := os.ReadFile(filepath.Join(dst, "manifest.json")); err == nil && bytes.Equal(have, want) {
		return dst, nil
	}
	tmp := dst + ".new"
	_ = os.RemoveAll(tmp)
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// copyTree copies regular files and directories (symlinks are skipped).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "_metadata" || strings.HasPrefix(rel, "_metadata/") {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		switch {
		case fi.IsDir():
			return os.MkdirAll(target, 0o700)
		case fi.Mode().IsRegular():
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
		return nil
	})
}

func (h *rbHost) applyAdblock(cfg rbAdblockConfig) {
	cur := h.adblock.Load()
	st := &rbAdblockState{version: cfg.Version, enabled: cfg.Enabled, allowed: cfg.AllowedSites, engine: cur.engine}
	if cfg.Texts != nil {
		started := time.Now()
		st.engine = BuildFilterEngine(cfg.Texts...)
		log.Printf("ds-browser: filter engine built: %d network, %d cosmetic in %s", st.engine.NetworkCount, st.engine.CosmeticCount, time.Since(started).Round(time.Millisecond))
	}
	h.adblock.Store(st)
	h.mu.Lock()
	list := make([]*rbInstance, 0, len(h.instances))
	for _, in := range h.instances {
		list = append(list, in)
	}
	h.mu.Unlock()
	for _, in := range list {
		go func(in *rbInstance) {
			in.applyUBOLModes()
			in.reloadForAdblock(cur, st)
		}(in)
	}
}

// blocksOn reports whether the blocker applies to pages on host.
func (s *rbAdblockState) blocksOn(host string) bool {
	return s != nil && s.enabled && !s.siteTrusted(host)
}
