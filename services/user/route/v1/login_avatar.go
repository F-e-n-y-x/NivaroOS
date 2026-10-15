package v1

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/image/draw"
	"golang.org/x/time/rate"
)

// The login screen shows the typed account's picture. The owner chose this
// everywhere (LAN, tailnet, internet), knowing it tells which names exist
// (relaxes part of audit M2). What keeps it from being a fast name scanner:
// a per-address budget and one internet-wide budget, refusals logged.
const loginAvatarSize = 192 // 96 px on a 2x screen

var (
	avatarLimiter  = newLoginLimits()
	avatarLogEvery = rate.NewLimiter(rate.Every(10*time.Second), 1)

	loginAvatarMu    sync.Mutex
	loginAvatarCache = map[int]loginAvatar{} // by user id; one entry per account
)

type loginAvatar struct {
	version, ctype string
	data           []byte
}

func avatarKeys(ip string) []string {
	keys := []string{"avatar-ip:" + ip}
	if isInternetIP(ip) {
		keys = append(keys, "avatar-net:")
	}
	return keys
}

func notFoundAvatar(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNotFound)
}

// @Summary the picture of ?username= for the login screen (public)
// @Description 404 for an unknown name or no picture. Rate-limited per client.
// @Router /users/login-avatar [get]
func GetLoginAvatar(c *gin.Context) {
	if !avatarLimiter.allow(avatarKeys(c.ClientIP())...) {
		if avatarLogEvery.Allow() {
			logger.Info("login avatar lookups refused: too many", zap.String("remote_ip", c.ClientIP()), zap.String("username", c.Query("username")))
		}
		c.Header("Retry-After", "5")
		c.Status(http.StatusTooManyRequests)
		return
	}
	user := service.MyService.User().GetUserInfoByUserName(c.Query("username"))
	p := avatarPathOf(user.Id)
	if user.Id == 0 || c.Query("username") == "" || user.AvatarVersion == "" || filepath.Clean(user.Avatar) != p {
		notFoundAvatar(c)
		return
	}
	loginAvatarMu.Lock()
	a, ok := loginAvatarCache[user.Id]
	loginAvatarMu.Unlock()
	if !ok || a.version != user.AvatarVersion {
		var err error
		if a, err = shrinkAvatar(p); err != nil {
			notFoundAvatar(c)
			return
		}
		a.version = user.AvatarVersion
		loginAvatarMu.Lock()
		loginAvatarCache[user.Id] = a
		loginAvatarMu.Unlock()
	}
	c.Header("ETag", `"l`+a.version+`"`)
	c.Header("Cache-Control", "private, max-age=60")
	c.Header("X-Content-Type-Options", "nosniff")
	if c.GetHeader("If-None-Match") == `"l`+a.version+`"` {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, a.ctype, a.data)
}

// shrinkAvatar: the stored 512 px PNG at loginAvatarSize, JPEG when opaque.
func shrinkAvatar(p string) (loginAvatar, error) {
	f, err := os.Open(p)
	if err != nil {
		return loginAvatar{}, err
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return loginAvatar{}, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, loginAvatarSize, loginAvatarSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	if dst.Opaque() {
		err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85})
		return loginAvatar{ctype: "image/jpeg", data: out.Bytes()}, err
	}
	err = png.Encode(&out, dst)
	return loginAvatar{ctype: "image/png", data: out.Bytes()}, err
}
