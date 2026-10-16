package jobs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigRestoreWaitsForTheApps(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	c := &ConfigRestorer{Root: root, Out: &out, StatusID: tokenID("tok")}
	man := &ConfigManifest{Files: []ConfigFile{
		{Path: "var/lib/nivaroos/apps/b/docker-compose.yml"}, {Path: "var/lib/nivaroos/apps/a/docker-compose.yml"},
		{Path: "var/lib/nivaroos/apps/a/.env"}, {Path: "var/lib/nivaroos/apps.json"}, {Path: "etc/nivaroos/gateway.ini"},
	}}
	if err := c.queueApps(man); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, appsRestoreFile)
	if st, _ := readAppsStatus(file); st == nil || len(st.Apps) != 2 || st.Apps[0].Name != "a" || st.Apps[1].Name != "b" || st.ID != tokenID("tok") {
		t.Fatalf("queued %+v", st)
	}

	// app-management stand-in: runs a, fails b, done
	c.appMgmtStarted = true
	oldPoll := appsPoll
	appsPoll = 10 * time.Millisecond
	defer func() { appsPoll = oldPoll }()
	go func() {
		for _, step := range []string{
			`{"state":"running","apps":[{"name":"a","status":"installing"},{"name":"b","status":"waiting"}]}`,
			`{"id":"` + tokenID("tok") + `","state":"done","apps":[{"name":"a","status":"ok","data_missing":true},{"name":"b","status":"failed","reason":"pull failed"}]}`,
		} {
			time.Sleep(50 * time.Millisecond)
			_ = writeFileAtomic(file, []byte(step), 0o600)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c.waitApps(ctx)
	for _, want := range []string{"app a: installing", "app b: failed", "ok         a (data not found - restore it from your backups)", "failed     b: pull failed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	// the Welcome screen: only with the restore's token, before or after an account exists
	h := newHarness(t, true, func(cfg *Config, h *harness) { cfg.ConfigRoot = root })
	lan := reqOpts{remote: "192.168.1.9:5000"}
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/config-restore/status", ConfigRestoreStatus{Token: "other"}, lan), 404)
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/config-restore/status", ConfigRestoreStatus{}, lan), 404)
	res := doRequest(h.svc, "POST", "/v1/backup/config-restore/status", ConfigRestoreStatus{Token: "tok"}, lan)
	wantStatus(t, res, 200)
	if b := res.Body.String(); !strings.Contains(b, `"pull failed"`) || strings.Contains(b, tokenID("tok")) {
		t.Fatalf("status %s", b)
	}
	_ = os.Remove(file)
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/config-restore/status", ConfigRestoreStatus{Token: "tok"}, lan), 404)
}
