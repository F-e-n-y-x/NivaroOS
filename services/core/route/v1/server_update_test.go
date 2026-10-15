package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

// serverUpdateEnv: a temp update dir, a fake nivaroos-update.service.
func serverUpdateEnv(t *testing.T, running bool) (dir string, started *int) {
	t.Helper()
	dir = t.TempDir()
	n := 0
	oldDir, oldStart, oldRunning := serverUpdateDir, startServerUpdate, serverUpdateRunning
	serverUpdateDir = dir
	startServerUpdate = func() error { n++; return nil }
	serverUpdateRunning = func() bool { return running }
	t.Cleanup(func() { serverUpdateDir, startServerUpdate, serverUpdateRunning = oldDir, oldStart, oldRunning })
	return dir, &n
}

func call(t *testing.T, h func(*echo.Context) error, method, body string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(method, "/v1/sys/update", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := h(NewEcho().NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestServerUpdateApplyWritesRequestAndStartsService(t *testing.T) {
	dir, started := serverUpdateEnv(t, false)
	code, _ := call(t, PostServerUpdateApply, http.MethodPost, `{"mode":"cancel_backups"}`)
	if code != http.StatusAccepted || *started != 1 {
		t.Fatalf("apply: %d, started %d", code, *started)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "request.json"))
	if string(b) != `{"action":"apply","mode":"cancel_backups"}` {
		t.Fatalf("request.json = %s", b)
	}
	// Anything else is "wait": backups are never cancelled by accident.
	call(t, PostServerUpdateApply, http.MethodPost, `{"mode":"nuke"}`)
	b, _ = os.ReadFile(filepath.Join(dir, "request.json"))
	if !strings.Contains(string(b), `"mode":"wait"`) {
		t.Fatalf("unknown mode must mean wait: %s", b)
	}
}

func TestServerUpdateRefusedWhileRunning(t *testing.T) {
	dir, started := serverUpdateEnv(t, true)
	if code, _ := call(t, PostServerUpdateRollback, http.MethodPost, `{}`); code != http.StatusConflict || *started != 0 {
		t.Fatalf("rollback while an update runs: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "request.json")); err == nil {
		t.Fatal("no request may be left for the running update to pick up")
	}
}

func TestServerUpdateStatusAndSettings(t *testing.T) {
	dir, _ := serverUpdateEnv(t, false)
	_ = os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"state":"available","available":{"version":"1.5.0"}}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(`{"to":"1.4.0","result":"installed"}`+"\n"+`{"to":"1.5.0","result":"rolled_back"}`+"\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "snap"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "snap", "apply.sh"), nil, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "snap", "state"), []byte("rolled-back\n"), 0o644)

	code, out := call(t, GetServerUpdate, http.MethodGet, "")
	data, _ := out["data"].(map[string]interface{})
	if code != 200 || data == nil {
		t.Fatalf("status: %d %v", code, out)
	}
	if st := data["state"].(map[string]interface{}); st["state"] != "available" {
		t.Fatalf("state: %v", st)
	}
	if h := data["history"].([]interface{}); len(h) != 2 || h[0].(map[string]interface{})["to"] != "1.5.0" {
		t.Fatalf("history must be newest first: %v", h)
	}
	if data["can_rollback"] != false {
		t.Fatal("a rolled-back update can't be rolled back again")
	}
	if s := data["settings"].(map[string]interface{}); s["auto"] != "install" || s["window"] != "04:00" {
		t.Fatalf("default settings: %v", s)
	}

	if code, _ := call(t, PutServerUpdateSettings, http.MethodPut, `{"window":"25:00"}`); code != http.StatusBadRequest {
		t.Fatalf("bad window: %d", code)
	}
	if code, _ := call(t, PutServerUpdateSettings, http.MethodPut, `{"auto":"notify","window":"03:15"}`); code != 200 {
		t.Fatalf("settings: %d", code)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s serverUpdateSettings
	if json.Unmarshal(b, &s) != nil || s.Auto != "notify" || s.Window != "03:15" || s.Channel != "stable" {
		t.Fatalf("settings.json = %s", b)
	}
}
