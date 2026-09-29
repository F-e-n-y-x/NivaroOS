// Package cloudspeed measures how fast an online-storage account really
// is, the way a person would judge it: how quickly a file goes up and
// comes back down once the transfer is actually flowing.
//
// The old test pushed one 8 MB file through a single stream and divided
// by the whole wall time. On a 300 Mbps line 8 MB takes ~0.2 s, so the
// provider's fixed per-file cost (token refresh, upload-session create,
// precreate/commit calls, download-link lookup, redirects, TLS, time to
// first byte) - often a second or more - dominated, and the number said
// more about API latency than about bandwidth. This engine keeps the two
// apart:
//
//   - latency: how long until the first byte moves (setup cost per file)
//   - throughput: the steady rate once it moves, from 250 ms samples
//
// Uploads grow in size until one lasts long enough to mean something;
// downloads are time-bounded, run once on one stream and once on several
// ranged streams (providers often throttle per connection), and report
// both. Everything is decimal: Mbps = 10^6 bit/s, MB/s = 10^6 byte/s, the
// units internet plans and speedtest.net use.
package cloudspeed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/object"
)

// TestFilePrefix names every object this package creates. Anything at the
// remote's root with this prefix is ours and safe to remove.
const TestFilePrefix = ".nivaroos-speedtest-"

const (
	mb  = 1000 * 1000
	mib = 1024 * 1024
)

// Options tunes a run. Zero values take the defaults below.
type Options struct {
	MinSize        int64         // first upload probe (16 MiB)
	MaxSize        int64         // never upload more than this (256 MiB)
	MinDuration    time.Duration // an upload shorter than this is too short to trust (6 s)
	TargetDuration time.Duration // what the next, bigger upload aims for; also each download phase's length (8 s)
	Streams        int           // parallel download streams (4)
	SampleTick     time.Duration // throughput sample slice (250 ms)
	UploadStreams  int           // how many connections the backend itself uses for one upload (informational)
	Progress       func(Progress)
}

func (o *Options) defaults() {
	if o.MinSize <= 0 {
		o.MinSize = 16 * mib
	}
	if o.MaxSize <= 0 {
		o.MaxSize = 256 * mib
	}
	if o.MinDuration <= 0 {
		o.MinDuration = 6 * time.Second
	}
	if o.TargetDuration <= 0 {
		o.TargetDuration = 8 * time.Second
	}
	if o.Streams <= 0 {
		o.Streams = 4
	}
	if o.SampleTick <= 0 {
		o.SampleTick = 250 * time.Millisecond
	}
	if o.UploadStreams <= 0 {
		o.UploadStreams = 1
	}
}

// Progress is what a poller sees while a run is in flight.
type Progress struct {
	Phase    string  `json:"phase"` // prepare | upload | download | download_parallel | cleanup
	LiveMbps float64 `json:"live_mbps"`
	Bytes    int64   `json:"bytes"`
}

// Direction is one side (upload or download) of a finished run.
type Direction struct {
	Mbps            float64 `json:"mbps"`          // headline: steady throughput, best of the modes tried
	MBps            float64 `json:"mb_per_s"`      // same, in decimal megabytes per second
	Streams         int     `json:"streams"`       // connections behind the headline number
	SingleMbps      float64 `json:"single_mbps"`   // one connection
	ParallelMbps    float64 `json:"parallel_mbps"` // ParallelStreams connections (0 = not tried)
	ParallelStreams int     `json:"parallel_streams,omitempty"`
	LatencyMs       float64 `json:"latency_ms"`     // request -> first byte moving (per-file setup cost)
	FinishMs        float64 `json:"finish_ms"`      // upload only: last byte handed over -> provider confirmed the file
	Bytes           int64   `json:"bytes"`          // bytes moved in this direction, all modes
	Seconds         float64 `json:"seconds"`        // time spent moving them
	EffectiveMbps   float64 `json:"effective_mbps"` // one whole file, setup included (what the old test reported)

	// Download only, set when big files were much slower than small ones:
	// the headline is then the small file's, and this is the big one's.
	LargeFileMbps  float64 `json:"large_file_mbps,omitempty"`
	LargeFileBytes int64   `json:"large_file_bytes,omitempty"`
}

