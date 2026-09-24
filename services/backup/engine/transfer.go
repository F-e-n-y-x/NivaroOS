package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/cache"
	"github.com/rclone/rclone/fs/fserrors"
	"github.com/rclone/rclone/fs/operations"
	rsync "github.com/rclone/rclone/fs/sync"
)

// Guard names (GuardInfo.Guard, JobRequest.GuardOverride).
const (
	guardDelete      = "delete"
	guardChange      = "change"
	guardEmptySource = "empty_source"
)

// planTimeout bounds a real mirror run's planning pass (spec §6.4); past
// it the delete limit and the recycle folder (where rclone also moves
// every file it overwrites) protect the run. A copy job has neither, so
// its plan always runs to the end. A variable for tests.
var planTimeout = 10 * time.Minute

const (
	// guardSample is how many paths a tripped guard reports.
	guardSample = 500
	// maxFileErrors is how many failures a partial result lists.
	maxFileErrors = 100
)

// Engine steps (Stats.Step) and the run phases they belong to.
const (
	stepResolve  = "resolve"
	stepPlan     = "plan"
	stepTransfer = "transfer"
	stepVerify   = "verify"
	stepPrune    = "prune"
	stepRestore  = "restore"
)

func overridden(list []string, guard string) bool {
	for _, g := range list {
		if g == guard {
			return true
		}
	}
	return false
}

// normGuards fills unset guard percentages with the defaults (spec §4).
func normGuards(g Guards) Guards {
	if g.EmptySourcePct <= 0 {
		g.EmptySourcePct = 50
	}
	if g.DeletePct <= 0 {
		g.DeletePct = 10
	}
	if g.ChangePct <= 0 {
		g.ChangePct = 30
	}
	return g
}

// runOp runs one job's op.
func (e *Engine) runOp(ctx context.Context, j *job) (Result, error) {
	switch j.req.Op {
	case OpCopy, OpSync:
		return e.runTransfer(ctx, j, j.req.Op, false)
	case OpPlan:
		if j.req.PlanOp == OpArchive {
			return e.runArchive(ctx, j, true)
		}
		return e.runTransfer(ctx, j, j.req.PlanOp, true)
	case OpCheck:
		return e.runCheck(ctx, j)
	case OpArchive:
		return e.runArchive(ctx, j, false)
	case OpPurgeVersions:
		return e.runPurgeVersions(ctx, j)
	case OpPurgeArchives:
		return e.runPurgeArchives(ctx, j)
	case OpRestore:
		return e.runRestore(ctx, j)
	case OpPurgeDest:
		return e.runPurgeDest(ctx, j)
	}
	return Result{}, Errorf(CodeInternal, "unknown op %q", j.req.Op)
}

func (j *job) phase(step, phaseKey string) {
	j.setStep(step)
	j.logLine(LogInfo, "phase", "backup.log.phase", map[string]interface{}{"phase_key": phaseKey})
}

