package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NotificationStore persists the notification feed and each user's read /
// dismissed state. It lives in its own SQLite file under the data path
// (/var/lib/nivaroos/db), not in message-bus.db: that one is in the
// runtime path (/var/run, a tmpfs) and is rebuilt on every boot, which is
// fine for event types but not for a feed that must survive a reboot.
//
// Read state per user is two watermarks plus per-entry rows:
//   - notification_user_marks.read_up_to / dismissed_up_to: "mark all
//     read" and "clear all" move a watermark instead of writing one row
//     per entry;
//   - notification_user_states: one entry read or dismissed on its own.
//
// Entries are shared by all users (every signed-in user already receives
// every event over the socket); only the state is per user.
type NotificationStore struct {
	db *gorm.DB
}

// NotificationStoreFileName is the feed database inside the data path.
const NotificationStoreFileName = "message-bus-notifications.db"

// notificationSchemaVersion is PRAGMA user_version after migrate().
const notificationSchemaVersion = 1

var memoryStoreSeq atomic.Int64

// NewNotificationStore opens (creating and migrating as needed) the feed
// database at path.
func NewNotificationStore(path string) (*NotificationStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	store, err := openNotificationStore(path)
	if err != nil {
		return nil, err
	}
	// Titles can name files and folders: readable by root only.
	_ = os.Chmod(path, 0o600)
	return store, nil
}

// NewNotificationStoreInMemory is a private in-memory store (tests).
func NewNotificationStoreInMemory() (*NotificationStore, error) {
	return openNotificationStore(fmt.Sprintf("file:notifications-%d?mode=memory&cache=shared", memoryStoreSeq.Add(1)))
}

func openNotificationStore(dsn string) (*NotificationStore, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	c, err := db.DB()
	if err != nil {
		return nil, err
	}
	// One connection: SQLite serialises writers anyway, and an in-memory
	// database lives exactly as long as its connection.
	c.SetMaxOpenConns(1)
	c.SetMaxIdleConns(1)
	c.SetConnMaxIdleTime(0)
	c.SetConnMaxLifetime(0)

	s := &NotificationStore{db: db}
	if err := s.migrate(); err != nil {
		c.Close()
		return nil, err
	}
	return s, nil
}

// migrate brings the schema to notificationSchemaVersion. Each step is
// idempotent and runs in a transaction together with the version bump, so
// an interrupted upgrade simply runs again on the next start.
func (s *NotificationStore) migrate() error {
	var version int
	if err := s.db.Raw("PRAGMA user_version").Scan(&version).Error; err != nil {
		return err
	}
	if version > notificationSchemaVersion {
		return fmt.Errorf("notification feed database is schema %d, newer than this message-bus (%d)", version, notificationSchemaVersion)
	}
	steps := []string{
		// 1: the feed, per-entry state, per-user watermarks. AUTOINCREMENT
		// so an id is never handed out twice, even after retention deleted
		// every row: clients poll with ?after=<last id they saw>.
		`CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at INTEGER NOT NULL,
			source_id TEXT NOT NULL DEFAULT '',
			event_name TEXT NOT NULL DEFAULT '',
			event_uuid TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '',
			level TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			msg_key TEXT NOT NULL DEFAULT '',
			args TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT '',
			icon TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON notifications(created_at);
		CREATE TABLE IF NOT EXISTS notification_user_states (
			notification_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			read_at INTEGER NOT NULL DEFAULT 0,
			dismissed_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (notification_id, user_id)
		);
		CREATE INDEX IF NOT EXISTS idx_notification_user_states_user ON notification_user_states(user_id);
		CREATE TABLE IF NOT EXISTS notification_user_marks (
			user_id INTEGER PRIMARY KEY,
			read_up_to INTEGER NOT NULL DEFAULT 0,
			dismissed_up_to INTEGER NOT NULL DEFAULT 0
		);`,
	}
	for v := version; v < len(steps); v++ {
		step := steps[v]
		next := v + 1
		if err := s.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(step).Error; err != nil {
				return err
			}
			return tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", next)).Error
		}); err != nil {
			return fmt.Errorf("notification feed migration %d: %w", next, err)
		}
	}
	return nil
}

func (s *NotificationStore) Close() {
	if c, err := s.db.DB(); err == nil {
		c.Close()
	}
}

