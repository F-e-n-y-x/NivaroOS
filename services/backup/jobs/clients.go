package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Loopback clients for the other NivaroOS services (spec §0): core's
// schedules API, app-management, vm-sidecar and the message bus. Each
// call is plain same-host automation - loopback peer, no Origin, no
// Sec-Fetch-Site, no proxy headers - so the target's local-automation
// rule (common/middleware/localauth.go) lets it through without a user
// token. Addresses are re-read from the runtime directory on every call,
// because those services pick a new random port on each start.

// Runtime address files (services/common/external).
const (
	coreURLFile       = "nivaroos.url"
	appMgmtURLFile    = "app-management.url"
	messageBusURLFile = "message-bus.url"
	// vmSidecarAddr is fixed (services/vm-sidecar main.go).
	vmSidecarAddr = "http://127.0.0.1:28641"
)

// errUnreachable marks a service that isn't running or not installed.
var errUnreachable = errors.New("service unreachable")

// loopbackClient is the HTTP client for service-to-service calls. It
// never follows redirects (a redirect off loopback would carry the
// automation privileges elsewhere).
var loopbackClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// runtimeAddr reads a service address file from the runtime directory.
func runtimeAddr(runtimePath, file string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(runtimePath, file))
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", errUnreachable, file, err)
	}
	addr := strings.TrimRight(strings.TrimSpace(string(raw)), "/")
	u, err := url.Parse(addr)
	if err != nil || u.Scheme != "http" || !isLoopbackHost(u.Hostname()) {
		return "", fmt.Errorf("%s: %q is not a loopback http address", file, addr)
	}
	return addr, nil
}

func isLoopbackHost(h string) bool {
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// httpStatusError is a non-2xx answer.
type httpStatusError struct {
	Status int
	Body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// doJSON sends body (JSON-encoded unless it is []byte) and decodes a
// 2xx JSON answer into out (when non-nil).
func doJSON(ctx context.Context, method, u string, body, out interface{}) error {
	var rd io.Reader
	if body != nil {
		raw, ok := body.([]byte)
		if !ok {
			var err error
			if raw, err = json.Marshal(body); err != nil {
				return err
			}
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := loopbackClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", errUnreachable, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return &httpStatusError{Status: res.StatusCode, Body: msg}
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, u, err)
	}
	return nil
}

// ---------------------------------------------------------------------
// Apps (app-management)

// AppController stops and starts container apps for hooks.
type AppController interface {
	// List returns every installed compose app and whether it runs.
	List(ctx context.Context) (map[string]bool, error)
	// Stop / Start ask app-management to change an app's status; they
	// return once the request is accepted (poll List for the result).
	Stop(ctx context.Context, name string) error
	Start(ctx context.Context, name string) error
}

// HTTPApps is AppController over app-management's v2 API.
type HTTPApps struct{ RuntimePath string }

func (a HTTPApps) base() (string, error) {
	addr, err := runtimeAddr(a.RuntimePath, appMgmtURLFile)
	if err != nil {
		return "", err
	}
	return addr + "/v2/app_management", nil
}

func (a HTTPApps) List(ctx context.Context) (map[string]bool, error) {
	base, err := a.base()
	if err != nil {
		return nil, err
	}
	var res struct {
		Data []struct {
			Name    *string `json:"name"`
			Status  *string `json:"status"`
			AppType string  `json:"app_type"`
		} `json:"data"`
	}
	// The app grid is app-management's fast listing (no update checks).
	if err := doJSON(ctx, http.MethodGet, base+"/web/appgrid", nil, &res); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, it := range res.Data {
		if it.AppType != "v2app" || it.Name == nil || *it.Name == "" {
			continue
		}
		out[*it.Name] = it.Status != nil && strings.HasPrefix(strings.ToLower(*it.Status), "running")
	}
	return out, nil
}

func (a HTTPApps) setStatus(ctx context.Context, name, status string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid app name %q", name)
	}
	base, err := a.base()
	if err != nil {
		return err
	}
	return doJSON(ctx, http.MethodPut, base+"/compose/"+url.PathEscape(name)+"/status", []byte(`"`+status+`"`), nil)
}

func (a HTTPApps) Stop(ctx context.Context, name string) error { return a.setStatus(ctx, name, "stop") }
func (a HTTPApps) Start(ctx context.Context, name string) error {
	return a.setStatus(ctx, name, "start")
}

// ---------------------------------------------------------------------
// VMs (vm-sidecar)

// VM states the hooks care about (vm-sidecar's domainStateString).
const (
	vmRunning = "running"
	vmShutOff = "shutoff"
)

// VMController shuts down and starts VMs for hooks. It never forces a VM
// off (spec §8.2).
type VMController interface {
	// List returns every VM and its state; errUnreachable when the VM
	// Manager isn't installed.
	List(ctx context.Context) (map[string]string, error)
	State(ctx context.Context, name string) (string, error)
	Shutdown(ctx context.Context, name string) error
	Start(ctx context.Context, name string) error
}

// HTTPVMs is VMController over vm-sidecar's API.
type HTTPVMs struct{ Base string }

func (v HTTPVMs) base() string {
	if v.Base != "" {
		return v.Base
	}
	return vmSidecarAddr
}

func (v HTTPVMs) List(ctx context.Context) (map[string]string, error) {
	var vms []struct {
		Name  string `json:"name"`
		State string `json:"state"`
	}
	if err := doJSON(ctx, http.MethodGet, v.base()+"/vms", nil, &vms); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(vms))
	for _, vm := range vms {
		out[vm.Name] = vm.State
	}
	return out, nil
}

