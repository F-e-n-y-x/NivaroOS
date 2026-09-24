package model

// Notification is one entry of the persisted notification feed: an event
// worth telling a person about, kept so that a device that wasn't
// connected when it happened (a phone, a browser opened later) still sees
// it. See service/notification_classify.go for which events become one.
//
// IDs only ever grow (SQLite AUTOINCREMENT), even after retention removed
// the newest rows, so `?after=<id>` is a safe polling cursor.
type Notification struct {
	ID        int64
	CreatedAt int64 // unix milliseconds
	SourceID  string
	EventName string
	EventUUID string
	Category  string // NotificationCategory*
	Level     string // NotificationLevel*
	Title     string // English, rendered by the producer
	Message   string // English, rendered by the producer
	Key       string // i18n key the UI renders instead of Message when it knows it
	Args      string // JSON object: arguments for Key
	Action    string // JSON object: what opening it does (see NotificationAction docs)
	Icon      string
}

// NotificationView is a Notification as one user sees it.
type NotificationView struct {
	Notification
	Read bool
}

// NotificationPage is one page of a user's feed, newest first.
type NotificationPage struct {
	Items       []NotificationView
	UnreadCount int64
	LatestID    int64 // newest notification this user can see (0: none)
	HasMore     bool  // more matching entries exist, older than the last item
}

// NotificationQuery selects a page of a user's feed.
type NotificationQuery struct {
	UserID     int
	After      int64 // only entries with id > After
	Before     int64 // only entries with id < Before (0: no bound)
	Limit      int
	UnreadOnly bool
}

const (
	NotificationLevelInfo    = "info"
	NotificationLevelSuccess = "success"
	NotificationLevelWarning = "warning"
	NotificationLevelError   = "error"

	NotificationCategoryBackup  = "backup"
	NotificationCategoryApp     = "app"
	NotificationCategoryStorage = "storage"
	NotificationCategorySystem  = "system"
)
