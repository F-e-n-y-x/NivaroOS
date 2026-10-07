package v1

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"image"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/user/model"
	"github.com/F-e-n-y-x/NivaroOS/services/user/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/user/service/model"
	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	avatarMaxBytes  = 5 << 20 // decoded upload
	avatarMaxPixels = 40_000_000
	avatarSize      = 512
)

var avatarFormats = map[string]bool{"png": true, "jpeg": true, "webp": true}

func avatarBad(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: msg})
}

// processAvatar validates an uploaded picture and returns it re-encoded as
// a 512 px square PNG (centre crop), so the bytes served are always ours,
// never the upload as-is.
func processAvatar(raw []byte) ([]byte, string) {
	if len(raw) > avatarMaxBytes {
		return nil, "image too large (max 5 MB)"
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || !avatarFormats[format] {
		return nil, "not a supported image (PNG, JPEG or WebP)"
	}
	// Checked before decoding: a tiny file can claim a huge canvas.
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > avatarMaxPixels {
		return nil, "image dimensions not supported"
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "not a supported image (PNG, JPEG or WebP)"
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	dst := image.NewRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, image.Rect(x0, y0, x0+side, y0+side), draw.Src, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, "could not encode the image"
	}
	return out.Bytes(), ""
}

func avatarPathOf(userID int) string {
	return filepath.Join(config.AppInfo.UserDataPath, strconv.Itoa(userID), "avatar.png")
}

// @Summary set the caller's profile picture
// @Accept application/json
// @Param file body string true "base64 or data: URL of a PNG, JPEG or WebP (max 5 MB)"
// @Router /users/avatar [put]
func PutUserAvatar(c *gin.Context) {
	user := service.MyService.User().GetUserInfoById(c.GetHeader("user_id"))
	if user.Id == 0 {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	// base64 of 5 MB plus a data: prefix; anything bigger never gets read.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, avatarMaxBytes*4/3+1024)
	var req struct {
		File string `json:"file"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		avatarBad(c, "image too large (max 5 MB) or not a JSON body")
		return
	}
	data := req.File
	if i := strings.Index(data, ";base64,"); strings.HasPrefix(data, "data:") && i > 0 {
		data = data[i+len(";base64,"):]
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(raw) == 0 {
		avatarBad(c, "not a valid image")
		return
	}
	out, msg := processAvatar(raw)
	if msg != "" {
		avatarBad(c, msg)
		return
	}
	p := avatarPathOf(user.Id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}
	if err := os.WriteFile(p+".tmp", out, 0o644); err != nil || os.Rename(p+".tmp", p) != nil {
		os.Remove(p + ".tmp")
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: "could not save the image"})
		return
	}
	sum := sha256.Sum256(out)
	user.Avatar, user.AvatarVersion = p, hex.EncodeToString(sum[:8])
	service.MyService.User().SetAvatar(user.Id, user.Avatar, user.AvatarVersion)
	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: user})
}

// @Summary remove the caller's profile picture
// @Router /users/avatar [delete]
func DeleteUserAvatar(c *gin.Context) {
	user := service.MyService.User().GetUserInfoById(c.GetHeader("user_id"))
	if user.Id == 0 {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	os.Remove(avatarPathOf(user.Id))
	user.Avatar, user.AvatarVersion = "", ""
	service.MyService.User().SetAvatar(user.Id, "", "")
	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: user})
}

// @Summary a user's profile picture (the caller's, or ?username=)
// @Description 404 when none is set (clients draw initials). With ?v= equal
// @Description to the current avatar_version the response is cacheable forever.
// @Router /users/avatar [get]
func GetUserAvatar(c *gin.Context) {
	var user model2.UserDBModel
	if name := c.Query("username"); name != "" {
		user = service.MyService.User().GetUserInfoByUserName(name)
	} else {
		user = service.MyService.User().GetUserInfoById(c.GetHeader("user_id"))
	}
	// Only the file PutUserAvatar wrote, whatever the database says.
	p := avatarPathOf(user.Id)
	if user.Id == 0 || user.AvatarVersion == "" || filepath.Clean(user.Avatar) != p {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.FILE_DOES_NOT_EXIST, Message: "no profile picture"})
		return
	}
	if _, err := os.Stat(p); err != nil {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.FILE_DOES_NOT_EXIST, Message: "no profile picture"})
		return
	}
	c.Header("ETag", `"`+user.AvatarVersion+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	if c.Query("v") == user.AvatarVersion {
		c.Header("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		c.Header("Cache-Control", "private, no-cache")
	}
	c.Header("Content-Type", "image/png")
	c.File(p)
}