func (v HTTPVMs) State(ctx context.Context, name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("invalid VM name %q", name)
	}
	var vm struct {
		State string `json:"state"`
	}
	if err := doJSON(ctx, http.MethodGet, v.base()+"/vms/"+url.PathEscape(name), nil, &vm); err != nil {
		return "", err
	}
	return vm.State, nil
}

func (v HTTPVMs) action(ctx context.Context, name, act string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid VM name %q", name)
	}
	return doJSON(ctx, http.MethodPost, v.base()+"/vms/"+url.PathEscape(name)+"/"+act, nil, nil)
}

func (v HTTPVMs) Shutdown(ctx context.Context, name string) error {
	return v.action(ctx, name, "shutdown")
}
func (v HTTPVMs) Start(ctx context.Context, name string) error { return v.action(ctx, name, "start") }

// ---------------------------------------------------------------------
// Scheduled Tasks (core)

// ScheduleTask is the part of a core Scheduled Task (services/core
// service/schedule.go ScheduleTask) Backup reads. Raw keeps the whole
// object, so an update sends back every field core has - including ones
// this build doesn't know.
type ScheduleTask struct {
	ID         string
	Name       string
	Enabled    bool
	Cron       string
	Type       string
	Action     string
	SyncMode   string
	SourcePath string
	DestPath   string
	TargetID   string
	Target     string
	TargetName string
	ExtraArgs  string
	LastStatus string
	MigratedTo string
	Raw        map[string]interface{}
}

func scheduleTaskFromRaw(raw map[string]interface{}) ScheduleTask {
	str := func(k string) string {
		s, _ := raw[k].(string)
		return s
	}
	enabled, _ := raw["enabled"].(bool)
	return ScheduleTask{
		ID: str("id"), Name: str("name"), Enabled: enabled, Cron: str("cron"), Type: str("type"),
		Action: str("action"), SyncMode: str("sync_mode"), SourcePath: str("source_path"), DestPath: str("dest_path"),
		TargetID: str("target_id"), Target: str("target"), TargetName: str("target_name"), ExtraArgs: str("extra_args"),
		LastStatus: str("last_status"), MigratedTo: str(ScheduleMigratedField), Raw: raw,
	}
}

// ScheduleClient reads and updates core's Scheduled Tasks.
type ScheduleClient interface {
	ListTasks(ctx context.Context) ([]ScheduleTask, error)
	// UpdateTask sets enabled and the migrated_to marker ("" clears it)
	// and returns the task as core stored it.
	UpdateTask(ctx context.Context, task ScheduleTask, enabled bool, migratedTo string) (ScheduleTask, error)
}

// HTTPSchedules is ScheduleClient over core's /v1/schedules.
type HTTPSchedules struct{ RuntimePath string }

