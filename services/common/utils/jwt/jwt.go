package jwt

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"time"

	jwt "github.com/golang-jwt/jwt/v4"
)

type Claims struct {
	Username string `json:"username"`
	ID       int    `json:"id"`
	// SessionID ("sid") is the login this token belongs to: the access and
	// refresh tokens of one sign-in share it, and a refresh keeps it, so
	// the whole session can be ended at once (see RevokeSessions). Empty in
	// tokens issued before sessions had ids.
	SessionID string `json:"sid,omitempty"`
	// Generation ("gen") is the account's token generation when the token
	// was issued (see sessions.go): revoking all of an account's sessions
	// increments it. Tokens from before generations existed have none,
	// which reads as 0 - every account's starting generation.
	Generation int64 `json:"gen,omitempty"`
	jwt.RegisteredClaims
}

func GenerateToken(username string, privateKey *ecdsa.PrivateKey, id int, issuer string, t time.Duration) (string, error) {
	return generateSessionToken(username, privateKey, id, "", 0, issuer, t)
}

func generateSessionToken(username string, privateKey *ecdsa.PrivateKey, id int, sid string, gen int64, issuer string, t time.Duration) (string, error) {
	claims := Claims{
		username,
		id,
		sid,
		gen,
		jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(t)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    issuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	signedToken, err := token.SignedString(privateKey)
	return signedToken, err
}

func ParseToken(signedToken string, publicKeyFunc func() (*ecdsa.PublicKey, error)) (*Claims, error) {
	token, err := jwt.ParseWithClaims(signedToken, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
		}
		return publicKeyFunc()
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// get AccessToken
func GetAccessToken(username string, privateKey *ecdsa.PrivateKey, id int) (string, error) {
	return GenerateToken(username, privateKey, id, "nivaroos", 3*time.Hour)
}

func GetRefreshToken(username string, private *ecdsa.PrivateKey, id int) (string, error) {
	return GenerateToken(username, private, id, "refresh", 7*24*time.Hour)
}

// GetSessionTokens issues the access and refresh tokens of session sid (a
// new one from NewSessionID at sign-in, the same one on a refresh).
// They carry generation 0; the user service uses IssueSessionTokens with
// the account's current generation.
func GetSessionTokens(username string, privateKey *ecdsa.PrivateKey, id int, sid string) (access, refresh string, err error) {
	return IssueSessionTokens(username, privateKey, id, sid, 0)
}

// IssueSessionTokens issues the access and refresh tokens of session sid for
// an account whose token generation is gen.
func IssueSessionTokens(username string, privateKey *ecdsa.PrivateKey, id int, sid string, gen int64) (access, refresh string, err error) {
	access, err = generateSessionToken(username, privateKey, id, sid, gen, "nivaroos", 3*time.Hour)
	if err != nil {
		return "", "", err
	}
	refresh, err = generateSessionToken(username, privateKey, id, sid, gen, "refresh", 7*24*time.Hour)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

func Validate(token string, publicKeyFunc func() (*ecdsa.PublicKey, error)) (bool, *Claims, error) {
	claims, err := ParseToken(token, publicKeyFunc)
	if err != nil {
		return false, nil, err
	}

	// Only access tokens: the refresh token (7 days, issuer "refresh") was
	// accepted here too.
	if claims != nil && claims.Issuer == "nivaroos" {
		// An ended session's tokens stop working at once, not when they
		// expire (up to 3 hours later).
		if rec, ok := SessionRevoked(claims.SessionID); ok {
			return false, nil, &SessionRevokedError{Reason: rec.Reason}
		}
		// And every session of an account ends at once on a password
		// change, "sign out everywhere" or its deletion.
		if err := CheckSession(claims); err != nil {
			return false, nil, err
		}
		return true, claims, nil
	}

	return false, nil, errors.New("invalid token")
}
