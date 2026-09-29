package main

// One browser window in the NivaroOS UI, connected over a WebSocket (the
// sidecar relays /v1/download-station/rb/ws here). Text frames are JSON
// messages {t: ...}; binary frames are pictures (see encodeFrameHeader).

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var rbUpgrader = websocket.Upgrader{
	ReadBufferSize:  16 << 10,
	WriteBufferSize: 256 << 10,
	// The sidecar checked the Origin and the token before relaying; this
	// socket is reachable only by root.
	CheckOrigin: func(*http.Request) bool { return true },
}

type rbOut struct {
	bin  bool
	data []byte
}

type rbViewer struct {
	inst *rbInstance
	conn *websocket.Conn
	out  chan rbOut
	done chan struct{}
	once sync.Once

	// set at hello; context is "" for the user's normal profile, or a
	// sign-in window's own cookie jar.
	mode     string
	provider string
	context  string

	mu       sync.Mutex
	active   uint32
	w, h     int
	dpr      float64
	touch    bool
	inflight int
	pending  []byte
	q        qualityCtl
	lastSlow bool
}

func (h *rbHost) serveViewer(w http.ResponseWriter, r *http.Request, uid string) {
	in, err := h.instance(uid, true)
	if err != nil {
		reason := "error"
		switch {
		case errors.Is(err, errRBBusy):
			reason = "busy"
		case err.Error() == "no_chrome":
			reason = "no_chrome"
		case strings.HasPrefix(err.Error(), "sandbox"):
			reason = "sandbox"
		}
		// Upgrade anyway so the UI gets a readable reason, then close.
		c, uerr := rbUpgrader.Upgrade(w, r, nil)
		if uerr != nil {
			return
		}
		msg, _ := json.Marshal(map[string]interface{}{"t": "fatal", "reason": reason, "text": err.Error()})
		_ = c.WriteMessage(websocket.TextMessage, msg)
		_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4000+len(reason), reason), time.Now().Add(time.Second))
		c.Close()
		return
	}
	c, err := rbUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	v := &rbViewer{inst: in, conn: c, out: make(chan rbOut, 512), done: make(chan struct{}), dpr: 1}
	go v.writeLoop()
	v.readLoop()
}

func (v *rbViewer) closeWith(reason, text string) {
	if text != "" {
		v.sendJSON(map[string]interface{}{"t": "fatal", "reason": reason, "text": text})
	}
	time.AfterFunc(200*time.Millisecond, func() { v.shutdown() })
}

func (v *rbViewer) shutdown() {
	v.once.Do(func() {
		close(v.done)
		_ = v.conn.Close()
	})
}

func (v *rbViewer) writeLoop() {
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-v.done:
			return
		case m := <-v.out:
			_ = v.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			typ := websocket.TextMessage
			if m.bin {
				typ = websocket.BinaryMessage
			}
			if err := v.conn.WriteMessage(typ, m.data); err != nil {
				v.shutdown()
				return
			}
		case <-ping.C:
			if err := v.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				v.shutdown()
				return
			}
		}
	}
}

func (v *rbViewer) enqueue(m rbOut) bool {
	select {
	case <-v.done:
		return false
	case v.out <- m:
		return true
	default:
		// The client stopped reading entirely.
		log.Printf("ds-browser: viewer queue full - disconnecting it")
		v.shutdown()
		return false
	}
}

func (v *rbViewer) sendJSON(msg interface{}) {
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	v.enqueue(rbOut{data: b})
}

func (v *rbViewer) activeID() uint32 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.active
}

func (v *rbViewer) quality() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.q.Quality()
}

// offerFrame queues a picture if fewer than rbMaxInflight are unacknowledged;
// otherwise it keeps it as the one to send next (replacing an older one), so
// the latest picture always arrives.
func (v *rbViewer) offerFrame(frame []byte) bool {
	v.mu.Lock()
	if v.inflight < rbMaxInflight {
		v.inflight++
		v.mu.Unlock()
		v.enqueue(rbOut{bin: true, data: frame})
		return true
	}
	v.pending = frame
	changed := v.q.OnBacklog(time.Now())
	v.mu.Unlock()
	if changed {
		v.qualityChanged()
	}
	return false
}

