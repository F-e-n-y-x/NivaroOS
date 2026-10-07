package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/rclone/rclone/backend/crypt"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/fs/operations"
	"github.com/rclone/rclone/fs/walk"
)

// Encrypted folders (Encryption.Mode "folder", spec §18): rclone's crypt
// backend over the destination, standard file name encryption with
// encrypted folder names, the password and salt (password2) as the key
// material. The keys never go into rclone.conf or a connection string:
// crypt wraps the destination through "nivaroosfs", an in-process backend
// that hands crypt the destination's own filesystem (opened exactly as
// for a plain backup) by an id.

const wrapBackend = "nivaroosfs"

// wrapMakers maps a nivaroosfs id to the function opening the wrapped
// filesystem at a root. Ids are a hash of everything that opens it, so
// the map only grows with distinct destinations, and the same id always
// opens the same thing (rclone caches it by that id).
var wrapMakers sync.Map // id -> func(ctx, root string) (fs.Fs, error)

func init() {
	fs.Register(&fs.RegInfo{
		Name:        wrapBackend,
		Description: "NivaroOS Backup: an in-process destination wrapped by crypt",
		NewFs: func(ctx context.Context, name, root string, m configmap.Mapper) (fs.Fs, error) {
			id, _ := m.Get("id")
			mk, ok := wrapMakers.Load(id)
			if !ok {
				return nil, fmt.Errorf("%s: unknown id %q", wrapBackend, id)
			}
			return mk.(func(context.Context, string) (fs.Fs, error))(ctx, root)
		},
		Options: fs.Options{{Name: "id", Help: "Wrapped filesystem id.", Sensitive: true}},
	})
}

// encrypted reports a destination written through crypt.
func (t *target) encrypted() bool {
	return t.enc != nil && t.enc.Mode == EncryptFolder
}

// checkKeys fails closed: an encrypted endpoint without its keys is never
// opened at all.
func checkKeys(enc *Encryption) error {
	if enc == nil {
		return nil
	}
	if enc.Mode != EncryptFolder && enc.Mode != EncryptArchive {
		return Errorf(CodeInternal, "unknown encryption mode %q", enc.Mode)
	}
	if enc.Keys == nil || enc.Keys.Password == "" || (enc.Mode == EncryptFolder && enc.Keys.Salt == "") {
		return Errorf(CodeEncryptionLocked, "the backup is encrypted and its key could not be unlocked on this server")
	}
	return nil
}

// cryptFsAt opens rel of an encrypted destination: crypt over the plain
// destination, which only ever sees encrypted names.
func (t *target) cryptFsAt(ctx context.Context, rel string, o fsOpts) (fs.Fs, error) {
	sum := sha256.Sum256([]byte(strings.Join([]string{t.backend, t.fsName, t.fsRoot, t.path, fmt.Sprint(t.opts), fmt.Sprint(o.links, o.oneFileSystem)}, "\x00")))
	id := hex.EncodeToString(sum[:])[:20]
	wrapMakers.Store(id, func(ctx context.Context, root string) (fs.Fs, error) { return t.plainFsAt(ctx, root, o) })
	return newCryptFs(ctx, ":"+wrapBackend+",id="+id+":", rel, t.enc.Keys)
}

// newCryptFs is rclone crypt over remote with the given keys.
func newCryptFs(ctx context.Context, remote, rel string, k *CryptKeys) (fs.Fs, error) {
	pw, err := obscure.Obscure(k.Password)
	if err != nil {
		return nil, Errorf(CodeInternal, "obscuring the password: %w", err)
	}
	salt, err := obscure.Obscure(k.Salt)
	if err != nil {
		return nil, Errorf(CodeInternal, "obscuring the salt: %w", err)
	}
	return newBackendFs(ctx, "crypt", ":crypt", rel, configmap.Simple{
		"remote": remote, "password": pw, "password2": salt,
		"filename_encryption": "standard", "directory_name_encryption": "true",
	})
}

// cryptQuirks adapts a destination's quirks to crypt: names are base32,
// so case and Windows characters no longer matter, and crypt has no
// hashes of its own (verify uses cryptcheck instead).
func cryptQuirks(q fsQuirks) fsQuirks {
	var keep []Quirk
	for _, x := range q.Quirks {
		if x != QuirkCaseInsensitive && x != QuirkNTFSChars {
			keep = append(keep, x)
		}
	}
	q.Quirks = append(keep, QuirkNoHash)
	q.CaseInsensitive, q.NoHash = false, true
	return q
}

