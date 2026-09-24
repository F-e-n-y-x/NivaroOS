package service

// Async storage jobs. Creating (partition+format) or formatting a disk can
// take longer than the UI's 60s request timeout; the backend kept going
// after the UI had already reported failure. POST/PUT /v1/storage now start
// a job, answer 202 {job_id} right away, and report progress on the message
// bus and at GET /v1/storage/jobs/:id.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/common"
	"go.uber.org/zap"
)

const (
	JobStateRunning = "running"
	JobStateDone    = "done"
	JobStateError   = "error"

	jobRetention = time.Hour
	jobMaxKept   = 100
)

type StorageJob struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // "create" | "format"
	Path       string `json:"path"`
	State      string `json:"state"` // running | done | error
	Step       string `json:"step"`  // current step, human readable
	Message    string `json:"message,omitempty"`
	MountPoint string `json:"mount_point,omitempty"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at,omitempty"`
}

// JobPublisher is called on every state/step change (event is one of the
// common.StorageJobEvent* names).
type JobPublisher func(event string, job StorageJob)

type JobStore struct {
	mu      sync.Mutex
	jobs    map[string]*StorageJob
	publish JobPublisher
	now     func() time.Time

	// events go out in order on one goroutine, so a slow message bus never
	// blocks the job or the 202 response
	events   chan jobEvent
	pubStart sync.Once
}

type jobEvent struct {
	name string
	job  StorageJob
}

func NewJobStore(publish JobPublisher) *JobStore {
	return &JobStore{jobs: map[string]*StorageJob{}, publish: publish, now: time.Now, events: make(chan jobEvent, 256)}
}

func newJobID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
	}
	return hex.EncodeToString(b)
}

// JobRun does the work. progress(step) reports a step; the returned mount
// point (if any) is recorded on success.
type JobRun func(progress func(step string)) (mountPoint string, err error)

// Start registers a job and runs it in the background. onFinish (may be nil)
// runs after run returns, before the final event - used to release the disk lock.
func (s *JobStore) Start(kind, path string, run JobRun, onFinish func()) StorageJob {
	job := &StorageJob{ID: newJobID(), Kind: kind, Path: path, State: JobStateRunning, Step: "queued", StartedAt: s.now().Unix()}
	s.mu.Lock()
	s.pruneLocked()
	s.jobs[job.ID] = job
	snapshot := *job
	s.mu.Unlock()
	s.emit(common.StorageJobEventProgress, snapshot)

	go func() {
		var mp string
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("storage job panicked", zap.Any("panic", r), zap.String("job", job.ID))
					err = &GuardError{Msg: "internal error"}
				}
			}()
			mp, err = run(func(step string) {
				s.update(job.ID, func(j *StorageJob) { j.Step = step }, common.StorageJobEventProgress)
			})
		}()
		if onFinish != nil {
			onFinish()
		}
		if err != nil {
			s.update(job.ID, func(j *StorageJob) {
				j.State, j.Message, j.FinishedAt = JobStateError, err.Error(), s.now().Unix()
			}, common.StorageJobEventError)
			return
		}
		s.update(job.ID, func(j *StorageJob) {
			j.State, j.Step, j.MountPoint, j.FinishedAt = JobStateDone, "done", mp, s.now().Unix()
		}, common.StorageJobEventEnd)
	}()
	return snapshot
}

func (s *JobStore) update(id string, fn func(*StorageJob), event string) {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	fn(j)
	snapshot := *j
	s.mu.Unlock()
	s.emit(event, snapshot)
}

func (s *JobStore) emit(event string, j StorageJob) {
	if s.publish == nil {
		return
	}
	s.pubStart.Do(func() {
		go func() {
			for e := range s.events {
				s.publish(e.name, e.job)
			}
		}()
	})
	select {
	case s.events <- jobEvent{event, j}:
	default:
		logger.Error("storage job event queue full - dropping event", zap.String("event", event), zap.String("job", j.ID))
	}
}

func (s *JobStore) Get(id string) (StorageJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return StorageJob{}, false
	}
	return *j, true
}

// pruneLocked drops finished jobs older than jobRetention and caps the store.
func (s *JobStore) pruneLocked() {
	cutoff := s.now().Add(-jobRetention).Unix()
	for id, j := range s.jobs {
		if j.State != JobStateRunning && j.FinishedAt > 0 && j.FinishedAt < cutoff {
			delete(s.jobs, id)
		}
	}
	if len(s.jobs) < jobMaxKept {
		return
	}
	var finished []*StorageJob
	for _, j := range s.jobs {
		if j.State != JobStateRunning {
			finished = append(finished, j)
		}
	}
	sort.Slice(finished, func(a, b int) bool { return finished[a].FinishedAt < finished[b].FinishedAt })
	for _, j := range finished {
		if len(s.jobs) < jobMaxKept {
			break
		}
		delete(s.jobs, j.ID)
	}
}

// JobEventProperties: message-bus properties for a job event.
func JobEventProperties(j StorageJob) map[string]string {
	return map[string]string{
		"local-storage:job_id":      j.ID,
		"local-storage:job_kind":    j.Kind,
		"local-storage:path":        j.Path,
		"local-storage:state":       j.State,
		"local-storage:step":        j.Step,
		"local-storage:message":     j.Message,
		"local-storage:mount_point": j.MountPoint,
	}
}

func publishJobEvent(event string, j StorageJob) {
	if MyService == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := MyService.MessageBus().PublishEventWithResponse(ctx, common.ServiceName, event, JobEventProperties(j))
	if err != nil {
		logger.Error("failed to publish storage job event", zap.Error(err), zap.String("event", event))
		return
	}
	if resp.StatusCode() != http.StatusOK {
		logger.Error("failed to publish storage job event", zap.String("status", resp.Status()), zap.String("event", event))
	}
}

// StorageJobs is the process-wide job store.
var StorageJobs = NewJobStore(publishJobEvent)
