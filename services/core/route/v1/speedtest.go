package v1

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Internet speed test against the nearest speedtest.net (Ookla) server -
// the same servers speedtest.net itself picks. Cloudflare is not used: its
// numbers were well off real line speed. The old test gave numbers far below
// the real line speed because it used ONE connection, a fixed 4 MB (1 MB up)
// transfer and counted DNS + TLS setup and TCP slow start as transfer time;
// it also reported a made-up 45 ms ping when the ping failed.
//
// Now: latency = fastest time-to-first-byte of tiny requests on an already
// open connection; throughput = several parallel streams for a fixed time,
// sampled in slices and trimmed like speedtest.net (see steadyMbps).

// speedTarget is one test server's endpoints.
type speedTarget struct {
	Name    string
	PingURL string
	DownURL func(size int) string
	UpURL   string
}

// ooklaTarget: a speedtest.net server's endpoints. scheme is "https" for
// the JSON list's hosts and "http" for the XML list's host:8080 entries.
func ooklaTarget(scheme, host, name string) speedTarget {
	base := scheme + "://" + host
	return speedTarget{
		Name:    name,
		PingURL: base + "/hello",
		DownURL: func(n int) string {
			return fmt.Sprintf("%s/download?nocache=%d&size=%d", base, time.Now().UnixNano(), n)
		},
		UpURL: base + "/upload",
	}
}

type ooklaServer struct {
	Scheme, Host, Name, Sponsor string
}

// ooklaServerList: the servers speedtest.net suggests for this IP, nearest
// first - from its JSON API, else its older XML list.
func ooklaServerList(ctx context.Context, c *http.Client) ([]ooklaServer, error) {
	get := func(url string) ([]byte, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	}
	var jsonErr error
	if b, err := get("https://www.speedtest.net/api/js/servers?engine=js&https_functional=true&limit=10"); err == nil {
		var list []struct {
			Host    string `json:"host"`
			Name    string `json:"name"`
			Sponsor string `json:"sponsor"`
		}
		if err := json.Unmarshal(b, &list); err == nil && len(list) > 0 {
			out := make([]ooklaServer, 0, len(list))
			for _, s := range list {
				if s.Host != "" {
					out = append(out, ooklaServer{"https", s.Host, s.Name, s.Sponsor})
				}
			}
			if len(out) > 0 {
				return out, nil
			}
		}
		jsonErr = errors.New("empty server list")
	} else {
		jsonErr = err
	}
	b, err := get("https://www.speedtest.net/speedtest-servers-static.php")
	if err != nil {
		return nil, fmt.Errorf("speedtest.net server list: %v; %v", jsonErr, err)
	}
	out := parseOoklaXML(b)
	if len(out) == 0 {
		return nil, fmt.Errorf("speedtest.net server list: %v; no servers in the XML list", jsonErr)
	}
	return out, nil
}

