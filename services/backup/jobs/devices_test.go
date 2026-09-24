package jobs

import (
	"crypto/ecdsa"
	"errors"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
)

// Device credentials (S-07, devices.go): hashing, scope, rotation,
// revoke, persistence.

func withDataDir(dir string) harnessOpt {
	return func(c *Config, h *harness) { c.DataDir = dir }
}

// devClock is a settable clock for rotation tests.
type devClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *devClock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *devClock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func enroll(t *testing.T, h *harness, name string, o reqOpts) DeviceEnrollment {
	t.Helper()
	rec := doRequest(h.svc, "POST", "/v1/backup/devices", DeviceEnrollRequest{Name: name, Platform: "android"}, o)
	wantStatus(t, rec, 201)
	var e DeviceEnrollment
	envelope(t, rec, &e)
	if e.Device.ID == "" || e.Token == "" {
		t.Fatalf("enrolment %+v", e)
	}
	return e
}

func ping(h *harness, id, token string) *httptest.ResponseRecorder {
	return doRequest(h.svc, "GET", "/v1/backup/devices/"+id+"/ping", nil, reqOpts{remote: remoteBrowser, token: token})
}

func TestDeviceTokenIsRandomAndStoredOnlyHashed(t *testing.T) {
	h := newHarness(t, true)
	a := enroll(t, h, "Pixel 8", reqOpts{})
	b := enroll(t, h, "Pixel 8", reqOpts{})
	for _, e := range []DeviceEnrollment{a, b} {
		if !looksLikeDeviceToken(e.Token) {
			t.Errorf("token %q is not nvd_ + 256 bits", e.Token)
		}
	}
	if a.Token == b.Token || a.Device.ID == b.Device.ID {
		t.Fatal("two enrolments share a token or id")
	}
	row, err := h.svc.store.GetDevice(a.Device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.TokenHash != hashDeviceToken(a.Token) || len(row.TokenHash) != 64 || strings.Contains(row.TokenHash, a.Token) {
		t.Errorf("stored hash %q", row.TokenHash)
	}
	// The plain token appears nowhere in the database files or listings.
	h.svc.store.db.Exec("PRAGMA wal_checkpoint(FULL)")
	for _, f := range []string{DBFile, DBFile + "-wal"} {
		raw, err := os.ReadFile(filepath.Join(h.svc.store.DataDir(), f))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), strings.TrimPrefix(a.Token, deviceTokenPrefix)) {
			t.Errorf("%s holds the plain token", f)
		}
	}
	rec := doRequest(h.svc, "GET", "/v1/backup/devices", nil, reqOpts{})
	if strings.Contains(rec.Body.String(), a.Token) || strings.Contains(rec.Body.String(), row.TokenHash) {
		t.Error("device list exposes the token or its hash")
	}
	if !hashEqual(hashDeviceToken(a.Token), row.TokenHash) || hashEqual(hashDeviceToken(b.Token), row.TokenHash) || hashEqual("", "") {
		t.Error("hashEqual")
	}
}

func TestDeviceDefaultDestination(t *testing.T) {
	h := newHarness(t, true)
	a := enroll(t, h, "Pixel 8", reqOpts{})
	b := enroll(t, h, "pixel 8", reqOpts{})
	c := enroll(t, h, "../../etc", reqOpts{})
	d := enroll(t, h, "...", reqOpts{})
	e := enroll(t, h, "Anna’s phone", reqOpts{})
	if a.Device.DefaultDest != "/DATA/Backup/Pixel 8" || b.Device.DefaultDest != "/DATA/Backup/pixel 8 (2)" {
		t.Errorf("same-name devices: %q %q", a.Device.DefaultDest, b.Device.DefaultDest)
	}
	for _, x := range []DeviceEnrollment{a, b, c, d, e} {
		if path.Dir(x.Device.DefaultDest) != DefaultDeviceBackupRoot || path.Clean(x.Device.DefaultDest) != x.Device.DefaultDest {
			t.Errorf("%q: default dest %q escapes %s", x.Device.Name, x.Device.DefaultDest, DefaultDeviceBackupRoot)
		}
	}
	if d.Device.DefaultDest != "/DATA/Backup/"+d.Device.ID || e.Device.DefaultDest != "/DATA/Backup/Anna_s phone" {
		t.Errorf("%q %q", d.Device.DefaultDest, e.Device.DefaultDest)
	}
	for _, bad := range []DeviceEnrollRequest{{Name: ""}, {Name: "  "}, {Name: "a\nb"}, {Name: strings.Repeat("x", 65)}, {Name: "ok", Platform: "windows-phone"}} {
		wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/devices", bad, reqOpts{}), 400)
	}
}

