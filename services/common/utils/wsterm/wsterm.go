// Package wsterm defines the WebSocket framing shared by every NivaroOS
// interactive terminal endpoint (core /v1/sys/wsterm, app-management
// /v1/container/:id/terminal), so control messages can never be confused
// with what the user types or pastes.
//
// Protocol (version 2):
//
//	Client -> server
//	  BINARY frame                 raw terminal input (keystrokes, paste),
//	                               written to the pty/exec verbatim - never
//	                               parsed.
//	  TEXT frame, first byte 0x00  control message: 0x00 followed by a JSON
//	                               object. Currently understood:
//	                                 {"type":"resize","cols":N,"rows":N}
//	                               Unknown/invalid control messages are
//	                               dropped (never typed into the terminal).
//	  TEXT frame, anything else    legacy: treated as raw input, EXCEPT a
//	                               frame that is exactly a legacy resize
//	                               object (only the keys type/cols/rows,
//	                               type=="resize"), which is still honoured
//	                               as a resize so an old UI keeps working.
//
//	Server -> client
//	  BINARY frame                 terminal output bytes.
//	  TEXT frame                   human-readable status/error line meant to
//	                               be written into the terminal as-is.
//	  CLOSE frame                  session over; the close reason carries a
//	                               short error when it ended abnormally
//	                               (1011 = server-side failure).
//
//	Initial size: ?cols=N&rows=N on the upgrade URL.
package wsterm

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

// ControlPrefix is the first byte of a control TEXT frame.
const ControlPrefix byte = 0x00

// Control is a parsed client control message.
type Control struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// Resize reports whether c is a usable resize request.
func (c *Control) Resize() (cols, rows uint16, ok bool) {
	if c == nil || c.Type != "resize" || c.Cols <= 0 || c.Rows <= 0 || c.Cols > 10000 || c.Rows > 10000 {
		return 0, 0, false
	}
	return uint16(c.Cols), uint16(c.Rows), true
}

// ParseClientMessage classifies one client frame as either terminal input
// or a control message (exactly one of the returns is non-nil, or both nil
// for a frame that should be ignored).
func ParseClientMessage(messageType int, data []byte) (input []byte, ctrl *Control) {
	switch messageType {
	case websocket.BinaryMessage:
		return data, nil
	case websocket.TextMessage:
		if len(data) > 0 && data[0] == ControlPrefix {
			c := &Control{}
			if err := json.Unmarshal(data[1:], c); err != nil || c.Type == "" {
				return nil, nil
			}
			return nil, c
		}
		if c := parseLegacyResize(data); c != nil {
			return nil, c
		}
		return data, nil
	}
	return nil, nil
}

// parseLegacyResize accepts only the exact object the pre-v2 UI sent
// ({"type":"resize","cols":N,"rows":N}); anything with other keys, or a
// different type, is treated as typed/pasted text.
func parseLegacyResize(data []byte) *Control {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) < 2 || trimmed[0] != '{' {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	c := &Control{}
	if err := dec.Decode(c); err != nil || dec.More() {
		return nil
	}
	if _, _, ok := c.Resize(); !ok {
		return nil
	}
	return c
}

// ParseSize parses the ?cols=/?rows= query values, falling back to the
// defaults for missing/invalid/out-of-range input.
func ParseSize(colsStr, rowsStr string, defCols, defRows uint16) (cols, rows uint16) {
	cols, rows = defCols, defRows
	if v, err := strconv.Atoi(colsStr); err == nil && v > 0 && v <= 10000 {
		cols = uint16(v)
	}
	if v, err := strconv.Atoi(rowsStr); err == nil && v > 0 && v <= 10000 {
		rows = uint16(v)
	}
	return cols, rows
}

// Pump reads client frames until the connection fails/closes, writing
// input to w and dispatching resize requests to onResize (may be nil).
// It returns the read error that ended the loop.
func Pump(ws *websocket.Conn, w io.Writer, onResize func(cols, rows uint16)) error {
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		input, ctrl := ParseClientMessage(mt, data)
		if ctrl != nil {
			if cols, rows, ok := ctrl.Resize(); ok && onResize != nil {
				onResize(cols, rows)
			}
			continue
		}
		if len(input) > 0 {
			if _, err := w.Write(input); err != nil {
				return err
			}
		}
	}
}

// CopyOutput forwards r to ws as BINARY frames until r fails.
func CopyOutput(r io.Reader, ws *websocket.Conn) error {
	buf := make([]byte, 8192)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			return err
		}
	}
}

// SendError reports a failure on an already-upgraded connection (where an
// HTTP/JSON response can no longer be written): a TEXT frame for the
// terminal to display, then a CLOSE frame with code 1011.
func SendError(ws *websocket.Conn, msg string) {
	_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31m"+msg+"\x1b[0m\r\n"))
	Close(ws, websocket.CloseInternalServerErr, msg)
}

// Close sends a CLOSE frame (reason truncated to the 123-byte limit).
func Close(ws *websocket.Conn, code int, reason string) {
	if len(reason) > 120 {
		reason = reason[:120]
	}
	_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
}
