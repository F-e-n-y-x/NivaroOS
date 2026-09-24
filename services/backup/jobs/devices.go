package jobs

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Device credentials (mobile plan §5.3 D5, fixes S-07). A phone is
// enrolled once by an admin (POST /devices, JWT) and gets a random
// 256-bit device token, shown once. The service keeps only its SHA-256
// hash. The token:
//
//   - is accepted ONLY on /devices/{id}/* for the device it was issued to
//     (Authorization: Bearer; never ?token=, which ends up in logs), and
//     never gives admin rights - everything else still wants the JWT;
//   - is independent of user-service, so it survives password changes and
//     session sign-outs, and does not expire on its own;
//   - is revoked by DELETE /devices/{id} (admin), which removes the record;
//   - is rotated by the server every DeviceTokenRotateAfter: on the first
//     device request after that, the answer carries a new token in
//     DeviceTokenHeader and the old one keeps working for DeviceTokenGrace.
//
// Rotation must not strand a phone whose answer got lost, or which had
// many requests in flight (parallel uploads) when it happened. The new
// token is therefore derived, not drawn: HMAC-SHA256 keyed with the old
// token over the device id and a random per-rotation nonce. Every request
// that still carries the old token inside the grace window is answered
// with that same new token, however many there are and in whatever order
// the phone processes the answers - there is only ever one new token, so
// none of them can be void. The server still stores only hashes (the
// nonce alone does not give the token: it needs the old token too, and
// whoever holds that may fetch the new one anyway).

const (
	// DeviceTokenHeader carries a rotated device token in a response.
	DeviceTokenHeader = "X-NivaroOS-Device-Token"
	// DeviceTokenOldValidUntilHeader is when the token the request used
	// stops working (RFC 3339), sent with DeviceTokenHeader.
	DeviceTokenOldValidUntilHeader = "X-NivaroOS-Device-Token-Old-Valid-Until"

	// DeviceTokenRotateAfter is the age at which the server replaces a
	// device token.
	DeviceTokenRotateAfter = 30 * 24 * time.Hour
	// DeviceTokenGrace is how long the replaced token keeps working.
	DeviceTokenGrace = 24 * time.Hour

	// DefaultDeviceBackupRoot is where each phone's backups go, one folder
	// per device (owner decision, mobile plan §7).
	DefaultDeviceBackupRoot = "/DATA/Backup"

	deviceTokenPrefix  = "nvd_"
	deviceTokenBytes   = 32 // 256 bits
	maxDevices         = 32
	maxDeviceNameLen   = 64
	// lastSeenEvery limits last_seen writes to one a minute per device.
	lastSeenEvery = time.Minute
)

// Device platforms.
const (
	PlatformAndroid = "android"
	PlatformIOS     = "ios"
	PlatformOther   = "other"
)

// Device is an enrolled phone (GET /devices).
type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	// DefaultDest is the folder the phone's backups go to by default,
	// DefaultDeviceBackupRoot/<device name>, unique per device. Uploads
	// arrive in Phase 2; nothing creates the folder yet.
	DefaultDest string     `json:"default_dest"`
	CreatedAt   time.Time  `json:"created_at"`
	LastSeen    *time.Time `json:"last_seen"`
	// TokenRotatedAt is when the current credential was issued.
	TokenRotatedAt time.Time `json:"token_rotated_at"`
}

// DeviceEnrollRequest is POST /devices.
type DeviceEnrollRequest struct {
	Name     string `json:"name"`
	Platform string `json:"platform"` // android (default), ios, other
}

// DeviceEnrollment is the answer to POST /devices. Token is shown only
// here; the server cannot show it again.
type DeviceEnrollment struct {
	Device Device `json:"device"`
	Token  string `json:"token"`
}

// DevicePing is GET /devices/{id}/ping, for the phone to check its
// credential.
type DevicePing struct {
	Device     Device    `json:"device"`
	ServerTime time.Time `json:"server_time"`
	// RotatesAt is when the server will next replace the token.
	RotatesAt time.Time `json:"rotates_at"`
}

// DeviceRow is the persisted device (gorm, table device_rows). Created by
// AutoMigrate on every open, so existing installs get it on upgrade
// without a schema step (an additive table; older builds ignore it).
type DeviceRow struct {
	ID          string `gorm:"primaryKey"`
	Name        string `gorm:"not null"`
	Platform    string `gorm:"not null"`
	Folder      string `gorm:"not null"` // unique folder name under the backup root
	DefaultDest string `gorm:"not null"`
	CreatedAt   time.Time
	LastSeen    *time.Time
	// TokenHash is hex SHA-256 of the current token, issued at
	// TokenIssuedAt; TokenUsed is set once a request used it.
	TokenHash     string `gorm:"not null"`
	TokenIssuedAt time.Time
	TokenUsed     bool
	// OldTokenHash is the token rotation replaced, valid until OldValidUntil.
	OldTokenHash  string
	OldValidUntil *time.Time
	// RotationNonce derives the current token from the old one (see
	// deriveRotatedToken) while the old one is still valid.
	RotationNonce string
}

