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

type CompanionIOHandler interface {
	IsCompanionPath(path string) bool
	GetSize(ctx context.Context, path string) (int64, error)
	CopyFromCompanion(ctx context.Context, companionSrc, dst, style string, onProgress func(processed int64)) error
	CopyToCompanion(ctx context.Context, src, companionDst, style string, onProgress func(processed int64)) error
	DeleteCompanionPath(ctx context.Context, path string) error
}

var (
	CompanionHandler CompanionIOHandler
	runningFileOps   sync.Map
)

func updateOperateItemProgress(taskId string, itemIndex int, processed int64) {
	item, ok := FileQueue.Load(taskId)
	if !ok {
		return
	}
	cur := item.(model.FileOperate)
	if itemIndex < len(cur.Item) {
		cur.Item[itemIndex].ProcessedSize = processed
		var total int64 = 0
		for _, it := range cur.Item {
			total += it.ProcessedSize
		}
		cur.ProcessedSize = total
		FileQueue.Store(taskId, cur)
	}
}

func FileOperate(k string) {
	if _, loaded := runningFileOps.LoadOrStore(k, true); loaded {
		return
	}
	defer runningFileOps.Delete(k)

	list, ok := FileQueue.Load(k)
	if !ok {
		return
	}

	temp := list.(model.FileOperate)

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
		isFromCompanion := CompanionHandler != nil && CompanionHandler.IsCompanionPath(v.From)
		isToCompanion := CompanionHandler != nil && CompanionHandler.IsCompanionPath(temp.To)

		var opErr error
		if isFromCompanion && isToCompanion {
			// Companion -> Companion: stage into a temp directory preserving original filename
			tmpDir, err := os.MkdirTemp("", "nivaroos-comp-xfer-*")
			if err == nil {
				origName := filepath.Base(v.From)
				stagedPath := filepath.Join(tmpDir, origName)
				opErr = CompanionHandler.CopyFromCompanion(ctx, v.From, stagedPath, "overwrite", func(p int64) {
					updateOperateItemProgress(k, i, p)
				})
				if opErr == nil {
					opErr = CompanionHandler.CopyToCompanion(ctx, stagedPath, temp.To, temp.Style, func(p int64) {
						updateOperateItemProgress(k, i, p)
					})
				}
				_ = os.RemoveAll(tmpDir)
			} else {
				opErr = err
			}
			if opErr == nil && temp.Type == "move" {
				_ = CompanionHandler.DeleteCompanionPath(ctx, v.From)
			}
		} else if isFromCompanion {
			// Companion -> Local / Cloud
			opErr = CompanionHandler.CopyFromCompanion(ctx, v.From, temp.To, temp.Style, func(p int64) {
				updateOperateItemProgress(k, i, p)
			})
			if opErr == nil && temp.Type == "move" {
				_ = CompanionHandler.DeleteCompanionPath(ctx, v.From)
			}
		} else if isToCompanion {
			// Local / Cloud -> Companion
			opErr = CompanionHandler.CopyToCompanion(ctx, v.From, temp.To, temp.Style, func(p int64) {
				updateOperateItemProgress(k, i, p)
			})
			if opErr == nil && temp.Type == "move" {
				_ = os.RemoveAll(v.From)
			}
		} else {
			// Standard local <-> local, local <-> cloud, cloud <-> cloud
			lastPath := filepath.Base(v.From)
			destItemPath := filepath.Join(temp.To, lastPath)
			if temp.Type == "move" {
				if !file.CheckNotExist(destItemPath) {
					if temp.Style == "skip" {
						temp.Item[i].Finished = true
						continue
					} else {
						if dinfo, statErr := os.Stat(destItemPath); statErr == nil && !dinfo.IsDir() {
							_ = os.Remove(destItemPath)
						}
					}
				}
				// Try fast filesystem rename first (instantaneous on same disk or rclone remote)
				if renameErr := os.Rename(v.From, destItemPath); renameErr == nil {
					temp.Item[i].Finished = true
					continue
				}
				opErr = file.CopyDirCtx(ctx, v.From, temp.To, temp.Style)
				if errors.Is(opErr, context.Canceled) {
					cancelled = true
					break
				}
				if opErr == nil {
					removeErr := os.RemoveAll(v.From)
					if removeErr != nil {
						logger.Error("file move RemoveAll error, attempting MoveFile", zap.Error(removeErr))
						moveErr := file.MoveFile(v.From, destItemPath)
						if moveErr != nil {
							logger.Error("MoveFile error", zap.Error(moveErr))
							continue
						}
					}
				}
			} else if temp.Type == "copy" {
				if filepath.Clean(temp.To) == filepath.Clean(filepath.Dir(v.From)) {
					// Duplicate in same folder: create non-conflicting name (e.g. file (copy).ext)
					dupPath := file.GenerateDuplicatePath(temp.To, lastPath)
					srcInfo, statErr := os.Stat(v.From)
					if statErr == nil {
						if srcInfo.IsDir() {
							opErr = file.CopyDirCtx(ctx, v.From, filepath.Dir(dupPath), temp.Style)
							_ = os.Rename(filepath.Join(temp.To, lastPath), dupPath)
						} else {
							opErr = file.CopySingleFile(v.From, dupPath, "overwrite")
						}
					} else {
						opErr = statErr
					}
				} else {
					opErr = file.CopyDirCtx(ctx, v.From, temp.To, temp.Style)
				}
				if errors.Is(opErr, context.Canceled) {
					cancelled = true
					break
				}
				if opErr != nil {
					logger.Error("file copy error", zap.Error(opErr))
					continue
				}
			} else {
				continue
			}
		}

		if errors.Is(opErr, context.Canceled) {
			cancelled = true
			break
		}

		if opErr == nil {
			temp.Item[i].Finished = true
			if temp.Item[i].Size > 0 {
				temp.Item[i].ProcessedSize = temp.Item[i].Size
			}
		}
	}
	temp.Finished = true
	temp.Cancelled = cancelled
	FileQueue.Store(k, temp)
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
		var size int64 = 0
		var err error
		if CompanionHandler != nil && CompanionHandler.IsCompanionPath(temp.Item[i].From) {
			size, err = CompanionHandler.GetSize(context.Background(), temp.Item[i].From)
		} else {
			size, err = file.GetFileOrDirSize(temp.Item[i].From)
		}
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
// the UI.
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
					if CompanionHandler != nil && (CompanionHandler.IsCompanionPath(temp.To) || CompanionHandler.IsCompanionPath(temp.Item[i].From)) {
						total += temp.Item[i].ProcessedSize
						continue
					}
					targetPath := filepath.Join(temp.To, filepath.Base(temp.Item[i].From))
					size, err := file.GetFileOrDirSize(targetPath)
					if err != nil {
						total += temp.Item[i].ProcessedSize
						continue
					}
					temp.Item[i].ProcessedSize = size
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
