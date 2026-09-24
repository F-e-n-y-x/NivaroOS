package engine

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/walk"
)

// maxReported bounds the example paths a check reports (spec §6.3: the
// first 20 collisions).
const maxReported = 20

// scanResult is what one pass over a source found.
type scanResult struct {
	Files, Bytes   int64
	TooLarge       []string // files FAT32 can't hold (first maxReported)
	TooLargeCount  int64
	Collisions     []string // "dir/a.txt | dir/A.txt" (first maxReported)
	CollisionCount int64
	Partial        bool // the time budget ran out before the walk finished
}

// scanOpts says what a scan looks for besides counting.
type scanOpts struct {
	caseCheck bool          // the destination is case-insensitive
	sizeCheck bool          // the destination is FAT32
	budget    time.Duration // 0 = walk everything
	// onFile, when set, sees every included file (archive previews).
	onFile func(remote string, size int64) error
}

// scanSource walks a source through the context's filter, counting files
// and bytes and collecting what the destination can't store. With a
// budget, a walk cut short is Partial rather than an error.
func scanSource(ctx context.Context, f fs.Fs, o scanOpts) (scanResult, error) {
	var res scanResult
	wctx := ctx
	if o.budget > 0 {
		var cancel context.CancelFunc
		wctx, cancel = context.WithTimeout(ctx, o.budget)
		defer cancel()
	}
	err := walk.Walk(wctx, f, "", false, -1, func(dir string, entries fs.DirEntries, err error) error {
		if err != nil {
			return err
		}
		var lower map[string]string
		if o.caseCheck {
			lower = make(map[string]string, len(entries))
		}
		for _, entry := range entries {
			remote := entry.Remote()
			if lower != nil {
				key := strings.ToLower(path.Base(remote))
				if other, dup := lower[key]; dup {
					res.CollisionCount++
					if len(res.Collisions) < maxReported {
						res.Collisions = append(res.Collisions, other+" | "+remote)
					}
				} else {
					lower[key] = remote
				}
			}
			obj, isObj := entry.(fs.Object)
			if !isObj {
				continue
			}
			size := obj.Size()
			res.Files++
			if size > 0 {
				res.Bytes += size
			}
			if o.sizeCheck && size > fat32MaxFile {
				res.TooLargeCount++
				if len(res.TooLarge) < maxReported {
					res.TooLarge = append(res.TooLarge, remote)
				}
			}
			if o.onFile != nil {
				if err := o.onFile(remote, size); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		if o.budget > 0 && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			res.Partial = true
			return res, nil
		}
		if errors.Is(err, fs.ErrorDirNotFound) {
			return res, nil // an empty (not yet created) folder
		}
		return res, err
	}
	return res, nil
}