func (r DeviceRow) toDevice() Device {
	return Device{
		ID: r.ID, Name: r.Name, Platform: r.Platform, DefaultDest: r.DefaultDest,
		CreatedAt: r.CreatedAt, LastSeen: r.LastSeen, TokenRotatedAt: r.TokenIssuedAt,
	}
}

// ---------------------------------------------------------------------
// Tokens

// newDeviceToken returns a fresh token and its stored hash.
func newDeviceToken() (token, hash string) {
	b := make([]byte, deviceTokenBytes)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	token = deviceTokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return token, hashDeviceToken(token)
}

// deriveRotatedToken is the token that replaces old at a rotation: the
// same for every request carrying old, so concurrent or retried requests
// all learn the one new token.
func deriveRotatedToken(old, deviceID, nonce string) string {
	m := hmac.New(sha256.New, []byte(old))
	m.Write([]byte("nivaroos-device-token-rotation\x00" + deviceID + "\x00" + nonce))
	return deviceTokenPrefix + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// hashDeviceToken is the stored form of a token: hex SHA-256. The token
// is 256 random bits, so a plain hash cannot be brute-forced.
func hashDeviceToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// hashEqual compares two hex hashes in constant time.
func hashEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// looksLikeDeviceToken is the cheap shape check before any lookup.
func looksLikeDeviceToken(tok string) bool {
	if !strings.HasPrefix(tok, deviceTokenPrefix) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(tok, deviceTokenPrefix))
	return err == nil && len(raw) == deviceTokenBytes
}

// NewDeviceID is "dev_" + 16 hex.
func NewDeviceID() string { return "dev_" + randomHex(8) }

// ---------------------------------------------------------------------
// Names and folders

// validDeviceName trims name and checks it (1..64 characters, no control
// characters).
func validDeviceName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxDeviceNameLen || !utf8.ValidString(name) {
		return name, false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return name, false
		}
	}
	return name, true
}

func validPlatform(p string) (string, bool) {
	p = strings.ToLower(strings.TrimSpace(p))
	switch p {
	case "":
		return PlatformAndroid, true
	case PlatformAndroid, PlatformIOS, PlatformOther:
		return p, true
	}
	return p, false
}

// deviceFolderStem makes a device name safe as one folder name, the same
// character rule as companion folders (core sanitizeFilename), plus no
// leading or trailing dots so "." and ".." can never be produced.
func deviceFolderStem(name string) string {
	res := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == ' ' || r == '.' {
			return r
		}
		return '_'
	}, name)
	return strings.Trim(res, " .")
}

// uniqueDeviceFolder picks stem, or "stem (2)", "stem (3)"... so that no
// two devices share a folder (compared case-insensitively, for
// filesystems like exFAT).
func uniqueDeviceFolder(stem string, taken []string) string {
	used := func(f string) bool {
		for _, t := range taken {
			if strings.EqualFold(t, f) {
				return true
			}
		}
		return false
	}
	f := stem
	for i := 2; used(f); i++ {
		f = fmt.Sprintf("%s (%d)", stem, i)
	}
	return f
}

// ---------------------------------------------------------------------
// Store

var errTooManyDevices = errors.New("too many devices")

// CreateDevice enrols a device and returns it with its token (the only
// time the token exists outside the phone).
func (s *Store) CreateDevice(name, platform, root string, now time.Time) (DeviceRow, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now = now.UTC().Truncate(time.Second)
	var row DeviceRow
	var token string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var rows []DeviceRow
		if err := tx.Select("id", "folder").Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) >= maxDevices {
			return errTooManyDevices
		}
		taken := make([]string, len(rows))
		for i, r := range rows {
			taken[i] = r.Folder
		}
		id := NewDeviceID()
		stem := deviceFolderStem(name)
		if stem == "" {
			stem = id
		}
		folder := uniqueDeviceFolder(stem, taken)
		var hash string
		token, hash = newDeviceToken()
		row = DeviceRow{
			ID: id, Name: name, Platform: platform, Folder: folder,
			DefaultDest: path.Join(root, folder), CreatedAt: now,
			TokenHash: hash, TokenIssuedAt: now,
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		if errors.Is(err, errTooManyDevices) {
			return DeviceRow{}, "", err
		}
		return DeviceRow{}, "", fmt.Errorf("store: create device: %w", err)
	}
	return row, token, nil
}

