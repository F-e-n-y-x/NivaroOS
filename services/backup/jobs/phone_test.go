package jobs

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
)

// Phone backup (phone_*.go): sessions, tus uploads, check, versions,
// retention, exports, destinations, auth.

type phoneEnv struct {
	t   *testing.T
	h   *harness
	dev DeviceEnrollment

	mu        sync.Mutex
	base      map[string]string // volume ref id -> its mount folder
	unplugged map[string]bool
}

func newPhoneEnv(t *testing.T, opts ...harnessOpt) *phoneEnv {
	t.Helper()
	h := newHarness(t, true, opts...)
	e := &phoneEnv{t: t, h: h, base: map[string]string{"data-uuid": t.TempDir(), "usb-uuid": t.TempDir()}, unplugged: map[string]bool{}}
	h.eng.Lock()
	h.eng.ResolvePathFn = func(req engine.ResolvePathRequest) (engine.ResolvePathResult, error) {
		if strings.HasPrefix(req.Path, "/DATA/") {
			return engine.ResolvePathResult{OK: true, Endpoint: &Endpoint{Kind: EPVolume, RefID: "data-uuid", SubPath: strings.TrimPrefix(req.Path, "/"), Label: "System"}}, nil
		}
		return engine.ResolvePathResult{Reason: ErrEndpointUnknown}, nil
	}
	h.eng.ResolveFn = func(req engine.ResolveRequest) (engine.Resolved, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		base, ok := e.base[req.Endpoint.RefID]
		if !ok || e.unplugged[req.Endpoint.RefID] {
			return engine.Resolved{Root: req.Endpoint.RefID, Quirks: []engine.Quirk{}}, nil
		}
		p := filepath.Join(base, filepath.FromSlash(req.Endpoint.SubPath))
		return engine.Resolved{Root: p, Online: true, LocalPath: p, Quirks: []engine.Quirk{}}, nil
	}
	h.eng.Unlock()
	e.dev = enroll(t, h, "Pixel 8", reqOpts{})
	return e
}

func (e *phoneEnv) unplug(ref string, v bool) {
	e.mu.Lock()
	e.unplugged[ref] = v
	e.mu.Unlock()
	e.h.svc.phone.forgetRoot(e.dev.Device.ID)
}

// root is the phone's folder on the default location.
func (e *phoneEnv) root() string {
	return filepath.Join(e.base["data-uuid"], "DATA", "Backup", "Pixel 8")
}

func (e *phoneEnv) req(method, p string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	return e.reqAs(e.dev.Token, method, p, body, headers)
}

func (e *phoneEnv) reqAs(tok, method, p string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	if !strings.HasPrefix(p, "/v1/backup") {
		p = "/v1/backup/devices/" + e.dev.Device.ID + p
	}
	return doRequest(e.h.svc, method, p, body, reqOpts{remote: remoteBrowser, token: tok, headers: headers})
}

func (e *phoneEnv) startSession(cats ...PhoneCategory) PhoneSession {
	e.t.Helper()
	rec := e.req("POST", "/sessions", SessionStartRequest{Categories: cats, Reason: "manual"}, nil)
	if rec.Code != 201 && rec.Code != 200 {
		e.t.Fatalf("start session: HTTP %d %s", rec.Code, rec.Body.String())
	}
	var s PhoneSession
	envelope(e.t, rec, &s)
	return s
}

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func tusMeta(kv map[string]string) string {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+" "+base64.StdEncoding.EncodeToString([]byte(kv[k])))
	}
	return strings.Join(parts, ",")
}

// create starts an upload and returns its answer.
func (e *phoneEnv) create(sid string, cat PhoneCategory, key, value string, data []byte, extra map[string]string) (*httptest.ResponseRecorder, UploadCreated) {
	e.t.Helper()
	meta := map[string]string{"session": sid, "category": string(cat), "sha256": shaHex(data), "mtime": "1700000000000"}
	if key != "" {
		meta[key] = value
	}
	for k, v := range extra {
		meta[k] = v
	}
	rec := e.req("POST", "/uploads", nil, map[string]string{
		"Tus-Resumable": TusVersion, "Upload-Length": strconv.Itoa(len(data)), "Upload-Metadata": tusMeta(meta),
	})
	var c UploadCreated
	if rec.Code < 300 {
		envelope(e.t, rec, &c)
	}
	return rec, c
}

func (e *phoneEnv) patch(id string, offset int, chunk []byte) *httptest.ResponseRecorder {
	return e.req("PATCH", "/uploads/"+id, chunk, map[string]string{
		"Tus-Resumable": TusVersion, "Content-Type": tusContentType, "Upload-Offset": strconv.Itoa(offset),
	})
}

// put uploads a whole file (in one PATCH) and wants it stored.
func (e *phoneEnv) put(sid string, cat PhoneCategory, key, value string, data []byte) string {
	e.t.Helper()
	rec, c := e.create(sid, cat, key, value, data, nil)
	if rec.Code != 201 && rec.Code != 200 {
		e.t.Fatalf("create %s: HTTP %d %s", value, rec.Code, rec.Body.String())
	}
	if c.UploadID == "" {
		return c.Status
	}
	rec = e.patch(c.UploadID, 0, data)
	wantStatus(e.t, rec, 204)
	return rec.Header().Get(UploadResultHeader)
}

func (e *phoneEnv) check(sid string, cat PhoneCategory, items ...CheckItem) []CheckAnswer {
	e.t.Helper()
	rec := e.req("POST", "/sessions/"+sid+"/check", CheckRequest{Category: cat, Items: items}, nil)
	wantStatus(e.t, rec, 200)
	var res CheckResult
	envelope(e.t, rec, &res)
	return res.Items
}

func (e *phoneEnv) finish(sid string, st RunStatus) FinishResult {
	e.t.Helper()
	rec := e.req("POST", "/sessions/"+sid+"/finish", FinishRequest{Status: st}, nil)
	wantStatus(e.t, rec, 200)
	var res FinishResult
	envelope(e.t, rec, &res)
	return res
}

func statuses(ans []CheckAnswer) []string {
	out := make([]string, len(ans))
	for i, a := range ans {
		out[i] = a.Status
	}
	return out
}

