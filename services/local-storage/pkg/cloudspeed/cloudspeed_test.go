package cloudspeed

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/hash"
)

// fakeFs is an in-memory remote with a network model: a fixed delay
// before the first byte of every request, a per-connection rate cap, and
// (for uploads) a buffered chunk the backend reads ahead of the wire plus
// a commit delay after the last byte - the costs that made the old test
// wrong.
type fakeFs struct {
	downTTFB    time.Duration
	downPerConn float64 // bytes/s per connection
	downTotal   float64 // bytes/s shared by all connections (0 = none)
	upSetup     time.Duration
	upRate      float64 // bytes/s
	upChunk     int     // bytes the backend buffers ahead of the wire
	upCommit    time.Duration
	free        int64 // 0 = About unsupported

	mu      sync.Mutex
	objects map[string][]byte
	dirs    map[string]bool
	shared  *pacer
	opens   atomic.Int32
}

func newFake() *fakeFs {
	return &fakeFs{objects: map[string][]byte{}, dirs: map[string]bool{}, upChunk: 256 * 1024}
}

// pacer spaces byte delivery to a rate; safe for concurrent use.
type pacer struct {
	mu   sync.Mutex
	rate float64
	next time.Time
}

func (p *pacer) wait(ctx context.Context, n int) error {
	p.mu.Lock()
	now := time.Now()
	// Allow catching up a little after a late wake-up, so scheduler
	// jitter doesn't make the fake slower than its nominal rate.
	if p.next.Before(now.Add(-50 * time.Millisecond)) {
		p.next = now.Add(-50 * time.Millisecond)
	}
	p.next = p.next.Add(time.Duration(float64(n) / p.rate * float64(time.Second)))
	until := p.next
	p.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(until)):
		return nil
	}
}

func (f *fakeFs) Name() string                        { return "fake" }
func (f *fakeFs) Root() string                        { return "" }
func (f *fakeFs) String() string                      { return "fake:" }
func (f *fakeFs) Precision() time.Duration            { return time.Second }
func (f *fakeFs) Hashes() hash.Set                    { return hash.Set(hash.None) }
func (f *fakeFs) Mkdir(context.Context, string) error { return nil }
func (f *fakeFs) Rmdir(context.Context, string) error { return nil }
func (f *fakeFs) Features() *fs.Features {
	feat := &fs.Features{}
	if f.free > 0 {
		feat.About = func(context.Context) (*fs.Usage, error) {
			free := f.free
			return &fs.Usage{Free: &free}, nil
		}
	}
	return feat
}

func (f *fakeFs) List(ctx context.Context, dir string) (fs.DirEntries, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out fs.DirEntries
	for name := range f.objects {
		out = append(out, &fakeObj{f: f, remote: name, size: int64(len(f.objects[name]))})
	}
	for d := range f.dirs {
		out = append(out, fs.NewDir(d, time.Now()))
	}
	return out, nil
}

func (f *fakeFs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[remote]
	if !ok {
		return nil, fs.ErrorObjectNotFound
	}
	return &fakeObj{f: f, remote: remote, size: int64(len(b))}, nil
}

