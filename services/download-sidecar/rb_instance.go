package main

// One user's Chromium: launch, the CDP event loop, tabs, downloads, uploads,
// dialogs, blocking and persistence. The viewers (rb_viewer.go) only send
// commands and receive pictures and tab lists.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	rbFreezeAfter   = 5 * time.Minute
	rbDiscardAfter  = 30 * time.Minute
	rbStillDelay    = 250 * time.Millisecond
	rbMaxInflight   = 2
	cdpSendTimeout  = 10 * time.Second
	rbDefaultW      = 1280
	rbDefaultH      = 800
	rbHomeFallback  = "https://duckduckgo.com/"
	rbMaxClosedTabs = 20
)

type rbTab struct {
	ID        uint32
	TargetID  string
	Session   string
	Context   string
	URL       string
	Title     string
	Favicon   string
	Loading   bool
	Progress  float64
	CanBack   bool
	CanFwd    bool
	Blocked   int
	Opener    uint32
	Crashed   bool
	Discarded bool
	Internal  bool
	Zoom      float64

	lastActive time.Time
	frozen     bool
	reqStarted int
	reqDone    int
	iconFor    string

	// screencast
	casting   bool
	castQ     int
	castW     int
	castH     int
	castDPR   float64
	frameSeq  uint32
	chromeAck int // unacked Chrome screencast frame (0 = none)
	stillAt   time.Time
	still     *time.Timer
	metricsW  int
	metricsH  int
	metricsD  float64
	touch     bool
	lastMouse time.Time
	cursorAt  time.Time
}

// rbTabInfo is what the UI gets for each tab.
type rbTabInfo struct {
	ID       uint32  `json:"id"`
	URL      string  `json:"url"`
	Title    string  `json:"title"`
	Favicon  string  `json:"favicon,omitempty"`
	Loading  bool    `json:"loading"`
	Progress float64 `json:"progress"`
	CanBack  bool    `json:"canBack"`
	CanFwd   bool    `json:"canFwd"`
	Blocked  int     `json:"blocked"`
	Opener   uint32  `json:"opener,omitempty"`
	Crashed  bool    `json:"crashed,omitempty"`
	Zoom     float64 `json:"zoom"`
}

type rbCapture struct {
	ID        string            `json:"id"`
	URL       string            `json:"url"`
	Filename  string            `json:"filename,omitempty"`
	Referer   string            `json:"referer,omitempty"`
	Headers   map[string]string `json:"headers"`
	CreatedAt time.Time         `json:"created_at"`
}