func TestPhoneSessionUploadAndBrowse(t *testing.T) {
	e := newPhoneEnv(t)
	rec := e.req("GET", "/config", nil, nil)
	wantStatus(t, rec, 200)
	var cfg DeviceConfig
	envelope(t, rec, &cfg)
	if cfg.Limits.ChunkSize != PhoneChunkSize || !cfg.Destination.Default || len(cfg.Categories) != len(PhoneCategories) {
		t.Fatalf("config %+v", cfg)
	}

	ses := e.startSession(CatMedia, CatContacts)
	if ses.RunID == "" || ses.Status != SessionOpen {
		t.Fatalf("session %+v", ses)
	}
	// The same categories again: the open session comes back.
	if again := e.startSession(CatContacts, CatMedia); again.ID != ses.ID || !again.Resumed {
		t.Fatalf("second start %+v", again)
	}
	photo := bytes.Repeat([]byte("jpegdata"), 1000)
	ans := e.check(ses.ID, CatMedia,
		CheckItem{Path: "DCIM/Camera/a.jpg", Size: int64(len(photo)), MTime: 1700000000000},
		CheckItem{Path: "DCIM/Camera/a.jpg", Size: int64(len(photo)), MTime: 1700000000000, SHA256: shaHex(photo)})
	if got := statuses(ans); got[0] != CheckNeedHash || got[1] != CheckNeed {
		t.Fatalf("check %v", got)
	}

	// Resumable upload: two chunks, a HEAD between, a wrong offset.
	rec, c := e.create(ses.ID, CatMedia, "path", "DCIM/Camera/a.jpg", photo, map[string]string{"taken_at": "1699999999000", "media_id": "42"})
	wantStatus(t, rec, 201)
	if c.Status != UploadCreatedStatus || c.Offset != 0 || !strings.HasSuffix(rec.Header().Get("Location"), "/uploads/"+c.UploadID) {
		t.Fatalf("created %+v %v", c, rec.Header())
	}
	wantStatus(t, e.patch(c.UploadID, 0, photo[:3000]), 204)
	head := e.req("HEAD", "/uploads/"+c.UploadID, nil, nil)
	if head.Code != 200 || head.Header().Get("Upload-Offset") != "3000" || head.Header().Get("Upload-Length") != strconv.Itoa(len(photo)) {
		t.Fatalf("HEAD %d %v", head.Code, head.Header())
	}
	rec = e.patch(c.UploadID, 0, photo[3000:])
	wantStatus(t, rec, 409)
	if b := errorBody(t, rec); b.ErrorCode != ErrOffsetMismatch || rec.Header().Get("Upload-Offset") != "3000" {
		t.Fatalf("offset mismatch %+v", b)
	}
	// A new create for the same file resumes it.
	_, again := e.create(ses.ID, CatMedia, "path", "DCIM/Camera/a.jpg", photo, nil)
	if again.UploadID != c.UploadID || again.Status != UploadResumed || again.Offset != 3000 {
		t.Fatalf("resume %+v", again)
	}
	// check also points at it.
	if a := e.check(ses.ID, CatMedia, CheckItem{Path: "DCIM/Camera/a.jpg", Size: int64(len(photo)), SHA256: shaHex(photo)}); a[0].UploadID != c.UploadID || a[0].Offset != 3000 {
		t.Fatalf("check resume %+v", a)
	}
	rec = e.patch(c.UploadID, 3000, photo[3000:])
	wantStatus(t, rec, 204)
	if rec.Header().Get(UploadResultHeader) != UploadStored || rec.Header().Get("Upload-Offset") != strconv.Itoa(len(photo)) {
		t.Fatalf("last PATCH %v", rec.Header())
	}
	stored := filepath.Join(e.root(), "media", "DCIM", "Camera", "a.jpg")
	got, err := os.ReadFile(stored)
	if err != nil || !bytes.Equal(got, photo) {
		t.Fatalf("stored file: %v", err)
	}
	if fi, _ := os.Stat(stored); fi.ModTime().UnixMilli() != 1700000000000 {
		t.Errorf("mtime %v", fi.ModTime())
	}
	if _, err := os.Stat(filepath.Join(e.root(), phoneMarkerFile)); err != nil {
		t.Errorf("marker: %v", err)
	}
	wantStatus(t, e.req("HEAD", "/uploads/"+c.UploadID, nil, nil), 404)
	if a := e.check(ses.ID, CatMedia, CheckItem{Path: "DCIM/Camera/a.jpg", Size: int64(len(photo)), MTime: 1700000000000}); a[0].Status != CheckHave {
		t.Fatalf("after upload %+v", a)
	}

	vcf := []byte("BEGIN:VCARD\nFN:A\nEND:VCARD\nBEGIN:VCARD\nFN:B\nEND:VCARD\n")
	if r := e.put(ses.ID, CatContacts, "", "", vcf); r != UploadStored {
		t.Fatalf("contacts %q", r)
	}
	// An unchanged export isn't stored twice.
	if r := e.put(ses.ID, CatContacts, "", "", vcf); r != UploadUnchanged {
		t.Fatalf("contacts again %q", r)
	}
	// A category the session doesn't cover is refused.
	rec, _ = e.create(ses.ID, CatSMS, "", "", []byte("<smses/>"), nil)
	wantStatus(t, rec, 400)

	fin := e.finish(ses.ID, StatusSuccess)
	if fin.Snapshot == nil || fin.Session.Status != SessionFinished || fin.Session.Counts.Uploaded != 2 || fin.Session.Counts.Bytes != int64(len(photo)+2*len(vcf)) {
		t.Fatalf("finish %+v", fin)
	}
	run, err := e.h.svc.store.GetRun(ses.RunID)
	if err != nil || run.Status != string(StatusSuccess) || run.Kind != string(KindDevice) || run.JobID != e.dev.Device.ID {
		t.Fatalf("run %+v %v", run, err)
	}
	var detail RunDetail
	envelope(t, doRequest(e.h.svc, "GET", "/v1/backup/runs/"+run.ID, nil, reqOpts{}), &detail)
	if detail.JobName != "Pixel 8" {
		t.Errorf("run job_name %q", detail.JobName)
	}
	// Closed session: no more uploads.
	rec, _ = e.create(ses.ID, CatMedia, "path", "x.jpg", []byte("x"), nil)
	wantStatus(t, rec, 409)
	if b := errorBody(t, rec); b.ErrorCode != ErrSessionClosed {
		t.Fatalf("closed session %+v", b)
	}

	// Browse, content, exports, restore manifest.
	var snaps []PhoneSnapshot
	envelope(t, e.req("GET", "/snapshots", nil, nil), &snaps)
	if len(snaps) != 1 || snaps[0].ID != fin.Snapshot.ID {
		t.Fatalf("snapshots %+v", snaps)
	}
	var br PhoneBrowseResult
	envelope(t, e.req("GET", "/snapshots/"+snaps[0].ID+"/browse?category=media", nil, nil), &br)
	if len(br.Entries) != 1 || !br.Entries[0].Dir || br.Entries[0].Name != "DCIM" {
		t.Fatalf("browse top %+v", br.Entries)
	}
	envelope(t, e.req("GET", "/snapshots/latest/browse?category=media&path=DCIM/Camera", nil, nil), &br)
	if len(br.Entries) != 1 || br.Entries[0].Name != "a.jpg" || br.Entries[0].MediaID != 42 || br.Entries[0].TakenAt != 1699999999000 {
		t.Fatalf("browse camera %+v", br.Entries)
	}
	rec = e.req("GET", "/files/content?category=media&path=DCIM/Camera/a.jpg&snapshot="+snaps[0].ID, nil, map[string]string{"Range": "bytes=0-7"})
	if rec.Code != 206 || rec.Body.String() != "jpegdata" || rec.Header().Get("X-NivaroOS-SHA256") != shaHex(photo) {
		t.Fatalf("content %d %q", rec.Code, rec.Body.String())
	}
	var exps []PhoneExport
	envelope(t, e.req("GET", "/exports?category=contacts", nil, nil), &exps)
	if len(exps) != 1 || exps[0].Items != 2 {
		t.Fatalf("exports %+v", exps)
	}
	rec = e.req("GET", "/exports/"+exps[0].ID+"/content", nil, nil)
	if rec.Code != 200 || rec.Body.String() != string(vcf) {
		t.Fatalf("export content %d", rec.Code)
	}
	var man RestoreManifest
	envelope(t, e.req("GET", "/restore-manifest?snapshot="+snaps[0].ID, nil, nil), &man)
	if len(man.Items) != 1 || len(man.Exports) != 1 || man.NextAfter != "" || !strings.Contains(man.Items[0].Content, "files/content?") {
		t.Fatalf("manifest %+v", man)
	}
	rec = e.req("GET", man.Items[0].Content[len("/devices/"+e.dev.Device.ID):], nil, nil)
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), photo) {
		t.Fatalf("manifest content %d", rec.Code)
	}

	// The owner's view.
	var dd DeviceDetail
	envelope(t, doRequest(e.h.svc, "GET", "/v1/backup/devices/"+e.dev.Device.ID, nil, reqOpts{}), &dd)
	if dd.Snapshots != 1 || dd.Device.LastBackupAt == nil || dd.Device.LastStatus != StatusSuccess || dd.Device.SizeBytes != int64(len(photo)+len(vcf)) || !dd.Destination.Online {
		t.Fatalf("detail %+v", dd)
	}
	for _, c := range dd.Categories {
		if c.Category == CatMedia && (c.Files != 1 || c.LastBackupAt == nil || c.LastStatus != StatusSuccess) {
			t.Errorf("media %+v", c)
		}
		if c.Category == CatSMS && c.LastBackupAt != nil {
			t.Errorf("sms %+v", c)
		}
	}
	// Web download of the photo.
	rec = doRequest(e.h.svc, "POST", "/v1/backup/devices/"+e.dev.Device.ID+"/downloads", DeviceDownloadRequest{Category: CatMedia, Path: "DCIM/Camera/a.jpg"}, reqOpts{})
	wantStatus(t, rec, 200)
	var dt DownloadToken
	envelope(t, rec, &dt)
	rec = doRequest(e.h.svc, "GET", "/v1/backup/downloads/"+dt.Token, nil, reqOpts{})
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), photo) || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("download %d %v", rec.Code, rec.Header())
	}
}

