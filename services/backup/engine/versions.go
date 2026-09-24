package engine

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
)

// Version id prefixes (Version.ID).
const (
	versionCurrent       = "current"
	versionRecyclePrefix = "v_"
	versionArchivePrefix = "a_"
	// versionCountLimit bounds how many recycle folders get their files
	// counted in a listing; older ones are listed without counts.
	versionCountLimit = 50
)

// recycleFolder is one .nivaro-versions/<ts> folder.
type recycleFolder struct {
	name string
	t    time.Time
}

// listRecycle lists a mirror destination's recycle folders, newest first.
// Only names that parse as timestamps count; the time is the name, never
// the folder's modtime (spec §9).
func listRecycle(ctx context.Context, f fs.Fs) ([]recycleFolder, error) {
	entries, err := f.List(ctx, VersionsDir)
	if errors.Is(err, fs.ErrorDirNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []recycleFolder
	for _, en := range entries {
		d, ok := en.(fs.Directory)
		if !ok {
			continue
		}
		name := path.Base(d.Remote())
		t, err := time.Parse(VersionTimeLayout, name)
		if err != nil {
			continue
		}
		out = append(out, recycleFolder{name: name, t: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].t.After(out[j].t) })
	return out, nil
}

// ListVersions lists what can be restored from a job's destination.
func (e *Engine) ListVersions(ctx context.Context, req VersionsRequest) ([]Version, error) {
	t, err := e.resolve(ctx, req.Dest, req.SMBCreds)
	if err != nil {
		return nil, err
	}
	if !t.online {
		return nil, Errorf(CodeDestOffline, "%s is not available", t.display)
	}
	cur := Version{ID: versionCurrent, Kind: VersionCurrent, LabelKey: "backup.ver.current"}
	switch req.JobType {
	case jobTypeCopy:
		return []Version{cur}, nil
	case jobTypeMirror:
		f, err := t.fsAt(ctx, "", fsOpts{})
		if err != nil {
			return nil, engineErr(err, "opening the destination")
		}
		folders, err := listRecycle(ctx, f)
		if err != nil {
			return nil, engineErr(err, "listing the recycle folder")
		}
		out := []Version{cur}
		for i, rf := range folders {
			ts := rf.t
			v := Version{ID: versionRecyclePrefix + rf.name, Kind: VersionRecycle, Time: &ts, LabelKey: "backup.ver.recycle"}
			if i < versionCountLimit {
				if sub, err := t.fsAt(ctx, VersionsDir+"/"+rf.name, fsOpts{}); err == nil {
					if n, b, _, err := operations.Count(ctx, sub); err == nil {
						v.Files, v.Bytes = n, b
					}
				}
			}
			out = append(out, v)
		}
		return out, nil
	case jobTypeArchive:
		archives, err := e.listArchives(ctx, t, req.JobID)
		if err != nil {
			return nil, err
		}
		f, err := t.fsAt(ctx, "", fsOpts{})
		if err != nil {
			return nil, engineErr(err, "opening the destination")
		}
		out := []Version{}
		for _, a := range archives {
			ts := a.t
			v := Version{ID: versionArchivePrefix + a.name, Kind: VersionArchive, Time: &ts, LabelKey: "backup.ver.archive", Bytes: a.size}
			if idx, ok, err := readIndex(ctx, f, a.name); err == nil && ok {
				v.Files = idx.header.Files
			}
			out = append(out, v)
		}
		return out, nil
	}
	return nil, Errorf(CodeInternal, "unknown job type %q", req.JobType)
}

// versionRoot maps a version id to where its files are, relative to the
// destination: "" for current, the recycle folder, or (archive) the
// archive file name.
func versionRoot(id string) (dir string, archive string, err error) {
	switch {
	case id == "" || id == versionCurrent:
		return "", "", nil
	case strings.HasPrefix(id, versionRecyclePrefix):
		ts := strings.TrimPrefix(id, versionRecyclePrefix)
		if _, perr := time.Parse(VersionTimeLayout, ts); perr != nil {
			return "", "", Errorf(CodeNotFound, "unknown version %q", id)
		}
		return VersionsDir + "/" + ts, "", nil
	case strings.HasPrefix(id, versionArchivePrefix):
		name := strings.TrimPrefix(id, versionArchivePrefix)
		if name == "" || strings.ContainsAny(name, "/\x00") || !strings.HasSuffix(name, archiveExt) {
			return "", "", Errorf(CodeNotFound, "unknown version %q", id)
		}
		return "", name, nil
	}
	return "", "", Errorf(CodeNotFound, "unknown version %q", id)
}

// prepareDestOp resolves a job's destination for an op that only reads
// or prunes it, and checks its identity marker first.
func (e *Engine) prepareDestOp(ctx context.Context, j *job) (*target, error) {
	r := j.req
	j.phase(stepResolve, "backup.phase.precheck")
	t, err := e.resolve(ctx, r.Dest, credsFor(r.SMBCreds, r.Dest))
	if err != nil {
		return nil, err
	}
	if !t.online {
		return nil, Errorf(CodeDestOffline, "%s is not available", t.display)
	}
	if t.local {
		j.watch(t.mv.m.ID, "destination ("+t.mountPoint+")")
	}
	if _, err := e.checkMarker(ctx, t, r.DestFolderID, false); err != nil {
		j.logLine(LogError, "check", "backup.log.check", map[string]interface{}{"check_key": "backup.check." + CheckDestMarker, "status": string(CheckFail)})
		return nil, err
	}
	j.logLine(LogInfo, "check", "backup.log.check", map[string]interface{}{"check_key": "backup.check." + CheckDestMarker, "status": string(CheckPass)})
	return t, nil
}

// runPurgeVersions removes recycle folders older than VersionsDays
// (spec §9). The job side decides when pruning is suspended.
func (e *Engine) runPurgeVersions(ctx context.Context, j *job) (Result, error) {
	var res Result
	days := j.req.Retention.VersionsDays
	t, err := e.prepareDestOp(ctx, j)
	if err != nil {
		return res, err
	}
	if t.local {
		res.MountID = t.mv.m.ID
	}
	j.phase(stepPrune, "backup.phase.prune")
	if days <= 0 {
		return res, nil // keep forever
	}
	rctx, _ := rcloneContext(ctx)
	f, _, err := e.openDest(rctx, j, t, "", fsOpts{}, j.req.DestFolderID)
	if err != nil {
		return res, err
	}
	folders, err := listRecycle(rctx, f)
	if err != nil {
		return res, engineErr(err, "listing the recycle folder")
	}
	cutoff := e.now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	for _, rf := range folders {
		if !rf.t.Before(cutoff) {
			continue
		}
		if err := operations.Purge(rctx, f, VersionsDir+"/"+rf.name); err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
			return res, engineErr(err, "removing "+rf.name)
		}
		res.Counts.Deleted++
		j.logLine(LogInfo, "pruned", "backup.log.pruned", map[string]interface{}{"name": rf.name})
	}
	return res, nil
}

