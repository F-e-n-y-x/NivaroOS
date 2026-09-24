package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"sync"
	"time"
)

// publisher sends message-bus events without ever blocking a run: events
// go into a bounded queue that one goroutine drains. Until the event types
// are registered (the bus may start after us) events wait in the queue;
// when it is full the oldest progress events are the ones that get lost,
// because a later one replaces them anyway.
type publisher struct {
	bus Bus
	ch  chan busEvent

	mu         sync.Mutex
	registered bool
	// warned: the bus being down is logged once, not on every retry.
	warned bool
}

type busEvent struct {
	name  string
	props map[string]string
}

const publishQueue = 512

func newPublisher(bus Bus) *publisher {
	return &publisher{bus: bus, ch: make(chan busEvent, publishQueue)}
}

// publish queues an event; it drops a progress event rather than wait
// when the queue is full, and drops the oldest queued event for anything
// else.
func (p *publisher) publish(name string, props map[string]string) {
	if p == nil || p.bus == nil {
		return
	}
	ev := busEvent{name: name, props: props}
	select {
	case p.ch <- ev:
		return
	default:
	}
	if name == EventRunProgress {
		return
	}
	select {
	case <-p.ch:
	default:
	}
	select {
	case p.ch <- ev:
	default:
	}
}

// run registers the event types (retrying with backoff while the bus is
// down) and then delivers queued events until ctx ends.
func (p *publisher) run(ctx context.Context) {
	if p == nil || p.bus == nil {
		return
	}
	backoff := 2 * time.Second
	for !p.ensureRegistered(ctx) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-p.ch:
			p.deliver(ctx, ev)
		}
	}
}

func (p *publisher) ensureRegistered(ctx context.Context) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.registered {
		return true
	}
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := p.bus.RegisterEventTypes(rctx, EventTypes()); err != nil {
		if !p.warned {
			log.Printf("backup: registering message-bus event types: %v (retrying in the background)", err)
			p.warned = true
		}
		return false
	}
	if p.warned {
		log.Printf("backup: message-bus event types registered")
	}
	p.registered, p.warned = true, false
	return true
}

func (p *publisher) deliver(ctx context.Context, ev busEvent) {
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := p.bus.Publish(pctx, ev.name, ev.props)
	var se *httpStatusError
	if errors.As(err, &se) && se.Status == 404 {
		// The bus restarted and forgot our types (its store is in /run):
		// register again and retry once.
		p.mu.Lock()
		p.registered = false
		p.mu.Unlock()
		if p.ensureRegistered(ctx) {
			err = p.bus.Publish(pctx, ev.name, ev.props)
		}
	}
	if err != nil && ev.name != EventRunProgress {
		log.Printf("backup: publish %s: %v", ev.name, err)
	}
}

// ---------------------------------------------------------------------
// Event payloads (every property a string, spec §10.1)

func runEventProps(r RunRow) map[string]string {
	return map[string]string{
		PropRunID: r.ID, PropJobID: r.JobID, PropKind: r.Kind, PropStatus: r.Status,
		PropPhase: r.Phase, PropErrorCode: r.ErrorCode,
	}
}

func progressEventProps(r RunRow, st LiveStats) map[string]string {
	props := runEventProps(r)
	delete(props, PropErrorCode)
	props[PropBytes] = strconv.FormatInt(st.Bytes, 10)
	props[PropTotalBytes] = strconv.FormatInt(st.TotalBytes, 10)
	props[PropFiles] = strconv.FormatInt(st.Files, 10)
	props[PropTotalFiles] = strconv.FormatInt(st.TotalFiles, 10)
	props[PropSpeedBps] = strconv.FormatInt(st.SpeedBps, 10)
	props[PropETASec] = ""
	if st.ETASec != nil {
		props[PropETASec] = strconv.FormatInt(*st.ETASec, 10)
	}
	props[PropErrors] = strconv.FormatInt(st.Errors, 10)
	props[PropCurrentFile] = st.CurrentFile
	return props
}

func endProps(r RunRow, st LiveStats) map[string]string {
	props := progressEventProps(r, st)
	props[PropErrorCode] = r.ErrorCode
	props[PropSummary] = r.Summary
	return props
}

// ---------------------------------------------------------------------
// Notifications (spec §10.4)

// Notification is one NotificationCenter entry, as sent on EventNotify.
type Notification struct {
	Message Message
	Level   string
	JobID   string
	RunID   string
	// WindowKind / WindowProps say what clicking it opens: a
	// ui/src/apps/backup/windows.js kind ("app", "run", "preview") and
	// its props.
	WindowKind  string
	WindowProps map[string]interface{}
}

func (n Notification) props() map[string]string {
	args := n.Message.Args
	if args == nil {
		args = map[string]interface{}{}
	}
	rawArgs, _ := json.Marshal(args)
	win := map[string]interface{}{"kind": n.WindowKind, "props": n.WindowProps}
	if n.WindowKind == "" {
		win = map[string]interface{}{"kind": "app", "props": map[string]interface{}{"section": "activity"}}
	}
	rawWin, _ := json.Marshal(win)
	return map[string]string{
		PropNotifyKey:  n.Message.Key,
		PropNotifyArgs: string(rawArgs),
		PropTitle:      RenderEnglish(Message{Key: "backup.app.title"}),
		PropMessage:    RenderEnglish(n.Message),
		PropLevel:      n.Level,
		PropJobID:      n.JobID,
		PropRunID:      n.RunID,
		PropWindow:     string(rawWin),
	}
}

// activityWindow opens BackupApp on Activity with the run selected.
func activityWindow(runID string) (string, map[string]interface{}) {
	props := map[string]interface{}{"section": "activity"}
	if runID != "" {
		props["runId"] = runID
	}
	return "app", props
}

// errorReasonKey is the reason_key arg for an error code.
func errorReasonKey(code ErrorCode) string {
	if code == "" {
		code = ErrInternal
	}
	return "backup.err." + string(code) + ".title"
}

// guardKey is the guard_key arg for a tripped guard name.
func guardKey(guard string) string {
	switch guard {
	case "delete", "change", "empty_source":
		return "backup.guard." + guard
	}
	return "backup.guard.change"
}
