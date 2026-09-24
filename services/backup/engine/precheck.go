package engine

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
)

// Job types as the job side names them (jobs.JobType values).
const (
	jobTypeCopy    = "copy"
	jobTypeMirror  = "mirror"
	jobTypeArchive = "archive"
)

const (
	gib = int64(1) << 30
	// defaultSizeBudget bounds Precheck's source walk (PrecheckRequest.SizeTimeoutSec).
	defaultSizeBudget = 20 * time.Second
	// maxArchiveSources is the most sources one archive takes.
	maxArchiveSources = 16
)

// precheckInput is a would-be run, from Precheck or a job.
type precheckInput struct {
	jobType      string
	sources      []Endpoint
	dest         Endpoint
	creds        map[string]SMBCreds
	filters      Filters
	guards       Guards
	destFolderID string
	firstRun     bool
	baseline     *Baseline
	preserveMeta *bool
	budget       time.Duration // 0: walk everything (a real run)
	jobID        string
}

// prepared is a resolved, checked run.
type prepared struct {
	in      precheckInput
	sources []*target
	dest    *target
	// excludes are folders left out of each source (by index): a
	// destination inside it.
	excludes [][]string
	scan     scanResult
	scanned  bool
	checks   []Check
	estimate Estimate
	marker   *Marker
	// meta: owners, modes and symlinks are kept (local to local).
	meta bool
}

// failure returns the first failed check as an error, or nil.
func (p *prepared) failure() *Error {
	for _, c := range p.checks {
		if c.Status == CheckFail {
			return &Error{Code: c.Code, Detail: checkDetail(c)}
		}
	}
	return nil
}

func checkDetail(c Check) string {
	var parts []string
	for k, v := range c.Args {
		parts = append(parts, k+"="+toString(v))
	}
	d := "check " + c.ID + " failed"
	if len(parts) > 0 {
		d += " (" + strings.Join(parts, ", ") + ")"
	}
	return d
}

func newCheck(id string, st CheckStatus, code ErrorCode, args map[string]interface{}) Check {
	return Check{ID: id, Status: st, Code: code, MsgKey: "backup.check." + id, Args: args}
}

func credsFor(m map[string]SMBCreds, ep Endpoint) *SMBCreds {
	if ep.Kind != EPSMB {
		return nil
	}
	if c, ok := m[ep.RefID]; ok {
		return &c
	}
	return nil
}

// resolveCheck resolves one endpoint for a check: the target when online,
// else the failing check.
func (e *Engine) resolveCheck(ctx context.Context, id string, ep Endpoint, creds *SMBCreds, offline ErrorCode) (*target, Check) {
	t, err := e.resolve(ctx, ep, creds)
	if err != nil {
		return nil, newCheck(id, CheckFail, CodeOf(err), map[string]interface{}{"detail": err.Error()})
	}
	if !t.online {
		return t, newCheck(id, CheckFail, offline, nil)
	}
	return t, newCheck(id, CheckPass, "", nil)
}

