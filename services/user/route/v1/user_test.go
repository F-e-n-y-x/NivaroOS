package v1

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
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
	// Handlers log (a refused refresh logs why); the logger needs a sink.
	logger.LogInitConsoleOnly()
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

// Deleting accounts had no guard: you could delete yourself or the last
// admin (which reopens first-time registration to anyone on the network).
func TestYouCantDeleteYourselfOrTheLastAdmin(t *testing.T) {
	f := setup(t)
	f.router.DELETE("/v1/users/:id", func(c *gin.Context) { c.Request.Header.Set("user_id", c.GetHeader("X-Test-As")) }, DeleteUser)

	if w := f.do(t, http.MethodDelete, "/v1/users/"+strconv.Itoa(f.alice.Id), f.alice.Id, nil); w.Code == http.StatusOK {
		t.Fatal("deleted the account making the request")
	}
	if w := f.do(t, http.MethodDelete, "/v1/users/"+strconv.Itoa(f.bob.Id), f.alice.Id, nil); w.Code != http.StatusOK {
		t.Fatalf("alice couldn't delete bob: %d %s", w.Code, w.Body)
	}
	// Alice is now the only admin; nobody can remove her (not even via a
	// second session of hers).
	if w := f.do(t, http.MethodDelete, "/v1/users/"+strconv.Itoa(f.alice.Id), f.bob.Id, nil); w.Code == http.StatusOK {
		t.Fatal("deleted the last admin")
	}
}

func encodeImg(t *testing.T, img image.Image, jpg bool, extra ...byte) string {
	t.Helper()
	var buf bytes.Buffer
	if jpg {
		jpeg.Encode(&buf, img, nil)
	} else {
		png.Encode(&buf, img)
	}
	buf.Write(extra)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestAvatarIsReencodedToA512SquareAndVersioned(t *testing.T) {
	f := setup(t)
	f.router.DELETE("/v1/users/avatar", func(c *gin.Context) { c.Request.Header.Set("user_id", c.GetHeader("X-Test-As")) }, DeleteUserAvatar)
	// A JPEG with HTML glued on: what is served must be our own PNG.
	src := encodeImg(t, image.NewRGBA(image.Rect(0, 0, 900, 600)), true, []byte("<script>alert(1)</script>")...)
	w := f.do(t, http.MethodPut, "/v1/users/avatar", f.alice.Id, map[string]string{"file": src})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var res struct {
		Data model2.UserDBModel `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	v := res.Data.AvatarVersion
	if v == "" {
		t.Fatal("no avatar_version in the response")
	}
	if u := service.MyService.User().GetUserInfoById(strconv.Itoa(f.alice.Id)); u.AvatarVersion != v {
		t.Fatalf("user info version %q, want %q", u.AvatarVersion, v)
	}

	got := f.do(t, http.MethodGet, "/v1/users/avatar?v="+v, f.alice.Id, nil)
	cfg, format, err := image.DecodeConfig(got.Body)
	if err != nil || format != "png" || cfg.Width != 512 || cfg.Height != 512 {
		t.Fatalf("served %s %dx%d (%v)", format, cfg.Width, cfg.Height, err)
	}
	if cc := got.Header().Get("Cache-Control"); cc != "private, max-age=31536000, immutable" {
		t.Fatalf("versioned URL Cache-Control %q", cc)
	}
	if cc := f.do(t, http.MethodGet, "/v1/users/avatar", f.alice.Id, nil).Header().Get("Cache-Control"); cc != "private, no-cache" {
		t.Fatalf("unversioned URL Cache-Control %q", cc)
	}
	// Bob (signed in) sees alice's picture by username.
	if w := f.do(t, http.MethodGet, "/v1/users/avatar?username=alice", f.bob.Id, nil); w.Code != http.StatusOK {
		t.Fatalf("by username: %d", w.Code)
	}
	if w := f.do(t, http.MethodGet, "/v1/users/avatar", f.bob.Id, nil); w.Code != http.StatusNotFound {
		t.Fatalf("bob has no picture, got %d", w.Code)
	}

	if w := f.do(t, http.MethodDelete, "/v1/users/avatar", f.alice.Id, nil); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if w := f.do(t, http.MethodGet, "/v1/users/avatar", f.alice.Id, nil); w.Code != http.StatusNotFound {
		t.Fatalf("removed picture still served: %d", w.Code)
	}
	if u := service.MyService.User().GetUserInfoById(strconv.Itoa(f.alice.Id)); u.AvatarVersion != "" || u.Avatar != "" {
		t.Fatalf("delete left %+v", u)
	}
}

func TestAvatarRejectsOtherFormatsHugeCanvasesAndBigFiles(t *testing.T) {
	f := setup(t)
	var gifBuf bytes.Buffer
	gif.Encode(&gifBuf, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black}), nil)
	big := make([]byte, 6<<20)
	for name, file := range map[string]string{
		"gif":    base64.StdEncoding.EncodeToString(gifBuf.Bytes()),
		"canvas": encodeImg(t, image.NewGray(image.Rect(0, 0, 8000, 6000)), false),
		"6MB":    base64.StdEncoding.EncodeToString(big),
	} {
		if w := f.do(t, http.MethodPut, "/v1/users/avatar", f.alice.Id, map[string]string{"file": file}); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, w.Code)
		}
	}
}

// The public image route served settings JSON from users' folders.
func TestPublicImageRouteOnlyServesPictures(t *testing.T) {
	dir := t.TempDir()
	js, pic := filepath.Join(dir, "link.json"), filepath.Join(dir, "wallpaper.png")
	os.WriteFile(js, []byte(`{"secret":1}`), 0o644)
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	os.WriteFile(pic, buf.Bytes(), 0o644)
	if isImageFile(js) || !isImageFile(pic) {
		t.Fatal("isImageFile wrong")
	}
}
