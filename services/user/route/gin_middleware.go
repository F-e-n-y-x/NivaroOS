package route

// This file provides gin-compatible Cors() and JWT() middleware for the v1
// (gin-based) router.
//
// Necessary deviation (Task 6 migration): services/common's middleware and
// jwt packages were forked (Task 2) to only expose echo.MiddlewareFunc
// variants of Cors() and JWT(), matching the other already-migrated services
// (core, app-management, gateway), which are all echo-based. The original
// pinned dependency, the upstream CasaOS-Common module at v0.4.8-alpha12,
// additionally provided gin.HandlerFunc variants (middleware/gin.go,
// utils/jwt/jwt_helper.go) that this service's v1 router (route/v1.go) still
// relies on. Since editing services/common is out of this task's scope, this
// file reimplements just the gin-specific glue locally, reusing the
// framework-agnostic pieces (jwt.Validate, model.Result, common_err) that
// still live in services/common. Behavior mirrors the original gin.go /
// jwt_helper.go JWT() implementation.

import (
	"crypto/ecdsa"
	v1 "github.com/F-e-n-y-x/NivaroOS/services/user/route/v1"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	"net/http"
	"strconv"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/gin-gonic/gin"
)

func ginCors() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE,UPDATE")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Length, X-CSRF-Token, Token,session,Language,Content-Type,Access-Control-Allow-Origin,Access-Control-Allow-Headers,Access-Control-Allow-Methods,Connection,Host,Origin,Referer,User-Agent,X-Requested-With")
		c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers")
		c.Header("Access-Control-Max-Age", "172800")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Set("Content-Type", "application/json")

		if c.Request.Method == "OPTIONS" {
			c.JSON(http.StatusOK, "ok!")
		}
		c.Request.Header.Del("Origin")
		defer func() {
			if err := recover(); err != nil {
				// mirror upstream's swallow-and-log-nothing behavior
				_ = err
			}
		}()

		c.Next()
	}
}

// ClaimsKey is the gin context key of the request's validated *jwt.Claims.
const ClaimsKey = v1.ClaimsKey

func ginJWT(publicKeyFunc func() (*ecdsa.PublicKey, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if len(token) == 0 {
			token = c.Query("token")
		}

		valid, claims, err := jwt.Validate(token, publicKeyFunc)
		if err != nil || !valid {
			// A revoked session says why (data.reason), so the app can
			// tell "this phone was removed" from an expired token.
			c.JSON(http.StatusUnauthorized, jwt.UnauthorizedResult(err))
			c.Abort()
			return
		}

		// The same account rule every service applies (through the
		// published session state), straight from the database here: a
		// deleted account, or a session from before a password change or
		// "sign out everywhere", is over.
		if owner := service.MyService.User().GetUserAllInfoById(strconv.Itoa(claims.ID)); !service.SessionAllows(owner, claims) {
			reason := owner.TokenRevokeReason
			if owner.Id == 0 {
				reason = jwt.ReasonAccountDeleted
			}
			c.JSON(http.StatusUnauthorized, jwt.UnauthorizedResult(&jwt.SessionRevokedError{Reason: reason}))
			c.Abort()
			return
		}
		// Handlers that re-issue this session's tokens need its claims.
		c.Set(ClaimsKey, claims)
		// Set, not Add: with Add, a user_id header sent by the client came
		// first and handlers (GetHeader) read that one.
		c.Request.Header.Set("user_id", strconv.Itoa(claims.ID))
		c.Next()
	}
}