func TestDeviceAPIAdminOnlyAndRevoke(t *testing.T) {
	key := newKey(t)
	h := newHarness(t, true, withPublicKey(&key.PublicKey))
	admin, err := jwt.GetAccessToken("owner", key, 1)
	if err != nil {
		t.Fatal(err)
	}
	viewer := signES256(t, key, map[string]interface{}{"username": "kid", "id": 2, "iss": "nivaroos", "exp": time.Now().Add(time.Hour).Unix(), "role": "user"})
	asAdmin := reqOpts{remote: remoteBrowser, token: admin}
	asViewer := reqOpts{remote: remoteBrowser, token: viewer}

	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/devices", DeviceEnrollRequest{Name: "x"}, asViewer), 403)
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/devices", nil, asViewer), 403)
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/devices", DeviceEnrollRequest{Name: "x"}, reqOpts{remote: remoteBrowser}), 401)

	e := enroll(t, h, "Pixel 8", asAdmin)
	if e.Device.Platform != PlatformAndroid || e.Device.LastSeen != nil || e.Device.CreatedAt.IsZero() {
		t.Errorf("device %+v", e.Device)
	}
	var list []Device
	envelope(t, doRequest(h.svc, "GET", "/v1/backup/devices", nil, asAdmin), &list)
	if len(list) != 1 || list[0].ID != e.Device.ID {
		t.Fatalf("list %+v", list)
	}

	rec := ping(h, e.Device.ID, e.Token)
	wantStatus(t, rec, 200)
	var p DevicePing
	envelope(t, rec, &p)
	if p.Device.ID != e.Device.ID || p.Device.LastSeen == nil || !p.RotatesAt.Equal(e.Device.TokenRotatedAt.Add(DeviceTokenRotateAfter)) {
		t.Errorf("ping %+v", p)
	}

	wantStatus(t, doRequest(h.svc, "DELETE", "/v1/backup/devices/"+e.Device.ID, nil, asViewer), 403)
	wantStatus(t, ping(h, e.Device.ID, e.Token), 200)
	wantStatus(t, doRequest(h.svc, "DELETE", "/v1/backup/devices/"+e.Device.ID, nil, asAdmin), 200)
	rec = ping(h, e.Device.ID, e.Token)
	wantStatus(t, rec, 401)
	if b := errorBody(t, rec); b.ErrorCode != ErrUnauthorized {
		t.Errorf("error_code %q", b.ErrorCode)
	}
	wantStatus(t, doRequest(h.svc, "DELETE", "/v1/backup/devices/"+e.Device.ID, nil, asAdmin), 404)
	envelope(t, doRequest(h.svc, "GET", "/v1/backup/devices", nil, asAdmin), &list)
	if len(list) != 0 {
		t.Errorf("revoked device still listed: %+v", list)
	}
}

