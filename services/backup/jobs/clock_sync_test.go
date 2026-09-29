package jobs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func withClockFakes(t *testing.T, adj func(*unix.Timex) (int, error), flag string, tdc func() ([]byte, error)) {
	t.Helper()
	oa, of, ot := adjtimexFn, timesyncFlag, timedatectlFn
	t.Cleanup(func() { adjtimexFn, timesyncFlag, timedatectlFn = oa, of, ot })
	adjtimexFn, timesyncFlag, timedatectlFn = adj, flag, tdc
}

var denied = func(*unix.Timex) (int, error) { return 0, unix.EPERM }

// Under ProtectClock=true adjtimex is denied; timesyncd's flag file says
// the clock is synced.
func TestClockSyncedWhenAdjtimexDeniedAndTimesyncdFlag(t *testing.T) {
	flag := filepath.Join(t.TempDir(), "synchronized")
	if err := os.WriteFile(flag, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	withClockFakes(t, denied, flag, func() ([]byte, error) { return nil, errors.New("not called") })
	if !kernelTimeSynced() {
		t.Fatal("want synced from timesyncd's flag")
	}
}

// chrony/ntpd: no flag file, timedated reports it.
func TestClockSyncedFromTimedatectl(t *testing.T) {
	withClockFakes(t, denied, "/nonexistent", func() ([]byte, error) { return []byte("yes\n"), nil })
	if !kernelTimeSynced() {
		t.Fatal("want synced from timedatectl")
	}
	withClockFakes(t, denied, "/nonexistent", func() ([]byte, error) { return []byte("no\n"), nil })
	if kernelTimeSynced() {
		t.Fatal("want not synced")
	}
}

// adjtimex works (no sandbox): it decides.
func TestClockSyncedFromAdjtimex(t *testing.T) {
	withClockFakes(t, func(tx *unix.Timex) (int, error) { tx.Status = unix.STA_UNSYNC; return 0, nil }, "/nonexistent", func() ([]byte, error) { return []byte("yes"), nil })
	if kernelTimeSynced() {
		t.Fatal("STA_UNSYNC must mean not synced")
	}
}
