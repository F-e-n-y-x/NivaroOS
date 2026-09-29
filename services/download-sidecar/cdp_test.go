package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeChrome is the other end of a CDPConn: it records every command and
// answers it with a handler's result (or {} by default).
type fakeChrome struct {
	t       *testing.T
	in      *bufio.Reader // commands from the client
	out     io.Writer     // replies/events to the client
	wmu     sync.Mutex
	mu      sync.Mutex
	calls   []cdpMessage
	handler func(m cdpMessage) (interface{}, *cdpError)
	notify  chan cdpMessage
}

// newFakeChrome wires a CDPConn to a fake browser. onEvent receives the
// events the fake emits.
func newFakeChrome(t *testing.T, onEvent func(cdpEvent)) (*CDPConn, *fakeChrome) {
	t.Helper()
	cmdR, cmdW := io.Pipe()
	resR, resW := io.Pipe()
	f := &fakeChrome{t: t, in: bufio.NewReader(cmdR), out: resW, notify: make(chan cdpMessage, 1024)}
	conn := NewCDPConn(resR, cmdW, multiCloser{cmdW, resR}, onEvent)
	go f.serve()
	t.Cleanup(func() { conn.Close(); resW.Close(); cmdR.Close() })
	return conn, f
}

func (f *fakeChrome) serve() {
	for {
		raw, err := f.in.ReadBytes(0)
		if err != nil {
			return
		}
		var m cdpMessage
		if json.Unmarshal(raw[:len(raw)-1], &m) != nil {
			continue
		}
		f.mu.Lock()
		f.calls = append(f.calls, m)
		h := f.handler
		f.mu.Unlock()
		select {
		case f.notify <- m:
		default:
		}
		var result interface{} = map[string]interface{}{}
		var cerr *cdpError
		if h != nil {
			if r, e := h(m); e != nil {
				cerr = e
			} else if r != nil {
				result = r
			}
		}
		reply := cdpMessage{ID: m.ID, SessionID: m.SessionID, Error: cerr}
		if cerr == nil {
			reply.Result, _ = json.Marshal(result)
		}
		f.write(reply)
	}
}

func (f *fakeChrome) write(m cdpMessage) {
	b, _ := json.Marshal(m)
	f.wmu.Lock()
	defer f.wmu.Unlock()
	_, _ = f.out.Write(append(b, 0))
}

// emit sends an event to the client.
func (f *fakeChrome) emit(session, method string, params interface{}) {
	raw, _ := json.Marshal(params)
	f.write(cdpMessage{SessionID: session, Method: method, Params: raw})
}

func (f *fakeChrome) setHandler(h func(m cdpMessage) (interface{}, *cdpError)) {
	f.mu.Lock()
	f.handler = h
	f.mu.Unlock()
}

// waitCall waits for a command with this method (and session, if not "*").
func (f *fakeChrome) waitCall(session, method string) cdpMessage {
	f.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		f.mu.Lock()
		for _, c := range f.calls {
			if c.Method == method && (session == "*" || c.SessionID == session) {
				f.mu.Unlock()
				return c
			}
		}
		f.mu.Unlock()
		select {
		case <-f.notify:
		case <-deadline:
			f.t.Fatalf("no %s call on session %q; got %v", method, session, f.methods())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (f *fakeChrome) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.SessionID+":"+c.Method)
	}
	return out
}

func (f *fakeChrome) callsOf(method string) []cdpMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []cdpMessage
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func TestCDPCallReplyAndError(t *testing.T) {
	conn, f := newFakeChrome(t, nil)
	f.setHandler(func(m cdpMessage) (interface{}, *cdpError) {
		if m.Method == "Bad.method" {
			return nil, &cdpError{Code: -32601, Message: "not found"}
		}
		return map[string]string{"product": "Chrome/154.0.0.0", "echo": m.SessionID}, nil
	})
	var out struct {
		Product string `json:"product"`
		Echo    string `json:"echo"`
	}
	if err := conn.Call(context.Background(), "S1", "Browser.getVersion", nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Product != "Chrome/154.0.0.0" || out.Echo != "S1" {
		t.Fatalf("reply %+v", out)
	}
	err := conn.Call(context.Background(), "", "Bad.method", map[string]int{"x": 1}, nil)
	var ce *cdpError
	if !errors.As(err, &ce) || ce.Code != -32601 {
		t.Fatalf("want cdp error, got %v", err)
	}
}

func TestCDPEventsInOrderAndSendOrdering(t *testing.T) {
	var mu sync.Mutex
	var got []string
	done := make(chan struct{})
	conn, f := newFakeChrome(t, func(ev cdpEvent) {
		mu.Lock()
		got = append(got, ev.Method)
		if len(got) == 50 {
			close(done)
		}
		mu.Unlock()
	})
	for i := 0; i < 50; i++ {
		f.emit("S", "E"+string(rune('A'+i%26)), map[string]int{"i": i})
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("events not delivered")
	}
	for i, m := range got {
		if m != "E"+string(rune('A'+i%26)) {
			t.Fatalf("event %d out of order: %s", i, m)
		}
	}
	// Fire-and-forget commands must reach the browser in call order (a
	// mouse-up must never overtake its mouse-down).
	for i := 0; i < 20; i++ {
		conn.Send("S", "Input.dispatchMouseEvent", map[string]int{"n": i})
	}
	f.waitCall("S", "Input.dispatchMouseEvent")
	deadline := time.Now().Add(3 * time.Second)
	for len(f.callsOf("Input.dispatchMouseEvent")) < 20 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	for i, c := range f.callsOf("Input.dispatchMouseEvent") {
		var p map[string]int
		_ = json.Unmarshal(c.Params, &p)
		if p["n"] != i {
			t.Fatalf("send %d arrived as %d", i, p["n"])
		}
	}
}

func TestCDPClosedAndTimeout(t *testing.T) {
	conn, f := newFakeChrome(t, nil)
	block := make(chan struct{})
	f.setHandler(func(m cdpMessage) (interface{}, *cdpError) {
		if m.Method == "Slow" {
			<-block
		}
		return nil, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := conn.Call(ctx, "", "Slow", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline, got %v", err)
	}
	close(block)
	conn.Close()
	if err := conn.Call(context.Background(), "", "After.close", nil, nil); !errors.Is(err, errCDPClosed) {
		t.Fatalf("want closed, got %v", err)
	}
	select {
	case <-conn.Done():
	default:
		t.Fatal("Done not closed")
	}
}