// prepareRun resolves and checks a run (spec §6.3) and logs each check.
// A failed check is returned as the error; the empty-source sentinel is
// a guard (waiting_user) unless the user already accepted it.
func (e *Engine) prepareRun(ctx context.Context, j *job, jobType string) (*prepared, Result, error) {
	j.phase(stepResolve, "backup.phase.precheck")
	r := j.req
	res := Result{}
	g := normGuards(r.Guards)
	p, err := e.prepare(ctx, precheckInput{
		jobType: jobType, sources: r.Sources, dest: r.Dest, creds: r.SMBCreds, filters: r.Filters,
		guards: g, destFolderID: r.DestFolderID, firstRun: r.FirstRun, baseline: r.Baseline,
		preserveMeta: r.Options.PreserveMeta, jobID: r.JobID,
	})
	if err != nil {
		return nil, res, err
	}
	// The sentinel is re-evaluated with the run's overrides.
	p.checks = replaceCheck(p.checks, sentinelCheck(p, r.GuardOverride))
	if jobType == jobTypeMirror && p.dest != nil && p.dest.online && !p.dest.local {
		if c := e.recycleCheck(ctx, p.dest); c != nil {
			p.checks = replaceCheck(p.checks, *c)
		}
	}
	res.Checks = p.checks
	for _, c := range p.checks {
		j.logLine(checkLevel(c.Status), "check", "backup.log.check", map[string]interface{}{"check_key": c.MsgKey, "status": string(c.Status)})
	}
	for i, t := range p.sources {
		if t != nil && t.local && t.online {
			j.watch(t.mv.m.ID, fmt.Sprintf("source %d (%s)", i+1, t.mountPoint))
		}
	}
	if p.dest != nil && p.dest.local && p.dest.online {
		j.watch(p.dest.mv.m.ID, "destination ("+p.dest.mountPoint+")")
		res.MountID = p.dest.mv.m.ID
	}
	if fail := p.failure(); fail != nil {
		if fail.Code == CodeEmptySource {
			gi := sentinelTrip(p.scan.Files, g, r.Baseline)
			res.Guard = gi
			if r.Op == OpPlan {
				// A preview shows what the run would do and never trips:
				// the guard is reported, the plan goes on.
				return p, res, nil
			}
			fail.Guard = gi
			fail.Detail = fmt.Sprintf("the source has %d files, %.1f%% fewer than the last successful run (limit %d%%)", p.scan.Files, gi.Pct, gi.Limit)
			j.logLine(LogWarn, "guard_tripped", "backup.log.guard_tripped", map[string]interface{}{
				"guard_key": "backup.guard." + gi.Guard, "pct": gi.Pct, "limit": gi.Limit})
		}
		res.Counts.SourceFiles = p.scan.Files
		return p, res, fail
	}
	return p, res, nil
}

func checkLevel(s CheckStatus) string {
	switch s {
	case CheckFail:
		return LogError
	case CheckWarn:
		return LogWarn
	}
	return LogInfo
}

// transferConfig applies the destination's quirks and the job's options
// to a context's rclone config.
func transferConfig(ctx context.Context, p *prepared, o Options) (context.Context, *fs.ConfigInfo) {
	ctx, ci := rcloneContext(ctx)
	if o.LowPriority {
		ci.Transfers = 2
	}
	q := p.dest.q
	if q.ModifyWindow > 0 {
		ci.ModifyWindow = fs.Duration(q.ModifyWindow)
	}
	if q.SizeOnly {
		ci.SizeOnly = true
	}
	ci.Metadata = p.meta
	return ctx, ci
}

// openPair opens the source and destination filesystems of a one-source
// run, the destination behind a mount guard when it is local.
func (e *Engine) openPair(ctx context.Context, j *job, p *prepared) (src, dst fs.Fs, g *mountGuard, err error) {
	s := p.sources[0]
	src, err = s.fsAt(ctx, "", fsOpts{links: p.meta, oneFileSystem: true})
	if err != nil {
		return nil, nil, nil, engineErr(err, "opening the source")
	}
	// The destination doesn't cross into other mounts either: a drive
	// mounted inside a mirror's destination is not the mirror's to
	// delete from (or to move into its recycle folder); the mount guard
	// only watches the destination's own mount.
	dst, g, err = e.openDest(ctx, j, p.dest, "", fsOpts{links: p.meta, oneFileSystem: true}, p.in.destFolderID)
	return src, dst, g, err
}

// openDest opens a destination (or rel below it) behind a guard: a
// local one checks its mount before every write, and every guard
// re-reads the marker before the first delete.
func (e *Engine) openDest(ctx context.Context, j *job, t *target, rel string, o fsOpts, destFolderID string) (fs.Fs, *mountGuard, error) {
	f, err := t.fsAt(ctx, rel, o)
	if err != nil {
		return nil, nil, engineErr(err, "opening the destination")
	}
	g := e.newGuard(j, t)
	if destFolderID != "" {
		g.beforeDelete = func(ctx context.Context) error {
			_, err := e.checkMarker(ctx, t, destFolderID, false)
			return err
		}
	}
	return newGuardFs(ctx, f, g), g, nil
}

// newGuard builds the write guard of a destination. A network
// destination has no mount to watch; its guard only re-checks the
// identity marker before the first delete.
func (e *Engine) newGuard(j *job, t *target) *mountGuard {
	if !t.local {
		return &mountGuard{e: e, j: j}
	}
	return &mountGuard{e: e, j: j, local: true, mountID: t.mv.m.ID, mountPoint: t.mountPoint, dev: t.mountDev}
}

