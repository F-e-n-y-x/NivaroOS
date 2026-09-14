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
	for i := 0; i < len(temp.Item); i++ {
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
			err := file.CopyDir(v.From, temp.To, temp.Style)
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
			err := file.CopyDir(v.From, temp.To, temp.Style)
			if err != nil {
				continue
			}
		} else {
			continue
		}

	}
	temp.Finished = true
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

// file move or copy and send notify
func CheckFileStatus() {
	for {
		snapshot := OpStrArrSnapshot()
		if len(snapshot) == 0 {
			return
		}
		for _, v := range snapshot {
			var total int64 = 0
			item, ok := FileQueue.Load(v)
			if !ok {
				continue
			}
			temp := item.(model.FileOperate)
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
			FileQueue.Store(v, temp)
		}
		time.Sleep(time.Second * 3)
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
