package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/moby/sys/mountinfo"
	"go.uber.org/zap"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
)

// Apps whose folders are on a drive that isn't mounted
//
// /etc/fstab says which drive belongs at which folder. When one isn't
// mounted (a power cut left it dirty, it is unplugged), its mount point
// is a plain folder on the system disk, and an app started against it
// writes there - Docker even creates the missing bind folders. So an app
// with a bind mount under such a mount point is refused (start, restart,
// install, apply changes) and, when Docker started it anyway (restart
// policy at boot), stopped. Once the drive is mounted, the containers
// stopped here are started again.

var (
	fstabPath     = "/etc/fstab"
	driveWaitFile = "/var/lib/nivaroos/app-management/drive-wait.json"
)

var fstabUnescape = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`)

// fstabMountPoints: every mount point fstab puts a drive at (not /, swap).
func fstabMountPoints(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || strings.HasPrefix(f[0], "#") || f[2] == "swap" {
			continue
		}
		mp := filepath.Clean(fstabUnescape.Replace(f[1]))
		if filepath.IsAbs(mp) && mp != "/" {
			out = append(out, mp)
		}
	}
	return out
}

// missingDrive: the mount point (the deepest one) holding one of sources
// that fstab lists but isn't mounted, or "".
func missingDrive(sources, configured []string, mounted map[string]bool) string {
	for _, src := range sources {
		src = filepath.Clean(src)
		holder := ""
		for _, mp := range configured {
			if (src == mp || strings.HasPrefix(src, mp+"/")) && len(mp) > len(holder) {
				holder = mp
			}
		}
		if holder != "" && !mounted[holder] {
			return holder
		}
	}
	return ""
}

func mountedNow() map[string]bool {
	out := map[string]bool{}
	ms, err := mountinfo.GetMounts(nil)
	if err != nil {
		logger.Error("drive guard: couldn't read the mount table", zap.Error(err))
	}
	for _, m := range ms {
		out[m.Mountpoint] = true
	}
	return out
}

// DriveWaitingFor: the unmounted drive one of sources is on, or "".
func DriveWaitingFor(sources []string) string {
	raw, err := os.ReadFile(fstabPath)
	if err != nil {
		return ""
	}
	return missingDrive(sources, fstabMountPoints(string(raw)), mountedNow())
}

func errWaitsForDrive(app, mp string) error {
	return fmt.Errorf("%s waits for drive %s: it isn't mounted, so the app's folders would be on the system disk. The app starts by itself once the drive is mounted (Settings > Storage)", app, filepath.Base(mp))
}

// checkComposeDrives refuses to start a compose app on a missing drive.
func (a *ComposeApp) checkComposeDrives() error {
	var sources []string
	for _, s := range a.Services {
		for _, v := range s.Volumes {
			if p := bindSourceToCreate(v, a.Volumes); p != "" {
				sources = append(sources, p)
			}
		}
	}
	if mp := DriveWaitingFor(sources); mp != "" {
		return errWaitsForDrive(a.Name, mp)
	}
	return nil
}

func bindSources(mounts []types.MountPoint) []string {
	var out []string
	for _, m := range mounts {
		if m.Type == "bind" {
			out = append(out, m.Source)
		}
	}
	return out
}

// guardDocker: the Docker calls the guard makes (a fake in tests).
type guardDocker interface {
	ContainerList(ctx context.Context, options types.ContainerListOptions) ([]types.Container, error)
	ContainerStop(ctx context.Context, id string, options container.StopOptions) error
	ContainerStart(ctx context.Context, id string, options types.ContainerStartOptions) error
}

type driveGuard struct {
	docker  guardDocker
	fstab   func() string
	mounted func() map[string]bool
	notify  func(title, message, level string)
	waiting map[string]string // container ID -> drive it waits for (stopped by us)
	file    string
}

func containerApp(c types.Container) string {
	if p := c.Labels["com.docker.compose.project"]; p != "" {
		return p
	}
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return c.ID[:12]
}

func (g *driveGuard) tick(ctx context.Context) {
	list, err := g.docker.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return // Docker not up yet
	}
	configured, mounted := fstabMountPoints(g.fstab()), g.mounted()
	changed, seen := false, map[string]bool{}
	for _, c := range list {
		seen[c.ID] = true
		mp := missingDrive(bindSources(c.Mounts), configured, mounted)
		running := c.State == "running" || c.State == "restarting"
		app, drive := containerApp(c), filepath.Base(mp)
		switch {
		case running && mp != "":
			timeout := 30
			if err := g.docker.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
				logger.Error("drive guard: couldn't stop an app whose drive isn't mounted", zap.String("app", app), zap.String("drive", mp), zap.Error(err))
				continue
			}
			logger.Info("drive guard: stopped an app whose drive isn't mounted", zap.String("app", app), zap.Strings("container", c.Names), zap.String("drive", mp))
			g.waiting[c.ID], changed = mp, true
			g.notify(app+" waits for drive "+drive,
				drive+" isn't mounted, so "+app+" was stopped before it could keep files on the system disk. It starts again by itself once "+drive+" is mounted (Settings > Storage).", "warning")
		case !running && mp == "" && g.waiting[c.ID] != "":
			if err := g.docker.ContainerStart(ctx, c.ID, types.ContainerStartOptions{}); err != nil {
				logger.Error("drive guard: couldn't start an app again after its drive came back", zap.String("app", app), zap.Error(err))
				continue
			}
			logger.Info("drive guard: started an app again, its drive is mounted", zap.String("app", app), zap.String("drive", g.waiting[c.ID]))
			g.notify(app+" started again", filepath.Base(g.waiting[c.ID])+" is mounted again, so "+app+" was started.", "success")
			delete(g.waiting, c.ID)
			changed = true
		}
	}
	for id := range g.waiting {
		if !seen[id] {
			delete(g.waiting, id)
			changed = true
		}
	}
	if changed {
		g.save()
	}
}

func (g *driveGuard) load() {
	if raw, err := os.ReadFile(g.file); err == nil {
		_ = json.Unmarshal(raw, &g.waiting)
	}
	if g.waiting == nil {
		g.waiting = map[string]string{}
	}
}

func (g *driveGuard) save() {
	raw, _ := json.Marshal(g.waiting)
	err := os.MkdirAll(filepath.Dir(g.file), 0o755)
	if err == nil {
		err = os.WriteFile(g.file, raw, 0o644)
	}
	if err != nil {
		logger.Error("drive guard: couldn't save the apps waiting for a drive", zap.Error(err))
	}
}

// StartDriveGuard checks every 10 s. The containers it stopped are kept
// in driveWaitFile, so they are started again even after a restart.
func StartDriveGuard(ctx context.Context) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Error("drive guard: no Docker client", zap.Error(err))
		return
	}
	g := &driveGuard{
		docker: cli,
		fstab: func() string {
			raw, _ := os.ReadFile(fstabPath)
			return string(raw)
		},
		mounted: mountedNow,
		notify: func(title, message, level string) {
			PublishEventWrapper(ctx, common.EventTypeAppNotify, map[string]string{
				"title": title, "message": message, "level": level, "category": "app",
				"action": `{"target":"settings","props":{"section":"storage"}}`,
			})
		},
		file: driveWaitFile,
	}
	g.load()
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			g.tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