// prepare runs every §6.3 check that needs no write, in order. It fails
// only for requests that can't be checked at all (bad filters, a wrong
// number of sources); failed checks are in the result.
func (e *Engine) prepare(ctx context.Context, in precheckInput) (*prepared, error) {
	switch {
	case len(in.sources) == 0:
		return nil, Errorf(CodeInternal, "no source given")
	case in.jobType != jobTypeArchive && len(in.sources) != 1:
		return nil, Errorf(CodeInternal, "a %s job takes exactly one source, got %d", in.jobType, len(in.sources))
	case len(in.sources) > maxArchiveSources:
		return nil, Errorf(CodeInternal, "an archive takes at most %d sources, got %d", maxArchiveSources, len(in.sources))
	}
	if _, err := newFilter(filterSpec{f: in.filters}); err != nil {
		return nil, err
	}
	p := &prepared{in: in, excludes: make([][]string, len(in.sources))}

	// 1. Resolution.
	srcCheck := newCheck(CheckSourceResolves, CheckPass, "", nil)
	allOnline := true
	for i, ep := range in.sources {
		t, c := e.resolveCheck(ctx, CheckSourceResolves, ep, credsFor(in.creds, ep), CodeSourceOffline)
		if c.Status != CheckPass {
			if c.Args == nil {
				c.Args = map[string]interface{}{}
			}
			c.Args["index"] = i
			if srcCheck.Status == CheckPass {
				srcCheck = c
			}
			allOnline = false
		}
		p.sources = append(p.sources, t)
	}
	dest, destCheck := e.resolveCheck(ctx, CheckDestResolves, in.dest, credsFor(in.creds, in.dest), CodeDestOffline)
	p.dest = dest
	if destCheck.Status != CheckPass {
		allOnline = false
	}
	p.checks = append(p.checks, srcCheck, destCheck)

	// 2. Allowed roots (resolution enforces them; report it as its own check).
	switch {
	case srcCheck.Code == CodePathNotAllowed || destCheck.Code == CodePathNotAllowed:
		p.checks = append(p.checks, newCheck(CheckAllowedRoots, CheckFail, CodePathNotAllowed, nil))
	case allOnline:
		p.checks = append(p.checks, newCheck(CheckAllowedRoots, CheckPass, "", nil))
	default:
		p.checks = append(p.checks, newCheck(CheckAllowedRoots, CheckSkip, "", nil))
	}

	// 3. Neither inside the other.
	if !allOnline {
		p.checks = append(p.checks, newCheck(CheckNotInside, CheckSkip, "", nil))
	} else {
		p.checks = append(p.checks, e.checkNotInside(p))
	}

	// 4. Walk the sources: size, count, what the destination can't store.
	if allOnline && p.failure() == nil {
		if err := e.scanAll(ctx, p); err != nil {
			return nil, err
		}
	}
	p.checks = append(p.checks, e.checkQuirks(p))
	p.meta = allOnline && dest.local && in.jobType != jobTypeArchive && allLocal(p.sources) && (in.preserveMeta == nil || *in.preserveMeta)
	switch {
	case !allOnline:
		p.checks = append(p.checks, newCheck(CheckMetadata, CheckSkip, "", nil))
	case p.meta:
		p.checks = append(p.checks, newCheck(CheckMetadata, CheckPass, "", nil))
	default:
		p.checks = append(p.checks, newCheck(CheckMetadata, CheckSkip, "", nil))
	}

	// 5. Free space.
	p.checks = append(p.checks, e.checkFreeSpace(ctx, p, 0))

	// 6. Source sentinel.
	p.checks = append(p.checks, sentinelCheck(p, nil))

	// 7. Destination identity.
	if destCheck.Status == CheckPass {
		p.checks = append(p.checks, e.markerCheck(ctx, p))
	} else {
		p.checks = append(p.checks, newCheck(CheckDestMarker, CheckSkip, "", nil))
	}
	p.estimate.SourceBytes = p.scan.Bytes
	p.estimate.SourceFiles = p.scan.Files
	p.estimate.Partial = p.scan.Partial || !p.scanned
	return p, nil
}

func allLocal(ts []*target) bool {
	for _, t := range ts {
		if t == nil || !t.local {
			return false
		}
	}
	return true
}

// sameSpace reports whether two targets address the same namespace, so
// their paths can be compared: both local, or the same remote.
func sameSpace(a, b *target) bool {
	if a.local && b.local {
		return true
	}
	return !a.local && !b.local && a.fsName == b.fsName
}

func spacePath(t *target) string {
	if t.local {
		return t.path
	}
	return "/" + strings.Trim(t.fsRoot, "/")
}

// checkNotInside implements §6.3 check 2: a destination inside a source
// is excluded from that source automatically; a source inside the
// destination (or the same folder) fails.
func (e *Engine) checkNotInside(p *prepared) Check {
	var excluded []string
	for i, s := range p.sources {
		if !sameSpace(s, p.dest) {
			continue
		}
		sp, dp := spacePath(s), spacePath(p.dest)
		if p.dest.q.CaseInsensitive || s.q.CaseInsensitive {
			sp, dp = strings.ToLower(sp), strings.ToLower(dp)
		}
		switch {
		case sp == dp || pathWithin(sp, dp):
			return newCheck(CheckNotInside, CheckFail, CodeDestInsideSource, map[string]interface{}{"index": i})
		case pathWithin(dp, sp):
			rel, err := filepath.Rel(spacePath(s), spacePath(p.dest))
			if err != nil {
				return newCheck(CheckNotInside, CheckFail, CodeDestInsideSource, map[string]interface{}{"index": i})
			}
			rel = filepath.ToSlash(rel)
			p.excludes[i] = append(p.excludes[i], rel)
			excluded = append(excluded, rel)
		}
	}
	if len(excluded) > 0 {
		return newCheck(CheckNotInside, CheckPass, "", map[string]interface{}{"excluded": strings.Join(excluded, ", ")})
	}
	return newCheck(CheckNotInside, CheckPass, "", nil)
}

