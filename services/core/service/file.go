/*
 * @Author: LinkLeong link@icewhale.com
 * @Date: 2021-12-20 14:15:46
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-07-04 16:18:23
 * @FilePath: /CasaOS/service/file.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/file"
	"github.com/moby/sys/mountinfo"
	"go.uber.org/zap"
)

var FileQueue sync.Map

// opStrArr is the FIFO queue of in-flight file-operation task IDs (only
// ever the front entry is actively processed - see ExecOpFile). It's
// mutated from multiple goroutines (the HTTP handler that enqueues a new
// task, the background notify loop, and now the immediate completion ping
// this file also triggers), so every access goes through the accessors
// below rather than touching the slice directly - a previous version of
// this code exposed the raw slice as an exported package var, read and
// written from three different files with no synchronization at all.
var opStrArr []string
var opStrArrMu sync.Mutex

// OpStrArrSnapshot returns a copy safe to range over without holding the lock.
func OpStrArrSnapshot() []string {
	opStrArrMu.Lock()
	defer opStrArrMu.Unlock()
	out := make([]string, len(opStrArr))
	copy(out, opStrArr)
	return out
}

func OpStrArrLen() int {
	opStrArrMu.Lock()
	defer opStrArrMu.Unlock()
	return len(opStrArr)
}

// OpStrArrPush enqueues a new task id and reports whether it's the only one
// in the queue (the caller uses that to decide whether to start the
// processing/notify goroutines, which should only ever run one at a time).
func OpStrArrPush(id string) (isOnly bool) {
	opStrArrMu.Lock()
	defer opStrArrMu.Unlock()
	opStrArr = append(opStrArr, id)
	return len(opStrArr) == 1
}

// OpStrArrPopFront removes the front entry - always safe to call unconditionally
// since only the front entry is ever the one found "Finished".
func OpStrArrPopFront() {
	opStrArrMu.Lock()
	defer opStrArrMu.Unlock()
	if len(opStrArr) > 0 {
		opStrArr = opStrArr[1:]
	}
}

// OpStrArrReset replaces the whole queue (used by "cancel all").
func OpStrArrReset(newList []string) {
	opStrArrMu.Lock()
	defer opStrArrMu.Unlock()
	opStrArr = newList
}

// OpStrArrRemove drops one specific task id (used by "cancel this one").
func OpStrArrRemove(id string) {
	opStrArrMu.Lock()
	defer opStrArrMu.Unlock()
	tempList := make([]string, 0, len(opStrArr))
	for _, v := range opStrArr {
		if v != id {
			tempList = append(tempList, v)
		}
	}
	opStrArr = tempList
}

// OpStrArrFront returns the current front task id, if any.
func OpStrArrFront() (string, bool) {
	opStrArrMu.Lock()
	defer opStrArrMu.Unlock()
	if len(opStrArr) == 0 {
		return "", false
	}
	return opStrArr[0], true
}

// OpCancelFuncs holds the context.CancelFunc for each task currently being
// actively copied by FileOperate, keyed by task id - lets a cancel request
// actually stop the in-flight copy (see CopyDirCtx), not just stop tracking
// it while it keeps writing to disk in the background.
var OpCancelFuncs sync.Map

// CancelOperateTask stops task `id`. If it's the one actively being copied
// right now (the front of the queue), this only signals its context - the
// running FileOperate goroutine notices at the next file boundary, marks
// itself Cancelled+Finished, and the existing "task finished" path in
// notify.go (shared with every other task ending, successful or not) is
// what actually removes it from the queue. Racing a direct removal here
// against that goroutine's own in-flight write to FileQueue would let the
// stale entry get resurrected, so a task with no running goroutine (still
// queued behind the front one) is the only case removed directly here.
func CancelOperateTask(id string) {
	if cf, ok := OpCancelFuncs.Load(id); ok {
		if cancel, ok2 := cf.(context.CancelFunc); ok2 {
			cancel()
		}
	}
	if front, ok := OpStrArrFront(); ok && front == id {
		return
	}
	FileQueue.Delete(id)
	OpStrArrRemove(id)
}

// CancelAllOperateTasks best-effort stops every queued/running task (the
// active one gets the same file-boundary cancellation as CancelOperateTask)
// and clears the queue outright - mirrors this package's pre-existing
// "cancel all" behavior of replacing the whole map, just also signalling
// cancellation first instead of only ever having stopped tracking.
func CancelAllOperateTasks() {
	for _, id := range OpStrArrSnapshot() {
		if cf, ok := OpCancelFuncs.Load(id); ok {
			if cancel, ok2 := cf.(context.CancelFunc); ok2 {
				cancel()
			}
		}
	}
	FileQueue = sync.Map{}
	OpStrArrReset([]string{})
}

type reader struct {
	ctx context.Context
	r   io.Reader
}

// NewReader wraps an io.Reader to handle context cancellation.
//
// Context state is checked BEFORE every Read.
func NewReader(ctx context.Context, r io.Reader) io.Reader {
	if r, ok := r.(*reader); ok && ctx == r.ctx {
		return r
	}
	return &reader{ctx: ctx, r: r}
}

func (r *reader) Read(p []byte) (n int, err error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.r.Read(p)
	}
}

type writer struct {
	ctx context.Context
	w   io.Writer
}

type copier struct {
	writer
}

func NewWriter(ctx context.Context, w io.Writer) io.Writer {
	if w, ok := w.(*copier); ok && ctx == w.ctx {
		return w
	}
	return &copier{writer{ctx: ctx, w: w}}
}

// Write implements io.Writer, but with context awareness.
func (w *writer) Write(p []byte) (n int, err error) {
	select {
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	default:
		return w.w.Write(p)
	}
}

func FileOperate(k string) {
	list, ok := FileQueue.Load(k)
	if !ok {
		return
	}

	temp := list.(model.FileOperate)
	if temp.ProcessedSize > 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	OpCancelFuncs.Store(k, cancel)
	defer func() {
		cancel()
		OpCancelFuncs.Delete(k)
	}()

	cancelled := false
	for i := 0; i < len(temp.Item); i++ {
		if ctx.Err() != nil {
			cancelled = true
			break
		}
		v := temp.Item[i]
		if temp.Type == "move" {
			lastPath := v.From[strings.LastIndex(v.From, "/")+1:]
			if !file.CheckNotExist(temp.To + "/" + lastPath) {
				if temp.Style == "skip" {
					temp.Item[i].Finished = true
					continue
				} else {
					os.RemoveAll(temp.To + "/" + lastPath)
				}
			}
			err := file.CopyDirCtx(ctx, v.From, temp.To, temp.Style)
			if errors.Is(err, context.Canceled) {
				cancelled = true
				break
			}
			if err == nil {
				err = os.RemoveAll(v.From)
				if err != nil {
					logger.Error("file move error", zap.Any("err", err))
					err = file.MoveFile(v.From, temp.To+"/"+lastPath)
					if err != nil {
						logger.Error("MoveFile error", zap.Any("err", err))
						continue
					}

				}
			}

		} else if temp.Type == "copy" {
			err := file.CopyDirCtx(ctx, v.From, temp.To, temp.Style)
			if errors.Is(err, context.Canceled) {
				cancelled = true
				break
			}
			if err != nil {
				continue
			}
		} else {
			continue
		}

	}
	temp.Finished = true
	temp.Cancelled = cancelled
	FileQueue.Store(k, temp)
	// CheckFileStatus/SendFileOperateNotify's own loop only samples progress
	// every 3s - without this, a small/fast operation that finishes well
	// inside that window sits in a silent gap where it's already done on
	// disk but the UI hasn't been told yet, making it look like the paste/
	// upload never completed. Pinging immediately on actual completion
	// closes that gap; the poll loop still owns in-progress percentage
	// updates for slower operations.
	go MyService.Notify().SendFileOperateNotify(true)
}

func ExecOpFile() {
	front, ok := OpStrArrFront()
	if !ok {
		return
	}
	go FileOperate(front)
}

// ComputeOperateSizes walks each item's source path to find its real size,
// then patches that into the stored task. Moved off PostOperateFileOrDir's
// request path on purpose: computing every item's size (recursively, for a
// folder) used to run synchronously before that handler could respond at
// all, which is exactly why a large paste showed nothing in the UI for
// several seconds - not perceived lag, a real blocking stall. Copying
// itself never needed a size upfront (FileOperate/CheckFileStatus derive
// real progress from what's actually landed at the destination), only the
// percentage shown to the user did, so this runs after the task is already
// queued and (via ExecOpFile) already starting to copy.
//
// Call this only for a task whose Item[].Size/TotalSize were stored as -1
// (unknown/still calculating) by the caller.
func ComputeOperateSizes(uid string) {
	item, ok := FileQueue.Load(uid)
	if !ok {
		return
	}
	temp := item.(model.FileOperate)

	sizes := make([]int64, len(temp.Item))
	var total int64 = 0
	for i := range temp.Item {
		size, err := file.GetFileOrDirSize(temp.Item[i].From)
		if err != nil {
			size = 0
		}
		sizes[i] = size
		total += size
	}

	// Re-load rather than storing `temp` back directly - FileOperate() or
	// CheckFileStatus() may have already progressed ProcessedSize/Finished
	// on this same task while this was walking the source tree, and this
	// should only patch in the now-known sizes, never clobber that.
	latest, ok := FileQueue.Load(uid)
	if !ok {
		// Already finished (and removed from the queue) before sizes came
		// back - e.g. an empty/near-empty paste, or a cancel. Nothing left
		// to patch.
		return
	}
	cur := latest.(model.FileOperate)
	cur.TotalSize = total
	for i := range cur.Item {
		if i < len(sizes) {
			cur.Item[i].Size = sizes[i]
		}
	}
	FileQueue.Store(uid, cur)
}

// checkFileStatusPollInterval controls both how often CheckFileStatus
// re-measures destination size and how often that progress is broadcast to
// the UI. It used to be 3s with nothing broadcasting in between at all (see
// the removed-call note below) - even after fixing that, 3s is still far
// coarser than a real copy dialog: most everyday copies (a few hundred MB on
// local SSD/same-host storage) land well inside a single 3s window, so the
// UI would still only ever see "Preparing" then "Done" with nothing shown
// between them. 400ms keeps multiple samples in flight for those common
// cases while staying cheap - each tick's cost is a recursive stat-based
// size walk of every unfinished item's destination path, not a full re-copy.
const checkFileStatusPollInterval = 400 * time.Millisecond

// file move or copy and send notify
func CheckFileStatus() {
	lastTick := time.Now()
	for {
		snapshot := OpStrArrSnapshot()
		if len(snapshot) == 0 {
			return
		}
		now := time.Now()
		elapsed := now.Sub(lastTick).Seconds()
		lastTick = now
		for _, v := range snapshot {
			var total int64 = 0
			item, ok := FileQueue.Load(v)
			if !ok {
				continue
			}
			temp := item.(model.FileOperate)
			prevProcessed := temp.ProcessedSize
			for i := 0; i < len(temp.Item); i++ {
				if !temp.Item[i].Finished {
					size, err := file.GetFileOrDirSize(temp.To + "/" + filepath.Base(temp.Item[i].From))
					if err != nil {
						continue
					}
					temp.Item[i].ProcessedSize = size
					if size == temp.Item[i].Size {
						temp.Item[i].Finished = true
					}
					total += size
				} else {
					total += temp.Item[i].ProcessedSize
				}
			}
			temp.ProcessedSize = total
			if !temp.Finished && elapsed > 0 && total > prevProcessed {
				temp.Speed = int64(float64(total-prevProcessed) / elapsed)
			} else {
				temp.Speed = 0
			}
			FileQueue.Store(v, temp)
		}
		// This loop was only ever updating FileQueue for itself to read back
		// later - nothing broadcast these intermediate samples to the UI, so
		// the only notify events a client ever received were the initial
		// "queued, size unknown" one (PostOperateFileOrDir) and the final
		// "finished" one (FileOperate) - i.e. exactly the "stuck on
		// Preparing, then jumps straight to Done" symptom, regardless of how
		// long the operation actually took. Broadcasting the freshly-sampled
		// progress/speed here on every tick is what actually makes this a
		// live progress bar.
		go MyService.Notify().SendFileOperateNotify(true)
		time.Sleep(checkFileStatusPollInterval)
	}
}
func IsMounted(path string) bool {
	mounted, _ := mountinfo.Mounted(path)
	if mounted {
		return true
	}
	connections := MyService.Connections().GetConnectionsList()
	for _, v := range connections {
		if v.MountPoint == path {
			return true
		}
	}
	return false
}
