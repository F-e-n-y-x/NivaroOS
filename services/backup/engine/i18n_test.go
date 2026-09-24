package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// enUSPath is the UI's English strings, relative to this package.
var enUSPath = filepath.Join("..", "..", "..", "ui", "src", "assets", "lang", "en_US.json")

func loadEnUS(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(enUSPath)
	if err != nil {
		t.Fatalf("reading en_US.json: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("en_US.json: %v", err)
	}
	return m
}

var placeholderRe = regexp.MustCompile(`\{([a-z_]+)\}`)

func placeholders(text string) []string {
	var out []string
	for _, m := range placeholderRe.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// TestEngineKeysInEnUS: every i18n key the engine sends exists, and its
// {placeholders} are among the args it is sent with (vue-i18n renders a
// missing key or arg raw).
func TestEngineKeysInEnUS(t *testing.T) {
	en := loadEnUS(t)
	var want []string
	for _, id := range []string{CheckSourceResolves, CheckDestResolves, CheckNotInside, CheckAllowedRoots, CheckFreeSpace,
		CheckFSQuirks, CheckMetadata, CheckSourceSentinel, CheckDestMarker} {
		want = append(want, "backup.check."+id)
	}
	for _, g := range []string{guardDelete, guardChange, guardEmptySource} {
		want = append(want, "backup.guard."+g)
	}
	for _, c := range EngineCodes {
		want = append(want, "backup.err."+string(c)+".title")
	}
	want = append(want, ReasonKeys...)
	want = append(want, "backup.ver.current", "backup.ver.recycle", "backup.ver.archive")
	// Every literal key in the engine's sources.
	files, err := filepath.Glob("*.go")
	must(t, err)
	lit := regexp.MustCompile(`"(backup\.[a-z_]+(?:\.[a-z_]+)+)"`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		must(t, err)
		for _, m := range lit.FindAllStringSubmatch(string(raw), -1) {
			want = append(want, m[1])
		}
	}
	sort.Strings(want)
	for _, k := range want {
		if _, ok := en[k]; !ok {
			t.Errorf("en_US.json has no %q", k)
		}
	}
}

// TestLogLineArgsMatchTexts runs the common ops and checks every log
// line's key and args against the English text.
func TestLogLineArgsMatchTexts(t *testing.T) {
	en := loadEnUS(t)
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "d")
	writeTree(t, root, map[string]string{"a": "1", "b": "2", "c": "3"})
	req := baseReq(OpSync, ep(src, "d"), ep(dst, "m"))
	req.Guards.DeletePct, req.Guards.ChangePct = 100, 100
	var ids []JobID
	run := func(r JobRequest, id string) JobStatus {
		st := runJob(t, e, withRun(r, id))
		ids = append(ids, st.ID)
		return st
	}
	run(req, "run_l1")
	must(t, os.Remove(filepath.Join(root, "a")))
	must(t, os.WriteFile(filepath.Join(root, "b"), []byte("22"), 0o644))
	req.FirstRun = false
	run(req, "run_l2")
	// A tripped guard.
	guarded := req
	guarded.Guards.ChangePct = 1
	must(t, os.WriteFile(filepath.Join(root, "c"), []byte("33"), 0o644))
	run(guarded, "run_l3")
	arch := archiveReq(ep(src, "d"), ep(dst, "arch"))
	run(arch, "run_l4")
	prune := req
	prune.Op, prune.Retention = OpPurgeVersions, Retention{VersionsDays: 1}
	must(t, os.MkdirAll(filepath.Join(dst.MountPoint, "m", VersionsDir, "20200101T000000Z"), 0o755))
	run(prune, "run_l5")
	rest := JobRequest{Op: OpRestore, JobID: req.JobID, Dest: req.Dest, DestFolderID: testFolderID,
		Restore: &RestoreSpec{VersionID: "current", Paths: []string{"b", "gone"}, Target: ep(src, "d"), Conflict: ConflictSkip}}
	run(rest, "run_l6")

	seen := map[string]bool{}
	for _, id := range ids {
		for _, l := range jobLog(t, e, id) {
			if l.Code == "raw" {
				if l.MsgKey != "" || l.Raw == "" {
					t.Errorf("raw line %+v", l)
				}
				continue
			}
			text, ok := en[l.MsgKey]
			if !ok {
				t.Errorf("log key %q (code %s) not in en_US.json", l.MsgKey, l.Code)
				continue
			}
			if last := l.MsgKey[strings.LastIndex(l.MsgKey, ".")+1:]; last != l.Code {
				t.Errorf("code %q for key %q", l.Code, l.MsgKey)
			}
			seen[l.Code] = true
			for _, ph := range placeholders(text) {
				v, ok := l.Args[ph]
				if !ok {
					t.Errorf("%s: text needs {%s}, args %v", l.MsgKey, ph, l.Args)
					continue
				}
				if strings.HasSuffix(ph, "_key") {
					if _, ok := en[v.(string)]; !ok {
						t.Errorf("%s: %s=%q is not in en_US.json", l.MsgKey, ph, v)
					}
				}
			}
		}
	}
	for _, code := range []string{"phase", "check", "copied", "updated", "recycled", "marker_written", "guard_tripped", "archive_done", "pruned", "skipped"} {
		if !seen[code] {
			t.Errorf("no %q line was logged by these runs", code)
		}
	}
}