type rbStaged struct {
	GUID     string `json:"guid"`
	Path     string `json:"path"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Done     bool   `json:"done"`
	context  string
}

type rbChooser struct {
	id       string
	tab      *rbTab
	session  string
	node     int64
	multiple bool
	at       time.Time
}

type rbDialog struct {
	id      string
	tab     *rbTab
	session string
	auth    string // Fetch requestId for an HTTP auth prompt
}

type rbSavedTab struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type rbSavedTabs struct {
	Tabs   []rbSavedTab `json:"tabs"`
	Active int          `json:"active"`
}

type rbInstance struct {
	host    *rbHost
	uid     string
	dir     string
	staging string

	ready     chan struct{}
	readyErr  error
	readyOnce sync.Once

	mu        sync.Mutex
	cmd       *exec.Cmd
	cdp       *CDPConn
	egress    *rbEgress
	ubolID    string
	ubolPage  string
	closing   bool
	crashes   []time.Time
	tabs      []*rbTab
	byTarget  map[string]*rbTab
	bySession map[string]*rbTab
	child     map[string]*rbTab // iframe/worker session -> owning tab (nil for service workers)
	frames    map[string]*rbTab // frame id -> tab
	nextTab   uint32
	viewers   map[*rbViewer]bool
	lastView  time.Time
	closed    []rbSavedTab
	captures  map[string]*rbCapture
	stagedDL  map[string]*rbStaged
	choosers  map[string]*rbChooser
	dialogs   map[string]*rbDialog
	typed     map[string]bool
	signins   map[string]*rbSignin
	saveTimer *time.Timer
	dirty     *time.Timer
	restored  *rbSavedTabs
	stderr    *tailBuffer
}

func newRBInstance(h *rbHost, uid string) *rbInstance {
	return &rbInstance{
		host:      h,
		uid:       uid,
		dir:       filepath.Join(h.stateDir, "profiles", uid),
		staging:   filepath.Join(h.stateDir, "staging", uid),
		ready:     make(chan struct{}),
		byTarget:  map[string]*rbTab{},
		bySession: map[string]*rbTab{},
		child:     map[string]*rbTab{},
		frames:    map[string]*rbTab{},
		viewers:   map[*rbViewer]bool{},
		lastView:  time.Now(),
		captures:  map[string]*rbCapture{},
		stagedDL:  map[string]*rbStaged{},
		choosers:  map[string]*rbChooser{},
		dialogs:   map[string]*rbDialog{},
		typed:     map[string]bool{},
		signins:   map[string]*rbSignin{},
	}
}

func (i *rbInstance) waitReady() error {
	select {
	case <-i.ready:
		return i.readyErr
	case <-time.After(45 * time.Second):
		return errors.New("the browser took too long to start")
	}
}

func (i *rbInstance) setReady(err error) {
	i.readyOnce.Do(func() {
		i.readyErr = err
		close(i.ready)
	})
}

// ---- launch ----

func (i *rbInstance) chromeArgs(proxy string) []string {
	return []string{
		"--headless=new",
		"--remote-debugging-pipe",
		// Needed for Extensions.loadUnpacked over the pipe (uBO Lite).
		"--enable-unsafe-extension-debugging",
		"--user-data-dir=" + i.dir,
		"--no-first-run", "--no-default-browser-check", "--no-service-autorun",
		"--disable-background-networking", "--disable-sync", "--disable-default-apps",
		"--disable-component-update", "--disable-domain-reliability", "--disable-crash-reporter",
		"--disable-quic",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--proxy-server=http://" + proxy,
		// "<-loopback>" removes Chrome's implicit bypass for localhost:
		// loopback must go through the proxy, which refuses it.
		"--proxy-bypass-list=<-loopback>",
		"--deny-permission-prompts",
		"--disable-features=MediaRouter,DialMediaRouteProvider,Translate,OptimizationHints,AutofillServerCommunication,InterestFeedContentSuggestions",
		// Without this, navigator.webdriver is true under the pipe.
		"--disable-blink-features=AutomationControlled",
		"--password-store=basic", "--use-mock-keychain",
		"--mute-audio",
		"--lang=en-US",
		"--window-size=1280,800",
		"--user-agent=" + i.host.userAgent(),
		"about:blank",
	}
}

func (i *rbInstance) run() {
	if err := i.launch(); err != nil {
		log.Printf("ds-browser: %s: %v", i.uid, err)
		i.setReady(err)
		i.host.dropInstance(i)
		return
	}
	i.setReady(nil)
	stopTitles := make(chan struct{})
	go i.watchTitles(stopTitles)
	defer close(stopTitles)
	for {
		i.mu.Lock()
		conn := i.cdp
		i.mu.Unlock()
		<-conn.Done()
		if i.cmd != nil {
			_ = i.cmd.Wait()
		}
		i.mu.Lock()
		closing := i.closing
		now := time.Now()
		recent := []time.Time{now}
		for _, t := range i.crashes {
			if now.Sub(t) < 5*time.Minute {
				recent = append(recent, t)
			}
		}
		i.crashes = recent
		i.mu.Unlock()
		if closing {
			break
		}
		log.Printf("ds-browser: %s's Chromium exited unexpectedly: %s", i.uid, i.stderr.String())
		if len(recent) >= 2 {
			i.broadcast(map[string]interface{}{"t": "fatal", "reason": "crashed", "text": "The browser keeps crashing, so Download Station switched to Lite mode."})
			break
		}
		i.broadcast(map[string]interface{}{"t": "notice", "level": "warning", "text": "The browser stopped unexpectedly and was restarted."})
		i.discardAll()
		if err := i.launch(); err != nil {
			i.broadcast(map[string]interface{}{"t": "fatal", "reason": "crashed", "text": err.Error()})
			break
		}
		i.mu.Lock()
		vs := i.viewerList()
		i.mu.Unlock()
		for _, v := range vs {
			v.reactivate()
		}
	}
	i.teardown()
	i.host.dropInstance(i)
}

func (i *rbInstance) launch() error {
	if err := os.MkdirAll(i.dir, 0o700); err != nil {
		return err
	}
	_ = os.MkdirAll(i.staging, 0o700)
	i.loadTyped()
	if i.egress == nil {
		eg, err := newRBEgress(i.isTyped)
		if err != nil {
			return err
		}
		i.egress = eg
	}
	// Chromium leaves this behind if it was killed; a stale lock makes the
	// next start refuse the profile.
	for _, f := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_ = os.Remove(filepath.Join(i.dir, f))
	}
	cmdR, cmdW, err := os.Pipe()
	if err != nil {
		return err
	}
	resR, resW, err := os.Pipe()
	if err != nil {
		cmdR.Close()
		cmdW.Close()
		return err
	}
	cmd := exec.Command(i.host.chrome, i.chromeArgs(i.egress.Addr())...)
	cmd.ExtraFiles = []*os.File{cmdR, resW} // fd 3: commands in, fd 4: replies out
	cmd.Env = []string{"HOME=" + i.dir, "PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "TMPDIR=" + os.TempDir(),
		"XDG_CONFIG_HOME=" + filepath.Join(i.dir, ".config"), "XDG_CACHE_HOME=" + filepath.Join(i.dir, ".cache")}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	i.stderr = &tailBuffer{max: 8 << 10}
	cmd.Stderr = i.stderr
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		cmdR.Close()
		cmdW.Close()
		resR.Close()
		resW.Close()
		return err
	}
	cmdR.Close()
	resW.Close()
	conn := NewCDPConn(resR, cmdW, multiCloser{cmdW, resR}, i.onEvent)
	i.mu.Lock()
	i.cmd = cmd
	i.cdp = conn
	i.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var ver struct {
		Product string `json:"product"`
	}
	if err := conn.Call(ctx, "", "Browser.getVersion", nil, &ver); err != nil {
		_ = cmd.Process.Kill()
		msg := i.stderr.String()
		if strings.Contains(msg, "sandbox") || strings.Contains(msg, "namespace") {
			i.host.setLaunchErr("sandbox")
			return fmt.Errorf("sandbox: Chromium's sandbox cannot run on this system (%s)", lastLine(msg))
		}
		return fmt.Errorf("Chromium did not start: %s", lastLine(msg))
	}
	_ = conn.Call(ctx, "", "Target.setDiscoverTargets", map[string]interface{}{"discover": true}, nil)
	_ = conn.Call(ctx, "", "Target.setAutoAttach", map[string]interface{}{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}, nil)
	_ = conn.Call(ctx, "", "Browser.setDownloadBehavior", map[string]interface{}{"behavior": "allowAndName", "downloadPath": i.staging, "eventsEnabled": true}, nil)
	i.loadUBOL(ctx)
	// The about:blank window Chromium opens at start is ours to reuse.
	i.restoreTabs()
	log.Printf("ds-browser: %s's browser is up (%s, blocker: %s)", i.uid, ver.Product, i.blockerName())
	return nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if j := strings.LastIndexByte(s, '\n'); j >= 0 {
		s = s[j+1:]
	}
	return rbText(s, 300)
}

type multiCloser []io.Closer

func (m multiCloser) Close() error {
	for _, c := range m {
		_ = c.Close()
	}
	return nil
}

// tailBuffer keeps the last max bytes written (Chromium's stderr).
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = t.b[len(t.b)-t.max:]
	}
	t.mu.Unlock()
	return len(p), nil
}

func (t *tailBuffer) String() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}

func (i *rbInstance) loadUBOL(ctx context.Context) {
	dir, err := i.host.ubolCopy()
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("ds-browser: uBO Lite: %v", err)
		}
		return
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := i.cdp.Call(ctx, "", "Extensions.loadUnpacked", map[string]interface{}{"path": dir}, &res); err != nil {
		log.Printf("ds-browser: uBO Lite did not load (%v) - using the built-in filter engine only", err)
		return
	}
	i.mu.Lock()
	i.ubolID = res.ID
	i.mu.Unlock()
	go i.applyUBOLModes()
}

func (i *rbInstance) blockerName() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.ubolID != "" {
		return "ubol+filters"
	}
	return "filters"
}

// applyUBOLModes pushes the global switch and the trusted sites into uBO
// Lite's own per-site filtering modes: "none" for trusted sites (or for
// every site when the blocker is off), "optimal" everywhere else. It talks
// to uBO Lite the way its own popup does, from one of its extension pages.
func (i *rbInstance) applyUBOLModes() {
	i.mu.Lock()
	id, conn := i.ubolID, i.cdp
	i.mu.Unlock()
	if id == "" || conn == nil {
		return
	}
	st := i.host.adblock.Load()
	none := []string{}
	optimal := []string{}
	if st.enabled {
		for _, s := range st.allowed {
			if h := normalizeHost(s); h != "" {
				none = append(none, h)
			}
		}
		optimal = append(optimal, "all-urls")
	} else {
		none = append(none, "all-urls")
	}
	modes, _ := json.Marshal(map[string]interface{}{"none": none, "basic": []string{}, "optimal": optimal, "complete": []string{}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var t struct {
		TargetID string `json:"targetId"`
	}
	page := "chrome-extension://" + id + "/dashboard.html"
	i.mu.Lock()
	i.ubolPage = page
	i.mu.Unlock()
	if err := conn.Call(ctx, "", "Target.createTarget", map[string]interface{}{"url": page, "background": true}, &t); err != nil {
		return
	}
	defer conn.Send("", "Target.closeTarget", map[string]interface{}{"targetId": t.TargetID})
	var at struct {
		SessionID string `json:"sessionId"`
	}
	if err := conn.Call(ctx, "", "Target.attachToTarget", map[string]interface{}{"targetId": t.TargetID, "flatten": true}, &at); err != nil {
		return
	}
	expr := `new Promise((resolve) => { const go = () => { if (!(window.chrome && chrome.runtime && chrome.runtime.sendMessage)) { setTimeout(go, 100); return; }
		chrome.runtime.sendMessage({ what: 'setFilteringModeDetails', modes: ` + string(modes) + ` }).then(() => resolve('ok'), e => resolve(String(e))); }; go(); })`
	var res struct {
		Result struct {
			Value interface{} `json:"value"`
		} `json:"result"`
	}
	for try := 0; try < 3; try++ {
		if err := conn.Call(ctx, at.SessionID, "Runtime.evaluate", map[string]interface{}{"expression": expr, "awaitPromise": true, "returnByValue": true}, &res); err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// ---- lifecycle ----

func (i *rbInstance) idleFor(now time.Time) time.Duration {
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.viewers) > 0 {
		return 0
	}
	return now.Sub(i.lastView)
}

func (i *rbInstance) close() {
	i.mu.Lock()
	if i.closing {
		i.mu.Unlock()
		return
	}
	i.closing = true
	conn, cmd := i.cdp, i.cmd
	vs := i.viewerList()
	i.mu.Unlock()
	i.saveTabsNow()
	for _, v := range vs {
		v.closeWith("closed", "The browser was closed.")
	}
	if conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		// A clean Browser.close flushes cookies and the profile to disk.
		_ = conn.Call(ctx, "", "Browser.close", nil, nil)
		cancel()
		select {
		case <-conn.Done():
		case <-time.After(5 * time.Second):
		}
	}
	if cmd != nil && cmd.Process != nil {
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	i.host.dropInstance(i)
}

func (i *rbInstance) teardown() {
	i.mu.Lock()
	eg := i.egress
	i.egress = nil
	i.mu.Unlock()
	if eg != nil {
		eg.Close()
	}
	i.setReady(errors.New("the browser stopped"))
}

// discardAll turns every tab into a placeholder that reloads when opened
// (after a browser restart).
func (i *rbInstance) discardAll() {
	i.mu.Lock()
	for _, t := range i.tabs {
		t.Discarded = true
		t.TargetID, t.Session = "", ""
		t.casting = false
		t.Loading = false
	}
	i.byTarget = map[string]*rbTab{}
	i.bySession = map[string]*rbTab{}
	i.child = map[string]*rbTab{}
	i.frames = map[string]*rbTab{}
	i.mu.Unlock()
}

// housekeep freezes tabs nobody has looked at for 5 minutes and discards
// them after 30 (they keep their address and title, and reload when
// clicked). It also drops stale captures and file choosers.
func (i *rbInstance) housekeep(now time.Time) {
	i.mu.Lock()
	var freeze []string
	var discard []*rbTab
	watched := i.watchedTabs()
	for _, t := range i.tabs {
		if t.Discarded || t.Internal || watched[t] || t.Session == "" {
			continue
		}
		idle := now.Sub(t.lastActive)
		if idle >= rbDiscardAfter && t.Context == "" {
			discard = append(discard, t)
		} else if idle >= rbFreezeAfter && !t.frozen {
			t.frozen = true
			freeze = append(freeze, t.Session)
		}
	}
	for id, c := range i.captures {
		if now.Sub(c.CreatedAt) > 2*time.Hour {
			delete(i.captures, id)
		}
	}
	for id, c := range i.choosers {
		if now.Sub(c.at) > 30*time.Minute {
			delete(i.choosers, id)
		}
	}
	conn := i.cdp
	i.mu.Unlock()
	if conn == nil {
		return
	}
	for _, s := range freeze {
		conn.Send(s, "Page.setWebLifecycleState", map[string]interface{}{"state": "frozen"})
	}
	for _, t := range discard {
		i.mu.Lock()
		target := t.TargetID
		t.Discarded = true
		delete(i.byTarget, t.TargetID)
		delete(i.bySession, t.Session)
		t.TargetID, t.Session = "", ""
		i.mu.Unlock()
		conn.Send("", "Target.closeTarget", map[string]interface{}{"targetId": target})
	}
	if len(discard) > 0 {
		i.tabsChanged()
	}
}

// ---- viewers ----

func (i *rbInstance) addViewer(v *rbViewer) {
	i.mu.Lock()
	i.viewers[v] = true
	i.mu.Unlock()
}

func (i *rbInstance) removeViewer(v *rbViewer) {
	i.mu.Lock()
	delete(i.viewers, v)
	i.lastView = time.Now()
	i.mu.Unlock()
	i.updateCasts()
}

func (i *rbInstance) viewerList() []*rbViewer {
	out := make([]*rbViewer, 0, len(i.viewers))
	for v := range i.viewers {
		out = append(out, v)
	}
	return out
}

// watchedTabs: tabs some viewer is showing. i.mu held.
func (i *rbInstance) watchedTabs() map[*rbTab]bool {
	m := map[*rbTab]bool{}
	for v := range i.viewers {
		if t := i.tabByID(v.activeID()); t != nil {
			m[t] = true
		}
	}
	return m
}

func (i *rbInstance) broadcast(msg interface{}) {
	i.mu.Lock()
	vs := i.viewerList()
	i.mu.Unlock()
	for _, v := range vs {
		v.sendJSON(msg)
	}
}

func (i *rbInstance) broadcastCtx(context string, msg interface{}) {
	i.mu.Lock()
	vs := i.viewerList()
	i.mu.Unlock()
	for _, v := range vs {
		if v.context == context {
			v.sendJSON(msg)
		}
	}
}

// tabsChanged schedules a tab-list update to every viewer (coalesced).
func (i *rbInstance) tabsChanged() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.dirty != nil {
		return
	}
	i.dirty = time.AfterFunc(80*time.Millisecond, func() {
		i.mu.Lock()
		i.dirty = nil
		vs := i.viewerList()
		i.mu.Unlock()
		for _, v := range vs {
			v.sendTabs()
		}
		i.scheduleSave()
	})
}

func (i *rbInstance) tabsFor(context string) []rbTabInfo {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := []rbTabInfo{}
	for _, t := range i.tabs {
		if t.Internal || t.Context != context {
			continue
		}
		z := t.Zoom
		if z == 0 {
			z = 1
		}
		out = append(out, rbTabInfo{ID: t.ID, URL: t.URL, Title: t.Title, Favicon: t.Favicon, Loading: t.Loading, Progress: t.Progress,
			CanBack: t.CanBack, CanFwd: t.CanFwd, Blocked: t.Blocked, Opener: t.Opener, Crashed: t.Crashed, Zoom: z})
	}
	return out
}

func (i *rbInstance) tabCount() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	n := 0
	for _, t := range i.tabs {
		if !t.Internal && t.Context == "" {
			n++
		}
	}
	return n
}

// i.mu held.
func (i *rbInstance) tabByID(id uint32) *rbTab {
	for _, t := range i.tabs {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (i *rbInstance) tab(id uint32) *rbTab {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.tabByID(id)
}

// ---- tabs ----

// i.mu held. Inserts a new tab after its opener (and the opener's other
// children), or at the end.
func (i *rbInstance) insertTab(t *rbTab) {
	i.nextTab++
	t.ID = i.nextTab
	t.lastActive = time.Now()
	if t.Zoom == 0 {
		t.Zoom = 1
	}
	pos := len(i.tabs)
	if t.Opener != 0 {
		for j, o := range i.tabs {
			if o.ID == t.Opener {
				pos = j + 1
				for pos < len(i.tabs) && i.tabs[pos].Opener == t.Opener {
					pos++
				}
				break
			}
		}
	}
	i.tabs = append(i.tabs, nil)
	copy(i.tabs[pos+1:], i.tabs[pos:])
	i.tabs[pos] = t
}

func (i *rbInstance) userTabCount(context string) int {
	n := 0
	for _, t := range i.tabs {
		if !t.Internal && t.Context == context {
			n++
		}
	}
	return n
}

var errRBTooManyTabs = fmt.Errorf("you can have at most %d tabs open - close one first", rbMaxTabsPerUser)

// newTab opens url in a new tab of context and returns it once Chrome has
// created it.
func (i *rbInstance) newTab(ctx context.Context, rawURL, context string, opener uint32) (*rbTab, error) {
	u, err := rbCheckNavURL(rawURL)
	if err != nil {
		return nil, err
	}
	i.mu.Lock()
	if i.userTabCount(context) >= rbMaxTabsPerUser {
		i.mu.Unlock()
		return nil, errRBTooManyTabs
	}
	conn := i.cdp
	i.mu.Unlock()
	// Each tab gets its own (headless) window: a page in a window's
	// background tab is hidden, and a hidden page stops painting, so two
	// viewers could not watch two tabs. Idle tabs are frozen instead.
	params := map[string]interface{}{"url": u, "newWindow": true}
	if context != "" {
		params["browserContextId"] = context
	}
	var res struct {
		TargetID string `json:"targetId"`
	}
	if err := conn.Call(ctx, "", "Target.createTarget", params, &res); err != nil {
		return nil, err
	}
	i.mu.Lock()
	t := i.byTarget[res.TargetID]
	if t == nil {
		t = &rbTab{TargetID: res.TargetID, Context: context, URL: u, Opener: opener}
		i.byTarget[res.TargetID] = t
		i.insertTab(t)
	} else if opener != 0 && t.Opener == 0 {
		t.Opener = opener
	}
	i.mu.Unlock()
	i.tabsChanged()
	return t, nil
}

// wake gives a discarded tab a real page again.
func (i *rbInstance) wake(ctx context.Context, t *rbTab) error {
	i.mu.Lock()
	if !t.Discarded {
		i.mu.Unlock()
		return nil
	}
	conn := i.cdp
	u := t.URL
	i.mu.Unlock()
	if u == "" {
		u = "about:blank"
	}
	params := map[string]interface{}{"url": u, "newWindow": true}
	if t.Context != "" {
		params["browserContextId"] = t.Context
	}
	var res struct {
		TargetID string `json:"targetId"`
	}
	if err := conn.Call(ctx, "", "Target.createTarget", params, &res); err != nil {
		return err
	}
	i.mu.Lock()
	// The attach event may already have made a tab for this target: fold it
	// into the discarded one.
	if other := i.byTarget[res.TargetID]; other != nil && other != t {
		t.Session = other.Session
		i.bySession[t.Session] = t
		for s, owner := range i.child {
			if owner == other {
				i.child[s] = t
			}
		}
		i.removeTabLocked(other)
	}
	t.TargetID = res.TargetID
	t.Discarded = false
	t.frozen = false
	i.byTarget[res.TargetID] = t
	i.mu.Unlock()
	i.tabsChanged()
	return nil
}

// i.mu held.
func (i *rbInstance) removeTabLocked(t *rbTab) {
	for j, o := range i.tabs {
		if o == t {
			i.tabs = append(i.tabs[:j], i.tabs[j+1:]...)
			break
		}
	}
	if t.TargetID != "" && i.byTarget[t.TargetID] == t {
		delete(i.byTarget, t.TargetID)
	}
	if t.Session != "" && i.bySession[t.Session] == t {
		delete(i.bySession, t.Session)
	}
	if t.still != nil {
		t.still.Stop()
	}
}

func (i *rbInstance) closeTab(id uint32) {
	i.mu.Lock()
	t := i.tabByID(id)
	if t == nil {
		i.mu.Unlock()
		return
	}
	if t.Context == "" && t.URL != "" && t.URL != "about:blank" {
		i.closed = append(i.closed, rbSavedTab{URL: t.URL, Title: t.Title})
		if len(i.closed) > rbMaxClosedTabs {
			i.closed = i.closed[1:]
		}
	}
	target := t.TargetID
	i.removeTabLocked(t)
	conn := i.cdp
	i.mu.Unlock()
	if target != "" && conn != nil {
		conn.Send("", "Target.closeTarget", map[string]interface{}{"targetId": target})
	}
	i.tabsChanged()
}

func (i *rbInstance) popClosed() (rbSavedTab, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.closed) == 0 {
		return rbSavedTab{}, false
	}
	t := i.closed[len(i.closed)-1]
	i.closed = i.closed[:len(i.closed)-1]
	return t, true
}

func (i *rbInstance) moveTab(id uint32, index int) {
	i.mu.Lock()
	var t *rbTab
	for j, o := range i.tabs {
		if o.ID == id {
			t = o
			i.tabs = append(i.tabs[:j], i.tabs[j+1:]...)
			break
		}
	}
	if t == nil {
		i.mu.Unlock()
		return
	}
	// index counts only the tabs of t's context that the UI shows.
	pos, seen := len(i.tabs), 0
	for j, o := range i.tabs {
		if o.Internal || o.Context != t.Context {
			continue
		}
		if seen == index {
			pos = j
			break
		}
		seen++
	}
	i.tabs = append(i.tabs, nil)
	copy(i.tabs[pos+1:], i.tabs[pos:])
	i.tabs[pos] = t
	i.mu.Unlock()
	i.tabsChanged()
}

// ---- persistence ----

func (i *rbInstance) tabsFile() string { return filepath.Join(i.dir, "nivaroos-tabs.json") }

func (i *rbInstance) scheduleSave() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.saveTimer != nil || i.closing {
		return
	}
	i.saveTimer = time.AfterFunc(2*time.Second, func() {
		i.mu.Lock()
		i.saveTimer = nil
		i.mu.Unlock()
		i.saveTabsNow()
	})
}

func (i *rbInstance) saveTabsNow() {
	i.mu.Lock()
	var st rbSavedTabs
	var activeID uint32
	for v := range i.viewers {
		if v.context == "" {
			activeID = v.activeID()
		}
	}
	for _, t := range i.tabs {
		if t.Internal || t.Context != "" || t.URL == "" {
			continue
		}
		if t.ID == activeID {
			st.Active = len(st.Tabs)
		}
		st.Tabs = append(st.Tabs, rbSavedTab{URL: t.URL, Title: t.Title})
	}
	i.mu.Unlock()
	_ = writeJSONAtomic(i.tabsFile(), st)
}

// restoreTabs brings back the previous session's tabs as placeholders; only
// the one a viewer opens is loaded.
func (i *rbInstance) restoreTabs() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.tabs) > 0 {
		return
	}
	raw, err := os.ReadFile(i.tabsFile())
	if err != nil {
		return
	}
	var st rbSavedTabs
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	for n, s := range st.Tabs {
		if n >= rbMaxTabsPerUser {
			break
		}
		if _, err := rbCheckNavURL(s.URL); err != nil {
			continue
		}
		i.insertTab(&rbTab{URL: s.URL, Title: rbText(s.Title, 300), Discarded: true})
	}
	i.restored = &st
}

// restoredActive is the tab that was open when the browser last stopped.
func (i *rbInstance) restoredActive() uint32 {
	i.mu.Lock()
	defer i.mu.Unlock()
	var list []*rbTab
	for _, t := range i.tabs {
		if !t.Internal && t.Context == "" {
			list = append(list, t)
		}
	}
	if len(list) == 0 {
		return 0
	}
	if i.restored != nil && i.restored.Active >= 0 && i.restored.Active < len(list) {
		return list[i.restored.Active].ID
	}
	return list[len(list)-1].ID
}

// ---- typed hosts ----

func (i *rbInstance) typedFile() string { return filepath.Join(i.dir, "nivaroos-typed-hosts.json") }

func (i *rbInstance) loadTyped() {
	raw, err := os.ReadFile(i.typedFile())
	if err != nil {
		return
	}
	var hosts []string
	if json.Unmarshal(raw, &hosts) == nil {
		i.mu.Lock()
		for _, h := range hosts {
			i.typed[normalizeHost(h)] = true
		}
		i.mu.Unlock()
	}
}

func (i *rbInstance) isTyped(host string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.typed[normalizeHost(host)]
}

// allowTyped: the user typed this host, so it may be on the LAN.
func (i *rbInstance) allowTyped(host string) {
	h := normalizeHost(host)
	if h == "" {
		return
	}
	i.mu.Lock()
	if i.typed[h] {
		i.mu.Unlock()
		return
	}
	i.typed[h] = true
	hosts := make([]string, 0, len(i.typed))
	for k := range i.typed {
		hosts = append(hosts, k)
	}
	i.mu.Unlock()
	sort.Strings(hosts)
	_ = writeJSONAtomic(i.typedFile(), hosts)
}

// ---- captures, staged downloads, uploads ----

func (i *rbInstance) capture(id string) (*rbCapture, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	c, ok := i.captures[id]
	return c, ok
}

func (i *rbInstance) staged(guid string) (*rbStaged, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	d, ok := i.stagedDL[guid]
	if !ok || !d.Done {
		return nil, false
	}
	return d, true
}

func (i *rbInstance) forgetStaged(guid string) {
	i.mu.Lock()
	d := i.stagedDL[guid]
	delete(i.stagedDL, guid)
	i.mu.Unlock()
	if d != nil {
		_ = os.Remove(d.Path)
	}
}

// captureURL records what Download Station needs to fetch url the way this
// tab would: its cookies for that URL, the page as Referer, and the
// browser's own UA. The UI only ever sees the capture's id.
func (i *rbInstance) captureURL(ctx context.Context, t *rbTab, rawURL, filename string) (*rbCapture, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("only http and https links can be downloaded with Download Station")
	}
	i.mu.Lock()
	conn := i.cdp
	session, referer := "", ""
	if t != nil {
		session, referer = t.Session, t.URL
	}
	i.mu.Unlock()
	headers := map[string]string{"User-Agent": i.host.userAgent()}
	if referer != "" && strings.HasPrefix(referer, "http") {
		headers["Referer"] = referer
	}
	var res struct {
		Cookies []cdpCookie `json:"cookies"`
	}
	if session != "" {
		if err := conn.Call(ctx, session, "Network.getCookies", map[string]interface{}{"urls": []string{u.String()}}, &res); err == nil && len(res.Cookies) > 0 {
			headers["Cookie"] = cookieHeader(res.Cookies)
		}
	}
	c := &rbCapture{ID: newID(), URL: u.String(), Filename: sanitizeFilename(filename), Referer: headers["Referer"], Headers: headers, CreatedAt: time.Now()}
	i.mu.Lock()
	i.captures[c.ID] = c
	i.mu.Unlock()
	return c, nil
}

type cdpCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
}

func cookieHeader(cs []cdpCookie) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

const rbMaxUpload = 4 << 30

// acceptUpload stores the files the user picked (multipart "file" parts)
// and hands them to the page's file input.
func (i *rbInstance) acceptUpload(id string, r *http.Request) (int, error) {
	i.mu.Lock()
	ch := i.choosers[id]
	delete(i.choosers, id)
	conn := i.cdp
	i.mu.Unlock()
	if ch == nil {
		return 0, errors.New("that file request has expired")
	}
	dir := filepath.Join(i.staging, "uploads", sanitizeFilename(id))
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return 0, err
	}
	var paths []string
	var total int64
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		name := sanitizeFilename(part.FileName())
		if name == "" {
			name = "upload"
		}
		p := filepath.Join(dir, fmt.Sprintf("%d", len(paths)))
		_ = os.MkdirAll(p, 0o700)
		p = filepath.Join(p, name)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return 0, err
		}
		n, err := io.Copy(f, io.LimitReader(part, rbMaxUpload-total+1))
		f.Close()
		total += n
		if err != nil {
			return 0, err
		}
		if total > rbMaxUpload {
			return 0, errors.New("files larger than 4 GB in total can't be uploaded")
		}
		paths = append(paths, p)
		if !ch.multiple {
			break
		}
	}
	if len(paths) == 0 {
		return 0, errors.New("no file was sent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := conn.Call(ctx, ch.session, "DOM.setFileInputFiles", map[string]interface{}{"files": paths, "backendNodeId": ch.node}, nil); err != nil {
		return 0, err
	}
	return len(paths), nil
}

// ---- CDP events ----

type cdpTargetInfo struct {
	TargetID         string `json:"targetId"`
	Type             string `json:"type"`
	Title            string `json:"title"`
	URL              string `json:"url"`
	OpenerID         string `json:"openerId"`
	BrowserContextID string `json:"browserContextId"`
}

var rbDebug = os.Getenv("RB_DEBUG") != ""

func (i *rbInstance) onEvent(ev cdpEvent) {
	if rbDebug && ev.Method != "Page.screencastFrame" && !strings.HasPrefix(ev.Method, "Network.") && ev.Method != "Fetch.requestPaused" {
		p := string(ev.Params)
		if len(p) > 300 {
			p = p[:300]
		}
		log.Printf("cdp< %s %s %s", ev.SessionID, ev.Method, p)
	}
	switch ev.Method {
	case "Target.attachedToTarget":
		var p struct {
			SessionID          string        `json:"sessionId"`
			TargetInfo         cdpTargetInfo `json:"targetInfo"`
			WaitingForDebugger bool          `json:"waitingForDebugger"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			i.onAttached(ev.SessionID, p.SessionID, p.TargetInfo, p.WaitingForDebugger)
		}
		return
	case "Target.detachedFromTarget":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			i.mu.Lock()
			delete(i.child, p.SessionID)
			if t := i.bySession[p.SessionID]; t != nil {
				delete(i.bySession, p.SessionID)
				t.Session = ""
			}
			i.mu.Unlock()
		}
		return
	case "Target.targetInfoChanged", "Target.targetCreated":
		var p struct {
			TargetInfo cdpTargetInfo `json:"targetInfo"`
		}
		if json.Unmarshal(ev.Params, &p) == nil && p.TargetInfo.Type == "page" {
			i.mu.Lock()
			t := i.byTarget[p.TargetInfo.TargetID]
			changed := false
			if t != nil && !t.Internal {
				if p.TargetInfo.URL != "" && p.TargetInfo.URL != t.URL {
					t.URL = p.TargetInfo.URL
					changed = true
				}
				if title := rbText(p.TargetInfo.Title, 300); title != t.Title && !strings.HasPrefix(title, "chrome-extension://") {
					t.Title = title
					changed = true
				}
			}
			i.mu.Unlock()
			if changed {
				i.tabsChanged()
			}
		}
		return
	case "Target.targetDestroyed":
		var p struct {
			TargetID string `json:"targetId"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			i.mu.Lock()
			t := i.byTarget[p.TargetID]
			if t != nil {
				i.removeTabLocked(t)
			}
			i.mu.Unlock()
			if t != nil {
				i.tabsChanged()
				i.onTabGone(t)
			}
		}
		return
	case "Target.targetCrashed":
		var p struct {
			TargetID string `json:"targetId"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			i.markCrashed(i.tabForTarget(p.TargetID))
		}
		return
	case "Browser.downloadWillBegin":
		var p struct {
			FrameID           string `json:"frameId"`
			GUID              string `json:"guid"`
			URL               string `json:"url"`
			SuggestedFilename string `json:"suggestedFilename"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			go i.onDownload(p.FrameID, p.GUID, p.URL, p.SuggestedFilename)
		}
		return
	case "Browser.downloadProgress":
		var p struct {
			GUID          string  `json:"guid"`
			TotalBytes    float64 `json:"totalBytes"`
			ReceivedBytes float64 `json:"receivedBytes"`
			State         string  `json:"state"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			i.onDownloadProgress(p.GUID, int64(p.ReceivedBytes), p.State)
		}
		return
	}

	if ev.SessionID == "" {
		return
	}
	i.mu.Lock()
	tab := i.bySession[ev.SessionID]
	owner, isChild := i.child[ev.SessionID]
	i.mu.Unlock()
	switch ev.Method {
	case "Fetch.requestPaused":
		i.onRequestPaused(ev.SessionID, tab, owner, ev.Params)
		return
	case "Fetch.authRequired":
		i.onAuthRequired(ev.SessionID, tab, owner, ev.Params)
		return
	case "Network.loadingFailed":
		var p struct {
			ErrorText     string `json:"errorText"`
			BlockedReason string `json:"blockedReason"`
			Type          string `json:"type"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			t := tab
			if t == nil {
				t = owner
			}
			if t != nil && (strings.Contains(p.ErrorText, "ERR_BLOCKED_BY_CLIENT") || p.BlockedReason == "inspector") {
				i.mu.Lock()
				t.Blocked++
				i.mu.Unlock()
				i.tabsChanged()
			}
			if tab != nil {
				i.requestDone(tab)
			}
		}
		return
	}
	if isChild || tab == nil {
		return
	}
	i.onPageEvent(tab, ev)
}

func (i *rbInstance) tabForTarget(id string) *rbTab {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.byTarget[id]
}

func (i *rbInstance) onTabGone(t *rbTab) {
	i.mu.Lock()
	vs := i.viewerList()
	i.mu.Unlock()
	for _, v := range vs {
		v.onTabClosed(t)
	}
	i.updateCasts()
}

func (i *rbInstance) onAttached(parent, session string, ti cdpTargetInfo, waiting bool) {
	i.mu.Lock()
	conn := i.cdp
	ubolPage := i.ubolPage
	i.mu.Unlock()
	run := func() {
		if waiting {
			conn.Send(session, "Runtime.runIfWaitingForDebugger", nil)
		}
	}
	ua := map[string]interface{}{"userAgent": i.host.userAgent(), "acceptLanguage": "en-US,en;q=0.9", "platform": "Linux", "userAgentMetadata": i.host.uaMetadata()}
	isExt := strings.HasPrefix(ti.URL, "chrome-extension://") || strings.HasPrefix(ti.URL, "chrome://") || strings.HasPrefix(ti.URL, "devtools://")

	switch ti.Type {
	case "page":
		if parent != "" {
			// A portal/prerender inside a page: nothing to do.
			run()
			return
		}
		if isExt {
			// uBO Lite's own pages (first-run, our settings call) are never
			// shown as tabs; a stray one is closed. Our own explicit attach
			// to the settings page (not waiting) is left alone.
			if !waiting {
				return
			}
			run()
			if ubolPage == "" || !strings.HasPrefix(ti.URL, ubolPage) {
				conn.Send("", "Target.closeTarget", map[string]interface{}{"targetId": ti.TargetID})
			}
			conn.Send("", "Target.detachFromTarget", map[string]interface{}{"sessionId": session})
			return
		}
		i.mu.Lock()
		t := i.byTarget[ti.TargetID]
		created := false
		if t == nil {
			// The about:blank page Chromium opens at start, a window.open /
			// target=_blank popup, or one of our createTarget calls whose
			// reply has not arrived yet.
			t = &rbTab{TargetID: ti.TargetID, Context: ti.BrowserContextID, URL: ti.URL, Title: rbText(ti.Title, 300)}
			if _, signin := i.signins[ti.BrowserContextID]; !signin {
				t.Context = ""
			}
			if op := i.byTarget[ti.OpenerID]; op != nil {
				t.Opener = op.ID
			}
			i.byTarget[ti.TargetID] = t
			i.insertTab(t)
			created = true
		}
		t.Session = session
		t.Crashed = false
		i.bySession[session] = t
		i.frames[ti.TargetID] = t
		w, h, dpr := t.metricsW, t.metricsH, t.metricsD
		if w == 0 {
			w, h, dpr = i.defaultMetricsLocked(t.Context)
		}
		i.mu.Unlock()
		conn.Send(session, "Page.enable", nil)
		conn.Send(session, "Network.enable", map[string]interface{}{"maxTotalBufferSize": 1 << 20, "maxResourceBufferSize": 256 << 10})
		conn.Send(session, "Emulation.setUserAgentOverride", ua)
		i.applyMetrics(t, w, h, dpr)
		conn.Send(session, "Page.setInterceptFileChooserDialog", map[string]interface{}{"enabled": true})
		conn.Send(session, "Fetch.enable", fetchPatterns())
		conn.Send(session, "Target.setAutoAttach", map[string]interface{}{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true})
		run()
		if created {
			i.onPopup(t)
		}
		i.tabsChanged()
		i.updateCasts()
	case "iframe":
		i.mu.Lock()
		owner := i.bySession[parent]
		if owner == nil {
			owner = i.child[parent]
		}
		i.child[session] = owner
		i.mu.Unlock()
		conn.Send(session, "Network.enable", map[string]interface{}{"maxTotalBufferSize": 256 << 10})
		conn.Send(session, "Emulation.setUserAgentOverride", ua)
		conn.Send(session, "Fetch.enable", fetchPatterns())
		conn.Send(session, "Target.setAutoAttach", map[string]interface{}{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true})
		run()
	case "worker", "shared_worker", "service_worker":
		if isExt {
			run()
			conn.Send("", "Target.detachFromTarget", map[string]interface{}{"sessionId": session})
			return
		}
		i.mu.Lock()
		owner := i.bySession[parent]
		if owner == nil {
			owner = i.child[parent]
		}
		i.child[session] = owner
		i.mu.Unlock()
		conn.Send(session, "Network.setUserAgentOverride", ua)
		conn.Send(session, "Network.enable", map[string]interface{}{"maxTotalBufferSize": 256 << 10})
		conn.Send(session, "Fetch.enable", fetchPatterns())
		run()
	default:
		run()
		if parent == "" {
			conn.Send("", "Target.detachFromTarget", map[string]interface{}{"sessionId": session})
		}
	}
}

// i.mu held: the viewport size of whichever viewer of context was last
// active.
func (i *rbInstance) defaultMetricsLocked(context string) (int, int, float64) {
	for v := range i.viewers {
		if v.context == context && v.w > 0 {
			return v.w, v.h, v.dpr
		}
	}
	return rbDefaultW, rbDefaultH, 1
}

func fetchPatterns() map[string]interface{} {
	types := []string{"Document", "Script", "Image", "XHR", "Fetch", "Media", "Stylesheet", "Font", "Ping", "Other", "EventSource", "WebSocket"}
	pats := make([]map[string]string, 0, len(types))
	for _, t := range types {
		pats = append(pats, map[string]string{"urlPattern": "*", "resourceType": t, "requestStage": "Request"})
	}
	return map[string]interface{}{"patterns": pats, "handleAuthRequests": true}
}

// onPopup shows a tab a page opened (window.open, target=_blank) in every
// viewer that was looking at its opener.
func (i *rbInstance) onPopup(t *rbTab) {
	if t.Opener == 0 {
		return
	}
	i.mu.Lock()
	vs := i.viewerList()
	i.mu.Unlock()
	for _, v := range vs {
		if v.activeID() == t.Opener {
			v.activate(t.ID)
		}
	}
}

func (i *rbInstance) applyMetrics(t *rbTab, w, h int, dpr float64) {
	i.mu.Lock()
	session := t.Session
	conn := i.cdp
	zoom := t.Zoom
	if zoom <= 0 {
		zoom = 1
	}
	t.metricsW, t.metricsH, t.metricsD = w, h, dpr
	touch := t.touch
	i.mu.Unlock()
	if session == "" || conn == nil {
		return
	}
	conn.Send(session, "Emulation.setDeviceMetricsOverride", map[string]interface{}{
		"width": int(float64(w)/zoom + 0.5), "height": int(float64(h)/zoom + 0.5), "deviceScaleFactor": dpr * zoom, "mobile": false,
		"screenWidth": w, "screenHeight": h,
	})
	conn.Send(session, "Emulation.setTouchEmulationEnabled", map[string]interface{}{"enabled": touch, "maxTouchPoints": 5})
}

func (i *rbInstance) onRequestPaused(session string, tab, owner *rbTab, raw json.RawMessage) {
	var p struct {
		RequestID    string `json:"requestId"`
		ResourceType string `json:"resourceType"`
		FrameID      string `json:"frameId"`
		Request      struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	i.mu.Lock()
	conn := i.cdp
	page := tab
	if page == nil {
		page = owner
	}
	pageURL := ""
	if page != nil {
		pageURL = page.URL
	}
	mainDoc := tab != nil && p.ResourceType == "Document" && p.FrameID == tab.TargetID
	i.mu.Unlock()
	if !mainDoc && i.shouldBlock(p.Request.URL, pageURL, p.ResourceType) {
		conn.Send(session, "Fetch.failRequest", map[string]interface{}{"requestId": p.RequestID, "errorReason": "BlockedByClient"})
		return
	}
	conn.Send(session, "Fetch.continueRequest", map[string]interface{}{"requestId": p.RequestID})
}

// shouldBlock applies the built-in filter engine (the Download Station
// lists and My filters) - the second layer after uBO Lite, and the only one
// if uBO Lite could not be loaded.
func (i *rbInstance) shouldBlock(reqURL, pageURL, resourceType string) bool {
	st := i.host.adblock.Load()
	if st == nil || !st.enabled || st.engine == nil {
		return false
	}
	if !strings.HasPrefix(reqURL, "http") && !strings.HasPrefix(reqURL, "ws") {
		return false
	}
	pageHost := urlHost(pageURL)
	if pageHost != "" && st.siteTrusted(pageHost) {
		return false
	}
	return st.engine.Match(RequestInfo{URL: reqURL, Host: urlHost(reqURL), PageHost: pageHost, Type: cdpResourceType(resourceType)}).Blocked
}

func (i *rbInstance) onAuthRequired(session string, tab, owner *rbTab, raw json.RawMessage) {
	var p struct {
		RequestID     string `json:"requestId"`
		AuthChallenge struct {
			Source string `json:"source"`
			Origin string `json:"origin"`
			Realm  string `json:"realm"`
		} `json:"authChallenge"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	t := tab
	if t == nil {
		t = owner
	}
	i.mu.Lock()
	conn := i.cdp
	hasViewer := false
	for v := range i.viewers {
		if t != nil && v.context == t.Context {
			hasViewer = true
		}
	}
	if t == nil || !hasViewer || p.AuthChallenge.Source == "Proxy" {
		i.mu.Unlock()
		conn.Send(session, "Fetch.continueWithAuth", map[string]interface{}{"requestId": p.RequestID, "authChallengeResponse": map[string]string{"response": "CancelAuth"}})
		return
	}
	d := &rbDialog{id: newID(), tab: t, session: session, auth: p.RequestID}
	i.dialogs[d.id] = d
	ctx := t.Context
	id := t.ID
	i.mu.Unlock()
	msg := "Sign in to " + rbText(p.AuthChallenge.Origin, 200)
	if p.AuthChallenge.Realm != "" {
		msg += " (" + rbText(p.AuthChallenge.Realm, 200) + ")"
	}
	i.broadcastCtx(ctx, map[string]interface{}{"t": "dialog", "id": d.id, "tab": id, "kind": "auth", "message": msg})
}

// answerDialog resolves a JS dialog or an HTTP sign-in prompt.
func (i *rbInstance) answerDialog(id string, accept bool, text, user, pass string) {
	i.mu.Lock()
	d := i.dialogs[id]
	delete(i.dialogs, id)
	conn := i.cdp
	i.mu.Unlock()
	if d == nil || conn == nil {
		return
	}
	if d.auth != "" {
		resp := map[string]string{"response": "CancelAuth"}
		if accept {
			resp = map[string]string{"response": "ProvideCredentials", "username": user, "password": pass}
		}
		conn.Send(d.session, "Fetch.continueWithAuth", map[string]interface{}{"requestId": d.auth, "authChallengeResponse": resp})
		return
	}
	conn.Send(d.session, "Page.handleJavaScriptDialog", map[string]interface{}{"accept": accept, "promptText": text})
}

func (i *rbInstance) requestDone(t *rbTab) {
	i.mu.Lock()
	t.reqDone++
	if t.Loading && t.reqStarted > 0 {
		p := 0.15 + 0.7*float64(t.reqDone)/float64(t.reqStarted+2)
		if p > t.Progress && p < 0.95 {
			t.Progress = p
		}
	}
	i.mu.Unlock()
	i.tabsChanged()
}

func (i *rbInstance) markCrashed(t *rbTab) {
	if t == nil {
		return
	}
	i.mu.Lock()
	t.Crashed = true
	t.Loading = false
	t.casting = false
	i.mu.Unlock()
	i.tabsChanged()
	i.broadcastCtx(t.Context, map[string]interface{}{"t": "crashed", "tab": t.ID})
}

func (i *rbInstance) onPageEvent(t *rbTab, ev cdpEvent) {
	switch ev.Method {
	case "Page.screencastFrame":
		var p struct {
			Data      string `json:"data"`
			SessionID int    `json:"sessionId"`
			Metadata  struct {
				DeviceWidth  float64 `json:"deviceWidth"`
				DeviceHeight float64 `json:"deviceHeight"`
			} `json:"metadata"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			img, err := base64.StdEncoding.DecodeString(p.Data)
			if err == nil {
				i.onFrame(t, img, p.SessionID, p.Metadata.DeviceWidth, p.Metadata.DeviceHeight, false)
			}
		}
	case "Page.frameStartedLoading":
		var p struct {
			FrameID string `json:"frameId"`
		}
		if json.Unmarshal(ev.Params, &p) == nil && p.FrameID == t.TargetID {
			i.mu.Lock()
			t.Loading, t.Progress, t.reqStarted, t.reqDone = true, 0.1, 0, 0
			i.mu.Unlock()
			i.tabsChanged()
		}
	case "Page.frameStoppedLoading":
		var p struct {
			FrameID string `json:"frameId"`
		}
		if json.Unmarshal(ev.Params, &p) == nil && p.FrameID == t.TargetID {
			i.mu.Lock()
			t.Loading, t.Progress = false, 1
			i.mu.Unlock()
			i.tabsChanged()
			go i.afterLoad(t)
		}
	case "Page.domContentEventFired":
		i.mu.Lock()
		if t.Loading && t.Progress < 0.7 {
			t.Progress = 0.7
		}
		i.mu.Unlock()
		i.tabsChanged()
		go i.refreshTitle(t)
	case "Page.loadEventFired":
		go i.refreshTitle(t)
	case "Network.requestWillBeSent":
		i.mu.Lock()
		if t.Loading {
			t.reqStarted++
		}
		i.mu.Unlock()
	case "Network.loadingFinished":
		i.requestDone(t)
	case "Page.frameNavigated":
		var p struct {
			Frame struct {
				ID       string `json:"id"`
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
			} `json:"frame"`
			Type string `json:"type"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		i.mu.Lock()
		i.frames[p.Frame.ID] = t
		main := p.Frame.ParentID == "" && p.Frame.ID == t.TargetID
		if main {
			t.URL = p.Frame.URL
			t.Blocked = 0
			t.Crashed = false
			if urlHost(t.iconFor) != urlHost(t.URL) {
				t.Favicon = ""
			}
		}
		i.mu.Unlock()
		if main {
			i.tabsChanged()
			go i.refreshHistory(t)
			go i.injectCosmetics(t)
		}
	case "Page.navigatedWithinDocument":
		var p struct {
			FrameID string `json:"frameId"`
			URL     string `json:"url"`
		}
		if json.Unmarshal(ev.Params, &p) == nil && p.FrameID == t.TargetID {
			i.mu.Lock()
			t.URL = p.URL
			i.mu.Unlock()
			i.tabsChanged()
			go i.refreshHistory(t)
		}
	case "Page.javascriptDialogOpening":
		var p struct {
			Message       string `json:"message"`
			Type          string `json:"type"`
			DefaultPrompt string `json:"defaultPrompt"`
			URL           string `json:"url"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		i.mu.Lock()
		hasViewer := false
		for v := range i.viewers {
			if v.context == t.Context {
				hasViewer = true
			}
		}
		d := &rbDialog{id: newID(), tab: t, session: t.Session}
		if hasViewer {
			i.dialogs[d.id] = d
		}
		conn := i.cdp
		i.mu.Unlock()
		if !hasViewer {
			// Nobody to ask: dismiss, as a closed tab would.
			conn.Send(t.Session, "Page.handleJavaScriptDialog", map[string]interface{}{"accept": p.Type == "beforeunload"})
			return
		}
		i.broadcastCtx(t.Context, map[string]interface{}{"t": "dialog", "id": d.id, "tab": t.ID, "kind": p.Type,
			"message": rbText(p.Message, 2000), "default": rbText(p.DefaultPrompt, 500), "origin": urlHost(p.URL)})
	case "Page.fileChooserOpened":
		var p struct {
			Mode          string `json:"mode"`
			BackendNodeID int64  `json:"backendNodeId"`
		}
		if json.Unmarshal(ev.Params, &p) != nil || p.BackendNodeID == 0 {
			return
		}
		c := &rbChooser{id: newID(), tab: t, session: t.Session, node: p.BackendNodeID, multiple: p.Mode == "selectMultiple", at: time.Now()}
		i.mu.Lock()
		i.choosers[c.id] = c
		i.mu.Unlock()
		i.broadcastCtx(t.Context, map[string]interface{}{"t": "filechooser", "id": c.id, "tab": t.ID, "multiple": c.multiple})
	case "Inspector.targetCrashed":
		i.markCrashed(t)
	}
}

func (i *rbInstance) refreshHistory(t *rbTab) {
	i.mu.Lock()
	conn, session := i.cdp, t.Session
	i.mu.Unlock()
	if session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var h cdpHistory
	if conn.Call(ctx, session, "Page.getNavigationHistory", nil, &h) != nil {
		return
	}
	i.mu.Lock()
	t.CanBack = h.CurrentIndex > 0
	t.CanFwd = h.CurrentIndex < len(h.Entries)-1
	i.mu.Unlock()
	i.tabsChanged()
}

type cdpHistory struct {
	CurrentIndex int `json:"currentIndex"`
	Entries      []struct {
		ID    int    `json:"id"`
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"entries"`
}

// historyStep goes back (-1) or forward (+1) in the tab's own history.
func (i *rbInstance) historyStep(t *rbTab, delta int) {
	i.mu.Lock()
	conn, session := i.cdp, t.Session
	i.mu.Unlock()
	if session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var h cdpHistory
	if conn.Call(ctx, session, "Page.getNavigationHistory", nil, &h) != nil {
		return
	}
	j := h.CurrentIndex + delta
	if j < 0 || j >= len(h.Entries) {
		return
	}
	conn.Send(session, "Page.navigateToHistoryEntry", map[string]interface{}{"entryId": h.Entries[j].ID})
}

// refreshTitle reads document.title (Chromium's target info does not
// always follow title changes in headless mode).
func (i *rbInstance) refreshTitle(t *rbTab) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var title string
	if i.eval(ctx, t, "document.title", &title) != nil {
		return
	}
	title = rbText(strings.TrimSpace(title), 300)
	i.mu.Lock()
	changed := title != "" && title != t.Title
	if changed {
		t.Title = title
	}
	i.mu.Unlock()
	if changed {
		i.tabsChanged()
	}
}

// watchTitles keeps the titles of tabs being looked at current (pages that
// change their title without navigating: players, chats, SPAs).
func (i *rbInstance) watchTitles(stop <-chan struct{}) {
	tk := time.NewTicker(2 * time.Second)
	defer tk.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tk.C:
			i.mu.Lock()
			var list []*rbTab
			for t := range i.watchedTabs() {
				if t.Session != "" {
					list = append(list, t)
				}
			}
			i.mu.Unlock()
			for _, t := range list {
				i.refreshTitle(t)
			}
		}
	}
}

// afterLoad fetches the page's favicon (through the netguarded transport,
// as a data: URL the UI can show without contacting the site itself).
func (i *rbInstance) afterLoad(t *rbTab) {
	i.mu.Lock()
	conn, session, pageURL := i.cdp, t.Session, t.URL
	have := t.Favicon != "" && urlHost(t.iconFor) == urlHost(pageURL)
	i.mu.Unlock()
	if have || session == "" || !strings.HasPrefix(pageURL, "http") {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var res struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	expr := `(() => { const l = document.querySelector('link[rel~="icon"][href], link[rel="shortcut icon"][href], link[rel="apple-touch-icon"][href]'); return l ? l.href : new URL('/favicon.ico', location.href).href })()`
	if conn.Call(ctx, session, "Runtime.evaluate", map[string]interface{}{"expression": expr, "returnByValue": true}, &res) != nil {
		return
	}
	icon := res.Result.Value
	if icon == "" {
		return
	}
	data := fetchFavicon(ctx, icon, i.isTyped(urlHost(icon)))
	if data == "" {
		return
	}
	i.mu.Lock()
	t.Favicon = data
	t.iconFor = pageURL
	i.mu.Unlock()
	i.tabsChanged()
}

var faviconClient = &http.Client{Transport: newTransport(false), Timeout: 8 * time.Second}

func fetchFavicon(ctx context.Context, raw string, allowPrivate bool) string {
	if strings.HasPrefix(raw, "data:image/") && len(raw) < 48<<10 {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	if allowPrivate {
		ctx = withPrivateHosts(ctx, u.Hostname())
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := faviconClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 33<<10))
	if err != nil || len(body) == 0 || len(body) > 32<<10 {
		return ""
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(ct, "image/") {
		ct = http.DetectContentType(body)
	}
	switch ct {
	case "image/png", "image/x-icon", "image/vnd.microsoft.icon", "image/gif", "image/jpeg", "image/webp":
	case "image/svg+xml":
		// SVG can carry script; the UI shows it in <img>, where script
		// never runs, but keep it out anyway.
		return ""
	default:
		return ""
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(body)
}

// injectCosmetics adds the element-hiding CSS for the page's host.
func (i *rbInstance) injectCosmetics(t *rbTab) {
	st := i.host.adblock.Load()
	if st == nil || !st.enabled || st.engine == nil {
		return
	}
	i.mu.Lock()
	conn, session, pageURL := i.cdp, t.Session, t.URL
	withUBOL := i.ubolID != ""
	i.mu.Unlock()
	host := urlHost(pageURL)
	if session == "" || host == "" || st.siteTrusted(host) {
		return
	}
	// uBO Lite already does generic hiding; without it, include the generic
	// always-on selectors too.
	css := CosmeticCSS(st.engine.SelectorsForPage(pageURL, host, nil, !withUBOL))
	if css == "" {
		return
	}
	cssJSON, _ := json.Marshal(css)
	expr := `(() => { const s = document.createElement('style'); s.setAttribute('data-nivaroos', ''); s.textContent = ` + string(cssJSON) + `; (document.head || document.documentElement).appendChild(s); })()`
	conn.Send(session, "Runtime.evaluate", map[string]interface{}{"expression": expr})
}

// ---- downloads ----

func (i *rbInstance) onDownload(frameID, guid, rawURL, filename string) {
	i.mu.Lock()
	t := i.frames[frameID]
	conn := i.cdp
	i.mu.Unlock()
	ctxID := ""
	if t != nil {
		ctxID = t.Context
	}
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		// Re-fetchable: cancel it in Chromium and hand it to the
		// multi-connection downloader with this tab's cookies.
		conn.Send("", "Browser.cancelDownload", map[string]interface{}{"guid": guid})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, err := i.captureURL(ctx, t, rawURL, filename)
		if err != nil {
			return
		}
		i.broadcastCtx(ctxID, map[string]interface{}{"t": "download", "url": c.URL, "filename": c.Filename, "captured": c.ID})
		return
	}
	// blob:/data: - only Chromium has the bytes; let it finish into staging.
	i.mu.Lock()
	i.stagedDL[guid] = &rbStaged{GUID: guid, Path: filepath.Join(i.staging, guid), Filename: sanitizeFilename(filename), context: ctxID}
	i.mu.Unlock()
	i.broadcastCtx(ctxID, map[string]interface{}{"t": "notice", "level": "info", "text": "Saving " + rbText(filename, 200) + "..."})
}

func (i *rbInstance) onDownloadProgress(guid string, received int64, state string) {
	i.mu.Lock()
	d := i.stagedDL[guid]
	if d == nil {
		i.mu.Unlock()
		return
	}
	d.Size = received
	switch state {
	case "completed":
		d.Done = true
	case "canceled":
		delete(i.stagedDL, guid)
	}
	i.mu.Unlock()
	if state == "completed" {
		i.broadcastCtx(d.context, map[string]interface{}{"t": "download", "filename": d.Filename, "staged": guid, "size": d.Size})
	}
}

// ---- screencast ----

// updateCasts starts the screencast on every tab some viewer is showing and
// stops it everywhere else, at the size and quality its viewer needs.
func (i *rbInstance) updateCasts() {
	i.mu.Lock()
	conn := i.cdp
	if conn == nil {
		i.mu.Unlock()
		return
	}
	type want struct {
		w, h int
		dpr  float64
		q    int
	}
	wants := map[*rbTab]want{}
	for v := range i.viewers {
		t := i.tabByID(v.activeID())
		if t == nil || t.Session == "" || v.w == 0 {
			continue
		}
		cur, ok := wants[t]
		q := v.quality()
		if !ok || q < cur.q {
			wants[t] = want{v.w, v.h, v.dpr, q}
		} else if ok {
			wants[t] = want{v.w, v.h, v.dpr, cur.q}
		}
	}
	var start []func()
	for _, t := range i.tabs {
		wt, ok := wants[t]
		if !ok {
			if t.casting {
				t.casting = false
				s := t.Session
				start = append(start, func() { conn.Send(s, "Page.stopScreencast", nil) })
			}
			continue
		}
		t.lastActive = time.Now()
		if t.frozen {
			t.frozen = false
			s := t.Session
			start = append(start, func() { conn.Send(s, "Page.setWebLifecycleState", map[string]interface{}{"state": "active"}) })
		}
		if t.casting && t.castW == wt.w && t.castH == wt.h && t.castDPR == wt.dpr && t.castQ == wt.q {
			continue
		}
		// Chromium keeps a running screencast's size and quality: restart it.
		restart := t.casting
		t.casting, t.castW, t.castH, t.castDPR, t.castQ = true, wt.w, wt.h, wt.dpr, wt.q
		s := t.Session
		params := map[string]interface{}{"format": "jpeg", "quality": wt.q, "maxWidth": int(float64(wt.w) * wt.dpr), "maxHeight": int(float64(wt.h) * wt.dpr), "everyNthFrame": 1}
		if rbDebug {
			log.Printf("cast tab=%d %dx%d@%v q=%d restart=%v", t.ID, wt.w, wt.h, wt.dpr, wt.q, restart)
		}
		start = append(start, func() {
			if restart {
				conn.Send(s, "Page.stopScreencast", nil)
			}
			conn.Send(s, "Page.startScreencast", params)
		})
	}
	i.mu.Unlock()
	for _, f := range start {
		f()
	}
}

// onFrame fans a picture out to the tab's viewers. Chrome's frame is only
// acknowledged (which is what lets it send the next one) once some viewer
// has room for it, so a slow link lowers the frame rate rather than
// building a queue.
func (i *rbInstance) onFrame(t *rbTab, img []byte, ackID int, devW, devH float64, still bool) {
	w, h := jpegSize(img)
	i.mu.Lock()
	conn := i.cdp
	session := t.Session
	t.frameSeq++
	seq := t.frameSeq
	zoom := t.Zoom
	if zoom <= 0 {
		zoom = 1
	}
	cssW, cssH := int(devW*zoom+0.5), int(devH*zoom+0.5)
	if cssW == 0 {
		cssW, cssH = t.metricsW, t.metricsH
	}
	q := t.castQ
	if still {
		q = 92
	}
	frame := append(encodeFrameHeader(rbFrameJPEG, t.ID, seq, w, h, q, cssW, cssH), img...)
	var targets []*rbViewer
	for v := range i.viewers {
		if v.activeID() == t.ID {
			targets = append(targets, v)
		}
	}
	if !still {
		if time.Since(t.stillAt) > 300*time.Millisecond {
			if t.still != nil {
				t.still.Stop()
			}
			t.still = time.AfterFunc(rbStillDelay, func() { i.captureStill(t) })
		}
	}
	i.mu.Unlock()
	room := len(targets) == 0
	for _, v := range targets {
		if v.offerFrame(frame) {
			room = true
		}
	}
	if rbDebug {
		log.Printf("frame tab=%d still=%v bytes=%d room=%v viewers=%d", t.ID, still, len(img), room, len(targets))
	}
	if ackID != 0 {
		if room {
			conn.Send(session, "Page.screencastFrameAck", map[string]interface{}{"sessionId": ackID})
		} else {
			i.mu.Lock()
			t.chromeAck = ackID
			i.mu.Unlock()
		}
	}
}

// viewerHasRoom is called when a viewer acknowledges a frame: a frame Chrome
// is still waiting on can now be acknowledged.
func (i *rbInstance) viewerHasRoom(tabID uint32) {
	i.mu.Lock()
	t := i.tabByID(tabID)
	if t == nil || t.chromeAck == 0 {
		i.mu.Unlock()
		return
	}
	ack := t.chromeAck
	t.chromeAck = 0
	conn, session := i.cdp, t.Session
	i.mu.Unlock()
	conn.Send(session, "Page.screencastFrameAck", map[string]interface{}{"sessionId": ack})
}

// captureStill sends one sharp picture once the page has stopped changing,
// so text at rest is crisp even when the stream itself is at low quality.
func (i *rbInstance) captureStill(t *rbTab) {
	i.mu.Lock()
	conn, session := i.cdp, t.Session
	watched := false
	for v := range i.viewers {
		if v.activeID() == t.ID {
			watched = true
		}
	}
	devW, devH := float64(t.metricsW), float64(t.metricsH)
	zoom := t.Zoom
	if zoom <= 0 {
		zoom = 1
	}
	t.stillAt = time.Now()
	i.mu.Unlock()
	if !watched || session == "" || conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var res struct {
		Data string `json:"data"`
	}
	if err := conn.Call(ctx, session, "Page.captureScreenshot", map[string]interface{}{"format": "jpeg", "quality": 92, "optimizeForSpeed": true}, &res); err != nil {
		return
	}
	img, err := base64.StdEncoding.DecodeString(res.Data)
	if err != nil {
		return
	}
	i.mu.Lock()
	t.stillAt = time.Now()
	i.mu.Unlock()
	i.onFrame(t, img, 0, devW/zoom, devH/zoom, true)
}

// jpegSize reads a JPEG's pixel size from its SOF marker.
func jpegSize(b []byte) (int, int) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 0, 0
	}
	p := 2
	for p+9 < len(b) {
		if b[p] != 0xFF {
			p++
			continue
		}
		m := b[p+1]
		if m == 0xD8 || m == 0x01 || (m >= 0xD0 && m <= 0xD7) {
			p += 2
			continue
		}
		seg := int(b[p+2])<<8 | int(b[p+3])
		if (m >= 0xC0 && m <= 0xCF) && m != 0xC4 && m != 0xC8 && m != 0xCC {
			h := int(b[p+5])<<8 | int(b[p+6])
			w := int(b[p+7])<<8 | int(b[p+8])
			return w, h
		}
		p += 2 + seg
	}
	return 0, 0
}

// ---- page helpers used by viewer actions ----

// eval runs expr in the tab's page. It never counts as a user gesture, so
// our own probes (cursor, title, context menu) cannot give the page the
// right to open pop-ups; evalGesture is only for actions the user asked
// for (Undo, Cut... from the menu).
func (i *rbInstance) eval(ctx context.Context, t *rbTab, expr string, out interface{}) error {
	return i.evalOpts(ctx, t, expr, out, false)
}

func (i *rbInstance) evalGesture(ctx context.Context, t *rbTab, expr string, out interface{}) error {
	return i.evalOpts(ctx, t, expr, out, true)
}

func (i *rbInstance) evalOpts(ctx context.Context, t *rbTab, expr string, out interface{}, gesture bool) error {
	i.mu.Lock()
	conn, session := i.cdp, t.Session
	i.mu.Unlock()
	if session == "" {
		return errors.New("the page is not loaded")
	}
	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := conn.Call(ctx, session, "Runtime.evaluate", map[string]interface{}{"expression": expr, "returnByValue": true, "awaitPromise": true, "userGesture": gesture}, &res); err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		return errors.New(res.ExceptionDetails.Text)
	}
	if out != nil && len(res.Result.Value) > 0 {
		return json.Unmarshal(res.Result.Value, out)
	}
	return nil
}

func (i *rbInstance) screenshotClip(ctx context.Context, t *rbTab, x, y, w, h float64) ([]byte, error) {
	i.mu.Lock()
	conn, session := i.cdp, t.Session
	i.mu.Unlock()
	var res struct {
		Data string `json:"data"`
	}
	if err := conn.Call(ctx, session, "Page.captureScreenshot", map[string]interface{}{"format": "png",
		"clip": map[string]float64{"x": x, "y": y, "width": w, "height": h, "scale": 1}}, &res); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(res.Data)
}
