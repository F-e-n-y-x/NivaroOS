package engine

import "github.com/rclone/rclone/fs"

// RcloneVersion is the linked rclone release (Health.Rclone). It comes
// from the library itself, so it can't drift from go.mod - which pins the
// same version as services/local-storage/go.mod, because both load the
// same rclone.conf and must agree on its format.
func RcloneVersion() string { return fs.VersionTag }
