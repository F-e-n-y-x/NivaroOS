package v1

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/cloudspeed"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/gin-gonic/gin"
	"github.com/rclone/rclone/fs"
	"go.uber.org/zap"
)

// Account speed test: a background job per account. POST starts it (or
// returns the one already running), GET polls progress / the result,
// DELETE cancels. A run takes ~30-60 s - far too long for one request -
// and the engine (pkg/cloudspeed) always removes its temporary file.

const speedJobTimeout = 5 * time.Minute

type speedJob struct {
	mu         sync.Mutex
	running    bool
	progress   cloudspeed.Progress
	startedAt  time.Time
	finishedAt time.Time
	result     *cloudspeed.Result
	err        string
	cancelled  bool
	cancel     context.CancelFunc
}

type speedJobView struct {
	Running    bool               `json:"running"`
	Phase      string             `json:"phase,omitempty"`
	LiveMbps   float64            `json:"live_mbps"`
	Bytes      int64              `json:"bytes"`
	ElapsedSec float64            `json:"elapsed_s"`
	StartedAt  *time.Time         `json:"started_at,omitempty"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
	Result     *cloudspeed.Result `json:"result,omitempty"`
	Error      string             `json:"error,omitempty"`
	Cancelled  bool               `json:"cancelled,omitempty"`
}

var (
	speedJobsMu sync.Mutex
	speedJobs   = map[string]*speedJob{}
)

func (j *speedJob) view() speedJobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := speedJobView{Running: j.running, Result: j.result, Error: j.err, Cancelled: j.cancelled}
	if j.running {
		v.Phase, v.LiveMbps, v.Bytes = j.progress.Phase, j.progress.LiveMbps, j.progress.Bytes
	}
	if !j.startedAt.IsZero() {
		st := j.startedAt
		v.StartedAt = &st
		end := time.Now()
		if !j.finishedAt.IsZero() {
			fin := j.finishedAt
			v.FinishedAt = &fin
			end = fin
		}
		v.ElapsedSec = float64(end.Sub(st).Milliseconds()) / 1000
	}
	return v
}

// speedTestRemote is the rclone remote spec the test runs against. For
// Drive, deletes normally go to the trash; the test file is ours and
// worthless, so remove it for good instead of leaving 100+ MB in the
// owner's trash every run.
func speedTestRemote(name, typ string) string {
	if typ == "drive" {
		return name + ",use_trash=false:"
	}
	return name + ":"
}

func speedTestOptions(name, typ string) cloudspeed.Options {
	opt := cloudspeed.Options{UploadStreams: 1}
	if typ == "terabox" {
		opt.UploadStreams = 5
		if v, err := strconv.Atoi(service.MyService.Storage().GetAttributeValueByName(name, "upload_threads")); err == nil && v > 0 {
			opt.UploadStreams = v
		}
	}
	return opt
}

func speedAccount(c *gin.Context) (name, typ string, ok bool) {
	name = c.Param("name")
	typ = service.MyService.Storage().GetAttributeValueByName(name, "type")
	if typ == "" {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "no such account"})
		return "", "", false
	}
	return name, typ, true
}

// PostCloudAccountSpeedTest starts a speed test for the account in the
// background and returns its state; poll GetCloudAccountSpeedTest.
func PostCloudAccountSpeedTest(c *gin.Context) {
	name, typ, ok := speedAccount(c)
	if !ok {
		return
	}
	speedJobsMu.Lock()
	job := speedJobs[name]
	if job != nil && job.view().Running {
		speedJobsMu.Unlock()
		c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: job.view()})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), speedJobTimeout)
	job = &speedJob{running: true, startedAt: time.Now(), cancel: cancel, progress: cloudspeed.Progress{Phase: "prepare"}}
	speedJobs[name] = job
	speedJobsMu.Unlock()

	go func() {
		defer cancel()
		res, err := runSpeedJob(ctx, job, name, typ)
		job.mu.Lock()
		defer job.mu.Unlock()
		job.running = false
		job.finishedAt = time.Now()
		job.result = res
		switch {
		case err == nil:
		case errors.Is(err, context.Canceled):
			job.cancelled = true
			job.err = "cancelled"
		case errors.Is(err, context.DeadlineExceeded):
			job.err = "the test took too long and was stopped"
		default:
			job.err = err.Error()
			logger.Error("speedtest failed", zap.String("name", name), zap.Error(err))
		}
	}()
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: job.view()})
}

func runSpeedJob(ctx context.Context, job *speedJob, name, typ string) (*cloudspeed.Result, error) {
	f, err := fs.NewFs(ctx, speedTestRemote(name, typ))
	if err != nil {
		return nil, err
	}
	opt := speedTestOptions(name, typ)
	opt.Progress = func(p cloudspeed.Progress) {
		job.mu.Lock()
		job.progress = p
		job.mu.Unlock()
	}
	res, err := cloudspeed.Run(ctx, f, opt)
	if err != nil {
		return nil, err
	}
	if typ == "terabox" && service.MyService.Storage().GetAttributeValueByName(name, "premium") != "true" {
		res.Notes = append(res.Notes, "TeraBox limits download speed for free accounts through its API (roughly 8 Mbps per file, far less for big files); uploads are not limited.")
	}
	return res, nil
}

// GetCloudAccountSpeedTest returns the account's running or last test
// (data is null if none has run since the service started).
func GetCloudAccountSpeedTest(c *gin.Context) {
	name, _, ok := speedAccount(c)
	if !ok {
		return
	}
	speedJobsMu.Lock()
	job := speedJobs[name]
	speedJobsMu.Unlock()
	var data any
	if job != nil {
		data = job.view()
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

// DeleteCloudAccountSpeedTest cancels a running test; its temporary file
// is still removed.
func DeleteCloudAccountSpeedTest(c *gin.Context) {
	name, _, ok := speedAccount(c)
	if !ok {
		return
	}
	speedJobsMu.Lock()
	job := speedJobs[name]
	speedJobsMu.Unlock()
	if job == nil {
		c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: nil})
		return
	}
	job.mu.Lock()
	if job.running && job.cancel != nil {
		job.cancel()
	}
	job.mu.Unlock()
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: job.view()})
}
