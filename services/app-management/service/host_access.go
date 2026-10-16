package service

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/samber/lo"
)

// Everything below is about one question: could this container take over
// the server? Host networking puts it on the host's loopback, which
// NivaroOS's APIs trust (audit H1); the rest is root on the host one way
// or another. Installing, re-configuring or updating an app into such
// access needs the admin's explicit OK.

// hostPaths: bind-mounting one of these (or a folder that contains one,
// such as / or /var) gives the container the server.
var hostPaths = []string{
	"/etc", "/root", "/boot", "/proc", "/sys", "/dev",
	"/var/lib/nivaroos", "/var/lib/casaos", "/var/lib/docker", "/var/lib/containerd",
	"/run/docker.sock", "/run/containerd", "/run/podman",
	"/var/run/docker.sock", "/var/run/containerd", "/var/run/podman",
}

// harmlessHostFiles: read by many store apps, nothing to take over.
var harmlessHostFiles = []string{"/etc/localtime", "/etc/timezone", "/etc/os-release"}

// capWhy: what an added capability lets a container do to the host.
var capWhy = map[string]string{
	"ALL":             "every Linux capability (cap_add: ALL) - root on the server",
	"SYS_ADMIN":       "capability SYS_ADMIN - near root on the server",
	"NET_ADMIN":       "capability NET_ADMIN - can change the server's network and firewall",
	"SYS_PTRACE":      "capability SYS_PTRACE - can look inside other processes",
	"SYS_MODULE":      "capability SYS_MODULE - can load kernel modules",
	"DAC_READ_SEARCH": "capability DAC_READ_SEARCH - can read any file on the server",
}

// containerSocket: a Docker / containerd / podman API socket, wherever it
// lives (rootless ones sit under /run/user/<uid>/).
func containerSocket(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".sock") &&
		(strings.Contains(base, "docker") || strings.Contains(base, "containerd") || strings.Contains(base, "podman"))
}

// within: path is p or inside it.
func within(path, p string) bool {
	return path == p || strings.HasPrefix(path, strings.TrimSuffix(p, "/")+"/")
}

// mountHostAccess says why bind-mounting host path src is dangerous ("" if
// it isn't).
func mountHostAccess(src string) string {
	if src == "" || !filepath.IsAbs(src) {
		return ""
	}
	src = filepath.Clean(src)
	if containerSocket(src) {
		return "controls the server's containers (mounts " + src + ")"
	}
	if lo.Contains(harmlessHostFiles, src) {
		return ""
	}
	for _, p := range hostPaths {
		if within(src, p) || within(p, src) {
			if src == "/" {
				return "mounts the server's whole disk (/)"
			}
			return "mounts the server's " + src
		}
	}
	return ""
}

// serviceHostAccess: the reasons one service gets root-equivalent access
// to this server (unsorted, may repeat).
func serviceHostAccess(s types.ServiceConfig) []string {
	var why []string
	if s.NetworkMode == "host" {
		why = append(why, "host network")
	}
	if s.Privileged {
		why = append(why, "privileged")
	}
	if s.Pid == "host" {
		why = append(why, "sees and signals every process on the server (pid: host)")
	}
	if s.Ipc == "host" {
		why = append(why, "shares the server's memory (ipc: host)")
	}
	if s.UserNSMode == "host" {
		why = append(why, "runs in the server's user namespace (userns_mode: host)")
	}
	for _, c := range s.CapAdd {
		c = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(c)), "CAP_")
		if w, ok := capWhy[c]; ok {
			why = append(why, w)
		} else if c != "" {
			why = append(why, "capability "+c)
		}
	}
	for _, d := range s.Devices {
		why = append(why, "uses the server's device "+d.Source)
	}
	for _, o := range s.SecurityOpt {
		switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(o)), "=", ":") {
		case "apparmor:unconfined":
			why = append(why, "AppArmor turned off (security_opt "+o+")")
		case "seccomp:unconfined":
			why = append(why, "seccomp turned off (security_opt "+o+")")
		case "label:disable":
			why = append(why, "SELinux labels turned off (security_opt "+o+")")
		case "systempaths:unconfined":
			why = append(why, "the server's /proc and /sys unmasked (security_opt "+o+")")
		}
	}
	for _, v := range s.Volumes {
		if v.Type != "" && v.Type != types.VolumeTypeBind {
			continue
		}
		if w := mountHostAccess(v.Source); w != "" {
			why = append(why, w)
		}
	}
	return why
}

// HostAccess says why a would get root-equivalent access to this server
// ("<service>: <reason>", sorted, no repeats), or nil.
func (a *ComposeApp) HostAccess() []string {
	var why []string
	for name, s := range a.Services {
		for _, w := range serviceHostAccess(s) {
			why = append(why, name+": "+w)
		}
	}
	if why == nil {
		return nil
	}
	why = lo.Uniq(why)
	sort.Strings(why)
	return why
}