// sourceSpec is the filter for source i of a prepared run.
func (p *prepared) sourceSpec(i int, protectDest bool) filterSpec {
	return filterSpec{f: p.in.filters, protectDest: protectDest, excludeDirs: p.excludes[i]}
}

// scanAll walks every source with its filter.
func (e *Engine) scanAll(ctx context.Context, p *prepared) error {
	opts := scanOpts{budget: p.in.budget}
	if p.in.jobType != jobTypeArchive {
		opts.caseCheck = p.dest.q.CaseInsensitive
		opts.sizeCheck = hasQuirk(p.dest.q.Quirks, QuirkMaxFile4G)
	}
	start := time.Now()
	for i, s := range p.sources {
		sctx, _, err := withFilter(ctx, p.sourceSpec(i, p.in.jobType != jobTypeArchive))
		if err != nil {
			return err
		}
		sctx, _ = rcloneContext(sctx)
		f, err := s.fsAt(sctx, "", fsOpts{links: p.dest != nil && p.dest.local && s.local, oneFileSystem: true})
		if err != nil {
			return engineErr(err, "opening the source")
		}
		o := opts
		if o.budget > 0 {
			o.budget -= time.Since(start)
			if o.budget <= 0 {
				p.scan.Partial = true
				break
			}
		}
		r, err := scanSource(sctx, f, o)
		if err != nil {
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			return engineErr(err, "reading the source")
		}
		p.scan.Files += r.Files
		p.scan.Bytes += r.Bytes
		p.scan.TooLargeCount += r.TooLargeCount
		p.scan.TooLarge = appendCapped(p.scan.TooLarge, r.TooLarge)
		p.scan.CollisionCount += r.CollisionCount
		p.scan.Collisions = appendCapped(p.scan.Collisions, r.Collisions)
		p.scan.Partial = p.scan.Partial || r.Partial
	}
	p.scanned = true
	return nil
}

func appendCapped(dst, src []string) []string {
	for _, s := range src {
		if len(dst) >= maxReported {
			break
		}
		dst = append(dst, s)
	}
	return dst
}

func hasQuirk(q []Quirk, x Quirk) bool {
	for _, y := range q {
		if y == x {
			return true
		}
	}
	return false
}

// checkQuirks is §6.3 check 5: FAT32 file size and name case collisions.
func (e *Engine) checkQuirks(p *prepared) Check {
	if !p.scanned {
		return newCheck(CheckFSQuirks, CheckSkip, "", nil)
	}
	if p.in.jobType == jobTypeArchive {
		// One archive file: only its size matters on FAT32.
		if hasQuirk(p.dest.q.Quirks, QuirkMaxFile4G) && p.scan.Bytes > fat32MaxFile {
			return newCheck(CheckFSQuirks, CheckFail, CodeFat32FileTooLarge, map[string]interface{}{"count": 1, "bytes": p.scan.Bytes})
		}
		return newCheck(CheckFSQuirks, CheckPass, "", nil)
	}
	if p.scan.TooLargeCount > 0 {
		return newCheck(CheckFSQuirks, CheckFail, CodeFat32FileTooLarge, map[string]interface{}{
			"count": p.scan.TooLargeCount, "paths": strings.Join(p.scan.TooLarge, "\n")})
	}
	if p.scan.CollisionCount > 0 {
		return newCheck(CheckFSQuirks, CheckFail, CodeCaseCollision, map[string]interface{}{
			"count": p.scan.CollisionCount, "paths": strings.Join(p.scan.Collisions, "\n")})
	}
	if p.scan.Partial {
		return newCheck(CheckFSQuirks, CheckSkip, "", nil)
	}
	return newCheck(CheckFSQuirks, CheckPass, "", nil)
}

