package v1

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	"github.com/glebarez/sqlite"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type qsRepo struct {
	service.Repository
	qs service.QuickShareService
}

func (r qsRepo) QuickShares() service.QuickShareService { return r.qs }

func quickShareServer(t *testing.T) *echo.Echo {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "qs.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model2.QuickShareDBModel{}); err != nil {
		t.Fatal(err)
	}
	old := service.MyService
	service.MyService = qsRepo{qs: service.NewQuickShareService(db)}
	t.Cleanup(func() {
		service.MyService = old
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	e := echo.New()
	e.GET("/v1/qs/:id", GetQuickShareRedeem)
	e.POST("/v1/qs/:id", PostQuickShareRedeem)
	e.POST("/v1/quickshare", PostCreateQuickShare)
	e.GET("/v1/quickshare", GetQuickSharesList)
	e.DELETE("/v1/quickshare/:id", DeleteQuickShare)
	return e
}

func do(e *echo.Echo, method, target, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func createShare(t *testing.T, e *echo.Echo, body map[string]interface{}) quickShareItem {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := do(e, http.MethodPost, "/v1/quickshare", "application/json", string(raw))
	if rec.Code != 200 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var res struct{ Data quickShareItem }
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.Data
}

func TestQuickShareOneTimePasswordLink(t *testing.T) {
	e := quickShareServer(t)
	f := filepath.Join(t.TempDir(), "report.txt")
	os.WriteFile(f, []byte("hello"), 0o644)

	item := createShare(t, e, map[string]interface{}{"path": f, "expiry": "7d", "max_downloads": 1, "password": "s3cret"})
	if !item.HasPassword || item.MaxDownloads != 1 || item.ExpiresAt == 0 || item.IsDir {
		t.Fatalf("item = %+v", item)
	}
	link := "/v1/qs/" + item.ID

	// A link preview (GET) gets the page, not the file, and uses nothing up.
	for i := 0; i < 2; i++ {
		rec := do(e, http.MethodGet, link, "", "")
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="password"`) || strings.Contains(rec.Body.String(), "hello") {
			t.Fatalf("GET: %d %s", rec.Code, rec.Body)
		}
	}
	if rec := do(e, http.MethodPost, link, "application/x-www-form-urlencoded", "password=nope"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong password: %d", rec.Code)
	}
	rec := do(e, http.MethodPost, link, "application/x-www-form-urlencoded", "password=s3cret")
	if rec.Code != 200 || rec.Body.String() != "hello" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body)
	}
	// Used up: gone from the link and from the list.
	if rec := do(e, http.MethodPost, link, "application/x-www-form-urlencoded", "password=s3cret"); rec.Code != http.StatusNotFound || strings.Count(rec.Body.String(), `"success"`) != 1 {
		t.Fatalf("second download: %d %s", rec.Code, rec.Body)
	}
	if rec := do(e, http.MethodGet, "/v1/quickshare", "", ""); strings.Contains(rec.Body.String(), item.ID) {
		t.Fatalf("still listed: %s", rec.Body)
	}
}

func TestQuickSharePlainLinkAndRevoke(t *testing.T) {
	e := quickShareServer(t)
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, []byte("abc"), 0o644)
	item := createShare(t, e, map[string]interface{}{"path": f, "expiry": "never"})
	if item.ExpiresAt != 0 || item.HasPassword || item.MaxDownloads != 0 {
		t.Fatalf("item = %+v", item)
	}
	for i := 0; i < 3; i++ {
		if rec := do(e, http.MethodGet, "/v1/qs/"+item.ID, "", ""); rec.Code != 200 || rec.Body.String() != "abc" {
			t.Fatalf("GET %d: %d %q", i, rec.Code, rec.Body)
		}
	}
	do(e, http.MethodDelete, "/v1/quickshare/"+item.ID, "", "")
	if rec := do(e, http.MethodGet, "/v1/qs/"+item.ID, "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("revoked: %d", rec.Code)
	}
}

func TestQuickShareFolderIsZip(t *testing.T) {
	e := quickShareServer(t)
	dir := filepath.Join(t.TempDir(), "Photos")
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("bee"), 0o644)
	item := createShare(t, e, map[string]interface{}{"path": dir, "expiry": "1d"})
	if !item.IsDir {
		t.Fatalf("item = %+v", item)
	}
	rec := do(e, http.MethodGet, "/v1/qs/"+item.ID, "", "")
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), url.PathEscape("Photos.zip")) {
		t.Fatalf("GET: %d %v", rec.Code, rec.Header())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "sub/b.txt" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			found = string(b) == "bee"
		}
	}
	if !found {
		t.Fatalf("zip entries: %v", zr.File)
	}
}

func TestQuickShareRejectsBadExpiry(t *testing.T) {
	e := quickShareServer(t)
	f := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(f, nil, 0o644)
	raw, _ := json.Marshal(map[string]interface{}{"path": f, "expiry": "30d"})
	if rec := do(e, http.MethodPost, "/v1/quickshare", "application/json", string(raw)); rec.Code == 200 {
		t.Fatalf("30d accepted")
	}
}