// planResult is what a dry run found.
type planResult struct {
	add, update, del, match int64
	bytesAdd                int64
	srcFiles, dstFiles      int64
	delSample, chgSample    []string
	complete                bool
}

// planListAdds caps the "add" items a real run's plan list holds (a
// first run lists every file, on the system disk). Updates and
// deletions - what the guards stop and the user reviews - are always
// listed; the adds left out are counted in one omitted_add line. A
// variable for tests.
var planListAdds int64 = 20000

// plan runs sync (or copy) with DryRun into a counting logger (spec
// §6.4), writing every change to preview when it is set, at most
// maxAdds "add" items of them (-1: all).
func (e *Engine) plan(ctx context.Context, src, dst fs.Fs, op Op, preview *bufio.Writer, maxAdds int64) (*planResult, error) {
	ctx, ci := fs.AddConfig(ctx)
	ci.DryRun = true
	pr := &planResult{complete: true}
	var mu sync.Mutex
	var werr error
	var addsListed, addsOmitted, bytesOmitted int64
	emit := func(opName, p string, size int64) {
		if preview == nil || werr != nil {
			return
		}
		if opName == "add" && maxAdds >= 0 {
			if addsListed >= maxAdds {
				addsOmitted++
				bytesOmitted += max(size, 0)
				return
			}
			addsListed++
		}
		raw, err := json.Marshal(PreviewItem{Op: opName, Path: p, Size: size})
		if err == nil {
			raw = append(raw, '\n')
			_, err = preview.Write(raw)
		}
		if err != nil {
			werr = err
		}
	}
	ctx = operations.WithLogger(ctx, func(ctx context.Context, sigil operations.Sigil, s, d fs.DirEntry, err error) {
		if err == fs.ErrorIsDir {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch sigil {
		case operations.MissingOnDst:
			if o, ok := s.(fs.Object); ok {
				pr.add++
				pr.srcFiles++
				pr.bytesAdd += max(o.Size(), 0)
				emit("add", o.Remote(), o.Size())
			}
		case operations.Differ:
			if o, ok := s.(fs.Object); ok {
				pr.update++
				pr.srcFiles++
				pr.dstFiles++
				pr.bytesAdd += max(o.Size(), 0)
				if len(pr.chgSample) < guardSample {
					pr.chgSample = append(pr.chgSample, o.Remote())
				}
				emit("update", o.Remote(), o.Size())
			}
		case operations.MissingOnSrc:
			if o, ok := d.(fs.Object); ok {
				pr.dstFiles++
				if op == OpSync {
					pr.del++
					if len(pr.delSample) < guardSample {
						pr.delSample = append(pr.delSample, o.Remote())
					}
					emit("delete", o.Remote(), o.Size())
				}
			}
		case operations.Match:
			if _, ok := s.(fs.Object); ok {
				pr.match++
				pr.srcFiles++
				pr.dstFiles++
			}
		}
	})
	var err error
	if op == OpSync {
		ctx, ci2 := fs.AddConfig(ctx)
		ci2.DeleteMode = fs.DeleteModeAfter
		err = rsync.Sync(ctx, dst, src, false)
	} else {
		err = rsync.CopyDir(ctx, dst, src, false)
	}
	if addsOmitted > 0 && preview != nil && werr == nil {
		raw, merr := json.Marshal(PreviewItem{Op: PreviewOmittedAdd, Size: bytesOmitted, Count: addsOmitted})
		if merr == nil {
			raw = append(raw, '\n')
			_, werr = preview.Write(raw)
		}
	}
	if werr != nil {
		return pr, Errorf(CodeIOError, "writing the preview: %w", werr)
	}
	if err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
		return pr, err
	}
	return pr, nil
}

