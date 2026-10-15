package v1

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// L2: the public wallpaper served whatever path a signed-in user had stored
// - the JWT signing key as readily as a JPEG.
func TestThePublicWallpaperIsOnlyEverAPicture(t *testing.T) {
	f := setup(t)
	f.router.GET("/v1/users/wallpaper", GetPublicWallpaper)
	secret := filepath.Join(t.TempDir(), "jwt_key.pem")
	os.WriteFile(secret, []byte("-----BEGIN EC PRIVATE KEY-----\nsecret\n"), 0o600)
	dir := filepath.Join(f.userDataPath, strconv.Itoa(f.alice.Id))
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "wallpaper.json"), []byte(`{"path":"`+secret+`"}`), 0o644)
	if w := f.do(t, http.MethodGet, "/v1/users/wallpaper", 0, nil); strings.Contains(w.Body.String(), "PRIVATE KEY") {
		t.Fatalf("served a non-picture: %d %s", w.Code, w.Body)
	}

	png := filepath.Join(t.TempDir(), "sky.png")
	os.WriteFile(png, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), 0o644)
	os.WriteFile(filepath.Join(dir, "wallpaper.json"), []byte(`{"path":"`+png+`"}`), 0o644)
	if w := f.do(t, http.MethodGet, "/v1/users/wallpaper", 0, nil); w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "\x89PNG") {
		t.Fatalf("the picture wasn't served: %d", w.Code)
	}
}

// DeleteUserImage checked that the path *contained* the caller's folder:
// .../users/1/../../anything was deleted.
func TestDeletingAnImageStaysInTheCallersFolder(t *testing.T) {
	f := setup(t)
	as := func(c *gin.Context) { c.Request.Header.Set("user_id", c.GetHeader("X-Test-As")) }
	f.router.DELETE("/v1/users/current/image", as, DeleteUserImage)
	victim := filepath.Join(f.userDataPath, "keep.txt")
	os.MkdirAll(filepath.Join(f.userDataPath, strconv.Itoa(f.alice.Id)), 0o755)
	os.WriteFile(victim, []byte("x"), 0o644)
	p := filepath.Join(f.userDataPath, strconv.Itoa(f.alice.Id)) + "/../keep.txt"
	f.do(t, http.MethodDelete, "/v1/users/current/image?path="+p, f.alice.Id, nil)
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("deleted a file outside the caller's folder")
	}
}
