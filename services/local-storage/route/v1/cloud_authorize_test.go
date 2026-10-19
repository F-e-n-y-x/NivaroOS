package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

const testSecret = "GOCSPX-saved_secret"

// fakeAuthorize swaps in a fake `rclone` (script body) and an account
// table, never the real rclone.conf.
func fakeAuthorize(t *testing.T, script string, accounts map[string]map[string]string) string {
	dir := t.TempDir()
	bin := filepath.Join(dir, "rclone")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$@\" > "+dir+"/args\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldBin, oldVal, oldSave := rcloneBin, accountValue, saveAccount
	rcloneBin = bin
	accountValue = func(name, key string) string { return accounts[name][key] }
	saveAccount = func(createAccountRequest, string) (string, error) { t.Error("saved an account"); return "", nil }
	t.Cleanup(func() {
		rcloneBin, accountValue, saveAccount = oldBin, oldVal, oldSave
		authMu.Lock()
		if authJob != nil {
			authJob.cancel()
		}
		authJob = nil
		authMu.Unlock()
	})
	return dir
}

func authorizeCall(t *testing.T, method string) (string, authorizeView) {
	h := PostCloudAccountAuthorize
	if method == http.MethodGet {
		h = GetCloudAccountAuthorize
	}
	w := httptestCall(h, method, "gdrive")
	body := w.Body.String()
	if strings.Contains(body, testSecret) {
		t.Fatalf("secret sent to the browser: %s", body)
	}
	var res struct{ Data *authorizeView }
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.Data == nil {
		return body, authorizeView{}
	}
	return body, *res.Data
}

func httptestCall(h gin.HandlerFunc, method, name string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(method, "/v1/cloud/accounts/"+name+"/authorize", nil)
	c.Params = gin.Params{{Key: "name", Value: name}}
	h(c)
	return w
}

var savedApp = map[string]map[string]string{
	"gdrive": {"type": "drive", "client_id": "383322-abc.apps.googleusercontent.com", "client_secret": testSecret},
	"plain":  {"type": "drive"},
	"half":   {"type": "drive", "client_id": "383322-abc.apps.googleusercontent.com"},
}

const linkAndToken = `
echo "NOTICE: If your browser doesn't open automatically go to the following link: http://127.0.0.1:53682/auth?state=xyz" >&2
sleep 0.2
echo "Paste the following into your remote machine --->"
echo '{"access_token":"ya29.a","refresh_token":"1//r"}'
echo "<---End paste"
`

func rcloneArgs(t *testing.T, dir string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(dir, "args")); err == nil && len(b) > 0 {
			return strings.TrimSpace(string(b))
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("rclone never ran")
	return ""
}

func newAuthorizeCall(t *testing.T, body string) (*httptest.ResponseRecorder, authorizeView) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodPost, "/v1/cloud/accounts/authorize", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	PostCloudNewAccountAuthorize(c)
	var res struct{ Data *authorizeView }
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.Data == nil {
		return w, authorizeView{}
	}
	return w, *res.Data
}

// pollUntilDone polls GET /accounts/<name>/authorize; nothing it returns
// may carry the secret.
func pollUntilDone(t *testing.T, name string, v authorizeView) authorizeView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for v.Running && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		w := httptestCall(GetCloudAccountAuthorize, http.MethodGet, name)
		if b := w.Body.String(); strings.Contains(b, testSecret) {
			t.Fatalf("poll leaked a secret: %s", b)
		}
		var res struct{ Data *authorizeView }
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Data == nil {
			t.Fatalf("no run for %q", name)
		}
		v = *res.Data
	}
	return v
}

func TestNewAccountOwnAppSavedServerSide(t *testing.T) {
	dir := fakeAuthorize(t, linkAndToken, nil)
	var saved createAccountRequest
	var savedName string
	saveAccount = func(req createAccountRequest, name string) (string, error) {
		saved, savedName = req, name
		return "/mnt/" + name, nil
	}

	w, v := newAuthorizeCall(t, `{"type":"drive","label":"Photos","client_id":"383322-abc.apps.googleusercontent.com","client_secret":"`+testSecret+`"}`)
	if strings.Contains(w.Body.String(), testSecret) {
		t.Fatalf("secret echoed back: %s", w.Body)
	}
	if v.Path != "/auth?state=xyz" || !strings.HasPrefix(v.Name, "photos_drive_") || !v.Running {
		t.Fatalf("start: %+v", v)
	}
	if got := rcloneArgs(t, dir); got != "authorize drive 383322-abc.apps.googleusercontent.com "+testSecret+" --auth-no-open-browser" {
		t.Fatalf("rclone args: %q", got)
	}
	v = pollUntilDone(t, v.Name, v)
	if !v.Saved || v.Token != "" || v.Error != "" {
		t.Fatalf("done: %+v", v)
	}
	if savedName != v.Name || saved.Type != "drive" || saved.Label != "Photos" ||
		saved.Params["client_id"] != "383322-abc.apps.googleusercontent.com" || saved.Params["client_secret"] != testSecret ||
		saved.Params["token"] != `{"access_token":"ya29.a","refresh_token":"1//r"}` {
		t.Fatalf("saved %q %+v", savedName, saved)
	}
}