func TestPhoneChecksumAndLimits(t *testing.T) {
	e := newPhoneEnv(t)
	ses := e.startSession(CatMedia)
	data := []byte("hello world")
	meta := map[string]string{"session": ses.ID, "category": "media", "path": "a.txt", "sha256": shaHex([]byte("something else"))}
	rec := e.req("POST", "/uploads", nil, map[string]string{"Upload-Length": strconv.Itoa(len(data)), "Upload-Metadata": tusMeta(meta)})
	wantStatus(t, rec, 201)
	var c UploadCreated
	envelope(t, rec, &c)
	rec = e.patch(c.UploadID, 0, data)
	wantStatus(t, rec, 422)
	if b := errorBody(t, rec); b.ErrorCode != ErrChecksumMismatch {
		t.Fatalf("%+v", b)
	}
	if _, err := os.Stat(filepath.Join(e.root(), "media", "a.txt")); err == nil {
		t.Fatal("a mismatching file was placed")
	}
	wantStatus(t, e.req("HEAD", "/uploads/"+c.UploadID, nil, nil), 404)

	// A body past Upload-Length.
	_, c = e.create(ses.ID, CatMedia, "path", "b.txt", data, nil)
	rec = e.patch(c.UploadID, 0, append(append([]byte{}, data...), 'x'))
	wantStatus(t, rec, 413)
	head := e.req("HEAD", "/uploads/"+c.UploadID, nil, nil)
	if head.Header().Get("Upload-Offset") != "0" {
		t.Fatalf("offset after refused chunk %v", head.Header())
	}
	// Wrong content type; too big a file.
	rec = e.req("PATCH", "/uploads/"+c.UploadID, data, map[string]string{"Content-Type": "application/json", "Upload-Offset": "0"})
	wantStatus(t, rec, 415)
	rec = e.req("POST", "/uploads", nil, map[string]string{"Upload-Length": strconv.FormatInt(PhoneMaxUploadSize+1, 10), "Upload-Metadata": tusMeta(meta)})
	wantStatus(t, rec, 413)
	// Empty file: stored at creation.
	rec, c = e.create(ses.ID, CatMedia, "path", "empty.txt", nil, nil)
	wantStatus(t, rec, 201)
	if c.Status != UploadStored {
		t.Fatalf("empty %+v", c)
	}
	if fi, err := os.Stat(filepath.Join(e.root(), "media", "empty.txt")); err != nil || fi.Size() != 0 {
		t.Fatalf("empty file %v", err)
	}
	// DELETE abandons.
	_, c = e.create(ses.ID, CatMedia, "path", "c.txt", data, nil)
	wantStatus(t, e.req("DELETE", "/uploads/"+c.UploadID, nil, nil), 204)
	if _, err := os.Stat(uploadPart(e.root(), c.UploadID)); err == nil {
		t.Error("part file left")
	}
}