// runPurgeArchives keeps the newest KeepLast archives (spec §9). Names
// are parsed, never modtimes; partial archives never count.
func (e *Engine) runPurgeArchives(ctx context.Context, j *job) (Result, error) {
	var res Result
	keep := j.req.Retention.KeepLast
	t, err := e.prepareDestOp(ctx, j)
	if err != nil {
		return res, err
	}
	if t.local {
		res.MountID = t.mv.m.ID
	}
	j.phase(stepPrune, "backup.phase.prune")
	if keep <= 0 {
		return res, nil // keep all
	}
	rctx, _ := rcloneContext(ctx)
	f, _, err := e.openDest(rctx, j, t, "", fsOpts{}, j.req.DestFolderID)
	if err != nil {
		return res, err
	}
	archives, err := e.listArchives(rctx, t, j.req.JobID)
	if err != nil {
		return res, err
	}
	for i, a := range archives {
		if i < keep {
			continue
		}
		for _, name := range []string{a.name, a.name + archiveIndexSuffix} {
			o, err := f.NewObject(rctx, name)
			if errIsNotFound(err) {
				continue
			}
			if err != nil {
				return res, engineErr(err, "removing "+name)
			}
			if err := o.Remove(rctx); err != nil {
				return res, engineErr(err, "removing "+name)
			}
		}
		res.Counts.Deleted++
		j.logLine(LogInfo, "pruned", "backup.log.pruned", map[string]interface{}{"name": a.name})
	}
	res.Counts.DestFiles = int64(min(len(archives), keep))
	return res, nil
}

