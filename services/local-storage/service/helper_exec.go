package service

import (
	"context"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/config"
)

// helperFunctions: the local-storage-helper.sh functions Go may call; the
// script's own dispatcher has the same allow-list.
var helperFunctions = map[string]bool{"do_mount": true, "USB_Start_Auto": true, "USB_Stop_Auto": true}

// HelperCommand builds `bash <ShellPath>/local-storage-helper.sh <fn> <args...>`
// - every argument its own argv entry, never a shell string.
func HelperCommand(ctx context.Context, shellPath, fn string, args ...string) *exec.Cmd {
	argv := append([]string{filepath.Join(shellPath, "local-storage-helper.sh"), fn}, args...)
	return exec.CommandContext(ctx, "bash", argv...)
}

// RunHelper runs a helper function with a 2-minute cap.
func RunHelper(fn string, args ...string) (string, error) {
	if !helperFunctions[fn] {
		return "", &InvalidDeviceError{Msg: "unknown helper function " + fn}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := HelperCommand(ctx, config.AppInfo.ShellPath, fn, args...)
	// a FUSE daemon (ntfs-3g) inheriting the output pipe must not hang us
	cmd.WaitDelay = 10 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}
