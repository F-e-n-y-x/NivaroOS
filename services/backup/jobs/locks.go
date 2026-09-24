package jobs

import "context"

// Lock kinds for BusyChecker. Hook targets are locked by app name and VM
// name, the same identifiers Scheduled Tasks use in target_id/target_name.
const (
	LockApp = "app" // a container app (app-management compose name)
	LockVM  = "vm"  // a libvirt domain name
)

// BusyChecker is the Backup <-> Scheduled Tasks lock contract (spec §8.1,
// §17, frozen): a backup hook must not stop an app or VM a Scheduled Task
// is working on, and vice versa.
//
// In the module layout the two live in different processes:
//   - Backup's own queue implements BusyChecker for its hook locks and
//     serves it as GET /v1/backup/busy?kind=&target= (see backup-api.json)
//     so core can ask, if it ever needs to.
//   - The Scheduled Tasks side is a BusyChecker the jobs package builds
//     on core's GET /v1/schedules over loopback: a task whose type is
//     "vm"/"container", whose target matches, and whose last_status is
//     "running" is busy. Hooks call WaitFree on it before acting.
type BusyChecker interface {
	// IsBusy reports whether kind/target is being worked on right now.
	IsBusy(kind, target string) bool
	// WaitFree blocks until kind/target is free or ctx ends (the caller
	// bounds ctx to 30 min and maps a timeout to ErrBusy). It returns
	// ctx.Err() when ctx ends first.
	WaitFree(ctx context.Context, kind, target string) error
}
