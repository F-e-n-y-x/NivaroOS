package main

// Pure helpers shared by the Download Station browser host (rb_host.go,
// rb_instance.go, rb_viewer.go) and the sidecar's side of it
// (rb_sidecar.go): the viewer wire format, adaptive picture quality, input
// translation, and address checks. Kept free of Chrome and sockets so they
// can be unit-tested directly.

import (
	"encoding/binary"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Binary frame kinds, first byte of every binary WebSocket message.
const (
	rbFrameJPEG      = 1 // a page picture
	rbFrameClipPNG   = 2 // an image the user asked to copy
	rbFrameHeader    = 18
	rbMaxTabsPerUser = 12
)

// encodeFrameHeader lays out
//
//	[u8 kind][u32 tab][u32 frame][u16 w][u16 h][u8 quality][u16 cssW][u16 cssH]
//
// w/h are the picture's pixel size; cssW/cssH are the viewport size (in CSS
// pixels) the picture covers, so a picture that arrives after a resize is
// still drawn at the right scale.
func encodeFrameHeader(kind byte, tab, frame uint32, w, h int, quality int, cssW, cssH int) []byte {
	b := make([]byte, rbFrameHeader)
	b[0] = kind
	binary.BigEndian.PutUint32(b[1:], tab)
	binary.BigEndian.PutUint32(b[5:], frame)
	binary.BigEndian.PutUint16(b[9:], uint16(clampInt(w, 0, 65535)))
	binary.BigEndian.PutUint16(b[11:], uint16(clampInt(h, 0, 65535)))
	b[13] = byte(clampInt(quality, 0, 100))
	binary.BigEndian.PutUint16(b[14:], uint16(clampInt(cssW, 0, 65535)))
	binary.BigEndian.PutUint16(b[16:], uint16(clampInt(cssH, 0, 65535)))
	return b
}

type rbFrameInfo struct {
	Kind          byte
	Tab, Frame    uint32
	W, H, Quality int
	CSSW, CSSH    int
}

func decodeFrameHeader(b []byte) (rbFrameInfo, bool) {
	if len(b) < rbFrameHeader {
		return rbFrameInfo{}, false
	}
	return rbFrameInfo{
		Kind:    b[0],
		Tab:     binary.BigEndian.Uint32(b[1:]),
		Frame:   binary.BigEndian.Uint32(b[5:]),
		W:       int(binary.BigEndian.Uint16(b[9:])),
		H:       int(binary.BigEndian.Uint16(b[11:])),
		Quality: int(b[13]),
		CSSW:    int(binary.BigEndian.Uint16(b[14:])),
		CSSH:    int(binary.BigEndian.Uint16(b[16:])),
	}, true
}

// ---- adaptive quality ----

// rbQualityLevels are the JPEG qualities the stream steps through: sharp on
// a fast link, lighter when frames back up.
var rbQualityLevels = []int{80, 60, 45}

// qualityCtl picks the stream's JPEG quality. A backlog (a frame had to
// wait because the viewer had not acknowledged the previous ones) steps the
// quality down, at most once a second; a link that keeps up for 5 s steps
// it back up.
type qualityCtl struct {
	level       int
	lastBacklog time.Time
	lastChange  time.Time
}

func (q *qualityCtl) Quality() int { return rbQualityLevels[q.level] }

// Slow reports whether the stream is currently below full quality (the UI
// shows a "slow connection" badge).
func (q *qualityCtl) Slow() bool { return q.level > 0 }

func (q *qualityCtl) OnBacklog(now time.Time) bool {
	q.lastBacklog = now
	if q.level < len(rbQualityLevels)-1 && now.Sub(q.lastChange) >= time.Second {
		q.level++
		q.lastChange = now
		return true
	}
	return false
}

func (q *qualityCtl) OnTick(now time.Time) bool {
	if q.level > 0 && now.Sub(q.lastBacklog) >= 5*time.Second && now.Sub(q.lastChange) >= 5*time.Second {
		q.level--
		q.lastChange = now
		return true
	}
	return false
}

// ---- input ----

// CDP modifier bits (Input.dispatchKeyEvent / dispatchMouseEvent).
const (
	modAlt   = 1
	modCtrl  = 2
	modMeta  = 4
	modShift = 8
)

type rbKey struct {
	Type    string `json:"type"` // down | up
	Key     string `json:"key"`
	Code    string `json:"code"`
	KeyCode int    `json:"keyCode"`
	Mods    int    `json:"mods"`
	Text    string `json:"text"`
	Repeat  bool   `json:"repeat"`
	Loc     int    `json:"loc"`
}

// keyEventParams turns a key message from the viewer into
// Input.dispatchKeyEvent parameters. A key-down that types a character is
// a "keyDown" with its text (so it inserts it); every other key-down is a
// "rawKeyDown", which runs the key's default action (arrows, Backspace,
// Ctrl+A...) without inserting anything.
func keyEventParams(k rbKey) map[string]interface{} {
	p := map[string]interface{}{
		"modifiers":             k.Mods & (modAlt | modCtrl | modMeta | modShift),
		"key":                   k.Key,
		"code":                  k.Code,
		"windowsVirtualKeyCode": k.KeyCode,
		"nativeVirtualKeyCode":  k.KeyCode,
		"autoRepeat":            k.Repeat,
		"location":              clampInt(k.Loc, 0, 3),
	}
	text := k.Text
	if text == "" {
		switch k.Key {
		case "Enter":
			text = "\r"
		}
	}
	// Ctrl/Meta chords never insert text (Ctrl+A selects, it doesn't type "a").
	if k.Mods&(modCtrl|modMeta) != 0 {
		text = ""
	}
	if len([]rune(text)) > 4 {
		text = ""
	}
	switch k.Type {
	case "up":
		p["type"] = "keyUp"
	default:
		if text != "" {
			p["type"] = "keyDown"
			p["text"] = text
			p["unmodifiedText"] = text
		} else {
			p["type"] = "rawKeyDown"
		}
	}
	return p
}

type rbMouse struct {
	Type    string  `json:"type"` // down | up | move | wheel | leave
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Button  int     `json:"button"` // DOM MouseEvent.button
	Buttons int     `json:"buttons"`
	Clicks  int     `json:"clicks"`
	Mods    int     `json:"mods"`
	DX      float64 `json:"dx"`
	DY      float64 `json:"dy"`
}

var domButtonNames = map[int]string{0: "left", 1: "middle", 2: "right", 3: "back", 4: "forward"}

// mouseEventParams maps a viewer mouse message onto
// Input.dispatchMouseEvent. zoom is the tab's page zoom: the viewer sends
// positions in its own CSS pixels, the page lays out at 1/zoom of them.
func mouseEventParams(m rbMouse, zoom float64) (map[string]interface{}, bool) {
	if zoom <= 0 {
		zoom = 1
	}
	p := map[string]interface{}{
		"x":         m.X / zoom,
		"y":         m.Y / zoom,
		"modifiers": m.Mods & (modAlt | modCtrl | modMeta | modShift),
		"buttons":   m.Buttons & 31,
	}
	switch m.Type {
	case "down", "up":
		name, ok := domButtonNames[m.Button]
		if !ok {
			return nil, false
		}
		if m.Type == "down" {
			p["type"] = "mousePressed"
		} else {
			p["type"] = "mouseReleased"
		}
		p["button"] = name
		p["clickCount"] = clampInt(m.Clicks, 1, 3)
	case "move":
		p["type"] = "mouseMoved"
		p["button"] = "none"
	case "wheel":
		p["type"] = "mouseWheel"
		p["deltaX"] = m.DX
		p["deltaY"] = m.DY
	default:
		return nil, false
	}
	return p, true
}

// ---- addresses ----

var errRBScheme = errors.New("only http and https addresses can be opened")

// rbCheckNavURL is the URL bar's rule: http, https or about:blank only.
// file:, chrome:, devtools:, javascript: and data: never reach Chrome from
// here (a page itself cannot navigate to the privileged ones either).
func rbCheckNavURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "about:blank" {
		return "about:blank", nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", errRBScheme
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", errRBScheme
	}
	if u.User != nil {
		// user:pass@host in a typed address is almost always phishing.
		u.User = nil
	}
	return u.String(), nil
}

var rbUIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// rbSafeUID keeps a user id usable as a directory name.
func rbSafeUID(s string) (string, bool) {
	if !rbUIDRe.MatchString(s) {
		return "", false
	}
	return s, true
}

// rbText trims page-supplied text (titles, dialog messages) before it goes
// to the UI - which only ever shows it as text, never as HTML.
func rbText(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}

// urlHost returns the lower-cased host of a URL ("" if none).
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// cdpResourceType maps Fetch/Network resourceType names onto the filter
// engine's types.
func cdpResourceType(t string) resType {
	switch t {
	case "Document":
		return typeSubdocument
	case "Stylesheet":
		return typeStylesheet
	case "Image":
		return typeImage
	case "Media":
		return typeMedia
	case "Font":
		return typeFont
	case "Script":
		return typeScript
	case "XHR", "Fetch", "EventSource":
		return typeXHR
	case "WebSocket":
		return typeWebsocket
	case "Ping", "CSPViolationReport":
		return typePing
	}
	return typeOther
}