func (v *rbViewer) onAck() {
	v.mu.Lock()
	if v.inflight > 0 {
		v.inflight--
	}
	var next []byte
	if v.pending != nil && v.inflight < rbMaxInflight {
		next = v.pending
		v.pending = nil
		v.inflight++
	}
	// Room for a new picture: under the in-flight limit, or the one waiting
	// slot just emptied.
	room := v.inflight < rbMaxInflight || next != nil
	changed := v.q.OnTick(time.Now())
	active := v.active
	v.mu.Unlock()
	if next != nil {
		v.enqueue(rbOut{bin: true, data: next})
	}
	if room {
		v.inst.viewerHasRoom(active)
	}
	if changed {
		v.qualityChanged()
	}
}

func (v *rbViewer) qualityChanged() {
	v.mu.Lock()
	slow := v.q.Slow()
	notify := slow != v.lastSlow
	v.lastSlow = slow
	v.mu.Unlock()
	v.inst.updateCasts()
	if notify {
		v.sendJSON(map[string]interface{}{"t": "quality", "slow": slow})
	}
}

func (v *rbViewer) sendTabs() {
	v.sendJSON(map[string]interface{}{"t": "tabs", "tabs": v.inst.tabsFor(v.context), "active": v.activeID()})
}

// activate shows tab id in this viewer (waking it if it was discarded).
func (v *rbViewer) activate(id uint32) {
	in := v.inst
	t := in.tab(id)
	if t == nil || t.Context != v.context || t.Internal {
		return
	}
	if t.Discarded {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := in.wake(ctx, t)
		cancel()
		if err != nil {
			v.sendJSON(map[string]interface{}{"t": "notice", "level": "error", "text": "Could not open the tab: " + err.Error()})
			return
		}
	}
	v.mu.Lock()
	v.active = id
	v.pending = nil
	v.inflight = 0
	w, h, dpr := v.w, v.h, v.dpr
	v.mu.Unlock()
	in.mu.Lock()
	t.lastActive = time.Now()
	t.touch = v.touch
	in.mu.Unlock()
	if w > 0 {
		in.applyMetrics(t, w, h, dpr)
	}
	in.updateCasts()
	v.sendTabs()
	// A still page sends no screencast frames; make sure there is a picture.
	go func() {
		time.Sleep(120 * time.Millisecond)
		in.captureStill(t)
	}()
}

// reactivate re-opens this viewer's tab after a browser restart.
func (v *rbViewer) reactivate() {
	id := v.activeID()
	if id == 0 {
		id = v.inst.restoredActive()
	}
	if id != 0 {
		v.activate(id)
	}
}

// onTabClosed moves a viewer off a tab that went away, to its neighbour.
func (v *rbViewer) onTabClosed(t *rbTab) {
	if v.activeID() != t.ID {
		return
	}
	tabs := v.inst.tabsFor(v.context)
	if len(tabs) == 0 {
		v.mu.Lock()
		v.active = 0
		v.mu.Unlock()
		v.sendTabs()
		if v.mode == "signin" {
			v.closeWith("closed", "The sign-in page was closed.")
		}
		return
	}
	next := tabs[len(tabs)-1].ID
	if t.Opener != 0 {
		for _, x := range tabs {
			if x.ID == t.Opener {
				next = x.ID
			}
		}
	}
	v.activate(next)
}

type rbClientMsg struct {
	T          string          `json:"t"`
	Tab        uint32          `json:"tab"`
	URL        string          `json:"url"`
	Typed      bool            `json:"typed"`
	Background bool            `json:"background"`
	Hard       bool            `json:"hard"`
	W          int             `json:"w"`
	H          int             `json:"h"`
	DPR        float64         `json:"dpr"`
	Touch      bool            `json:"touch"`
	Mode       string          `json:"mode"`
	Provider   string          `json:"provider"`
	Home       string          `json:"home"`
	Index      int             `json:"index"`
	X          float64         `json:"x"`
	Y          float64         `json:"y"`
	Seq        int             `json:"seq"`
	Action     string          `json:"action"`
	Text       string          `json:"text"`
	Composing  bool            `json:"composing"`
	SelStart   int             `json:"selStart"`
	SelEnd     int             `json:"selEnd"`
	Dir        string          `json:"dir"`
	Factor     float64         `json:"factor"`
	ID         string          `json:"id"`
	Accept     bool            `json:"accept"`
	User       string          `json:"user"`
	Pass       string          `json:"pass"`
	Entry      int             `json:"entry"`
	Filename   string          `json:"filename"`
	Points     []rbTouchPoint  `json:"points"`
	Mods       int             `json:"mods"`
	Raw        json.RawMessage `json:"-"`
}

