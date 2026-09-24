package engine

import (
	"runtime"

	"golang.org/x/sys/unix"
)

// ioprio_set(2) constants (not in x/sys/unix).
const (
	ioprioWhoProcess = 1
	ioprioClassIdle  = 3
	ioprioClassShift = 13
)

// runLowPriority runs fn on a dedicated OS thread with nice 10 and the
// idle I/O class (spec §4 Options.LowPriority). rclone's own worker
// goroutines run on other threads, so this is best effort, which is why
// low-priority jobs also use fewer transfers. The thread is never
// returned to the Go scheduler: the goroutine ends while still locked,
// so the runtime discards the thread instead of reusing a slowed one.
func runLowPriority(fn func() (Result, error)) (Result, error) {
	type out struct {
		res Result
		err error
	}
	ch := make(chan out, 1)
	go func() {
		runtime.LockOSThread()
		// Deliberately no UnlockOSThread (see above).
		tid := unix.Gettid()
		_ = unix.Setpriority(unix.PRIO_PROCESS, tid, 10)
		_, _, _ = unix.Syscall(unix.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(tid), ioprioClassIdle<<ioprioClassShift)
		var o out
		func() {
			defer func() {
				if r := recover(); r != nil {
					o.err = Errorf(CodeInternal, "the engine hit a bug: %v", r)
				}
			}()
			o.res, o.err = fn()
		}()
		ch <- o
	}()
	o := <-ch
	return o.res, o.err
}