// ListDevices returns every device, oldest first.
func (s *Store) ListDevices() ([]DeviceRow, error) {
	var rows []DeviceRow
	if err := s.db.Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: list devices: %w", err)
	}
	return rows, nil
}

// GetDevice returns one device (errNoRecord when absent).
func (s *Store) GetDevice(id string) (DeviceRow, error) {
	var r DeviceRow
	err := s.db.Where("id = ?", id).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r, fmt.Errorf("device %s: %w", id, errNoRecord)
	}
	if err != nil {
		return r, fmt.Errorf("store: device %s: %w", id, err)
	}
	return r, nil
}

// DeleteDevice revokes a device: its record and every token it holds are
// gone (errNoRecord when absent).
func (s *Store) DeleteDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := s.db.Where("id = ?", id).Delete(&DeviceRow{})
	if res.Error != nil {
		return fmt.Errorf("store: delete device %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("device %s: %w", id, errNoRecord)
	}
	return nil
}

// DeviceAuth is the outcome of AuthenticateDevice.
type DeviceAuth struct {
	Device DeviceRow
	// NewToken is set when this answer must hand the phone a new token;
	// OldValidUntil is when the token the request used stops working.
	NewToken      string
	OldValidUntil time.Time
}

// AuthenticateDevice checks token against device id and applies the
// rotation rules. ok is false for an unknown device or a wrong, revoked
// or expired token; err is only for store failures.
func (s *Store) AuthenticateDevice(id, token string, now time.Time) (DeviceAuth, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now = now.UTC()
	presented := hashDeviceToken(token)
	var out DeviceAuth
	ok := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row DeviceRow
		err := tx.Where("id = ?", id).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		oldValid := row.OldTokenHash != "" && row.OldValidUntil != nil && now.Before(*row.OldValidUntil)

		// Compare against both candidates, without stopping early.
		isCurrent := hashEqual(presented, row.TokenHash)
		isOld := hashEqual(presented, row.OldTokenHash) && oldValid

		changed := false
		switch {
		case isCurrent:
			if !row.TokenUsed {
				row.TokenUsed, changed = true, true
			}
			if now.Sub(row.TokenIssuedAt) >= DeviceTokenRotateAfter {
				nonce := randomHex(16)
				tok := deriveRotatedToken(token, row.ID, nonce)
				until := now.Add(DeviceTokenGrace).Truncate(time.Second)
				row.OldTokenHash, row.OldValidUntil, row.RotationNonce = row.TokenHash, &until, nonce
				row.TokenHash, row.TokenIssuedAt, row.TokenUsed = hashDeviceToken(tok), now.Truncate(time.Second), false
				out.NewToken, out.OldValidUntil = tok, until
				changed = true
			}
		case isOld:
			// The phone hasn't switched yet (its answer got lost, or this
			// request was already in flight): tell it the new token again -
			// the very same one every time.
			if row.RotationNonce != "" {
				if tok := deriveRotatedToken(token, row.ID, row.RotationNonce); hashEqual(hashDeviceToken(tok), row.TokenHash) {
					out.NewToken, out.OldValidUntil = tok, *row.OldValidUntil
				}
			}
		default:
			return nil
		}
		ok = true
		if row.OldTokenHash != "" && (row.OldValidUntil == nil || !now.Before(*row.OldValidUntil)) {
			row.OldTokenHash, row.OldValidUntil, row.RotationNonce, changed = "", nil, "", true
		}
		if row.LastSeen == nil || now.Sub(*row.LastSeen) >= lastSeenEvery {
			seen := now.Truncate(time.Second)
			row.LastSeen, changed = &seen, true
		}
		if changed {
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		out.Device = row
		return nil
	})
	if err != nil {
		return DeviceAuth{}, false, fmt.Errorf("store: authenticate device: %w", err)
	}
	return out, ok, nil
}

// ---------------------------------------------------------------------
// HTTP

type deviceCtxKey struct{}

// deviceOf is the device a device-scoped request authenticated as.
func deviceOf(r *http.Request) DeviceRow {
	d, _ := r.Context().Value(deviceCtxKey{}).(DeviceRow)
	return d
}