func TestDeviceTokenScope(t *testing.T) {
	key := newKey(t)
	h := newHarness(t, true, withPublicKey(&key.PublicKey))
	admin, _ := jwt.GetAccessToken("owner", key, 1)
	a := enroll(t, h, "Phone A", reqOpts{})
	b := enroll(t, h, "Phone B", reqOpts{})
	lan := func(tok string) reqOpts { return reqOpts{remote: remoteBrowser, token: tok} }

	wantStatus(t, ping(h, a.Device.ID, a.Token), 200)
	wantStatus(t, ping(h, b.Device.ID, b.Token), 200)
	// Not on another device's routes.
	wantStatus(t, ping(h, b.Device.ID, a.Token), 401)
	wantStatus(t, ping(h, "dev_0000000000000000", a.Token), 401)
	// Never outside /devices/{id}/*, and never as an admin.
	for _, rt := range []struct{ method, path string }{
		{"GET", "/v1/backup/settings"}, {"GET", "/v1/backup/jobs"}, {"POST", "/v1/backup/jobs"},
		{"GET", "/v1/backup/devices"}, {"POST", "/v1/backup/devices"},
		{"DELETE", "/v1/backup/devices/" + a.Device.ID}, {"DELETE", "/v1/backup/devices/" + b.Device.ID},
		{"GET", "/v1/backup/capabilities"}, {"GET", "/v1/backup/runs"},
	} {
		if rec := doRequest(h.svc, rt.method, rt.path, DeviceEnrollRequest{Name: "x"}, lan(a.Token)); rec.Code != 401 {
			t.Errorf("device token on %s %s: HTTP %d", rt.method, rt.path, rec.Code)
		}
	}
	// ?token= is not accepted for device tokens (URLs end up in logs).
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/devices/"+a.Device.ID+"/ping?token="+a.Token, nil, reqOpts{remote: remoteBrowser}), 401)
	// Device routes want the device token: not the admin JWT, not the
	// loopback shortcut.
	wantStatus(t, ping(h, a.Device.ID, admin), 401)
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/devices/"+a.Device.ID+"/ping", nil, reqOpts{}), 401)
	// Paths that could be cleaned into another route are refused.
	for _, p := range []string{
		"/v1/backup/devices/" + a.Device.ID + "/../../settings",
		"/v1/backup/devices/" + a.Device.ID + "/./ping",
		"/v1/backup/devices//ping",
		"/v1/backup/devices/" + a.Device.ID + "/ping/",
	} {
		if rec := doRequest(h.svc, "GET", p, nil, lan(a.Token)); rec.Code != 404 {
			t.Errorf("%s: HTTP %d", p, rec.Code)
		}
	}
	// Unknown device sub-route: authenticated, then 404.
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/devices/"+a.Device.ID+"/nope", nil, lan(a.Token)), 404)
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/devices/"+a.Device.ID+"/nope", nil, lan(b.Token)), 401)
	// Without the gateway prefix too.
	wantStatus(t, doRequest(h.svc, "GET", "/devices/"+a.Device.ID+"/ping", nil, lan(a.Token)), 200)
	// A truncated or altered token fails.
	wantStatus(t, ping(h, a.Device.ID, a.Token[:len(a.Token)-1]), 401)
	alt := "A"
	if strings.HasSuffix(a.Token, "A") {
		alt = "B"
	}
	wantStatus(t, ping(h, a.Device.ID, a.Token[:len(a.Token)-1]+alt), 401)
}

func TestDeviceTokenRotation(t *testing.T) {
	clk := &devClock{t: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)}
	h := newHarness(t, true, withNow(clk.now))
	e := enroll(t, h, "Pixel 8", reqOpts{})
	id, t0 := e.Device.ID, e.Token

	clk.add(DeviceTokenRotateAfter - time.Hour)
	rec := ping(h, id, t0)
	wantStatus(t, rec, 200)
	if rec.Header().Get(DeviceTokenHeader) != "" {
		t.Fatal("rotated before 30 days")
	}

	clk.add(time.Hour)
	rec = ping(h, id, t0)
	wantStatus(t, rec, 200)
	t1 := rec.Header().Get(DeviceTokenHeader)
	until, err := time.Parse(time.RFC3339, rec.Header().Get(DeviceTokenOldValidUntilHeader))
	if !looksLikeDeviceToken(t1) || t1 == t0 || err != nil || !until.Equal(clk.now().Add(DeviceTokenGrace)) {
		t.Fatalf("rotation: token %q until %v %v", t1, until, err)
	}

	// The answer got lost: the phone retries with the old token and is
	// told the very same new token again (never a second, different one).
	clk.add(time.Hour)
	rec = ping(h, id, t0)
	wantStatus(t, rec, 200)
	if t2 := rec.Header().Get(DeviceTokenHeader); t2 != t1 || rec.Header().Get(DeviceTokenOldValidUntilHeader) != until.Format(time.RFC3339) {
		t.Fatalf("retry with old token: %q (want %q), until %q", t2, t1, rec.Header().Get(DeviceTokenOldValidUntilHeader))
	}
	rec = ping(h, id, t1)
	wantStatus(t, rec, 200)
	if rec.Header().Get(DeviceTokenHeader) != "" {
		t.Error("a fresh token was rotated again")
	}
	// A straggler with the old token still works inside the window and
	// learns the current token, not a new one.
	rec = ping(h, id, t0)
	wantStatus(t, rec, 200)
	if got := rec.Header().Get(DeviceTokenHeader); got != t1 {
		t.Errorf("old token after the new one was used: header %q, want %q", got, t1)
	}
	wantStatus(t, ping(h, id, t1), 200)

	// 24 h after rotation the old token is dead; the new one lives on.
	clk.add(DeviceTokenGrace)
	wantStatus(t, ping(h, id, t0), 401)
	wantStatus(t, ping(h, id, t1), 200)
	row, _ := h.svc.store.GetDevice(id)
	if row.OldTokenHash != "" || row.RotationNonce != "" {
		t.Errorf("expired old token not cleared: %+v", row)
	}

	// The next rotation is 30 days after the last one.
	clk.add(DeviceTokenRotateAfter - DeviceTokenGrace - 3*time.Hour)
	if rec = ping(h, id, t1); rec.Header().Get(DeviceTokenHeader) != "" {
		t.Error("rotated early the second time")
	}
	clk.add(2 * time.Hour)
	if rec = ping(h, id, t1); rec.Header().Get(DeviceTokenHeader) == "" {
		t.Error("no second rotation")
	}
}

