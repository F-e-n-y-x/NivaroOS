package service

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/repository"
	"go.uber.org/zap/zapcore"
	"gotest.tools/assert"
)

func backupNotifyEvent(level, message string) model.Event {
	return model.Event{SourceID: "nivaroos-backup", Name: "nivaroos:backup:notify", UUID: "u-1", Properties: map[string]string{
		"key":     "backup.notify.failed",
		"args":    `{"job":"Documents","reason_key":"backup.err.cloud_auth.title"}`,
		"title":   "Backup & Sync",
		"message": message,
		"level":   level,
		"job_id":  "j1",
		"run_id":  "r1",
		"window":  `{"kind":"app","props":{"section":"activity","runId":"r1"}}`,
	}}
}

func TestClassifyNotification(t *testing.T) {
	cases := []struct {
		name  string
		event model.Event
		want  *model.Notification
	}{
		{
			name:  "backup notify",
			event: backupNotifyEvent("error", "Documents failed: sign-in expired."),
			want: &model.Notification{
				SourceID: "nivaroos-backup", EventName: "nivaroos:backup:notify", EventUUID: "u-1",
				Category: "backup", Level: "error", Title: "Backup & Sync", Message: "Documents failed: sign-in expired.",
				Key: "backup.notify.failed", Args: `{"job":"Documents","reason_key":"backup.err.cloud_auth.title"}`,
				Action: `{"target":"backup","window":{"kind":"app","props":{"runId":"r1","section":"activity"}}}`,
			},
		},
		{
			name: "any :notify with a title, unknown level and category normalised",
			event: model.Event{SourceID: "nivaroos-download", Name: "nivaroos:download:notify", Properties: map[string]string{
				"title": "Download complete", "message": "big.iso", "level": "WARN", "category": "weird", "action": `{"target":"downloads"}`, "args": "not json",
			}},
			want: &model.Notification{
				SourceID: "nivaroos-download", EventName: "nivaroos:download:notify", Category: "system", Level: "warning",
				Title: "Download complete", Message: "big.iso", Action: `{"target":"downloads"}`,
			},
		},
		{
			name:  ":notify without a title is not one",
			event: model.Event{SourceID: "x", Name: "x:notify", Properties: map[string]string{"message": "m"}},
		},
		{
			name: "app installed",
			event: model.Event{SourceID: "app-management", Name: "app:install-end", Properties: map[string]string{
				"app:name": "immich", "app:icon": "https://i/icon.png", "app:title": `{"en_us":"Immich"}`,
			}},
			want: &model.Notification{
				SourceID: "app-management", EventName: "app:install-end", Category: "app", Level: "success",
				Title: "App installed", Message: "Immich", Key: "notify.app.installed",
				Args: `{"app":"Immich","app_title":"{\"en_us\":\"Immich\"}"}`, Icon: "https://i/icon.png",
			},
		},
		{
			name: "app install failed carries the error",
			event: model.Event{SourceID: "app-management", Name: "app:install-error", Properties: map[string]string{
				"app:name": "immich", "message": "port 2283 in use",
			}},
			want: &model.Notification{
				SourceID: "app-management", EventName: "app:install-error", Category: "app", Level: "error",
				Title: "App installation failed", Message: "immich: port 2283 in use", Key: "notify.app.install_failed",
				Args: `{"app":"immich","error":"port 2283 in use"}`,
			},
		},
		{
			name:  "update that changed nothing is not one",
			event: model.Event{SourceID: "app-management", Name: "app:update-end", Properties: map[string]string{"app:name": "a", "docker:image:updated": "false"}},
		},
		{
			name:  "successful start is not one",
			event: model.Event{SourceID: "app-management", Name: "app:start-end", Properties: map[string]string{"app:name": "a"}},
		},
		{
			name:  "install progress is not one",
			event: model.Event{SourceID: "app-management", Name: "app:install-progress", Properties: map[string]string{"app:name": "a"}},
		},
		{
			name: "format finished",
			event: model.Event{SourceID: "local-storage", Name: "local-storage:storage-job:end", Properties: map[string]string{
				"local-storage:job_kind": "format", "local-storage:path": "/dev/sdb", "local-storage:mount_point": "/media/sdb",
			}},
			want: &model.Notification{
				SourceID: "local-storage", EventName: "local-storage:storage-job:end", Category: "storage", Level: "success",
				Title: "Formatting finished", Message: "/dev/sdb → /media/sdb", Key: "notify.storage.job_done",
				Args: `{"kind":"format","mount_point":"/media/sdb","path":"/dev/sdb"}`, Action: `{"target":"settings","props":{"section":"storage"}}`,
			},
		},
		{
			name: "storage creation failed",
			event: model.Event{SourceID: "local-storage", Name: "local-storage:storage-job:error", Properties: map[string]string{
				"local-storage:job_kind": "create", "local-storage:path": "/dev/sdc", "local-storage:message": "disk busy",
			}},
			want: &model.Notification{
				SourceID: "local-storage", EventName: "local-storage:storage-job:error", Category: "storage", Level: "error",
				Title: "Storage setup failed", Message: "/dev/sdc: disk busy", Key: "notify.storage.job_failed",
				Args: `{"error":"disk busy","kind":"create","mount_point":"","path":"/dev/sdc"}`, Action: `{"target":"settings","props":{"section":"storage"}}`,
			},
		},
		{name: "utilization", event: model.Event{SourceID: "nivaroos", Name: "nivaroos:system:utilization", Properties: map[string]string{"title": "x"}}},
		{name: "file operation", event: model.Event{SourceID: "nivaroos", Name: "nivaroos:file:operate"}},
		{name: "disk hot-plug", event: model.Event{SourceID: "local-storage", Name: "local-storage:disk:added"}},
		{name: "storage job progress", event: model.Event{SourceID: "local-storage", Name: "local-storage:storage-job:progress"}},
		{name: "backup run end", event: model.Event{SourceID: "nivaroos-backup", Name: "nivaroos:backup:run-end", Properties: map[string]string{"status": "failed"}}},
		{name: "the feed's own events", event: model.Event{SourceID: "message-bus", Name: "message-bus:foo:notify", Properties: map[string]string{"title": "loop"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ClassifyNotification(tc.event)
			if tc.want == nil {
				assert.Assert(t, !ok, "classified as a notification: %+v", got)
				return
			}
			assert.Assert(t, ok)
			assert.DeepEqual(t, got, *tc.want)
		})
	}
}

func TestClassifyNotificationCapsSizes(t *testing.T) {
	long := strings.Repeat("é", 5000)
	n, ok := ClassifyNotification(model.Event{SourceID: "nivaroos-backup", Name: "nivaroos:backup:notify", Properties: map[string]string{
		"title": long, "message": long, "args": `{"a":"` + strings.Repeat("x", 9000) + `"}`,
	}})
	assert.Assert(t, ok)
	assert.Equal(t, len([]rune(n.Title)), notifyTitleMax)
	assert.Equal(t, len([]rune(n.Message)), notifyMessageMax)
	assert.Equal(t, n.Args, "")
}

type recorder struct {
	mu     sync.Mutex
	events []model.Event
}

func (r *recorder) publish(e model.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) named(name string) []model.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.Event
	for _, e := range r.events {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out
}

func newTestNotificationService(t *testing.T) (*NotificationService, *recorder, *time.Time) {
	t.Helper()
	logger.LogInitWithWriterSyncers(zapcore.AddSync(io.Discard))
	store, err := repository.NewNotificationStoreInMemory()
	assert.NilError(t, err)
	t.Cleanup(store.Close)
	rec := &recorder{}
	svc := NewNotificationService(store, rec.publish)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	return svc, rec, &now
}

func TestNotificationServiceIngest(t *testing.T) {
	svc, rec, now := newTestNotificationService(t)

	_, ok := svc.Ingest(model.Event{SourceID: "nivaroos", Name: "nivaroos:system:utilization"})
	assert.Assert(t, !ok)

	stored, ok := svc.Ingest(backupNotifyEvent("error", "Documents failed"))
	assert.Assert(t, ok)
	assert.Assert(t, stored.ID > 0)

	// The same thing reported again right away is not news...
	_, ok = svc.Ingest(backupNotifyEvent("error", "Documents failed"))
	assert.Assert(t, !ok)
	// ...but it is once the dedup window passed, and a different one is.
	_, ok = svc.Ingest(backupNotifyEvent("error", "Photos failed"))
	assert.Assert(t, ok)
	*now = now.Add(3 * time.Second)
	_, ok = svc.Ingest(backupNotifyEvent("error", "Documents failed"))
	assert.Assert(t, ok)

	created := rec.named(NotificationCreatedEvent)
	assert.Equal(t, len(created), 3)
	first := created[0]
	assert.Equal(t, first.SourceID, "message-bus")
	assert.Equal(t, first.Properties["id"], strconv.FormatInt(stored.ID, 10))
	assert.Equal(t, first.Properties["title"], "Backup & Sync")
	assert.Equal(t, first.Properties["level"], "error")
	assert.Equal(t, first.Properties["time"], "2026-09-24T12:00:00Z")
	var action map[string]interface{}
	assert.NilError(t, json.Unmarshal([]byte(first.Properties["action"]), &action))
	assert.Equal(t, action["target"], "backup")

	page, err := svc.List(model.NotificationQuery{UserID: 7, Limit: 10})
	assert.NilError(t, err)
	assert.Equal(t, len(page.Items), 3)
	assert.Equal(t, page.UnreadCount, int64(3))
}

func TestNotificationServiceStateEvents(t *testing.T) {
	svc, rec, _ := newTestNotificationService(t)
	a, _ := svc.Ingest(backupNotifyEvent("error", "a"))
	b, _ := svc.Ingest(backupNotifyEvent("error", "b"))

	unread, err := svc.MarkRead(3, []int64{a.ID}, false, 0)
	assert.NilError(t, err)
	assert.Equal(t, unread, int64(1))

	unread, err = svc.MarkRead(3, nil, true, 0)
	assert.NilError(t, err)
	assert.Equal(t, unread, int64(0))

	unread, err = svc.Dismiss(4, []int64{b.ID}, false, 0)
	assert.NilError(t, err)
	assert.Equal(t, unread, int64(1))

	states := rec.named(NotificationStateEvent)
	assert.Equal(t, len(states), 3)
	assert.DeepEqual(t, states[0].Properties, map[string]string{"user_id": "3", "change": "read", "ids": strconv.FormatInt(a.ID, 10), "up_to": ""})
	assert.DeepEqual(t, states[1].Properties, map[string]string{"user_id": "3", "change": "read_all", "ids": "", "up_to": strconv.FormatInt(b.ID, 10)})
	assert.Equal(t, states[2].Properties["change"], "dismiss")
}

func TestNotificationServiceKeepsAtMostMaxItems(t *testing.T) {
	svc, _, now := newTestNotificationService(t)
	svc.sourceBurst = 0 // the retention cap, not the per-source limit
	for i := 0; i < NotificationMaxItems+5; i++ {
		_, ok := svc.Ingest(model.Event{SourceID: "nivaroos-backup", Name: "nivaroos:backup:notify", Properties: map[string]string{"title": "n" + strconv.Itoa(i)}})
		assert.Assert(t, ok)
	}
	count, err := svc.store.Count()
	assert.NilError(t, err)
	assert.Equal(t, count, int64(NotificationMaxItems))

	// The age limit is applied by the periodic pass.
	*now = now.Add(NotificationMaxAge + time.Hour)
	svc.prune()
	count, err = svc.store.Count()
	assert.NilError(t, err)
	assert.Equal(t, count, int64(0))
}

func TestNotificationServiceRegistersItsEventTypes(t *testing.T) {
	repo, err := repository.NewDatabaseRepositoryInMemory()
	assert.NilError(t, err)
	defer repo.Close()
	types := NewEventTypeService(&repo)
	svc, _, _ := newTestNotificationService(t)
	assert.NilError(t, svc.RegisterEventTypes(types))
	for _, name := range []string{NotificationCreatedEvent, NotificationStateEvent} {
		et, err := types.GetEventType("message-bus", name)
		assert.NilError(t, err)
		assert.Equal(t, et.Name, name)
	}
}

// Review finding 6: any publisher could plant persistent notifications
// under any source, with a remote icon every viewer's browser loads, and
// flood the feed until the real entries were evicted.
func TestNotifyOnlyFromNivaroOSSourcesWithLocalIcons(t *testing.T) {
	ev := func(source, icon string) model.Event {
		return model.Event{SourceID: source, Name: source + ":notify", Properties: map[string]string{"title": "t", "icon": icon}}
	}
	_, ok := ClassifyNotification(ev("some-app", ""))
	assert.Assert(t, !ok, "a :notify from an unknown source was stored")

	for icon, want := range map[string]string{
		"/img/backup.svg":                       "/img/backup.svg",
		"data:image/png;base64,iVBORw0KGgo=":    "data:image/png;base64,iVBORw0KGgo=",
		"https://tracker.example/pixel.png":     "",
		"//tracker.example/pixel.png":           "",
		"/\\tracker.example/pixel.png":          "",
		"data:image/svg+xml,<svg/>":             "",
		"javascript:alert(1)":                   "",
		"http://tracker.example/x.png?viewer=1": "",
	} {
		n, ok := ClassifyNotification(ev("nivaroos-backup", icon))
		assert.Assert(t, ok)
		assert.Equal(t, n.Icon, want, icon)
	}

	// App store icons stay (http(s) is what the app store serves them from);
	// other schemes don't.
	app := func(icon string) string {
		n, ok := ClassifyNotification(model.Event{SourceID: "app-management", Name: "app:install-end", Properties: map[string]string{"app:name": "a", "app:icon": icon}})
		assert.Assert(t, ok)
		return n.Icon
	}
	assert.Equal(t, app("https://cdn.example/icon.png"), "https://cdn.example/icon.png")
	assert.Equal(t, app("javascript:alert(1)"), "")
	assert.Equal(t, app("https://user:pw@cdn.example/icon.png"), "")
}

func TestNotificationServiceRateLimitsEachSource(t *testing.T) {
	svc, _, now := newTestNotificationService(t)
	for i := 0; i < notificationSourceBurst; i++ {
		_, ok := svc.Ingest(model.Event{SourceID: "nivaroos-backup", Name: "nivaroos:backup:notify", Properties: map[string]string{"title": "n" + strconv.Itoa(i)}})
		assert.Assert(t, ok, i)
	}
	_, ok := svc.Ingest(model.Event{SourceID: "nivaroos-backup", Name: "nivaroos:backup:notify", Properties: map[string]string{"title": "flood"}})
	assert.Assert(t, !ok, "entry past the per-source limit was stored")
	// Another source is unaffected...
	_, ok = svc.Ingest(model.Event{SourceID: "app-management", Name: "app:install-end", Properties: map[string]string{"app:name": "a"}})
	assert.Assert(t, ok)
	// ...and the source may raise notifications again after the window.
	*now = now.Add(notificationSourceWindow)
	_, ok = svc.Ingest(model.Event{SourceID: "nivaroos-backup", Name: "nivaroos:backup:notify", Properties: map[string]string{"title": "later"}})
	assert.Assert(t, ok)
}
