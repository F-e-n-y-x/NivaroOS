package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLog(t *testing.T, lines int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "nivaroos.log")
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "line %d some log text\n", i)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The log endpoint sent the whole file (10.7 MB, 44k lines) every time
// Settings > System opened, and the Terminal app re-fetched it every 5 s -
// the browser froze while rendering it. Only the requested tail is sent.
func TestLogTailReturnsOnlyTheLastLines(t *testing.T) {
	p := writeLog(t, 50000)
	got, err := tailLines(p, 100)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 100 {
		t.Fatalf("got %d lines, want 100", len(lines))
	}
	if lines[0] != "line 49901 some log text" || lines[99] != "line 50000 some log text" {
		t.Fatalf("wrong window: first=%q last=%q", lines[0], lines[99])
	}
}

func TestLogTailOfAShortFileIsTheWholeFile(t *testing.T) {
	p := writeLog(t, 3)
	got, _ := tailLines(p, 100)
	if got != "line 1 some log text\nline 2 some log text\nline 3 some log text\n" {
		t.Fatalf("got %q", got)
	}
}

func TestLogTailIsFastOnAHugeFile(t *testing.T) {
	p := writeLog(t, 1_000_000) // ~26 MB
	start := time.Now()
	if _, err := tailLines(p, 2000); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("tail took %v - it must not read the whole file", d)
	}
}
