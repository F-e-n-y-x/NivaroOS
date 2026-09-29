package main

// The sidecar's side of the Download Station browser: the /rb/* API the UI
// talks to (through the gateway, so it works over HTTPS and tunnels). The
// browser itself runs in nivaroos-ds-browser.service (rb_host.go); this
// file authenticates the user, relays to that service's root-only unix
// socket, keeps its ad-block settings in step with Download Station's, and
// does the root-side work the browser user may not do (moving a finished
// download into the user's folders).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type rbClient struct {
	socket     string
	stagingDir string
	http       *http.Client
	proxy      *httputil.ReverseProxy
	ab         *Adblocker
	st         *SettingsStore

	mu       sync.Mutex
	lastUsed time.Time
}

func newRBClient(socket, stagingDir string, ab *Adblocker, st *SettingsStore) *rbClient {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}
	tr := &http.Transport{DialContext: dial, MaxIdleConns: 8, IdleConnTimeout: 30 * time.Second}
	c := &rbClient{socket: socket, stagingDir: stagingDir, ab: ab, st: st,
		http: &http.Client{Transport: tr, Timeout: 60 * time.Second}}
	c.proxy = &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = "http"
			r.URL.Host = "ds-browser"
			r.Host = "ds-browser"
		},
		Transport:     tr,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the browser service is not available: " + err.Error()})
		},
	}
	if st != nil {
		st.OnChange(func(Settings) {
			if c.recentlyUsed() {
				go c.pushAdblock(context.Background())
			}
		})
	}
	return c
}

func (c *rbClient) installed() bool {
	fi, err := os.Stat(c.socket)
	return err == nil && fi.Mode()&os.ModeSocket != 0
}

func (c *rbClient) markUsed() {
	c.mu.Lock()
	c.lastUsed = time.Now()
	c.mu.Unlock()
}

func (c *rbClient) recentlyUsed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Since(c.lastUsed) < rbHostIdle+time.Minute
}

// do makes one request to the browser service as user uid.
func (c *rbClient) do(ctx context.Context, method, path, uid string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://ds-browser"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set(rbUserHeader, uid)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.http.Do(req)
}

func (c *rbClient) getJSON(ctx context.Context, path, uid string, out interface{}) error {
	resp, err := c.do(ctx, http.MethodGet, path, uid, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return hostError(resp)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

func hostError(resp *http.Response) error {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
	if e.Error == "" {
		e.Error = resp.Status
	}
	return errors.New(e.Error)
}

// pushAdblock sends Download Station's blocker settings to the browser
// service, and the filter lists themselves only when they changed.
func (c *rbClient) pushAdblock(ctx context.Context) {
	if c.ab == nil || !c.installed() {
		return
	}
	s := c.st.Get()
	texts, version := c.ab.SourceTexts()
	cfg := rbAdblockConfig{Version: version, Enabled: s.AdblockEnabled, AllowedSites: s.AllowedSites}
	var cur struct {
		Version string `json:"version"`
	}
	if err := c.getJSON(ctx, "/adblock/version", "0", &cur); err != nil {
		return
	}
	if cur.Version != version {
		cfg.Texts = texts
	}
	raw, _ := json.Marshal(cfg)
	resp, err := c.do(ctx, http.MethodPut, "/adblock", "0", bytes.NewReader(raw), "application/json")
	if err != nil {
		log.Printf("ds-browser: pushing ad-block settings: %v", err)
		return
	}
	resp.Body.Close()
}

// rbUserID is the NivaroOS user a request is for: the (already validated)
// token's id claim. Same-host automation without a token gets its own
// "local" profile.
func rbUserID(r *http.Request) string {
	token := r.Header.Get("Authorization")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		if raw, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			var claims map[string]interface{}
			if json.Unmarshal(raw, &claims) == nil {
				switch id := claims["id"].(type) {
				case float64:
					return strconv.FormatInt(int64(id), 10)
				case string:
					if s, ok := rbSafeUID(id); ok {
						return s
					}
				}
				if u, ok := claims["username"].(string); ok {
					if s, ok := rbSafeUID(u); ok {
						return "u-" + s
					}
				}
			}
		}
	}
	return "local"
}

