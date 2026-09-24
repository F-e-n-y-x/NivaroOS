package model

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Mount points the storage code is allowed to create/mount on: exactly one
// level below one of these roots, e.g. /mnt/Storage_sdb1. Anything else
// (a nested path, "..", /etc, a path with shell metacharacters) is refused.
var MountRoots = []string{"/mnt", "/media", "/DATA"}

var safeMountNameRe = regexp.MustCompile(`^[A-Za-z0-9 _.-]+$`)

var (
	ErrInvalidMountPoint  = errors.New("invalid mount point")
	ErrInvalidStorageName = errors.New("invalid storage name")
)

// IsSafeMountName: a single path element made only of [A-Za-z0-9 _.-], not
// "." / "..", no leading "-" (would read as an option to mount/mkdir) and no
// leading/trailing space.
func IsSafeMountName(name string) bool {
	if name == "" || len(name) > 128 || name == "." || name == ".." {
		return false
	}
	if strings.HasPrefix(name, "-") || strings.TrimSpace(name) != name {
		return false
	}
	return safeMountNameRe.MatchString(name)
}

// ValidateMountPoint accepts only a clean absolute path <root>/<safe name>
// with root one of MountRoots.
func ValidateMountPoint(mp string) error {
	if mp == "" || !filepath.IsAbs(mp) || filepath.Clean(mp) != mp {
		return fmt.Errorf("%w: %q must be a clean absolute path", ErrInvalidMountPoint, mp)
	}
	dir, base := filepath.Dir(mp), filepath.Base(mp)
	okRoot := false
	for _, r := range MountRoots {
		if dir == r {
			okRoot = true
			break
		}
	}
	if !okRoot {
		return fmt.Errorf("%w: %q must be directly under %s", ErrInvalidMountPoint, mp, strings.Join(MountRoots, ", "))
	}
	if !IsSafeMountName(base) {
		return fmt.Errorf("%w: %q - the folder name may only contain letters, digits, space, '_', '.', '-'", ErrInvalidMountPoint, mp)
	}
	return nil
}

// ValidateStorageName checks a client-supplied storage name (used as the
// mount folder name). Path separators and ".." are refused outright; other
// characters are sanitised by SanitizeMountName.
func ValidateStorageName(name string) error {
	if strings.ContainsAny(name, "/\\\x00") || strings.Contains(name, "..") {
		return fmt.Errorf("%w: %q may not contain '/', '\\' or '..'", ErrInvalidStorageName, name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: control characters are not allowed", ErrInvalidStorageName)
		}
	}
	if len(name) > 64 {
		return fmt.Errorf("%w: name is too long (max 64)", ErrInvalidStorageName)
	}
	return nil
}

// SanitizeMountName maps every character outside [A-Za-z0-9 _.-] to '_' and
// trims what would make the name unsafe (leading '-', surrounding spaces,
// dots-only names). Device-provided strings (label, model) go through this.
func SanitizeMountName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == ' ', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimLeft(out, "-")
	for strings.Contains(out, "..") {
		out = strings.ReplaceAll(out, "..", "_")
	}
	if strings.Trim(out, ".") == "" {
		out = ""
	}
	if len(out) > 120 {
		out = out[:120]
	}
	return strings.TrimSpace(out)
}
