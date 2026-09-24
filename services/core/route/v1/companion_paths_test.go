package v1

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/labstack/echo/v4"
)

func TestCompanionSubdirStaysInsideTheDeviceFolder(t *testing.T) {
	root := "/data/companion/Pixel"
	for sub, want := range map[string]string{
		"":              root,
		"/":             root,
		"Photos":        root + "/Photos",
		"/Photos/2026":  root + "/Photos/2026",
		"a/../b":        root + "/b",
		"../../etc":     root + "/etc", // rooted first, so ".." can't climb out
		"/../../../etc": root + "/etc",
	} {
		got, err := companionSubdir(root, sub)
		if err != nil || got != want {
			t.Errorf("companionSubdir(%q) = %q, %v; want %q", sub, got, err, want)
		}
	}
}

func TestWithinDir(t *testing.T) {
	for _, c := range []struct {
		root, p string
		want    bool
	}{
		{"/data/companion", "/data/companion", true},
		{"/data/companion", "/data/companion/Pixel/a.jpg", true},
		{"/data/companion", "/data/companion-evil/a.jpg", false},
		{"/data/companion", "/data/companion/../etc/shadow", false},
		{"/data/companion", "/etc/shadow", false},
		// A device without a storage path used to allow every path.
		{"", "/etc/shadow", false},
	} {
		if got := withinDir(c.root, c.p); got != c.want {
			t.Errorf("withinDir(%q, %q) = %v, want %v", c.root, c.p, got, c.want)
		}
	}
}

// ?path=../.. on the upload endpoint wrote anywhere as root.
func TestCompanionUploadCannotLeaveTheDeviceFolder(t *testing.T) {
	logger.LogInitConsoleOnly()
	base := t.TempDir()
	devDir := filepath.Join(base, "Pixel")
	companionMu.Lock()
	companionDevices["t1"] = &CompanionDevice{ID: "t1", Name: "Pixel", StoragePath: devDir}
	companionMu.Unlock()
	defer func() {
		companionMu.Lock()
		delete(companionDevices, "t1")
		companionMu.Unlock()
	}()

	upload := func(query, filename string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", filename)
		fw.Write([]byte("x"))
		mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/v1/companion/devices/t1/upload?"+query, &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("t1")
		if err := PostCompanionDeviceUpload(c); err != nil {
			t.Fatal(err)
		}
		return rec
	}

	rec := upload("path=../../escape", "evil.txt")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(base, "..", "escape", "evil.txt")); err == nil {
		t.Fatal("upload escaped the device folder")
	}
	if _, err := os.Stat(filepath.Join(devDir, "escape", "evil.txt")); err != nil {
		t.Fatalf("upload should land inside the device folder: %v", err)
	}

	upload("path=Photos", "../../../pwn.txt")
	if _, err := os.Stat(filepath.Join(devDir, "Photos", "pwn.txt")); err != nil {
		t.Fatalf("a file name with ../ should be reduced to its last element: %v", err)
	}
}
