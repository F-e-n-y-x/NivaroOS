package external

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
)

const (
	UserServiceAddressFilename = "user-service.url"
)

var (
	// publicKeyMu guards cachedPublicKey/lastUpdate - GetPublicKey is
	// called from every concurrent request's auth check.
	publicKeyMu     sync.Mutex
	cachedPublicKey *ecdsa.PublicKey
	lastUpdate      time.Time

	// jwksClient bounds the JWKS fetch so a hung user-service can't stall
	// every authenticated request (and pile up goroutines) indefinitely.
	jwksClient = &http.Client{Timeout: 5 * time.Second}
)

func GetPublicKey(runtimePath string) (*ecdsa.PublicKey, error) {
	publicKeyMu.Lock()
	defer publicKeyMu.Unlock()

	if cachedPublicKey != nil && time.Since(lastUpdate) < 10*time.Second {
		return cachedPublicKey, nil
	}

	address, err := getAddress(filepath.Join(runtimePath, UserServiceAddressFilename))
	if err != nil {
		return nil, err
	}

	jwksURL, err := url.JoinPath(address, jwt.JWKSPath)
	if err != nil {
		return nil, err
	}

	resp, err := jwksClient.Get(jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch JWKS: received status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read JWKS response: %w", err)
	}

	var jwks jwt.JWKS
	err = json.Unmarshal(body, &jwks)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWKS: %w", err)
	}

	// Use the first key in JWKS to validate the JWT
	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("no keys found in JWKS")
	}

	key := jwks.Keys[0]
	xBytes, err := base64.RawURLEncoding.DecodeString(key.X)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWK x value: %w", err)
	}

	yBytes, err := base64.RawURLEncoding.DecodeString(key.Y)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWK y value: %w", err)
	}

	cachedPublicKey = &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}

	lastUpdate = time.Now()

	return cachedPublicKey, nil
}
