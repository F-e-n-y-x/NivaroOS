package v1

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	modelCommon "github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/labstack/echo/v4"
)

// "Free up memory" (POST /v1/sys/memory/clear): flushes dirty pages, asks
// the kernel to drop its page cache (and dentries/inodes), optionally
// compacts memory, and - only when it is clearly safe - pulls swap back
// into RAM. Linux refills the cache by itself, so this is a "right now"
// action; the UI says so. Core runs as root (nivaroos.service has no
// User= and no sandboxing), so it writes /proc/sys/vm directly.

// memSnapshot is the part of /proc/meminfo the action reports, in bytes.
type memSnapshot struct {
	MemTotal     uint64 `json:"mem_total"`
	MemFree      uint64 `json:"mem_free"`
	MemAvailable uint64 `json:"mem_available"`
	Buffers      uint64 `json:"buffers"`
	Cached       uint64 `json:"cached"`
	SwapTotal    uint64 `json:"swap_total"`
	SwapFree     uint64 `json:"swap_free"`
	SwapUsed     uint64 `json:"swap_used"`
}

// parseMeminfo reads /proc/meminfo's "Key:   123 kB" lines. MemTotal and
// MemFree must be there; the rest default to 0 (MemAvailable falls back
// to MemFree+Buffers+Cached on kernels older than 3.14).
func parseMeminfo(data []byte) (memSnapshot, error) {
	var s memSnapshot
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		fields := strings.Fields(line[colon+1:])
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 1 && strings.EqualFold(fields[1], "kB") {
			n *= 1024
		}
		var dst *uint64
		switch key {
		case "MemTotal":
			dst = &s.MemTotal
		case "MemFree":
			dst = &s.MemFree
		case "MemAvailable":
			dst = &s.MemAvailable
		case "Buffers":
			dst = &s.Buffers
		case "Cached":
			dst = &s.Cached
		case "SwapTotal":
			dst = &s.SwapTotal
		case "SwapFree":
			dst = &s.SwapFree
		}
		if dst != nil {
			*dst = n
			seen[key] = true
		}
	}
	if !seen["MemTotal"] || !seen["MemFree"] {
		return memSnapshot{}, errors.New("meminfo: MemTotal/MemFree missing")
	}
	if !seen["MemAvailable"] {
		s.MemAvailable = s.MemFree + s.Buffers + s.Cached
	}
	if s.SwapTotal > s.SwapFree {
		s.SwapUsed = s.SwapTotal - s.SwapFree
	}
	return s, nil
}

// swapReclaimMargin is the RAM that must stay available after swap is
// read back in: the larger of 512 MiB and 10% of RAM.
func swapReclaimMargin(s memSnapshot) uint64 {
	m := s.MemTotal / 10
	if m < 512<<20 {
		m = 512 << 20
	}
	return m
}

// errSwapUnsafe: swapoff would have to fit everything in swap into RAM;
// without room to spare the kernel starts killing processes instead.
var errSwapUnsafe = errors.New("swap unsafe")