// parseOoklaXML reads speedtest-servers-static.php (<server host="h:8080"
// name="City" sponsor="ISP" .../>), in the order given (nearest first).
func parseOoklaXML(b []byte) []ooklaServer {
	var doc struct {
		Servers []struct {
			Host    string `xml:"host,attr"`
			Name    string `xml:"name,attr"`
			Sponsor string `xml:"sponsor,attr"`
		} `xml:"servers>server"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return nil
	}
	var out []ooklaServer
	for _, s := range doc.Servers {
		if s.Host != "" {
			out = append(out, ooklaServer{"http", s.Host, s.Name, s.Sponsor})
		}
	}
	return out
}

// nearestOokla asks speedtest.net for the servers nearest this IP and
// returns the one with the lowest measured latency.
func nearestOokla(ctx context.Context, c *http.Client) (speedTarget, error) {
	servers, err := ooklaServerList(ctx, c)
	if err != nil {
		return speedTarget{}, err
	}
	if len(servers) > 5 {
		servers = servers[:5]
	}
	best, bestMs := speedTarget{}, math.MaxFloat64
	for _, s := range servers {
		t := ooklaTarget(s.Scheme, s.Host, fmt.Sprintf("%s (%s)", s.Sponsor, s.Name))
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		ms, _, err := measureLatency(pctx, c, t, 3)
		cancel()
		if err == nil && ms < bestMs {
			best, bestMs = t, ms
		}
	}
	if bestMs == math.MaxFloat64 {
		return speedTarget{}, errors.New("no speedtest.net server answered")
	}
	return best, nil
}

var (
	stStreams    = 8
	stDuration   = 10 * time.Second
	stSampleTick = 250 * time.Millisecond
	stDownChunk  = 25 * 1000 * 1000 // per request; streams re-request until time is up
	stUpChunk    = 8 * 1000 * 1000
)

type speedResult struct {
	PingMs       float64 `json:"ping_ms"`
	JitterMs     float64 `json:"jitter_ms"`
	DownloadMbps float64 `json:"download_mbps"`
	UploadMbps   float64 `json:"upload_mbps"`
	Server       string  `json:"server"`
	Provider     string  `json:"provider"` // speedtest.net
	Timestamp    int64   `json:"timestamp"`
}

func speedClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = stStreams * 2
	tr.MaxConnsPerHost = 0
	tr.ForceAttemptHTTP2 = false // one TCP connection per stream, like a real test
	tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	tr.TLSClientConfig = &tls.Config{NextProtos: []string{"http/1.1"}}
	tr.DisableCompression = true
	return &http.Client{Transport: tr}
}

// measureLatency: warm the connection, then TTFB of n tiny requests.
// Like OpenSpeedTest/Ookla: the minimum is the ping (the others include
// queueing), jitter is the mean change between consecutive samples.
func measureLatency(ctx context.Context, c *http.Client, t speedTarget, n int) (ping, jitter float64, err error) {
	var samples []float64
	for i := 0; i < n+1; i++ {
		var wrote, first time.Time
		trace := &httptrace.ClientTrace{
			WroteRequest:         func(httptrace.WroteRequestInfo) { wrote = time.Now() },
			GotFirstResponseByte: func() { first = time.Now() },
		}
		req, _ := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, t.PingURL, nil)
		resp, e := c.Do(req)
		if e != nil {
			err = e
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			return 0, 0, errRateLimited
		}
		if i == 0 || wrote.IsZero() || first.IsZero() {
			continue // first request pays for DNS + TCP + TLS
		}
		ms := float64(first.Sub(wrote).Microseconds()) / 1000
		// Cloudflare reports its own processing time; it isn't network latency.
		if d := serverTimingMs(resp.Header.Get("Server-Timing")); d > 0 && d < ms {
			ms -= d
		}
		samples = append(samples, ms)
	}
	if len(samples) == 0 {
		if err == nil {
			err = errors.New("no latency samples")
		}
		return 0, 0, err
	}
	var diffs float64
	for i := 1; i < len(samples); i++ {
		diffs += math.Abs(samples[i] - samples[i-1])
	}
	if len(samples) > 1 {
		jitter = diffs / float64(len(samples)-1)
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	return sorted[0], jitter, nil
}

func serverTimingMs(h string) float64 {
	// e.g. "cfRequestDuration;dur=1.23"
	i := strings.Index(h, "dur=")
	if i < 0 {
		return 0
	}
	v := h[i+4:]
	if j := strings.IndexAny(v, ",; "); j >= 0 {
		v = v[:j]
	}
	d, _ := strconv.ParseFloat(v, 64)
	return d
}

// steadyMbps runs `worker` on stStreams goroutines for stDuration and
// samples the throughput of every 250 ms slice. The result is computed the
// way speedtest.net does it: drop the slowest 30% of slices (TCP ramp-up,
// stalls) and the fastest 10% (bursts), average the rest. Nothing caps it -
// the streams re-request until time is up, however fast the line is.
func steadyMbps(ctx context.Context, worker func(ctx context.Context, count *int64) int) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, stDuration)
	defer cancel()
	var count int64
	var badStatus int32
	var wg sync.WaitGroup
	for i := 0; i < stStreams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code := worker(ctx, &count); code != 0 {
				atomic.StoreInt32(&badStatus, int32(code))
			}
		}()
	}

	var slices []float64
	lastN, lastT := int64(0), time.Now()
	tick := time.NewTicker(stSampleTick)
	defer tick.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case now := <-tick.C:
			n := atomic.LoadInt64(&count)
			if d := now.Sub(lastT).Seconds(); d > 0 {
				slices = append(slices, float64(n-lastN)*8/d/1e6)
			}
			lastN, lastT = n, now
			live := math.Round(trimmedMean(slices)*10) / 10
			setProgress(func(p *speedProgress) { p.LiveMbps = live })
		}
	}
	total := atomic.LoadInt64(&count)
	cancel()
	wg.Wait()

	if code := atomic.LoadInt32(&badStatus); code == http.StatusTooManyRequests && total < 5*1000*1000 {
		return 0, errRateLimited
	}
	if total == 0 {
		if code := atomic.LoadInt32(&badStatus); code != 0 {
			return 0, fmt.Errorf("the test server answered HTTP %d", code)
		}
		return 0, errors.New("no data transferred")
	}
	return trimmedMean(slices), nil
}

// trimmedMean: mean of the samples without the slowest 30% and fastest 10%.
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

type countingWriter struct{ n *int64 }

func (w countingWriter) Write(p []byte) (int, error) {
	atomic.AddInt64(w.n, int64(len(p)))
	return len(p), nil
}

// Workers return the HTTP status that stopped them early (0 = ran to time).
func downloadWorker(c *http.Client, t speedTarget) func(context.Context, *int64) int {
	return func(ctx context.Context, count *int64) int {
		for ctx.Err() == nil {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, t.DownURL(stDownChunk), nil)
			resp, err := c.Do(req)
			if err != nil {
				return 0
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return resp.StatusCode
			}
			io.Copy(countingWriter{count}, resp.Body)
			resp.Body.Close()
		}
		return 0
	}
}

// countingReader counts bytes as the HTTP client actually sends them.
type countingReader struct {
	data []byte
	off  int
	n    *int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	k := copy(p, r.data[r.off:])
	r.off += k
	atomic.AddInt64(r.n, int64(k))
	return k, nil
}

func uploadWorker(c *http.Client, t speedTarget, payload []byte) func(context.Context, *int64) int {
	return func(ctx context.Context, count *int64) int {
		for ctx.Err() == nil {
			body := &countingReader{data: payload, n: count}
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, t.UpURL, body)
			req.ContentLength = int64(len(payload))
			req.Header.Set("Content-Type", "application/octet-stream")
			resp, err := c.Do(req)
			if err != nil {
				return 0
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return resp.StatusCode
			}
		}
		return 0
	}
}

// One test at a time: parallel runs would split the line between them.
var speedTestMu sync.Mutex

// speedProgress is what the widget polls while a test runs in the
// background, so it can show live numbers instead of a 20 s spinner.
type speedProgress struct {
	Running  bool         `json:"running"`
	Phase    string       `json:"phase"` // ping | download | upload | done | error
	LiveMbps float64      `json:"live_mbps"`
	Result   *speedResult `json:"result,omitempty"`
	Error    string       `json:"error,omitempty"`
}

var (
	progressMu sync.Mutex
	progress   speedProgress
)

func setProgress(f func(p *speedProgress)) {
	progressMu.Lock()
	f(&progress)
	progressMu.Unlock()
}

func currentProgress() speedProgress {
	progressMu.Lock()
	defer progressMu.Unlock()
	return progress
}

// errRateLimited: test servers may answer 429 after several
// back-to-back runs.
var errRateLimited = errors.New("the test server is rate-limiting speed tests from this IP - wait a few minutes and try again")

func runSpeedTest(ctx context.Context) (speedResult, error) {
	if !speedTestMu.TryLock() {
		return speedResult{}, errors.New("a speed test is already running")
	}
	defer speedTestMu.Unlock()
	setProgress(func(p *speedProgress) { *p = speedProgress{Running: true, Phase: "ping"} })
	res, err := runSpeedTestLocked(ctx)
	setProgress(func(p *speedProgress) {
		p.Running, p.LiveMbps = false, 0
		if err != nil {
			p.Phase, p.Error = "error", err.Error()
		} else {
			p.Phase, p.Result = "done", &res
		}
	})
	return res, err
}

func runSpeedTestLocked(ctx context.Context) (speedResult, error) {
	c := speedClient()
	defer c.CloseIdleConnections()

	target, err := nearestOokla(ctx, c)
	if err != nil {
		return speedResult{}, fmt.Errorf("no speedtest.net server available: %w", err)
	}
	provider := "speedtest.net"
	ping, jitter, err := measureLatency(ctx, c, target, 10)
	if errors.Is(err, errRateLimited) {
		return speedResult{}, err
	}
	if err != nil {
		return speedResult{}, fmt.Errorf("cannot reach %s: %w", target.Name, err)
	}
	setProgress(func(p *speedProgress) {
		p.Phase = "download"
		p.Result = &speedResult{PingMs: math.Round(ping*10) / 10, JitterMs: math.Round(jitter*10) / 10, Server: target.Name, Provider: provider}
	})
	down, err := steadyMbps(ctx, downloadWorker(c, target))
	if err != nil {
		return speedResult{}, fmt.Errorf("download test: %w", err)
	}
	setProgress(func(p *speedProgress) {
		p.Phase = "upload"
		p.LiveMbps = 0
		p.Result.DownloadMbps = math.Round(down*10) / 10
	})
	// Random payload so no compression anywhere on the path inflates it.
	payload := make([]byte, stUpChunk)
	rand.Read(payload)
	up, err := steadyMbps(ctx, uploadWorker(c, target, payload))
	if err != nil {
		return speedResult{}, fmt.Errorf("upload test: %w", err)
	}
	round := func(v float64) float64 { return math.Round(v*10) / 10 }
	return speedResult{
		PingMs:       round(ping),
		JitterMs:     round(jitter),
		DownloadMbps: round(down),
		UploadMbps:   round(up),
		Server:       target.Name,
		Provider:     provider,
		Timestamp:    time.Now().Unix(),
	}, nil
}

// StartSpeedTest runs the test in the background; the widget polls
// SpeedTestStatus for live numbers.
func StartSpeedTest() error {
	if currentProgress().Running {
		return errors.New("a speed test is already running")
	}
	setProgress(func(p *speedProgress) { *p = speedProgress{Running: true, Phase: "ping"} })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		runSpeedTest(ctx)
	}()
	return nil
}
