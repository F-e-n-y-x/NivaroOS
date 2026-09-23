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

	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/transfer"
	"github.com/labstack/echo/v4"
)

// The exact body the web UI and the mobile app (files_screen.dart) send.
const legacyPasteBody = `{"type":"copy","item":[{"from":"%SRC%"}],"to":"%DST%","style":"overwrite"}`

func TestLegacyBatchTaskRunsOnTheEngineAndIsListed(t *testing.T) {
	service.Transfers = transfer.NewManager(transfer.Options{})
	defer service.Transfers.Close()

	srcDir, dst := t.TempDir(), t.TempDir()
	src := filepath.Join(srcDir, "movie.mkv")
	os.WriteFile(src, []byte("frames"), 0o644)

	e := echo.New()
	body := strings.NewReplacer("%SRC%", src, "%DST%", dst).Replace(legacyPasteBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/batch/task", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := PostOperateFileOrDir(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Success int                    `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	id, _ := resp.Data["id"].(string)
	if resp.Success != 200 || id == "" {
		t.Fatalf("response: %s", rec.Body.String())
	}

	// The job finishes and the file really arrived.
	deadline := time.Now().Add(5 * time.Second)
	for {
		j, _ := service.Transfers.Get(id)
		if j.State.Terminal() {
			if j.State != transfer.StateDone {
				t.Fatalf("state %s", j.State)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "movie.mkv")); string(b) != "frames" {
		t.Fatal("file not copied")
	}

	// GET /v1/batch/tasks returns it with both legacy and new fields.
	rec = httptest.NewRecorder()
	if err := GetTransferTasks(e.NewContext(httptest.NewRequest(http.MethodGet, "/v1/batch/tasks", nil), rec)); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Data) != 1 {
		t.Fatalf("tasks: %s", rec.Body.String())
	}
	got := list.Data[0]
	if got["id"] != id || got["finished"] != true || got["status"] != "FINISHED" || got["state"] != "done" || got["files_done"] != float64(1) {
		t.Fatalf("task shape: %v", got)
	}
}
