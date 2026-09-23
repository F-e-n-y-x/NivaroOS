package jwt

import (
	"crypto/ecdsa"
	"testing"
)

// Validate is what every service's auth middleware uses for access tokens.
// It accepted the 7-day refresh token too.
func TestARefreshTokenIsNotAnAccessToken(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	key := func() (*ecdsa.PublicKey, error) { return pub, nil }
	access, _ := GetAccessToken("alice", priv, 1)
	refresh, _ := GetRefreshToken("alice", priv, 1)
	if ok, _, err := Validate(access, key); !ok || err != nil {
		t.Fatalf("access token rejected: %v", err)
	}
	if ok, _, _ := Validate(refresh, key); ok {
		t.Fatal("refresh token accepted as an access token")
	}
}