// Insert stores n and returns it with its id.
func (s *NotificationStore) Insert(n model.Notification) (model.Notification, error) {
	var id int64
	err := s.db.Raw(`INSERT INTO notifications
		(created_at, source_id, event_name, event_uuid, category, level, title, message, msg_key, args, action, icon)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		n.CreatedAt, n.SourceID, n.EventName, n.EventUUID, n.Category, n.Level, n.Title, n.Message, n.Key, n.Args, n.Action, n.Icon,
	).Scan(&id).Error
	if err != nil {
		return n, err
	}
	if id == 0 {
		return n, errors.New("insert returned no id")
	}
	n.ID = id
	return n, nil
}

type notificationRow struct {
	ID        int64  `gorm:"column:id"`
	CreatedAt int64  `gorm:"column:created_at"`
	SourceID  string `gorm:"column:source_id"`
	EventName string `gorm:"column:event_name"`
	EventUUID string `gorm:"column:event_uuid"`
	Category  string `gorm:"column:category"`
	Level     string `gorm:"column:level"`
	Title     string `gorm:"column:title"`
	Message   string `gorm:"column:message"`
	MsgKey    string `gorm:"column:msg_key"`
	Args      string `gorm:"column:args"`
	Action    string `gorm:"column:action"`
	Icon      string `gorm:"column:icon"`
	IsRead    bool   `gorm:"column:is_read"`
}

type userMarks struct {
	ReadUpTo      int64 `gorm:"column:read_up_to"`
	DismissedUpTo int64 `gorm:"column:dismissed_up_to"`
}

func (s *NotificationStore) marks(db *gorm.DB, userID int) (userMarks, error) {
	var m userMarks
	err := db.Raw("SELECT read_up_to, dismissed_up_to FROM notification_user_marks WHERE user_id = ?", userID).Scan(&m).Error
	return m, err
}

// visible is the WHERE fragment (on n, s) for entries the user hasn't
// dismissed; its args are appended by the caller.
const visibleSQL = `n.id > ? AND COALESCE(s.dismissed_at, 0) = 0`
const unreadSQL = `n.id > ? AND COALESCE(s.read_at, 0) = 0`
const joinSQL = `FROM notifications n LEFT JOIN notification_user_states s ON s.notification_id = n.id AND s.user_id = ?`

// List returns a page of q.UserID's feed, newest first.
func (s *NotificationStore) List(q model.NotificationQuery) (model.NotificationPage, error) {
	page := model.NotificationPage{Items: []model.NotificationView{}}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		m, err := s.marks(tx, q.UserID)
		if err != nil {
			return err
		}
		lower := q.After
		if m.DismissedUpTo > lower {
			lower = m.DismissedUpTo
		}

		where := visibleSQL
		args := []interface{}{m.ReadUpTo, q.UserID, lower}
		if q.Before > 0 {
			where += " AND n.id < ?"
			args = append(args, q.Before)
		}
		if q.UnreadOnly {
			where += " AND " + unreadSQL
			args = append(args, m.ReadUpTo)
		}
		args = append(args, limit+1)

		var rows []notificationRow
		if err := tx.Raw(`SELECT n.*, (n.id <= ? OR COALESCE(s.read_at, 0) > 0) AS is_read `+joinSQL+` WHERE `+where+` ORDER BY n.id DESC LIMIT ?`, args...).
			Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) > limit {
			page.HasMore = true
			rows = rows[:limit]
		}
		for _, r := range rows {
			page.Items = append(page.Items, model.NotificationView{Read: r.IsRead, Notification: model.Notification{
				ID: r.ID, CreatedAt: r.CreatedAt, SourceID: r.SourceID, EventName: r.EventName, EventUUID: r.EventUUID,
				Category: r.Category, Level: r.Level, Title: r.Title, Message: r.Message, Key: r.MsgKey,
				Args: r.Args, Action: r.Action, Icon: r.Icon,
			}})
		}

		var counts struct {
			Unread int64 `gorm:"column:unread"`
			Latest int64 `gorm:"column:latest"`
		}
		if err := tx.Raw(`SELECT
				COALESCE(SUM(CASE WHEN `+unreadSQL+` THEN 1 ELSE 0 END), 0) AS unread,
				COALESCE(MAX(n.id), 0) AS latest `+joinSQL+` WHERE `+visibleSQL,
			m.ReadUpTo, q.UserID, m.DismissedUpTo).Scan(&counts).Error; err != nil {
			return err
		}
		page.UnreadCount, page.LatestID = counts.Unread, counts.Latest
		return nil
	})
	return page, err
}

// UnreadCount is the number of q.UserID's unread, undismissed entries.
func (s *NotificationStore) UnreadCount(userID int) (int64, error) {
	page, err := s.List(model.NotificationQuery{UserID: userID, Limit: 1})
	return page.UnreadCount, err
}

func (s *NotificationStore) setState(userID int, ids []int64, column string, now int64) error {
	if len(ids) == 0 {
		return nil
	}
	// Only ids that exist; the first time wins (read_at keeps the moment
	// it was first read).
	return s.db.Exec(`INSERT INTO notification_user_states (notification_id, user_id, `+column+`)
		SELECT id, ?, ? FROM notifications WHERE id IN ?
		ON CONFLICT (notification_id, user_id) DO UPDATE SET `+column+` = CASE WHEN `+column+` = 0 THEN excluded.`+column+` ELSE `+column+` END`,
		userID, now, ids).Error
}

func (s *NotificationStore) moveMark(userID int, upTo int64, column string) (int64, error) {
	var moved int64
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var maxID int64
		if err := tx.Raw("SELECT COALESCE(MAX(id), 0) FROM notifications").Scan(&maxID).Error; err != nil {
			return err
		}
		// Never past what exists: an entry created after the client
		// loaded its page must still arrive unread.
		if upTo <= 0 || upTo > maxID {
			upTo = maxID
		}
		moved = upTo
		return tx.Exec(`INSERT INTO notification_user_marks (user_id, `+column+`) VALUES (?, ?)
			ON CONFLICT (user_id) DO UPDATE SET `+column+` = MAX(`+column+`, excluded.`+column+`)`, userID, upTo).Error
	})
	return moved, err
}

// MarkRead marks the given entries read for userID.
func (s *NotificationStore) MarkRead(userID int, ids []int64, now time.Time) error {
	return s.setState(userID, ids, "read_at", now.UnixMilli())
}

// MarkAllRead marks every entry up to upTo (0: all that exist) read for
// userID and returns the watermark it applied.
func (s *NotificationStore) MarkAllRead(userID int, upTo int64) (int64, error) {
	return s.moveMark(userID, upTo, "read_up_to")
}

// Dismiss hides the given entries from userID's feed.
func (s *NotificationStore) Dismiss(userID int, ids []int64, now time.Time) error {
	return s.setState(userID, ids, "dismissed_at", now.UnixMilli())
}

// DismissAll hides every entry up to upTo (0: all that exist) from
// userID's feed and returns the watermark it applied.
func (s *NotificationStore) DismissAll(userID int, upTo int64) (int64, error) {
	return s.moveMark(userID, upTo, "dismissed_up_to")
}

// Prune applies retention: entries older than maxAge go, and only the
// newest maxItems are kept. State rows of removed entries go with them.
// It returns how many entries were removed.
func (s *NotificationStore) Prune(now time.Time, maxAge time.Duration, maxItems int) (int64, error) {
	var removed int64
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if maxAge > 0 {
			res := tx.Exec("DELETE FROM notifications WHERE created_at < ?", now.Add(-maxAge).UnixMilli())
			if res.Error != nil {
				return res.Error
			}
			removed += res.RowsAffected
		}
		if maxItems > 0 {
			res := tx.Exec(`DELETE FROM notifications WHERE id < (SELECT id FROM notifications ORDER BY id DESC LIMIT 1 OFFSET ?)`, maxItems-1)
			if res.Error != nil {
				return res.Error
			}
			removed += res.RowsAffected
		}
		if removed > 0 {
			return tx.Exec("DELETE FROM notification_user_states WHERE notification_id NOT IN (SELECT id FROM notifications)").Error
		}
		return nil
	})
	return removed, err
}

// Count is the number of stored entries (tests, diagnostics).
func (s *NotificationStore) Count() (int64, error) {
	var n int64
	err := s.db.Raw("SELECT COUNT(*) FROM notifications").Scan(&n).Error
	return n, err
}
