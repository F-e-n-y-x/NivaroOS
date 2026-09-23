package v1

import (
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	json2 "encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	url2 "net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/user/common"
	"github.com/F-e-n-y-x/NivaroOS/services/user/model"
	"github.com/F-e-n-y-x/NivaroOS/services/user/model/system_model"
	"github.com/F-e-n-y-x/NivaroOS/services/user/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/user/pkg/utils/file"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/user/service/model"
	uuid "github.com/satori/go.uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	"github.com/gin-gonic/gin"
)

// Usernames appear in URLs (/v1/users/:username), so they are restricted to
// a safe charset and can't be one of the fixed route names - a user called
// "current" made the UI's delete look up (and delete) the caller instead.
var validUserName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,31}$`)

var reservedUserNames = map[string]bool{
	"current": true, "avatar": true, "name": true, "status": true, "refresh": true,
	"register": true, "register-key": true, "login": true, "custom": true,
	"image": true, "all": true, "info": true, "logout": true,
}

func checkUserName(name string) error {
	if !validUserName.MatchString(name) {
		return errors.New("usernames use letters, digits, dot, dash and underscore (up to 32)")
	}
	if reservedUserNames[strings.ToLower(name)] {
		return errors.New("that name is reserved")
	}
	return nil
}

// @Summary register user
// @Router /user/register/ [post]
func PostUserRegister(c *gin.Context) {
	json := make(map[string]string)
	c.ShouldBind(&json)

	username := json["username"]
	pwd := json["password"]
	key := json["key"]
	if _, ok := service.UserRegisterHash[key]; !ok {
		c.JSON(common_err.CLIENT_ERROR,
			model.Result{Success: common_err.KEY_NOT_EXIST, Message: common_err.GetMsg(common_err.KEY_NOT_EXIST)})
		return
	}

	if len(username) == 0 || len(pwd) == 0 {
		c.JSON(common_err.CLIENT_ERROR,
			model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	if len(pwd) < 6 {
		c.JSON(common_err.CLIENT_ERROR,
			model.Result{Success: common_err.PWD_IS_TOO_SIMPLE, Message: common_err.GetMsg(common_err.PWD_IS_TOO_SIMPLE)})
		return
	}
	if err := checkUserName(username); err != nil {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: err.Error()})
		return
	}
	oldUser := service.MyService.User().GetUserInfoByUserName(username)
	if oldUser.Id > 0 {
		c.JSON(common_err.CLIENT_ERROR,
			model.Result{Success: common_err.USER_EXIST, Message: common_err.GetMsg(common_err.USER_EXIST)})
		return
	}

	user := model2.UserDBModel{}
	user.Username = username
	user.Password = hashPassword(pwd)
	user.Role = "admin"

	user = service.MyService.User().CreateUser(user)
	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR)})
		return
	}
	file.MkDir(config.AppInfo.UserDataPath + "/" + strconv.Itoa(user.Id))
	delete(service.UserRegisterHash, key)
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// PostGenerateRegisterKey mints a fresh one-time registration key on
// demand. Upstream CasaOS only ever issues one (via GetUserStatus, and
// only before the first user exists) - single-user by design. This is
// JWT-protected (only an already-logged-in user can call it), so it's
// safe to allow minting new keys any time to support adding more
// NivaroOS-level users later.
func PostGenerateRegisterKey(c *gin.Context) {
	key := uuid.NewV4().String()
	service.UserRegisterHash[key] = key
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: key})
}

var limiter = rate.NewLimiter(rate.Every(time.Minute), 5)

// @Summary login
// @Produce  application/json
// @Accept application/json
// @Tags user
// @Param user_name query string true "User name"
// @Param pwd  query string true "password"
// @Success 200 {string} string "ok"
// @Router /user/login [post]
func PostUserLogin(c *gin.Context) {
	json := make(map[string]string)
	c.ShouldBind(&json)

	username := json["username"]
	limitKeys := []string{"ip:" + c.ClientIP(), "user:" + strings.ToLower(username)}
	if !loginLimiter.allow(limitKeys...) {
		c.JSON(common_err.TOO_MANY_REQUEST,
			model.Result{
				Success: common_err.TOO_MANY_LOGIN_REQUESTS,
				Message: common_err.GetMsg(common_err.TOO_MANY_LOGIN_REQUESTS),
			})
		return
	}

	password := json["password"]
	// check params is empty
	if len(username) == 0 || len(password) == 0 {
		c.JSON(common_err.CLIENT_ERROR,
			model.Result{
				Success: common_err.CLIENT_ERROR,
				Message: common_err.GetMsg(common_err.INVALID_PARAMS),
			})
		return
	}
	user := service.MyService.User().GetUserAllInfoByName(username)
	if user.Id == 0 {
		c.JSON(common_err.CLIENT_ERROR,
			model.Result{Success: common_err.USER_NOT_EXIST_OR_PWD_INVALID, Message: common_err.GetMsg(common_err.USER_NOT_EXIST_OR_PWD_INVALID)})
		return
	}
	if !verifyPassword(&user, password) {
		c.JSON(common_err.CLIENT_ERROR,
			model.Result{Success: common_err.USER_NOT_EXIST_OR_PWD_INVALID, Message: common_err.GetMsg(common_err.USER_NOT_EXIST_OR_PWD_INVALID)})
		return
	}
	loginLimiter.reset("ip:" + c.ClientIP())

	privateKey, _ := service.MyService.User().GetKeyPair()

	token := system_model.VerifyInformation{}

	accessToken, err := jwt.GetAccessToken(user.Username, privateKey, user.Id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
	}
	token.AccessToken = accessToken

	refreshToken, err := jwt.GetRefreshToken(user.Username, privateKey, user.Id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
	}
	token.RefreshToken = refreshToken

	token.ExpiresAt = time.Now().Add(3 * time.Hour * time.Duration(1)).Unix()
	data := make(map[string]interface{}, 2)
	user.Password = ""
	data["token"] = token

	// TODO:1 Database fields cannot be external
	data["user"] = user

	c.JSON(common_err.SUCCESS,
		model.Result{
			Success: common_err.SUCCESS,
			Message: common_err.GetMsg(common_err.SUCCESS),
			Data:    data,
		})
}

// @Summary edit user head
// @Produce  application/json
// @Accept multipart/form-data
// @Tags user
// @Param file formData file true "用户头像"
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /users/avatar [put]
func PutUserAvatar(c *gin.Context) {
	id := c.GetHeader("user_id")
	user := service.MyService.User().GetUserInfoById(id)
	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR,
			model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	json := make(map[string]string)
	c.ShouldBind(&json)

	data := json["file"]
	if len(data) > 8<<20 {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: "image too large (max 6 MB)"})
		return
	}
	// Any data:image/...;base64, prefix (not only PNG).
	if i := strings.Index(data, ";base64,"); strings.HasPrefix(data, "data:image/") && i > 0 {
		data = data[i+len(";base64,"):]
	}
	decodeData, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: "not a valid image"})
		return
	}
	// A picture that doesn't decode is the client's mistake - this used to
	// call log.Fatal and stop the whole user service.
	img, _, err := image.Decode(strings.NewReader(string(decodeData)))
	if err != nil {
		c.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: "not a supported image (PNG, JPEG or GIF)"})
		return
	}

	dir := filepath.Join(config.AppInfo.UserDataPath, strconv.Itoa(user.Id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}
	avatarPath := filepath.Join(dir, "avatar.png")
	tmp := avatarPath + ".tmp"
	outFile, err := os.Create(tmp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}
	encErr := png.Encode(outFile, img)
	closeErr := outFile.Close()
	if encErr != nil || closeErr != nil {
		os.Remove(tmp)
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: "could not save the image"})
		return
	}
	if err := os.Rename(tmp, avatarPath); err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}
	user.Avatar = avatarPath
	service.MyService.User().UpdateUser(user)
	c.JSON(http.StatusOK,
		model.Result{
			Success: common_err.SUCCESS,
			Message: common_err.GetMsg(common_err.SUCCESS),
			Data:    user,
		})
}

// ownAvatar reports whether p is a file inside the user's own data folder
// (the only place PutUserAvatar writes) - anything else stored in the
// database is never served.
func ownAvatar(p string, userID int) bool {
	if p == "" {
		return false
	}
	base := filepath.Join(config.AppInfo.UserDataPath, strconv.Itoa(userID))
	rel, err := filepath.Rel(base, filepath.Clean(p))
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// @Summary get user head
// @Produce  application/json
// @Tags user
// @Param file formData file true "用户头像"
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /users/avatar [get]
func GetUserAvatar(c *gin.Context) {
	id := c.GetHeader("user_id")
	user := service.MyService.User().GetUserInfoById(id)
	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR,
			model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}

	if ownAvatar(user.Avatar, user.Id) && file.Exists(user.Avatar) {
		c.Header("Content-Disposition", "attachment; filename*=utf-8''"+url2.PathEscape(path.Base(user.Avatar)))
		c.Header("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate, value")
		c.File(user.Avatar)
		return
	}
	user.Avatar = "/usr/share/nivaroos/www/avatar.svg"
	if file.Exists(user.Avatar) {
		c.Header("Content-Disposition", "attachment; filename*=utf-8''"+url2.PathEscape(path.Base(user.Avatar)))
		c.Header("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate, value")
		c.File(user.Avatar)
		return
	}
	user.Avatar = "/var/lib/nivaroos/www/avatar.svg"
	c.Header("Content-Disposition", "attachment; filename*=utf-8''"+url2.PathEscape(path.Base(user.Avatar)))
	c.Header("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate, value")
	c.File(user.Avatar)
}

// @Summary edit user name
// @Produce  application/json
// @Accept application/json
// @Tags user
// @Param old_name  query string true "Old user name"
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /user/name/:id [put]
func PutUserInfo(c *gin.Context) {
	id := c.GetHeader("user_id")
	user := service.MyService.User().GetUserInfoById(id)
	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR,
			model.Result{Success: common_err.USER_NOT_EXIST_OR_PWD_INVALID, Message: common_err.GetMsg(common_err.USER_NOT_EXIST_OR_PWD_INVALID)})
		return
	}
	// Only these fields of the caller's own account. The whole record used
	// to be bound from the body and saved by the body's id - any session
	// could rename or re-role another user, or point `avatar` at any file
	// (which GetUserAvatar then served as root).
	var req struct {
		Username    string `json:"username"`
		Nickname    string `json:"nickname"`
		Email       string `json:"email"`
		Description string `json:"description"`
	}
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	if req.Username != "" && req.Username != user.Username {
		if err := checkUserName(req.Username); err != nil {
			c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: err.Error()})
			return
		}
		if u := service.MyService.User().GetUserInfoByUserName(req.Username); u.Id > 0 {
			c.JSON(common_err.CLIENT_ERROR,
				model.Result{Success: common_err.USER_EXIST, Message: common_err.GetMsg(common_err.USER_EXIST)})
			return
		}
		user.Username = req.Username
	}
	if req.Nickname != "" {
		user.Nickname = req.Nickname
	}
	if req.Email != "" {
		user.Email = req.Email
	}
	if req.Description != "" {
		user.Description = req.Description
	}
	service.MyService.User().UpdateUser(user)
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: user})
}

// @Summary edit user password
// @Produce  application/json
// @Accept application/json
// @Tags user
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /user/password/:id [put]
func PutUserPassword(c *gin.Context) {
	id := c.GetHeader("user_id")
	json := make(map[string]string)
	c.ShouldBind(&json)
	oldPwd := json["old_password"]
	pwd := json["password"]
	if len(oldPwd) == 0 || len(pwd) == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	user := service.MyService.User().GetUserAllInfoById(id)
	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR,
			model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	if len(pwd) < 6 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.PWD_IS_TOO_SIMPLE, Message: common_err.GetMsg(common_err.PWD_IS_TOO_SIMPLE)})
		return
	}
	if !verifyPassword(&user, oldPwd) {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.PWD_INVALID_OLD, Message: common_err.GetMsg(common_err.PWD_INVALID_OLD)})
		return
	}
	user.Password = hashPassword(pwd)
	service.MyService.User().UpdateUserPassword(user)
	// Every session from before the change ends (they used to keep
	// working); tokens issued from now on are valid.
	service.MyService.User().SetTokensValidAfter(user.Id, time.Now().Unix())
	user.Password = ""
	// The session making the change continues with fresh tokens.
	privateKey, _ := service.MyService.User().GetKeyPair()
	access, _ := jwt.GetAccessToken(user.Username, privateKey, user.Id)
	refresh, _ := jwt.GetRefreshToken(user.Username, privateKey, user.Id)
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: map[string]interface{}{
		"user":  user,
		"token": system_model.VerifyInformation{AccessToken: access, RefreshToken: refresh, ExpiresAt: time.Now().Add(3 * time.Hour).Unix()},
	}})
}

// @Summary edit user nick
// @Produce  application/json
// @Accept application/json
// @Tags user
// @Param nick_name query string false "nick name"
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /user/nick [put]
func PutUserNick(c *gin.Context) {
	id := c.GetHeader("user_id")
	json := make(map[string]string)
	c.ShouldBind(&json)
	Nickname := json["nick_name"]
	if len(Nickname) == 0 {
		c.JSON(http.StatusOK, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	user := service.MyService.User().GetUserInfoById(id)
	if user.Id == 0 {
		c.JSON(http.StatusOK,
			model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	user.Nickname = Nickname
	service.MyService.User().UpdateUser(user)
	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: user})
}

// @Summary edit user description
// @Produce  application/json
// @Accept multipart/form-data
// @Tags user
// @Param description formData string false "Description"
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /user/desc [put]
func PutUserDesc(c *gin.Context) {
	id := c.GetHeader("user_id")
	json := make(map[string]string)
	c.ShouldBind(&json)
	desc := json["description"]
	if len(desc) == 0 {
		c.JSON(http.StatusOK, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	user := service.MyService.User().GetUserInfoById(id)
	if user.Id == 0 {
		c.JSON(http.StatusOK,
			model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	user.Description = desc

	service.MyService.User().UpdateUser(user)

	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: user})
}

// @Summary get user info
// @Produce  application/json
// @Accept  application/json
// @Tags user
// @Success 200 {string} string "ok"
// @Router /user/info/:id [get]
func GetUserInfo(c *gin.Context) {
	id := c.GetHeader("user_id")
	user := service.MyService.User().GetUserInfoById(id)

	c.JSON(common_err.SUCCESS,
		model.Result{
			Success: common_err.SUCCESS,
			Message: common_err.GetMsg(common_err.SUCCESS),
			Data:    user,
		})
}

/**
 * @description:
 * @param {*gin.Context} c
 * @param {string} Username
 * @return {*}
 * @method:
 * @router:
 */
func GetUserInfoByUsername(c *gin.Context) {
	username := c.Param("username")
	if len(username) == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	user := service.MyService.User().GetUserInfoByUserName(username)
	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}

	c.JSON(common_err.SUCCESS,
		model.Result{
			Success: common_err.SUCCESS,
			Message: common_err.GetMsg(common_err.SUCCESS),
			Data:    user,
		})
}

/**
 * @description: get all Usernames
 * @method:GET
 * @router:/user/all/name
 */
func GetUserAllUsername(c *gin.Context) {
	users := service.MyService.User().GetAllUserName()
	names := []string{}
	for _, v := range users {
		names = append(names, v.Username)
	}
	c.JSON(common_err.SUCCESS,
		model.Result{
			Success: common_err.SUCCESS,
			Message: common_err.GetMsg(common_err.SUCCESS),
			Data:    names,
		})
}

/**
 * @description:get custom file by user
 * @param {path} name string "file name"
 * @method: GET
 * @router: /user/custom/:key
 */
func GetUserCustomConf(c *gin.Context) {
	name := c.Param("key")
	if len(name) == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	id := c.GetHeader("user_id")

	user := service.MyService.User().GetUserInfoById(id)
	//	user := service.MyService.User().GetUserInfoByUsername(Username)
	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR,
			model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	filePath := config.AppInfo.UserDataPath + "/" + id + "/" + name + ".json"

	data := file.ReadFullFile(filePath)
	if !gjson.ValidBytes(data) {
		c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: string(data)})
		return
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: json2.RawMessage(string(data))})
}

/**
 * @description:create or update custom conf by user
 * @param {path} name string "file name"
 * @method:POST
 * @router:/user/custom/:key
 */
func PostUserCustomConf(c *gin.Context) {
	name := c.Param("key")
	if len(name) == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	id := c.GetHeader("user_id")
	user := service.MyService.User().GetUserInfoById(id)
	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR,
			model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	data, _ := io.ReadAll(c.Request.Body)
	filePath := config.AppInfo.UserDataPath + "/" + strconv.Itoa(user.Id)

	if err := file.IsNotExistMkDir(filePath); err != nil {
		c.JSON(common_err.SERVICE_ERROR,
			model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR)})
		return
	}

	if err := file.WriteToPath(data, filePath, name+".json"); err != nil {
		c.JSON(common_err.SERVICE_ERROR,
			model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR)})
		return
	}

	if name == "system" {
		dataMap := make(map[string]string, 1)
		dataMap["system"] = string(data)
		response, err := service.MyService.MessageBus().PublishEventWithResponse(context.Background(), common.SERVICENAME, "zimaos:user:save_config", dataMap)
		if err != nil {
			logger.Error("failed to publish event to message bus", zap.Error(err), zap.Any("event", string(data)))
			return
		}
		if response.StatusCode() != http.StatusOK {
			logger.Error("failed to publish event to message bus", zap.String("status", response.Status()), zap.Any("response", response))
		}

	}

	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: json2.RawMessage(string(data))})
}

/**
 * @description: delete user custom config
 * @param {path} key string
 * @method:delete
 * @router:/user/custom/:key
 */
func DeleteUserCustomConf(c *gin.Context) {
	name := c.Param("key")
	if len(name) == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	id := c.GetHeader("user_id")
	user := service.MyService.User().GetUserInfoById(id)
	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR,
			model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	filePath := config.AppInfo.UserDataPath + "/" + strconv.Itoa(user.Id) + "/" + name + ".json"
	err := os.Remove(filePath)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR)})
		return
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

/**
 * @description:
 * @param {path} id string "user id"
 * @method:DELETE
 * @router:/user/delete/:id
 */
func DeleteUser(c *gin.Context) {
	id := c.Param("id")
	// Never your own account, never the last one: with no users left the
	// unauthenticated status endpoint hands out a registration key, so
	// anyone on the network could claim the server.
	if id == c.GetHeader("user_id") {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: "you can't delete the account you're signed in with"})
		return
	}
	target := service.MyService.User().GetUserInfoById(id)
	if target.Id == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	if service.MyService.User().GetUserCount() <= 1 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: "this is the only account - it can't be deleted"})
		return
	}
	service.MyService.User().DeleteUserById(id)
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: id})
}

/**
 * @description:update user image
 * @method:POST
 * @router:/user/current/image/:key
 */
func PutUserImage(c *gin.Context) {
	id := c.GetHeader("user_id")
	json := make(map[string]string)
	c.ShouldBind(&json)

	path := json["path"]
	key := c.Param("key")
	if len(path) == 0 || len(key) == 0 {
		c.JSON(http.StatusOK, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	if !file.Exists(path) {
		c.JSON(http.StatusOK, model.Result{Success: common_err.FILE_DOES_NOT_EXIST, Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST)})
		return
	}

	_, err := file.GetImageExt(path)
	if err != nil {
		c.JSON(http.StatusOK, model.Result{Success: common_err.NOT_IMAGE, Message: common_err.GetMsg(common_err.NOT_IMAGE)})
		return
	}

	user := service.MyService.User().GetUserInfoById(id)
	if user.Id == 0 {
		c.JSON(http.StatusOK, model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	fstat, _ := os.Stat(path)
	if fstat.Size() > 10<<20 {
		c.JSON(http.StatusOK, model.Result{Success: common_err.IMAGE_TOO_LARGE, Message: common_err.GetMsg(common_err.IMAGE_TOO_LARGE)})
		return
	}
	ext := file.GetExt(path)
	filePath := config.AppInfo.UserDataPath + "/" + strconv.Itoa(user.Id) + "/" + key + ext
	file.CopySingleFile(path, filePath, "overwrite")

	data := make(map[string]string, 3)
	data["path"] = filePath
	data["file_name"] = key + ext
	data["online_path"] = "/v1/users/image?path=" + filePath
	c.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

/**
* @description:
* @param {*gin.Context} c
* @param {file} file
* @param {string} key
* @param {string} type:avatar,background
* @return {*}
* @method:
* @router:
 */
func PostUserUploadImage(c *gin.Context) {
	id := c.GetHeader("user_id")
	f, err := c.FormFile("file")
	key := c.Param("key")
	t := c.PostForm("type")
	if len(key) == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	if err != nil {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: err.Error()})
		return
	}

	_, err = file.GetImageExtByName(f.Filename)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.NOT_IMAGE, Message: common_err.GetMsg(common_err.NOT_IMAGE)})
		return
	}
	ext := filepath.Ext(f.Filename)
	user := service.MyService.User().GetUserInfoById(id)

	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	if t == "avatar" {
		key = "avatar"
	}
	path := config.AppInfo.UserDataPath + "/" + strconv.Itoa(user.Id) + "/" + key + ext

	c.SaveUploadedFile(f, path)
	data := make(map[string]string, 3)
	data["path"] = path
	data["file_name"] = key + ext
	data["online_path"] = "/v1/users/image?path=" + path
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

/**
 * @description: get current user's image
 * @method:GET
 * @router:/user/image/:id
 */
func GetUserImage(c *gin.Context) {
	filePath := c.Query("path")
	if len(filePath) == 0 {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	absFilePath, err := filepath.Abs(filepath.Clean(filePath))
	if err != nil {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	if !file.Exists(absFilePath) {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.FILE_DOES_NOT_EXIST, Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST)})
		return
	}
	if !strings.Contains(absFilePath, config.AppInfo.UserDataPath) {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.INSUFFICIENT_PERMISSIONS, Message: common_err.GetMsg(common_err.INSUFFICIENT_PERMISSIONS)})
		return
	}

	matched, err := regexp.MatchString(`^/var/lib/nivaroos/\d`, absFilePath)
	if err != nil {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.INSUFFICIENT_PERMISSIONS, Message: common_err.GetMsg(common_err.INSUFFICIENT_PERMISSIONS)})
		return
	}
	if !matched {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.INSUFFICIENT_PERMISSIONS, Message: common_err.GetMsg(common_err.INSUFFICIENT_PERMISSIONS)})
		return
	}

	fileName := path.Base(absFilePath)

	// @tiger - RESTful 规范下不应该返回文件本身内容，而是返回文件的静态URL，由前端去解析
	c.Header("Content-Disposition", "attachment; filename*=utf-8''"+url2.PathEscape(fileName))
	c.File(absFilePath)
}

func DeleteUserImage(c *gin.Context) {
	id := c.GetHeader("user_id")
	path := c.Query("path")
	if len(path) == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
		return
	}
	user := service.MyService.User().GetUserInfoById(id)
	if user.Id == 0 {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	if !file.Exists(path) {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_DOES_NOT_EXIST, Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST)})
		return
	}
	if !strings.Contains(path, config.AppInfo.UserDataPath+"/"+strconv.Itoa(user.Id)) {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.INSUFFICIENT_PERMISSIONS, Message: common_err.GetMsg(common_err.INSUFFICIENT_PERMISSIONS)})
		return
	}
	os.Remove(path)
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

/**
 * @description:
 * @param {*gin.Context} c
 * @param {string} refresh_token
 * @return {*}
 * @method:
 * @router:
 */
func PostUserRefreshToken(c *gin.Context) {
	js := make(map[string]string)
	c.ShouldBind(&js)
	refresh := js["refresh_token"]

	privateKey, _ := service.MyService.User().GetKeyPair()

	claims, err := jwt.ParseToken(
		refresh,
		func() (*ecdsa.PublicKey, error) {
			_, publicKey := service.MyService.User().GetKeyPair()
			return publicKey, nil
		})
	if err != nil {
		c.JSON(http.StatusUnauthorized, model.Result{Success: common_err.VERIFICATION_FAILURE, Message: common_err.GetMsg(common_err.VERIFICATION_FAILURE), Data: err.Error()})
		return
	}
	if !claims.VerifyExpiresAt(time.Now(), true) || !claims.VerifyIssuer("refresh", true) {
		c.JSON(http.StatusUnauthorized, model.Result{Success: common_err.VERIFICATION_FAILURE, Message: common_err.GetMsg(common_err.VERIFICATION_FAILURE)})
		return
	}
	// The account must still exist, and the session must be newer than the
	// last password change (deleted users and old sessions refreshed forever).
	owner := service.MyService.User().GetUserAllInfoById(strconv.Itoa(claims.ID))
	if owner.Id == 0 || claims.IssuedAt == nil || claims.IssuedAt.Unix() < owner.TokensValidAfter {
		c.JSON(http.StatusUnauthorized, model.Result{Success: common_err.VERIFICATION_FAILURE, Message: "this session has ended - sign in again"})
		return
	}

	newAccessToken, err := jwt.GetAccessToken(claims.Username, privateKey, claims.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}

	newRefreshToken, err := jwt.GetRefreshToken(claims.Username, privateKey, claims.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}

	verifyInfo := system_model.VerifyInformation{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshToken,
		ExpiresAt:    time.Now().Add(3 * time.Hour).Unix(),
	}

	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: verifyInfo})
}

func DeleteUserAll(c *gin.Context) {
	service.MyService.User().DeleteAllUser()
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// GetPublicWallpaper serves the currently configured wallpaper without authentication.
func GetPublicWallpaper(c *gin.Context) {
	entries, err := os.ReadDir(config.AppInfo.UserDataPath)
	if err != nil {
		c.JSON(http.StatusNotFound, model.Result{Success: common_err.FILE_DOES_NOT_EXIST, Message: "No wallpaper found"})
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			wpJsonPath := filepath.Join(config.AppInfo.UserDataPath, entry.Name(), "wallpaper.json")
			if file.Exists(wpJsonPath) {
				wpData := file.ReadFullFile(wpJsonPath)
				if gjson.ValidBytes(wpData) {
					parsed := gjson.ParseBytes(wpData)
					themeReq := strings.ToLower(c.Query("theme"))
					if themeReq == "" {
						themeReq = strings.ToLower(c.Query("mode"))
					}
					wpPath := parsed.Get("path").String()
					if parsed.Get("dualMode").Bool() {
						if themeReq == "light" && parsed.Get("light.path").Exists() && parsed.Get("light.path").String() != "" {
							wpPath = parsed.Get("light.path").String()
						} else if themeReq == "dark" && parsed.Get("dark.path").Exists() && parsed.Get("dark.path").String() != "" {
							wpPath = parsed.Get("dark.path").String()
						}
					}

					// If path is a URL containing path= parameter, extract the real filesystem path
					if strings.Contains(wpPath, "path=") {
						u, err := url2.Parse(wpPath)
						if err == nil {
							realPath := u.Query().Get("path")
							if realPath != "" {
								wpPath = realPath
							}
						}
					}

					// If it points to a local file on disk
					if file.Exists(wpPath) {
						c.Header("Cache-Control", "no-cache")
						c.File(wpPath)
						return
					}

					// Check user uploaded wallpaper in user data folder
					userWpPng := filepath.Join(config.AppInfo.UserDataPath, entry.Name(), "wallpaper.png")
					if file.Exists(userWpPng) {
						c.Header("Cache-Control", "no-cache")
						c.File(userWpPng)
						return
					}

					// If it's a built-in asset path
					if wpPath != "" {
						c.JSON(http.StatusOK, model.Result{
							Success: common_err.SUCCESS,
							Message: common_err.GetMsg(common_err.SUCCESS),
							Data:    map[string]string{"path": wpPath},
						})
						return
					}
				}
			}
		}
	}

	c.JSON(http.StatusNotFound, model.Result{Success: common_err.FILE_DOES_NOT_EXIST, Message: "No wallpaper found"})
}

// GetPublicAppearance returns the saved appearance settings (alpha, blur) without authentication.
func GetPublicAppearance(c *gin.Context) {
	entries, err := os.ReadDir(config.AppInfo.UserDataPath)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				appJsonPath := filepath.Join(config.AppInfo.UserDataPath, entry.Name(), "appearance.json")
				if file.Exists(appJsonPath) {
					appData := file.ReadFullFile(appJsonPath)
					if gjson.ValidBytes(appData) {
						c.JSON(http.StatusOK, model.Result{
							Success: common_err.SUCCESS,
							Message: common_err.GetMsg(common_err.SUCCESS),
							Data:    json2.RawMessage(string(appData)),
						})
						return
					}
				}
			}
		}
	}

	c.JSON(http.StatusOK, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    map[string]interface{}{"alpha": 1.0, "blur": 0},
	})
}

// @Summary 检查是否进入引导状态
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/init/check [get]
func GetUserStatus(c *gin.Context) {
	data := make(map[string]interface{}, 4)

	if service.MyService.User().GetUserCount() > 0 {
		data["initialized"] = true
		data["key"] = ""
	} else {
		key := uuid.NewV4().String()
		service.UserRegisterHash[key] = key
		data["key"] = key
		data["initialized"] = false
	}
	gpus, err := external.NvidiaGPUInfoList()
	if err != nil {
		logger.Error("NvidiaGPUInfoList error", zap.Error(err))
	}
	data["gpus"] = len(gpus)

	// Return saved wallpaper and appearance for login/welcome screens so
	// browsers on reload, new session, or cache-clear show the configured settings.
	entries, err := os.ReadDir(config.AppInfo.UserDataPath)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				wpPath := filepath.Join(config.AppInfo.UserDataPath, entry.Name(), "wallpaper.json")
				if data["wallpaper"] == nil && file.Exists(wpPath) {
					wpData := file.ReadFullFile(wpPath)
					if gjson.ValidBytes(wpData) {
						data["wallpaper"] = json2.RawMessage(string(wpData))
					}
				}
				appPath := filepath.Join(config.AppInfo.UserDataPath, entry.Name(), "appearance.json")
				if data["appearance"] == nil && file.Exists(appPath) {
					appData := file.ReadFullFile(appPath)
					if gjson.ValidBytes(appData) {
						data["appearance"] = json2.RawMessage(string(appData))
					}
				}
			}
		}
	}

	c.JSON(common_err.SUCCESS,
		model.Result{
			Success: common_err.SUCCESS,
			Message: common_err.GetMsg(common_err.SUCCESS),
			Data:    data,
		})
}