func TestPhoneIncrementalCheck(t *testing.T) {
	e := newPhoneEnv(t)
	ses := e.startSession(CatMedia, CatFiles)
	a := []byte("photo A")
	e.put(ses.ID, CatMedia, "path", "DCIM/a.jpg", a)
	b := []byte("photo B")
	e.put(ses.ID, CatMedia, "path", "DCIM/b.jpg", b)
	// Owner excludes b's content.
	wantStatus(t, doRequest(e.h.svc, "POST", "/v1/backup/devices/"+e.dev.Device.ID+"/excluded", ExcludedUpdate{Add: []PhoneExcluded{{SHA256: shaHex(b), Path: "DCIM/b.jpg"}}}, reqOpts{}), 200)

	ans := e.check(ses.ID, CatMedia,
		CheckItem{Path: "DCIM/a.jpg", Size: int64(len(a)), MTime: 1700000000000},         // have by size+mtime
		CheckItem{Path: "DCIM/a.jpg", Size: int64(len(a)), MTime: 1},                     // mtime changed: need_hash
		CheckItem{Path: "Moved/a.jpg", Size: int64(len(a)), MTime: 5, SHA256: shaHex(a)}, // same content elsewhere
		CheckItem{Path: "DCIM/b2.jpg", Size: int64(len(b)), SHA256: shaHex(b)},           // excluded
		CheckItem{Path: "../etc/passwd", Size: 1},                                        // invalid
		CheckItem{Path: "/abs", Size: 1},                                                 // invalid
		CheckItem{Path: "a/\x00b", Size: 1},                                              // invalid
		CheckItem{Path: strings.Repeat("x", 300), Size: 1},                               // segment too long
		CheckItem{Path: "new.jpg", Size: 3, SHA256: shaHex([]byte("new"))},               // need
		CheckItem{Path: "bad-sha.jpg", Size: 3, SHA256: "xyz"},                           // invalid
	)
	want := []string{CheckHave, CheckNeedHash, CheckHaveElsewhere, CheckExcluded, CheckInvalid, CheckInvalid, CheckInvalid, CheckInvalid, CheckNeed, CheckInvalid}
	if got := statuses(ans); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("check\n got %v\nwant %v", got, want)
	}
	if got, err := os.ReadFile(filepath.Join(e.root(), "media", "Moved", "a.jpg")); err != nil || !bytes.Equal(got, a) {
		t.Fatalf("linked copy: %v", err)
	}
	// Uploading excluded content is refused politely.
	if r := e.put(ses.ID, CatMedia, "path", "DCIM/b3.jpg", b); r != UploadExcluded {
		t.Fatalf("excluded upload %q", r)
	}

	// Non-ASCII folders browse and delete by characters, not bytes.
	e.put(ses.ID, CatFiles, "path", "Документы/Отчёт.pdf", []byte("pdf"))
	var ub PhoneBrowseResult
	envelope(t, e.req("GET", "/snapshots/latest/browse?category=files&path="+url.QueryEscape("Документы"), nil, nil), &ub)
	if len(ub.Entries) != 1 || ub.Entries[0].Name != "Отчёт.pdf" {
		t.Fatalf("unicode browse %+v", ub.Entries)
	}
	envelope(t, e.req("GET", "/snapshots/latest/browse?category=files", nil, nil), &ub)
	if len(ub.Entries) != 1 || !ub.Entries[0].Dir || ub.Entries[0].Name != "Документы" {
		t.Fatalf("unicode top %+v", ub.Entries)
	}
	wantStatus(t, doRequest(e.h.svc, "DELETE", "/v1/backup/devices/"+e.dev.Device.ID+"/files?category=files&path="+url.QueryEscape("Документы"), nil, reqOpts{}), 200)
	if a := e.check(ses.ID, CatFiles, CheckItem{Path: "Документы/Отчёт.pdf", Size: 3, SHA256: shaHex([]byte("pdf"))}); a[0].Status != CheckExcluded {
		t.Fatalf("after owner delete %+v", a)
	}

	// Deleted on the phone: marked, hidden, then back again.
	rec := e.req("POST", "/sessions/"+ses.ID+"/deleted", DeletedRequest{Category: CatMedia, Paths: []string{"DCIM/a.jpg", "never.jpg"}}, nil)
	wantStatus(t, rec, 200)
	var dr DeletedResult
	envelope(t, rec, &dr)
	if dr.Marked != 1 || dr.Unknown != 1 {
		t.Fatalf("deleted %+v", dr)
	}
	var br PhoneBrowseResult
	envelope(t, e.req("GET", "/snapshots/latest/browse?category=media&path=DCIM", nil, nil), &br)
	for _, en := range br.Entries {
		if en.Name == "a.jpg" {
			t.Fatal("a deleted file is listed without include_deleted")
		}
	}
	envelope(t, e.req("GET", "/snapshots/latest/browse?category=media&path=DCIM&include_deleted=1", nil, nil), &br)
	found := false
	for _, en := range br.Entries {
		if en.Name == "a.jpg" && en.DeletedOnDeviceAt != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("include_deleted %+v", br.Entries)
	}
	e.check(ses.ID, CatMedia, CheckItem{Path: "DCIM/a.jpg", Size: int64(len(a)), MTime: 1700000000000})
	var row PhoneFileRow
	e.h.svc.store.db.Where("device_id = ? AND path = ?", e.dev.Device.ID, "DCIM/a.jpg").Take(&row)
	if row.DeletedOnDeviceAt != nil {
		t.Error("a file seen again keeps its deleted mark")
	}
	// A damaged file (verify) is asked for again.
	os.Remove(filepath.Join(e.root(), "media", "DCIM", "a.jpg"))
	rec = e.req("POST", "/verify", VerifyRequest{}, nil)
	wantStatus(t, rec, 200)
	var vr VerifyResult
	envelope(t, rec, &vr)
	if vr.Missing != 1 || vr.Checked != 3 {
		t.Fatalf("verify %+v", vr)
	}
	if a := e.check(ses.ID, CatMedia, CheckItem{Path: "DCIM/a.jpg", Size: int64(len(a)), MTime: 1700000000000, SHA256: shaHex(a)}); a[0].Status == CheckHave {
		t.Fatalf("damaged file answered %+v", a)
	}
	// check-items needs an incremental category of the session.
	wantStatus(t, e.req("POST", "/sessions/"+ses.ID+"/check-items", ItemKeysRequest{Category: CatSMS, Keys: []string{"k"}}, nil), 400)
	// Too many items.
	many := make([]CheckItem, PhoneCheckBatch+1)
	wantStatus(t, e.req("POST", "/sessions/"+ses.ID+"/check", CheckRequest{Category: CatMedia, Items: many}, nil), 400)
}

func TestPhoneVersionsAndRetention(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	e := newPhoneEnv(t, withNow(clock))
	v1, v2 := []byte("version one"), []byte("version two!")

	s1 := e.startSession(CatMedia, CatSettings)
	e.put(s1.ID, CatMedia, "path", "doc.txt", v1)
	e.put(s1.ID, CatSettings, "", "", []byte(`{"a":1}`))
	snap1 := e.finish(s1.ID, StatusSuccess).Snapshot
	advance(time.Hour)
	s2 := e.startSession(CatMedia, CatSettings)
	e.put(s2.ID, CatMedia, "path", "doc.txt", v2)
	e.put(s2.ID, CatSettings, "", "", []byte(`{"a":2}`))
	snap2 := e.finish(s2.ID, StatusSuccess).Snapshot

	content := func(snap string) string {
		rec := e.req("GET", "/files/content?category=media&path=doc.txt&snapshot="+snap, nil, nil)
		wantStatus(t, rec, 200)
		return rec.Body.String()
	}
	if content(snap1.ID) != string(v1) || content(snap2.ID) != string(v2) || content("latest") != string(v2) {
		t.Fatal("snapshot contents")
	}
	var br PhoneBrowseResult
	envelope(t, e.req("GET", "/snapshots/"+snap1.ID+"/browse?category=media", nil, nil), &br)
	if len(br.Entries) != 1 || !br.Entries[0].Version || br.Entries[0].Size != int64(len(v1)) {
		t.Fatalf("browse snap1 %+v", br.Entries)
	}
	var exps []PhoneExport
	envelope(t, e.req("GET", "/exports?category=settings&snapshot="+snap1.ID, nil, nil), &exps)
	if len(exps) != 1 {
		t.Fatalf("exports at snap1 %+v", exps)
	}
	envelope(t, e.req("GET", "/exports?all=1", nil, nil), &exps)
	if len(exps) != 2 {
		t.Fatalf("all exports %+v", exps)
	}

	// Keep only the newest snapshot: the old version and export go.
	one := DeviceSettings{KeepLast: 1, KeepDays: 0, StaleDays: 7}
	rec := doRequest(e.h.svc, "PUT", "/v1/backup/devices/"+e.dev.Device.ID, DeviceUpdateRequest{Settings: &one}, reqOpts{})
	wantStatus(t, rec, 200)
	var snaps []PhoneSnapshot
	envelope(t, e.req("GET", "/snapshots", nil, nil), &snaps)
	if len(snaps) != 1 || snaps[0].ID != snap2.ID {
		t.Fatalf("after prune %+v", snaps)
	}
	var nv int64
	e.h.svc.store.db.Model(&PhoneVersionRow{}).Count(&nv)
	if nv != 0 {
		t.Errorf("%d versions left", nv)
	}
	entries, _ := os.ReadDir(filepath.Join(e.root(), phoneVersionsDir))
	if len(entries) != 0 {
		t.Errorf("versions folder not emptied: %v", entries)
	}
	envelope(t, e.req("GET", "/exports?all=1", nil, nil), &exps)
	if len(exps) != 1 {
		t.Errorf("exports after prune %+v", exps)
	}
	wantStatus(t, e.req("GET", "/snapshots/"+snap1.ID+"/browse?category=media", nil, nil), 404)

	// deleted_purge_days removes files the phone deleted long ago.
	s3 := e.startSession(CatMedia)
	e.req("POST", "/sessions/"+s3.ID+"/deleted", DeletedRequest{Category: CatMedia, Paths: []string{"doc.txt"}}, nil)
	e.finish(s3.ID, StatusSuccess)
	advance(40 * 24 * time.Hour)
	purge := DeviceSettings{KeepLast: 1, DeletedPurgeDays: 30, StaleDays: 7}
	wantStatus(t, doRequest(e.h.svc, "PUT", "/v1/backup/devices/"+e.dev.Device.ID, DeviceUpdateRequest{Settings: &purge}, reqOpts{}), 200)
	if _, err := os.Stat(filepath.Join(e.root(), "media", "doc.txt")); err == nil {
		t.Error("purged file still on disk")
	}
	// Stale: 40 days without a backup warns once.
	e.h.svc.checkDeviceStale()
	e.h.svc.checkDeviceStale()
	if !e.h.notified("backup.notify.device_stale") {
		t.Error("no stale notification")
	}
	n := 0
	for _, k := range e.h.bus.notifications() {
		if k == "backup.notify.device_stale" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d stale notifications", n)
	}
}

