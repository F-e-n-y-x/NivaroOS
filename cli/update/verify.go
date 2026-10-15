// Package update is `nivaroos update`: the server self-updater's check,
// download, verify, apply and rollback (self-updater design, sections 6-9).
// The apply step itself is the release's own apply.sh.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The release signing key (minisign; the secret half never leaves the
// release server). Only keys compiled in are trusted - never one fetched.
//
//go:embed nivaroos-server.pub
var releaseKey string

type pubKey struct {
	id  [8]byte
	key ed25519.PublicKey
}

func parsePub(file string) (pubKey, error) {
	var pk pubKey
	lines := strings.Split(strings.TrimSpace(file), "\n")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[len(lines)-1]))
	if err != nil || len(raw) != 42 || string(raw[:2]) != "Ed" {
		return pk, errors.New("not a minisign public key")
	}
	copy(pk.id[:], raw[2:10])
	pk.key = ed25519.PublicKey(raw[10:])
	return pk, nil
}

// verifySig checks a .minisig over msg and returns its trusted comment.
// Only legacy signatures (Ed25519 over the file itself: `minisign -S -l`)
// are accepted - SHA256SUMS is tiny, and that keeps the check stdlib-only.
func verifySig(pk pubKey, msg, sig []byte) (string, error) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimRight(string(sig), "\r\n"), "\r", ""), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "untrusted comment:") || !strings.HasPrefix(lines[2], "trusted comment: ") {
		return "", errors.New("malformed signature file")
	}
	s, err := base64.StdEncoding.DecodeString(lines[1])
	if err != nil || len(s) != 74 {
		return "", errors.New("malformed signature")
	}
	switch string(s[:2]) {
	case "Ed":
	case "ED":
		return "", errors.New("prehashed signature: sign with minisign -l")
	default:
		return "", errors.New("unknown signature algorithm")
	}
	if !bytes.Equal(s[2:10], pk.id[:]) {
		return "", errors.New("signed with an unknown key")
	}
	if !ed25519.Verify(pk.key, msg, s[10:]) {
		return "", errors.New("bad signature")
	}
	comment := strings.TrimPrefix(lines[2], "trusted comment: ")
	g, err := base64.StdEncoding.DecodeString(lines[3])
	if err != nil || len(g) != 64 || !ed25519.Verify(pk.key, append(append([]byte{}, s[10:]...), comment...), g) {
		return "", errors.New("bad trusted comment signature")
	}
	return comment, nil
}

// TrustedComment is what release-server.sh signs: a validly signed old
// release served as new, or a beta served on stable, doesn't match.
func TrustedComment(version, channel string) string {
	return fmt.Sprintf("nivaroos-server %s %s", version, channel)
}

// TarName is the bundle file of a version for an architecture.
func TarName(version, arch string) string {
	return fmt.Sprintf("nivaroos-server-%s-linux-%s.tar.gz", version, arch)
}

// Manifest is a bundle's MANIFEST (see installer/build-bundle.sh).
type Manifest struct {
	Version, Channel, Commit, Arch, MinFrom, GlibcMin string
	Files                                             map[string]string // bundle path -> sha256
}

func parseManifest(r io.Reader) (*Manifest, error) {
	m := &Manifest{Files: map[string]string{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "version":
			m.Version = f[1]
		case "channel":
			m.Channel = f[1]
		case "commit":
			m.Commit = f[1]
		case "arch":
			m.Arch = f[1]
		case "min_from":
			m.MinFrom = f[1]
		case "glibc_min":
			m.GlibcMin = f[1]
		case "file":
			if len(f) != 5 {
				return nil, fmt.Errorf("bad MANIFEST line %q", sc.Text())
			}
			m.Files[f[4]] = f[3]
		}
	}
	return m, sc.Err()
}

// Expect is what a verified bundle must be.
type Expect struct{ Version, Channel, Arch string }