// guardTrip evaluates the delete and change guards on a plan (spec §6.4).
// A guard the user accepted only covers what they reviewed (rev): a plan
// that deletes or changes more than that trips it again.
func guardTrip(op Op, pr *planResult, g Guards, override []string, rev *Reviewed) *GuardInfo {
	total := pr.dstFiles
	if total <= 0 {
		return nil
	}
	if op == OpSync && (!overridden(override, guardDelete) || rev.exceeded(pr.del, rev.deleted())) {
		if pct := percent(pr.del, total); pct > float64(g.DeletePct) {
			return &GuardInfo{Guard: guardDelete, Pct: pct, Limit: g.DeletePct, Count: pr.del, Total: total, Sample: pr.delSample}
		}
	}
	changed := pr.update
	if op == OpSync {
		changed += pr.del
	}
	if !overridden(override, guardChange) || rev.exceeded(changed, rev.changed(op)) {
		if pct := percent(changed, total); pct > float64(g.ChangePct) {
			sample := append([]string(nil), pr.chgSample...)
			for _, p := range pr.delSample {
				if len(sample) >= guardSample {
					break
				}
				sample = append(sample, p)
			}
			return &GuardInfo{Guard: guardChange, Pct: pct, Limit: g.ChangePct, Count: changed, Total: total, Sample: sample}
		}
	}
	return nil
}

// exceeded reports whether n is more than the reviewed count; always
// false without a review (an accepted guard from an older version covers
// whatever the new plan shows, as it did then).
func (r *Reviewed) exceeded(n, reviewed int64) bool {
	return r != nil && n > reviewed
}

func (r *Reviewed) deleted() int64 {
	if r == nil {
		return 0
	}
	return r.Deleted
}

// changed is the reviewed count in the change guard's terms: updates,
// and for a mirror its deletions too.
func (r *Reviewed) changed(op Op) int64 {
	if r == nil {
		return 0
	}
	if op == OpSync {
		return r.Updated + r.Deleted
	}
	return r.Updated
}

func guardError(gi *GuardInfo) *Error {
	code := CodeDeleteGuard
	what := "delete"
	if gi.Guard == guardChange {
		code, what = CodeChangeGuard, "change"
	}
	return &Error{Code: code, Guard: gi,
		Detail: fmt.Sprintf("plan would %s %d of %d files (%.1f%%), limit %d%%", what, gi.Count, gi.Total, gi.Pct, gi.Limit)}
}

// fileLogger counts a real transfer's outcomes and logs every file.
type fileLogger struct {
	mu                     sync.Mutex
	j                      *job
	op                     Op
	added, changed, delete int64
	skipped, errored       int64
	fileErrors             []FileError
	maxDeleteHit           bool // rclone stopped at MaxDelete
}

// isMaxDelete reports rclone's --max-delete stop, which it wraps into a
// generic "failed to delete N files" for the run as a whole.
func isMaxDelete(err error) bool {
	return err != nil && strings.Contains(err.Error(), "max-delete threshold reached")
}