func (f *fakeFs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	select {
	case <-time.After(f.upSetup):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	wire := &pacer{rate: f.upRate}
	var buf bytes.Buffer
	chunk := make([]byte, f.upChunk)
	for {
		// Read a whole chunk ahead (instantly), then "send" it at wire speed.
		n, err := io.ReadFull(in, chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			for off := 0; off < n; off += 32 * 1024 {
				k := min(32*1024, n-off)
				if werr := wire.wait(ctx, k); werr != nil {
					return nil, werr
				}
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	time.Sleep(f.upCommit)
	f.mu.Lock()
	f.objects[src.Remote()] = buf.Bytes()
	f.mu.Unlock()
	return &fakeObj{f: f, remote: src.Remote(), size: int64(buf.Len())}, nil
}

type fakeObj struct {
	f      *fakeFs
	remote string
	size   int64
}

func (o *fakeObj) Fs() fs.Info                                     { return o.f }
func (o *fakeObj) String() string                                  { return o.remote }
func (o *fakeObj) Remote() string                                  { return o.remote }
func (o *fakeObj) ModTime(context.Context) time.Time               { return time.Now() }
func (o *fakeObj) Size() int64                                     { return o.size }
func (o *fakeObj) Hash(context.Context, hash.Type) (string, error) { return "", nil }
func (o *fakeObj) Storable() bool                                  { return true }
func (o *fakeObj) SetModTime(context.Context, time.Time) error     { return nil }
func (o *fakeObj) Update(context.Context, io.Reader, fs.ObjectInfo, ...fs.OpenOption) error {
	return errors.New("not supported")
}

func (o *fakeObj) Remove(ctx context.Context) error {
	o.f.mu.Lock()
	defer o.f.mu.Unlock()
	if _, ok := o.f.objects[o.remote]; !ok {
		return fs.ErrorObjectNotFound
	}
	delete(o.f.objects, o.remote)
	return nil
}

func (o *fakeObj) Open(ctx context.Context, options ...fs.OpenOption) (io.ReadCloser, error) {
	o.f.opens.Add(1)
	o.f.mu.Lock()
	data, ok := o.f.objects[o.remote]
	o.f.mu.Unlock()
	if !ok {
		return nil, fs.ErrorObjectNotFound
	}
	select {
	case <-time.After(o.f.downTTFB):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &throttled{ctx: ctx, r: bytes.NewReader(data), conn: &pacer{rate: o.f.downPerConn}, shared: o.f.sharedPacer()}, nil
}

func (f *fakeFs) sharedPacer() *pacer {
	if f.downTotal <= 0 {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.shared == nil {
		f.shared = &pacer{rate: f.downTotal}
	}
	return f.shared
}

type throttled struct {
	ctx    context.Context
	r      io.Reader
	conn   *pacer
	shared *pacer
}

func (t *throttled) Read(p []byte) (int, error) {
	if len(p) > 16*1024 {
		p = p[:16*1024]
	}
	n, err := t.r.Read(p)
	if n > 0 {
		if werr := t.conn.wait(t.ctx, n); werr != nil {
			return n, werr
		}
		if t.shared != nil {
			if werr := t.shared.wait(t.ctx, n); werr != nil {
				return n, werr
			}
		}
	}
	return n, err
}
func (t *throttled) Close() error { return nil }

// fast options so the suite runs in seconds; the logic is the same.
func testOpts() Options {
	return Options{
		MinSize:        1 * mib,
		MaxSize:        8 * mib,
		MinDuration:    700 * time.Millisecond,
		TargetDuration: 900 * time.Millisecond,
		Streams:        4,
		SampleTick:     25 * time.Millisecond,
	}
}

func near(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > want*tol {
		t.Errorf("%s = %.1f, want %.1f ±%.0f%%", what, got, want, tol*100)
	}
}

// The old test's failure mode: a slow first byte and a commit step make a
// short transfer look slow. The steady numbers must match the wire rate,
// the setup must show up as latency, not as lost bandwidth.
func TestSlowFirstByteIsLatencyNotBandwidth(t *testing.T) {
	f := newFake()
	f.downTTFB = 300 * time.Millisecond
	f.downPerConn = 1e9
	f.downTotal = 4e6 // 32 Mbps line, whatever the stream count
	f.upSetup = 300 * time.Millisecond
	f.upRate = 2e6 // 16 Mbps
	f.upCommit = 250 * time.Millisecond

	res, err := Run(context.Background(), f, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	near(t, "download Mbps", res.Download.Mbps, 32, 0.15)
	near(t, "download single Mbps", res.Download.SingleMbps, 32, 0.15)
	near(t, "download latency ms", res.Download.LatencyMs, 300, 0.25)
	near(t, "upload Mbps", res.Upload.Mbps, 16, 0.15)
	near(t, "upload latency ms", res.Upload.LatencyMs, 300, 0.25)
	if res.Upload.FinishMs < 200 {
		t.Errorf("upload finish = %.0f ms, want >= the 250 ms commit", res.Upload.FinishMs)
	}
	near(t, "MB/s is decimal Mbps/8", res.Download.MBps, res.Download.Mbps/8, 0.02)
	if res.Download.Streams != 1 {
		t.Errorf("no per-connection limit, yet headline credited %d streams", res.Download.Streams)
	}
	// What the old test would have said (whole wall time) is clearly lower.
	if res.Upload.EffectiveMbps >= res.Upload.Mbps*0.95 {
		t.Errorf("effective %.1f should be below steady %.1f with 550 ms of fixed cost", res.Upload.EffectiveMbps, res.Upload.Mbps)
	}
	if n := len(f.objects); n != 0 {
		t.Errorf("%d test objects left behind", n)
	}
}

// A provider that caps each connection: one stream gets 16 Mbps, four get
// 64. Both must be reported and the headline must be the parallel one.
func TestThrottledSingleStream(t *testing.T) {
	f := newFake()
	f.downTTFB = 100 * time.Millisecond
	f.downPerConn = 2e6 // 16 Mbps per connection
	f.upRate = 8e6

	res, err := Run(context.Background(), f, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	near(t, "single Mbps", res.Download.SingleMbps, 16, 0.15)
	near(t, "parallel Mbps", res.Download.ParallelMbps, 64, 0.2)
	near(t, "headline Mbps", res.Download.Mbps, 64, 0.2)
	if res.Download.Streams != 4 {
		t.Errorf("streams = %d, want 4", res.Download.Streams)
	}
	if !hasNote(res, "limits each connection") {
		t.Errorf("no per-connection note: %v", res.Notes)
	}
}

// A line-limited provider (shared cap): parallel doesn't beat single, so
// the headline stays single-stream and no throttle note appears.
func TestLineLimitedNoParallelCredit(t *testing.T) {
	f := newFake()
	f.downTTFB = 50 * time.Millisecond
	f.downPerConn = 1e9
	f.downTotal = 3e6 // 24 Mbps whatever the stream count
	f.upRate = 8e6

	res, err := Run(context.Background(), f, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	near(t, "download Mbps", res.Download.Mbps, 24, 0.15)
	if res.Download.Streams != 1 || hasNote(res, "limits each connection") {
		t.Errorf("parallel wrongly credited: streams=%d notes=%v", res.Download.Streams, res.Notes)
	}
}

// The upload grows until a transfer lasts MinDuration (or hits MaxSize),
// removing the too-short probes as it goes.
func TestAdaptiveSizeGrowsAndCleansUp(t *testing.T) {
	f := newFake()
	f.downPerConn = 50e6
	f.upRate = 6e6 // 1 MiB probe takes ~0.17 s -> must grow
	res, err := Run(context.Background(), f, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.FileProbes < 2 {
		t.Errorf("probes = %d, want the size to grow past the 1 MiB probe", res.FileProbes)
	}
	if res.FileBytes <= 1*mib {
		t.Errorf("final file %d bytes, want bigger than the probe", res.FileBytes)
	}
	near(t, "upload Mbps", res.Upload.Mbps, 48, 0.15)
	if n := len(f.objects); n != 0 {
		t.Errorf("%d test objects left behind", n)
	}
}

func TestFreeSpaceCapsSize(t *testing.T) {
	f := newFake()
	f.downPerConn = 50e6
	f.upRate = 50e6
	f.free = 12 * mib // max = free/4 = 3 MiB
	res, err := Run(context.Background(), f, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.FileBytes > 3*mib {
		t.Errorf("file %d bytes, want <= free/4", res.FileBytes)
	}

	f2 := newFake()
	f2.free = 1 * mib
	if _, err := Run(context.Background(), f2, testOpts()); err == nil || !strings.Contains(err.Error(), "free space") {
		t.Errorf("want a free-space error, got %v", err)
	}
}

// Cancel mid-download: Run returns the context error and still removes
// the test file. Leftovers from an earlier crashed run are swept; other
// files are not touched.
func TestCancelCleansUpAndSweepSparesOthers(t *testing.T) {
	f := newFake()
	f.downPerConn = 1e6
	f.upRate = 20e6
	f.objects[TestFilePrefix+"123"] = []byte("stale")
	f.objects["holiday.jpg"] = []byte("keep me")
	f.dirs[TestFilePrefix+"dir"] = true

	ctx, cancel := context.WithCancel(context.Background())
	opt := testOpts()
	opt.Progress = func(p Progress) {
		if p.Phase == "download" && p.Bytes > 0 {
			cancel()
		}
	}
	_, err := Run(ctx, f, opt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(f.objects) != 1 || f.objects["holiday.jpg"] == nil {
		t.Errorf("objects after run: %v", keys(f.objects))
	}
}

// Big files much slower than small ones (free TeraBox): both are
// measured and the headline is the small-file rate, with a note.
func TestBigFileThrottleReportsBothTiers(t *testing.T) {
	f := &bigFileThrottleFs{fakeFs: newFake(), limit: 40 * mib}
	f.downPerConn = 4e6 // 32 Mbps for small files
	f.upRate = 60e6     // fast upload -> grows to a big file
	opt := testOpts()
	opt.MaxSize = 64 * mib
	res, err := Run(context.Background(), f, opt)
	if err != nil {
		t.Fatal(err)
	}
	// Big file: 1.6 Mbps per connection, so at best 4 x 1.6 on 4 streams.
	if res.Download.LargeFileMbps == 0 || res.Download.LargeFileMbps > 10 {
		t.Errorf("large-file Mbps = %.1f, want the big-file crawl (<= ~6.4)", res.Download.LargeFileMbps)
	}
	near(t, "small-file Mbps", res.Download.SingleMbps, 32, 0.2)
	if !hasNote(res, "Big files download much slower") {
		t.Errorf("no big-file note: %v", res.Notes)
	}
	if n := len(f.objects); n != 0 {
		t.Errorf("%d test objects left behind", n)
	}
}

type bigFileThrottleFs struct {
	*fakeFs
	limit int64
}

func (b *bigFileThrottleFs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	o, err := b.fakeFs.Put(ctx, in, src, options...)
	if err != nil {
		return nil, err
	}
	return &bigObj{fakeObj: o.(*fakeObj), limit: b.limit}, nil
}

type bigObj struct {
	*fakeObj
	limit int64
}

func (o *bigObj) Open(ctx context.Context, options ...fs.OpenOption) (io.ReadCloser, error) {
	rc, err := o.fakeObj.Open(ctx, options...)
	if err != nil || o.size <= o.limit {
		return rc, err
	}
	t := rc.(*throttled)
	t.conn = &pacer{rate: 0.2e6}
	return t, nil
}

func hasNote(r *Result, s string) bool {
	for _, n := range r.Notes {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestTrimmedMean(t *testing.T) {
	// 10 samples: slowest 3 and fastest 1 dropped.
	got := trimmedMean([]float64{0, 0, 0, 10, 10, 10, 10, 10, 10, 1000})
	if got != 10 {
		t.Errorf("trimmedMean = %v, want 10", got)
	}
}