// Result is a finished run.
type Result struct {
	Download   Direction `json:"download"`
	Upload     Direction `json:"upload"`
	FileBytes  int64     `json:"file_bytes"` // size of the test file the numbers come from
	FileProbes int       `json:"file_probes"`
	Notes      []string  `json:"notes,omitempty"`
}

// Run uploads a temporary file to f's root, reads it back, and removes
// it - always, even on error or cancel. Files a crashed earlier run left
// behind (same prefix) are swept first and last.
func Run(ctx context.Context, f fs.Fs, opt Options) (res *Result, err error) {
	opt.defaults()
	report := func(p Progress) {
		if opt.Progress != nil {
			opt.Progress(p)
		}
	}
	report(Progress{Phase: "prepare"})

	Sweep(ctx, f)
	var created []fs.Object
	defer func() {
		report(Progress{Phase: "cleanup"})
		cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, o := range created {
			if rerr := o.Remove(cctx); rerr != nil {
				fs.Errorf(f, "speedtest: failed to remove %q: %v", o.Remote(), rerr)
			}
		}
		Sweep(cctx, f)
	}()

	maxSize := opt.MaxSize
	if about := f.Features().About; about != nil {
		actx, cancel := context.WithTimeout(ctx, 30*time.Second)
		u, aerr := about(actx)
		cancel()
		if aerr == nil && u != nil && u.Free != nil {
			free := *u.Free
			if free < 2*opt.MinSize {
				return nil, fmt.Errorf("not enough free space for a speed test (%s free)", fmtMB(free))
			}
			if free/4 < maxSize {
				maxSize = roundMiB(free / 4)
			}
		}
	}
	if maxSize < opt.MinSize {
		maxSize = opt.MinSize
	}

	res = &Result{}

	// --- Upload: grow the file until one transfer lasts MinDuration.
	size := opt.MinSize
	var up *uploadRun
	for {
		res.FileProbes++
		name := TestFilePrefix + strconv.FormatInt(time.Now().UnixNano(), 10)
		var run *uploadRun
		var uerr error
		// Providers answer the odd request with a rate-limit or 5xx (Drive's
		// shared client_id quota does, often); retry the file a couple of
		// times before calling the test failed.
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt) * 3 * time.Second):
				}
			}
			run, uerr = upload(ctx, f, name, size, opt, func(live float64, n int64) {
				report(Progress{Phase: "upload", LiveMbps: live, Bytes: n})
			})
			if run != nil && run.obj != nil {
				created = append(created, run.obj)
			}
			if uerr == nil || ctx.Err() != nil {
				break
			}
		}
		if uerr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("upload failed: %w", uerr)
		}
		// Long enough = the bytes themselves flowed for MinDuration (setup
		// and commit don't count). A backend that buffers the whole file
		// reads it instantly; there a long first-byte -> done time is the
		// best signal we get, so accept that too rather than growing a
		// slow upload to MaxSize.
		readSpan := run.m.lastTime().Sub(run.m.firstTime())
		wire := run.end.Sub(run.m.firstTime())
		if readSpan >= opt.MinDuration || wire >= 3*opt.MinDuration || size >= maxSize {
			up = run
			break
		}
		// Too short to trust: drop it and size the next one from what we saw.
		if rerr := run.obj.Remove(ctx); rerr == nil {
			created = created[:len(created)-1]
		}
		rate := float64(size) / math.Max(wire.Seconds(), 0.05)
		next := int64(rate * opt.TargetDuration.Seconds() * 1.2)
		if next < 2*size {
			next = 2 * size
		}
		if next > maxSize {
			next = maxSize
		}
		size = roundMiB(next)
	}
	res.FileBytes = up.size
	res.Upload = up.direction(opt.UploadStreams)

	// --- Download: time-bounded, one stream then several.
	dl, err := measureDownload(ctx, up.obj, opt, report)
	if err != nil {
		return nil, err
	}
	res.Download = dl

	// Some providers throttle big files far harder than small ones (free
	// TeraBox: ~8 Mbps under ~50 MB, a fraction of 1 Mbps above). If the
	// big file came down at a crawl next to the upload, measure a small
	// one too and report both, so the headline isn't just the worst case.
	if up.size > smallTierSize && dl.Mbps < res.Upload.Mbps/4 {
		name := TestFilePrefix + strconv.FormatInt(time.Now().UnixNano(), 10)
		small, uerr := upload(ctx, f, name, smallTierSize, opt, nil)
		if small != nil && small.obj != nil {
			created = append(created, small.obj)
		}
		if uerr == nil {
			sdl, derr := measureDownload(ctx, small.obj, opt, report)
			if derr == nil && sdl.Mbps > dl.Mbps*1.5 {
				sdl.LargeFileMbps = dl.Mbps
				sdl.LargeFileBytes = up.size
				sdl.Bytes += dl.Bytes
				sdl.Seconds = round2(sdl.Seconds + dl.Seconds)
				res.Download = sdl
				res.Notes = append(res.Notes, fmt.Sprintf("Big files download much slower here: a %s file came down at %s Mbps, a %s file at %s Mbps.",
					fmtMB(up.size), fmtMbps(dl.Mbps), fmtMB(smallTierSize), fmtMbps(sdl.Mbps)))
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	if p := res.Download.ParallelMbps; p > 0 && p > res.Download.SingleMbps*1.5 {
		res.Notes = append(res.Notes, fmt.Sprintf("This provider limits each connection: %d parallel streams reached %s Mbps, one stream %s Mbps.", opt.Streams, fmtMbps(p), fmtMbps(res.Download.SingleMbps)))
	}
	if res.Download.LatencyMs >= 1000 || res.Upload.LatencyMs >= 1000 {
		res.Notes = append(res.Notes, "Every file has a noticeable start-up delay at this provider, so many small files transfer much slower than the speeds above.")
	}
	return res, nil
}

const smallTierSize = 32 * mib

// measureDownload runs the single-stream and the parallel phase on obj.
func measureDownload(ctx context.Context, obj fs.Object, opt Options, report func(Progress)) (Direction, error) {
	single, err := download(ctx, obj, 1, opt, func(live float64, n int64) {
		report(Progress{Phase: "download", LiveMbps: live, Bytes: n})
	})
	if err != nil {
		if ctx.Err() != nil {
			return Direction{}, ctx.Err()
		}
		return Direction{}, fmt.Errorf("download failed: %w", err)
	}
	d := Direction{
		SingleMbps:    round1(single.mbps),
		Streams:       1,
		LatencyMs:     round1(ms(single.latency)),
		Bytes:         single.bytes,
		Seconds:       round2(single.seconds),
		EffectiveMbps: round1(single.effectiveMbps),
	}
	best := single.mbps
	if opt.Streams > 1 {
		par, perr := download(ctx, obj, opt.Streams, opt, func(live float64, n int64) {
			report(Progress{Phase: "download_parallel", LiveMbps: live, Bytes: n})
		})
		if perr == nil {
			d.ParallelMbps = round1(par.mbps)
			d.ParallelStreams = opt.Streams
			d.Bytes += par.bytes
			d.Seconds = round2(d.Seconds + par.seconds)
			// Only credit parallel when it clearly wins; within noise, one
			// stream is the honest answer (it's what opening a file gets).
			if par.mbps > single.mbps*1.15 {
				best = par.mbps
				d.Streams = opt.Streams
			}
		} else if ctx.Err() != nil {
			return Direction{}, ctx.Err()
		}
	}
	d.Mbps = round1(best)
	d.MBps = round1(best / 8)
	return d, nil
}

func fmtMB(n int64) string { return strconv.FormatInt((n+mb/2)/mb, 10) + " MB" }

func fmtMbps(v float64) string {
	if v < 10 {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strconv.FormatFloat(v, 'f', 0, 64)
}

// Sweep removes leftover test files (our prefix, root level, files only).
func Sweep(ctx context.Context, f fs.Fs) {
	lctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	entries, err := f.List(lctx, "")
	if err != nil {
		return
	}
	for _, e := range entries {
		o, ok := e.(fs.Object)
		if !ok || !strings.HasPrefix(o.Remote(), TestFilePrefix) {
			continue
		}
		if err := o.Remove(lctx); err != nil {
			fs.Errorf(f, "speedtest: failed to remove leftover %q: %v", o.Remote(), err)
		}
	}
}

// --- measurement

type sample struct {
	t      time.Time
	n      int64
	active int32 // responses mid-body at this instant
	gen    int64 // start/end-of-body transitions so far
}

// meter counts bytes as they move and keeps periodic samples.
type meter struct {
	n           atomic.Int64
	active      atomic.Int32
	gen         atomic.Int64
	mu          sync.Mutex
	first, last time.Time
	samples     []sample
}

func (m *meter) bodyStart() { m.active.Add(1); m.gen.Add(1) }
func (m *meter) bodyEnd()   { m.active.Add(-1); m.gen.Add(1) }

func (m *meter) snap(t time.Time) sample {
	return sample{t: t, n: m.n.Load(), active: m.active.Load(), gen: m.gen.Load()}
}

func (m *meter) add(k int) {
	if k <= 0 {
		return
	}
	now := time.Now()
	m.mu.Lock()
	if m.first.IsZero() {
		m.first = now
	}
	m.last = now
	m.mu.Unlock()
	m.n.Add(int64(k))
}

func (m *meter) firstTime() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.first
}

func (m *meter) lastTime() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}

// sampleEvery records (time, bytes) each tick until stop is closed, and
// feeds the live rate (last ~1 s) to live.
func (m *meter) sampleEvery(tick time.Duration, stop <-chan struct{}, live func(float64, int64)) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			sn := m.snap(time.Now())
			m.mu.Lock()
			m.samples = append(m.samples, sn)
			m.mu.Unlock()
			return
		case now := <-t.C:
			sn := m.snap(now)
			n := sn.n
			m.mu.Lock()
			m.samples = append(m.samples, sn)
			s := m.samples
			m.mu.Unlock()
			if live != nil {
				k := len(s) - 1 - int(time.Second/tick)
				if k < 0 {
					k = 0
				}
				if d := now.Sub(s[k].t).Seconds(); d > 0 {
					live(round1(float64(n-s[k].n)*8/d/1e6), n)
				}
			}
		}
	}
}

