package repository

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"gotest.tools/assert"
)

func newTestStore(t *testing.T) *NotificationStore {
	t.Helper()
	s, err := NewNotificationStoreInMemory()
	assert.NilError(t, err)
	t.Cleanup(s.Close)
	return s
}

func insertN(t *testing.T, s *NotificationStore, count int, at time.Time) []int64 {
	t.Helper()
	ids := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		n, err := s.Insert(model.Notification{CreatedAt: at.UnixMilli(), SourceID: "src", EventName: "src:notify", Title: "t", Level: "info", Category: "system"})
		assert.NilError(t, err)
		ids = append(ids, n.ID)
	}
	return ids
}

func pageIDs(p model.NotificationPage) []int64 {
	out := []int64{}
	for _, it := range p.Items {
		out = append(out, it.ID)
	}
	return out
}

func TestNotificationStoreMigrationIsIdempotentAndPersistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db", NotificationStoreFileName)

	s, err := NewNotificationStore(path)
	assert.NilError(t, err)
	_, err = s.Insert(model.Notification{CreatedAt: time.Now().UnixMilli(), Title: "kept across restarts"})
	assert.NilError(t, err)
	s.Close()

	info, err := os.Stat(path)
	assert.NilError(t, err)
	assert.Equal(t, info.Mode().Perm(), os.FileMode(0o600))

	// Second open (a service restart / an update): migration is a no-op
	// and the data is still there.
	s, err = NewNotificationStore(path)
	assert.NilError(t, err)
	defer s.Close()
	var version int
	assert.NilError(t, s.db.Raw("PRAGMA user_version").Scan(&version).Error)
	assert.Equal(t, version, notificationSchemaVersion)
	page, err := s.List(model.NotificationQuery{UserID: 1})
	assert.NilError(t, err)
	assert.Equal(t, len(page.Items), 1)
	assert.Equal(t, page.Items[0].Title, "kept across restarts")
}

func TestNotificationStoreRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), NotificationStoreFileName)
	s, err := NewNotificationStore(path)
	assert.NilError(t, err)
	assert.NilError(t, s.db.Exec("PRAGMA user_version = 99").Error)
	s.Close()

	_, err = NewNotificationStore(path)
	assert.ErrorContains(t, err, "newer")
}

func TestNotificationStoreListPaging(t *testing.T) {
	s := newTestStore(t)
	ids := insertN(t, s, 5, time.Now())

	page, err := s.List(model.NotificationQuery{UserID: 1, Limit: 2})
	assert.NilError(t, err)
	assert.DeepEqual(t, pageIDs(page), []int64{ids[4], ids[3]})
	assert.Assert(t, page.HasMore)
	assert.Equal(t, page.UnreadCount, int64(5))
	assert.Equal(t, page.LatestID, ids[4])

	page, err = s.List(model.NotificationQuery{UserID: 1, Limit: 2, Before: ids[3]})
	assert.NilError(t, err)
	assert.DeepEqual(t, pageIDs(page), []int64{ids[2], ids[1]})
	assert.Assert(t, page.HasMore)

	page, err = s.List(model.NotificationQuery{UserID: 1, Limit: 2, Before: ids[1]})
	assert.NilError(t, err)
	assert.DeepEqual(t, pageIDs(page), []int64{ids[0]})
	assert.Assert(t, !page.HasMore)

	// Polling: only what is newer than the last id seen.
	page, err = s.List(model.NotificationQuery{UserID: 1, Limit: 50, After: ids[2]})
	assert.NilError(t, err)
	assert.DeepEqual(t, pageIDs(page), []int64{ids[4], ids[3]})
	assert.Assert(t, !page.HasMore)
}

func TestNotificationStoreReadStateIsPerUser(t *testing.T) {
	s := newTestStore(t)
	ids := insertN(t, s, 3, time.Now())

	assert.NilError(t, s.MarkRead(1, []int64{ids[1], 999}, time.Now()))
	// Marking twice is harmless.
	assert.NilError(t, s.MarkRead(1, []int64{ids[1]}, time.Now()))

	p1, err := s.List(model.NotificationQuery{UserID: 1})
	assert.NilError(t, err)
	assert.Equal(t, p1.UnreadCount, int64(2))
	assert.Assert(t, !p1.Items[0].Read)
	assert.Assert(t, p1.Items[1].Read)

	p2, err := s.List(model.NotificationQuery{UserID: 2})
	assert.NilError(t, err)
	assert.Equal(t, p2.UnreadCount, int64(3))

	unread, err := s.List(model.NotificationQuery{UserID: 1, UnreadOnly: true})
	assert.NilError(t, err)
	assert.DeepEqual(t, pageIDs(unread), []int64{ids[2], ids[0]})

	// No state row for an id that doesn't exist.
	var rows int64
	assert.NilError(t, s.db.Raw("SELECT COUNT(*) FROM notification_user_states").Scan(&rows).Error)
	assert.Equal(t, rows, int64(1))
}

