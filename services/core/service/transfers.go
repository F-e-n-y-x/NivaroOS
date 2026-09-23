package service

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/core/common"
	"github.com/F-e-n-y-x/NivaroOS/services/core/model/notify"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/transfer"
	"go.uber.org/zap"
)

// Transfers is the single copy/move/delete engine for the Files app (and
// the mobile app's /v1/batch/task calls). See service/transfer.
var Transfers *transfer.Manager

// InitTransfers starts the engine; call once at startup after MyService.
func InitTransfers(dataDir string) {
	Transfers = transfer.NewManager(transfer.Options{
		StatePath: filepath.Join(dataDir, "transfer-jobs.json"),
		OnChange:  publishTransferJobs,
		Remote:    companionRemote{},
	})
}

// TransferEvent is one job in the nivaroos:file:operate event. The first
// block is the legacy shape every existing client already reads; the rest
// is the engine's full snapshot.
type TransferEvent struct {
	File notify.File  `json:"-"`
	Job  transfer.Job `json:"-"`
}

// recentWindow: finished jobs are included in live events for this long,
// so every client sees each job's final state at least once. The full
// history is always available from GET /v1/batch/tasks.
const recentWindow = 20 * time.Second

func publishTransferJobs(jobs []transfer.Job) {
	if MyService == nil {
		return
	}
	now := time.Now()
	list := make([]TransferEvent, 0, len(jobs))
	for _, j := range jobs {
		if j.State.Terminal() && j.FinishedAt != nil && now.Sub(*j.FinishedAt) > recentWindow {
			continue
		}
		list = append(list, ToTransferEvent(j))
	}
	payload, _ := json.Marshal(notify.NotifyModel{State: "NORMAL", Data: list})
	msg := map[string]string{"file_operate": string(payload)}
	resp, err := MyService.MessageBus().PublishEventWithResponse(context.Background(), common.SERVICENAME, "nivaroos:file:operate", msg)
	if err != nil {
		logger.Error("publish file operate event", zap.Error(err))
		return
	}
	if resp.StatusCode() != http.StatusOK {
		logger.Error("publish file operate event", zap.String("status", resp.Status()))
	}
}

// ToTransferEvent fills the legacy fields from a job snapshot.
func ToTransferEvent(j transfer.Job) TransferEvent {
	f := notify.File{
		Id:             j.ID,
		To:             j.Dest,
		Type:           string(j.Kind),
		ProcessedSize:  j.BytesDone,
		TotalSize:      j.BytesTotal,
		Speed:          j.Speed,
		ProcessingPath: j.Current,
		Finished:       j.State.Terminal(),
		Cancelled:      j.State == transfer.StateCancelled,
	}
	switch j.State {
	case transfer.StateQueued, transfer.StateScanning:
		f.Status = "CALCULATING"
		f.TotalSize = -1
	case transfer.StateRunning:
		f.Status = "PROCESSING"
	case transfer.StateSyncing:
		f.Status = "SYNCING"
	case transfer.StateDone:
		f.Status = "FINISHED"
	case transfer.StateDoneWithErrors:
		f.Status = "FINISHED_WITH_ERRORS"
	case transfer.StateCancelled:
		f.Status = "CANCELLED"
	default:
		f.Status = "FAILED"
	}
	return TransferEvent{File: f, Job: j}
}

// MarshalJSON flattens both halves into one object; legacy fields win on
// the (identical-meaning) keys both define: id, speed.
func (e TransferEvent) MarshalJSON() ([]byte, error) {
	a, err := json.Marshal(e.File)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(e.Job)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	var legacy map[string]interface{}
	if err := json.Unmarshal(a, &legacy); err != nil {
		return nil, err
	}
	for k, v := range legacy {
		m[k] = v
	}
	return json.Marshal(m)
}

// companionRemote lets the engine move data to and from companion devices
// through CompanionHandler, with real per-item errors.
type companionRemote struct{}

func (companionRemote) Handles(p string) bool {
	return CompanionHandler != nil && CompanionHandler.IsCompanionPath(p)
}

func (companionRemote) Size(ctx context.Context, p string) (int64, error) {
	if CompanionHandler.IsCompanionPath(p) {
		return CompanionHandler.GetSize(ctx, p)
	}
	return dirSize(p)
}

func (companionRemote) Delete(ctx context.Context, p string) error {
	return CompanionHandler.DeleteCompanionPath(ctx, p)
}

func (companionRemote) Transfer(ctx context.Context, src, destDir string, conflict transfer.Conflict, progress func(int64)) error {
	style := "overwrite"
	if conflict == transfer.ConflictSkip || conflict == transfer.ConflictResume {
		style = "skip"
	}
	// CompanionHandler reports progress as bytes-so-far per file; the engine
	// wants deltas.
	var last int64
	delta := func(p int64) {
		if p < last {
			last = 0
		}
		progress(p - last)
		last = p
	}
	fromC := CompanionHandler.IsCompanionPath(src)
	toC := CompanionHandler.IsCompanionPath(destDir)
	switch {
	case fromC && toC:
		tmp, err := os.MkdirTemp("", "nivaroos-comp-xfer-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		if err := CompanionHandler.CopyFromCompanion(ctx, src, tmp+"/", "overwrite", delta); err != nil {
			return err
		}
		last = 0
		return CompanionHandler.CopyToCompanion(ctx, filepath.Join(tmp, filepath.Base(src)), destDir, style, func(int64) {})
	case fromC:
		return CompanionHandler.CopyFromCompanion(ctx, src, destDir+"/", style, delta)
	default:
		return CompanionHandler.CopyToCompanion(ctx, src, destDir, style, delta)
	}
}

func dirSize(p string) (int64, error) {
	var total int64
	err := filepath.Walk(p, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