// sliceMbps is the speedtest.net-style steady rate: per-slice throughput,
// slowest 30% (TCP ramp-up, stalls) and fastest 10% (bursts) dropped, the
// rest averaged. Only slices during which all `streams` responses were
// mid-body the whole time count - the gap while a stream waits for its
// next response's first byte is setup cost, reported as latency, not
// bandwidth. (Too few such slices - e.g. a tiny file - falls back to every
// slice after the first byte.)
func (m *meter) sliceMbps(streams int) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var full, all []float64
	for i := 1; i < len(m.samples); i++ {
		a, b := m.samples[i-1], m.samples[i]
		if b.n == 0 || !b.t.After(m.first) {
			continue // nothing moving yet
		}
		start := a.t
		if start.Before(m.first) {
			start = m.first
		}
		d := b.t.Sub(start).Seconds()
		if d <= 0.02 {
			continue
		}
		r := float64(b.n-a.n) * 8 / d / 1e6
		all = append(all, r)
		if a.gen == b.gen && b.active >= int32(streams) {
			full = append(full, r)
		}
	}
	if len(full) >= 4 {
		return trimmedMean(full)
	}
	return trimmedMean(all)
}

func trimmedMean(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	s := append([]float64(nil), samples...)
	sort.Float64s(s)
	lo, hi := len(s)*3/10, len(s)-len(s)/10
	if hi <= lo {
		lo, hi = 0, len(s)
	}
	var sum float64
	for _, v := range s[lo:hi] {
		sum += v
	}
	return sum / float64(hi-lo)
}