func TestNotificationStoreMarkAllReadDoesNotCoverLaterEntries(t *testing.T) {
	s := newTestStore(t)
	ids := insertN(t, s, 3, time.Now())

	// A client that loaded up to ids[1] marks "all" read up to what it saw.
	applied, err := s.MarkAllRead(1, ids[1])
	assert.NilError(t, err)
	assert.Equal(t, applied, ids[1])
	p, err := s.List(model.NotificationQuery{UserID: 1})
	assert.NilError(t, err)
	assert.Equal(t, p.UnreadCount, int64(1))

	// up_to beyond what exists is capped, so a future entry arrives unread.
	applied, err = s.MarkAllRead(1, ids[2]+100)
	assert.NilError(t, err)
	assert.Equal(t, applied, ids[2])
	later := insertN(t, s, 1, time.Now())
	p, err = s.List(model.NotificationQuery{UserID: 1})
	assert.NilError(t, err)
	assert.Equal(t, p.UnreadCount, int64(1))
	assert.Equal(t, p.Items[0].ID, later[0])
	assert.Assert(t, !p.Items[0].Read)

	// The watermark never moves back.
	_, err = s.MarkAllRead(1, ids[0])
	assert.NilError(t, err)
	p, err = s.List(model.NotificationQuery{UserID: 1})
	assert.NilError(t, err)
	assert.Equal(t, p.UnreadCount, int64(1))
}

func TestNotificationStoreDismiss(t *testing.T) {
	s := newTestStore(t)
	ids := insertN(t, s, 4, time.Now())

	assert.NilError(t, s.Dismiss(1, []int64{ids[3]}, time.Now()))
	p, err := s.List(model.NotificationQuery{UserID: 1})
	assert.NilError(t, err)
	assert.DeepEqual(t, pageIDs(p), []int64{ids[2], ids[1], ids[0]})
	assert.Equal(t, p.UnreadCount, int64(3))
	assert.Equal(t, p.LatestID, ids[2])

	_, err = s.DismissAll(1, ids[1])
	assert.NilError(t, err)
	p, err = s.List(model.NotificationQuery{UserID: 1})
	assert.NilError(t, err)
	assert.DeepEqual(t, pageIDs(p), []int64{ids[2]})

	_, err = s.DismissAll(1, 0)
	assert.NilError(t, err)
	p, err = s.List(model.NotificationQuery{UserID: 1})
	assert.NilError(t, err)
	assert.Equal(t, len(p.Items), 0)
	assert.Equal(t, p.UnreadCount, int64(0))
	assert.Equal(t, p.LatestID, int64(0))

	// Other users are unaffected.
	p, err = s.List(model.NotificationQuery{UserID: 2})
	assert.NilError(t, err)
	assert.Equal(t, len(p.Items), 4)

	// Entries after "clear all" still show up.
	later := insertN(t, s, 1, time.Now())
	p, err = s.List(model.NotificationQuery{UserID: 1})
	assert.NilError(t, err)
	assert.DeepEqual(t, pageIDs(p), later)
}

func TestNotificationStoreRetention(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	old := insertN(t, s, 3, now.Add(-31*24*time.Hour))
	recent := insertN(t, s, 5, now)
	assert.NilError(t, s.MarkRead(1, []int64{old[0], recent[0]}, now))

	removed, err := s.Prune(now, 30*24*time.Hour, 3)
	assert.NilError(t, err)
	assert.Equal(t, removed, int64(5)) // 3 by age, then 2 by count
	p, err := s.List(model.NotificationQuery{UserID: 1})
	assert.NilError(t, err)
	assert.DeepEqual(t, pageIDs(p), []int64{recent[4], recent[3], recent[2]})

	// State rows of removed entries went with them.
	var rows int64
	assert.NilError(t, s.db.Raw("SELECT COUNT(*) FROM notification_user_states").Scan(&rows).Error)
	assert.Equal(t, rows, int64(0))

	// Nothing to do: nothing removed.
	removed, err = s.Prune(now, 30*24*time.Hour, 3)
	assert.NilError(t, err)
	assert.Equal(t, removed, int64(0))
}

func TestNotificationStoreIDsNeverRepeat(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	first := insertN(t, s, 3, now.Add(-40*24*time.Hour))
	_, err := s.Prune(now, 30*24*time.Hour, 1000)
	assert.NilError(t, err)
	n, err := s.Count()
	assert.NilError(t, err)
	assert.Equal(t, n, int64(0))

	// A client polling with after=<last id> must still see the next one.
	next := insertN(t, s, 1, now)
	assert.Assert(t, next[0] > first[2], "id %d reused after %d", next[0], first[2])
	p, err := s.List(model.NotificationQuery{UserID: 1, After: first[2]})
	assert.NilError(t, err)
	assert.DeepEqual(t, pageIDs(p), next)
}
