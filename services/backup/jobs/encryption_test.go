package jobs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/secret"
)

func testBox(t *testing.T, seed byte) func() (*secret.Box, error) {
	t.Helper()
	key := bytes.Repeat([]byte{seed}, 32)
	return func() (*secret.Box, error) { return secret.New(key) }
}

func encryptedJob(name, pw string, recovery bool) Job {
	j := sampleJob(name)
	j.Dest.Encryption = &engine.Encryption{Mode: engine.EncryptFolder}
	j.EncryptionSecret = &EncryptionSecret{Password: pw, MakeRecoveryKey: recovery}
	return j
}

func TestEncryptedJobKeysNeverLeave(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	h := newHarness(t, true)
	h.svc.store.Secrets = testBox(t, 7)
	const pw = "correct horse battery"

	rec := doRequest(h.svc, "POST", "/v1/backup/jobs", encryptedJob("short", "2short", false), reqOpts{})
	wantStatus(t, rec, 400)
	if b := errorBody(t, rec); b.FieldErrors["encryption_secret.password"] != string(FieldPasswordShort) {
		t.Fatalf("short password: %+v", b)
	}
	src := encryptedJob("archive-mode", pw, false)
	src.Dest.Encryption.Mode = engine.EncryptArchive // copy job: archive mode needs type archive
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/jobs", src, reqOpts{}), 400)

	rec = doRequest(h.svc, "POST", "/v1/backup/jobs", encryptedJob("Vault", pw, true), reqOpts{})
	wantStatus(t, rec, 201)
	var created JobDetail
	envelope(t, rec, &created)
	if !regexp.MustCompile(`^([A-Z2-7]{6}-){7}[A-Z2-7]{6}$`).MatchString(created.RecoveryKey) {
		t.Fatalf("recovery key %q", created.RecoveryKey)
	}
	if e := created.Dest.Encryption; e == nil || e.Mode != engine.EncryptFolder || !e.RecoveryKey {
		t.Fatalf("dest.encryption %+v", e)
	}
	bodies := rec.Body.String()
	for _, path := range []string{"/v1/backup/jobs", "/v1/backup/jobs/" + created.ID} {
		r := doRequest(h.svc, "GET", path, nil, reqOpts{})
		wantStatus(t, r, 200)
		bodies += r.Body.String()
		if strings.Contains(r.Body.String(), created.RecoveryKey) {
			t.Errorf("GET %s shows the recovery key again", path)
		}
	}

	stored, err := h.svc.store.GetJob(created.ID)
	must(t, err)
	k := stored.Dest.Encryption.Keys
	if k == nil || k.Password != pw || len(k.Salt) < 40 || !bytes.Contains(k.KeyFile, []byte(`"recovery"`)) {
		t.Fatalf("keys not attached: %v", k)
	}
	secrets := []string{pw, k.Salt}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", stored, stored, stored), pw) {
		t.Error("a formatted job shows the password")
	}
	raw, _ := json.Marshal(stored)
	bodies += string(raw)

	// Test password: the password and the recovery key work, nothing else.
	for in, want := range map[string]bool{pw: true, "wrong horse battery": false, created.RecoveryKey: true, strings.ToLower(strings.ReplaceAll(created.RecoveryKey, "-", " ")): true, "": false} {
		r := doRequest(h.svc, "POST", "/v1/backup/jobs/"+created.ID+"/encryption/test", EncryptionTestRequest{Password: in}, reqOpts{})
		wantStatus(t, r, 200)
		var res EncryptionTestResult
		envelope(t, r, &res)
		if res.OK != want {
			t.Errorf("test %q = %v, want %v", in, res.OK, want)
		}
		bodies += r.Body.String()
	}
	if r := doRequest(h.svc, "POST", "/v1/backup/jobs/"+h.createJob(sampleJob("plain")).ID+"/encryption/test", EncryptionTestRequest{Password: pw}, reqOpts{}); r.Code != 409 {
		t.Errorf("test on a plain job: %d", r.Code)
	}

	// A run hands the engine the keys; the request's JSON doesn't carry them.
	runID := h.run(stored, KindBackup)
	req := h.finish(1, okResult(1))
	h.waitStatus(runID, StatusSuccess)
	if req.Dest.Encryption == nil || req.Dest.Encryption.Keys == nil || req.Dest.Encryption.Keys.Password != pw {
		t.Fatalf("engine got %+v", req.Dest.Encryption)
	}
	raw, _ = json.Marshal(req)
	bodies += string(raw)

	// "Check backup" is a verify run.
	r := doRequest(h.svc, "POST", "/v1/backup/jobs/"+created.ID+"/run", RunRequest{Check: true}, reqOpts{})
	wantStatus(t, r, 202)
	if creq := h.finish(2, okResult(0)); creq.Op != engine.OpCheck {
		t.Fatalf("check run op %s", creq.Op)
	}

	// Encryption can't be switched off (or changed) by an edit; the
	// server-owned recovery flag survives one.
	cur, _ := h.svc.store.GetJob(created.ID)
	edit := cur
	edit.Dest.Encryption = nil
	wantStatus(t, doRequest(h.svc, "PUT", "/v1/backup/jobs/"+created.ID, edit, reqOpts{}), 400)
	edit.Dest.Encryption = &engine.Encryption{Mode: engine.EncryptFolder}
	edit.Name = "Vault 2"
	r = doRequest(h.svc, "PUT", "/v1/backup/jobs/"+created.ID, edit, reqOpts{})
	wantStatus(t, r, 200)
	bodies += r.Body.String()
	if j, _ := h.svc.store.GetJob(created.ID); !j.Dest.Encryption.RecoveryKey || j.Dest.Encryption.Keys == nil || j.Dest.Encryption.Keys.Password != pw {
		t.Fatalf("after edit: %+v", j.Dest.Encryption)
	}

	// Nothing ever showed the secrets: API bodies, logs, the database file.
	h.stop()
	var db []byte
	for _, f := range []string{DBFile, DBFile + "-wal"} {
		b, _ := os.ReadFile(filepath.Join(h.svc.store.DataDir(), f))
		db = append(db, b...)
	}
	for _, s := range secrets {
		if strings.Contains(bodies, s) {
			t.Errorf("an API answer holds a secret")
		}
		if strings.Contains(logs.String(), s) {
			t.Errorf("the log holds a secret")
		}
		if s == pw && bytes.Contains(db, []byte(s)) {
			t.Errorf("the database holds the password in clear") // the salt is public (it is in the key file)
		}
	}
}

