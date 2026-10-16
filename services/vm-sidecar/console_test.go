package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestParseVNCAddress_ResolvedPort(t *testing.T) {
	xmlDesc := `<domain><devices>
		<graphics type='vnc' port='5901' listen='127.0.0.1'/>
	</devices></domain>`

	addr, err := parseVNCAddress(xmlDesc)
	if err != nil {
		t.Fatalf("parseVNCAddress: %v", err)
	}
	if addr != "127.0.0.1:5901" {
		t.Fatalf("expected 127.0.0.1:5901, got %q", addr)
	}
}

func TestParseVNCAddress_UnresolvedAutoportRejected(t *testing.T) {
	xmlDesc := `<domain><devices>
		<graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1'/>
	</devices></domain>`

	if _, err := parseVNCAddress(xmlDesc); err == nil {
		t.Fatal("expected an error for an unresolved (-1) VNC port, got nil")
	}
}

func TestParseVNCAddress_NoGraphicsDevice(t *testing.T) {
	xmlDesc := `<domain><devices></devices></domain>`
	if _, err := parseVNCAddress(xmlDesc); err == nil {
		t.Fatal("expected an error when there is no graphics device, got nil")
	}
}

func TestParseVNCAddress_DefaultsListenHostWhenEmpty(t *testing.T) {
	xmlDesc := `<domain><devices>
		<graphics type='vnc' port='5902'/>
	</devices></domain>`

	addr, err := parseVNCAddress(xmlDesc)
	if err != nil {
		t.Fatalf("parseVNCAddress: %v", err)
	}
	if addr != "127.0.0.1:5902" {
		t.Fatalf("expected default listen host 127.0.0.1, got %q", addr)
	}
}

func TestVNCAddress_UnstartedVMHasUnresolvedPort(t *testing.T) {
	store := newTestStore(t)
	diskPath := t.TempDir() + "/console-vm.qcow2"
	_, err := store.CreateVM(CreateVMRequest{
		Name:      "console-vm",
		VCPUs:     1,
		MemoryMiB: 256,
		Disks:     []DiskSpec{{Path: diskPath, GiB: 1}},
		Networks:  []NICSpec{{Mode: "nat"}},
	})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteVM("console-vm", true) })

	// The test:///default driver never simulates real QEMU VNC listening,
	// so autoport stays unresolved even though the domain reports
	// running - this exercises the same "not ready yet" rejection a real
	// not-yet-started VM would hit.
	if _, err := vncAddress(store, "console-vm"); err == nil {
		t.Fatal("expected vncAddress to reject an unresolved VNC port, got nil error")
	}
}

// A still screen sends nothing; the proxy must still prove the link is
// alive with (ignorable) text frames, and keep passing RFB bytes as binary.
func TestProxyConsoleHeartbeatOnIdle(t *testing.T) {
	old := consoleHeartbeat
	consoleHeartbeat = 50 * time.Millisecond
	// Restore only after proxyConsole is gone: it reads consoleHeartbeat
	// from a hijacked handler that srv.Close doesn't wait for.
	served := make(chan struct{})
	defer func() {
		select {
		case <-served:
		case <-time.After(5 * time.Second): // the dial failed: no handler ran
		}
		consoleHeartbeat = old
	}()

	vnc, far := net.Pipe()
	defer far.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(served)
		ws, err := consoleUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		proxyConsole(ws, vnc)
	}))
	defer srv.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))

	for i := 0; i < 3; i++ { // one up front, then one per idle interval
		mt, data, err := c.ReadMessage()
		if err != nil || mt != websocket.TextMessage || len(data) != 0 {
			t.Fatalf("idle frame %d: type %d len %d err %v, want an empty text frame", i, mt, len(data), err)
		}
	}
	go far.Write([]byte("RFB 003.008\n"))
	for {
		mt, data, err := c.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if mt == websocket.BinaryMessage {
			if string(data) != "RFB 003.008\n" {
				t.Fatalf("got %q", data)
			}
			return
		}
	}
}

// x11vnc keeps a dropped viewer's keys down: the proxy must let go of
// what the viewer held (here Ctrl and the left button) when it closes.
func TestHostInputTrackerReleasesHeldInputOnClose(t *testing.T) {
	vnc, far := net.Pipe()
	tr := &hostInputTracker{Conn: vnc}
	got := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(far)
		got <- b
	}()
	msgs := [][]byte{
		[]byte("RFB 003.008\n"), {1}, {1}, // version, security None, ClientInit
		{2, 0, 0, 2, 0, 0, 0, 7, 0xff, 0xff, 0xff, 0x21}, // SetEncodings: Tight, DesktopSize
		{6, 0, 0, 0, 0, 0, 0, 2, 'h', 'i'},               // ClientCutText
		{4, 1, 0, 0, 0, 0, 0xff, 0xe3},                   // Ctrl down
		{4, 1, 0, 0, 0, 0, 0, 0x63},                      // c down
		{4, 0, 0, 0, 0, 0, 0, 0x63},                      // c up
		{5, 1, 0, 10, 0, 20},                             // left button down at 10,20
	}
	var sent []byte
	for _, m := range msgs {
		// Split across writes, as a WebSocket may deliver it.
		for _, part := range [][]byte{m[:len(m)/2], m[len(m)/2:]} {
			if _, err := tr.Write(part); err != nil {
				t.Fatal(err)
			}
			sent = append(sent, part...)
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	b := <-got
	want := []byte{4, 0, 0, 0, 0, 0, 0xff, 0xe3, 5, 0, 0, 10, 0, 20}
	if !bytes.Equal(b[len(sent):], want) || !bytes.Equal(b[:len(sent)], sent) {
		t.Fatalf("after the session: % x\nwant % x", b[len(sent):], want)
	}
}

func TestRFBInputGivesUpOnUnknownMessages(t *testing.T) {
	var r rfbInput
	r.feed([]byte("RFB 003.008\n\x01\x01"))
	r.feed([]byte{4, 1, 0, 0, 0, 0, 0xff, 0xe3})
	if rel := r.releases(); len(rel) != 8 {
		t.Fatalf("releases %v", rel)
	}
	r.feed([]byte{4, 1, 0}) // mid-message: nothing may be injected
	if r.releases() != nil {
		t.Fatal("injected mid-message")
	}
	r.feed([]byte{0, 0, 0, 0, 0, 0x41, 99, 1, 2})
	if r.releases() != nil {
		t.Fatal("an unknown message type must stop the tracking")
	}
}