// Stage verifies a downloaded bundle and extracts it into stageDir/<version>:
// the signature over SHA256SUMS with the compiled-in key, its trusted
// comment, the tarball's hash, then - from the same open file, so nothing
// can swap it in between - a safe extract, and every file against MANIFEST.
func Stage(tarPath string, sums, sig []byte, want Expect, stageDir string) (string, *Manifest, error) {
	pk, err := parsePub(releaseKey)
	if err != nil {
		return "", nil, err
	}
	return stage(pk, tarPath, sums, sig, want, stageDir)
}

func stage(pk pubKey, tarPath string, sums, sig []byte, want Expect, stageDir string) (string, *Manifest, error) {
	comment, err := verifySig(pk, sums, sig)
	if err != nil {
		return "", nil, fmt.Errorf("signature check failed: %w", err)
	}
	if exp := TrustedComment(want.Version, want.Channel); comment != exp {
		return "", nil, fmt.Errorf("signature is for %q, not %q", comment, exp)
	}
	name := TarName(want.Version, want.Arch)
	var sum string
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			sum = f[0]
		}
	}
	if sum == "" {
		return "", nil, fmt.Errorf("SHA256SUMS has no %s", name)
	}
	f, err := os.Open(tarPath)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", nil, err
	}
	if hex.EncodeToString(h.Sum(nil)) != sum {
		return "", nil, errors.New("the download does not match SHA256SUMS")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", nil, err
	}

	dst := filepath.Join(stageDir, want.Version)
	tmp := dst + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return "", nil, err
	}
	got, err := extract(f, tmp, strings.TrimSuffix(name, ".tar.gz"))
	if err != nil {
		_ = os.RemoveAll(tmp)
		return "", nil, err
	}
	m, err := checkManifest(tmp, got)
	if err == nil && (m.Version != want.Version || m.Arch != want.Arch) {
		err = fmt.Errorf("bundle is %s/%s, not %s/%s", m.Version, m.Arch, want.Version, want.Arch)
	}
	if err != nil {
		_ = os.RemoveAll(tmp)
		return "", nil, err
	}
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return "", nil, err
	}
	return dst, m, nil
}

// extract unpacks only directories and regular files, all under top/,
// with no absolute or .. paths; it returns each file's sha256.
func extract(r io.Reader, dir, top string) (map[string]string, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(zr)
	sums := map[string]string{}
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			return sums, nil
		}
		if err != nil {
			return nil, err
		}
		clean := path.Clean(hd.Name)
		if clean == top {
			continue
		}
		rel, ok := strings.CutPrefix(clean, top+"/")
		if !ok || path.IsAbs(hd.Name) || rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
			return nil, fmt.Errorf("unsafe path in bundle: %q", hd.Name)
		}
		p := filepath.Join(dir, filepath.FromSlash(rel))
		switch hd.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return nil, err
			}
			out, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(hd.Mode)&0o755)
			if err != nil {
				return nil, err
			}
			h := sha256.New()
			_, err = io.Copy(io.MultiWriter(out, h), tr)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return nil, err
			}
			sums[rel] = hex.EncodeToString(h.Sum(nil))
		default:
			return nil, fmt.Errorf("bundle entry %q is not a file or folder", hd.Name)
		}
	}
}

// checkManifest: exactly the files MANIFEST lists, each with its hash.
func checkManifest(dir string, got map[string]string) (*Manifest, error) {
	f, err := os.Open(filepath.Join(dir, "MANIFEST"))
	if err != nil {
		return nil, errors.New("bundle has no MANIFEST")
	}
	defer f.Close()
	m, err := parseManifest(f)
	if err != nil {
		return nil, err
	}
	delete(got, "MANIFEST")
	for p, sum := range m.Files {
		if got[p] != sum {
			return nil, fmt.Errorf("%s does not match MANIFEST", p)
		}
	}
	if len(got) != len(m.Files) {
		return nil, errors.New("bundle has files MANIFEST doesn't list")
	}
	return m, nil
}