func TestEncryptedJobWithoutItsHostKeyFailsClosed(t *testing.T) {
	h := newHarness(t, true)
	h.svc.store.Secrets = testBox(t, 1)
	rec := doRequest(h.svc, "POST", "/v1/backup/jobs", encryptedJob("v", "a long password", false), reqOpts{})
	wantStatus(t, rec, 201)
	var created JobDetail
	envelope(t, rec, &created)
	h.svc.store.Secrets = testBox(t, 2) // another host's key
	j, err := h.svc.store.GetJob(created.ID)
	must(t, err)
	if j.Dest.Encryption == nil || j.Dest.Encryption.Keys != nil {
		t.Fatalf("keys opened with the wrong host key: %+v", j.Dest.Encryption)
	}
	// Deleting the job drops its keys.
	wantStatus(t, doRequest(h.svc, "DELETE", "/v1/backup/jobs/"+created.ID, nil, reqOpts{}), 200)
	if _, ok, _ := h.svc.store.GetMeta(metaCryptPrefix + created.ID); ok {
		t.Error("the keys outlived the job")
	}
}

func TestEncryptedJobAdoptsExistingBackup(t *testing.T) {
	h := newHarness(t, true)
	h.svc.store.Secrets = testBox(t, 3)
	old, recovery, err := newCryptKeys(engine.EncryptFolder, "the old password", true)
	must(t, err)
	h.eng.ResolveFn = func(req engine.ResolveRequest) (engine.Resolved, error) {
		return engine.Resolved{Online: true, Quirks: []engine.Quirk{}, KeyFile: old.KeyFile}, nil
	}
	rec := doRequest(h.svc, "POST", "/v1/backup/jobs", encryptedJob("a", "not the old one", false), reqOpts{})
	wantStatus(t, rec, 400)
	if b := errorBody(t, rec); b.FieldErrors["encryption_secret.password"] != string(ErrWrongPassword) {
		t.Fatalf("wrong password: %+v", b)
	}
	for i, sec := range []EncryptionSecret{{Password: "the old password"}, {RecoveryKey: recovery}} {
		j := sampleJob(fmt.Sprintf("adopt%d", i))
		j.Dest.Encryption = &engine.Encryption{Mode: engine.EncryptFolder}
		j.EncryptionSecret = &sec
		rec = doRequest(h.svc, "POST", "/v1/backup/jobs", j, reqOpts{})
		wantStatus(t, rec, 201)
		var d JobDetail
		envelope(t, rec, &d)
		got, _ := h.svc.store.GetJob(d.ID)
		k := got.Dest.Encryption.Keys
		if k.Password != "the old password" || k.Salt != old.Salt || !got.Dest.Encryption.RecoveryKey || d.RecoveryKey != "" {
			t.Fatalf("adopted keys %v, detail %+v", k, d.Dest.Encryption)
		}
	}
}

func TestKeyFileSlots(t *testing.T) {
	k, rk, err := newCryptKeys(engine.EncryptArchive, "pässwörd ok", true)
	must(t, err)
	var kf keyFile
	must(t, json.Unmarshal(k.KeyFile, &kf))
	if kf.Salt != "" || kf.Mode != engine.EncryptArchive || kf.Recovery == nil || !strings.Contains(kf.Help, "7-Zip") {
		t.Fatalf("key file %+v", kf)
	}
	if bytes.Contains(k.KeyFile, []byte("pässwörd")) {
		t.Fatal("the key file holds the password")
	}
	if _, err := kf.Check.open("pässwörd OK"); err == nil {
		t.Fatal("check opened with a wrong password")
	}
	if p, err := kf.Recovery.open(normRecoveryKey(rk)); err != nil || string(p) != "pässwörd ok" {
		t.Fatalf("recovery slot: %q %v", p, err)
	}
	for pw, want := range map[string]FieldCode{"": FieldRequired, "1234567": FieldPasswordShort, "12345678": "", "two\nlines!": FieldInvalid} {
		if got := checkPassword(pw); got != want {
			t.Errorf("checkPassword(%q) = %q, want %q", pw, got, want)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