func TestKeptSnapshots(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	var snaps []PhoneSnapshotRow
	for i := 0; i < 20; i++ { // two a day, newest first
		snaps = append(snaps, PhoneSnapshotRow{ID: fmt.Sprint(i), TakenAt: now.Add(-time.Duration(i) * 12 * time.Hour)})
	}
	kept := keptSnapshots(snaps, DeviceSettings{KeepLast: 3, KeepDays: 5}, now)
	// 0,1,2 by count (0 and 1 are both today); the newest of each of
	// the last 5 days: 0, 2, 4, 6, 8, and 10 (exactly 5 days ago).
	for _, id := range []string{"0", "1", "2", "4", "6", "8", "10"} {
		if !kept[id] {
			t.Errorf("snapshot %s not kept", id)
		}
	}
	if kept["3"] || kept["5"] || kept["11"] || kept["19"] {
		t.Errorf("kept too much: %v", kept)
	}
}

func TestPhoneMissingDriveNeverFallsBack(t *testing.T) {
	e := newPhoneEnv(t)
	ses := e.startSession(CatMedia)
	e.put(ses.ID, CatMedia, "path", "a.jpg", []byte("a"))
	e.finish(ses.ID, StatusSuccess)

	// Move the phone to the USB drive, then unplug it.
	usb := Endpoint{Kind: EPUSB, RefID: "usb-uuid", SubPath: "Phones"}
	rec := doRequest(e.h.svc, "POST", "/v1/backup/devices/"+e.dev.Device.ID+"/destination", DeviceDestRequest{Location: &usb}, reqOpts{})
	wantStatus(t, rec, 400) // has backups: mode required
	if b := errorBody(t, rec); b.FieldErrors["mode"] != string(FieldRequired) {
		t.Fatalf("%+v", b)
	}
	rec = doRequest(e.h.svc, "POST", "/v1/backup/devices/"+e.dev.Device.ID+"/destination", DeviceDestRequest{Location: &usb, Mode: DestModeMove}, reqOpts{})
	wantStatus(t, rec, 200)
	usbRoot := filepath.Join(e.base["usb-uuid"], "Phones", "Pixel 8")
	if _, err := os.Stat(filepath.Join(usbRoot, "media", "a.jpg")); err != nil {
		t.Fatalf("moved file: %v", err)
	}
	if _, err := os.Stat(e.root()); err == nil {
		t.Fatal("old folder left behind after a move")
	}

	e.unplug("usb-uuid", true)
	rec = e.req("POST", "/sessions", SessionStartRequest{Categories: []PhoneCategory{CatMedia}}, nil)
	wantStatus(t, rec, 409)
	if b := errorBody(t, rec); b.ErrorCode != ErrDestOffline {
		t.Fatalf("%+v", b)
	}
	var cfg DeviceConfig
	envelope(t, e.req("GET", "/config", nil, nil), &cfg)
	if cfg.Destination.Online || cfg.Destination.ErrorCode != ErrDestOffline || cfg.Destination.Default {
		t.Fatalf("destination while unplugged %+v", cfg.Destination)
	}
	// Nothing was written anywhere else (the "system disk").
	if _, err := os.Stat(e.root()); err == nil {
		t.Fatal("a folder appeared on the default location")
	}
	// Browsing while the drive is missing: refused, not empty.
	wantStatus(t, e.req("GET", "/files/content?category=media&path=a.jpg", nil, nil), 409)

	// Plugged back: works. Then the drive vanishes mid-session - the
	// folder is gone from under the cached root: the next write is
	// refused, and nothing is recreated on the bare mount point.
	e.unplug("usb-uuid", false)
	ses = e.startSession(CatMedia)
	_, c := e.create(ses.ID, CatMedia, "path", "b.jpg", []byte("bbbb"), nil)
	if err := os.RemoveAll(usbRoot); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.unplugged["usb-uuid"] = true // no forgetRoot: the cache still holds the old root
	e.mu.Unlock()
	rec = e.patch(c.UploadID, 0, []byte("bbbb"))
	wantStatus(t, rec, 409)
	if b := errorBody(t, rec); b.ErrorCode != ErrDestOffline {
		t.Fatalf("%+v", b)
	}
	if _, err := os.Stat(usbRoot); err == nil {
		t.Fatal("the folder of an unplugged drive was recreated")
	}
}

