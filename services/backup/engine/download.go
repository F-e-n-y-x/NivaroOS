package engine

import (
	"archive/tar"
	"archive/zip"
	"context"
	"errors"
	"io"
	"mime"
	"path"
	"strings"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/walk"
)

// OpenDownload opens files of a version for streaming to the browser: a
// single file raw, anything else (several paths, a folder) as a zip that
// is built while it streams. The caller must close Body; closing it early
// stops the work.
func (e *Engine) OpenDownload(ctx context.Context, req DownloadRequest) (*Download, error) {
	if len(req.Paths) == 0 {
		return nil, Errorf(CodeNotFound, "nothing to download")
	}
	paths := make([]string, 0, len(req.Paths))
	for _, p := range req.Paths {
		c, err := cleanSubPath(p)
		if err != nil {
			return nil, err
		}
		if c == "" {
			return nil, Errorf(CodePathNotAllowed, "the whole backup can't be downloaded; pick files or folders")
		}
		paths = append(paths, c)
	}
	t, err := e.resolve(ctx, req.Dest, req.SMBCreds)
	if err != nil {
		return nil, err
	}
	if !t.online {
		return nil, Errorf(CodeDestOffline, "%s is not available", t.display)
	}
	dir, archive, err := versionRoot(req.VersionID)
	if err != nil {
		return nil, err
	}
	// The stream outlives this call: it runs under the engine's context,
	// ended by Body.Close or Close.
	sctx, cancel := context.WithCancel(e.ctx)
	if archive != "" {
		d, err := e.downloadArchive(sctx, t, archive, paths)
		if err != nil {
			cancel()
			return nil, err
		}
		d.Body = &cancelCloser{ReadCloser: d.Body, cancel: cancel}
		return d, nil
	}
	f, err := t.fsAt(sctx, dir, fsOpts{})
	if err != nil {
		cancel()
		return nil, engineErr(err, "opening the backup")
	}
	if len(paths) == 1 {
		if o, err := f.NewObject(sctx, paths[0]); err == nil && !hiddenNames[path.Base(paths[0])] {
			rc, err := o.Open(sctx)
			if err != nil {
				cancel()
				return nil, engineErr(err, "opening "+paths[0])
			}
			name := path.Base(paths[0])
			return &Download{Name: name, ContentType: contentType(name), Size: o.Size(), Body: &cancelCloser{ReadCloser: rc, cancel: cancel}}, nil
		}
	}
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(zipTree(sctx, f, paths, pw))
	}()
	return &Download{Name: zipName(paths), ContentType: "application/zip", Size: -1, Body: &cancelCloser{ReadCloser: pr, cancel: cancel}}, nil
}

// zipTree writes the files at paths (files or folders) of f as a zip.
func zipTree(ctx context.Context, f fs.Fs, paths []string, w io.Writer) error {
	zw := zip.NewWriter(w)
	add := func(o fs.Object) error {
		if hiddenNames[path.Base(o.Remote())] {
			return nil
		}
		hdr := &zip.FileHeader{Name: o.Remote(), Method: zip.Deflate, Modified: o.ModTime(ctx)}
		dst, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		rc, err := o.Open(ctx)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, rc)
		rc.Close()
		return err
	}
	found := false
	for _, p := range paths {
		if o, err := f.NewObject(ctx, p); err == nil {
			found = true
			if err := add(o); err != nil {
				return err
			}
			continue
		}
		err := walk.Walk(ctx, f, p, false, -1, func(_ string, entries fs.DirEntries, err error) error {
			if err != nil {
				return err
			}
			for _, en := range entries {
				if o, ok := en.(fs.Object); ok {
					found = true
					if err := add(o); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
			return err
		}
	}
	if !found {
		return Errorf(CodeNotFound, "none of the files are in this version")
	}
	return zw.Close()
}

// downloadArchive streams members of an archive: one file raw, else a
// zip of the selected members.
func (e *Engine) downloadArchive(ctx context.Context, t *target, archive string, paths []string) (*Download, error) {
	f, err := t.fsAt(ctx, "", fsOpts{})
	if err != nil {
		return nil, engineErr(err, "opening the backup")
	}
	single := ""
	if len(paths) == 1 {
		if idx, ok, err := readIndex(ctx, f, archive); err == nil && ok {
			for _, en := range idx.entries {
				if en.P == paths[0] && !en.D && en.L == "" {
					single = en.P
				}
			}
		}
	}
	tr, closer, err := openArchive(ctx, f, archive)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	if single != "" {
		go func() {
			defer closer()
			for {
				h, err := tr.Next()
				if err == io.EOF {
					pw.CloseWithError(Errorf(CodeNotFound, "%s is not in the archive", single))
					return
				}
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				if strings.TrimSuffix(h.Name, "/") == single && h.Typeflag == tar.TypeReg {
					_, err := io.Copy(pw, tr)
					pw.CloseWithError(err)
					return
				}
			}
		}()
		name := path.Base(single)
		return &Download{Name: name, ContentType: contentType(name), Size: -1, Body: pr}, nil
	}
	go func() {
		defer closer()
		zw := zip.NewWriter(pw)
		found := false
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			rel, err := cleanSubPath(strings.TrimSuffix(h.Name, "/"))
			if err != nil || rel == "" || h.Typeflag != tar.TypeReg || !selected(paths, rel) {
				continue
			}
			found = true
			dst, err := zw.CreateHeader(&zip.FileHeader{Name: rel, Method: zip.Deflate, Modified: h.ModTime})
			if err == nil {
				_, err = io.Copy(dst, tr)
			}
			if err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		if !found {
			pw.CloseWithError(Errorf(CodeNotFound, "none of the files are in the archive"))
			return
		}
		pw.CloseWithError(zw.Close())
	}()
	return &Download{Name: zipName(paths), ContentType: "application/zip", Size: -1, Body: pr}, nil
}

func zipName(paths []string) string {
	if len(paths) == 1 {
		return path.Base(paths[0]) + ".zip"
	}
	return "backup-files.zip"
}

func contentType(name string) string {
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// cancelCloser ends a download's work when its body is closed.
type cancelCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
