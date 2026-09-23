package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
)

// dockerPruneSteps is what the "Docker cleanup" maintenance task runs. It
// used to be `docker system prune -f`, which also deletes every *stopped
// container* - including ones the owner stopped on purpose. Only things
// that are safe to lose: dangling images, build cache, unused networks.
// Never containers or volumes.
var dockerPruneSteps = [][]string{
	{"image", "prune", "-f"},
	{"builder", "prune", "-f"},
	{"network", "prune", "-f"},
}

type ScheduleTask struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	Cron        string    `json:"cron"`        // standard 5-part cron e.g. "0 23 * * *" or descriptor like "@daily"
	Type        string    `json:"type"`        // "vm" | "container" | "maintenance" | "command" | "backup" | "sync"
	Action      string    `json:"action"`      // "stop", "start", "restart", "reboot", "force_off", "update", "fstrim", "drop_caches", "docker_prune", "disk_standby_check", "copy", "sync", "move", "archive", "rsync", "run_command"
	TargetID    string    `json:"target_id"`   // VM ID/Name, Container ID/Name, or Source ID
	TargetName  string    `json:"target_name"` // Friendly display name
	Command     string    `json:"command"`     // Command for script execution
	SourcePath  string    `json:"source_path"` // Source directory or cloud remote
	DestPath    string    `json:"dest_path"`   // Destination directory or cloud remote
	Direction   string    `json:"direction"`   // "local_to_cloud" | "cloud_to_local" | "local_to_local"
	SyncMode    string    `json:"sync_mode"`   // "copy" | "sync" | "archive" | "move" | "rsync"
	ExtraArgs   string    `json:"extra_args"`  // Optional extra flags e.g. --bwlimit
	ActionType  string    `json:"action_type"` // for backwards compatibility
	Target      string    `json:"target"`      // for backwards compatibility
	Description string    `json:"description"`
	LastRun     string    `json:"last_run"`
	LastStatus  string    `json:"last_status"` // "success" | "error" | "running" | ""
	LastOutput  string    `json:"last_output"`
	NextRun     string    `json:"next_run"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	entryID cron.EntryID `json:"-"`
}

type ScheduleService interface {
	GetTasks() []ScheduleTask
	GetTask(id string) (*ScheduleTask, error)
	CreateTask(task ScheduleTask) (*ScheduleTask, error)
	UpdateTask(id string, task ScheduleTask) (*ScheduleTask, error)
	DeleteTask(id string) error
	ToggleTask(id string, enabled bool) (*ScheduleTask, error)
	RunTaskNow(id string) (string, error)
	GetTargets() (map[string]interface{}, error)
}

type scheduleService struct {
	mu       sync.RWMutex
	running  map[string]bool // tasks executing right now (never twice at once)
	tasks    map[string]*ScheduleTask
	cron     *cron.Cron
	dataFile string
	parser   cron.Parser
}

func getScheduleDataPath() string {
	paths := []string{
		"/var/lib/nivaroos/schedules.json",
		"/var/lib/casaos/schedules.json",
	}
	for _, p := range paths {
		dir := filepath.Dir(p)
		if _, err := os.Stat(dir); err == nil {
			return p
		}
	}
	_ = os.MkdirAll("/var/lib/nivaroos", 0755)
	return "/var/lib/nivaroos/schedules.json"
}

func NewScheduleService() ScheduleService {
	parser := cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	c := cron.New(cron.WithParser(parser))
	c.Start()

	s := &scheduleService{
		tasks:    make(map[string]*ScheduleTask),
		cron:     c,
		dataFile: getScheduleDataPath(),
		parser:   parser,
	}

	s.load()
	return s
}

func (s *scheduleService) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.dataFile)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Error("failed to read schedules data", zap.Error(err), zap.String("file", s.dataFile))
		}
		return
	}

	var list []ScheduleTask
	if err := json.Unmarshal(data, &list); err != nil {
		logger.Error("failed to unmarshal schedules", zap.Error(err))
		return
	}

	for i := range list {
		task := list[i]
		// The service stopped while this ran - it didn't finish.
		if task.LastStatus == "running" {
			task.LastStatus = "interrupted"
			task.LastOutput = "The run was interrupted (the NivaroOS service restarted before it finished).\n" + task.LastOutput
		}
		s.tasks[task.ID] = &task
		if task.Enabled {
			s.scheduleLocked(&task)
		} else {
			task.NextRun = ""
		}
	}
}

func (s *scheduleService) saveLocked() error {
	dir := filepath.Dir(s.dataFile)
	_ = os.MkdirAll(dir, 0755)

	list := make([]ScheduleTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, *t)
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.dataFile, data, 0644)
}

func (s *scheduleService) scheduleLocked(t *ScheduleTask) {
	if t.entryID != 0 {
		s.cron.Remove(t.entryID)
		t.entryID = 0
	}

	taskID := t.ID
	entryID, err := s.cron.AddFunc(t.Cron, func() {
		s.executeTask(taskID)
	})
	if err != nil {
		logger.Error("failed to register cron task", zap.String("id", t.ID), zap.String("cron", t.Cron), zap.Error(err))
		t.NextRun = ""
		return
	}

	t.entryID = entryID
	entry := s.cron.Entry(entryID)
	if !entry.Next.IsZero() {
		t.NextRun = entry.Next.Format(time.RFC3339)
	}
}

func (s *scheduleService) unscheduleLocked(t *ScheduleTask) {
	if t.entryID != 0 {
		s.cron.Remove(t.entryID)
		t.entryID = 0
	}
	t.NextRun = ""
}

// maxTaskOutput is how much of a run's output is kept (the end of it -
// that's where errors are). It used to be stored whole and sent with every
// list request.
const maxTaskOutput = 64 << 10

func tailOutput(out string) string {
	if len(out) <= maxTaskOutput {
		return out
	}
	cut := out[len(out)-maxTaskOutput:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i < 512 {
		cut = cut[i+1:]
	}
	return fmt.Sprintf("[... %d earlier bytes of output not kept ...]\n%s", len(out)-len(cut), cut)
}

func (s *scheduleService) executeTask(taskID string) {
	s.mu.Lock()
	t, ok := s.tasks[taskID]
	if !ok {
		s.mu.Unlock()
		return
	}
	// The timer and "Run now" can overlap; running the same task twice at
	// once (two rsync --delete into one place) is never what anyone wants.
	if s.running == nil {
		s.running = map[string]bool{}
	}
	if s.running[taskID] {
		s.mu.Unlock()
		logger.Info("scheduled task skipped: still running from the previous start", zap.String("task", taskID))
		return
	}
	s.running[taskID] = true
	t.LastRun = time.Now().Format(time.RFC3339)
	t.LastStatus = "running"
	_ = s.saveLocked()
	snapshot := *t // run from a copy: edits to the task mustn't race the run
	s.mu.Unlock()

	out, err := s.runTaskAction(&snapshot)
	out = tailOutput(out)

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, taskID)

	t, ok = s.tasks[taskID]
	if !ok {
		return
	}

	if err != nil {
		t.LastStatus = "error"
		t.LastOutput = fmt.Sprintf("Error: %v\nOutput: %s", err, out)
	} else {
		t.LastStatus = "success"
		t.LastOutput = strings.TrimSpace(out)
		if t.LastOutput == "" {
			t.LastOutput = "Task completed successfully with no output"
		}
	}

	if t.entryID != 0 {
		entry := s.cron.Entry(t.entryID)
		if !entry.Next.IsZero() {
			t.NextRun = entry.Next.Format(time.RFC3339)
		}
	}
	_ = s.saveLocked()
}

// taskTimeout: copies and syncs take as long as the data takes (they were
// killed at 10 minutes, leaving a `move` half done); a user command gets an
// hour; everything else is quick.
func taskTimeout(t *ScheduleTask) time.Duration {
	switch t.Type {
	case "backup", "sync":
		return 24 * time.Hour
	case "command":
		return time.Hour
	}
	if t.Command != "" {
		return time.Hour
	}
	return 10 * time.Minute
}

func (s *scheduleService) runTaskAction(t *ScheduleTask) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), taskTimeout(t))
	defer cancel()

	// 1. Structured Type & Action
	if t.Type == "vm" {
		target := t.TargetID
		if target == "" {
			target = t.Target
		}
		switch t.Action {
		case "start":
			cmd := exec.CommandContext(ctx, "virsh", "start", target)
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		case "reboot":
			cmd := exec.CommandContext(ctx, "virsh", "reboot", target)
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		case "force_off":
			cmd := exec.CommandContext(ctx, "virsh", "destroy", target)
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		case "stop", "":
			cmd := exec.CommandContext(ctx, "virsh", "shutdown", target)
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		}
	}

	if t.Type == "container" {
		target := t.TargetID
		if target == "" {
			target = t.Target
		}
		switch t.Action {
		case "start":
			cmd := exec.CommandContext(ctx, "docker", "start", target)
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		case "stop":
			cmd := exec.CommandContext(ctx, "docker", "stop", target)
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		case "update":
			return updateContainerViaAppManagement(ctx, target)
		case "restart", "":
			cmd := exec.CommandContext(ctx, "docker", "restart", target)
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		}
	}

	if t.Type == "maintenance" {
		switch t.Action {
		case "fstrim":
			cmd := exec.CommandContext(ctx, "fstrim", "-av")
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		case "drop_caches":
			cmd := exec.CommandContext(ctx, "bash", "-c", "sync && echo 3 > /proc/sys/vm/drop_caches && free -h")
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		case "docker_prune":
			var out strings.Builder
			for _, args := range dockerPruneSteps {
				buf, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
				out.WriteString("$ docker " + strings.Join(args, " ") + "\n")
				out.Write(buf)
				if err != nil {
					return out.String(), err
				}
			}
			return out.String(), nil
		case "disk_standby_check":
			cmd := exec.CommandContext(ctx, "bash", "-c", "hdparm -C /dev/sd[b-z] 2>&1 || true")
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		}
	}

	if t.Type == "backup" || t.Type == "sync" {
		src := strings.TrimSpace(t.SourcePath)
		dest := strings.TrimSpace(t.DestPath)
		if src == "" {
			src = strings.TrimSpace(t.TargetID)
		}
		if dest == "" {
			dest = strings.TrimSpace(t.Target)
		}
		if src == "" || dest == "" {
			if t.Command != "" {
				cmd := exec.CommandContext(ctx, "bash", "-c", t.Command)
				buf, err := cmd.CombinedOutput()
				return string(buf), err
			}
			return "", fmt.Errorf("source and destination are required for backup/sync task")
		}

		action := t.Action
		if action == "" {
			action = t.SyncMode
		}
		if action == "" {
			action = "copy"
		}

		extra := strings.TrimSpace(t.ExtraArgs)

		switch action {
		case "sync", "rclone_sync":
			args := []string{"sync", src, dest, "--stats-one-line", "-v"}
			if extra != "" {
				args = append(args, strings.Fields(extra)...)
			}
			cmd := exec.CommandContext(ctx, "rclone", args...)
			buf, err := cmd.CombinedOutput()
			return string(buf), err

		case "copy", "rclone_copy":
			args := []string{"copy", src, dest, "--stats-one-line", "-v"}
			if extra != "" {
				args = append(args, strings.Fields(extra)...)
			}
			cmd := exec.CommandContext(ctx, "rclone", args...)
			buf, err := cmd.CombinedOutput()
			return string(buf), err

		case "move", "rclone_move":
			args := []string{"move", src, dest, "--stats-one-line", "-v"}
			if extra != "" {
				args = append(args, strings.Fields(extra)...)
			}
			cmd := exec.CommandContext(ctx, "rclone", args...)
			buf, err := cmd.CombinedOutput()
			return string(buf), err

		case "rsync", "rsync_backup":
			srcSlash := src
			if !strings.HasSuffix(srcSlash, "/") && !strings.Contains(srcSlash, ":") {
				srcSlash += "/"
			}
			args := []string{"-avh", "--delete", srcSlash, dest}
			if extra != "" {
				args = append(args, strings.Fields(extra)...)
			}
			cmd := exec.CommandContext(ctx, "rsync", args...)
			buf, err := cmd.CombinedOutput()
			return string(buf), err

		case "archive", "tar_archive":
			tarCmd := fmt.Sprintf(`mkdir -p "%s" && tar -czf "%s/backup_$(date +%%Y%%m%%d_%%H%%M%%S).tar.gz" -C "%s" "%s"`,
				dest, dest, filepath.Dir(src), filepath.Base(src))
			cmd := exec.CommandContext(ctx, "bash", "-c", tarCmd)
			buf, err := cmd.CombinedOutput()
			return string(buf), err

		default:
			args := []string{"copy", src, dest, "--stats-one-line", "-v"}
			if extra != "" {
				args = append(args, strings.Fields(extra)...)
			}
			cmd := exec.CommandContext(ctx, "rclone", args...)
			buf, err := cmd.CombinedOutput()
			return string(buf), err
		}
	}

	if t.Type == "command" || t.Command != "" {
		cmdStr := t.Command
		if cmdStr == "" {
			cmdStr = t.Target
		}
		cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
		buf, err := cmd.CombinedOutput()
		return string(buf), err
	}

	// 2. Fallback ActionType string
	actionType := t.ActionType
	target := t.Target
	switch actionType {
	case "vm_start":
		cmd := exec.CommandContext(ctx, "virsh", "start", target)
		buf, err := cmd.CombinedOutput()
		return string(buf), err
	case "vm_stop":
		cmd := exec.CommandContext(ctx, "virsh", "shutdown", target)
		buf, err := cmd.CombinedOutput()
		return string(buf), err
	case "vm_restart":
		cmd := exec.CommandContext(ctx, "virsh", "reboot", target)
		buf, err := cmd.CombinedOutput()
		return string(buf), err
	case "container_start":
		cmd := exec.CommandContext(ctx, "docker", "start", target)
		buf, err := cmd.CombinedOutput()
		return string(buf), err
	case "container_stop":
		cmd := exec.CommandContext(ctx, "docker", "stop", target)
		buf, err := cmd.CombinedOutput()
		return string(buf), err
	case "container_restart":
		cmd := exec.CommandContext(ctx, "docker", "restart", target)
		buf, err := cmd.CombinedOutput()
		return string(buf), err
	case "container_update":
		return updateContainerViaAppManagement(ctx, target)
	case "ssd_trim":
		cmd := exec.CommandContext(ctx, "fstrim", "-av")
		buf, err := cmd.CombinedOutput()
		return string(buf), err
	case "clear_cache":
		cmd := exec.CommandContext(ctx, "bash", "-c", "sync && echo 3 > /proc/sys/vm/drop_caches && free -h")
		buf, err := cmd.CombinedOutput()
		return string(buf), err
	case "command":
		cmd := exec.CommandContext(ctx, "bash", "-c", target)
		buf, err := cmd.CombinedOutput()
		return string(buf), err
	default:
		return "", fmt.Errorf("unsupported action '%s'", actionType)
	}
}

func (s *scheduleService) GetTasks() []ScheduleTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]ScheduleTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		taskCopy := *t
		if t.Enabled && t.entryID != 0 {
			entry := s.cron.Entry(t.entryID)
			if !entry.Next.IsZero() {
				taskCopy.NextRun = entry.Next.Format(time.RFC3339)
			}
		}
		res = append(res, taskCopy)
	}
	return res
}

func (s *scheduleService) GetTask(id string) (*ScheduleTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task '%s' not found", id)
	}
	taskCopy := *t
	return &taskCopy, nil
}

func (s *scheduleService) CreateTask(task ScheduleTask) (*ScheduleTask, error) {
	if _, err := s.parser.Parse(task.Cron); err != nil {
		return nil, fmt.Errorf("invalid cron expression '%s': %w", task.Cron, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	task.ID = "task_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
	task.CreatedAt = time.Now()
	task.UpdatedAt = time.Now()

	s.tasks[task.ID] = &task
	if task.Enabled {
		s.scheduleLocked(&task)
	} else {
		task.NextRun = ""
	}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	taskCopy := task
	return &taskCopy, nil
}

func (s *scheduleService) UpdateTask(id string, update ScheduleTask) (*ScheduleTask, error) {
	if _, err := s.parser.Parse(update.Cron); err != nil {
		return nil, fmt.Errorf("invalid cron expression '%s': %w", update.Cron, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task '%s' not found", id)
	}

	t.Name = update.Name
	t.Description = update.Description
	t.Cron = update.Cron
	t.Type = update.Type
	t.Action = update.Action
	t.TargetID = update.TargetID
	t.TargetName = update.TargetName
	t.Command = update.Command
	// Backup / sync settings (these weren't copied: editing a backup task
	// silently kept the old paths and mode).
	t.SourcePath = update.SourcePath
	t.DestPath = update.DestPath
	t.Direction = update.Direction
	t.SyncMode = update.SyncMode
	t.ExtraArgs = update.ExtraArgs
	t.ActionType = update.ActionType
	t.Target = update.Target
	t.Enabled = update.Enabled
	t.UpdatedAt = time.Now()

	if t.Enabled {
		s.scheduleLocked(t)
	} else {
		s.unscheduleLocked(t)
	}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	taskCopy := *t
	return &taskCopy, nil
}

func (s *scheduleService) DeleteTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task '%s' not found", id)
	}

	s.unscheduleLocked(t)
	delete(s.tasks, id)
	return s.saveLocked()
}

func (s *scheduleService) ToggleTask(id string, enabled bool) (*ScheduleTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task '%s' not found", id)
	}

	t.Enabled = enabled
	t.UpdatedAt = time.Now()

	if t.Enabled {
		s.scheduleLocked(t)
	} else {
		s.unscheduleLocked(t)
	}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	taskCopy := *t
	return &taskCopy, nil
}

func (s *scheduleService) RunTaskNow(id string) (string, error) {
	s.mu.RLock()
	_, ok := s.tasks[id]
	s.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("task '%s' not found", id)
	}

	go s.executeTask(id)
	return "Execution started", nil
}

type VMTargetInfo struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Active bool   `json:"active"`
}

type ContainerTargetInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
	State  string `json:"state"`
}

type CloudTargetInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Remote     string `json:"remote"`
	MountPoint string `json:"mount_point"`
}

func (s *scheduleService) GetTargets() (map[string]interface{}, error) {
	vms := make([]VMTargetInfo, 0)
	containers := make([]ContainerTargetInfo, 0)
	clouds := make([]CloudTargetInfo, 0)

	virshOut, err := exec.Command("virsh", "list", "--all").Output()
	if err == nil {
		lines := strings.Split(string(virshOut), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "Id") || strings.HasPrefix(line, "---") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				name := parts[1]
				state := strings.Join(parts[2:], " ")
				vms = append(vms, VMTargetInfo{
					Name:   name,
					State:  state,
					Active: state == "running",
				})
			}
		}
	}

	dockerOut, err := exec.Command("docker", "ps", "-a", "--format", "{{.ID}}|{{.Names}}|{{.Image}}|{{.Status}}|{{.State}}").Output()
	if err == nil {
		lines := strings.Split(string(dockerOut), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "|")
			if len(parts) >= 5 {
				containers = append(containers, ContainerTargetInfo{
					ID:     parts[0],
					Name:   parts[1],
					Image:  parts[2],
					Status: parts[3],
					State:  parts[4],
				})
			}
		}
	}

	// Fetch cloud accounts from rclone
	dumpOut, err := exec.Command("rclone", "config", "dump").Output()
	if err == nil {
		var rawConfig map[string]map[string]interface{}
		if err := json.Unmarshal(dumpOut, &rawConfig); err == nil {
			for remoteName, cfg := range rawConfig {
				cType, _ := cfg["type"].(string)
				cUser, _ := cfg["username"].(string)
				cMount, _ := cfg["mount_point"].(string)
				name := cUser
				if name == "" {
					name = remoteName
				}
				clouds = append(clouds, CloudTargetInfo{
					Name:       name,
					Type:       cType,
					Remote:     remoteName + ":",
					MountPoint: cMount,
				})
			}
		}
	}

	// Pre-populate standard local data paths
	candidatePaths := []string{
		"/DATA",
		"/DATA/Documents",
		"/DATA/Media",
		"/DATA/AppData",
		"/DATA/Gallery",
		"/DATA/Downloads",
		"/DATA/Desktop",
		"/DATA/Backup",
		"/DATA/VMs",
	}
	localPaths := make([]string, 0)
	for _, p := range candidatePaths {
		if _, err := os.Stat(p); err == nil {
			localPaths = append(localPaths, p)
		}
	}
	if len(localPaths) == 0 {
		localPaths = append(localPaths, "/DATA")
	}

	return map[string]interface{}{
		"vms":         vms,
		"containers":  containers,
		"clouds":      clouds,
		"local_paths": localPaths,
	}, nil
}

// updateContainerViaAppManagement runs the real container update (pull, then
// recreate with the new image only if there is one - keeping volumes, ports
// and a stopped container stopped). This action used to `docker pull` then
// `docker restart`, which keeps running the old image.
func updateContainerViaAppManagement(ctx context.Context, target string) (string, error) {
	if target == "" {
		return "", errors.New("no container selected for this task")
	}
	port := "80"
	if err, raw := MyService.Gateway().GetPort(); err == nil {
		if p := gjson.Get(raw, "data").String(); p != "" {
			port = p
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+"/v1/container/"+url.PathEscape(target)+"/update", nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("container update request failed: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	msg := gjson.GetBytes(body, "message").String()
	if res.StatusCode != http.StatusOK {
		return string(body), fmt.Errorf("container update failed: %s", msg)
	}
	return target + ": " + msg, nil
}