// HostAccessAsDeployed is HostAccess of compose yaml with its variables
// resolved as they are when the app starts (interpolationEnv): the parse
// the routes check keeps ${SOCK:-/var/run/docker.sock} as an unresolved
// reference, which then starts as the Docker socket. Fails closed.
func HostAccessAsDeployed(yaml []byte, appID string) []string {
	env := map[string]string{}
	for _, kv := range interpolationEnv(appID) {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	project, err := loader.LoadWithContext(context.Background(), types.ConfigDetails{
		ConfigFiles: []types.ConfigFile{{Content: yaml}},
		Environment: env,
		WorkingDir:  catalogWorkingDir,
	}, func(o *loader.Options) {
		o.SkipValidation = true
		o.SetProjectName(lo.CoalesceOrEmpty(getNameFrom(yaml), appID, "app"), false)
	})
	if err != nil {
		return []string{"its compose file could not be checked (" + err.Error() + ")"}
	}
	return (*ComposeApp)(project).HostAccess()
}

// ServiceHostAccess is HostAccess for one container (the legacy settings API).
func ServiceHostAccess(s types.ServiceConfig) []string {
	why := lo.Uniq(serviceHostAccess(s))
	sort.Strings(why)
	return why
}

// ErrUpdateNeedsApproval: the store's new version of an app wants access
// to the server that the installed one doesn't have (UpdateNeedsApproval).
var ErrUpdateNeedsApproval = errors.New("this update needs the admin's OK")

// UpdateNeedsApproval is returned by ComposeApp.Update instead of
// updating: the installed version keeps running until the admin approves.
type UpdateNeedsApproval struct {
	App     string
	Reasons []string
}

func (e *UpdateNeedsApproval) Error() string {
	return e.App + " update wants: " + strings.Join(e.Reasons, ", ") + ". Approve it with the app's Update button."
}

func (e *UpdateNeedsApproval) Unwrap() error { return ErrUpdateNeedsApproval }

// newStoreAccess: what the store's version of the app wants that the
// installed one doesn't have. The installed app's own access was approved
// when it was installed or last saved.
func newStoreAccess(installed, store *ComposeApp) []string {
	// Only services the installed app has: Update refuses any other shape.
	want := store.withServices(lo.PickByKeys(store.Services, lo.Keys(installed.Services))).HostAccess()
	return lo.Without(want, installed.HostAccess()...)
}

// grantStoreAccess gives local the access settings its store version s
// has (after the admin approved them): an update otherwise only takes the
// store's image, so a new version that needs e.g. the Docker socket would
// start without it.
func grantStoreAccess(local, s types.ServiceConfig) types.ServiceConfig {
	if s.NetworkMode == "host" && local.NetworkMode != "host" {
		local.NetworkMode, local.Networks = s.NetworkMode, s.Networks
	}
	local.Privileged = local.Privileged || s.Privileged
	for _, f := range []struct {
		to   *string
		from string
	}{{&local.Pid, s.Pid}, {&local.Ipc, s.Ipc}, {&local.UserNSMode, s.UserNSMode}} {
		if f.from == "host" {
			*f.to = "host"
		}
	}
	local.CapAdd = lo.Union(local.CapAdd, s.CapAdd)
	local.SecurityOpt = lo.Union(local.SecurityOpt, s.SecurityOpt)
	for _, d := range s.Devices {
		if !lo.ContainsBy(local.Devices, func(l types.DeviceMapping) bool { return l.Target == d.Target }) {
			local.Devices = append(local.Devices, d)
		}
	}
	for _, v := range s.Volumes {
		if (v.Type == "" || v.Type == types.VolumeTypeBind) && mountHostAccess(v.Source) != "" &&
			!lo.ContainsBy(local.Volumes, func(l types.ServiceVolumeConfig) bool { return l.Target == v.Target }) {
			local.Volumes = append(local.Volumes, v)
		}
	}
	return local
}

// awaitingApproval: the reasons each app's update was last held for, so
// the nightly auto-update tells the owner once, not every night.
// ponytail: in memory - a restart of app-management notifies once more.
var (
	awaitingMu       sync.Mutex
	awaitingApproval = map[string]string{}
)

// holdUpdate notifies the owner (once per set of reasons) that app's
// update waits for their OK.
func holdUpdate(app string, reasons []string) *UpdateNeedsApproval {
	held := &UpdateNeedsApproval{App: app, Reasons: reasons}
	key := strings.Join(reasons, "\n")
	awaitingMu.Lock()
	seen := awaitingApproval[app] == key
	awaitingApproval[app] = key
	awaitingMu.Unlock()
	if !seen && notifyHeldUpdate != nil {
		notifyHeldUpdate(app, reasons)
	}
	return held
}

// notifyHeldUpdate puts "<app> update wants: …" in the owner's feed
// (replaceable in tests).
var notifyHeldUpdate = func(app string, reasons []string) {
	PublishEventWrapper(context.Background(), common.EventTypeAppNotify, map[string]string{
		"title":    app + " update wants: " + strings.Join(reasons, ", "),
		"message":  "The new version of " + app + " asks for this access to the server, so it wasn't updated - the current version keeps running. If you trust it, open the App Store and press Update on " + app + " to approve.",
		"level":    "warning",
		"category": "app",
	})
}
