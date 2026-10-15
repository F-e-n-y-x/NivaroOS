package v1

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/user/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/user/service/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// A failed user count read as "no users yet": the status endpoint said
// initialized=false and handed out a registration key, so the web wiped its
// sign-in and anyone on the network could claim the server.
func TestUserStatusOnDatabaseErrorIsNotUninitialized(t *testing.T) {
	logger.LogInitConsoleOnly()
	dir := t.TempDir()
	config.AppInfo.DBPath = filepath.Join(dir, "db")
	config.AppInfo.UserDataPath = filepath.Join(dir, "users")
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "user.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model2.UserDBModel{}); err != nil {
		t.Fatal(err)
	}
	us := service.NewUserService(db)
	us.CreateUser(model2.UserDBModel{Username: "alice", Password: "x", Role: "admin"})
	service.MyService = testRepo{user: us}
	sqlDB, _ := db.DB()
	sqlDB.Close() // every query fails from here on

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/users/status", GetUserStatus)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/users/status", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503: %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), `"initialized":false`) || strings.Contains(w.Body.String(), `"key"`) {
		t.Fatalf("a database error looked like a fresh server: %s", w.Body)
	}
}