func TestPhoneDestinationFreshAndRules(t *testing.T) {
	e := newPhoneEnv(t)
	ses := e.startSession(CatMedia)
	e.put(ses.ID, CatMedia, "path", "a.jpg", []byte("a"))
	e.finish(ses.ID, StatusSuccess)
	id := e.dev.Device.ID
	dest := func(req DeviceDestRequest) *httptest.ResponseRecorder {
		return doRequest(e.h.svc, "POST", "/v1/backup/devices/"+id+"/destination", req, reqOpts{})
	}
	// Refused while a session is open.
	open := e.startSession(CatMedia)
	usb := Endpoint{Kind: EPUSB, RefID: "usb-uuid", SubPath: "P"}
	wantStatus(t, dest(DeviceDestRequest{Location: &usb, Mode: DestModeFresh}), 409)
	e.finish(open.ID, StatusSuccess)
	// Not inside the current folder; not a cloud; not a non-empty folder.
	inside := Endpoint{Kind: EPVolume, RefID: "data-uuid", SubPath: "DATA/Backup/Pixel 8"}
	wantStatus(t, dest(DeviceDestRequest{Location: &inside, Mode: DestModeFresh}), 400)
	cloud := Endpoint{Kind: EPCloud, RefID: "gdrive", SubPath: "x"}
	wantStatus(t, dest(DeviceDestRequest{Location: &cloud, Mode: DestModeFresh}), 400)
	os.MkdirAll(filepath.Join(e.base["usb-uuid"], "P", "Pixel 8", "junk"), 0o755)
	wantStatus(t, dest(DeviceDestRequest{Location: &usb, Mode: DestModeFresh}), 409)
	os.RemoveAll(filepath.Join(e.base["usb-uuid"], "P"))
	// Not inside another job's destination.
	job := sampleJob("usbjob")
	job.Dest = Endpoint{Kind: EPUSB, RefID: "usb-uuid", SubPath: "P", Match: &DevMatch{Serial: "s"}}
	job.Type = TypeMirror
	j := e.h.createJob(job)
	wantStatus(t, dest(DeviceDestRequest{Location: &usb, Mode: DestModeFresh}), 409)
	doRequest(e.h.svc, "DELETE", "/v1/backup/jobs/"+j.ID, nil, reqOpts{})

	rec := dest(DeviceDestRequest{Location: &usb, Mode: DestModeFresh})
	wantStatus(t, rec, 200)
	var dd DeviceDetail
	envelope(t, rec, &dd)
	if dd.Snapshots != 0 || dd.Destination.Default || dd.Device.Dest == nil || dd.Device.Dest.RefID != "usb-uuid" {
		t.Fatalf("after fresh %+v", dd)
	}
	// The old backups stay on disk; the new place starts empty.
	if _, err := os.Stat(filepath.Join(e.root(), "media", "a.jpg")); err != nil {
		t.Fatalf("old files: %v", err)
	}
	ses = e.startSession(CatMedia)
	if a := e.check(ses.ID, CatMedia, CheckItem{Path: "a.jpg", Size: 1, SHA256: shaHex([]byte("a"))}); a[0].Status != CheckNeed {
		t.Fatalf("fresh start %+v", a)
	}
	e.finish(ses.ID, StatusSuccess)
	// Back to the default: its old folder is not empty, so refused.
	wantStatus(t, dest(DeviceDestRequest{Mode: DestModeFresh}), 409)

	// Rename moves the folder along.
	name := "Anna phone"
	wantStatus(t, doRequest(e.h.svc, "PUT", "/v1/backup/devices/"+id, DeviceUpdateRequest{Name: &name}, reqOpts{}), 200)
	if _, err := os.Stat(filepath.Join(e.base["usb-uuid"], "P", "Anna phone", phoneMarkerFile)); err != nil {
		t.Fatalf("renamed folder: %v", err)
	}
}

func TestCopyTree(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(src, "media", "DCIM"), 0o755)
	os.MkdirAll(filepath.Join(src, phoneUploadsDir), 0o755)
	os.WriteFile(filepath.Join(src, "media", "DCIM", "a.jpg"), []byte("abc"), 0o644)
	os.WriteFile(filepath.Join(src, phoneUploadsDir, "x.part"), []byte("zz"), 0o644)
	mt := time.Unix(1700000000, 0)
	os.Chtimes(filepath.Join(src, "media", "DCIM", "a.jpg"), mt, mt)
	var files int64
	if err := copyTree(t.Context(), src, dst, func(f, b int64) { files = f }); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dst, "media", "DCIM", "a.jpg"))
	if err != nil || !fi.ModTime().Equal(mt) || files != 1 {
		t.Fatalf("copy %v %v %d", err, fi, files)
	}
	if _, err := os.Stat(filepath.Join(dst, phoneUploadsDir)); err == nil {
		t.Error("uploads copied")
	}
}

func TestPhoneAuthScoping(t *testing.T) {
	key := newKey(t)
	e := newPhoneEnv(t, withPublicKey(&key.PublicKey))
	admin, _ := jwt.GetAccessToken("owner", key, 1)
	viewer := signES256(t, key, map[string]interface{}{"username": "kid", "id": 2, "iss": "nivaroos", "exp": time.Now().Add(time.Hour).Unix(), "role": "user"})
	other := enroll(t, e.h, "Other", reqOpts{})
	ses := e.startSession(CatMedia)

	// Another phone's token: 401 on this phone's routes.
	for _, rt := range []struct{ m, p string }{{"POST", "/sessions"}, {"GET", "/config"}, {"POST", "/uploads"}, {"GET", "/snapshots"}, {"POST", "/sessions/" + ses.ID + "/finish"}} {
		if rec := e.reqAs(other.Token, rt.m, rt.p, SessionStartRequest{}, nil); rec.Code != 401 {
			t.Errorf("other phone on %s %s: HTTP %d", rt.m, rt.p, rec.Code)
		}
	}
	// The other phone can't use this phone's session through its own id.
	rec := doRequest(e.h.svc, "POST", "/v1/backup/devices/"+other.Device.ID+"/sessions/"+ses.ID+"/finish", FinishRequest{Status: StatusSuccess}, reqOpts{remote: remoteBrowser, token: other.Token})
	wantStatus(t, rec, 404)
	// The admin JWT can't upload or run sessions (device only)...
	for _, rt := range []struct{ m, p string }{{"POST", "/sessions"}, {"POST", "/uploads"}, {"PUT", "/phone-settings"}, {"POST", "/sessions/" + ses.ID + "/check"}} {
		if rec := e.reqAs(admin, rt.m, rt.p, SessionStartRequest{}, nil); rec.Code != 401 {
			t.Errorf("admin JWT on %s %s: HTTP %d", rt.m, rt.p, rec.Code)
		}
	}
	// ...but reads the backup; a viewer can't.
	wantStatus(t, e.reqAs(admin, "GET", "/config", nil, nil), 200)
	wantStatus(t, e.reqAs(admin, "GET", "/snapshots", nil, nil), 200)
	wantStatus(t, e.reqAs(viewer, "GET", "/snapshots", nil, nil), 403)
	wantStatus(t, e.reqAs(viewer, "GET", "/v1/backup/devices/"+e.dev.Device.ID, nil, nil), 403)
	// The device token opens no owner route.
	for _, rt := range []struct{ m, p string }{{"POST", "/revoke"}, {"POST", "/token"}, {"POST", "/destination"}, {"POST", "/downloads"}, {"DELETE", "/files?category=media&path=a"}} {
		if rec := e.req(rt.m, rt.p, DeviceDestRequest{}, nil); rec.Code != 404 && rec.Code != 401 {
			t.Errorf("device token on %s %s: HTTP %d", rt.m, rt.p, rec.Code)
		}
	}
	if rec := e.req("GET", "/v1/backup/devices/"+e.dev.Device.ID, nil, nil); rec.Code != 401 {
		t.Errorf("device token on GET /devices/:id: HTTP %d", rec.Code)
	}
	wantStatus(t, ping(e.h, e.dev.Device.ID, admin), 401)

	// Revoke: the token stops, the backup stays browsable; a new token
	// re-links the phone.
	e.put(ses.ID, CatMedia, "path", "a.jpg", []byte("a"))
	wantStatus(t, doRequest(e.h.svc, "POST", "/v1/backup/devices/"+e.dev.Device.ID+"/revoke", nil, reqOpts{remote: remoteBrowser, token: admin}), 200)
	wantStatus(t, e.req("GET", "/config", nil, nil), 401)
	wantStatus(t, e.reqAs(admin, "GET", "/files/content?category=media&path=a.jpg", nil, nil), 200)
	var s PhoneSessionRow
	e.h.svc.store.db.Where("id = ?", ses.ID).Take(&s)
	if s.Status != SessionCancelled {
		t.Errorf("session after revoke %q", s.Status)
	}
	rec = doRequest(e.h.svc, "POST", "/v1/backup/devices/"+e.dev.Device.ID+"/token", nil, reqOpts{remote: remoteBrowser, token: admin})
	wantStatus(t, rec, 200)
	var en DeviceEnrollment
	envelope(t, rec, &en)
	if en.Device.RevokedAt != nil {
		t.Error("still revoked")
	}
	e.dev.Token = en.Token
	wantStatus(t, e.req("GET", "/config", nil, nil), 200)

	// Remove with purge_data deletes the folder.
	wantStatus(t, doRequest(e.h.svc, "DELETE", "/v1/backup/devices/"+e.dev.Device.ID+"?purge_data=1", nil, reqOpts{remote: remoteBrowser, token: admin}), 200)
	if _, err := os.Stat(e.root()); err == nil {
		t.Error("folder kept after purge")
	}
	var n int64
	e.h.svc.store.db.Model(&PhoneFileRow{}).Where("device_id = ?", e.dev.Device.ID).Count(&n)
	if n != 0 {
		t.Error("index kept after remove")
	}
}