// needBytes is what the free-space check requires (spec §6.3 check 4):
// a first run needs the source plus 5 %; later copy and mirror runs need
// 1 GiB plus the delta (delta is known after the planning pass, 0
// before); every archive is a full new file, sized like the newest
// existing one (or the source, if there is none) plus headroom.
func needBytes(p *prepared, delta int64, lastArchive int64) int64 {
	full := int64(math.Ceil(float64(p.scan.Bytes) * 1.05))
	switch {
	case p.in.jobType == jobTypeArchive && lastArchive > 0:
		return int64(math.Ceil(float64(lastArchive)*1.1)) + gib
	case p.in.jobType == jobTypeArchive, p.in.firstRun:
		return full
	}
	return gib + delta
}

// checkFreeSpace compares the destination's free space with the need.
func (e *Engine) checkFreeSpace(ctx context.Context, p *prepared, delta int64) Check {
	if p.dest == nil || !p.dest.online || !p.scanned {
		return newCheck(CheckFreeSpace, CheckSkip, "", nil)
	}
	var last int64
	if p.in.jobType == jobTypeArchive && p.in.jobID != "" {
		if archives, err := e.listArchives(ctx, p.dest, p.in.jobID); err == nil && len(archives) > 0 {
			last = archives[0].size
		}
	}
	need := needBytes(p, delta, last)
	if c, staged := e.stagingCheck(ctx, p, need); staged {
		if c.Status == CheckFail {
			return c
		}
	}
	free := e.freeSpace(ctx, p.dest)
	p.estimate.DestFree = free
	p.estimate.NeedBytes = need
	if free == nil {
		return newCheck(CheckFreeSpace, CheckSkip, "", map[string]interface{}{"need_bytes": need})
	}
	args := map[string]interface{}{"need_bytes": need, "free_bytes": *free}
	if p.scan.Partial {
		// Only part of the source was measured: a shortfall is certain,
		// enough space is not.
		if *free < need {
			return newCheck(CheckFreeSpace, CheckFail, CodeNoSpace, args)
		}
		return newCheck(CheckFreeSpace, CheckSkip, "", args)
	}
	if *free < need {
		return newCheck(CheckFreeSpace, CheckFail, CodeNoSpace, args)
	}
	return newCheck(CheckFreeSpace, CheckPass, "", args)
}

// stagingCheck: an archive for a remote that can't take streamed
// uploads is built in the staging folder first (see stageArchive), so
// that folder needs room for all of it. staged is false when no staging
// is involved.
func (e *Engine) stagingCheck(ctx context.Context, p *prepared, need int64) (c Check, staged bool) {
	if p.in.jobType != jobTypeArchive || p.dest.local {
		return Check{}, false
	}
	f, err := p.dest.fsAt(ctx, "", fsOpts{})
	if err != nil || f.Features().PutStream != nil {
		return Check{}, false
	}
	free := statfsFree(e.cfg.StagingDir)
	if free == nil {
		return newCheck(CheckFreeSpace, CheckSkip, "", map[string]interface{}{"need_bytes": need}), true
	}
	args := map[string]interface{}{"need_bytes": need, "free_bytes": *free, "where": "staging"}
	if *free < need {
		return newCheck(CheckFreeSpace, CheckFail, CodeNoSpace, args), true
	}
	return newCheck(CheckFreeSpace, CheckPass, "", args), true
}

// sentinelTrip evaluates the empty-source sentinel (spec §6.3 check 7);
// nil when it doesn't trip.
func sentinelTrip(files int64, g Guards, base *Baseline) *GuardInfo {
	if files == 0 && !g.AllowEmptySrc {
		total := int64(0)
		if base != nil {
			total = base.SourceFiles
		}
		return &GuardInfo{Guard: guardEmptySource, Pct: 100, Limit: g.EmptySourcePct, Count: total, Total: total, Sample: []string{}}
	}
	if base == nil || base.SourceFiles <= 0 || files >= base.SourceFiles || g.EmptySourcePct <= 0 {
		return nil
	}
	drop := base.SourceFiles - files
	pct := percent(drop, base.SourceFiles)
	if pct > float64(g.EmptySourcePct) {
		return &GuardInfo{Guard: guardEmptySource, Pct: pct, Limit: g.EmptySourcePct, Count: drop, Total: base.SourceFiles, Sample: []string{}}
	}
	return nil
}