// checkSwapReclaim reports whether swap can be emptied: nothing to do
// when none is used, and refused unless MemAvailable exceeds SwapUsed by
// swapReclaimMargin.
func checkSwapReclaim(s memSnapshot) error {
	if s.SwapUsed == 0 {
		return nil
	}
	if s.MemAvailable < s.SwapUsed || s.MemAvailable-s.SwapUsed < swapReclaimMargin(s) {
		return fmt.Errorf("%w: %s in swap needs %s of available memory to move back safely, only %s is available - swap was left alone",
			errSwapUnsafe, humanBytes(s.SwapUsed), humanBytes(s.SwapUsed+swapReclaimMargin(s)), humanBytes(s.MemAvailable))
	}
	return nil
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// memClearEnv is everything the action touches, so tests never drop the
// real caches or touch swap.
type memClearEnv struct {
	readFile  func(path string) ([]byte, error)
	writeFile func(path string, data []byte) error
	run       func(ctx context.Context, name string, args ...string) error
	now       func() time.Time
}

func realMemClearEnv() memClearEnv {
	return memClearEnv{
		readFile: os.ReadFile,
		writeFile: func(path string, data []byte) error {
			f, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			_, err = f.Write(data)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			return err
		},
		run: func(ctx context.Context, name string, args ...string) error {
			out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			if err != nil {
				msg := strings.TrimSpace(string(out))
				if msg == "" {
					return err
				}
				return fmt.Errorf("%s: %s", name, msg)
			}
			return nil
		},
		now: time.Now,
	}
}

type memClearOptions struct {
	// Level 1 drops the page cache only; 3 (the default) also dentries
	// and inodes.
	Level int `json:"level"`
	// Compact defaults to true.
	Compact *bool `json:"compact"`
	Swap    bool  `json:"swap"`
}

type memClearResult struct {
	Before        memSnapshot `json:"before"`
	After         memSnapshot `json:"after"`
	Freed         uint64      `json:"freed"` // growth of MemFree
	Level         int         `json:"level"`
	Compacted     bool        `json:"compacted"`
	SwapReclaimed bool        `json:"swap_reclaimed"`
	// Note: something that didn't stop the run (compaction unsupported).
	Note       string `json:"note,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// memClearError carries the HTTP status to answer with.
type memClearError struct {
	Status     int
	Msg        string
	RetryAfter int
}

func (e *memClearError) Error() string { return e.Msg }

// memClearer runs one clear at a time and at most once per interval.
type memClearer struct {
	env      memClearEnv
	interval time.Duration

	busy sync.Mutex // held while a clear runs
	mu   sync.Mutex // guards last
	last time.Time
}

const (
	memDropCachesPath = "/proc/sys/vm/drop_caches"
	memCompactPath    = "/proc/sys/vm/compact_memory"
	memInfoPath       = "/proc/meminfo"
)

func (m *memClearer) snapshot() (memSnapshot, error) {
	b, err := m.env.readFile(memInfoPath)
	if err != nil {
		return memSnapshot{}, err
	}
	return parseMeminfo(b)
}

// reserve claims the next slot, or says how long until there is one.
func (m *memClearer) reserve() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.env.now()
	if !m.last.IsZero() {
		if wait := m.interval - now.Sub(m.last); wait > 0 {
			secs := int((wait + time.Second - 1) / time.Second)
			return &memClearError{Status: http.StatusTooManyRequests, RetryAfter: secs,
				Msg: fmt.Sprintf("Memory was just cleared - try again in %d s", secs)}
		}
	}
	m.last = now
	return nil
}

func (m *memClearer) Clear(ctx context.Context, opt memClearOptions) (*memClearResult, error) {
	if opt.Level == 0 {
		opt.Level = 3
	}
	if opt.Level != 1 && opt.Level != 3 {
		return nil, &memClearError{Status: http.StatusBadRequest, Msg: "level must be 1 (page cache) or 3 (page cache, dentries and inodes)"}
	}
	compact := opt.Compact == nil || *opt.Compact

	if !m.busy.TryLock() {
		return nil, &memClearError{Status: http.StatusConflict, Msg: "Memory is already being cleared"}
	}
	defer m.busy.Unlock()

	before, err := m.snapshot()
	if err != nil {
		return nil, fmt.Errorf("read memory usage: %w", err)
	}
	// Refuse before touching anything, so "no" means nothing happened.
	if opt.Swap {
		if err := checkSwapReclaim(before); err != nil {
			return nil, &memClearError{Status: http.StatusConflict, Msg: strings.TrimPrefix(err.Error(), errSwapUnsafe.Error()+": ")}
		}
	}
	if err := m.reserve(); err != nil {
		return nil, err
	}

	start := m.env.now()
	res := &memClearResult{Before: before, Level: opt.Level}
	var notes []string

	// Dirty pages can't be dropped; write them out first.
	if err := m.env.run(ctx, "sync"); err != nil {
		notes = append(notes, "sync failed: "+err.Error())
	}
	if err := m.env.writeFile(memDropCachesPath, []byte(strconv.Itoa(opt.Level)+"\n")); err != nil {
		return nil, fmt.Errorf("drop caches: %w", err)
	}
	if compact {
		if err := m.env.writeFile(memCompactPath, []byte("1\n")); err != nil {
			notes = append(notes, "memory compaction isn't available on this kernel")
		} else {
			res.Compacted = true
		}
	}
	if opt.Swap && before.SwapUsed > 0 {
		// Check again with the cache gone (it only gets safer, unless
		// something grew meanwhile).
		now, err := m.snapshot()
		if err == nil {
			err = checkSwapReclaim(now)
		}
		if err != nil {
			notes = append(notes, strings.TrimPrefix(err.Error(), errSwapUnsafe.Error()+": "))
		} else {
			sctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			offErr := m.env.run(sctx, "swapoff", "-a")
			cancel()
			// Always turn swap back on, even if swapoff stopped halfway.
			onErr := m.env.run(context.Background(), "swapon", "-a")
			switch {
			case offErr != nil:
				notes = append(notes, "emptying swap failed: "+offErr.Error())
			case onErr != nil:
				notes = append(notes, "swap was emptied but turning it back on failed: "+onErr.Error())
			default:
				res.SwapReclaimed = true
			}
		}
	}

	after, err := m.snapshot()
	if err != nil {
		return nil, fmt.Errorf("read memory usage: %w", err)
	}
	res.After = after
	if after.MemFree > before.MemFree {
		res.Freed = after.MemFree - before.MemFree
	}
	res.Note = strings.Join(notes, "; ")
	res.DurationMs = m.env.now().Sub(start).Milliseconds()
	return res, nil
}

var sysMemClearer = &memClearer{env: realMemClearEnv(), interval: 30 * time.Second}

// requestIsAdmin: same rule as Backup & Sync / Download Station. Today's
// tokens carry no role - every account is the owner - so a token without
// a role claim is an admin; once a role/roles claim exists it must say
// admin/administrator/owner. Same-host automation that the JWT
// middleware let through without a token is root on this host already.
func requestIsAdmin(ctx echo.Context) bool {
	token := ctx.Request().Header.Get(echo.HeaderAuthorization)
	if token == "" {
		token = ctx.QueryParam("token")
	}
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if token == "" {
		return ctx.Get("user") == nil // skipped by LocalAutomationSkipper
	}
	return tokenClaimsAdmin(token)
}

// tokenClaimsAdmin reads the role claims of a token the JWT middleware
// has already verified.
func tokenClaimsAdmin(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims map[string]interface{}
	if json.Unmarshal(raw, &claims) != nil {
		return false
	}
	isAdmin := func(v interface{}) bool {
		s, ok := v.(string)
		s = strings.ToLower(strings.TrimSpace(s))
		return ok && (s == "admin" || s == "administrator" || s == "owner")
	}
	checked := false
	for _, key := range []string{"role", "roles"} {
		v, ok := claims[key]
		if !ok || v == nil {
			continue
		}
		checked = true
		switch t := v.(type) {
		case string:
			if isAdmin(t) {
				return true
			}
		case []interface{}:
			for _, x := range t {
				if isAdmin(x) {
					return true
				}
			}
		}
	}
	if b, ok := claims["is_admin"].(bool); ok {
		return b
	}
	return !checked
}

func memClearFail(ctx echo.Context, status int, msg string, data interface{}) error {
	return ctx.JSON(status, modelCommon.Result{Success: status, Message: msg, Data: data})
}

// PostSystemMemoryClear frees the server's RAM now.
// Body (all optional): {"level": 1|3, "compact": true, "swap": false}.
func PostSystemMemoryClear(ctx echo.Context) error {
	if !requestIsAdmin(ctx) {
		return memClearFail(ctx, http.StatusForbidden, "Freeing memory needs an administrator account", nil)
	}
	var opt memClearOptions
	if ctx.Request().ContentLength != 0 {
		if err := ctx.Bind(&opt); err != nil {
			return memClearFail(ctx, http.StatusBadRequest, "invalid body", nil)
		}
	}
	res, err := sysMemClearer.Clear(ctx.Request().Context(), opt)
	if err != nil {
		var mce *memClearError
		if errors.As(err, &mce) {
			var data interface{}
			if mce.RetryAfter > 0 {
				ctx.Response().Header().Set("Retry-After", strconv.Itoa(mce.RetryAfter))
				data = map[string]int{"retry_after": mce.RetryAfter}
			}
			return memClearFail(ctx, mce.Status, mce.Msg, data)
		}
		return memClearFail(ctx, http.StatusInternalServerError, err.Error(), nil)
	}
	return ok(ctx, res)
}
