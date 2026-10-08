package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/unit"
	"go.uber.org/zap"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/common"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/fstab"
)

// Drives that should be mounted but aren't
//
// A NivaroOS fstab drive set to mount at boot is a problem when it isn't
// mounted and either its drive isn't connected, systemd's mount of it
// failed (a power cut left an NTFS volume dirty or damaged), or our own
// mount of it failed. One unmounted on purpose (Unmount, or umount by
// hand) is not: its mount unit is inactive, not failed.
//
// At start (before Docker - the unit is Before=docker.service) failed boot
// mounts are tried again, NTFS ones that ntfs3 refuses through ntfs-3g.
// What still fails shows on the drive's card in Storage and goes to the
// notification feed once. NTFS and ext2-4 drives can be repaired, only on
// an admin's click (RepairFstabDrive). app-management holds apps whose
// folders are on such a drive until it is mounted.

var (
	// runTool runs a repair/diagnosis tool (fakes in tests).
	runTool = func(timeout time.Duration, name string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		return string(out), err
	}
	// mountUnitState: systemd's ActiveState of the mount unit for mp.
	mountUnitState = func(mp string) string {
		out, _ := exec.Command("systemctl", "show", "-p", "ActiveState", "--value", unit.UnitNamePathEscape(mp)+".mount").Output()
		return strings.TrimSpace(string(out))
	}
	kernelLog = func() string {
		out, _ := exec.Command("dmesg").Output()
		return string(out)
	}
	notifyDrive = func(props map[string]string) {
		resp, err := MyService.MessageBus().PublishEventWithResponse(context.Background(), common.ServiceName, common.DriveNotifyEvent, props)
		if err != nil {
			logger.Error("drive notification not sent", zap.Error(err), zap.Any("props", props))
		} else if resp.StatusCode() != 200 {
			logger.Error("drive notification not sent", zap.String("status", resp.Status()))
		}
	}
)

var drives = struct {
	sync.Mutex
	mountErr map[string]string              // our last mount of mp failed: why
	probe    map[string]*model.DriveProblem // diagnosis, until mp is fine again
	repair   map[string]*model.DriveRepair
	notified map[string]string // mp -> reason sent to the feed
}{mountErr: map[string]string{}, probe: map[string]*model.DriveProblem{}, repair: map[string]*model.DriveRepair{}, notified: map[string]string{}}

func setMountErr(mp, why string) {
	drives.Lock()
	defer drives.Unlock()
	if why == "" {
		delete(drives.mountErr, mp)
	} else {
		drives.mountErr[mp] = why
	}
	delete(drives.probe, mp)
}

func isNTFS(t string) bool { return strings.HasPrefix(t, "ntfs") }
func isExt(t string) bool  { return t == "ext2" || t == "ext3" || t == "ext4" }

func driveName(mp string) string { return filepath.Base(mp) }

// kernelLine: the last kernel message about dev ("sdb1").
func kernelLine(klog, dev string) string {
	last := ""
	for _, l := range strings.Split(klog, "\n") {
		if strings.Contains(l, "("+dev+")") || strings.Contains(l, " "+dev+":") {
			last = l
		}
	}
	if i := strings.Index(last, "] "); i >= 0 && strings.HasPrefix(last, "[") {
		last = last[i+2:]
	}
	return strings.TrimSpace(last)
}

// classify says why an unmounted drive failed, from what the tools said:
// ntfsfix -n (read-only) for NTFS, the kernel log, our mount's error.
func classify(name, fstype, dev, ntfsfixOut string, ntfsfixErr error, klog, mountErr string) *model.DriveProblem {
	klogLine := kernelLine(klog, dev)
	detail := klogLine
	if detail == "" {
		detail = mountErr
	}
	switch {
	case isNTFS(fstype) && ntfsfixErr != nil:
		line := ""
		for _, l := range strings.Split(ntfsfixOut, "\n") {
			l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "Mounting volume..."))
			low := strings.ToLower(l)
			if strings.Contains(low, "does not match") || strings.Contains(low, "error") || strings.Contains(low, "corrupt") || strings.Contains(low, "inconsistent") {
				line = l
				break
			}
		}
		if line == "" {
			line = detail
		}
		return &model.DriveProblem{Reason: "damaged", Repairable: true, Detail: line,
			Message: name + " couldn't be mounted: its file system is damaged, most likely by a power cut or an unclean shutdown. Repair drive fixes the usual damage (Windows chkdsk can check it fully)."}
	case isNTFS(fstype) && strings.Contains(klog, "ntfs3("+dev+"): volume is dirty"):
		return &model.DriveProblem{Reason: "dirty", Repairable: true, Detail: "volume is dirty",
			Message: name + " couldn't be mounted after an unclean shutdown (a power cut, or unplugged without ejecting). Repair drive clears this."}
	case isNTFS(fstype) || isExt(fstype):
		return &model.DriveProblem{Reason: "failed", Repairable: true, Detail: detail,
			Message: name + " couldn't be mounted. After a power cut its file system usually needs a check: Repair drive runs one."}
	}
	return &model.DriveProblem{Reason: "failed", Detail: detail,
		Message: name + " couldn't be mounted. Check the drive (its " + fstype + " file system can't be repaired here)."}
}

