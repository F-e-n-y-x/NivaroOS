package wsterm

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// The frames the web UI's TerminalCard sends, end to end over a real
// WebSocket: typed input as BINARY, resize and keepalive as NUL-prefixed
// TEXT control frames. Only the input may reach the terminal.
func TestPumpDeliversOnlyInputFromTheUIFrames(t *testing.T) {
	var (
		mu      sync.Mutex
		typed   bytes.Buffer
		resizes [][2]uint16
		done    = make(chan struct{})
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer close(done)
		_ = Pump(ws, writerFunc(func(p []byte) (int, error) {
			mu.Lock()
			defer mu.Unlock()
			return typed.Write(p)
		}), func(c, r uint16) {
			mu.Lock()
			defer mu.Unlock()
			resizes = append(resizes, [2]uint16{c, r})
		})
	}))
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	send := func(mt int, s string) {
		if err := c.WriteMessage(mt, []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	send(websocket.TextMessage, "\x00"+`{"type":"resize","cols":120,"rows":32}`)
	send(websocket.BinaryMessage, "ls -la\r")
	// A paste that looks exactly like a control message is still input.
	send(websocket.BinaryMessage, `{"type":"resize","cols":1,"rows":1}`)
	send(websocket.TextMessage, "\x00"+`{"type":"ping"}`)
	send(websocket.TextMessage, "\x00garbage")
	c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	c.Close()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if got, want := typed.String(), `ls -la`+"\r"+`{"type":"resize","cols":1,"rows":1}`; got != want {
		t.Fatalf("terminal received %q, want %q", got, want)
	}
	if len(resizes) != 1 || resizes[0] != [2]uint16{120, 32} {
		t.Fatalf("want exactly one resize, got %v", resizes)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
