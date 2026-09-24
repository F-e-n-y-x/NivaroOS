package route

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/repository"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/service"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"go.uber.org/zap/zapcore"
	"gotest.tools/assert"
)

type feedFixture struct {
	server   *httptest.Server
	services *service.Services
	alice    string // user 1
	bob      string // user 2
}

func newFeedFixture(t *testing.T, withFeed bool) *feedFixture {
	t.Helper()
	logger.LogInitWithWriterSyncers(zapcore.AddSync(io.Discard))

	privateKey, publicKey, err := jwt.GenerateKeyPair()
	assert.NilError(t, err)
	alice, err := jwt.GetAccessToken("alice", privateKey, 1)
	assert.NilError(t, err)
	bob, err := jwt.GetAccessToken("bob", privateKey, 2)
	assert.NilError(t, err)

	repo, err := repository.NewDatabaseRepositoryInMemory()
	assert.NilError(t, err)
	t.Cleanup(func() { repo.Close() })

	services := service.NewServices(&repo)
	if withFeed {
		store, err := repository.NewNotificationStoreInMemory()
		assert.NilError(t, err)
		t.Cleanup(store.Close)
		services.NotificationService = service.NewNotificationService(store, services.PublishEvent)
		assert.NilError(t, services.NotificationService.RegisterEventTypes(services.EventTypeService))
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	services.Start(&ctx)

	swagger, err := codegen.GetSwagger()
	assert.NilError(t, err)
	handler, err := newAPIRouter(swagger, &services, func() (*ecdsa.PublicKey, error) { return publicKey, nil })
	assert.NilError(t, err)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	f := &feedFixture{server: server, services: &services, alice: alice, bob: bob}
	// The backup service registers its types as same-host automation.
	f.do(t, http.MethodPost, "/v2/message_bus/event_type", "", `[{"sourceID":"nivaroos-backup","name":"nivaroos:backup:notify","propertyTypeList":[]},{"sourceID":"nivaroos","name":"nivaroos:system:utilization","propertyTypeList":[]}]`, http.StatusOK)
	return f
}

// do sends a request. With a token it goes "through the gateway" (proxy
// headers, not local automation), so the JWT is what identifies the user.
func (f *feedFixture) do(t *testing.T, method, path, token, body string, want int) []byte {
	t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, bytes.NewBufferString(body))
	assert.NilError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Forwarded-For", "192.0.2.10")
	}
	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	assert.Equal(t, resp.StatusCode, want, method+" "+path+": "+string(b))
	return b
}

func (f *feedFixture) publishBackupFailure(t *testing.T, job string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"key": "backup.notify.failed", "args": `{"job":"` + job + `"}`, "title": "Backup & Sync",
		"message": job + " failed", "level": "error", "window": `{"kind":"run","props":{"runId":"r1"}}`,
	})
	f.do(t, http.MethodPost, "/v2/message_bus/event/nivaroos-backup/nivaroos:backup:notify", "", string(body), http.StatusOK)
}

func (f *feedFixture) list(t *testing.T, token, query string) codegen.NotificationList {
	t.Helper()
	var out codegen.NotificationList
	assert.NilError(t, json.Unmarshal(f.do(t, http.MethodGet, "/v2/message_bus/notifications"+query, token, "", http.StatusOK), &out))
	return out
}