// runPurgeDest deletes a job's data at its destination (the job is being
// deleted with purge_data). The marker must name the job's folder id -
// prepareDestOp checks it, and the guard checks it again before the
// first delete - so a folder that belongs to someone else, or a drive
// that isn't the job's, is never touched. A destination folder is
// removed whole; at the root of a drive (no sub-path) only the engine's
// own files go, because everything else there may not be the job's.
func (e *Engine) runPurgeDest(ctx context.Context, j *job) (Result, error) {
	var res Result
	t, err := e.prepareDestOp(ctx, j)
	if err != nil {
		return res, err
	}
	if t.local {
		res.MountID = t.mv.m.ID
	}
	j.phase(stepPrune, "backup.phase.prune")
	rctx, _ := rcloneContext(ctx)
	f, _, err := e.openDest(rctx, j, t, "", fsOpts{}, j.req.DestFolderID)
	if err != nil {
		return res, err
	}
	if t.sub != "" {
		n, _, _, err := operations.Count(rctx, f)
		if err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
			return res, engineErr(err, "counting the backup")
		}
		if err := operations.Purge(rctx, f, ""); err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
			return res, engineErr(err, "removing the backup")
		}
		res.Counts.Deleted = n
		j.logLine(LogInfo, "pruned", "backup.log.pruned", map[string]interface{}{"name": t.display})
		return res, nil
	}
	// Drive root: the recycle folder, the job's archives and indexes
	// (finished or partial), and last the marker.
	switch _, err := f.List(rctx, VersionsDir); {
	case errors.Is(err, fs.ErrorDirNotFound):
		// a copy or archive job: no recycle folder
	case err != nil:
		return res, engineErr(err, "listing "+VersionsDir)
	default:
		var recycled int64
		if sub, err := t.fsAt(rctx, VersionsDir, fsOpts{}); err == nil {
			if n, _, _, err := operations.Count(rctx, sub); err == nil {
				recycled = n
			}
		}
		if err := operations.Purge(rctx, f, VersionsDir); err != nil {
			return res, engineErr(err, "removing "+VersionsDir)
		}
		res.Counts.Deleted += recycled
		j.logLine(LogInfo, "pruned", "backup.log.pruned", map[string]interface{}{"name": VersionsDir})
	}
	entries, err := f.List(rctx, "")
	if err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
		return res, engineErr(err, "listing the destination")
	}
	for _, en := range entries {
		o, ok := en.(fs.Object)
		if !ok {
			continue
		}
		name := path.Base(o.Remote())
		base := strings.TrimSuffix(strings.TrimSuffix(name, archiveIndexSuffix), archivePartial)
		if _, ok := parseArchiveName(j.req.JobID, base); !ok {
			continue
		}
		if err := o.Remove(rctx); err != nil {
			return res, engineErr(err, "removing "+name)
		}
		res.Counts.Deleted++
		j.logLine(LogInfo, "pruned", "backup.log.pruned", map[string]interface{}{"name": name})
	}
	if o, err := f.NewObject(rctx, MarkerFile); err == nil {
		if err := o.Remove(rctx); err != nil {
			return res, engineErr(err, "removing "+MarkerFile)
		}
	} else if !errIsNotFound(err) {
		return res, engineErr(err, "removing "+MarkerFile)
	}
	j.logRaw(LogInfo, "the backup is at the root of its drive: only its own files were removed, the rest was left in place")
	return res, nil
}