func mountsAtBoot(e *fstab.Entry) bool {
	_, atBoot, _ := deriveFlags(e)
	return e.Enabled && atBoot
}

// driveProblem: nil when e is fine (mounted, not meant to mount at boot,
// or unmounted on purpose).
func driveProblem(e *fstab.Entry, blk *model.LSBLKModel, mounted bool) *model.DriveProblem {
	if mounted {
		setMountErr(e.MountPoint, "") // a later unmount starts fresh
	}
	if !e.Managed || !mountsAtBoot(e) || mounted {
		return nil
	}
	name := driveName(e.MountPoint)
	if blk == nil {
		return &model.DriveProblem{Reason: "missing",
			Message: name + " isn't connected (or was reformatted, so its ID changed). Apps that keep files on it wait until it is back."}
	}
	drives.Lock()
	mountErr, cached := drives.mountErr[e.MountPoint], drives.probe[e.MountPoint]
	drives.Unlock()
	if mountErr == "" && mountUnitState(e.MountPoint) != "failed" {
		return nil
	}
	if cached != nil {
		return cached
	}
	var out string
	var err error
	if isNTFS(blk.FsType) {
		out, err = runTool(2*time.Minute, "ntfsfix", "-n", blk.Path)
	}
	p := classify(name, blk.FsType, filepath.Base(blk.Path), out, err, kernelLog(), mountErr)
	drives.Lock()
	drives.probe[e.MountPoint] = p
	drives.Unlock()
	return p
}

// attachDriveState fills Problem and Repair of managed mounts.
func attachDriveState(list []model.FstabMount, entries []*fstab.Entry, blkList []model.LSBLKModel) {
	byMP := map[string]*fstab.Entry{}
	for _, e := range entries {
		byMP[e.MountPoint] = e
	}
	for i := range list {
		m := &list[i]
		if e := byMP[m.MountPoint]; e != nil {
			m.Problem = driveProblem(e, findBlockDeviceByUUID(blkList, m.UUID), m.Mounted)
		}
		drives.Lock()
		if r := drives.repair[m.MountPoint]; r != nil {
			c := *r
			m.Repair = &c
		}
		drives.Unlock()
	}
}

func bootedWithin(d time.Duration) bool {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return false
	}
	s, err := strconv.ParseFloat(strings.Fields(string(raw))[0], 64)
	return err == nil && time.Duration(s*float64(time.Second)) < d
}

// RetryFstabMounts mounts, once at start, managed drives whose boot mount
// failed (or never happened: the drive came up late). One unmounted on
// purpose later on is left alone: its unit is inactive, and the system has
// been up a while by then.
func (d *diskService) RetryFstabMounts() {
	entries, err := fstab.Get().GetEntries()
	if err != nil {
		logger.Error("drive check: couldn't read fstab", zap.Error(err))
		return
	}
	mounted, _ := currentMountPoints()
	blkList := d.LSBLK(false)
	early := bootedWithin(10 * time.Minute)
	for _, e := range entries {
		if !e.Managed || !mountsAtBoot(e) || mounted[e.MountPoint] || findBlockDeviceByUUID(blkList, extractUUID(e.Source)) == nil {
			continue
		}
		if st := mountUnitState(e.MountPoint); st != "failed" && !(early && st == "inactive") {
			continue
		}
		if err := d.MountFstabEntry(e.MountPoint); err != nil {
			logger.Error("drive check: a drive that should be mounted couldn't be", zap.String("mountPoint", e.MountPoint), zap.Error(err))
		} else {
			logger.Info("drive check: mounted a drive its boot mount had missed", zap.String("mountPoint", e.MountPoint))
		}
	}
}

// CheckDriveProblems sends each new problem to the notification feed once
// (again only after it was fixed and came back).
func (d *diskService) CheckDriveProblems() {
	// The usual case, cheaply (the full list runs lsblk and smartctl).
	entries, err := fstab.Get().GetAllEntries()
	mounted, err2 := currentMountPoints()
	if err != nil || err2 != nil {
		return
	}
	allFine := true
	for _, e := range entries {
		if mounted[e.MountPoint] {
			setMountErr(e.MountPoint, "") // a later unmount starts fresh
		} else if e.Managed && mountsAtBoot(e) {
			allFine = false
		}
	}
	if allFine {
		drives.Lock()
		drives.notified = map[string]string{}
		drives.Unlock()
		return
	}
	list, err := d.ListFstabMounts()
	if err != nil {
		return
	}
	for _, m := range list {
		drives.Lock()
		was := drives.notified[m.MountPoint]
		if m.Problem == nil {
			delete(drives.notified, m.MountPoint)
		} else {
			drives.notified[m.MountPoint] = m.Problem.Reason
		}
		drives.Unlock()
		if m.Problem == nil || was == m.Problem.Reason {
			continue
		}
		p := m.Problem
		title, level := driveName(m.MountPoint)+" couldn't be mounted", "error"
		if p.Reason == "missing" {
			title, level = driveName(m.MountPoint)+" isn't connected", "warning"
		}
		msg := p.Message
		if p.Reason != "missing" {
			msg += " Apps that keep files on it wait until it is mounted."
		}
		if p.Repairable {
			msg += " Open Settings > Storage to repair it."
		}
		notifyDrive(driveNotifyProps(title, msg, level, m.MountPoint, p.Reason))
	}
}

