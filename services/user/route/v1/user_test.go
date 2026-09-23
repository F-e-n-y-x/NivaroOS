package v1

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/user/codegen/message_bus"
	"github.com/F-e-n-y-x/NivaroOS/services/user/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/user/service/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testRepo is the real user service on a throwaway SQLite database.
type testRepo struct{ user service.UserService }

func (r testRepo) Gateway() external.ManagementService          { return nil }
func (r testRepo) User() service.UserService                    { return r.user }
func (r testRepo) MessageBus() *message_bus.ClientWithResponses { return nil }
func (r testRepo) Event() service.EventService                  { return nil }

type fixture struct {
	router       *gin.Engine
	alice, bob   model2.UserDBModel
	userDataPath string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	// Keep the JWT key and user data inside the test directory - never the
	// real /var/lib/nivaroos.
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
	service.MyService = testRepo{user: us}
	f := &fixture{userDataPath: config.AppInfo.UserDataPath}
	f.alice = us.CreateUser(model2.UserDBModel{Username: "alice", Password: "x", Role: "admin", Avatar: ""})
	f.bob = us.CreateUser(model2.UserDBModel{Username: "bob", Password: "y", Role: "admin", Avatar: ""})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Stand-in for the JWT middleware: it sets user_id from the token.
	as := func(c *gin.Context) { c.Request.Header.Set("user_id", c.GetHeader("X-Test-As")) }
	r.PUT("/v1/users/current", as, PutUserInfo)
	r.PUT("/v1/users/avatar", as, PutUserAvatar)
	r.GET("/v1/users/avatar", as, GetUserAvatar)
	f.router = r
	return f
}

func (f *fixture) do(t *testing.T, method, path string, asID int, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-As", strconv.Itoa(asID))
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

// The profile save bound the whole user record from the body and saved it
// by the body's id: any session could rename or re-role another account.
func TestProfileUpdateOnlyEverChangesTheCallersOwnAccount(t *testing.T) {
	f := setup(t)
	w := f.do(t, http.MethodPut, "/v1/users/current", f.alice.Id, map[string]any{
		"id": f.bob.Id, "username": "pwned", "role": "guest", "nickname": "Alice A.",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	bob := service.MyService.User().GetUserInfoById(strconv.Itoa(f.bob.Id))
	if bob.Username != "bob" || bob.Role != "admin" {
		t.Fatalf("bob was modified by alice's request: %+v", bob)
	}
	alice := service.MyService.User().GetUserInfoById(strconv.Itoa(f.alice.Id))
	if alice.Username != "pwned" || alice.Nickname != "Alice A." {
		t.Fatalf("alice's own allowed fields not saved: %+v", alice)
	}
	if alice.Role != "admin" {
		t.Fatalf("role changed through the profile form: %q", alice.Role)
	}
}

// The avatar path came from the same body: avatar="/etc/shadow" and the
// avatar endpoint served that file.
func TestProfileUpdateCantPointTheAvatarAtAnyFile(t *testing.T) {
	f := setup(t)
	f.do(t, http.MethodPut, "/v1/users/current", f.alice.Id, map[string]any{"avatar": "/etc/hostname"})
	alice := service.MyService.User().GetUserInfoById(strconv.Itoa(f.alice.Id))
	if alice.Avatar == "/etc/hostname" {
		t.Fatal("avatar path taken from the request body")
	}
	// Even a bad path already in the database is never served.
	service.MyService.User().UpdateUser(model2.UserDBModel{Id: f.alice.Id, Avatar: "/etc/hostname"})
	w := f.do(t, http.MethodGet, "/v1/users/avatar", f.alice.Id, nil)
	host, _ := os.ReadFile("/etc/hostname")
	if len(host) > 0 && bytes.Equal(w.Body.Bytes(), host) {
		t.Fatal("served a file outside the user's data folder")
	}
}

func TestUsernameCantShadowAPIRoutesOrBeTaken(t *testing.T) {
	f := setup(t)
	for _, name := range []string{"current", "avatar", "bob", "../x", "", "a b"} {
		w := f.do(t, http.MethodPut, "/v1/users/current", f.alice.Id, map[string]any{"username": name})
		alice := service.MyService.User().GetUserInfoById(strconv.Itoa(f.alice.Id))
		if name != "" && alice.Username == name {
			t.Errorf("rename to %q accepted (status %d)", name, w.Code)
		}
	}
}

// A malformed avatar upload called log.Fatal and took the service down.
func TestABadAvatarUploadIsRejectedNotFatal(t *testing.T) {
	f := setup(t)
	junk := base64.StdEncoding.EncodeToString([]byte("definitely not an image"))
	w := f.do(t, http.MethodPut, "/v1/users/avatar", f.alice.Id, map[string]string{"file": "data:image/png;base64," + junk})
	if w.Code == http.StatusOK {
		t.Fatalf("junk accepted as an avatar: %s", w.Body)
	}
}

func TestAValidAvatarIsStoredInTheUsersFolder(t *testing.T) {
	f := setup(t)
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	w := f.do(t, http.MethodPut, "/v1/users/avatar", f.alice.Id, map[string]string{"file": "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	want := filepath.Join(f.userDataPath, strconv.Itoa(f.alice.Id), "avatar.png")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("avatar not written to %s: %v", want, err)
	}
	if got := f.do(t, http.MethodGet, "/v1/users/avatar", f.alice.Id, nil); got.Code != http.StatusOK || got.Body.Len() == 0 {
		t.Fatalf("stored avatar not served: %d", got.Code)
	}
}