// --- upload

type uploadRun struct {
	obj        fs.Object
	size       int64
	start, end time.Time // Put called / Put returned
	m          *meter
}

// upload writes size pseudo-random bytes. The meter counts bytes as the
// backend pulls them from our reader - which runs ahead of the network by
// whatever the backend buffers (one chunk, or a few for parallel-chunk
// backends). That lead is constant while the transfer flows, so the slope
// between the 25% mark and the last byte handed over is the wire rate;
// the fill at the start and the drain + commit at the end are excluded.
func upload(ctx context.Context, f fs.Fs, name string, size int64, opt Options, live func(float64, int64)) (*uploadRun, error) {
	m := &meter{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { m.sampleEvery(opt.SampleTick, stop, live); close(done) }()

	var seed [32]byte
	binary := uint64(time.Now().UnixNano())
	for i := 0; i < 8; i++ {
		seed[i] = byte(binary >> (8 * i))
	}
	src := &countingReader{r: io.LimitReader(rand.NewChaCha8(seed), size), m: m}
	info := object.NewStaticObjectInfo(name, time.Now(), size, true, nil, nil)
	run := &uploadRun{size: size, m: m, start: time.Now()}
	uctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	obj, err := f.Put(uctx, src, info)
	run.end = time.Now()
	close(stop)
	<-done
	run.obj = obj
	if err != nil {
		return run, err
	}
	if obj == nil {
		return run, errors.New("backend returned no object")
	}
	if n := m.n.Load(); n != size {
		return run, fmt.Errorf("backend read %d of %d bytes", n, size)
	}
	return run, nil
}

func (u *uploadRun) direction(streams int) Direction {
	first, last := u.m.firstTime(), u.m.lastTime()
	total := u.m.n.Load()
	wire := u.end.Sub(first) // first byte -> Put returned (includes drain + commit)
	rate := 0.0

	u.m.mu.Lock()
	var mark *sample
	for i := range u.m.samples {
		s := u.m.samples[i]
		if s.n >= total/4 && s.t.After(first) {
			mark = &s
			break
		}
	}
	u.m.mu.Unlock()
	if mark != nil && last.After(mark.t) {
		span := last.Sub(mark.t)
		// If the reader was drained long before Put returned, the backend
		// buffered most of the file and the reader says nothing about the
		// wire - fall back to first byte -> done.
		if span.Seconds() >= 0.5*wire.Seconds() && total > mark.n {
			rate = float64(total-mark.n) * 8 / span.Seconds() / 1e6
		}
	}
	if rate == 0 && wire > 0 {
		rate = float64(total) * 8 / wire.Seconds() / 1e6
	}
	whole := u.end.Sub(u.start)
	return Direction{
		Mbps:          round1(rate),
		MBps:          round1(rate / 8),
		Streams:       streams,
		SingleMbps:    round1(rate),
		LatencyMs:     round1(ms(first.Sub(u.start))),
		FinishMs:      round1(ms(u.end.Sub(last))),
		Bytes:         total,
		Seconds:       round2(wire.Seconds()),
		EffectiveMbps: round1(float64(total) * 8 / math.Max(whole.Seconds(), 1e-3) / 1e6),
	}
}

type countingReader struct {
	r io.Reader
	m *meter
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.m.add(n)
	return n, err
}

// --- download

// bodyReader counts a response body and tells the meter while it's
// mid-body (first byte seen, not yet finished).
type bodyReader struct {
	r       io.Reader
	m       *meter
	started bool
}

func (b *bodyReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if n > 0 && !b.started {
		b.started = true
		b.m.bodyStart()
	}
	b.m.add(n)
	return n, err
}

func (b *bodyReader) done() {
	if b.started {
		b.started = false
		b.m.bodyEnd()
	}
}

type downloadRun struct {
	mbps          float64
	latency       time.Duration // first request -> first byte
	bytes         int64
	seconds       float64
	effectiveMbps float64 // the first full read of the file, setup included
}

// download reads obj on `streams` concurrent connections for
// TargetDuration from the first byte.
func download(ctx context.Context, obj fs.Object, streams int, opt Options, live func(float64, int64)) (*downloadRun, error) {
	m := &meter{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { m.sampleEvery(opt.SampleTick, stop, live); close(done) }()

	// The time box starts at the first byte, not at the first request, so
	// a slow first byte doesn't eat the measurement (it's reported as
	// latency). A hard cap still bounds a provider that never answers.
	dctx, cancel := context.WithTimeout(ctx, opt.TargetDuration+30*time.Second)
	defer cancel()
	go func() {
		poll := time.NewTicker(20 * time.Millisecond)
		defer poll.Stop()
		for {
			select {
			case <-dctx.Done():
				return
			case <-poll.C:
				if f := m.firstTime(); !f.IsZero() {
					select {
					case <-dctx.Done():
					case <-time.After(time.Until(f.Add(opt.TargetDuration))):
						cancel()
					}
					return
				}
			}
		}
	}()

	size := obj.Size()
	start := time.Now()
	var (
		wg        sync.WaitGroup
		errMu     sync.Mutex
		firstErr  error
		wholeOnce sync.Once
		wholeMbps float64
	)
	// Every stream reads the whole file, again and again until time is up.
	// Splitting one file into per-stream ranges would make each request
	// short, and each stream would then spend much of the window waiting
	// for its next first byte instead of overlapping with the others.
	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for dctx.Err() == nil {
				t0 := time.Now()
				rc, err := obj.Open(dctx)
				if err != nil {
					if dctx.Err() == nil {
						errMu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						errMu.Unlock()
					}
					return
				}
				body := &bodyReader{r: io.LimitReader(rc, size), m: m}
				n, err := io.Copy(io.Discard, body)
				body.done()
				_ = rc.Close()
				if err == nil && n == size {
					wholeOnce.Do(func() { wholeMbps = float64(n) * 8 / time.Since(t0).Seconds() / 1e6 })
				}
				if err != nil && dctx.Err() == nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	<-done

	total := m.n.Load()
	if total == 0 {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, errors.New("no data received")
	}
	first := m.firstTime()
	steady := m.sliceMbps(streams)
	// A trickle arrives in rare bursts: most slices are empty and the
	// trimmed mean reads ~0. The plain average is the better estimate then.
	if span := m.lastTime().Sub(first).Seconds(); span > 0 {
		if avg := float64(total) * 8 / span / 1e6; avg > steady {
			steady = avg
		}
	}
	r := &downloadRun{
		mbps:          steady,
		latency:       first.Sub(start),
		bytes:         total,
		seconds:       m.lastTime().Sub(first).Seconds(),
		effectiveMbps: wholeMbps,
	}
	if r.effectiveMbps == 0 {
		// Never finished one whole file within the time box: setup + what
		// we got is the closest equivalent.
		r.effectiveMbps = float64(total) * 8 / math.Max(m.lastTime().Sub(start).Seconds(), 1e-3) / 1e6
	}
	return r, nil
}

// --- helpers

func roundMiB(n int64) int64 {
	r := (n + mib - 1) / mib * mib
	if r < mib {
		r = mib
	}
	return r
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
func round1(v float64) float64   { return math.Round(v*10) / 10 }
func round2(v float64) float64   { return math.Round(v*100) / 100 }