func TestNewAccountRclonesApp(t *testing.T) {
	dir := fakeAuthorize(t, linkAndToken, nil)
	var saved createAccountRequest
	saveAccount = func(req createAccountRequest, name string) (string, error) { saved = req; return "", nil }
	_, v := newAuthorizeCall(t, `{"type":"dropbox"}`)
	if got := rcloneArgs(t, dir); got != "authorize dropbox --auth-no-open-browser" {
		t.Fatalf("rclone args: %q", got)
	}
	if v = pollUntilDone(t, v.Name, v); !v.Saved {
		t.Fatalf("%+v", v)
	}
	if saved.Label != "Dropbox" || len(saved.Params) != 1 || saved.Params["token"] == "" {
		t.Fatalf("saved %+v", saved)
	}
}

func TestNewAccountSaveFailureIsReported(t *testing.T) {
	fakeAuthorize(t, linkAndToken, nil)
	saveAccount = func(createAccountRequest, string) (string, error) { return "", errors.New("mount failed") }
	_, v := newAuthorizeCall(t, `{"type":"onedrive"}`)
	if v = pollUntilDone(t, v.Name, v); v.Saved || !strings.Contains(v.Error, "mount failed") {
		t.Fatalf("%+v", v)
	}
}

func TestNewAccountRefusesBadRequests(t *testing.T) {
	for _, body := range []string{
		`{"type":"s3"}`,                    // not a browser sign-in
		`{"type":"nope"}`,                  // unknown
		`{"type":"drive","client_id":"x"}`, // half the keys
		`{"type":"drive","client_id":"--config=/tmp/x","client_secret":"y"}`, // a flag
		`{"type":"drive","client_id":"a b","client_secret":"y"}`,
	} {
		fakeAuthorize(t, "", nil)
		if w, _ := newAuthorizeCall(t, body); w.Code == http.StatusOK || authJob != nil {
			t.Fatalf("%s: started (%d %s)", body, w.Code, w.Body)
		}
	}
}

func TestAuthorizeUsesSavedAppServerSide(t *testing.T) {
	dir := fakeAuthorize(t, `
echo 'NOTICE: Make sure your Redirect URL is set to "http://127.0.0.1:53682/" in your custom config.' >&2
echo "NOTICE: If your browser doesn't open automatically go to the following link: http://127.0.0.1:53682/auth?state=xyz" >&2
sleep 0.2
echo "Paste the following into your remote machine --->"
echo '{"access_token":"ya29.a","refresh_token":"1//r"}'
echo "<---End paste"
`, savedApp)

	_, v := authorizeCall(t, http.MethodPost)
	if v.Path != "/auth?state=xyz" || v.Error != "" {
		t.Fatalf("start: %+v", v)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if got := strings.TrimSpace(string(args)); got != "authorize drive 383322-abc.apps.googleusercontent.com "+testSecret+" --auth-no-open-browser" {
		t.Fatalf("rclone args: %q", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for v.Running && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		_, v = authorizeCall(t, http.MethodGet)
	}
	if v.Running || v.Token != `{"access_token":"ya29.a","refresh_token":"1//r"}` {
		t.Fatalf("poll: %+v", v)
	}
}

func TestAuthorizeRefusesHalfSavedApp(t *testing.T) {
	fakeAuthorize(t, "", savedApp)
	w := httptestCall(PostCloudAccountAuthorize, http.MethodPost, "half")
	if w.Code == http.StatusOK || authJob != nil {
		t.Fatalf("started with half a saved app: %d %s", w.Code, w.Body)
	}
}

// An account on rclone's app reconnects with rclone's app (the phone has
// no terminal to run the command in).
func TestAuthorizeAccountOnRclonesApp(t *testing.T) {
	dir := fakeAuthorize(t, linkAndToken, savedApp)
	w := httptestCall(PostCloudAccountAuthorize, http.MethodPost, "plain")
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got := rcloneArgs(t, dir); got != "authorize drive --auth-no-open-browser" {
		t.Fatalf("rclone args: %q", got)
	}
	var res struct{ Data authorizeView }
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if v := pollUntilDone(t, "plain", res.Data); v.Token == "" || v.Saved {
		t.Fatalf("%+v", v)
	}
}

func TestAuthorizeErrorWithoutSecretIsReadable(t *testing.T) {
	fakeAuthorize(t, `echo "Failed: no network" >&2; exit 1`, savedApp)
	w := httptestCall(PostCloudAccountAuthorize, http.MethodPost, "plain")
	var res struct{ Data authorizeView }
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if v := pollUntilDone(t, "plain", res.Data); v.Error != "Failed: no network" {
		t.Fatalf("%+v", v)
	}
}

func TestAuthorizeErrorHidesSecret(t *testing.T) {
	fakeAuthorize(t, `echo "Failed: oauth2: invalid_client for $4" >&2; exit 1`, savedApp)
	_, v := authorizeCall(t, http.MethodPost)
	if v.Running || !strings.Contains(v.Error, "invalid_client for ***") {
		t.Fatalf("%+v", v)
	}
}
