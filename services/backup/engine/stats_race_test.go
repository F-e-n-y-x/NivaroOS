package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/rclone/rclone/fs/accounting"
)

// rclone's RemoteStats reads errors/lastError outside its lock, so polling
// a job while its transfers fail was a data race (and could deref a nil
// lastError). liveStats must only use rclone's locked getters.
func TestLiveStatsWhileRcloneCountsErrors(t *testing.T) {
	j := &job{group: "bk_live_stats_race", state: JobRunning}
	sg := accounting.StatsGroup(context.Background(), j.group)
	t.Cleanup(func() { deleteStatsGroup(j.group) })
	e := &Engine{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			sg.Error(fmt.Errorf("transfer %d failed", i))
		}
	}()
	for i := 0; i < 500; i++ {
		e.liveStats(j)
	}
	<-done
	if st := e.liveStats(j); st.Errors != 500 {
		t.Fatalf("errors = %d, want 500", st.Errors)
	}
}