func (l *fileLogger) fn(ctx context.Context, sigil operations.Sigil, s, d fs.DirEntry, err error) {
	if err == fs.ErrorIsDir {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	switch sigil {
	case operations.MissingOnDst:
		if o, ok := s.(fs.Object); ok {
			l.added++
			l.j.logLine(LogInfo, "copied", "backup.log.copied", map[string]interface{}{"path": o.Remote()})
		}
	case operations.Differ:
		if o, ok := s.(fs.Object); ok {
			l.changed++
			l.j.logLine(LogInfo, "updated", "backup.log.updated", map[string]interface{}{"path": o.Remote()})
		}
	case operations.MissingOnSrc:
		if o, ok := d.(fs.Object); ok && l.op == OpSync {
			l.delete++
			l.j.logLine(LogInfo, "recycled", "backup.log.recycled", map[string]interface{}{"path": o.Remote()})
		}
	case operations.Match:
		if _, ok := s.(fs.Object); ok {
			l.skipped++
		}
	case operations.TransferError:
		if isMaxDelete(err) {
			// The delete limit, not a broken file: the run reports the
			// delete guard (spec §6.4 second barrier).
			l.maxDeleteHit = true
			if d != nil && s == nil {
				l.delete--
			}
			return
		}
		var remote string
		switch {
		case s != nil:
			remote = s.Remote()
			if d != nil {
				l.changed--
			} else {
				l.added--
			}
		case d != nil:
			remote = d.Remote()
			l.delete--
		}
		if remote == "" {
			return
		}
		code := classifyError(err)
		if code == CodeCancelledByUser || code == CodeCancelledUnmounted {
			return // the run's cancel, not this file's fault
		}
		l.errored++
		detail := ""
		if err != nil {
			detail = err.Error()
		}
		if len(l.fileErrors) < maxFileErrors {
			l.fileErrors = append(l.fileErrors, FileError{Path: remote, Code: code, Detail: detail})
		}
		l.j.appendLine(LogLine{T: time.Now(), Lvl: LogError, Code: "file_error", MsgKey: "backup.log.file_error",
			Args: map[string]interface{}{"path": remote, "reason_key": "backup.err." + string(code) + ".title"}, Raw: detail})
	}
}

// runTransfer is copy and mirror (op sync), and their preview (dry).
func (e *Engine) runTransfer(ctx context.Context, j *job, op Op, dry bool) (Result, error) {
	r := j.req
	jobType := jobTypeCopy
	if op == OpSync {
		jobType = jobTypeMirror
	}
	p, res, err := e.prepareRun(ctx, j, jobType)
	if err != nil {
		return res, err
	}
	g := normGuards(r.Guards)
	dest := p.dest
	fctx, _, err := withFilter(ctx, p.sourceSpec(0, true))
	if err != nil {
		return res, err
	}
	tctx, _ := transferConfig(fctx, p, r.Options)
	src, dst, guard, err := e.openPair(tctx, j, p)
	if err != nil {
		return res, err
	}
	j.setTotals(p.scan.Files, p.scan.Bytes)
	res.Counts.SourceFiles = p.scan.Files
	res.Counts.BytesTotal = p.scan.Bytes

	// Planning pass. Every real run plans, small ones too: spec §6.4
	// skipped it below 1000 files and left MaxDelete to stop a mass
	// delete, but rclone's MaxDelete only stops once the limit is spent -
	// new files are already copied and the first deletions already made
	// (into the recycle folder) when the guard reports. A dry run over a
	// small tree costs next to nothing and keeps "before any write" true.
	var pr *planResult
	{
		j.setStep(stepPlan)
		var preview *bufio.Writer
		var pf *os.File
		// A real run writes the plan too when asked: a tripped guard
		// is then reviewed from the same list a preview shows.
		if r.PreviewFile != "" {
			pf, err = createPreviewFile(r.PreviewFile)
			if err != nil {
				return res, err
			}
			preview = bufio.NewWriterSize(pf, 256<<10)
		}
		pctx := tctx
		var cancel context.CancelFunc = func() {}
		if !dry && op == OpSync {
			// Only a mirror may go on without a complete plan: the
			// change guard is a copy job's only protection against a
			// source that was encrypted or overwritten, and it needs the
			// whole plan.
			pctx, cancel = context.WithTimeout(tctx, planTimeout)
		}
		// The dry run counts its would-be deletions in rclone's
		// accounting; a group of its own keeps them from eating the real
		// pass's MaxDelete and from showing up in the run's counts.
		planGroup := j.group + "-plan"
		// A preview lists everything the user asked to see; a real run's
		// list is only for reviewing a guard it may trip.
		maxAdds := int64(-1)
		if !dry {
			maxAdds = planListAdds
		}
		pr, err = e.plan(accounting.WithStatsGroup(pctx, planGroup), src, dst, op, preview, maxAdds)
		timedOut := !dry && errors.Is(pctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
		cancel()
		deleteStatsGroup(planGroup)
		if pf != nil {
			if ferr := preview.Flush(); ferr != nil && err == nil {
				err = Errorf(CodeIOError, "writing the preview: %w", ferr)
			}
			if cerr := pf.Close(); cerr != nil && err == nil {
				err = Errorf(CodeIOError, "writing the preview: %w", cerr)
			}
		}
		switch {
		case ctx.Err() != nil:
			return res, context.Cause(ctx)
		case timedOut:
			j.logRaw(LogWarn, "the planning pass took longer than 10 minutes; the delete limit and the recycle folder protect this run")
			pr = nil
		case err != nil:
			return res, engineErr(err, "planning")
		}
	}
	if dry {
		res.Counts = Counts{Added: pr.add, Changed: pr.update, Deleted: pr.del, Skipped: pr.match,
			SourceFiles: pr.srcFiles, DestFiles: pr.dstFiles, BytesAdd: pr.bytesAdd, BytesTotal: p.scan.Bytes}
		// Informative: a preview never trips.
		if gi := guardTrip(op, pr, g, r.GuardOverride, r.GuardReviewed); gi != nil {
			res.Guard = gi
		}
		return res, nil
	}
	destCount := int64(0)
	switch {
	case pr != nil:
		destCount = pr.dstFiles
		if gi := guardTrip(op, pr, g, r.GuardOverride, r.GuardReviewed); gi != nil {
			res.Guard = gi
			j.logLine(LogWarn, "guard_tripped", "backup.log.guard_tripped", map[string]interface{}{
				"guard_key": "backup.guard." + gi.Guard, "pct": gi.Pct, "limit": gi.Limit})
			res.Counts.DestFiles = destCount
			return res, guardError(gi)
		}
		j.setTotals(p.scan.Files, pr.bytesAdd)
		if c := e.checkFreeSpace(ctx, p, pr.bytesAdd); c.Status == CheckFail {
			res.Checks = replaceCheck(res.Checks, c)
			return res, &Error{Code: CodeNoSpace, Detail: checkDetail(c)}
		}
	case r.Baseline != nil:
		destCount = r.Baseline.DestFiles
	}

	// Identity: write the marker before the first byte of a first run.
	if p.marker == nil {
		if guard != nil {
			if err := guard.check(); err != nil {
				return res, err
			}
		}
		if dest.local {
			if err := mkdirPrivate(dest.path); err != nil {
				return res, err
			}
		}
		if err := e.writeMarker(ctx, dest, r.JobID, r.DestFolderID); err != nil {
			return res, err
		}
		res.MarkerWritten = true
		j.logLine(LogInfo, "marker_written", "backup.log.marker_written", nil)
	}

	if dest.local {
		// The destination is opened with one_file_system, and rclone's
		// local backend then only lists folders on the device its root
		// was on when it was opened - unknown when the root didn't exist
		// yet (a first run). Mkdir on the root (it exists by now: the
		// marker is in it) records that device.
		if err := dst.Mkdir(tctx, ""); err != nil {
			return res, engineErr(err, "opening the destination")
		}
	}

	// Real pass.
	j.phase(stepTransfer, "backup.phase.transfer")
	fl := &fileLogger{j: j, op: op}
	rctx := operations.WithLogger(tctx, fl.fn)
	limit := int64(-1)
	if op == OpSync {
		var ci2 *fs.ConfigInfo
		rctx, ci2 = fs.AddConfig(rctx)
		ci2.DeleteMode = fs.DeleteModeAfter
		limit = maxDelete(destCount, g.DeletePct, pr, r.GuardOverride, r.GuardReviewed)
		ci2.MaxDelete = limit
		key, cleanup, err := e.recycleDir(rctx, j, p, dest)
		if err != nil {
			return res, err
		}
		defer cleanup()
		ci2.BackupDir = key
	}
	if op == OpSync {
		err = rsync.Sync(rctx, dst, src, r.Options.CopyEmptyDirs)
	} else {
		err = rsync.CopyDir(rctx, dst, src, r.Options.CopyEmptyDirs)
	}
	if cerr := ctx.Err(); cerr != nil {
		// rclone may end a cancelled sync without an error of its own.
		err = context.Cause(ctx)
	}
	stats := accounting.Stats(rctx)
	fl.mu.Lock()
	res.Counts.Added, res.Counts.Changed, res.Counts.Skipped, res.Counts.Errored = max(fl.added, 0), max(fl.changed, 0), fl.skipped, fl.errored
	res.FileErrors = append([]FileError(nil), fl.fileErrors...)
	hitLimit := fl.maxDeleteHit
	fl.mu.Unlock()
	res.Counts.Deleted = stats.GetDeletes()
	res.Counts.BytesTransferred = stats.GetBytes()
	res.Counts.DestFiles = max(destCount+res.Counts.Added-res.Counts.Deleted, 0)
	if err != nil {
		if code := classifyError(err); code == CodeDeleteGuard || hitLimit {
			// MaxDelete, the second barrier: more deletions than the limit.
			gi := &GuardInfo{Guard: guardDelete, Limit: g.DeletePct, Count: limit + 1, Total: destCount, Sample: []string{}}
			if destCount > 0 {
				gi.Pct = percent(gi.Count, destCount)
			}
			res.Guard = gi
			return res, &Error{Code: CodeDeleteGuard, Guard: gi, Detail: "stopped at the delete limit: " + err.Error(), Err: err}
		}
		if partialOK(err, res.Counts.Errored) {
			// Finished with file errors: a partial success (spec §5).
			return e.verifyAfter(ctx, j, p, src, dst, res)
		}
		return res, engineErr(err, "")
	}
	return e.verifyAfter(ctx, j, p, src, dst, res)
}

// partialOK reports whether a transfer error only reflects individual
// files that failed while the rest completed.
func partialOK(err error, errored int64) bool {
	if errored == 0 || fserrors.IsFatalError(err) {
		return false
	}
	code := classifyError(err)
	return code == CodeIOError || code == CodeFat32FileTooLarge
}

// maxDelete is the real pass's delete limit (spec §6.4): the configured
// share of the destination, or - when the user accepted the delete guard
// for this run - what they reviewed (else, from an older version's
// decision, what the plan showed).
func maxDelete(destCount int64, pct int, pr *planResult, override []string, rev *Reviewed) int64 {
	limit := int64(math.Ceil(float64(destCount) * float64(pct) / 100))
	if !overridden(override, guardDelete) {
		return limit
	}
	switch {
	case rev != nil:
		return max(rev.Deleted, limit)
	case pr == nil:
		return -1
	}
	return max(pr.del, limit)
}

// recycleDir prepares the mirror's recycle folder
// <dest>/.nivaro-versions/<run start> and returns the key rclone's
// --backup-dir reads it by. The folder is opened here (guarded like the
// destination) and put in rclone's filesystem cache under a private key,
// so no path or credential goes through rclone's string parsing.
func (e *Engine) recycleDir(ctx context.Context, j *job, p *prepared, dest *target) (string, func(), error) {
	ts := j.startedAt().UTC().Format(VersionTimeLayout)
	rel := VersionsDir + "/" + ts
	f, _, err := e.openDest(ctx, j, dest, rel, fsOpts{links: p.meta, oneFileSystem: true}, p.in.destFolderID)
	if err != nil {
		return "", nil, err
	}
	key := "nivaroos-recycle-" + j.group
	cache.Put(key, f)
	// Put maps key to the folder's canonical name; dropping that mapping
	// makes the guarded filesystem unreachable once the run is over (the
	// cache entry itself expires on its own).
	canonical := fs.ConfigString(f)
	cleanup := func() {
		cache.ClearMappingsPrefix(canonical)
	}
	return key, cleanup, nil
}

func (j *job) startedAt() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.started != nil {
		return *j.started
	}
	return time.Now()
}

// verifyAfter runs the optional verify step (spec §4 Options.Verify).
func (e *Engine) verifyAfter(ctx context.Context, j *job, p *prepared, src, dst fs.Fs, res Result) (Result, error) {
	if !j.req.Options.Verify {
		return res, nil
	}
	if p.dest.q.NoHash {
		j.logRaw(LogWarn, "verify skipped: the destination has no checksums to compare")
		return res, nil
	}
	j.phase(stepVerify, "backup.phase.verify")
	fctx, _, err := withFilter(ctx, p.sourceSpec(0, true))
	if err != nil {
		return res, err
	}
	vctx, _ := transferConfig(fctx, p, j.req.Options)
	diffs, _, err := verifyTrees(vctx, src, dst)
	if err != nil {
		return res, engineErr(err, "verifying")
	}
	for _, d := range diffs {
		res.Counts.Errored++
		if len(res.FileErrors) < maxFileErrors {
			res.FileErrors = append(res.FileErrors, d)
		}
		j.appendLine(LogLine{T: time.Now(), Lvl: LogError, Code: "file_error", MsgKey: "backup.log.file_error",
			Args: map[string]interface{}{"path": d.Path, "reason_key": "backup.err.io_error.title"}, Raw: d.Detail})
	}
	return res, nil
}

// verifyTrees checks every source file against the destination, one way
// (extra destination files are fine). It returns the differences.
func verifyTrees(ctx context.Context, src, dst fs.Fs) ([]FileError, int64, error) {
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
			diffs = append(diffs, FileError{Path: p, Code: CodeIOError, Detail: "differs from the source after the copy"})
		case '+':
			diffs = append(diffs, FileError{Path: p, Code: CodeIOError, Detail: "missing on the destination"})
		case '!':
			diffs = append(diffs, FileError{Path: p, Code: CodeIOError, Detail: "could not be checked"})
		}
	}}
	err := operations.Check(ctx, &operations.CheckOpt{Fdst: dst, Fsrc: src, OneWay: true, Combined: w})
	w.flush()
	if err != nil && len(diffs) > 0 && !fserrors.IsFatalError(err) {
		err = nil // the differences are the result
	}
	return diffs, checked, err
}