// unwrapCrypt finds the crypt layer of f (nil when there is none).
func unwrapCrypt(f fs.Fs) *crypt.Fs {
	for f != nil {
		if c, ok := f.(*crypt.Fs); ok {
			return c
		}
		u := f.Features().UnWrap
		if u == nil {
			return nil
		}
		f = u()
	}
	return nil
}

// cryptSample is how many files a check decrypts end to end when the
// destination has no hashes to compare.
const cryptSample = 20

// cryptCheckTrees is verifyTrees for an encrypted destination: rclone's
// cryptcheck (each source file encrypted with the stored file's nonce and
// hashed, compared with the destination's own hash). A destination
// without hashes (TeraBox, SMB) gets a sample of files downloaded and
// decrypted in full instead (crypt authenticates every 64 KiB block) and
// the rest compared by size. ok=false: nothing could be checked.
func cryptCheckTrees(ctx context.Context, src, dst fs.Fs) ([]FileError, int64, error) {
	cf := unwrapCrypt(dst)
	if cf == nil {
		return nil, 0, Errorf(CodeInternal, "cryptcheck on a destination that isn't encrypted")
	}
	ht := cf.UnWrap().Hashes().GetOne()
	if ht == hash.None {
		return decryptSample(ctx, src, cf)
	}
	var mu sync.Mutex
	var diffs []FileError
	var checked int64
	w := &sigilWriter{fn: func(sigil byte, p string) {
		mu.Lock()
		defer mu.Unlock()
		switch sigil {
		case '=':
			checked++
		case '*':
			checked++
			diffs = append(diffs, FileError{Path: p, Code: CodeIOError, Detail: "the encrypted copy doesn't match the source"})
		case '+':
			diffs = append(diffs, FileError{Path: p, Code: CodeIOError, Detail: "missing on the destination"})
		case '!':
			diffs = append(diffs, FileError{Path: p, Code: CodeIOError, Detail: "could not be checked"})
		}
	}}
	err := operations.CheckFn(ctx, &operations.CheckOpt{Fdst: cf, Fsrc: src, OneWay: true, Combined: w,
		Check: func(ctx context.Context, dst, src fs.Object) (differ, noHash bool, err error) {
			co, ok := dst.(*crypt.Object)
			if !ok {
				return true, false, errors.New("not an encrypted object")
			}
			want, err := co.UnWrap().Hash(ctx, ht)
			if err != nil || want == "" {
				return false, true, err
			}
			got, err := cf.ComputeHash(ctx, co, src, ht)
			if err != nil || got == "" {
				return false, true, err
			}
			return got != want, false, nil
		}})
	w.flush()
	if err != nil && len(diffs) > 0 {
		err = nil // the differences are the result
	}
	return diffs, checked, err
}

// decryptSample decrypts up to cryptSample random files of an encrypted
// destination in full and compares every file's size with the source.
func decryptSample(ctx context.Context, src fs.Fs, cf *crypt.Fs) ([]FileError, int64, error) {
	var objs []fs.Object
	err := walk.Walk(ctx, cf, "", false, -1, func(dir string, entries fs.DirEntries, err error) error {
		if err != nil {
			return err
		}
		for _, en := range entries {
			if o, ok := en.(fs.Object); ok && !strings.HasPrefix(o.Remote(), VersionsDir+"/") {
				objs = append(objs, o)
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
		return nil, 0, err
	}
	var diffs []FileError
	var checked int64
	for _, o := range objs {
		so, err := src.NewObject(ctx, o.Remote())
		if err == nil && so.Size() != o.Size() {
			diffs = append(diffs, FileError{Path: o.Remote(), Code: CodeIOError, Detail: "differs in size from the source"})
		}
	}
	rand.Shuffle(len(objs), func(i, j int) { objs[i], objs[j] = objs[j], objs[i] })
	for _, o := range objs[:min(len(objs), cryptSample)] {
		if err := ctx.Err(); err != nil {
			return diffs, checked, err
		}
		rc, err := o.Open(ctx)
		if err == nil {
			_, err = io.Copy(io.Discard, rc)
			rc.Close()
		}
		if err != nil {
			diffs = append(diffs, FileError{Path: o.Remote(), Code: CodeIOError, Detail: "could not be decrypted: " + err.Error()})
			continue
		}
		checked++
	}
	return diffs, checked, nil
}
