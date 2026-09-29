package v1

import (
	"strconv"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/user/model/system_model"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/user/service/model"
	"github.com/gin-gonic/gin"
)

// ClaimsKey is the gin context key the JWT middleware stores the request's
// validated *jwt.Claims under.
const ClaimsKey = "jwt_claims"

func requestClaims(c *gin.Context) *jwt.Claims {
	if v, ok := c.Get(ClaimsKey); ok {
		if claims, ok := v.(*jwt.Claims); ok {
			return claims
		}
	}
	return nil
}

// revokeAllButThisSession ends every session of the account - in every
// service, refresh tokens included - and hands the request's own session
// fresh tokens of the new generation. It keeps its session id, so a phone
// changing its password stays the session its companion entry is bound to.
func revokeAllButThisSession(c *gin.Context, user model2.UserDBModel, reason string) (system_model.VerifyInformation, error) {
	gen, err := service.MyService.User().RevokeSessions(user.Id, reason)
	if err != nil {
		return system_model.VerifyInformation{}, err
	}
	sid := ""
	if claims := requestClaims(c); claims != nil && claims.ID == user.Id {
		sid = claims.SessionID
	}
	if sid == "" {
		sid = jwt.NewSessionID()
	}
	privateKey, _ := service.MyService.User().GetKeyPair()
	access, refresh, err := jwt.IssueSessionTokens(user.Username, privateKey, user.Id, sid, gen)
	if err != nil {
		return system_model.VerifyInformation{}, err
	}
	return system_model.VerifyInformation{AccessToken: access, RefreshToken: refresh, ExpiresAt: time.Now().Add(jwt.AccessTokenLifetime).Unix()}, nil
}

// DeleteUserSessions is "sign out everywhere else": every other browser,
// phone and script signed in to this account is signed out at once, in
// every service. The caller continues with the fresh tokens returned.
//
//	DELETE /v1/users/current/sessions
func DeleteUserSessions(c *gin.Context) {
	user := service.MyService.User().GetUserAllInfoById(c.GetHeader("user_id"))
	if user.Id == 0 {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.USER_NOT_EXIST, Message: common_err.GetMsg(common_err.USER_NOT_EXIST)})
		return
	}
	token, err := revokeAllButThisSession(c, user, jwt.ReasonSignedOutEverywhere)
	if err != nil {
		c.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		return
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: map[string]interface{}{
		"token":   token,
		"user_id": strconv.Itoa(user.Id),
	}})
}
