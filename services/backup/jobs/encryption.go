package jobs

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/secret"
)

// Encrypted backups (spec §18). The user's password is the key: rclone
// crypt's "password" (with a random salt as "password2") for encrypted
// folders, the 7z password for encrypted archives, so the backup also
// opens with plain rclone or 7-Zip on any computer. The server keeps the
// password sealed under the host key (common/utils/secret, context
// "backup.crypt.<job id>") in the meta row crypt.<job id> and attaches it
// to the job's destination whenever a job is loaded; no client ever gets
// it back. Beside the marker at the destination sits engine.KeyFileName:
// the salt, a check that tells a wrong password apart, and optionally the
// password sealed under a recovery key (scrypt + AES-256-GCM), so the
// recovery key unlocks the backup too. The password can't be changed:
// rclone crypt and 7z derive their keys from it.

const (
	metaCryptPrefix = "crypt."
	cryptContext    = "backup.crypt."
	minPasswordLen  = 8
	maxPasswordLen  = 1024
	checkPlain      = "nivaroos-backup-password-check"
	// scrypt cost of the key file slots (~32 MiB, ~0.1 s).
	scryptN, scryptR, scryptP = 1 << 15, 8, 1
)

// EncryptionSecret is the write-only part of POST /jobs (and /validate)
// for an encrypted destination. It is never stored as sent and never
// returned.
type EncryptionSecret struct {
	Password string `json:"password,omitempty"`
	// RecoveryKey opens an encrypted backup that already exists at the
	// destination (a reinstall), instead of its password.
	RecoveryKey     string `json:"recovery_key,omitempty"`
	MakeRecoveryKey bool   `json:"make_recovery_key,omitempty"`
}

// String keeps secrets out of %v logs.
func (s EncryptionSecret) String() string { return "encryption secret (redacted)" }

// GoString keeps them out of %#v too.
func (s EncryptionSecret) GoString() string { return s.String() }

// keyFile is engine.KeyFileName's content.
type keyFile struct {
	V        int      `json:"v"`
	Mode     string   `json:"mode"`
	Salt     string   `json:"salt,omitempty"` // rclone crypt password2 (folder mode)
	Check    kdfSlot  `json:"check"`          // opens with the password
	Recovery *kdfSlot `json:"recovery,omitempty"`
	Help     string   `json:"help"`
}

// kdfSlot is plaintext sealed with AES-256-GCM under scrypt(secret).
type kdfSlot struct {
	KDF   string `json:"kdf"` // "scrypt"
	N     int    `json:"n"`
	R     int    `json:"r"`
	P     int    `json:"p"`
	Salt  []byte `json:"salt"`
	Nonce []byte `json:"nonce"`
	CT    []byte `json:"ct"`
}

func slotAEAD(secretText string, salt []byte, n, r, p int) (cipher.AEAD, error) {
	if n < 2 || n > 1<<20 || r < 1 || r > 32 || p < 1 || p > 16 {
		return nil, errors.New("key file: scrypt parameters out of range")
	}
	key, err := scrypt.Key([]byte(secretText), salt, n, r, p, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealSlot(secretText string, plain []byte) (kdfSlot, error) {
	s := kdfSlot{KDF: "scrypt", N: scryptN, R: scryptR, P: scryptP, Salt: randBytes(16)}
	aead, err := slotAEAD(secretText, s.Salt, s.N, s.R, s.P)
	if err != nil {
		return s, err
	}
	s.Nonce = randBytes(aead.NonceSize())
	s.CT = aead.Seal(nil, s.Nonce, plain, []byte("nivaroos-backup-key-file"))
	return s, nil
}

func (s kdfSlot) open(secretText string) ([]byte, error) {
	if s.KDF != "scrypt" {
		return nil, fmt.Errorf("key file: unknown kdf %q", s.KDF)
	}
	aead, err := slotAEAD(secretText, s.Salt, s.N, s.R, s.P)
	if err != nil {
		return nil, err
	}
	if len(s.Nonce) != aead.NonceSize() {
		return nil, errors.New("key file: bad nonce")
	}
	return aead.Open(nil, s.Nonce, s.CT, []byte("nivaroos-backup-key-file"))
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on Linux
	}
	return b
}

// newRecoveryKey is 240 random bits as 8 groups of 6 base32 letters.
func newRecoveryKey() string {
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randBytes(30))
	var g []string
	for i := 0; i < len(s); i += 6 {
		g = append(g, s[i:i+6])
	}
	return strings.Join(g, "-")
}

