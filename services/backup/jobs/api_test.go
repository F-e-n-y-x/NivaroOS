package jobs

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
)

// The REST API (spec §11, §16.1 api_test.go): every route through the
// real handler, envelope shape, auth, 409 revision conflicts, lists
// without log fields, single-use download tokens.

type reqOpts struct {
	token   string
	remote  string // default: loopback automation
	headers map[string]string
}

func doRequest(svc *Service, method, path string, body interface{}, o reqOpts) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != nil {
		raw, ok := body.([]byte)
		if !ok {
			raw, _ = json.Marshal(body)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = "127.0.0.1:40000"
	if o.remote != "" {
		req.RemoteAddr = o.remote
	}
	if o.token != "" {
		req.Header.Set("Authorization", "Bearer "+o.token)
	}
	for k, v := range o.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	return rec
}

// envelope decodes the standard {success, message, data} answer.
func envelope(t *testing.T, rec *httptest.ResponseRecorder, data interface{}) Envelope {
	t.Helper()
	var env struct {
		Success int             `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("not an envelope (%d): %s", rec.Code, rec.Body.String())
	}
	if env.Success != rec.Code {
		t.Errorf("envelope success %d != HTTP status %d", env.Success, rec.Code)
	}
	if data != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, data); err != nil {
			t.Fatalf("decode data: %v: %s", err, env.Data)
		}
	}
	return Envelope{Success: env.Success, Message: env.Message}
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) ErrorBody {
	t.Helper()
	var b ErrorBody
	envelope(t, rec, &b)
	return b
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("HTTP %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
}

// signES256 signs arbitrary claims the way user-service does, so tests
// can mint tokens with a role claim.
func signES256(t *testing.T, key *ecdsa.PrivateKey, claims map[string]interface{}) string {
	t.Helper()
	enc := func(v interface{}) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	signing := enc(map[string]string{"alg": "ES256", "typ": "JWT"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func withPublicKey(pub *ecdsa.PublicKey) harnessOpt {
	return func(c *Config, h *harness) {
		c.PublicKey = func() (*ecdsa.PublicKey, error) { return pub, nil }
	}
}

const remoteBrowser = "192.168.1.20:51000"

func TestHealthNeedsNoTokenAndIsNotEnveloped(t *testing.T) {
	h := newHarness(t, true)
	for _, p := range []string{"/v1/backup/health", "/health"} {
		rec := doRequest(h.svc, "GET", p, nil, reqOpts{remote: remoteBrowser})
		wantStatus(t, rec, 200)
		var hl ServiceHealth
		if err := json.Unmarshal(rec.Body.Bytes(), &hl); err != nil || !hl.Installed || !hl.Running || hl.Version != "test" || hl.Service != "backup" {
			t.Errorf("health %s: %+v %v", p, hl, err)
		}
		if strings.Contains(rec.Body.String(), `"success"`) {
			t.Errorf("health is enveloped: %s", rec.Body.String())
		}
	}
}

func TestAuth(t *testing.T) {
	key := newKey(t)
	h := newHarness(t, true, withPublicKey(&key.PublicKey))
	admin, err := jwt.GetAccessToken("owner", key, 1)
	if err != nil {
		t.Fatal(err)
	}
	refresh, _ := jwt.GetRefreshToken("owner", key, 1)
	exp := time.Now().Add(time.Hour).Unix()
	viewer := signES256(t, key, map[string]interface{}{"username": "kid", "id": 2, "iss": "nivaroos", "exp": exp, "role": "user"})
	roleAdmin := signES256(t, key, map[string]interface{}{"username": "boss", "id": 3, "iss": "nivaroos", "exp": exp, "roles": []string{"user", "admin"}})
	other := signES256(t, newKey(t), map[string]interface{}{"username": "x", "id": 4, "iss": "nivaroos", "exp": exp})

	cases := []struct {
		name   string
		method string
		o      reqOpts
		want   int
	}{
		{"no token from the LAN", "GET", reqOpts{remote: remoteBrowser}, 401},
		{"garbage token", "GET", reqOpts{remote: remoteBrowser, token: "abc"}, 401},
		{"refresh token", "GET", reqOpts{remote: remoteBrowser, token: refresh}, 401},
		{"token signed by another key", "GET", reqOpts{remote: remoteBrowser, token: other}, 401},
		{"access token", "GET", reqOpts{remote: remoteBrowser, token: admin}, 200},
		{"access token writes", "PUT", reqOpts{remote: remoteBrowser, token: admin}, 200},
		{"role user reads", "GET", reqOpts{remote: remoteBrowser, token: viewer}, 200},
		{"role user writes", "PUT", reqOpts{remote: remoteBrowser, token: viewer}, 403},
		{"roles incl. admin writes", "PUT", reqOpts{remote: remoteBrowser, token: roleAdmin}, 200},
		{"loopback automation", "PUT", reqOpts{}, 200},
		{"browser tab on this box", "GET", reqOpts{headers: map[string]string{"Sec-Fetch-Site": "same-origin"}}, 401},
		{"DNS rebinding page", "GET", reqOpts{headers: map[string]string{"Origin": "http://evil.example"}}, 401},
		{"proxied without the gateway's word", "GET", reqOpts{headers: map[string]string{"X-Forwarded-For": "10.0.0.9"}}, 401},
		{"proxied local automation", "GET", reqOpts{headers: map[string]string{"X-Forwarded-For": "127.0.0.1", middleware.LocalAutomationHeader: "1"}}, 200},
		{"forged gateway header from the LAN", "GET", reqOpts{remote: remoteBrowser, headers: map[string]string{middleware.LocalAutomationHeader: "1"}}, 401},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body interface{}
			if c.method == "PUT" {
				body = DefaultAppSettings()
			}
			rec := doRequest(h.svc, c.method, "/v1/backup/settings", body, c.o)
			if rec.Code != c.want {
				t.Fatalf("HTTP %d, want %d: %s", rec.Code, c.want, rec.Body.String())
			}
			switch c.want {
			case 401:
				if b := errorBody(t, rec); b.ErrorCode != ErrUnauthorized {
					t.Errorf("error_code %q", b.ErrorCode)
				}
			case 403:
				if b := errorBody(t, rec); b.ErrorCode != ErrForbidden {
					t.Errorf("error_code %q", b.ErrorCode)
				}
			}
		})
	}
	// ?token= works too (EventSource and download links can't set headers).
	rec := doRequest(h.svc, "GET", "/v1/backup/settings?token="+admin, nil, reqOpts{remote: remoteBrowser})
	wantStatus(t, rec, 200)
}

func TestCORSOnlyForThisHost(t *testing.T) {
	h := newHarness(t, true)
	rec := doRequest(h.svc, "OPTIONS", "/v1/backup/jobs", nil, reqOpts{headers: map[string]string{"Origin": "http://evil.test:8080"}})
	req := httptest.NewRequest("OPTIONS", "http://192.168.1.2:28643/v1/backup/jobs", nil)
	req.Header.Set("Origin", "http://192.168.1.2")
	same := httptest.NewRecorder()
	h.svc.Handler().ServeHTTP(same, req)
	if got := same.Header().Get("Access-Control-Allow-Origin"); got != "http://192.168.1.2" {
		t.Errorf("same-host origin not allowed: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("foreign origin allowed: %q", got)
	}
}

func TestUnknownRouteIsEnveloped404(t *testing.T) {
	h := newHarness(t, true)
	rec := doRequest(h.svc, "GET", "/v1/backup/nope", nil, reqOpts{})
	wantStatus(t, rec, 404)
	if b := errorBody(t, rec); b.ErrorCode != ErrNotFound {
		t.Errorf("error_code %q", b.ErrorCode)
	}
	// Without the prefix too (the gateway may strip it).
	wantStatus(t, doRequest(h.svc, "GET", "/settings", nil, reqOpts{}), 200)
}

func TestJobCRUD(t *testing.T) {
	h := newHarness(t, true)
	// Invalid job: 400 validation with field errors.
	bad := sampleJob("x")
	bad.Sources[0].SubPath = "../etc"
	rec := doRequest(h.svc, "POST", "/v1/backup/jobs", bad, reqOpts{})
	wantStatus(t, rec, 400)
	if b := errorBody(t, rec); b.ErrorCode != ErrValidation || b.FieldErrors["sources[0].sub_path"] != string(ErrPathNotAllowed) {
		t.Fatalf("validation body: %+v", b)
	}
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/jobs", []byte("{nope"), reqOpts{}), 400)

	// Create.
	rec = doRequest(h.svc, "POST", "/v1/backup/jobs", sampleJob("Docs"), reqOpts{})
	wantStatus(t, rec, 201)
	var created JobDetail
	envelope(t, rec, &created)
	if created.ID == "" || created.Revision != 1 || created.Health != HealthOK || !created.DestOnline {
		t.Fatalf("created: %+v", created)
	}
	if strings.Contains(rec.Body.String(), "dest_folder_id") || strings.Contains(rec.Body.String(), "DestFolderID") {
		t.Error("the destination folder id leaks into the API")
	}

	// List: health, no log fields.
	rec = doRequest(h.svc, "GET", "/v1/backup/jobs", nil, reqOpts{})
	wantStatus(t, rec, 200)
	var list []JobListItem
	envelope(t, rec, &list)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list: %+v", list)
	}
	for _, leak := range []string{"log_path", "LogPath", "lines", "preview_path"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("GET /jobs carries %q", leak)
		}
	}

	// Update: revision required, stale revision 409 with current.
	edit := created.Job
	edit.Name = "Documents"
	edit.Revision = 0
	wantStatus(t, doRequest(h.svc, "PUT", "/v1/backup/jobs/"+created.ID, edit, reqOpts{}), 400)
	edit.Revision = 1
	rec = doRequest(h.svc, "PUT", "/v1/backup/jobs/"+created.ID, edit, reqOpts{})
	wantStatus(t, rec, 200)
	var updated JobDetail
	envelope(t, rec, &updated)
	if updated.Revision != 2 || updated.Name != "Documents" {
		t.Fatalf("updated: %+v", updated)
	}
	rec = doRequest(h.svc, "PUT", "/v1/backup/jobs/"+created.ID, edit, reqOpts{})
	wantStatus(t, rec, 409)
	if b := errorBody(t, rec); b.ErrorCode != ErrRevisionConflict || b.Current == nil || b.Current.Revision != 2 {
		t.Fatalf("409 body: %+v", b)
	}
	wantStatus(t, doRequest(h.svc, "PUT", "/v1/backup/jobs/bk_000000000000", edit, reqOpts{}), 404)

	// A new destination is a new backup folder: new folder id, first run.
	stored, _ := h.svc.store.GetJob(created.ID)
	h.svc.store.UpdateJobState(created.ID, func(st *JobState) { now := time.Now(); st.LastSuccessAt = &now })
	edit.Revision = 2
	edit.Dest.SubPath = "Elsewhere"
	wantStatus(t, doRequest(h.svc, "PUT", "/v1/backup/jobs/"+created.ID, edit, reqOpts{}), 200)
	moved, _ := h.svc.store.GetJob(created.ID)
	if moved.DestFolderID == stored.DestFolderID {
		t.Error("destination changed but the folder id stayed")
	}
	if st, _ := h.svc.store.JobState(created.ID); st.LastSuccessAt != nil {
		t.Error("first-run state not reset for the new destination")
	}

	// Toggle.
	rec = doRequest(h.svc, "POST", "/v1/backup/jobs/"+created.ID+"/toggle", ToggleRequest{Enabled: false}, reqOpts{})
	wantStatus(t, rec, 200)
	var toggled JobDetail
	envelope(t, rec, &toggled)
	if toggled.Enabled || toggled.Health != HealthDisabled {
		t.Errorf("toggled: %+v", toggled)
	}

	// Delete, then it's gone.
	rec = doRequest(h.svc, "DELETE", "/v1/backup/jobs/"+created.ID, nil, reqOpts{})
	wantStatus(t, rec, 200)
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/jobs/"+created.ID, nil, reqOpts{}), 404)
	h.busEvents(EventJobChanged, 4)
}

func TestRunNowCancelAndDeleteGuard(t *testing.T) {
	h := newHarness(t, true)
	j := h.createJob(sampleJob("Docs"))
	rec := doRequest(h.svc, "POST", "/v1/backup/jobs/"+j.ID+"/run", RunRequest{}, reqOpts{})
	wantStatus(t, rec, 202)
	var started RunStarted
	envelope(t, rec, &started)
	h.startedJob(1)

	// A second "run now" coalesces onto the running one.
	rec = doRequest(h.svc, "POST", "/v1/backup/jobs/"+j.ID+"/run", RunRequest{}, reqOpts{})
	var again RunStarted
	envelope(t, rec, &again)
	if again.RunID != started.RunID {
		t.Errorf("second run %s, want coalesced onto %s", again.RunID, started.RunID)
	}

	// Deleting a job with an active run is refused.
	rec = doRequest(h.svc, "DELETE", "/v1/backup/jobs/"+j.ID, nil, reqOpts{})
	wantStatus(t, rec, 409)

	// Run detail while running: live block, steps, no file paths.
	rec = doRequest(h.svc, "GET", "/v1/backup/runs/"+started.RunID, nil, reqOpts{})
	wantStatus(t, rec, 200)
	var d RunDetail
	envelope(t, rec, &d)
	if d.Status != StatusRunning || d.Live == nil || len(d.Steps) == 0 || d.Coalesced != 1 {
		t.Errorf("run detail: %+v", d)
	}
	if strings.Contains(rec.Body.String(), h.svc.store.DataDir()) {
		t.Error("run detail leaks a server path")
	}

	rec = doRequest(h.svc, "POST", "/v1/backup/runs/"+started.RunID+"/cancel", nil, reqOpts{})
	wantStatus(t, rec, 200)
	r := h.waitStatus(started.RunID, StatusCancelled)
	if ErrorCode(r.ErrorCode) != ErrCancelledByUser {
		t.Errorf("cancel code %q", r.ErrorCode)
	}
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/runs/"+started.RunID+"/cancel", nil, reqOpts{}), 409)
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/runs/"+started.RunID+"/decide", DecideRequest{Proceed: true}, reqOpts{}), 409)

	// Log: finished and readable.
	rec = doRequest(h.svc, "GET", "/v1/backup/runs/"+started.RunID+"/log", nil, reqOpts{})
	wantStatus(t, rec, 200)
	var lp LogPage
	envelope(t, rec, &lp)
	if !lp.Done || len(lp.Lines) == 0 {
		t.Errorf("log page: %+v", lp)
	}

	wantStatus(t, doRequest(h.svc, "DELETE", "/v1/backup/jobs/"+j.ID, nil, reqOpts{}), 200)
	if _, err := h.svc.store.GetRun(started.RunID); !isNoRecord(err) {
		t.Errorf("runs of a deleted job kept: %v", err)
	}
}

func TestRunsListPaging(t *testing.T) {
	h := newHarness(t, true)
	j := h.createJob(sampleJob("Docs"))
	for i := 0; i < 5; i++ {
		id := NewRunID(time.Now())
		now := time.Now()
		h.svc.store.CreateRun(RunRow{ID: id, JobID: j.ID, Kind: string(KindBackup), Trigger: string(RunBySchedule),
			Status: string(StatusSuccess), QueuedAt: now, EndedAt: &now, LogPath: "/secret/path.jsonl.gz", Summary: "backup.run.summary.ok_nothing"})
	}
	rec := doRequest(h.svc, "GET", "/v1/backup/runs?job_id="+j.ID+"&limit=2", nil, reqOpts{})
	wantStatus(t, rec, 200)
	var page RunList
	envelope(t, rec, &page)
	if len(page.Runs) != 2 || page.NextBefore == "" || page.Runs[0].JobName != "Docs" || page.Runs[0].Summary.Key != "backup.run.summary.ok_nothing" {
		t.Fatalf("page 1: %+v", page)
	}
	if strings.Contains(rec.Body.String(), "/secret/") {
		t.Error("run list leaks log paths")
	}
	seen := len(page.Runs)
	for page.NextBefore != "" {
		rec = doRequest(h.svc, "GET", "/v1/backup/runs?job_id="+j.ID+"&limit=2&before="+page.NextBefore, nil, reqOpts{})
		page = RunList{}
		envelope(t, rec, &page)
		seen += len(page.Runs)
	}
	if seen != 5 {
		t.Errorf("paged through %d runs, want 5", seen)
	}
	for _, q := range []string{"limit=0", "limit=9999", "status=bogus", "kind=bogus"} {
		wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/runs?"+q, nil, reqOpts{}), 400)
	}
	rec = doRequest(h.svc, "GET", "/v1/backup/runs?status=success,failed", nil, reqOpts{})
	wantStatus(t, rec, 200)
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/runs/run_nope", nil, reqOpts{}), 404)
}

func TestDownloadTokenIsSingleUseAndIPBound(t *testing.T) {
	key := newKey(t)
	h := newHarness(t, true, withPublicKey(&key.PublicKey))
	tok, _ := jwt.GetAccessToken("owner", key, 1)
	j := h.createJob(sampleJob("Docs"))
	issue := func(remote string) string {
		rec := doRequest(h.svc, "POST", "/v1/backup/downloads", DownloadRequest{JobID: j.ID, VersionID: "current", Paths: []string{"a.txt"}},
			reqOpts{remote: remote, token: tok})
		wantStatus(t, rec, 200)
		var dt DownloadToken
		envelope(t, rec, &dt)
		if !strings.HasPrefix(dt.Token, "dl_") || len(dt.Token) != 35 || dt.ExpiresIn != 60 {
			t.Fatalf("token: %+v", dt)
		}
		return dt.Token
	}
	h.eng.Lock()
	h.eng.DownloadV = &engine.Download{Name: "a.txt", ContentType: "text/plain", Size: 5, Body: io.NopCloser(strings.NewReader("hello"))}
	h.eng.Unlock()

	t1 := issue(remoteBrowser)
	// No JWT needed on the download itself.
	rec := doRequest(h.svc, "GET", "/v1/backup/downloads/"+t1, nil, reqOpts{remote: remoteBrowser})
	wantStatus(t, rec, 200)
	if rec.Body.String() != "hello" || !strings.Contains(rec.Header().Get("Content-Disposition"), "a.txt") {
		t.Errorf("download: %q %v", rec.Body.String(), rec.Header())
	}
	// Used once.
	rec = doRequest(h.svc, "GET", "/v1/backup/downloads/"+t1, nil, reqOpts{remote: remoteBrowser})
	wantStatus(t, rec, 404)
	if b := errorBody(t, rec); b.ErrorCode != ErrTokenInvalid {
		t.Errorf("reuse: %+v", b)
	}
	// Another IP burns it.
	t2 := issue(remoteBrowser)
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/downloads/"+t2, nil, reqOpts{remote: "192.168.1.99:1"}), 404)
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/downloads/"+t2, nil, reqOpts{remote: remoteBrowser}), 404)
	// Expired after 60 s.
	t3 := issue(remoteBrowser)
	h.setNow(func() time.Time { return time.Now().Add(61 * time.Second) })
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/downloads/"+t3, nil, reqOpts{remote: remoteBrowser}), 404)
	h.setNow(nil)
	// Bad requests never issue a token.
	rec = doRequest(h.svc, "POST", "/v1/backup/downloads", DownloadRequest{JobID: j.ID, VersionID: "v_bad", Paths: []string{"../x"}}, reqOpts{})
	wantStatus(t, rec, 400)
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/downloads", DownloadRequest{JobID: "bk_nope", VersionID: "current", Paths: []string{"a"}}, reqOpts{}), 404)

	// Behind the gateway the browser's address is the last X-Forwarded-For hop.
	gw := map[string]string{"X-Forwarded-For": "6.6.6.6, 192.168.1.20", middleware.LocalAutomationHeader: ""}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:9"
	for k, v := range gw {
		req.Header.Set(k, v)
	}
	if ip := clientIP(req); ip != "192.168.1.20" {
		t.Errorf("clientIP behind gateway = %q", ip)
	}
	req.RemoteAddr = "10.0.0.5:9"
	if ip := clientIP(req); ip != "10.0.0.5" {
		t.Errorf("clientIP from a LAN peer trusts X-Forwarded-For: %q", ip)
	}
}

func TestSettingsAPI(t *testing.T) {
	h := newHarness(t, true)
	rec := doRequest(h.svc, "GET", "/v1/backup/settings", nil, reqOpts{})
	var st AppSettings
	envelope(t, rec, &st)
	if st != DefaultAppSettings() {
		t.Errorf("defaults: %+v", st)
	}
	st.MaxConcurrent = 9
	rec = doRequest(h.svc, "PUT", "/v1/backup/settings", st, reqOpts{})
	wantStatus(t, rec, 400)
	if b := errorBody(t, rec); b.FieldErrors["max_concurrent"] != string(FieldOutOfRange) {
		t.Errorf("settings validation: %+v", b)
	}
	st.MaxConcurrent, st.LogRetentionDays = 3, 30
	wantStatus(t, doRequest(h.svc, "PUT", "/v1/backup/settings", st, reqOpts{}), 200)
	if got, _ := h.svc.store.Settings(); got.MaxConcurrent != 3 || got.LogRetentionDays != 30 {
		t.Errorf("stored: %+v", got)
	}
}

func TestCronPreviewAPI(t *testing.T) {
	h := newHarness(t, true)
	rec := doRequest(h.svc, "POST", "/v1/backup/cron/preview", CronPreviewRequest{Cron: "0 3 * * *"}, reqOpts{})
	wantStatus(t, rec, 200)
	var p CronPreview
	envelope(t, rec, &p)
	if !p.Valid || p.HumanKey != "backup.cron.daily_at" || p.Args["time"] != "03:00" || len(p.Next) != 5 || p.Timezone == "" {
		t.Errorf("preview: %+v", p)
	}
	rec = doRequest(h.svc, "POST", "/v1/backup/cron/preview", CronPreviewRequest{Cron: "nope"}, reqOpts{})
	wantStatus(t, rec, 200)
	p = CronPreview{}
	envelope(t, rec, &p)
	if p.Valid || p.Error == "" {
		t.Errorf("invalid cron preview: %+v", p)
	}
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/cron/preview", CronPreviewRequest{Cron: strings.Repeat("*", 300)}, reqOpts{}), 400)
}

func TestValidateAPI(t *testing.T) {
	h := newHarness(t, true)
	h.eng.Lock()
	h.eng.PrecheckV = engine.PrecheckResult{OK: true, Checks: []engine.Check{{ID: engine.CheckDestResolves, Status: engine.CheckPass}}}
	h.eng.LocationsV = []engine.Location{
		{Kind: EPVolume, RefID: "root-uuid", PhysicalDisk: "sda"},
		{Kind: EPVolume, RefID: "tank-uuid", PhysicalDisk: "sda"},
	}
	h.eng.Unlock()
	rec := doRequest(h.svc, "POST", "/v1/backup/validate", sampleJob("Docs"), reqOpts{})
	wantStatus(t, rec, 200)
	var res ValidateResult
	envelope(t, rec, &res)
	ids := map[string]engine.CheckStatus{}
	for _, c := range res.Checks {
		ids[c.ID] = c.Status
	}
	if !res.OK || ids[engine.CheckDestResolves] != engine.CheckPass || ids[CheckSameDisk] != engine.CheckWarn || ids[CheckCron] != engine.CheckPass {
		t.Errorf("validate: %+v", res)
	}
	// A field error makes it not ok and skips the engine.
	calls := len(h.eng.CallsTo("Precheck"))
	bad := sampleJob("Docs")
	bad.Dest.SubPath = "/abs"
	rec = doRequest(h.svc, "POST", "/v1/backup/validate", bad, reqOpts{})
	res = ValidateResult{}
	envelope(t, rec, &res)
	if res.OK || res.FieldErrors["dest.sub_path"] == "" || len(h.eng.CallsTo("Precheck")) != calls {
		t.Errorf("invalid job validate: %+v", res)
	}
}

func TestRestoreAPI(t *testing.T) {
	h := newHarness(t, false)
	j := h.createJob(sampleJob("Docs"))
	h.start()
	cases := []struct {
		req  RestoreRequest
		want int
	}{
		{RestoreRequest{VersionID: "current", Paths: []string{"a.pdf"}, Target: RestoreTarget{Mode: RestoreOriginal}}, 202},
		{RestoreRequest{VersionID: "v_20260923T030000Z", Target: RestoreTarget{Mode: RestoreOther, Endpoint: &Endpoint{Kind: EPVolume, RefID: "tank-uuid", SubPath: "Restored"}}}, 202},
		{RestoreRequest{VersionID: "v_bad"}, 400},
		{RestoreRequest{VersionID: "current", Paths: []string{"../../etc/shadow"}}, 400},
		{RestoreRequest{VersionID: "current", Conflict: "merge"}, 400},
		{RestoreRequest{VersionID: "current", Target: RestoreTarget{Mode: RestoreOther}}, 400},
		{RestoreRequest{VersionID: "current", Target: RestoreTarget{Mode: "sideways"}}, 400},
	}
	for i, c := range cases {
		rec := doRequest(h.svc, "POST", "/v1/backup/jobs/"+j.ID+"/restore", c.req, reqOpts{})
		if rec.Code != c.want {
			t.Errorf("case %d: HTTP %d, want %d: %s", i, rec.Code, c.want, rec.Body.String())
		}
	}
	_, req := h.startedJob(1)
	if req.Op != engine.OpRestore || req.Restore == nil || req.Restore.Conflict != engine.ConflictKeepBoth || !sameEndpoint(req.Restore.Target, j.Sources[0]) {
		t.Errorf("restore request: %+v", req.Restore)
	}
}

func TestRateLimit(t *testing.T) {
	h := newHarness(t, true)
	codes := map[int]int{}
	for i := 0; i < 32; i++ {
		codes[doRequest(h.svc, "POST", "/v1/backup/jobs", Job{}, reqOpts{}).Code]++
	}
	if codes[400] != 30 || codes[429] != 2 {
		t.Errorf("status counts %v, want 30 x 400 then 429", codes)
	}
	// Reads are not limited.
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/jobs", nil, reqOpts{}), 200)
}

func TestDrivesAndBusyAPI(t *testing.T) {
	h := newHarness(t, true)
	usb := Endpoint{Kind: EPUSB, RefID: "3A4F-1C22", Label: "Sandisk"}
	job := sampleJob("To stick")
	job.Dest = Endpoint{Kind: EPUSB, RefID: usb.RefID, SubPath: "Backups", Label: "Sandisk"}
	rec := doRequest(h.svc, "POST", "/v1/backup/jobs", job, reqOpts{})
	wantStatus(t, rec, 201)
	var created JobDetail
	envelope(t, rec, &created)

	rec = doRequest(h.svc, "GET", "/v1/backup/drives", nil, reqOpts{})
	var drives []RememberedDrive
	envelope(t, rec, &drives)
	if len(drives) != 1 || drives[0].Endpoint.RefID != usb.RefID {
		t.Fatalf("drives: %+v", drives)
	}
	rec = doRequest(h.svc, "PUT", "/v1/backup/drives/"+usb.RefID, RememberedDriveUpdate{Label: "Offsite"}, reqOpts{})
	wantStatus(t, rec, 200)
	wantStatus(t, doRequest(h.svc, "PUT", "/v1/backup/drives/NOPE", RememberedDriveUpdate{Label: "x"}, reqOpts{}), 404)
	// In use by a job: can't forget it.
	wantStatus(t, doRequest(h.svc, "DELETE", "/v1/backup/drives/"+usb.RefID, nil, reqOpts{}), 409)
	wantStatus(t, doRequest(h.svc, "DELETE", "/v1/backup/jobs/"+created.ID, nil, reqOpts{}), 200)
	wantStatus(t, doRequest(h.svc, "DELETE", "/v1/backup/drives/"+usb.RefID, nil, reqOpts{}), 200)

	rec = doRequest(h.svc, "GET", "/v1/backup/busy?kind=app&target=immich", nil, reqOpts{})
	var b BusyResult
	envelope(t, rec, &b)
	if b.Busy {
		t.Errorf("idle app busy: %+v", b)
	}
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/busy?kind=disk&target=x", nil, reqOpts{}), 400)
}

func TestCapabilitiesAndMigrationAPI(t *testing.T) {
	h := newHarness(t, true)
	rec := doRequest(h.svc, "GET", "/v1/backup/capabilities", nil, reqOpts{})
	wantStatus(t, rec, 200)
	var c Capabilities
	envelope(t, rec, &c)
	if !c.Engine.Available || c.Engine.API != engine.APIVersion || !c.ClockArmed || c.MaxConcurrent != DefaultMaxConcurrent ||
		!c.Installed.AppManagement || !c.Installed.VMManager || c.Timezone == "" || c.Version != "test" {
		t.Errorf("capabilities: %+v", c)
	}
	rec = doRequest(h.svc, "GET", "/v1/backup/migration", nil, reqOpts{})
	wantStatus(t, rec, 200)
	rec = doRequest(h.svc, "POST", "/v1/backup/migration/rerun", nil, reqOpts{})
	wantStatus(t, rec, 200)
	var rep MigrationReport
	envelope(t, rec, &rep)
	if rep.State != MigrationDone || rep.Items == nil {
		t.Errorf("migration rerun: %+v", rep)
	}
}

func TestLocationsAPI(t *testing.T) {
	h := newHarness(t, true)
	h.eng.Lock()
	h.eng.LocationsV = []engine.Location{{Kind: EPVolume, RefID: "root-uuid", Label: "System disk", Online: true}}
	h.eng.Unlock()
	rec := doRequest(h.svc, "GET", "/v1/backup/locations?role=dest", nil, reqOpts{})
	wantStatus(t, rec, 200)
	var locs []Location
	envelope(t, rec, &locs)
	if len(locs) == 0 || locs[0].Quirks == nil {
		t.Errorf("locations: %+v", locs)
	}
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/locations?role=sideways", nil, reqOpts{}), 400)
	wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/locations/browse?kind=volume&ref_id=root-uuid&path=../x", nil, reqOpts{}), 400)
	rec = doRequest(h.svc, "GET", "/v1/backup/locations/browse?kind=volume&ref_id=root-uuid&path=DATA&dirs_only=1", nil, reqOpts{})
	wantStatus(t, rec, 200)
	br := h.eng.CallsTo("Browse")
	if len(br) != 1 || !br[0].Req.(engine.BrowseRequest).DirsOnly || br[0].Req.(engine.BrowseRequest).Path != "DATA" {
		t.Errorf("browse request: %+v", br)
	}
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/locations/resolve-path", ResolvePathRequest{Path: "relative"}, reqOpts{}), 400)
	// A path the engine can't place is an answer, not an error.
	rec = doRequest(h.svc, "POST", "/v1/backup/locations/resolve-path", ResolvePathRequest{Path: "/DATA//x/"}, reqOpts{})
	wantStatus(t, rec, 200)
	var rp ResolvePathResult
	envelope(t, rec, &rp)
	calls := h.eng.CallsTo("ResolvePath")
	if rp.OK || rp.Reason != ErrEndpointUnknown || calls[len(calls)-1].Req.(engine.ResolvePathRequest).Path != "/DATA/x" {
		t.Errorf("resolve-path: %+v", rp)
	}
	h.eng.Lock()
	h.eng.ErrResolvePath = &engine.Error{Code: engine.CodeEngineUnavailable}
	h.eng.Unlock()
	rec = doRequest(h.svc, "POST", "/v1/backup/locations/resolve-path", ResolvePathRequest{Path: "/DATA/x"}, reqOpts{})
	if b := errorBody(t, rec); rec.Code != 503 || b.ErrorCode != ErrEngineUnavailable {
		t.Errorf("resolve-path with the engine down: %d %+v", rec.Code, b)
	}
}

func TestDecideAPI(t *testing.T) {
	h := newHarness(t, true)
	j := h.createJob(sampleJob("Docs"))
	runID := h.run(j, KindPreview)
	h.finish(1, engine.Result{Counts: engine.Counts{Added: 3}})
	h.waitStatus(runID, StatusWaitingUser)
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/runs/"+runID+"/decide", DecideRequest{Proceed: true, Mode: "sideways"}, reqOpts{}), 400)
	rec := doRequest(h.svc, "POST", "/v1/backup/runs/"+runID+"/decide", DecideRequest{Proceed: true}, reqOpts{})
	wantStatus(t, rec, 200)
	_, req := h.startedJob(2)
	if req.Op != engine.OpCopy {
		t.Errorf("approved preview ran %s", req.Op)
	}
	h.finish(2, okResult(3))
	r := h.waitStatus(runID, StatusSuccess)
	if RunKind(r.Kind) != KindBackup {
		t.Errorf("approved preview kind %q", r.Kind)
	}
}