// sigilWriter splits rclone's "combined" report ("= path\n") into calls.
type sigilWriter struct {
	mu  sync.Mutex
	buf []byte
	fn  func(sigil byte, path string)
}

func (w *sigilWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if len(line) >= 2 {
			w.fn(line[0], line[2:])
		}
	}
	return len(p), nil
}

func (w *sigilWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if line := string(w.buf); len(line) >= 2 {
		w.fn(line[0], line[2:])
	}
	w.buf = nil
}

// runCheck is a standalone one-way verify (run kind "verify").
func (e *Engine) runCheck(ctx context.Context, j *job) (Result, error) {
	p, res, err := e.prepareRun(ctx, j, jobTypeCopy)
	if err != nil {
		return res, err
	}
	if p.dest.q.NoHash {
		j.logRaw(LogWarn, "verify skipped: the destination has no checksums to compare")
		res.Counts.SourceFiles = p.scan.Files
		return res, nil
	}
	fctx, _, err := withFilter(ctx, p.sourceSpec(0, true))
	if err != nil {
		return res, err
	}
	vctx, _ := transferConfig(fctx, p, j.req.Options)
	src, err := p.sources[0].fsAt(vctx, "", fsOpts{links: p.meta, oneFileSystem: true})
	if err != nil {
		return res, engineErr(err, "opening the source")
	}
	dst, err := p.dest.fsAt(vctx, "", fsOpts{links: p.meta})
	if err != nil {
		return res, engineErr(err, "opening the destination")
	}
	j.phase(stepVerify, "backup.phase.verify")
	j.setTotals(p.scan.Files, p.scan.Bytes)
	diffs, checked, err := verifyTrees(vctx, src, dst)
	if err != nil {
		return res, engineErr(err, "verifying")
	}
	res.Counts.SourceFiles = p.scan.Files
	res.Counts.Skipped = checked
	for _, d := range diffs {
		res.Counts.Errored++
		if len(res.FileErrors) < maxFileErrors {
			res.FileErrors = append(res.FileErrors, d)
		}
		j.appendLine(LogLine{T: time.Now(), Lvl: LogError, Code: "file_error", MsgKey: "backup.log.file_error",
			Args: map[string]interface{}{"path": d.Path, "reason_key": "backup.err.io_error.title"}, Raw: d.Detail})
	}
	return res, nil
}

// createPreviewFile opens the plan's preview output. It belongs to the
// job side (its data folder), so it is checked for shape, not against
// the backup roots.
func createPreviewFile(p string) (*os.File, error) {
	if !filepath.IsAbs(p) || strings.ContainsRune(p, 0) {
		return nil, Errorf(CodeInternal, "preview_file must be an absolute path, got %q", p)
	}
	f, err := os.OpenFile(filepath.Clean(p), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, Errorf(CodeIOError, "creating the preview: %w", err)
	}
	return f, nil
}

// mkdirPrivate creates a destination folder 0700 where the filesystem
// allows it (spec §14); FAT and exFAT ignore modes.
func mkdirPrivate(p string) error {
	if err := os.MkdirAll(p, 0o700); err != nil {
		return Errorf(classifyError(err), "creating %s: %w", p, err)
	}
	return nil
}