func lowMemory() bool {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				kb, _ := strconv.ParseInt(f[1], 10, 64)
				return kb > 0 && kb < 1900*1024
			}
		}
	}
	return false
}

const rbInstallUnit = "nivaroos-ds-browser-install.service"

// installState is "installing" while the install unit runs, "failed" if
// its last run failed, "" otherwise ("absent" when the unit is missing).
func installState(ctx context.Context) string {
	out, _ := exec.CommandContext(ctx, "systemctl", "show", "-p", "ActiveState", "-p", "Result", "-p", "LoadState", rbInstallUnit).Output()
	props := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[k] = v
		}
	}
	switch {
	case props["LoadState"] != "loaded":
		return "absent"
	case props["ActiveState"] == "activating" || props["ActiveState"] == "active":
		return "installing"
	case props["Result"] != "" && props["Result"] != "success":
		return "failed"
	}
	return ""
}

func (c *rbClient) status(ctx context.Context, uid string) map[string]interface{} {
	out := map[string]interface{}{"available": false, "engine": "lite", "running": false}
	if !c.installed() {
		out["reason"] = "not_installed"
		st := installState(ctx)
		out["install"] = st
		out["can_install"] = st != "absent"
		return out
	}
	if lowMemory() {
		out["reason"] = "low_memory"
		return out
	}
	var st map[string]interface{}
	if err := c.getJSON(ctx, "/status", uid, &st); err != nil {
		out["reason"] = "unreachable"
		out["detail"] = err.Error()
		return out
	}
	for k, v := range st {
		out[k] = v
	}
	if a, _ := st["available"].(bool); a {
		out["engine"] = "chromium"
	} else {
		inst := installState(ctx)
		out["install"] = inst
		out["can_install"] = inst != "absent"
	}
	return out
}

// rbCaptureLookup is set when the browser service is wired up; POST
// /downloads and /probe use it for rb_capture.
var rbCaptureLookup func(ctx context.Context, uid, id string) (*rbCapture, error)

// applyRBCapture fills in the headers the browser recorded for a download
// it handed over. The URL must be the one it captured (the dialog may
// only change the name and folder).
func applyRBCapture(r *http.Request, id string, req *AddRequest) error {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cp, err := rbCaptureLookup(ctx, rbUserID(r), id)
	if err != nil {
		return fmt.Errorf("the browser's download details are gone: %w", err)
	}
	if req.URL == "" {
		req.URL = cp.URL
	}
	if req.URL != cp.URL {
		return nil
	}
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	for k, v := range cp.Headers {
		req.Headers[k] = v
	}
	if req.Source == "" {
		req.Source = "browser"
	}
	if req.Filename == "" {
		req.Filename = cp.Filename
	}
	return nil
}

func registerRBRoutes(mux *http.ServeMux, c *rbClient, m *Manager) {
	rbCaptureLookup = c.captureHeaders
	c.register(mux, m)
}