// normRecoveryKey makes typing forgiving: case, spaces and dashes don't
// matter.
func normRecoveryKey(k string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}, k)
}

func keyFileHelp(mode string) string {
	if mode == engine.EncryptArchive {
		return "Encrypted by NivaroOS Backup & Sync: 7z archives (AES-256, file names encrypted), split into numbered volumes. Open the .7z.001 file with 7-Zip and your backup password; inside is a .tar of your files."
	}
	return "Encrypted by NivaroOS Backup & Sync with rclone crypt (filename_encryption = standard, directory_name_encryption = true). To read it without NivaroOS: an rclone crypt remote over this folder with password = your backup password and password2 = the salt above."
}

// checkPassword is the password rule; "" when it's fine.
func checkPassword(pw string) FieldCode {
	switch {
	case pw == "":
		return FieldRequired
	case utf8.RuneCountInString(pw) < minPasswordLen, len(pw) > maxPasswordLen:
		return FieldPasswordShort
	case !utf8.ValidString(pw) || strings.ContainsAny(pw, "\x00\r\n"):
		// 7z reads the password as one line from its stdin.
		return FieldInvalid
	}
	return ""
}

// newCryptKeys makes the keys of a new encrypted backup; recovery is the
// recovery key to show once ("" when none was asked for).
func newCryptKeys(mode, pw string, makeRecovery bool) (keys engine.CryptKeys, recovery string, err error) {
	kf := keyFile{V: 1, Mode: mode, Help: keyFileHelp(mode)}
	if mode == engine.EncryptFolder {
		kf.Salt = base64.RawURLEncoding.EncodeToString(randBytes(32))
	}
	if kf.Check, err = sealSlot(pw, []byte(checkPlain)); err != nil {
		return keys, "", err
	}
	if makeRecovery {
		recovery = newRecoveryKey()
		slot, err := sealSlot(normRecoveryKey(recovery), []byte(pw))
		if err != nil {
			return keys, "", err
		}
		kf.Recovery = &slot
	}
	raw, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return keys, "", err
	}
	return engine.CryptKeys{Password: pw, Salt: kf.Salt, KeyFile: append(raw, '\n')}, recovery, nil
}

// adoptCryptKeys opens the encrypted backup already at a destination (its
// key file raw) with the password or the recovery key.
func adoptCryptKeys(raw []byte, mode string, sec EncryptionSecret) (keys engine.CryptKeys, hasRecovery bool, err error) {
	var kf keyFile
	if err := json.Unmarshal(raw, &kf); err != nil || kf.V < 1 {
		return keys, false, &engine.Error{Code: ErrDestMarkerMismatch, Detail: "the destination's key file can't be read"}
	}
	if kf.Mode != mode {
		return keys, false, &engine.Error{Code: ErrDestMarkerMismatch, Detail: fmt.Sprintf("the backup there is encrypted as %q, not %q", kf.Mode, mode)}
	}
	pw := sec.Password
	if sec.RecoveryKey != "" {
		if kf.Recovery == nil {
			return keys, false, &engine.Error{Code: ErrWrongPassword, Detail: "that backup has no recovery key"}
		}
		p, err := kf.Recovery.open(normRecoveryKey(sec.RecoveryKey))
		if err != nil {
			return keys, false, &engine.Error{Code: ErrWrongPassword, Detail: "the recovery key does not open the backup"}
		}
		pw = string(p)
	}
	if plain, err := kf.Check.open(pw); err != nil || string(plain) != checkPlain {
		return keys, false, &engine.Error{Code: ErrWrongPassword, Detail: "the password does not open the backup"}
	}
	return engine.CryptKeys{Password: pw, Salt: kf.Salt, KeyFile: raw}, kf.Recovery != nil, nil
}

