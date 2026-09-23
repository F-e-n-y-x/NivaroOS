package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	client2 "github.com/docker/docker/client"
)

// UpdateJob is one container update running in the background. Updates
// ran inside the HTTP request: a large pull or a rebuild outlived the UI's
// 60 s limit and showed "failed" while it carried on.
type UpdateJob struct {
	Container  string     `json:"container"`
	Kind       string     `json:"kind"`  // registry | git | local
	State      string     `json:"state"` // running | done | failed
	Updated    bool       `json:"updated"`
	Error      string     `json:"error,omitempty"`
	Log        []string   `json:"log"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

var (
	jobsMu     sync.Mutex
	updateJobs = map[string]*UpdateJob{}
	errBusy    = errors.New("an update of this container is already running")
)

func (j *UpdateJob) logf(format string, a ...any) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	j.Log = append(j.Log, fmt.Sprintf(format, a...))
	if len(j.Log) > 2000 {
		j.Log = j.Log[len(j.Log)-2000:]
	}
}

func (j *UpdateJob) snapshot() UpdateJob {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	c := *j
	c.Log = append([]string(nil), j.Log...)
	return c
}

// UpdateStatus is the latest update job for a container (nil if none).
func (m *ContainerUpdateManager) UpdateStatus(ctx context.Context, nameOrID string) *UpdateJob {
	name := containerName(ctx, nameOrID)
	jobsMu.Lock()
	j := updateJobs[name]
	jobsMu.Unlock()
	if j == nil {
		return nil
	}
	s := j.snapshot()
	return &s
}

// StartUpdate starts updating a container in the background.
func (m *ContainerUpdateManager) StartUpdate(nameOrID string) (*UpdateJob, error) {
	ctx := context.Background()
	cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	inspect, err := cli.ContainerInspect(ctx, nameOrID)
	cli.Close()
	if err != nil {
		return nil, err
	}
	name := strings.TrimPrefix(inspect.Name, "/")
	src := detectSource(inspect.Config.Labels, dockerPathMapper(ctx))

	jobsMu.Lock()
	if j := updateJobs[name]; j != nil && j.State == "running" {
		jobsMu.Unlock()
		return nil, errBusy
	}
	job := &UpdateJob{Container: name, Kind: src.Kind, State: "running", StartedAt: time.Now()}
	updateJobs[name] = job
	jobsMu.Unlock()

	go func() {
		updated, err := m.runUpdate(context.Background(), job, name, inspect.State.Running, src)
		now := time.Now()
		jobsMu.Lock()
		job.FinishedAt = &now
		job.Updated = updated
		if err != nil {
			job.State, job.Error = "failed", err.Error()
		} else {
			job.State = "done"
		}
		jobsMu.Unlock()
	}()
	s := job.snapshot()
	return &s, nil
}

func (m *ContainerUpdateManager) runUpdate(ctx context.Context, job *UpdateJob, name string, wasRunning bool, src ContainerSource) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()
	switch src.Kind {
	case "git", "local":
		if src.Kind == "git" {
			job.logf("Source: %s (%s)", src.Remote, orDetached(src.Branch))
			if src.Branch == "" {
				return false, fmt.Errorf("%s is not on a branch (detached HEAD) - check out a branch first", src.Repo)
			}
		} else {
			job.logf("Source: local folder %s (rebuilding)", src.Context)
		}
		for _, step := range rebuildSteps(src, wasRunning) {
			job.logf("$ %s", strings.Join(step, " "))
			cmd := exec.CommandContext(ctx, step[0], step[1:]...)
			cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "COMPOSE_PROGRESS=plain")
			cmd.Dir = filepath.Dir(src.ComposeFile)
			out, err := cmd.CombinedOutput()
			for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
				if l != "" {
					job.logf("%s", l)
				}
			}
			if err != nil {
				if step[0] == "git" {
					return false, fmt.Errorf("git pull failed (local changes or diverged history?): %v", err)
				}
				return false, fmt.Errorf("%s failed: %v", step[len(step)-2], err)
			}
		}
		m.markUpdated(name, true)
		return true, nil
	default:
		job.logf("Pulling the image and recreating the container if it changed...")
		_, updated, err := m.UpdateAndRecreateContainer(ctx, name)
		if err != nil {
			return false, err
		}
		if updated {
			job.logf("Recreated with the newer image.")
		} else {
			job.logf("Already up to date - nothing changed.")
		}
		return updated, nil
	}
}

func orDetached(b string) string {
	if b == "" {
		return "detached HEAD"
	}
	return b
}

func (m *ContainerUpdateManager) markUpdated(name string, updated bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.configs[name]
	cfg.Name = name
	cfg.HasUpdate = false
	cfg.LastCheckedAt = time.Now().Format(time.RFC3339)
	if updated {
		cfg.LastUpdatedAt = cfg.LastCheckedAt
	}
	m.configs[name] = cfg
	_ = m.saveLocked()
}

// dockerPathMapper maps paths seen inside other containers (Portainer's
// /data/compose/<id>/...) to host paths using Docker's own mount table.
var (
	mapperMu    sync.Mutex
	mapperMount map[string]string
	mapperAt    time.Time
)

func dockerPathMapper(ctx context.Context) hostPathMapper {
	mapperMu.Lock()
	defer mapperMu.Unlock()
	if mapperMount == nil || time.Since(mapperAt) > time.Minute {
		mapperMount = map[string]string{}
		if cli, err := client2.NewClientWithOpts(client2.FromEnv, client2.WithAPIVersionNegotiation()); err == nil {
			if list, err := cli.ContainerList(ctx, types.ContainerListOptions{All: true}); err == nil {
				for _, c := range list {
					for _, mnt := range c.Mounts {
						if mnt.Source != "" && mnt.Destination != "" && mnt.Destination != "/" {
							mapperMount[mnt.Destination] = mnt.Source
						}
					}
				}
			}
			cli.Close()
		}
		mapperAt = time.Now()
	}
	mounts := mapperMount
	return func(p string) string {
		if fileExists(p) {
			return p
		}
		for dst, srcDir := range mounts {
			if strings.HasPrefix(p, dst+"/") {
				if host := filepath.Join(srcDir, strings.TrimPrefix(p, dst)); fileExists(host) {
					return host
				}
			}
		}
		return p
	}
}
