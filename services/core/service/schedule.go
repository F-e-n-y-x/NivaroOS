package service

import (
	"context"
	"encoding/json"
	"fmt"
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

func (s *scheduleService) executeTask(taskID string) {
	s.mu.Lock()
	t, ok := s.tasks[taskID]
	if !ok {
		s.mu.Unlock()
		return
	}
	t.LastRun = time.Now().Format(time.RFC3339)
	t.LastStatus = "running"
	_ = s.saveLocked()
	s.mu.Unlock()

	out, err := s.runTaskAction(t)

	s.mu.Lock()
	defer s.mu.Unlock()

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

func (s *scheduleService) runTaskAction(t *ScheduleTask) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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
			inspectCmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Config.Image}}", target)
			imgBytes, err := inspectCmd.Output()
			if err != nil {
				return string(imgBytes), fmt.Errorf("failed to inspect container image: %w", err)
			}
			imgName := strings.TrimSpace(string(imgBytes))
			pullCmd := exec.CommandContext(ctx, "docker", "pull", imgName)
			pullOut, err := pullCmd.CombinedOutput()
			if err != nil {
				return string(pullOut), fmt.Errorf("failed to pull image %s: %w", imgName, err)
			}
			restartCmd := exec.CommandContext(ctx, "docker", "restart", target)
			restartOut, err := restartCmd.CombinedOutput()
			return fmt.Sprintf("Pulled %s:\n%s\nRestarted %s:\n%s", imgName, string(pullOut), target, string(restartOut)), err
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
			cmd := exec.CommandContext(ctx, "docker", "system", "prune", "-f")
			buf, err := cmd.CombinedOutput()
			return string(buf), err
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
		inspectCmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Config.Image}}", target)
		imgBytes, err := inspectCmd.Output()
		if err != nil {
			return string(imgBytes), fmt.Errorf("failed to inspect container image: %w", err)
		}
		imgName := strings.TrimSpace(string(imgBytes))
		pullCmd := exec.CommandContext(ctx, "docker", "pull", imgName)
		pullOut, err := pullCmd.CombinedOutput()
		if err != nil {
			return string(pullOut), fmt.Errorf("failed to pull image %s: %w", imgName, err)
		}
		restartCmd := exec.CommandContext(ctx, "docker", "restart", target)
		restartOut, err := restartCmd.CombinedOutput()
		return fmt.Sprintf("Pulled %s:\n%s\nRestarted %s:\n%s", imgName, string(pullOut), target, string(restartOut)), err
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