func TestNotificationFeedAPI(t *testing.T) {
	f := newFeedFixture(t, true)

	// Not a notification: never stored.
	f.do(t, http.MethodPost, "/v2/message_bus/event/nivaroos/nivaroos:system:utilization", "", `{"cpu":"3"}`, http.StatusOK)
	f.publishBackupFailure(t, "Documents")
	f.publishBackupFailure(t, "Photos")

	list := f.list(t, f.alice, "")
	assert.Equal(t, len(list.Data), 2)
	assert.Equal(t, list.UnreadCount, int64(2))
	assert.Assert(t, !list.HasMore)
	newest := list.Data[0]
	assert.Equal(t, newest.Message, "Photos failed")
	assert.Equal(t, newest.Key, "backup.notify.failed")
	assert.Equal(t, newest.Args["job"], "Photos")
	assert.Equal(t, string(newest.Category), "backup")
	assert.Equal(t, string(newest.Level), "error")
	assert.Equal(t, newest.SourceId, "nivaroos-backup")
	assert.Assert(t, newest.Action != nil)
	assert.Equal(t, (*newest.Action)["target"], "backup")
	assert.Equal(t, list.LatestId, newest.Id)
	assert.Assert(t, time.Since(newest.Time) < time.Minute)

	// Paging and polling cursors.
	page := f.list(t, f.alice, "?limit=1")
	assert.Equal(t, len(page.Data), 1)
	assert.Assert(t, page.HasMore)
	older := f.list(t, f.alice, "?limit=1&before="+strconv.FormatInt(page.Data[0].Id, 10))
	assert.Equal(t, older.Data[0].Message, "Documents failed")
	none := f.list(t, f.alice, "?after="+strconv.FormatInt(list.LatestId, 10))
	assert.Equal(t, len(none.Data), 0)
	assert.Equal(t, none.UnreadCount, int64(2))

	// Alice reads one; Bob's state is his own.
	var count codegen.NotificationCount
	assert.NilError(t, json.Unmarshal(f.do(t, http.MethodPost, "/v2/message_bus/notifications/read", f.alice,
		`{"ids":[`+strconv.FormatInt(newest.Id, 10)+`]}`, http.StatusOK), &count))
	assert.Equal(t, count.UnreadCount, int64(1))
	assert.Assert(t, f.list(t, f.alice, "").Data[0].Read)
	assert.Equal(t, f.list(t, f.bob, "").UnreadCount, int64(2))
	assert.Equal(t, len(f.list(t, f.alice, "?unread=true").Data), 1)

	// Mark all read up to what she has seen: a later one stays unread.
	f.do(t, http.MethodPost, "/v2/message_bus/notifications/read", f.alice, `{"all":true,"up_to":`+strconv.FormatInt(list.LatestId, 10)+`}`, http.StatusOK)
	f.publishBackupFailure(t, "Music")
	after := f.list(t, f.alice, "")
	assert.Equal(t, after.UnreadCount, int64(1))
	assert.Equal(t, after.Data[0].Message, "Music failed")

	// Bob dismisses one, then clears everything.
	f.do(t, http.MethodPost, "/v2/message_bus/notifications/dismiss", f.bob, `{"ids":[`+strconv.FormatInt(newest.Id, 10)+`]}`, http.StatusOK)
	assert.Equal(t, len(f.list(t, f.bob, "").Data), 2)
	f.do(t, http.MethodPost, "/v2/message_bus/notifications/dismiss", f.bob, `{"all":true}`, http.StatusOK)
	cleared := f.list(t, f.bob, "")
	assert.Equal(t, len(cleared.Data), 0)
	assert.Equal(t, cleared.UnreadCount, int64(0))
	assert.Equal(t, len(f.list(t, f.alice, "").Data), 3)
}

func TestNotificationFeedAPIValidation(t *testing.T) {
	f := newFeedFixture(t, true)

	// The normal JWT rules: no token (via the gateway) is refused.
	req, err := http.NewRequest(http.MethodGet, f.server.URL+"/v2/message_bus/notifications", nil)
	assert.NilError(t, err)
	req.Header.Set("X-Forwarded-For", "192.0.2.10")
	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	resp.Body.Close()
	assert.Equal(t, resp.StatusCode, http.StatusUnauthorized)

	f.do(t, http.MethodGet, "/v2/message_bus/notifications?limit=0", f.alice, "", http.StatusBadRequest)
	f.do(t, http.MethodGet, "/v2/message_bus/notifications?limit=500", f.alice, "", http.StatusBadRequest)
	f.do(t, http.MethodGet, "/v2/message_bus/notifications?after=-1", f.alice, "", http.StatusBadRequest)
	f.do(t, http.MethodPost, "/v2/message_bus/notifications/read", f.alice, `{}`, http.StatusBadRequest)
	f.do(t, http.MethodPost, "/v2/message_bus/notifications/read", f.alice, `{"ids":"x"}`, http.StatusBadRequest)
	f.do(t, http.MethodPost, "/v2/message_bus/notifications/dismiss", f.alice, `{"all":true,"up_to":-3}`, http.StatusBadRequest)

	// Same-host automation (nivaroos-cli) may read it, as user 0.
	f.publishBackupFailure(t, "Documents")
	var out codegen.NotificationList
	assert.NilError(t, json.Unmarshal(f.do(t, http.MethodGet, "/v2/message_bus/notifications", "", "", http.StatusOK), &out))
	assert.Equal(t, len(out.Data), 1)
}

func TestNotificationFeedUnavailable(t *testing.T) {
	f := newFeedFixture(t, false)
	f.do(t, http.MethodGet, "/v2/message_bus/notifications", f.alice, "", http.StatusServiceUnavailable)
	f.do(t, http.MethodPost, "/v2/message_bus/notifications/read", f.alice, `{"all":true}`, http.StatusServiceUnavailable)
	// Events still flow without a feed.
	f.publishBackupFailure(t, "Documents")
}

func TestNotificationFeedLiveEvent(t *testing.T) {
	f := newFeedFixture(t, true)

	conn, _, _, err := ws.Dial(context.Background(), "ws"+f.server.URL[len("http"):]+"/v2/message_bus/event/message-bus?token="+f.alice)
	assert.NilError(t, err)
	defer conn.Close()

	// The subscription is registered asynchronously after the upgrade.
	deadline := time.Now().Add(5 * time.Second)
	got := map[string]interface{}{}
	for time.Now().Before(deadline) {
		f.publishBackupFailure(t, "Live"+strconv.FormatInt(time.Now().UnixNano(), 10))
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		msg, op, err := wsutil.ReadServerData(conn)
		if err != nil || op != ws.OpText {
			continue
		}
		assert.NilError(t, json.Unmarshal(msg, &got))
		break
	}
	assert.Equal(t, got["name"], service.NotificationCreatedEvent)
	props, _ := got["properties"].(map[string]interface{})
	assert.Equal(t, props["title"], "Backup & Sync")
	assert.Assert(t, props["id"] != "")
}
