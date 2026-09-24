package jobs

// Message-bus events (spec §10.1). The service registers EventTypes() with
// the message bus at start, the way other services register theirs, and
// publishes every run transition and job change. The UI receives them over
// socket.io (path /v2/message_bus/socket.io/) as {Properties: {...}} with
// every property a string; ui/src/apps/backup/events.js is generated from
// this file.

// EventSourceID is the message-bus source of every backup event.
const EventSourceID = "nivaroos-backup"

// Event names.
const (
	EventRunBegin    = "nivaroos:backup:run-begin"    // queued -> running
	EventRunProgress = "nivaroos:backup:run-progress" // at most 1/s per run, only when a value changed
	EventRunEnd      = "nivaroos:backup:run-end"      // any final state
	EventRunWaiting  = "nivaroos:backup:run-waiting"  // -> waiting_user
	EventJobChanged  = "nivaroos:backup:job-changed"  // created, updated, deleted, toggled
	// EventNotify is a notification for the desktop's NotificationCenter
	// (spec §10.4). NotificationCenter keeps history in the browser, so
	// the service can only hand notifications to the UI over the bus.
	EventNotify = "nivaroos:backup:notify"
)

// Event properties. All values are strings: numbers in decimal, times
// RFC 3339, absent values "".
const (
	PropRunID       = "run_id"
	PropJobID       = "job_id"
	PropKind        = "kind"   // RunKind
	PropStatus      = "status" // RunStatus
	PropPhase       = "phase"  // RunPhase
	PropErrorCode   = "error_code"
	PropBytes       = "bytes"
	PropTotalBytes  = "total_bytes"
	PropFiles       = "files"
	PropTotalFiles  = "total_files"
	PropSpeedBps    = "speed_bps"
	PropETASec      = "eta_sec" // "" = unknown
	PropErrors      = "errors"
	PropCurrentFile = "current_file"
	PropSummary     = "summary" // EncodeMessage form, run-end only
	PropChange      = "change"  // job-changed: JobChange*
	PropRevision    = "revision"
	// notify
	PropNotifyKey  = "key"     // a NotifyKeys key
	PropNotifyArgs = "args"    // its args, a JSON object (Message conventions)
	PropTitle      = "title"   // English, rendered by the service ("Backup & Sync")
	PropMessage    = "message" // English, rendered by the service from en_US.json
	PropLevel      = "level"   // NotifyLevel*
	PropWindow     = "window"  // JSON {kind, props} for the UI's backupWindow() (windows.js), opened on click
)

// Notification levels (NotificationCenter's status values).
const (
	NotifyLevelInfo    = "info"
	NotifyLevelSuccess = "success"
	NotifyLevelWarning = "warning"
	NotifyLevelError   = "error"
)

// job-changed "change" values.
const (
	JobChangeCreated = "created"
	JobChangeUpdated = "updated"
	JobChangeDeleted = "deleted"
	JobChangeToggled = "toggled"
)

var runProps = []string{PropRunID, PropJobID, PropKind, PropStatus, PropPhase, PropErrorCode}

var progressProps = []string{
	PropRunID, PropJobID, PropKind, PropStatus, PropPhase,
	PropBytes, PropTotalBytes, PropFiles, PropTotalFiles, PropSpeedBps, PropETASec, PropErrors, PropCurrentFile,
}

// EventPropertyType is one property of an event type, in the message
// bus's registration shape.
type EventPropertyType struct {
	Name string `json:"name"`
}

// EventType is one event type, in the message bus's registration shape
// (POST /v2/message_bus/event_type takes a JSON array of these).
type EventType struct {
	SourceID         string              `json:"sourceID"`
	Name             string              `json:"name"`
	PropertyTypeList []EventPropertyType `json:"propertyTypeList"`
}

// EventTypes returns every backup event type with its properties.
func EventTypes() []EventType {
	mk := func(name string, props []string) EventType {
		list := make([]EventPropertyType, len(props))
		for i, p := range props {
			list[i] = EventPropertyType{Name: p}
		}
		return EventType{SourceID: EventSourceID, Name: name, PropertyTypeList: list}
	}
	return []EventType{
		mk(EventRunBegin, runProps),
		mk(EventRunProgress, progressProps),
		mk(EventRunEnd, append(append([]string{}, progressProps...), PropErrorCode, PropSummary)),
		mk(EventRunWaiting, runProps),
		mk(EventJobChanged, []string{PropJobID, PropChange, PropRevision}),
		mk(EventNotify, []string{PropNotifyKey, PropNotifyArgs, PropTitle, PropMessage, PropLevel, PropJobID, PropRunID, PropWindow}),
	}
}
