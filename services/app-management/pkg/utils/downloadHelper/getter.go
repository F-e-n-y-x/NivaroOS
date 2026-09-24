// Package downloadHelper downloads an app store archive (a plain http(s) .zip)
// and extracts it.
//
// It used to wrap hashicorp/go-getter v1.7.0, which accepts far more than a
// zip URL (git/hg/s3 sources, "//subdir" and "?archive=" tricks, symlinks in
// archives) and has known CVEs (e.g. CVE-2024-3817). The store source is
// user-supplied, so this is now a small, bounded HTTP download + zip
// extraction.
package downloadHelper

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	// MaxDownloadSize is the largest archive that is downloaded.
	MaxDownloadSize int64 = 200 << 20
	// MaxExtractedSize is the most that is written while extracting.
	MaxExtractedSize int64 = 200 << 20
	// MaxFiles is the most entries an archive may have.
	MaxFiles = 20000
	// Timeout bounds the whole download + extraction.
	Timeout = 5 * time.Minute

	ErrUnsupportedScheme = errors.New("only http and https app store URLs are supported")
	ErrTooLarge          = errors.New("app store archive is too large")
	ErrTooManyFiles      = errors.New("app store archive has too many files")
	ErrIllegalPath       = errors.New("app store archive contains an illegal path")
)

// ValidateURL checks that src is an absolute http(s) URL with a host.
func ValidateURL(src string) error {
	u, err := url.Parse(src)
	if err != nil {
		return err
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrUnsupportedScheme
	}

	if u.Host == "" {
		return fmt.Errorf("app store URL %q has no host", src)
	}

	return nil
}

func httpClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return ErrUnsupportedScheme
			}
			return nil
		},
	}
}

// Download fetches the zip at src and extracts it into dst (created if needed).
func Download(src string, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	return DownloadContext(ctx, src, dst)
}

func DownloadContext(ctx context.Context, src string, dst string) error {
	if err := ValidateURL(src); err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "nivaroos-appstore-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}

	res, err := httpClient().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: status code %d", src, res.StatusCode)
	}

	if res.ContentLength > MaxDownloadSize {
		return ErrTooLarge
	}

	n, err := io.Copy(tmp, io.LimitReader(res.Body, MaxDownloadSize+1))
	if err != nil {
		return err
	}
	if n > MaxDownloadSize {
		return ErrTooLarge
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	return Unzip(ctx, tmp.Name(), dst)
}

// safeJoin returns dst/name, or an error if name is absolute or escapes dst
// (zip-slip).
func safeJoin(dst, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", fmt.Errorf("%w: %q", ErrIllegalPath, name)
	}

	cleanDst := filepath.Clean(dst)
	target := filepath.Join(cleanDst, filepath.FromSlash(name))

	if target != cleanDst && !strings.HasPrefix(target, cleanDst+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %q", ErrIllegalPath, name)
	}

	return target, nil
}

// Unzip extracts archive into dst. Only regular files and directories are
// extracted (symlinks and other special entries are skipped) and the total
// size and number of entries are bounded.
func Unzip(ctx context.Context, archive, dst string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()

	if len(r.File) > MaxFiles {
		return ErrTooManyFiles
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	var written int64

	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return err
		}

		target, err := safeJoin(dst, f.Name)
		if err != nil {
			return err
		}

		mode := f.Mode()

		switch {
		case mode.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		case !mode.IsRegular():
			// symlinks, devices, ... are never needed in an app store
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		n, err := extractFile(f, target, MaxExtractedSize-written)
		written += n
		if err != nil {
			return err
		}
	}

	return nil
}

func extractFile(f *zip.File, target string, remaining int64) (int64, error) {
	if remaining <= 0 || f.UncompressedSize64 > uint64(remaining) {
		return 0, ErrTooLarge
	}

	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	// the header size can lie - bound what is actually written
	n, err := io.Copy(out, io.LimitReader(rc, remaining+1))
	if err != nil {
		return n, err
	}
	if n > remaining {
		return n, ErrTooLarge
	}

	return n, nil
}
