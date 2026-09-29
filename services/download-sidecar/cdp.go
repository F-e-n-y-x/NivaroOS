package main

// A small Chrome DevTools Protocol client for the Download Station browser
// (rb_*.go). It speaks CDP's "flat" session mode over one connection: every
// message is a JSON object, commands carry an id (and a sessionId when they
// target an attached page), and events are delivered to one handler.
//
// The connection is Chrome's --remote-debugging-pipe (fd 3 in, fd 4 out,
// messages terminated by a NUL byte), so there is no TCP debugging port a
// web page or a LAN host could reach. chromedp's generated cdproto (tens of
// MB of code) is not used: the browser needs a few dozen methods, and raw
// JSON keeps this client small enough to test against a fake peer.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// cdpMessage is one frame in either direction.
type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (e *cdpError) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("cdp %d: %s (%s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("cdp %d: %s", e.Code, e.Message)
}

// cdpEvent is an event as handed to the handler.
type cdpEvent struct {
	SessionID string
	Method    string
	Params    json.RawMessage
}

var errCDPClosed = errors.New("browser connection closed")

// CDPConn is safe for concurrent use. Events are delivered, in order, on a
// single goroutine; a handler must not block on a Call it makes (the reply
// arrives on the reader goroutine, so that is fine) - but it must not wait
// for another event.
type CDPConn struct {
	w       io.Writer
	wmu     sync.Mutex
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan cdpMessage
	closed  chan struct{}
	err     error
	events  chan cdpEvent
	onEvent func(cdpEvent)
	closer  io.Closer
}

// NewCDPConn starts reading from r immediately. onEvent may be nil.
func NewCDPConn(r io.Reader, w io.Writer, closer io.Closer, onEvent func(cdpEvent)) *CDPConn {
	c := &CDPConn{
		w:       w,
		pending: map[int64]chan cdpMessage{},
		closed:  make(chan struct{}),
		events:  make(chan cdpEvent, 1024),
		onEvent: onEvent,
		closer:  closer,
	}
	go c.readLoop(r)
	go c.eventLoop()
	return c
}

// Done is closed when the connection ends.
func (c *CDPConn) Done() <-chan struct{} { return c.closed }

func (c *CDPConn) Close() error {
	c.shutdown(errCDPClosed)
	if c.closer != nil {
		return c.closer.Close()
	}
	return nil
}

func (c *CDPConn) shutdown(err error) {
	c.mu.Lock()
	select {
	case <-c.closed:
		c.mu.Unlock()
		return
	default:
	}
	c.err = err
	close(c.closed)
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

func (c *CDPConn) readLoop(r io.Reader) {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		raw, err := br.ReadBytes(0)
		if err != nil {
			c.shutdown(errCDPClosed)
			close(c.events)
			return
		}
		raw = raw[:len(raw)-1]
		var m cdpMessage
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if m.ID != 0 && m.Method == "" {
			c.mu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		if m.Method != "" {
			select {
			case c.events <- cdpEvent{SessionID: m.SessionID, Method: m.Method, Params: m.Params}:
			case <-c.closed:
			}
		}
	}
}

func (c *CDPConn) eventLoop() {
	for ev := range c.events {
		if c.onEvent != nil {
			c.onEvent(ev)
		}
	}
}

// Call sends one command and waits for its reply. params may be nil; out
// may be nil to discard the result.
func (c *CDPConn) Call(ctx context.Context, session, method string, params interface{}, out interface{}) error {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	id := c.nextID.Add(1)
	ch := make(chan cdpMessage, 1)
	c.mu.Lock()
	select {
	case <-c.closed:
		c.mu.Unlock()
		return errCDPClosed
	default:
	}
	c.pending[id] = ch
	c.mu.Unlock()

	frame, _ := json.Marshal(cdpMessage{ID: id, SessionID: session, Method: method, Params: raw})
	frame = append(frame, 0)
	c.wmu.Lock()
	_, err := c.w.Write(frame)
	c.wmu.Unlock()
	if err != nil {
		c.drop(id)
		c.shutdown(errCDPClosed)
		return errCDPClosed
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return errCDPClosed
		}
		if m.Error != nil {
			return m.Error
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.drop(id)
		return ctx.Err()
	}
}

// Send writes a command without waiting for its reply (input events, frame
// acks). Writes are in call order, so a mouse-down sent before a mouse-up
// reaches Chrome first. The reply, when it comes, is dropped.
func (c *CDPConn) Send(session, method string, params interface{}) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return
		}
		raw = b
	}
	id := c.nextID.Add(1)
	c.mu.Lock()
	select {
	case <-c.closed:
		c.mu.Unlock()
		return
	default:
	}
	c.pending[id] = make(chan cdpMessage, 1)
	c.mu.Unlock()
	frame, _ := json.Marshal(cdpMessage{ID: id, SessionID: session, Method: method, Params: raw})
	frame = append(frame, 0)
	c.wmu.Lock()
	_, err := c.w.Write(frame)
	c.wmu.Unlock()
	if err != nil {
		c.drop(id)
		c.shutdown(errCDPClosed)
	}
}

func (c *CDPConn) drop(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
