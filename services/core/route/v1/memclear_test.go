package v1

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const sampleMeminfo = `MemTotal:       16303412 kB
MemFree:         1203400 kB
MemAvailable:    9876544 kB
Buffers:          204800 kB
Cached:          8000000 kB
SwapCached:        10240 kB
SwapTotal:       4194300 kB
SwapFree:        3145724 kB
HugePages_Total:       0
`

func TestParseMeminfo(t *testing.T) {
	s, err := parseMeminfo([]byte(sampleMeminfo))
	if err != nil {
		t.Fatal(err)
	}
	want := memSnapshot{
		MemTotal: 16303412 << 10, MemFree: 1203400 << 10, MemAvailable: 9876544 << 10,
		Buffers: 204800 << 10, Cached: 8000000 << 10,
		SwapTotal: 4194300 << 10, SwapFree: 3145724 << 10, SwapUsed: (4194300 - 3145724) << 10,
	}
	if s != want {
		t.Fatalf("got %+v\nwant %+v", s, want)
	}
}

func TestParseMeminfoOldKernelAndGarbage(t *testing.T) {
	s, err := parseMeminfo([]byte("junk line\nMemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 10 kB\nCached: x kB\nCached: 50 kB\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.MemAvailable != 160<<10 {
		t.Errorf("MemAvailable fallback = %d, want %d", s.MemAvailable, 160<<10)
	}
	if s.SwapUsed != 0 {
		t.Errorf("SwapUsed = %d", s.SwapUsed)
	}
	if _, err := parseMeminfo([]byte("MemFree: 1 kB\n")); err == nil {
		t.Error("accepted meminfo without MemTotal")
	}
}

func TestSwapReclaimSafety(t *testing.T) {
	const gib = 1 << 30
	cases := []struct {
		name    string
		s       memSnapshot
		allowed bool
	}{
		{"no swap in use", memSnapshot{MemTotal: 8 * gib, MemAvailable: 100 << 20}, true},
		{"plenty of room", memSnapshot{MemTotal: 16 * gib, MemAvailable: 10 * gib, SwapUsed: 1 * gib}, true},
		{"more swap than RAM available", memSnapshot{MemTotal: 16 * gib, MemAvailable: 1 * gib, SwapUsed: 2 * gib}, false},
		// 16 GiB RAM: margin is 1.6 GiB.
		{"fits but under the margin", memSnapshot{MemTotal: 16 * gib, MemAvailable: 3 * gib, SwapUsed: 2 * gib}, false},
		// 2 GiB RAM: margin is the 512 MiB floor.
		{"small box, floor margin", memSnapshot{MemTotal: 2 * gib, MemAvailable: 1 * gib, SwapUsed: 600 << 20}, false},
		{"small box, just enough", memSnapshot{MemTotal: 2 * gib, MemAvailable: 1 * gib, SwapUsed: 500 << 20}, true},
	}
	for _, c := range cases {
		err := checkSwapReclaim(c.s)
		if (err == nil) != c.allowed {
			t.Errorf("%s: err=%v, allowed want %v", c.name, err, c.allowed)
		}
		if err != nil && !errors.Is(err, errSwapUnsafe) {
			t.Errorf("%s: not errSwapUnsafe: %v", c.name, err)
		}
	}
}

// fakeHost records what the clearer would do to the machine.
type fakeHost struct {
	mu       sync.Mutex
	meminfo  []string // served in order; the last one repeats
	reads    int
	writes   []string
	commands []string
	clock    time.Time
	block    chan struct{} // if set, "sync" waits on it
	started  chan struct{}
}

func (h *fakeHost) env() memClearEnv {
	return memClearEnv{
		readFile: func(path string) ([]byte, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if path != memInfoPath {
				return nil, fmt.Errorf("unexpected read %s", path)
			}
			i := h.reads
			if i >= len(h.meminfo) {
				i = len(h.meminfo) - 1
			}
			h.reads++
			return []byte(h.meminfo[i]), nil
		},
		writeFile: func(path string, data []byte) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.writes = append(h.writes, path+"="+strings.TrimSpace(string(data)))
			return nil
		},
		run: func(_ context.Context, name string, args ...string) error {
			if name == "sync" && h.block != nil {
				close(h.started)
				<-h.block
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			h.commands = append(h.commands, strings.TrimSpace(name+" "+strings.Join(args, " ")))
			return nil
		},
		now: func() time.Time {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.clock
		},
	}
}

func (h *fakeHost) advance(d time.Duration) {
	h.mu.Lock()
	h.clock = h.clock.Add(d)
	h.mu.Unlock()
}

func meminfoKB(total, free, avail, cached, swapTotal, swapFree uint64) string {
	return fmt.Sprintf("MemTotal: %d kB\nMemFree: %d kB\nMemAvailable: %d kB\nBuffers: 0 kB\nCached: %d kB\nSwapTotal: %d kB\nSwapFree: %d kB\n",
		total, free, avail, cached, swapTotal, swapFree)
}

const gibKB = 1 << 20

func TestMemClearDropsCachesAndReportsFreed(t *testing.T) {
	h := &fakeHost{
		clock: time.Unix(1000, 0),
		meminfo: []string{
			meminfoKB(16*gibKB, 1*gibKB, 10*gibKB, 8*gibKB, 0, 0),
			meminfoKB(16*gibKB, 3*gibKB, 10*gibKB, 6*gibKB, 0, 0),
		},
	}
	m := &memClearer{env: h.env(), interval: 30 * time.Second}
	res, err := m.Clear(context.Background(), memClearOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Freed != 2<<30 {
		t.Errorf("freed = %d, want 2 GiB", res.Freed)
	}
	if res.Before.MemFree != 1<<30 || res.After.MemFree != 3<<30 {
		t.Errorf("before/after = %d/%d", res.Before.MemFree, res.After.MemFree)
	}
	if got := strings.Join(h.commands, ","); got != "sync" {
		t.Errorf("commands = %s", got)
	}
	if got := strings.Join(h.writes, ","); got != memDropCachesPath+"=3,"+memCompactPath+"=1" {
		t.Errorf("writes = %s", got)
	}
	if res.SwapReclaimed || !res.Compacted || res.Level != 3 {
		t.Errorf("result = %+v", res)
	}
}

func TestMemClearLevelAndNoCompact(t *testing.T) {
	h := &fakeHost{clock: time.Unix(1000, 0), meminfo: []string{meminfoKB(8*gibKB, gibKB, 4*gibKB, 2*gibKB, 0, 0)}}
	m := &memClearer{env: h.env(), interval: 30 * time.Second}
	no := false
	if _, err := m.Clear(context.Background(), memClearOptions{Level: 1, Compact: &no}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.writes, ","); got != memDropCachesPath+"=1" {
		t.Errorf("writes = %s", got)
	}
	if _, err := m.Clear(context.Background(), memClearOptions{Level: 2}); err == nil {
		t.Error("accepted level 2")
	}
}

func TestMemClearRateLimit(t *testing.T) {
	h := &fakeHost{clock: time.Unix(1000, 0), meminfo: []string{meminfoKB(8*gibKB, gibKB, 4*gibKB, 2*gibKB, 0, 0)}}
	m := &memClearer{env: h.env(), interval: 30 * time.Second}
	if _, err := m.Clear(context.Background(), memClearOptions{}); err != nil {
		t.Fatal(err)
	}
	h.advance(10 * time.Second)
	_, err := m.Clear(context.Background(), memClearOptions{})
	var mce *memClearError
	if !errors.As(err, &mce) || mce.Status != http.StatusTooManyRequests || mce.RetryAfter != 20 {
		t.Fatalf("second clear: %v (%+v)", err, mce)
	}
	if n := strings.Count(strings.Join(h.writes, ","), memDropCachesPath); n != 1 {
		t.Errorf("drop_caches written %d times", n)
	}
	h.advance(20 * time.Second)
	if _, err := m.Clear(context.Background(), memClearOptions{}); err != nil {
		t.Fatalf("after the interval: %v", err)
	}
}

func TestMemClearSerialised(t *testing.T) {
	h := &fakeHost{clock: time.Unix(1000, 0), meminfo: []string{meminfoKB(8*gibKB, gibKB, 4*gibKB, 2*gibKB, 0, 0)},
		block: make(chan struct{}), started: make(chan struct{})}
	m := &memClearer{env: h.env(), interval: 0}
	done := make(chan error)
	go func() { _, err := m.Clear(context.Background(), memClearOptions{}); done <- err }()
	<-h.started
	_, err := m.Clear(context.Background(), memClearOptions{})
	var mce *memClearError
	if !errors.As(err, &mce) || mce.Status != http.StatusConflict {
		t.Fatalf("concurrent clear: %v", err)
	}
	close(h.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMemClearSwapRefusedTouchesNothing(t *testing.T) {
	// 2 GiB in swap, 2.5 GiB available on 16 GiB: under the 1.6 GiB margin.
	h := &fakeHost{clock: time.Unix(1000, 0), meminfo: []string{meminfoKB(16*gibKB, gibKB/2, 5*gibKB/2, gibKB, 4*gibKB, 2*gibKB)}}
	m := &memClearer{env: h.env(), interval: 30 * time.Second}
	_, err := m.Clear(context.Background(), memClearOptions{Swap: true})
	var mce *memClearError
	if !errors.As(err, &mce) || mce.Status != http.StatusConflict || !strings.Contains(mce.Msg, "swap was left alone") {
		t.Fatalf("got %v", err)
	}
	if len(h.writes) != 0 || len(h.commands) != 0 {
		t.Errorf("touched the host: writes=%v commands=%v", h.writes, h.commands)
	}
	// A refusal doesn't use up the rate limit.
	if _, err := m.Clear(context.Background(), memClearOptions{}); err != nil {
		t.Errorf("plain clear after refusal: %v", err)
	}
}

func TestMemClearSwapReclaimed(t *testing.T) {
	h := &fakeHost{clock: time.Unix(1000, 0), meminfo: []string{meminfoKB(16*gibKB, gibKB, 10*gibKB, 8*gibKB, 4*gibKB, 3*gibKB)}}
	m := &memClearer{env: h.env(), interval: 30 * time.Second}
	res, err := m.Clear(context.Background(), memClearOptions{Swap: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.commands, ","); got != "sync,swapoff -a,swapon -a" {
		t.Errorf("commands = %s", got)
	}
	if !res.SwapReclaimed {
		t.Error("swap not reported reclaimed")
	}
}

func TestTokenClaimsAdmin(t *testing.T) {
	tok := func(payload string) string {
		return "x." + strings.TrimRight(b64(payload), "=") + ".y"
	}
	for payload, want := range map[string]bool{
		`{"username":"a","id":1}`:            true,
		`{"username":"a","role":"admin"}`:    true,
		`{"username":"a","roles":["owner"]}`: true,
		`{"username":"a","role":"member"}`:   false,
		`{"username":"a","is_admin":false}`:  false,
	} {
		if got := tokenClaimsAdmin(tok(payload)); got != want {
			t.Errorf("%s: %v, want %v", payload, got, want)
		}
	}
	if tokenClaimsAdmin("garbage") {
		t.Error("garbage token is admin")
	}
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