func driveNotifyProps(title, message, level, mp, reason string) map[string]string {
	args, _ := json.Marshal(map[string]string{"mount_point": mp, "reason": reason})
	return map[string]string{
		"title": title, "message": message, "level": level, "category": "storage",
		"args": string(args), "action": `{"target":"settings","props":{"section":"storage"}}`,
	}
}

// StartDriveProblemWatcher checks every 30 s.
func (d *diskService) StartDriveProblemWatcher(ctx context.Context) {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			d.CheckDriveProblems()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// repairSteps: the commands that repair fstype on dev, nil when it can't
// be repaired here. ntfsfix fixes the usual NTFS damage ($MFTMirr, the
// journal) and -d then clears the dirty flag ntfs3 refuses; e2fsck -y
// fixes what it can on an unmounted ext drive.
func repairSteps(fstype, dev string) [][]string {
	switch {
	case isNTFS(fstype):
		return [][]string{{"ntfsfix", dev}, {"ntfsfix", "-d", dev}}
	case isExt(fstype):
		return [][]string{{"e2fsck", "-y", dev}}
	}
	return nil
}

// stepOK: e2fsck exits 1 (errors fixed) or 2 (fixed, reboot advised for a
// mounted root - never the case here) after a successful repair.
func stepOK(tool string, err error) bool {
	var ee *exec.ExitError
	if err == nil {
		return true
	}
	return tool == "e2fsck" && errors.As(err, &ee) && ee.ExitCode() < 4
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// RepairFstabDrive repairs an unmounted managed drive in the background
// and mounts it; Repair on the drive's mount shows how it went.
func (d *diskService) RepairFstabDrive(mp string) error {
	e, err := findManagedEntry(mp)
	if err != nil {
		return err
	}
	if e == nil {
		return newFstabError(common_err.FSTAB_ENTRY_NOT_FOUND, "no managed fstab entry at that mount point")
	}
	if mounted, _ := currentMountPoints(); mounted[mp] {
		return newFstabError(common_err.FSTAB_INVALID_FIELD, "the drive is mounted - nothing to repair")
	}
	blk := findBlockDeviceByUUID(d.LSBLK(false), extractUUID(e.Source))
	if blk == nil {
		return newFstabError(common_err.FSTAB_DEVICE_NOT_FOUND, "the drive isn't connected")
	}
	// A repair tool on a mounted file system destroys it.
	if in := NodeMountPoints(*blk); len(in) > 0 {
		return newFstabError(common_err.FSTAB_INVALID_FIELD, "the drive is mounted at "+in[0]+" - unmount it first")
	}
	steps := repairSteps(blk.FsType, blk.Path)
	if steps == nil {
		return newFstabError(common_err.FSTAB_INVALID_FIELD, "a "+blk.FsType+" drive can't be repaired here")
	}
	drives.Lock()
	if r := drives.repair[mp]; r != nil && r.Running {
		drives.Unlock()
		return newFstabError(common_err.FSTAB_INVALID_FIELD, "a repair of this drive is already running")
	}
	drives.repair[mp] = &model.DriveRepair{Running: true}
	drives.Unlock()

	go func() {
		name := driveName(mp)
		var out strings.Builder
		r := &model.DriveRepair{OK: true}
		for _, s := range steps {
			o, err := runTool(12*time.Hour, s[0], s[1:]...)
			fmt.Fprintf(&out, "$ %s\n%s\n", strings.Join(s, " "), strings.TrimSpace(o))
			if !stepOK(s[0], err) {
				r.OK, r.Message = false, name+" couldn't be repaired ("+s[0]+": "+err.Error()+"). Its file system needs a full check, e.g. chkdsk /f on Windows for NTFS."
				break
			}
		}
		if r.OK {
			if err := d.MountFstabEntry(mp); err != nil {
				r.OK, r.Message = false, name+" was repaired, but it still couldn't be mounted: "+err.Error()
			} else {
				r.Message = name + " was repaired and is mounted again."
			}
		}
		r.Output, r.FinishedAt = tail(out.String(), 4000), time.Now().Unix()
		drives.Lock()
		drives.repair[mp] = r
		delete(drives.probe, mp)
		drives.Unlock()
		logger.Info("drive repair finished", zap.String("mountPoint", mp), zap.Bool("ok", r.OK), zap.String("output", r.Output))
		title, level := name+" repaired", "success"
		if !r.OK {
			title, level = "Repairing "+name+" didn't work", "error"
		}
		notifyDrive(driveNotifyProps(title, r.Message, level, mp, "repair"))
	}()
	return nil
}