// deviceScope reports whether p (without APIBase) is a device-scoped
// route, /devices/{id}/..., and returns the id. A path with empty, "."
// or ".." segments is never device scoped (bad is true): it is refused
// rather than cleaned, so it can't be steered to another route.
func deviceScope(p string) (id string, scoped, bad bool) {
	rest, ok := strings.CutPrefix(p, "/devices/")
	if !ok {
		return "", false, false
	}
	id, sub, ok := strings.Cut(rest, "/")
	if !ok {
		return "", false, false // /devices/{id}: admin routes
	}
	for _, seg := range strings.Split(rest, "/") {
		if seg == "." || seg == ".." {
			return "", false, true
		}
	}
	if id == "" || sub == "" || strings.Contains(sub, "//") || strings.HasSuffix(sub, "/") {
		return "", false, true
	}
	return id, true, false
}

// deviceRoutes are the routes a device token opens.
func (s *Service) deviceRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /devices/{id}/ping", s.handleDevicePing)
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: r.Method + " " + r.URL.Path})
	})
}

// serveDevice authenticates a device-scoped request by the device token
// alone - no JWT, no loopback shortcut - and serves it.
func (s *Service) serveDevice(mux http.Handler, w http.ResponseWriter, r *http.Request, id string) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	tok, hasBearer := strings.CutPrefix(auth, "Bearer ")
	tok = strings.TrimSpace(tok)
	if !hasBearer || !looksLikeDeviceToken(tok) {
		writeError(w, http.StatusUnauthorized, ErrorBody{ErrorCode: ErrUnauthorized, Detail: "this route needs the device's token (Authorization: Bearer)"})
		return
	}
	res, ok, err := s.store.AuthenticateDevice(id, tok, s.now())
	if err != nil {
		s.fail(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, ErrorBody{ErrorCode: ErrUnauthorized, Detail: "device token not valid for this device (revoked, replaced or unknown)"})
		return
	}
	if res.NewToken != "" {
		w.Header().Set(DeviceTokenHeader, res.NewToken)
		w.Header().Set(DeviceTokenOldValidUntilHeader, res.OldValidUntil.Format(time.RFC3339))
	}
	ctx := context.WithValue(r.Context(), deviceCtxKey{}, res.Device)
	ctx = context.WithValue(ctx, callerKey{}, Caller{User: "device:" + id})
	mux.ServeHTTP(w, r.WithContext(ctx))
}

func (s *Service) handleDevicePing(w http.ResponseWriter, r *http.Request) {
	d := deviceOf(r)
	if d.ID == "" || d.ID != r.PathValue("id") {
		// Unreachable: serveDevice authenticated this very id.
		writeError(w, http.StatusUnauthorized, ErrorBody{ErrorCode: ErrUnauthorized})
		return
	}
	writeOK(w, http.StatusOK, DevicePing{
		Device: d.toDevice(), ServerTime: s.now().UTC().Truncate(time.Second),
		RotatesAt: d.TokenIssuedAt.Add(DeviceTokenRotateAfter),
	})
}

func (s *Service) deviceRoot() string {
	if s.cfg.DeviceBackupRoot != "" {
		return s.cfg.DeviceBackupRoot
	}
	return DefaultDeviceBackupRoot
}

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if callerOf(r).Admin {
		return true
	}
	writeError(w, http.StatusForbidden, ErrorBody{ErrorCode: ErrForbidden, Detail: "devices are managed by an administrator account"})
	return false
}

func (s *Service) handleDevicesList(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	rows, err := s.store.ListDevices()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]Device, len(rows))
	for i, row := range rows {
		out[i] = row.toDevice()
	}
	writeOK(w, http.StatusOK, out)
}

func (s *Service) handleDeviceEnroll(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) || s.rateLimited(w, r) {
		return
	}
	var req DeviceEnrollRequest
	if !decode(w, r, &req) {
		return
	}
	fe := map[string]string{}
	name, okName := validDeviceName(req.Name)
	if !okName {
		if name == "" {
			fe["name"] = string(FieldRequired)
		} else {
			fe["name"] = string(FieldInvalid)
		}
	}
	platform, okPlat := validPlatform(req.Platform)
	if !okPlat {
		fe["platform"] = string(FieldInvalid)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	row, token, err := s.store.CreateDevice(name, platform, s.deviceRoot(), s.now())
	if errors.Is(err, errTooManyDevices) {
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrInvalidState, Detail: fmt.Sprintf("at most %d devices; remove one first", maxDevices)})
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit(r, "device_enroll", "", fmt.Sprintf("device=%s name=%q", row.ID, row.Name))
	writeOK(w, http.StatusCreated, DeviceEnrollment{Device: row.toDevice(), Token: token})
}

func (s *Service) handleDeviceRevoke(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := s.store.DeleteDevice(id); err != nil {
		s.fail(w, err)
		return
	}
	s.audit(r, "device_revoke", "", "device="+id)
	writeOK(w, http.StatusOK, struct{}{})
}