type rbTouchPoint struct {
	ID int     `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

func (v *rbViewer) readLoop() {
	in := v.inst
	defer func() {
		v.shutdown()
		in.removeViewer(v)
		if v.mode == "signin" && v.context != "" {
			in.endSignin(v.context)
		}
	}()
	v.conn.SetReadLimit(4 << 20)
	_ = v.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	v.conn.SetPongHandler(func(string) error {
		return v.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	})
	for {
		typ, data, err := v.conn.ReadMessage()
		if err != nil {
			return
		}
		_ = v.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		if typ != websocket.TextMessage {
			continue
		}
		raw := data
		var m rbClientMsg
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if m.T != "hello" && v.mode == "" {
			continue
		}
		v.handle(m, raw)
	}
}

func clampViewport(w, h int, dpr float64) (int, int, float64) {
	w, h = clampInt(w, 200, 3840), clampInt(h, 150, 2400)
	if dpr < 1 {
		dpr = 1
	}
	if dpr > 2 {
		dpr = 2
	}
	return w, h, dpr
}

func (v *rbViewer) handle(m rbClientMsg, raw []byte) {
	in := v.inst
	switch m.T {
	case "hello":
		if v.mode != "" {
			return
		}
		v.mu.Lock()
		v.w, v.h, v.dpr = clampViewport(m.W, m.H, m.DPR)
		v.touch = m.Touch
		v.mu.Unlock()
		if m.Mode == "signin" {
			v.mode, v.provider = "signin", m.Provider
			in.addViewer(v)
			if err := in.startSignin(v, m.Provider); err != nil {
				v.closeWith("error", err.Error())
				return
			}
		} else {
			v.mode = "browse"
			in.addViewer(v)
		}
		v.sendJSON(map[string]interface{}{"t": "hello", "engine": in.blockerName(), "chrome": in.host.chromeV, "maxTabs": rbMaxTabsPerUser, "mode": v.mode})
		if v.mode == "browse" {
			if id := in.restoredActive(); id != 0 {
				v.activate(id)
			} else {
				v.sendTabs()
			}
		}
	case "resize":
		v.mu.Lock()
		v.w, v.h, v.dpr = clampViewport(m.W, m.H, m.DPR)
		w, h, dpr := v.w, v.h, v.dpr
		v.mu.Unlock()
		if t := in.tab(v.activeID()); t != nil {
			in.applyMetrics(t, w, h, dpr)
		}
		in.updateCasts()
	case "ack":
		v.onAck()
	case "tab.new":
		if v.mode == "signin" {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			u := m.URL
			if u == "" {
				u = "about:blank"
			}
			if m.Typed {
				in.allowTyped(urlHost(u))
			}
			t, err := in.newTab(ctx, u, v.context, m.Tab)
			if err != nil {
				v.sendJSON(map[string]interface{}{"t": "notice", "level": "error", "text": err.Error()})
				return
			}
			if !m.Background {
				v.activate(t.ID)
			}
		}()
	case "tab.close":
		if t := in.tab(m.Tab); t != nil && t.Context == v.context {
			in.closeTab(m.Tab)
		}
	case "tab.activate":
		go v.activate(m.Tab)
	case "tab.duplicate":
		if t := in.tab(m.Tab); t != nil && t.Context == v.context && v.mode != "signin" {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				nt, err := in.newTab(ctx, t.URL, v.context, t.ID)
				if err == nil {
					v.activate(nt.ID)
				}
			}()
		}
	case "tab.reopen":
		if v.mode == "signin" {
			return
		}
		if s, ok := in.popClosed(); ok {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if t, err := in.newTab(ctx, s.URL, "", 0); err == nil {
					v.activate(t.ID)
				}
			}()
		}
	case "tab.move":
		if t := in.tab(m.Tab); t != nil && t.Context == v.context {
			in.moveTab(m.Tab, m.Index)
		}
	case "nav":
		t := v.ownTab(m.Tab)
		if t == nil {
			return
		}
		u, err := rbCheckNavURL(m.URL)
		if err != nil {
			v.sendJSON(map[string]interface{}{"t": "notice", "level": "error", "text": err.Error()})
			return
		}
		if m.Typed {
			in.allowTyped(urlHost(u))
		}
		in.mu.Lock()
		t.URL = u
		t.Crashed = false
		in.mu.Unlock()
		in.send(t, "Page.navigate", map[string]interface{}{"url": u, "transitionType": "typed"})
	case "back", "fwd":
		if t := v.ownTab(m.Tab); t != nil {
			d := -1
			if m.T == "fwd" {
				d = 1
			}
			go in.historyStep(t, d)
		}
	case "reload":
		if t := v.ownTab(m.Tab); t != nil {
			in.mu.Lock()
			t.Crashed = false
			in.mu.Unlock()
			in.send(t, "Page.reload", map[string]interface{}{"ignoreCache": m.Hard})
		}
	case "stop":
		if t := v.ownTab(m.Tab); t != nil {
			in.send(t, "Page.stopLoading", nil)
		}
	case "history.get":
		if t := v.ownTab(m.Tab); t != nil {
			go v.sendHistory(t)
		}
	case "history.go":
		if t := v.ownTab(m.Tab); t != nil {
			in.send(t, "Page.navigateToHistoryEntry", map[string]interface{}{"entryId": m.Entry})
		}
	case "mouse":
		var mm rbMouse
		if json.Unmarshal(raw, &mm) != nil {
			return
		}
		t := in.tab(v.activeID())
		if t == nil {
			return
		}
		p, ok := mouseEventParams(mm, t.Zoom)
		if !ok {
			return
		}
		in.send(t, "Input.dispatchMouseEvent", p)
		if mm.Type == "move" {
			v.maybeProbeCursor(t, mm.X, mm.Y)
		}
	case "touch":
		t := in.tab(v.activeID())
		if t == nil {
			return
		}
		z := t.Zoom
		if z <= 0 {
			z = 1
		}
		pts := make([]map[string]interface{}, 0, len(m.Points))
		for _, p := range m.Points {
			pts = append(pts, map[string]interface{}{"x": p.X / z, "y": p.Y / z, "id": p.ID, "radiusX": 8, "radiusY": 8, "force": 1})
		}
		typ := map[string]string{"start": "touchStart", "move": "touchMove", "end": "touchEnd", "cancel": "touchCancel"}[m.Action]
		if typ == "" {
			return
		}
		in.send(t, "Input.dispatchTouchEvent", map[string]interface{}{"type": typ, "touchPoints": pts, "modifiers": m.Mods & 15})
	case "key":
		var k rbKey
		if json.Unmarshal(raw, &k) != nil {
			return
		}
		if t := in.tab(v.activeID()); t != nil {
			in.send(t, "Input.dispatchKeyEvent", keyEventParams(k))
		}
	case "ime":
		t := in.tab(v.activeID())
		if t == nil {
			return
		}
		if m.Composing {
			in.send(t, "Input.imeSetComposition", map[string]interface{}{"text": rbText(m.Text, 1000), "selectionStart": m.SelStart, "selectionEnd": m.SelEnd})
		} else {
			in.send(t, "Input.insertText", map[string]interface{}{"text": rbText(m.Text, 1000)})
		}
	case "paste":
		if t := in.tab(v.activeID()); t != nil && m.Text != "" {
			in.send(t, "Input.insertText", map[string]interface{}{"text": rbText(m.Text, 1<<20)})
		}
	case "hit":
		if t := in.tab(v.activeID()); t != nil {
			go v.hitTest(t, m.X, m.Y, m.Seq)
		}
	case "act":
		if t := in.tab(v.activeID()); t != nil {
			go v.action(t, m)
		}
	case "find":
		if t := v.ownTab(m.Tab); t != nil {
			go v.find(t, m.Text, m.Dir == "back")
		}
	case "zoom":
		if t := v.ownTab(m.Tab); t != nil {
			v.zoom(t, m.Factor)
		}
	case "dialog":
		in.answerDialog(m.ID, m.Accept, m.Text, m.User, m.Pass)
	case "filecancel":
		in.mu.Lock()
		delete(in.choosers, m.ID)
		in.mu.Unlock()
	case "signin.check":
		go in.checkSignin(v.context, true)
	}
}

// ownTab returns tab id if it belongs to this viewer's cookie jar.
func (v *rbViewer) ownTab(id uint32) *rbTab {
	if id == 0 {
		id = v.activeID()
	}
	t := v.inst.tab(id)
	if t == nil || t.Context != v.context {
		return nil
	}
	return t
}

func (i *rbInstance) send(t *rbTab, method string, params interface{}) {
	i.mu.Lock()
	conn, session := i.cdp, t.Session
	i.mu.Unlock()
	if conn != nil && session != "" {
		conn.Send(session, method, params)
	}
}

func (v *rbViewer) sendHistory(t *rbTab) {
	in := v.inst
	in.mu.Lock()
	conn, session := in.cdp, t.Session
	in.mu.Unlock()
	if session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var h cdpHistory
	if conn.Call(ctx, session, "Page.getNavigationHistory", nil, &h) != nil {
		return
	}
	type entry struct {
		ID    int    `json:"id"`
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	out := make([]entry, 0, len(h.Entries))
	for _, e := range h.Entries {
		out = append(out, entry{e.ID, e.URL, rbText(e.Title, 200)})
	}
	v.sendJSON(map[string]interface{}{"t": "history", "tab": t.ID, "entries": out, "index": h.CurrentIndex})
}

const rbCursorJS = `((x, y) => { let d = document, e = d.elementFromPoint(x, y);
	for (let n = 0; n < 4 && e && (e.tagName === 'IFRAME' || e.tagName === 'FRAME'); n++) { try { const r = e.getBoundingClientRect(); const cd = e.contentDocument; if (!cd) break; x -= r.left; y -= r.top; const inner = cd.elementFromPoint(x, y); if (!inner) break; d = cd; e = inner } catch (err) { break } }
	if (!e) return 'default';
	let c = getComputedStyle(e).cursor;
	if (c && c !== 'auto') return c;
	if (e.closest('a[href],button,[role=button],summary,label[for]')) return 'pointer';
	if (e.closest('input:not([type=button]):not([type=submit]):not([type=checkbox]):not([type=radio]),textarea,[contenteditable=""],[contenteditable=true]')) return 'text';
	return 'default' })`

// maybeProbeCursor asks the page what the pointer is over (headless Chrome
// has no cursor events), at most 10 times a second.
func (v *rbViewer) maybeProbeCursor(t *rbTab, x, y float64) {
	in := v.inst
	in.mu.Lock()
	if time.Since(t.cursorAt) < 100*time.Millisecond {
		in.mu.Unlock()
		return
	}
	t.cursorAt = time.Now()
	z := t.Zoom
	in.mu.Unlock()
	if z <= 0 {
		z = 1
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var css string
		if in.eval(ctx, t, rbCursorJS+"("+ftoa(x/z)+","+ftoa(y/z)+")", &css) == nil {
			css = safeCursor(css)
			v.sendJSON(map[string]interface{}{"t": "cursor", "css": css})
		}
	}()
}

// safeCursor keeps only plain CSS cursor keywords (a page's url(...) cursor
// is never passed on).
func safeCursor(c string) string {
	c = strings.TrimSpace(c)
	if j := strings.LastIndexByte(c, ','); j >= 0 {
		c = strings.TrimSpace(c[j+1:])
	}
	switch c {
	case "default", "pointer", "text", "wait", "progress", "crosshair", "move", "help", "not-allowed", "grab", "grabbing",
		"ew-resize", "ns-resize", "nesw-resize", "nwse-resize", "col-resize", "row-resize", "zoom-in", "zoom-out", "copy", "alias",
		"cell", "vertical-text", "context-menu", "no-drop", "all-scroll", "e-resize", "w-resize", "n-resize", "s-resize",
		"ne-resize", "nw-resize", "se-resize", "sw-resize", "none":
		return c
	}
	return "default"
}

func ftoa(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

const rbHitJS = `((x, y) => { const out = { editable: false }; let d = document, e = d.elementFromPoint(x, y), fx = x, fy = y;
	for (let n = 0; n < 4 && e && (e.tagName === 'IFRAME' || e.tagName === 'FRAME'); n++) { try { const r = e.getBoundingClientRect(); const cd = e.contentDocument; if (!cd) { out.frame = 'cross-origin'; break } fx -= r.left; fy -= r.top; const inner = cd.elementFromPoint(fx, fy); if (!inner) break; d = cd; e = inner } catch (err) { out.frame = 'cross-origin'; break } }
	if (!e) return out;
	const ox = x - fx, oy = y - fy;
	const a = e.closest('a[href]'); if (a && /^https?:/i.test(a.href)) { out.link = a.href; out.linkText = (a.innerText || a.textContent || '').trim().slice(0, 500) }
	const img = e.closest('img');
	if (img && (img.currentSrc || img.src)) { out.image = img.currentSrc || img.src; const r = img.getBoundingClientRect(); out.imageRect = [r.left + ox, r.top + oy, r.width, r.height] }
	const m = e.closest('video,audio'); if (m) { const s = m.currentSrc || m.src || (m.querySelector('source[src]') || {}).src; if (s) out.media = s; out.mediaKind = m.tagName.toLowerCase() }
	const ed = e.closest('input,textarea,[contenteditable]');
	if (ed && !ed.disabled && !ed.readOnly && (ed.isContentEditable || ed.tagName === 'TEXTAREA' || (ed.tagName === 'INPUT' && /^(text|search|email|url|tel|password|number|)$/i.test(ed.type || '')))) out.editable = true;
	if (ed && (ed.tagName === 'INPUT' || ed.tagName === 'TEXTAREA')) { try { const s = ed.value.substring(ed.selectionStart, ed.selectionEnd); if (s && ed.type !== 'password') out.selection = s } catch (err) {} }
	else { const s = String(d.getSelection ? d.getSelection() : ''); if (s) out.selection = s.slice(0, 20000) }
	out.pageUrl = location.href; out.pageTitle = document.title;
	return out })`

func (v *rbViewer) hitTest(t *rbTab, x, y float64, seq int) {
	z := t.Zoom
	if z <= 0 {
		z = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var res map[string]interface{}
	if err := v.inst.eval(ctx, t, rbHitJS+"("+ftoa(x/z)+","+ftoa(y/z)+")", &res); err != nil || res == nil {
		res = map[string]interface{}{}
	}
	res["t"] = "hit"
	res["seq"] = seq
	for _, k := range []string{"link", "image", "media", "pageUrl"} {
		if s, ok := res[k].(string); ok {
			res[k] = rbText(s, 8000)
		}
	}
	for _, k := range []string{"linkText", "pageTitle"} {
		if s, ok := res[k].(string); ok {
			res[k] = rbText(s, 500)
		}
	}
	v.sendJSON(res)
}

const rbSelectionJS = `(() => { let d = document;
	for (let n = 0; n < 5; n++) { const a = d.activeElement; if (a && (a.tagName === 'IFRAME' || a.tagName === 'FRAME')) { try { if (a.contentDocument) { d = a.contentDocument; continue } } catch (e) {} } break }
	const a = d.activeElement;
	if (a && (a.tagName === 'INPUT' || a.tagName === 'TEXTAREA')) { if (a.type === 'password') return ''; try { return a.value.substring(a.selectionStart, a.selectionEnd) } catch (e) { return '' } }
	return String(d.getSelection()) })()`

func execCommandJS(cmd string) string {
	return `(() => { let d = document; for (let n = 0; n < 5; n++) { const a = d.activeElement; if (a && (a.tagName === 'IFRAME' || a.tagName === 'FRAME')) { try { if (a.contentDocument) { d = a.contentDocument; continue } } catch (e) {} } break } return d.execCommand('` + cmd + `') })()`
}

// action runs one context-menu / shortcut action that needs the page.
func (v *rbViewer) action(t *rbTab, m rbClientMsg) {
	in := v.inst
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch m.Action {
	case "copy", "cut":
		var s string
		if err := in.eval(ctx, t, rbSelectionJS, &s); err == nil {
			v.sendJSON(map[string]interface{}{"t": "clip", "seq": m.Seq, "text": rbText(s, 1<<20)})
			if m.Action == "cut" && s != "" {
				_ = in.evalGesture(ctx, t, execCommandJS("delete"), nil)
			}
		}
	case "undo", "redo", "selectAll", "delete":
		_ = in.evalGesture(ctx, t, execCommandJS(m.Action), nil)
	case "copyImage":
		z := t.Zoom
		if z <= 0 {
			z = 1
		}
		var rect []float64
		_ = json.Unmarshal([]byte(m.Text), &rect)
		if len(rect) != 4 || rect[2] <= 0 || rect[3] <= 0 {
			return
		}
		png, err := in.screenshotClip(ctx, t, rect[0], rect[1], rect[2], rect[3])
		if err != nil || len(png) > 12<<20 {
			v.sendJSON(map[string]interface{}{"t": "notice", "level": "error", "text": "Could not copy the image."})
			return
		}
		frame := append([]byte{rbFrameClipPNG}, png...)
		v.enqueue(rbOut{bin: true, data: frame})
	case "capture":
		c, err := in.captureURL(ctx, t, m.URL, m.Filename)
		if err != nil {
			v.sendJSON(map[string]interface{}{"t": "notice", "level": "error", "text": err.Error()})
			return
		}
		v.sendJSON(map[string]interface{}{"t": "captured", "seq": m.Seq, "id": c.ID, "url": c.URL, "filename": c.Filename})
	case "source":
		var s string
		if err := in.eval(ctx, t, `document.documentElement ? document.documentElement.outerHTML : ''`, &s); err == nil {
			if len(s) > 4<<20 {
				s = s[:4<<20]
			}
			v.sendJSON(map[string]interface{}{"t": "source", "seq": m.Seq, "url": t.URL, "text": strings.ToValidUTF8(s, "")})
		}
	}
}

func (v *rbViewer) find(t *rbTab, text string, back bool) {
	if text == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q, _ := json.Marshal(rbText(text, 500))
	b := "false"
	if back {
		b = "true"
	}
	var found bool
	_ = v.inst.eval(ctx, t, `window.find(`+string(q)+`, false, `+b+`, true, false, true, false)`, &found)
	v.sendJSON(map[string]interface{}{"t": "find", "tab": t.ID, "found": found})
}

var rbZoomSteps = []float64{0.5, 0.67, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2}

// zoomStep: factor 0 resets, >1 zooms in a step, <1 zooms out a step.
func zoomStep(cur, factor float64) float64 {
	if cur <= 0 {
		cur = 1
	}
	if factor == 0 {
		return 1
	}
	if factor > 1 {
		for _, s := range rbZoomSteps {
			if s > cur+0.001 {
				return s
			}
		}
		return rbZoomSteps[len(rbZoomSteps)-1]
	}
	for j := len(rbZoomSteps) - 1; j >= 0; j-- {
		if rbZoomSteps[j] < cur-0.001 {
			return rbZoomSteps[j]
		}
	}
	return rbZoomSteps[0]
}

func (v *rbViewer) zoom(t *rbTab, factor float64) {
	in := v.inst
	in.mu.Lock()
	t.Zoom = zoomStep(t.Zoom, factor)
	w, h, d := t.metricsW, t.metricsH, t.metricsD
	in.mu.Unlock()
	if w > 0 {
		in.applyMetrics(t, w, h, d)
	}
	in.tabsChanged()
}
