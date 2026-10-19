package v1

import (
	"encoding/json"
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
	oldBin, oldVal := rcloneBin, accountValue
	rcloneBin = bin
	accountValue = func(name, key string) string { return accounts[name][key] }
	t.Cleanup(func() {
		rcloneBin, accountValue = oldBin, oldVal
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

func TestAuthorizeRefusesAccountWithoutSavedApp(t *testing.T) {
	fakeAuthorize(t, "", savedApp)
	w := httptestCall(PostCloudAccountAuthorize, http.MethodPost, "plain")
	if w.Code == http.StatusOK || authJob != nil {
		t.Fatalf("started without a saved app: %d %s", w.Code, w.Body)
	}
}

func TestAuthorizeErrorHidesSecret(t *testing.T) {
	fakeAuthorize(t, `echo "Failed: oauth2: invalid_client for $4" >&2; exit 1`, savedApp)
	_, v := authorizeCall(t, http.MethodPost)
	if v.Running || !strings.Contains(v.Error, "invalid_client for ***") {
		t.Fatalf("%+v", v)
	}
}