func TestDeviceSurvivesRestartAndNeedsNoUserService(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, true, withDataDir(dir))
	e := enroll(t, h, "Pixel 8", reqOpts{})
	wantStatus(t, ping(h, e.Device.ID, e.Token), 200)
	h.stop()

	// A restart, with user-service (JWT keys) unreachable, as after a
	// password change or while it restarts: the device token still works.
	noKeys := func(c *Config, h *harness) {
		c.PublicKey = func() (*ecdsa.PublicKey, error) { return nil, errors.New("user-service down") }
	}
	h2 := newHarness(t, true, withDataDir(dir), noKeys)
	rec := ping(h2, e.Device.ID, e.Token)
	wantStatus(t, rec, 200)
	var p DevicePing
	envelope(t, rec, &p)
	if p.Device.Name != "Pixel 8" || p.Device.DefaultDest != "/DATA/Backup/Pixel 8" {
		t.Errorf("after restart %+v", p.Device)
	}
	// Revocation persists too.
	wantStatus(t, doRequest(h2.svc, "DELETE", "/v1/backup/devices/"+e.Device.ID, nil, reqOpts{}), 200)
	h2.stop()
	h3 := newHarness(t, true, withDataDir(dir))
	wantStatus(t, ping(h3, e.Device.ID, e.Token), 401)
}

// An install upgraded from a build without devices gets the table on
// open, keeping its jobs; opening twice is a no-op.
func TestStoreAddsDevicesTableOnUpgrade(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	j, err := st.CreateJob(sampleJob("keep"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.db.Migrator().DropTable(&DeviceRow{}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	for i := 0; i < 2; i++ {
		st, err = OpenStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !st.db.Migrator().HasTable(&DeviceRow{}) {
			t.Fatal("devices table not created on upgrade")
		}
		if _, err := st.GetJob(j.ID); err != nil {
			t.Fatalf("job lost: %v", err)
		}
		if _, _, err := st.CreateDevice("p", PlatformAndroid, DefaultDeviceBackupRoot, time.Now()); err != nil {
			t.Fatal(err)
		}
		st.Close()
	}
}

func TestDeviceScopePaths(t *testing.T) {
	cases := []struct {
		p           string
		id          string
		scoped, bad bool
	}{
		{"/devices", "", false, false},
		{"/devices/dev_1", "", false, false},
		{"/devices/dev_1/ping", "dev_1", true, false},
		{"/devices/dev_1/sessions/s1/finish", "dev_1", true, false},
		{"/devices/dev_1/", "", false, true},
		{"/devices//ping", "", false, true},
		{"/devices/dev_1/../x", "", false, true},
		{"/devices/../jobs", "", false, true},
		{"/devicesx/dev_1/ping", "", false, false},
		{"/jobs/devices/dev_1/ping", "", false, false},
	}
	for _, c := range cases {
		id, scoped, bad := deviceScope(c.p)
		if id != c.id || scoped != c.scoped || bad != c.bad {
			t.Errorf("deviceScope(%q) = %q %v %v, want %q %v %v", c.p, id, scoped, bad, c.id, c.scoped, c.bad)
		}
	}
}
