package route

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/user/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
)

func (w *world) loginAvatar(ip, name string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/users/login-avatar?username="+name, nil)
	req.RemoteAddr = ip + ":5000"
	rec := httptest.NewRecorder()
	w.users.ServeHTTP(rec, req)
	return rec
}

func giveAvatar(t *testing.T, id int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for i := range img.Pix {
		img.Pix[i] = max(200, uint8(i%4/3*255)) // opaque grey
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	p := filepath.Join(config.AppInfo.UserDataPath, strconv.Itoa(id), "avatar.png")
	os.MkdirAll(filepath.Dir(p), 0o755)
	var b bytes.Buffer
	png.Encode(&b, img)
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	service.MyService.User().SetAvatar(id, p, "v1")
}

// Public, no token: the picture (small, cacheable briefly) for a known name
// with one; the same bare 404 for a name with none and an unknown name.
func TestLoginAvatar(t *testing.T) {
	w := newWorld(t)
	owner := w.addUser(t, "owner", "pw")
	w.addUser(t, "nopic", "pw")
	giveAvatar(t, owner.Id)

	rec := w.loginAvatar("192.168.1.5", "owner")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" || rec.Header().Get("Cache-Control") != "private, max-age=60" {
		t.Fatalf("known: %d %v", rec.Code, rec.Header())
	}
	cfg, _, err := image.DecodeConfig(rec.Body)
	if err != nil || cfg.Width != 192 {
		t.Fatalf("not a 192 px image: %v %v", cfg, err)
	}
	none, unknown := w.loginAvatar("192.168.1.5", "nopic"), w.loginAvatar("192.168.1.5", "ghost")
	if none.Code != 404 || unknown.Code != 404 || none.Body.Len() != 0 || unknown.Body.Len() != 0 {
		t.Fatalf("no picture %d %q, unknown %d %q", none.Code, none.Body, unknown.Code, unknown.Body)
	}
	if empty := w.loginAvatar("192.168.1.5", ""); empty.Code != 404 {
		t.Fatalf("empty name: %d", empty.Code)
	}
}

// A scanner gets 20 lookups at once per address, then 1 per 3 s; the whole
// internet shares a budget too, so rotating addresses doesn't help much.
func TestLoginAvatarIsRateLimited(t *testing.T) {
	w := newWorld(t)
	w.addUser(t, "owner", "pw")
	for i := 0; i < 20; i++ {
		if c := w.loginAvatar("192.168.1.9", "guess"+strconv.Itoa(i)).Code; c != 404 {
			t.Fatalf("lookup %d: %d", i, c)
		}
	}
	if c := w.loginAvatar("192.168.1.9", "owner").Code; c != http.StatusTooManyRequests {
		t.Fatalf("21st lookup from one address: %d, want 429", c)
	}
	if c := w.loginAvatar("192.168.1.10", "owner").Code; c != 404 {
		t.Fatalf("another LAN address was limited: %d", c)
	}
	n := 0
	for i := 0; i < 200 && w.loginAvatar("203.0.113."+strconv.Itoa(i%250), "x").Code != http.StatusTooManyRequests; i++ {
		n++
	}
	if n > 61 {
		t.Fatalf("%d internet lookups from rotating addresses before any refusal", n)
	}
}