// ---------------------------------------------------------------------
// Storage

// cryptRecord is meta row crypt.<job id>.
type cryptRecord struct {
	Sealed  string          `json:"sealed"` // secret.Box sealed JSON cryptSecret
	KeyFile json.RawMessage `json:"key_file"`
}

type cryptSecret struct {
	Password string `json:"password"`
	Salt     string `json:"salt,omitempty"`
}

func (s *Store) secretBox() (*secret.Box, error) {
	if s.Secrets != nil {
		return s.Secrets()
	}
	return secret.LoadOrCreate(secret.DefaultKeyPath)
}

// SaveCryptKeys stores a job's keys, sealed.
func (s *Store) SaveCryptKeys(jobID string, k engine.CryptKeys) error {
	box, err := s.secretBox()
	if err != nil {
		return fmt.Errorf("store: host secret key: %w", err)
	}
	plain, err := json.Marshal(cryptSecret{Password: k.Password, Salt: k.Salt})
	if err != nil {
		return err
	}
	sealed, err := box.Seal(string(plain), cryptContext+jobID)
	if err != nil {
		return err
	}
	return s.SetMetaJSON(metaCryptPrefix+jobID, cryptRecord{Sealed: sealed, KeyFile: k.KeyFile})
}

// cryptKeys loads a job's keys.
func (s *Store) cryptKeys(jobID string) (*engine.CryptKeys, error) {
	var rec cryptRecord
	ok, err := s.GetMetaJSON(metaCryptPrefix+jobID, &rec)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("no keys stored")
	}
	box, err := s.secretBox()
	if err != nil {
		return nil, err
	}
	if !secret.IsSealed(rec.Sealed) {
		return nil, errors.New("keys are not sealed")
	}
	plain, err := box.Open(rec.Sealed, cryptContext+jobID)
	if err != nil {
		return nil, err
	}
	var cs cryptSecret
	if err := json.Unmarshal([]byte(plain), &cs); err != nil {
		return nil, err
	}
	return &engine.CryptKeys{Password: cs.Password, Salt: cs.Salt, KeyFile: rec.KeyFile}, nil
}

// unlock attaches a loaded job's keys to its destination. A job whose
// keys can't be opened keeps its Encryption without Keys: the engine then
// refuses it (encryption_locked) rather than writing anything in clear.
func (s *Store) unlock(j *Job) {
	if j.Dest.Encryption == nil {
		return
	}
	enc := *j.Dest.Encryption
	enc.Keys = nil
	j.Dest.Encryption = &enc
	k, err := s.cryptKeys(j.ID)
	if err != nil {
		log.Printf("backup: job %s: the encryption key can't be opened: %v", j.ID, err)
		return
	}
	enc.Keys = k
}

// DropCryptKeys forgets a job's keys.
func (s *Store) DropCryptKeys(jobID string) error { return s.DeleteMeta(metaCryptPrefix + jobID) }

// ---------------------------------------------------------------------
// Validation and API

// normalizeEncryption checks the shape of dest.encryption (spec §18).
func normalizeEncryption(j *Job, fe fieldErrors) {
	for i := range j.Sources {
		if j.Sources[i].Encryption != nil {
			fe.add(fmt.Sprintf("sources[%d].encryption", i), FieldInvalid)
		}
	}
	enc := j.Dest.Encryption
	if enc == nil {
		return
	}
	enc.Keys = nil
	switch enc.Mode {
	case engine.EncryptFolder:
		enc.VolumeBytes = 0
	case engine.EncryptArchive:
		if j.Type != TypeArchive {
			fe.add("dest.encryption.mode", FieldInvalid)
		}
		if enc.VolumeBytes == 0 {
			enc.VolumeBytes = engine.DefaultVolumeBytes
		} else if enc.VolumeBytes < minVolumeBytes || enc.VolumeBytes > maxVolumeBytes {
			fe.add("dest.encryption.volume_bytes", FieldOutOfRange)
		}
		if engine.SevenZip() == "" {
			fe.add("dest.encryption.mode", ErrSevenZipMissing)
		}
	default:
		fe.add("dest.encryption.mode", FieldInvalid)
	}
}