func TestPhonePathAttacks(t *testing.T) {
	e := newPhoneEnv(t)
	ses := e.startSession(CatMedia)
	for _, p := range []string{"../x", "a/../../x", "/etc/passwd", "a//b", "./a", "a\x00b", "a\\b", strings.Repeat("a/", 2100) + "x", "a/" + strings.Repeat("y", 256), "", "a/\x01"} {
		rec, _ := e.create(ses.ID, CatMedia, "path", p, []byte("x"), nil)
		if rec.Code != 400 {
			t.Errorf("path %q: HTTP %d", p, rec.Code)
		}
	}
	// A symbolic link planted in the backup folder is not followed.
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(e.root(), "media"), 0o755)
	if err := os.Symlink(outside, filepath.Join(e.root(), "media", "evil")); err != nil {
		t.Fatal(err)
	}
	rec, c := e.create(ses.ID, CatMedia, "path", "evil/x.txt", []byte("x"), nil)
	wantStatus(t, rec, 201)
	rec = e.patch(c.UploadID, 0, []byte("x"))
	if rec.Code == 204 {
		t.Fatal("wrote through a symlink")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("a file landed outside the backup")
	}
	// Browse and content refuse bad paths.
	wantStatus(t, e.req("GET", "/snapshots/latest/browse?category=media&path=../..", nil, nil), 400)
	wantStatus(t, e.req("GET", "/files/content?category=media&path=../../etc/passwd", nil, nil), 400)
	// Export names.
	rec, _ = e.create(ses.ID, CatMedia, "", "", []byte("x"), nil) // no path
	wantStatus(t, rec, 400)
}

func TestPhoneMessagesExports(t *testing.T) {
	e := newPhoneEnv(t)
	sms := func(items ...string) []byte {
		return []byte(`<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>` + "\n<smses count=\"" + strconv.Itoa(len(items)) + "\">\n" + strings.Join(items, "\n") + "\n</smses>\n")
	}
	m1 := `<sms protocol="0" address="+4912345" date="1700000000000" type="1" body="Hello &amp; welcome" read="1" />`
	m2 := `<sms protocol="0" address="+4912345" date="1700000001000" type="2" body="Thanks" read="1" />`
	mms := `<mms date="1700000002000" msg_box="1" address="+4999" m_id="abc"><parts><part seq="0" ct="text/plain" text="pic" /></parts></mms>`
	k1 := ItemKey("sms", "+4912345", "1700000000000", "1", "Hello & welcome")

	ses := e.startSession(CatSMS, CatCallLog, CatCalendar)
	rec := e.req("POST", "/sessions/"+ses.ID+"/check-items", ItemKeysRequest{Category: CatSMS, Keys: []string{k1, k1}}, nil)
	var ik ItemKeysResult
	envelope(t, rec, &ik)
	if len(ik.New) != 1 {
		t.Fatalf("new keys %+v", ik)
	}
	if r := e.put(ses.ID, CatSMS, "", "", sms(m1, mms)); r != UploadStored {
		t.Fatalf("first sms %q", r)
	}
	envelope(t, e.req("POST", "/sessions/"+ses.ID+"/check-items", ItemKeysRequest{Category: CatSMS, Keys: []string{k1}}, nil), &ik)
	if len(ik.New) != 0 {
		t.Fatalf("k1 still new %+v", ik)
	}
	// Nothing new: not stored.
	if r := e.put(ses.ID, CatSMS, "", "", sms(m1)); r != UploadNoNewItems {
		t.Fatalf("repeat sms %q", r)
	}
	if r := e.put(ses.ID, CatSMS, "", "", sms(m1, m2)); r != UploadStored {
		t.Fatalf("second sms %q", r)
	}
	// Not XML.
	rec, c := e.create(ses.ID, CatSMS, "", "", []byte("garbage <"), nil)
	rec = e.patch(c.UploadID, 0, []byte("garbage <"))
	if rec.Code != 400 {
		t.Errorf("bad xml: HTTP %d", rec.Code)
	}
	calls := []byte(`<?xml version='1.0' encoding='UTF-8' standalone='yes' ?><calls count="1"><call number="+4912345" duration="30" date="1700000000000" type="1" /></calls>`)
	e.put(ses.ID, CatCallLog, "", "", calls)
	// Calendars are named streams.
	rec, _ = e.create(ses.ID, CatCalendar, "", "", []byte("BEGIN:VCALENDAR"), nil)
	wantStatus(t, rec, 400)
	if r := e.put(ses.ID, CatCalendar, "name", "Work", []byte("BEGIN:VCALENDAR\nBEGIN:VEVENT\nEND:VEVENT\nEND:VCALENDAR\n")); r != UploadStored {
		t.Fatalf("calendar %q", r)
	}
	if r := e.put(ses.ID, CatCalendar, "name", "Home", []byte("BEGIN:VCALENDAR\nEND:VCALENDAR\n")); r != UploadStored {
		t.Fatalf("calendar 2 %q", r)
	}
	snap := e.finish(ses.ID, StatusSuccess).Snapshot
	var exps []PhoneExport
	envelope(t, e.req("GET", "/exports?category=calendar", nil, nil), &exps)
	if len(exps) != 2 {
		t.Fatalf("calendars %+v", exps)
	}

	// The full file: every message once.
	rec = e.req("GET", "/exports/full?category=sms&snapshot="+snap.ID, nil, nil)
	wantStatus(t, rec, 200)
	body := rec.Body.String()
	if !strings.Contains(body, `<smses count="3"`) || strings.Count(body, `date="1700000000000"`) != 1 || !strings.Contains(body, "Thanks") || !strings.Contains(body, `<part seq="0"`) {
		t.Fatalf("full sms:\n%s", body)
	}
	keys, err := scanItems(strings.NewReader(body))
	if err != nil || len(keys) != 3 || keys[0] != k1 {
		t.Fatalf("full keys %v %v", keys, err)
	}

	// Import a file made elsewhere: only the new messages count.
	m3 := `<sms protocol="0" address="+4977" date="1600000000000" type="1" body="old" read="1" />`
	rec = doRequest(e.h.svc, "POST", "/v1/backup/devices/"+e.dev.Device.ID+"/exports/import?category=sms", sms(m1, m3), reqOpts{})
	wantStatus(t, rec, 200)
	var ir ImportResult
	envelope(t, rec, &ir)
	if ir.Result != UploadStored || ir.Export == nil || !ir.Export.Imported {
		t.Fatalf("import %+v", ir)
	}
	rec = doRequest(e.h.svc, "POST", "/v1/backup/devices/"+e.dev.Device.ID+"/exports/import?category=sms", sms(m1), reqOpts{})
	envelope(t, rec, &ir)
	if ir.Result != UploadNoNewItems {
		t.Fatalf("import again %+v", ir)
	}
	rec = e.req("GET", "/exports/full?category=sms", nil, nil)
	if !strings.Contains(rec.Body.String(), `count="4"`) {
		t.Fatalf("after import:\n%s", rec.Body.String())
	}
	var dd DeviceDetail
	envelope(t, doRequest(e.h.svc, "GET", "/v1/backup/devices/"+e.dev.Device.ID, nil, reqOpts{}), &dd)
	for _, c := range dd.Categories {
		if c.Category == CatSMS && (c.Items != 4 || c.Exports != 3) {
			t.Errorf("sms status %+v", c)
		}
	}
}