func (c *rbClient) register(mux *http.ServeMux, m *Manager) {
	mux.HandleFunc("GET /rb/status", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		writeJSON(w, 200, c.status(ctx, rbUserID(r)))
	})
	mux.HandleFunc("GET /rb/ws", func(w http.ResponseWriter, r *http.Request) {
		// A browser always sends Origin on a WebSocket; it must be the page
		// this host served (the NivaroOS UI), so no other site the user has
		// open can drive their browser with a stolen token.
		if r.Header.Get("Origin") != "" && !sameHostOrigin(r) {
			writeErr(w, 403, errors.New("origin not allowed"))
			return
		}
		if !c.installed() {
			writeErr(w, 503, errors.New("the full browser is not installed"))
			return
		}
		c.markUsed()
		uid := rbUserID(r)
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		c.pushAdblock(ctx)
		cancel()
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/ws"
		r2.URL.RawQuery = ""
		r2.Header.Del("Authorization")
		r2.Header.Del("Cookie")
		r2.Header.Set(rbUserHeader, uid)
		c.proxy.ServeHTTP(w, r2)
	})
	relay := func(method, hostPath string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var body io.Reader
			if r.Body != nil {
				body = http.MaxBytesReader(w, r.Body, 1<<20)
			}
			resp, err := c.do(r.Context(), method, hostPath, rbUserID(r), body, "application/json")
			if err != nil {
				writeErr(w, 502, err)
				return
			}
			defer resp.Body.Close()
			if ct := resp.Header.Get("Content-Type"); ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, io.LimitReader(resp.Body, 8<<20))
		}
	}
	mux.HandleFunc("POST /rb/typed", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL string `json:"url"`
		}
		if err := readJSON(r, &body); err != nil {
			writeErr(w, 400, err)
			return
		}
		u, err := validateDownloadURL(body.URL)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		raw, _ := json.Marshal(map[string]string{"host": u.Hostname()})
		resp, err := c.do(r.Context(), http.MethodPost, "/typed", rbUserID(r), bytes.NewReader(raw), "application/json")
		if err != nil {
			writeErr(w, 502, err)
			return
		}
		resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
	})
	mux.HandleFunc("DELETE /rb/profile", relay(http.MethodDelete, "/profile"))
	// "Install full browser": runs the installer's own step
	// (install-ds-browser.sh) in its unit; the UI polls /rb/status.
	mux.HandleFunc("POST /rb/install", func(w http.ResponseWriter, r *http.Request) {
		if lowMemory() {
			writeErr(w, 409, errors.New("this box has less than 2 GB of RAM - the full browser needs more"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "systemctl", "start", "--no-block", rbInstallUnit).CombinedOutput(); err != nil {
			writeErr(w, 500, fmt.Errorf("could not start the install: %s", strings.TrimSpace(string(out))))
			return
		}
		writeJSON(w, 202, map[string]string{"install": "installing"})
	})
	mux.HandleFunc("POST /rb/cookies/export", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Provider string `json:"provider"`
			Context  string `json:"context"`
		}
		if err := readJSON(r, &body); err != nil {
			writeErr(w, 400, err)
			return
		}
		if _, ok := rbProviders[body.Provider]; !ok {
			writeErr(w, 400, errors.New("unknown provider"))
			return
		}
		raw, _ := json.Marshal(body)
		uid := rbUserID(r)
		resp, err := c.do(r.Context(), http.MethodPost, "/cookies/export", uid, bytes.NewReader(raw), "application/json")
		if err != nil {
			writeErr(w, 502, err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			writeErr(w, resp.StatusCode, hostError(resp))
			return
		}
		var out rbCookieExport
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
			writeErr(w, 502, err)
			return
		}
		// Audit: who exported which provider's sign-in (never the cookie).
		log.Printf("ds-browser: user %s exported their %s sign-in (%s) for Online Accounts", uid, body.Provider, strings.Join(out.Domains, ","))
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("POST /rb/upload/{id}", func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "multipart/form-data") {
			writeErr(w, 400, errors.New("expected a multipart upload"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Hour)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://ds-browser/uploads/"+urlPathEscape(r.PathValue("id")), http.MaxBytesReader(w, r.Body, rbMaxUpload+(1<<20)))
		req.Header.Set(rbUserHeader, rbUserID(r))
		req.Header.Set("Content-Type", ct)
		client := &http.Client{Transport: c.http.Transport}
		resp, err := client.Do(req)
		if err != nil {
			writeErr(w, 502, err)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 64<<10))
	})
	mux.HandleFunc("POST /rb/staged/{guid}/save", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Dir string `json:"dir"`
		}
		_ = readJSON(r, &body)
		uid := rbUserID(r)
		guid := r.PathValue("guid")
		var d rbStaged
		if err := c.getJSON(r.Context(), "/staged/"+urlPathEscape(guid), uid, &d); err != nil {
			writeErr(w, 404, err)
			return
		}
		dir := strings.TrimSpace(body.Dir)
		if dir == "" {
			dir = c.st.Get().DefaultDir
		}
		dest, err := c.moveStaged(uid, d, dir)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		if resp, err := c.do(r.Context(), http.MethodDelete, "/staged/"+urlPathEscape(guid), uid, nil, ""); err == nil {
			resp.Body.Close()
		}
		writeJSON(w, 200, map[string]string{"path": dest})
	})
}

