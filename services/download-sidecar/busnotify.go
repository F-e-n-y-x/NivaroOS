package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/localtoken"
)

// Finished and failed downloads in the NivaroOS notification feed
//
// Each Event is also published on the message bus as a ":notify" event
// (the feed's contract, services/message-bus/service/notification_classify.go),
// so it lands in the web UI's Notification Center and on the phone even
// when no Download Station window was open. The action says what opening
// it shows: {"target":"download","props":{"kind":"download"|"torrent","id":..}}.

const (
	busSourceID    = "nivaroos-download"
	busNotifyEvent = "nivaroos-download:download:notify"
)

var busNotifyProps = []string{"title", "message", "level", "category", "key", "args", "action"}

// notifyProps is the bus event for e.
func notifyProps(e Event) map[string]string {
	kind := "download"
	if e.Torrent {
		kind = "torrent"
	}
	title, level, key, msg := "Download finished", "success", "notify.download.done", e.Filename
	if e.Torrent {
		title = "Torrent finished"
	}
	if e.Kind == "failed" {
		title, level, key = "Download failed", "error", "notify.download.failed"
		if e.Torrent {
			title = "Torrent failed"
		}
		if e.Message != "" {
			msg = e.Filename + ": " + e.Message
		}
	}
	args, _ := json.Marshal(map[string]string{"kind": kind, "id": e.ID, "filename": e.Filename, "path": e.Path, "error": e.Message})
	action, _ := json.Marshal(map[string]interface{}{"target": "download", "props": map[string]string{"kind": kind, "id": e.ID}})
	return map[string]string{
		"title": title, "message": msg, "level": level, "category": "system",
		"key": key, "args": string(args), "action": string(action),
	}
}

// busNotifier publishes each event in the background. The bus keeps its
// event types in /run, so the type is registered (an upsert) before every
// publish - downloads finish rarely enough for that to cost nothing.
func busNotifier(runtimePath string) func(Event) {
	client := &http.Client{Timeout: 10 * time.Second}
	post := func(u string, body interface{}) error {
		raw, _ := json.Marshal(body)
		req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		localtoken.Add(req)
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode/100 != 2 {
			return fmt.Errorf("%s: %s", u, res.Status)
		}
		return nil
	}
	return func(e Event) {
		go func() {
			raw, err := os.ReadFile(filepath.Join(runtimePath, "message-bus.url"))
			if err != nil {
				log.Printf("notification feed: %v", err)
				return
			}
			base := strings.TrimRight(strings.TrimSpace(string(raw)), "/")
			if u, err := url.Parse(base); err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
				log.Printf("notification feed: message-bus.url %q is not a loopback http address", base)
				return
			}
			base += "/v2/message_bus"
			props := make([]map[string]string, 0, len(busNotifyProps))
			for _, n := range busNotifyProps {
				props = append(props, map[string]string{"name": n})
			}
			types := []map[string]interface{}{{"sourceID": busSourceID, "name": busNotifyEvent, "propertyTypeList": props}}
			if err := post(base+"/event_type", types); err != nil {
				log.Printf("notification feed: registering: %v", err)
				return
			}
			if err := post(base+"/event/"+busSourceID+"/"+url.PathEscape(busNotifyEvent), notifyProps(e)); err != nil {
				log.Printf("notification feed: publishing: %v", err)
			}
		}()
	}
}