type coreEnvelope struct {
	Success int             `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (s HTTPSchedules) base() (string, error) {
	addr, err := runtimeAddr(s.RuntimePath, coreURLFile)
	if err != nil {
		return "", err
	}
	return addr + "/v1/schedules", nil
}

func (s HTTPSchedules) ListTasks(ctx context.Context) ([]ScheduleTask, error) {
	base, err := s.base()
	if err != nil {
		return nil, err
	}
	var env coreEnvelope
	if err := doJSON(ctx, http.MethodGet, base, nil, &env); err != nil {
		return nil, err
	}
	var raws []map[string]interface{}
	if len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, &raws); err != nil {
			return nil, fmt.Errorf("decode schedules: %w", err)
		}
	}
	out := make([]ScheduleTask, 0, len(raws))
	for _, r := range raws {
		out = append(out, scheduleTaskFromRaw(r))
	}
	return out, nil
}

func (s HTTPSchedules) UpdateTask(ctx context.Context, task ScheduleTask, enabled bool, migratedTo string) (ScheduleTask, error) {
	base, err := s.base()
	if err != nil {
		return ScheduleTask{}, err
	}
	body := make(map[string]interface{}, len(task.Raw)+2)
	for k, v := range task.Raw {
		body[k] = v
	}
	body["enabled"] = enabled
	body[ScheduleMigratedField] = migratedTo
	var env coreEnvelope
	if err := doJSON(ctx, http.MethodPut, base+"/"+url.PathEscape(task.ID), body, &env); err != nil {
		return ScheduleTask{}, err
	}
	var raw map[string]interface{}
	if len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, &raw); err != nil {
			return ScheduleTask{}, fmt.Errorf("decode schedule %s: %w", task.ID, err)
		}
	}
	if raw == nil {
		raw = body
	}
	return scheduleTaskFromRaw(raw), nil
}

// ---------------------------------------------------------------------
// Scheduled Tasks side of the lock contract

// scheduleBusyTTL is how long one GET /v1/schedules answer is reused.
const scheduleBusyTTL = 5 * time.Second

// ScheduleBusy is the BusyChecker for Scheduled Tasks (spec §8.1, the
// frozen contract in locks.go): a "vm" or "container" task whose target
// matches and whose last_status is "running" holds that VM or app. When
// core can't be reached nothing counts as busy - Scheduled Tasks then
// can't be running either.
type ScheduleBusy struct {
	Client ScheduleClient
	Poll   time.Duration // WaitFree re-check interval (default 10 s)

	mu      sync.Mutex
	fetched time.Time
	tasks   []ScheduleTask
}

var _ BusyChecker = (*ScheduleBusy)(nil)

func (b *ScheduleBusy) snapshot() []ScheduleTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	if time.Since(b.fetched) < scheduleBusyTTL && b.tasks != nil {
		return b.tasks
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tasks, err := b.Client.ListTasks(ctx)
	if err != nil {
		tasks = []ScheduleTask{}
	}
	b.tasks, b.fetched = tasks, time.Now()
	return tasks
}

func (b *ScheduleBusy) IsBusy(kind, target string) bool {
	want := map[string]string{LockApp: "container", LockVM: "vm"}[kind]
	if want == "" || target == "" {
		return false
	}
	for _, t := range b.snapshot() {
		if t.Type != want || t.LastStatus != "running" {
			continue
		}
		if t.TargetID == target || t.TargetName == target || t.Target == target {
			return true
		}
	}
	return false
}

func (b *ScheduleBusy) WaitFree(ctx context.Context, kind, target string) error {
	poll := b.Poll
	if poll <= 0 {
		poll = 10 * time.Second
	}
	for b.IsBusy(kind, target) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
		b.mu.Lock()
		b.fetched = time.Time{}
		b.mu.Unlock()
	}
	return nil
}

// ---------------------------------------------------------------------
// Message bus

// Bus publishes backup events (spec §10.1).
type Bus interface {
	RegisterEventTypes(ctx context.Context, types []EventType) error
	Publish(ctx context.Context, name string, props map[string]string) error
}

// HTTPBus is Bus over the message bus's REST API.
type HTTPBus struct{ RuntimePath string }

func (b HTTPBus) base() (string, error) {
	addr, err := runtimeAddr(b.RuntimePath, messageBusURLFile)
	if err != nil {
		return "", err
	}
	return addr + "/v2/message_bus", nil
}

func (b HTTPBus) RegisterEventTypes(ctx context.Context, types []EventType) error {
	base, err := b.base()
	if err != nil {
		return err
	}
	return doJSON(ctx, http.MethodPost, base+"/event_type", types, nil)
}

func (b HTTPBus) Publish(ctx context.Context, name string, props map[string]string) error {
	base, err := b.base()
	if err != nil {
		return err
	}
	return doJSON(ctx, http.MethodPost, base+"/event/"+url.PathEscape(EventSourceID)+"/"+url.PathEscape(name), props, nil)
}