func urlPathEscape(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '?' || r == '#' || r == '%' || r < 0x20 {
			return -1
		}
		return r
	}, s)
	return s
}

// captureHeaders returns the cookies/Referer/UA the browser recorded for a
// download it handed over.
func (c *rbClient) captureHeaders(ctx context.Context, uid, id string) (*rbCapture, error) {
	var cp rbCapture
	if err := c.getJSON(ctx, "/captures/"+urlPathEscape(id), uid, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// moveStaged moves a download the browser finished itself (blob:/data:
// links) from its staging folder into dir. The browser runs as an
// unprivileged user and this runs as root, so the file is only taken if it
// is a plain file that user owns, inside that user's staging folder - never
// a symlink or a hard link to something else.
func (c *rbClient) moveStaged(uid string, d rbStaged, dir string) (string, error) {
	root := filepath.Join(c.stagingDir, uid)
	src := filepath.Clean(d.Path)
	if filepath.Dir(src) != root {
		return "", errors.New("the file is not in the browser's download folder")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() {
		return "", errors.New("the browser's download folder is missing")
	}
	f, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", fmt.Errorf("the download is gone: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	st, ok1 := fi.Sys().(*syscall.Stat_t)
	rst, ok2 := rootInfo.Sys().(*syscall.Stat_t)
	if !ok1 || !ok2 || st.Uid != rst.Uid || st.Nlink != 1 {
		return "", errors.New("refusing to move that file")
	}
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		return "", errors.New("save folder must be an absolute path")
	}
	if _, err := pathPolicy.MkdirAll(dir); err != nil {
		return "", err
	}
	name := sanitizeFilename(d.Filename)
	if name == "" {
		name = "download"
	}
	dest := uniquePath(filepath.Join(dir, name))
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, f); err != nil {
		out.Close()
		os.Remove(dest)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(dest)
		return "", err
	}
	if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
		// e.g. the staging folder appeared after this service started and is
		// still read-only to it (a restart fixes that).
		log.Printf("ds-browser: could not remove the staged copy of %s: %v", name, err)
	}
	return dest, nil
}

func uniquePath(p string) string {
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for n := 1; n < 10000; n++ {
		q := fmt.Sprintf("%s (%d)%s", base, n, ext)
		if _, err := os.Lstat(q); err != nil {
			return q
		}
	}
	return p
}

// SourceTexts is what the browser service builds its own filter engine
// from: the enabled cached lists plus My filters, and a version that
// changes whenever any of them does.
func (a *Adblocker) SourceTexts() ([]string, string) {
	s := a.settings.Get()
	h := sha256.New()
	var texts []string
	for _, id := range s.EnabledLists {
		raw, err := os.ReadFile(a.listPath(id))
		if err != nil {
			continue
		}
		texts = append(texts, string(raw))
		h.Write([]byte(id))
		fi, _ := os.Stat(a.listPath(id))
		if fi != nil {
			fmt.Fprintf(h, "%d:%d;", fi.Size(), fi.ModTime().UnixNano())
		}
	}
	texts = append(texts, s.CustomFilters)
	h.Write([]byte(s.CustomFilters))
	return texts, hex.EncodeToString(h.Sum(nil))[:16]
}
