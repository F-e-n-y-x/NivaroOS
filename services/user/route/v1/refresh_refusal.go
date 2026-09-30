package v1

import (
	"errors"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/user/service/model"
	"github.com/gin-gonic/gin"
	jwtlib "github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
)

// Why /v1/users/refresh answered 401. Logged (never the token itself) so a
// sign-out can be explained afterwards - on 2026-09-30 a web UI was signed
// out by a refused refresh and nothing said why - and sent to the client
// as data.refused so the web UI can tell a dead session from a race.
const (
	refuseMissing        = "missing"         // no refresh token sent
	refuseMalformed      = "malformed"       // not a JWT
	refuseBadSignature   = "bad_signature"   // not signed by this server's key
	refuseExpired        = "expired"         // older than 7 days
	refuseNotYetValid    = "not_yet_valid"   // clock skew
	refuseWrongIssuer    = "wrong_issuer"    // e.g. an access token sent as the refresh token
	refuseUnverifiable   = "unverifiable"    // bad algorithm / no key
	refuseInvalid        = "invalid"         // any other parse failure
	refuseAccountDeleted = "account_deleted" // the account is gone
	refuseGeneration     = "generation"      // password change / sign out everywhere since
	refuseBeforeCutoff   = "issued_before_cutoff"
	refuseSessionRevoked = "session_revoked" // that one session was ended
)

func parseRefusal(token string, err error) string {
	switch {
	case strings.TrimSpace(token) == "" || token == "null" || token == "undefined":
		return refuseMissing
	case errors.Is(err, jwtlib.ErrTokenMalformed):
		return refuseMalformed
	case errors.Is(err, jwtlib.ErrTokenSignatureInvalid):
		return refuseBadSignature
	case errors.Is(err, jwtlib.ErrTokenExpired):
		return refuseExpired
	case errors.Is(err, jwtlib.ErrTokenNotValidYet), errors.Is(err, jwtlib.ErrTokenUsedBeforeIssued):
		return refuseNotYetValid
	case errors.Is(err, jwtlib.ErrTokenUnverifiable):
		return refuseUnverifiable
	}
	return refuseInvalid
}

// sessionRefusal: why service.SessionAllows said no.
func sessionRefusal(owner model2.UserDBModel, c *jwt.Claims) string {
	switch {
	case owner.Id == 0:
		return refuseAccountDeleted
	case c.Generation != owner.TokenGeneration:
		return refuseGeneration
	case owner.TokensValidAfter > 0:
		return refuseBeforeCutoff
	}
	return "session_not_allowed"
}

func unixOrZero(d *jwtlib.NumericDate) time.Time {
	if d == nil {
		return time.Time{}
	}
	return d.Time
}

// logRefreshRefused logs one refused refresh: the reason and what the
// token said about itself (account, session, generation, times) - never
// the token.
func logRefreshRefused(c *gin.Context, reason string, claims *jwt.Claims, owner *model2.UserDBModel, err error) {
	fields := []zap.Field{
		zap.String("reason", reason),
		zap.String("remote_ip", c.ClientIP()),
		zap.String("user_agent", c.Request.UserAgent()),
	}
	if err != nil {
		// The jwt library's message ("token is expired by 1h2m3s",
		// "signature is invalid"): it never contains the token.
		fields = append(fields, zap.String("detail", err.Error()))
	}
	if claims != nil {
		fields = append(fields,
			zap.Int("user_id", claims.ID),
			zap.String("session", claims.SessionID),
			zap.String("issuer", claims.Issuer),
			zap.Int64("token_generation", claims.Generation),
			zap.Time("issued_at", unixOrZero(claims.IssuedAt)),
			zap.Time("expires_at", unixOrZero(claims.ExpiresAt)),
		)
	}
	if owner != nil && owner.Id != 0 {
		fields = append(fields,
			zap.Int64("account_generation", owner.TokenGeneration),
			zap.String("account_revoke_reason", owner.TokenRevokeReason),
		)
		if owner.TokensValidAfter > 0 {
			fields = append(fields, zap.Time("account_tokens_valid_after", time.Unix(owner.TokensValidAfter, 0)))
		}
	}
	logger.Error("refresh refused (401)", fields...)
}
