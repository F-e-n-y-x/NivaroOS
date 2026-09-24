package engine

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
)

// hiddenNames are never shown in a listing: the engine's own bookkeeping.
var hiddenNames = map[string]bool{VersionsDir: true, MarkerFile: true}

// Browse lists one directory of an endpoint, or of a version of a job's
// destination, never above the endpoint root.
func (e *Engine) Browse(ctx context.Context, req BrowseRequest) (BrowseResult, error) {
	rel, err := cleanSubPath(req.Path)
	if err != nil {
		return BrowseResult{}, err
	}
	t, err := e.resolve(ctx, req.Endpoint, req.SMBCreds)
	if err != nil {
		return BrowseResult{}, err
	}
	if !t.online {
		return BrowseResult{}, Errorf(CodeDestOffline, "%s is not available", t.display)
	}
	dir, archive, err := versionRoot(req.VersionID)
	if err != nil {
		return BrowseResult{}, err
	}
	var entries []Entry
	switch {
	case archive != "":
		entries, err = e.browseArchive(ctx, t, archive, rel)
	case t.local:
		entries, err = browseLocal(t, joinSub(dir, rel), e.policy)
	default:
		entries, err = browseRemote(ctx, t, joinSub(dir, rel))
	}
	if err != nil {
		return BrowseResult{}, err
	}
	var out []Entry
	for _, en := range entries {
		if hiddenNames[en.Name] {
			continue
		}
		if req.DirsOnly && !en.Dir {
			continue
		}
		out = append(out, en)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	res := BrowseResult{Path: rel, Entries: out}
	if len(out) > MaxBrowseEntries {
		res.Entries, res.Truncated = out[:MaxBrowseEntries], true
	}
	if res.Entries == nil {
		res.Entries = []Entry{}
	}
	return res, nil
}

// browseLocal reads a local folder directly (no symlink is followed: a
// link is listed as what it is, never entered).
func browseLocal(t *target, rel string, policy rootPolicy) ([]Entry, error) {
	p := filepath.Join(t.path, filepath.FromSlash(rel))
	real, err := filepath.EvalSymlinks(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, Errorf(CodeNotFound, "%s does not exist", rel)
	}
	if err != nil {
		return nil, Errorf(CodeIOError, "reading %s: %w", p, err)
	}
	if !pathWithin(real, t.mountPoint) || !pathWithin(real, t.path) {
		return nil, Errorf(CodePathNotAllowed, "%s leads outside the location", rel)
	}
	if err := policy.check(real); err != nil {
		return nil, err
	}
	des, err := os.ReadDir(real)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, Errorf(CodeNotFound, "%s does not exist", rel)
		}
		return nil, Errorf(CodeIOError, "reading %s: %w", p, err)
	}
	out := make([]Entry, 0, len(des))
	for _, d := range des {
		fi, err := d.Info()
		if err != nil {
			continue // removed while listing
		}
		mt := fi.ModTime()
		en := Entry{Name: d.Name(), Dir: fi.IsDir(), MTime: &mt}
		if !en.Dir {
			en.Size = fi.Size()
		}
		out = append(out, en)
	}
	return out, nil
}

func browseRemote(ctx context.Context, t *target, rel string) ([]Entry, error) {
	f, err := t.fsAt(ctx, "", fsOpts{})
	if err != nil {
		return nil, engineErr(err, "opening the location")
	}
	entries, err := f.List(ctx, rel)
	if errors.Is(err, fs.ErrorDirNotFound) {
		return nil, Errorf(CodeNotFound, "%s does not exist", rel)
	}
	if err != nil {
		return nil, engineErr(err, "listing "+rel)
	}
	out := make([]Entry, 0, len(entries))
	for _, en := range entries {
		e := Entry{Name: path.Base(en.Remote())}
		mt := en.ModTime(ctx)
		if !mt.IsZero() && mt.Unix() > 0 {
			e.MTime = &mt
		}
		switch x := en.(type) {
		case fs.Directory:
			e.Dir = true
		case fs.Object:
			e.Size = x.Size()
		}
		out = append(out, e)
	}
	return out, nil
}

// browseArchive lists one folder of an archive from its index (or, when
// the index is missing, by reading the archive).
func (e *Engine) browseArchive(ctx context.Context, t *target, archive, rel string) ([]Entry, error) {
	f, err := t.fsAt(ctx, "", fsOpts{})
	if err != nil {
		return nil, engineErr(err, "opening the destination")
	}
	idx, ok, err := readIndex(ctx, f, archive)
	if err != nil {
		return nil, err
	}
	if !ok {
		idx, err = scanArchiveIndex(ctx, f, archive)
		if err != nil {
			return nil, err
		}
	}
	return indexChildren(idx.entries, rel), nil
}

// indexChildren lists the direct children of dir in an archive index,
// including folders only implied by deeper paths.
func indexChildren(entries []indexEntry, dir string) []Entry {
	seen := map[string]int{}
	var out []Entry
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	for _, en := range entries {
		if !strings.HasPrefix(en.P, prefix) || en.P == dir {
			continue
		}
		rest := strings.TrimPrefix(en.P, prefix)
		name, deeper, isDeeper := strings.Cut(rest, "/")
		_ = deeper
		isDir := isDeeper || en.D
		if i, ok := seen[name]; ok {
			if isDir {
				out[i].Dir = true
				out[i].Size = 0
			}
			continue
		}
		e := Entry{Name: name, Dir: isDir}
		if !isDeeper {
			if !en.D {
				e.Size = en.S
			}
			if en.M != 0 {
				mt := time.Unix(en.M, 0).UTC()
				e.MTime = &mt
			}
		}
		seen[name] = len(out)
		out = append(out, e)
	}
	return out
}
