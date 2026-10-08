package engine

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DefaultAllowedRoots are where backups may write: destinations and
// restore targets (spec §6.2). The staging folder is added from
// Config.StagingDir. Sources may be read from anywhere outside
// readDeniedRoots (owner request 2026-10-08: back up users' homes and
// other system-drive folders, not only /DATA).
var DefaultAllowedRoots = []string{"/DATA", "/mnt", "/media", "/home", "/root", "/srv", "/opt"}

// deniedRoots are never written, even if an allowed root is configured
// below them - except the staging folder, which is under
// /var/lib/nivaroos on purpose.
var deniedRoots = []string{"/proc", "/sys", "/dev", "/boot", "/etc", "/run", "/var/lib/nivaroos"}

// readDeniedRoots are never read as a source: kernel and runtime views,
// not files. Backup's own staging and spool folders are added in
// newRootPolicy (a job backing them up would chase its own tail).
var readDeniedRoots = []string{"/proc", "/sys", "/dev", "/run"}

// pathWithin reports whether p is base or below it. Both must be clean
// and absolute.
func pathWithin(p, base string) bool {
	if base == "/" {
		return strings.HasPrefix(p, "/")
	}
	return p == base || strings.HasPrefix(p, base+"/")
}

// cleanSubPath validates and normalises an endpoint sub-path or a path
// relative to it: slash separated, no "..", no NUL, no leading "/".
// "" and "." both mean the root.
func cleanSubPath(sub string) (string, error) {
	if strings.ContainsRune(sub, 0) {
		return "", Errorf(CodePathNotAllowed, "path contains a NUL byte")
	}
	if strings.HasPrefix(sub, "/") {
		return "", Errorf(CodePathNotAllowed, "path %q must be relative", sub)
	}
	for _, part := range strings.Split(sub, "/") {
		if part == ".." {
			return "", Errorf(CodePathNotAllowed, "path %q may not contain ..", sub)
		}
	}
	c := path.Clean("/" + sub)
	if c == "/" {
		return "", nil
	}
	return strings.TrimPrefix(c, "/"), nil
}

// joinSub joins relative sub-paths (already clean) with "/".
func joinSub(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, "/")
}

// evalExisting resolves symlinks in p like filepath.EvalSymlinks, but p
// may not exist yet (a destination folder is created on the first run):
// the longest existing prefix is resolved and the rest appended.
func evalExisting(p string) (string, error) {
	p = filepath.Clean(p)
	rest := ""
	cur := p
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			if rest == "" {
				return real, nil
			}
			return filepath.Join(real, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// rootPolicy is the allowed/denied roots rule (spec §6.2).
type rootPolicy struct {
	allowed    []string // clean, absolute, symlinks resolved
	staging    string   // allowed although under /var/lib/nivaroos
	denied     []string
	readDenied []string
}

// newRootPolicy: own lists Backup's working folders (staging, spool),
// which are never a source.
func newRootPolicy(allowed []string, staging string, own ...string) rootPolicy {
	p := rootPolicy{denied: deniedRoots, readDenied: append([]string{}, readDeniedRoots...)}
	for _, a := range allowed {
		if a == "" {
			continue
		}
		p.allowed = append(p.allowed, resolveRoot(a))
	}
	if staging != "" {
		p.staging = resolveRoot(staging)
		p.allowed = append(p.allowed, p.staging)
		p.readDenied = append(p.readDenied, p.staging)
	}
	for _, o := range own {
		if o != "" {
			p.readDenied = append(p.readDenied, resolveRoot(o))
		}
	}
	return p
}

// checkRead reports whether a real (symlink-free) path may be read as a
// source or browsed. The error is path_not_allowed.
func (p rootPolicy) checkRead(real string) error {
	for _, d := range p.readDenied {
		if pathWithin(real, d) {
			return Errorf(CodePathNotAllowed, "%s is inside %s, which backups never read", real, d)
		}
	}
	return nil
}

// checkFor is checkRead for a source, check otherwise.
func (p rootPolicy) checkFor(real string, read bool) error {
	if read {
		return p.checkRead(real)
	}
	return p.check(real)
}

// resolveRoot cleans a configured root and resolves symlinks in it when
// it exists (so a /DATA symlink to another disk still matches real
// paths).
func resolveRoot(r string) string {
	r = filepath.Clean(r)
	if real, err := evalExisting(r); err == nil {
		return real
	}
	return r
}

// check reports whether a real (symlink-free) path may be read or
// written. The error is path_not_allowed.
func (p rootPolicy) check(real string) error {
	if p.staging != "" && pathWithin(real, p.staging) {
		return nil
	}
	for _, d := range p.denied {
		if pathWithin(real, d) {
			return Errorf(CodePathNotAllowed, "%s is inside %s, which backups never use", real, d)
		}
	}
	for _, a := range p.allowed {
		if pathWithin(real, a) {
			return nil
		}
	}
	return Errorf(CodePathNotAllowed, "%s is outside the folders backups may use (%s)", real, strings.Join(p.allowed, ", "))
}

// allowedAncestor reports whether dir contains an allowed root (the root
// filesystem contains /DATA), so a volume mounted there is listed even
// though its mount point itself is not an allowed root.
func (p rootPolicy) allowedAncestor(dir string) bool {
	for _, a := range p.allowed {
		if pathWithin(a, dir) {
			return true
		}
	}
	return false
}

// lstatDir reports whether p exists and is a directory (not following a
// final symlink).
func lstatDir(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir()
}

// escapeGlob quotes the characters rclone's filter globs treat
// specially, so a literal path can be used in a rule.
func escapeGlob(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '*', '?', '[', ']', '{', '}':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
