package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/internal/fixturetest"
)

// fixtureTypes maps a testdata/engine file name (without ".json" and any
// "__variant" suffix) to the type it holds. Every fixture file must be
// listed, so a new one can't go unchecked.
var fixtureTypes = map[string]func() interface{}{
	"health":                func() interface{} { return new(Health) },
	"locations":             func() interface{} { return new([]Location) },
	"volumes":               func() interface{} { return new([]Volume) },
	"resolve_request":       func() interface{} { return new(ResolveRequest) },
	"resolve_response":      func() interface{} { return new(Resolved) },
	"resolve_path_request":  func() interface{} { return new(ResolvePathRequest) },
	"resolve_path_response": func() interface{} { return new(ResolvePathResult) },
	"browse_request":        func() interface{} { return new(BrowseRequest) },
	"browse_response":       func() interface{} { return new(BrowseResult) },
	"precheck_request":      func() interface{} { return new(PrecheckRequest) },
	"precheck_response":     func() interface{} { return new(PrecheckResult) },
	"versions_request":      func() interface{} { return new(VersionsRequest) },
	"versions_response":     func() interface{} { return new([]Version) },
	"download_request":      func() interface{} { return new(DownloadRequest) },
	"job_request":           func() interface{} { return new(JobRequest) },
	"job_status":            func() interface{} { return new(JobStatus) },
	"log_lines":             func() interface{} { return new([]LogLine) },
	"events":                func() interface{} { return new([]Event) },
	"preview_items":         func() interface{} { return new([]PreviewItem) },
	"marker":                func() interface{} { return new(Marker) },
}

func TestEngineFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "testdata", "engine", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures in testdata/engine")
	}
	seen := map[string]bool{}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		base, _, _ := strings.Cut(name, "__")
		mk, ok := fixtureTypes[base]
		if !ok {
			t.Errorf("%s: no type registered for fixture %q", f, base)
			continue
		}
		seen[base] = true
		t.Run(name, func(t *testing.T) { fixturetest.CheckFile(t, f, mk()) })
	}
	for base := range fixtureTypes {
		if !seen[base] {
			t.Errorf("type for %q is registered but has no fixture", base)
		}
	}
}

// Every op a job request can carry has a fixture for it or is covered by
// the plan/purge fixtures' shape.
func TestJobOpsHaveFixtures(t *testing.T) {
	covered := map[Op]bool{}
	files, _ := filepath.Glob(filepath.Join("..", "testdata", "engine", "job_request__*.json"))
	for _, f := range files {
		var req JobRequest
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := fixturetest.CheckJSON(raw, &req); err != nil {
			t.Fatal(err)
		}
		covered[req.Op] = true
	}
	for _, op := range []Op{OpCopy, OpSync, OpArchive, OpPlan, OpPurgeVersions, OpRestore} {
		if !covered[op] {
			t.Errorf("no job_request fixture for op %q", op)
		}
	}
}

func TestSMBCredsNeverPrintPassword(t *testing.T) {
	c := SMBCreds{Host: "nas", Share: "backup", User: "u", Password: "hunter2"}
	for _, s := range []string{fmt.Sprint(c), fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c), fmt.Sprint(&c)} {
		if strings.Contains(s, "hunter2") {
			t.Fatalf("password leaked: %s", s)
		}
	}
}

func TestCodeOf(t *testing.T) {
	cause := errors.New("disk gone")
	wrapped := fmt.Errorf("run: %w", Errorf(CodeDestOffline, "mount %d: %w", 203, cause))
	cases := []struct {
		err  error
		want ErrorCode
	}{
		{nil, ""},
		{wrapped, CodeDestOffline},
		{context.Canceled, CodeCancelledByUser},
		{fmt.Errorf("x: %w", context.DeadlineExceeded), CodeMaxDuration},
		{errors.New("boom"), CodeIOError},
	}
	for _, c := range cases {
		if got := CodeOf(c.err); got != c.want {
			t.Errorf("CodeOf(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	if !errors.Is(wrapped, cause) {
		t.Error("Errorf lost the %w cause")
	}
	if got := wrapped.Error(); got != "run: dest_offline: mount 203: disk gone" {
		t.Errorf("message = %q", got)
	}
}

func TestEngineCodesUnique(t *testing.T) {
	seen := map[ErrorCode]bool{}
	for _, c := range EngineCodes {
		if seen[c] {
			t.Errorf("duplicate code %q", c)
		}
		seen[c] = true
	}
}

func TestRcloneVersionPinned(t *testing.T) {
	// Must equal services/local-storage/go.mod's rclone: both read the
	// same rclone.conf.
	if got := RcloneVersion(); got != "v1.75.1" {
		t.Errorf("linked rclone %s, want v1.75.1 (keep in step with local-storage)", got)
	}
}
