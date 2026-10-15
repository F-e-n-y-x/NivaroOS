package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
)

// rclone's sync skips its deletes after errors only if fs.CountError
// counts them, which needs accounting.Start (engine.New).
func TestEngineCountsRcloneErrors(t *testing.T) {
	newTestSys(t).engine()
	ctx := accounting.WithStatsGroup(context.Background(), "bk_count_errors")
	_ = fs.CountError(ctx, errors.New("read error"))
	if !accounting.Stats(ctx).Errored() {
		t.Fatal("fs.CountError did not count: accounting was not started")
	}
}
