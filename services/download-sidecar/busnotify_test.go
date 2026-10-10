package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifyProps(t *testing.T) {
	p := notifyProps(Event{Kind: "completed", ID: "abc", Filename: "a.iso", Path: "/DATA/Downloads/a.iso"})
	if p["title"] != "Download finished" || p["level"] != "success" || p["message"] != "a.iso" {
		t.Fatalf("completed: %v", p)
	}
	var action struct {
		Target string            `json:"target"`
		Props  map[string]string `json:"props"`
	}
	if json.Unmarshal([]byte(p["action"]), &action) != nil || action.Target != "download" || action.Props["kind"] != "download" || action.Props["id"] != "abc" {
		t.Fatalf("action %q", p["action"])
	}
	p = notifyProps(Event{Kind: "failed", ID: "x", Filename: "b.zip", Message: "404"})
	if p["title"] != "Download failed" || p["level"] != "error" || p["message"] != "b.zip: 404" {
		t.Fatalf("failed: %v", p)
	}
	p = notifyProps(Event{Kind: "completed", ID: "hash", Filename: "linux", Torrent: true})
	if p["title"] != "Torrent finished" || !json.Valid([]byte(p["action"])) || json.Unmarshal([]byte(p["action"]), &action) != nil || action.Props["kind"] != "torrent" {
		t.Fatalf("torrent: %v", p)
	}
}

// A pushed event reaches a (fake) message bus: type registered, then published.
func TestBusNotifierPublishes(t *testing.T) {
	got := make(chan string, 4)
	bus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- r.URL.Path + " " + string(body)
	}))
	defer bus.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "message-bus.url"), []byte(bus.URL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := newEventLog(10)
	l.notify = busNotifier(dir)
	l.Push(Event{Kind: "completed", ID: "d1", Filename: "f.bin"})
	want := []string{"/v2/message_bus/event_type", "/v2/message_bus/event/nivaroos-download/nivaroos-download:download:notify"}
	for _, w := range want {
		select {
		case s := <-got:
			if len(s) < len(w) || s[:len(w)] != w {
				t.Fatalf("got %q, want %s ...", s, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no request for %s", w)
		}
	}
}
