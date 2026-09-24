package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
)

// maxMarkerSize bounds what is read as a marker: anything bigger is not
// one of ours.
const maxMarkerSize = 64 << 10

// machineHost is the marker's host id: the first 16 hex digits of
// sha256(machine-id), so a marker says which box wrote it without
// revealing the id itself.
func machineHost(path string) string {
	id := readTrimmed(path)
	if id == "" {
		if h, err := os.Hostname(); err == nil {
			id = h
		}
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:16]
}

// readMarker reads the destination identity marker at a target (spec
// §6.2). No marker is (nil, nil); a marker that can't be parsed is an
// error, because it means someone else's file sits where ours should.
func (e *Engine) readMarker(ctx context.Context, t *target) (*Marker, error) {
	var raw []byte
	if t.local {
		f, err := os.Open(filepath.Join(t.path, MarkerFile))
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, Errorf(CodeIOError, "reading the destination marker: %w", err)
		}
		defer f.Close()
		raw, err = io.ReadAll(io.LimitReader(f, maxMarkerSize+1))
		if err != nil {
			return nil, Errorf(CodeIOError, "reading the destination marker: %w", err)
		}
	} else {
		f, err := t.fsAt(ctx, "", fsOpts{})
		if err != nil {
			return nil, &Error{Code: classifyError(err), Detail: "opening the destination: " + redact(err.Error(), t.smb), Err: err}
		}
		o, err := f.NewObject(ctx, MarkerFile)
		if errors.Is(err, fs.ErrorObjectNotFound) || errors.Is(err, fs.ErrorDirNotFound) || errors.Is(err, fs.ErrorIsDir) {
			return nil, nil
		}
		if err != nil {
			return nil, &Error{Code: classifyError(err), Detail: "reading the destination marker: " + redact(err.Error(), t.smb), Err: err}
		}
		rc, err := o.Open(ctx)
		if err != nil {
			return nil, &Error{Code: classifyError(err), Detail: "reading the destination marker: " + redact(err.Error(), t.smb), Err: err}
		}
		raw, err = io.ReadAll(io.LimitReader(rc, maxMarkerSize+1))
		_ = rc.Close()
		if err != nil {
			return nil, &Error{Code: classifyError(err), Detail: "reading the destination marker: " + redact(err.Error(), t.smb), Err: err}
		}
	}
	if len(raw) > maxMarkerSize {
		return nil, Errorf(CodeDestMarkerMismatch, "%s at %s is not a NivaroOS backup marker (too large)", MarkerFile, t.display)
	}
	var m Marker
	if err := json.Unmarshal(raw, &m); err != nil || m.V < 1 || m.DestFolderID == "" {
		return nil, Errorf(CodeDestMarkerMismatch, "%s at %s is not a NivaroOS backup marker", MarkerFile, t.display)
	}
	return &m, nil
}

// checkMarker is the identity check before any write (spec §6.2): the
// marker must name destFolderID; a missing marker is only accepted for
// a job that never succeeded (firstRun). It returns the marker found.
func (e *Engine) checkMarker(ctx context.Context, t *target, destFolderID string, firstRun bool) (*Marker, error) {
	m, err := e.readMarker(ctx, t)
	if err != nil {
		return nil, err
	}
	if m == nil {
		if firstRun {
			return nil, nil
		}
		return nil, Errorf(CodeDestMarkerMismatch, "no %s at %s: this is not the folder the job wrote to, or its drive is not the one mounted", MarkerFile, t.display)
	}
	if destFolderID == "" || m.DestFolderID != destFolderID {
		return m, Errorf(CodeDestMarkerMismatch, "%s at %s belongs to another backup (folder id %s, job %s)", MarkerFile, t.display, m.DestFolderID, m.JobID)
	}
	return m, nil
}

// writeMarker creates the destination marker (first run). Local
// destinations get it atomically (temp file, fsync, rename) and private
// (0600).
func (e *Engine) writeMarker(ctx context.Context, t *target, jobID, destFolderID string) error {
	if destFolderID == "" {
		return Errorf(CodeInternal, "no dest_folder_id to write into the destination marker")
	}
	m := Marker{V: 1, JobID: jobID, DestFolderID: destFolderID, Created: e.now().UTC(), Host: e.host}
	if t.local && t.mv.dev != nil {
		m.FSUUID = t.mv.dev.UUID
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Errorf(CodeInternal, "encoding the marker: %w", err)
	}
	raw = append(raw, '\n')
	if t.local {
		return writeFileAtomic(filepath.Join(t.path, MarkerFile), raw, 0o600)
	}
	f, err := t.fsAt(ctx, "", fsOpts{})
	if err != nil {
		return &Error{Code: classifyError(err), Detail: redact(err.Error(), t.smb), Err: err}
	}
	if _, err := operations.Rcat(ctx, f, MarkerFile, io.NopCloser(bytes.NewReader(raw)), e.now(), nil); err != nil {
		return &Error{Code: classifyError(err), Detail: "writing the destination marker: " + redact(err.Error(), t.smb), Err: err}
	}
	return nil
}

// writeFileAtomic writes a small file so a reader sees either the old or
// the new content, never half of it.
func writeFileAtomic(p string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(p)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(p)+".tmp-*")
	if err != nil {
		return Errorf(classifyError(err), "writing %s: %w", p, err)
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return Errorf(classifyError(err), "writing %s: %w", p, err)
	}
	if err := tmp.Chmod(mode); err != nil && !isPermissionIgnorable(err) {
		tmp.Close()
		cleanup()
		return Errorf(CodeIOError, "writing %s: %w", p, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return Errorf(classifyError(err), "writing %s: %w", p, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return Errorf(classifyError(err), "writing %s: %w", p, err)
	}
	if err := os.Rename(name, p); err != nil {
		cleanup()
		return Errorf(classifyError(err), "writing %s: %w", p, err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// isPermissionIgnorable: FAT and exFAT can't store modes; chmod fails
// there (or does nothing) and that is not an error for us.
func isPermissionIgnorable(err error) bool {
	return errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted")
}