func sentinelCheck(p *prepared, override []string) Check {
	if !p.scanned || p.scan.Partial {
		return newCheck(CheckSourceSentinel, CheckSkip, "", nil)
	}
	g := sentinelTrip(p.scan.Files, p.in.guards, p.in.baseline)
	if g == nil || overridden(override, guardEmptySource) {
		return newCheck(CheckSourceSentinel, CheckPass, "", nil)
	}
	return newCheck(CheckSourceSentinel, CheckFail, CodeEmptySource, map[string]interface{}{"pct": g.Pct, "files": p.scan.Files})
}

// markerCheck is the identity check as a precheck. A new job (no folder
// id yet) only gets a warning when the folder belongs to another job.
func (e *Engine) markerCheck(ctx context.Context, p *prepared) Check {
	if p.in.destFolderID == "" {
		m, err := e.readMarker(ctx, p.dest)
		if err != nil {
			return newCheck(CheckDestMarker, CheckWarn, CodeOf(err), nil)
		}
		if m != nil {
			return newCheck(CheckDestMarker, CheckWarn, CodeDestMarkerMismatch, map[string]interface{}{"job_id": m.JobID})
		}
		return newCheck(CheckDestMarker, CheckPass, "", nil)
	}
	m, err := e.checkMarker(ctx, p.dest, p.in.destFolderID, p.in.firstRun)
	p.marker = m
	if err != nil {
		return newCheck(CheckDestMarker, CheckFail, CodeOf(err), nil)
	}
	return newCheck(CheckDestMarker, CheckPass, "", nil)
}

func percent(n, of int64) float64 {
	if of <= 0 {
		return 0
	}
	return math.Round(float64(n)*1000/float64(of)) / 10
}

// Precheck runs every check that needs no write and estimates the size
// (POST /validate). Nothing is written, and the source walk is bounded
// by SizeTimeoutSec.
func (e *Engine) Precheck(ctx context.Context, req PrecheckRequest) (PrecheckResult, error) {
	budget := defaultSizeBudget
	if req.SizeTimeoutSec > 0 {
		budget = time.Duration(req.SizeTimeoutSec) * time.Second
	}
	jt := req.JobType
	if jt != jobTypeCopy && jt != jobTypeMirror && jt != jobTypeArchive {
		return PrecheckResult{}, Errorf(CodeInternal, "unknown job type %q", jt)
	}
	p, err := e.prepare(ctx, precheckInput{
		jobType: jt, sources: req.Sources, dest: req.Dest, creds: req.SMBCreds, filters: req.Filters,
		guards: req.Guards, destFolderID: req.DestFolderID, firstRun: req.FirstRun, baseline: req.Baseline,
		budget: budget,
	})
	if err != nil {
		return PrecheckResult{}, err
	}
	if jt == jobTypeMirror && p.dest != nil && p.dest.online && !p.dest.local {
		if c := e.recycleCheck(ctx, p.dest); c != nil {
			p.checks = replaceCheck(p.checks, *c)
		}
	}
	res := PrecheckResult{OK: true, Checks: p.checks, Estimate: p.estimate}
	for _, c := range p.checks {
		if c.Status == CheckFail {
			res.OK = false
		}
	}
	return res, nil
}

// recycleCheck fails a mirror to a remote that can't move files
// server-side: the recycle folder would need a full re-upload of every
// replaced file, and rclone refuses. nil when fine.
func (e *Engine) recycleCheck(ctx context.Context, t *target) *Check {
	f, err := t.fsAt(ctx, "", fsOpts{})
	if err != nil {
		return nil
	}
	if f.Features().Move != nil || f.Features().Copy != nil {
		return nil
	}
	c := newCheck(CheckFSQuirks, CheckFail, CodePathNotAllowed, map[string]interface{}{"reason": "no_server_side_move"})
	return &c
}

func replaceCheck(cs []Check, c Check) []Check {
	for i := range cs {
		if cs[i].ID == c.ID {
			cs[i] = c
			return cs
		}
	}
	return append(cs, c)
}

func toString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case error:
		return x.Error()
	}
	return strings.TrimSpace(strings.ReplaceAll(fmtAny(v), "\n", " "))
}

// errIsNotFound reports rclone's "nothing there" errors.
func errIsNotFound(err error) bool {
	return errors.Is(err, fs.ErrorDirNotFound) || errors.Is(err, fs.ErrorObjectNotFound)
}