func TestPhoneIdleSessionAndCancel(t *testing.T) {
	var mu sync.Mutex
	now := time.Now().UTC()
	e := newPhoneEnv(t, withNow(func() time.Time { mu.Lock(); defer mu.Unlock(); return now }))
	ses := e.startSession(CatMedia)
	_, c := e.create(ses.ID, CatMedia, "path", "big.bin", []byte("0123456789"), nil)
	wantStatus(t, e.patch(c.UploadID, 0, []byte("01234")), 204)
	mu.Lock()
	now = now.Add(PhoneSessionIdle + time.Minute)
	mu.Unlock()
	e.h.svc.sweepSessions()
	run, _ := e.h.svc.store.GetRun(ses.RunID)
	if run.Status != string(StatusInterrupted) {
		t.Fatalf("idle run %s", run.Status)
	}
	// The upload survives into the next session.
	ses2 := e.startSession(CatMedia)
	_, again := e.create(ses2.ID, CatMedia, "path", "big.bin", []byte("0123456789"), nil)
	if again.UploadID != c.UploadID || again.Offset != 5 {
		t.Fatalf("resume across sessions %+v", again)
	}
	rec := e.patch(c.UploadID, 5, []byte("56789"))
	wantStatus(t, rec, 204)
	if rec.Header().Get(UploadResultHeader) != UploadStored {
		t.Fatal("not stored")
	}
	// The owner cancels the session's run.
	wantStatus(t, doRequest(e.h.svc, "POST", "/v1/backup/runs/"+ses2.RunID+"/cancel", nil, reqOpts{}), 200)
	var s PhoneSessionRow
	e.h.svc.store.db.Where("id = ?", ses2.ID).Take(&s)
	if s.Status != SessionCancelled {
		t.Fatalf("after cancel %q", s.Status)
	}
	// A restart doesn't touch an open session's run.
	ses3 := e.startSession(CatMedia)
	if err := e.h.svc.recoverRuns(); err != nil {
		t.Fatal(err)
	}
	run, _ = e.h.svc.store.GetRun(ses3.RunID)
	if run.Status != string(StatusRunning) {
		t.Fatalf("restart ended the session run: %s", run.Status)
	}
	_ = url.Values{}
}

// SQLite compares the stored times the way the snapshot queries need.
func TestPhoneTimeOrderInSQL(t *testing.T) {
	h := newHarness(t, false)
	base := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	times := []time.Time{base, base.Add(time.Microsecond), base.Add(500 * time.Millisecond), base.Add(time.Second), base.Add(1100 * time.Millisecond)}
	for i, tt := range times {
		h.svc.store.db.Create(&PhoneSnapshotRow{ID: fmt.Sprint(i), DeviceID: "d", TakenAt: tt})
	}
	for i, tt := range times {
		var n int64
		h.svc.store.db.Model(&PhoneSnapshotRow{}).Where("taken_at <= ?", tt).Count(&n)
		if n != int64(i+1) {
			t.Errorf("<= %v: %d rows, want %d", tt, n, i+1)
		}
	}
}

// Every phone route in backup-api.json is served with the auth it
// documents: device routes refuse the JWT, jwt routes refuse the device
// token, device_or_jwt routes take both.
func TestPhoneRoutesMatchContractAuth(t *testing.T) {
	key := newKey(t)
	e := newPhoneEnv(t, withPublicKey(&key.PublicKey))
	admin, _ := jwt.GetAccessToken("owner", key, 1)
	doc := loadAPIDoc(t)
	seen := 0
	for _, ep := range doc.Endpoints {
		if !strings.HasPrefix(ep.Path, "/devices/:id/") || ep.Path == "/devices/:id/ping" {
			continue
		}
		seen++
		p := strings.NewReplacer(":id", e.dev.Device.ID, ":sid", "ses_x", ":uid", "up_x", ":snap", "latest", ":eid", "exp_x").Replace(ep.Path)
		dev := doRequest(e.h.svc, ep.Method, "/v1/backup"+p, nil, reqOpts{remote: remoteBrowser, token: e.dev.Token})
		if strings.HasSuffix(ep.Path, "/revoke") || strings.HasSuffix(ep.Path, "/token") {
			// Not run as the owner: it would end this test's device token.
			if dev.Code != 401 && dev.Code != 404 {
				t.Errorf("%s %s: device token got %d", ep.Method, ep.Path, dev.Code)
			}
			continue
		}
		jw := doRequest(e.h.svc, ep.Method, "/v1/backup"+p, nil, reqOpts{remote: remoteBrowser, token: admin})
		switch ep.Auth {
		case "device":
			if dev.Code == 401 || dev.Code == 404 && strings.Contains(dev.Body.String(), ep.Method+" ") {
				t.Errorf("%s %s: device token refused (%d)", ep.Method, ep.Path, dev.Code)
			}
			if jw.Code != 401 {
				t.Errorf("%s %s: JWT got %d", ep.Method, ep.Path, jw.Code)
			}
		case "jwt":
			if dev.Code != 401 && dev.Code != 404 {
				t.Errorf("%s %s: device token got %d", ep.Method, ep.Path, dev.Code)
			}
			if jw.Code == 401 || jw.Code == 403 {
				t.Errorf("%s %s: JWT refused (%d)", ep.Method, ep.Path, jw.Code)
			}
		case "device_or_jwt":
			if dev.Code == 401 || jw.Code == 401 || jw.Code == 403 {
				t.Errorf("%s %s: device %d, JWT %d", ep.Method, ep.Path, dev.Code, jw.Code)
			}
		default:
			t.Errorf("%s %s: auth %q", ep.Method, ep.Path, ep.Auth)
		}
	}
	if seen < 25 {
		t.Errorf("only %d phone routes in backup-api.json", seen)
	}
}