// 7z volume limits: 100 MB .. 4 GiB - 1 MiB (FAT32, TeraBox free).
const (
	minVolumeBytes = 100_000_000
	maxVolumeBytes = 4<<30 - 1<<20
)

// prepareCryptKeys makes (or, when an encrypted backup is already at the
// destination, opens) the keys of a job being created. fe gets the
// password problems.
func (s *Service) prepareCryptKeys(ctx context.Context, j *Job, sec *EncryptionSecret, fe fieldErrors) (keys engine.CryptKeys, recovery string, err error) {
	enc := j.Dest.Encryption
	if sec == nil {
		sec = &EncryptionSecret{}
	}
	var existing []byte
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	creds, cerr := s.smbCredsFor(rctx, []Endpoint{j.Dest})
	if cerr == nil {
		probe := j.Dest
		probe.Encryption = nil // reading the key file needs no keys
		req := engine.ResolveRequest{Endpoint: probe}
		if c, ok := creds[probe.RefID]; ok {
			req.SMBCreds = &c
		}
		if res, err := s.engine.Resolve(rctx, req); err == nil {
			existing = res.KeyFile
		}
	}
	if len(existing) > 0 {
		if sec.RecoveryKey == "" {
			if code := checkPassword(sec.Password); code != "" {
				fe.add("encryption_secret.password", code)
				return keys, "", nil
			}
		}
		keys, hasRecovery, err := adoptCryptKeys(existing, enc.Mode, *sec)
		if err != nil {
			fe.add("encryption_secret.password", engine.CodeOf(err))
			return keys, "", nil
		}
		enc.RecoveryKey = hasRecovery
		return keys, "", nil
	}
	if code := checkPassword(sec.Password); code != "" {
		fe.add("encryption_secret.password", code)
		return keys, "", nil
	}
	keys, recovery, err = newCryptKeys(enc.Mode, sec.Password, sec.MakeRecoveryKey)
	enc.RecoveryKey = recovery != ""
	return keys, recovery, err
}

// EncryptionTestRequest is POST /jobs/:id/encryption/test.
type EncryptionTestRequest struct {
	Password string `json:"password"`
}

// EncryptionTestResult says whether the password (or recovery key)
// unlocks the job's backup.
type EncryptionTestResult struct {
	OK bool `json:"ok"`
}

func (s *Service) handleEncryptionTest(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(w, r) {
		return
	}
	var req EncryptionTestRequest
	if !decode(w, r, &req) {
		return
	}
	j, err := s.store.GetJob(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	enc := j.Dest.Encryption
	if enc == nil {
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrInvalidState, Detail: "this job's backups are not encrypted"})
		return
	}
	if enc.Keys == nil {
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrEncryptionLocked})
		return
	}
	ok := subtle.ConstantTimeCompare([]byte(req.Password), []byte(enc.Keys.Password)) == 1
	if !ok && len(enc.Keys.KeyFile) > 0 {
		// A recovery key works too.
		var kf keyFile
		if json.Unmarshal(enc.Keys.KeyFile, &kf) == nil && kf.Recovery != nil {
			if p, err := kf.Recovery.open(normRecoveryKey(req.Password)); err == nil {
				ok = subtle.ConstantTimeCompare(p, []byte(enc.Keys.Password)) == 1
			}
		}
	}
	s.audit(r, "encryption_test", j.ID, fmt.Sprintf("ok=%v", ok))
	writeOK(w, http.StatusOK, EncryptionTestResult{OK: ok})
}

func encryptionMode(ep Endpoint) string {
	if ep.Encryption == nil {
		return ""
	}
	return ep.Encryption.Mode
}
