package service

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/moby/sys/mountinfo"
	"go.uber.org/zap"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/fstab"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/mount"
)

// FstabAPIError carries a common_err code alongside a human-readable message, so a route
// handler can report the right HTTP status/code without every failure path having to be
// a distinct sentinel error.
type FstabAPIError struct {
	Code    int
	Message string
}

func (e *FstabAPIError) Error() string { return e.Message }

func newFstabError(code int, message string) *FstabAPIError {
	return &FstabAPIError{Code: code, Message: message}
}

// reservedMountPoints must never be handed to mount/fstab.Add, whether as the target of
// a new managed entry or as something a candidate partition happens to already be mounted
// at. This is deliberately a short, well-known system-paths denylist rather than an
// allowlist - the point of this feature is to let an admin pick any mount point they want
// (e.g. under /DATA, /mnt, /home, or anywhere else), the same way they could by hand-
// editing /etc/fstab; only paths that would break the running system are off-limits.
var reservedMountPoints = []string{
	"/", "/bin", "/boot", "/dev", "/etc", "/lib", "/lib64",
	"/proc", "/root", "/run", "/sbin", "/sys", "/usr", "/var",
}

// validFSTypeRe/validOptionsRe exist to keep a filesystem type or options string from
// ever containing a tab or newline (which would corrupt /etc/fstab's line-based format)
// or shell/format metacharacters - not because mount or the fstab writer are vulnerable
// to them (both are argv/field-based, never shell strings), but as basic input hygiene
// for a field that ends up persisted to a system config file.
var (
	validFSTypeRe  = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	validOptionsRe = regexp.MustCompile(`^[a-zA-Z0-9_,=./:-]*$`)
)

func isSafeMountPoint(mountPoint string) error {
	if mountPoint == "" || !path.IsAbs(mountPoint) {
		return fmt.Errorf("mount point must be an absolute path")
	}

	if cleaned := path.Clean(mountPoint); cleaned != mountPoint {
		return fmt.Errorf("mount point must be a clean absolute path with no '.', '..', or trailing slash")
	}

	for _, reserved := range reservedMountPoints {
		if mountPoint == reserved || strings.HasPrefix(mountPoint, reserved+"/") {
			return fmt.Errorf("%q is a reserved system path and cannot be used as a mount point", mountPoint)
		}
	}

	return nil
}

func isReservedMountPoint(mountPoint string) bool {
	if mountPoint == "" {
		return false
	}
	return isSafeMountPoint(mountPoint) != nil
}

// composeOptions builds the final fstab options string server-side from structured
// booleans plus an optional free-text "advanced" extra field, so the ro/rw and
// auto/noauto flags are always present exactly once regardless of what the caller typed
// in the advanced field.
func composeOptions(extra string, readOnly, mountAtBoot bool) string {
	tokens := []string{}

	if extra != "" {
		for _, t := range strings.Split(extra, ",") {
			t = strings.TrimSpace(t)
			switch t {
			case "", "ro", "rw", "auto", "noauto":
				continue
			}
			tokens = append(tokens, t)
		}
	}

	if readOnly {
		tokens = append(tokens, "ro")
	} else {
		tokens = append(tokens, "rw")
	}

	if !mountAtBoot {
		tokens = append(tokens, "noauto")
	}

	return strings.Join(tokens, ",")
}

func deriveFlags(e *fstab.Entry) (readOnly, mountAtBoot, checkAtBoot bool) {
	mountAtBoot = true

	for _, t := range strings.Split(e.Options, ",") {
		switch strings.TrimSpace(t) {
		case "ro":
			readOnly = true
		case "noauto":
			mountAtBoot = false
		}
	}

	checkAtBoot = e.Pass > 0
	return
}

func extractUUID(source string) string {
	if strings.HasPrefix(source, "UUID=") {
		return strings.TrimPrefix(source, "UUID=")
	}
	return ""
}

func findBlockDeviceByUUID(blkList []model.LSBLKModel, uuid string) *model.LSBLKModel {
	if uuid == "" {
		return nil
	}

	for i := range blkList {
		if blk := WalkDisk(blkList[i], 5, func(b model.LSBLKModel) bool {
			return b.UUID == uuid
		}); blk != nil {
			return blk
		}
	}

	return nil
}

func currentMountPoints() (map[string]bool, error) {
	mounts, err := mountinfo.GetMounts(nil)
	if err != nil {
		return nil, err
	}

	result := make(map[string]bool, len(mounts))
	for _, m := range mounts {
		result[m.Mountpoint] = true
	}

	return result, nil
}

// verifyMounted confirms mountPoint is actually an active mount after a mount.Mount call.
// This is necessary because every managed entry's options include "nofail" (so a missing
// removable drive never hangs boot) - and mount(8) honors "nofail" outside of boot too,
// silently returning success even when the source device can't be found or mounted at all
// (e.g. a stale/changed UUID after a reformat or drive swap). mount.Mount's own error return
// can't be trusted to catch that case, so an explicit, user-initiated mount/add/edit action
// must double-check reality before reporting success.
func verifyMounted(mountPoint string) error {
	mounted, err := currentMountPoints()
	if err != nil {
		return err
	}
	if !mounted[mountPoint] {
		return fmt.Errorf("the drive could not be mounted - it may be disconnected, or its filesystem may not be recognized")
	}
	return nil
}

func findManagedEntry(mountPoint string) (*fstab.Entry, error) {
	entries, err := fstab.Get().GetAllEntries()
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if e.MountPoint == mountPoint && e.Managed {
			return e, nil
		}
	}

	return nil, nil
}

func buildFstabMountView(e *fstab.Entry, blkList []model.LSBLKModel, activeMounts map[string]bool) model.FstabMount {
	readOnly, mountAtBoot, checkAtBoot := deriveFlags(e)
	uuid := extractUUID(e.Source)

	fm := model.FstabMount{
		MountPoint:  e.MountPoint,
		Source:      e.Source,
		UUID:        uuid,
		FSType:      e.FSType,
		Options:     e.Options,
		Dump:        e.Dump,
		Pass:        e.Pass,
		ReadOnly:    readOnly,
		MountAtBoot: mountAtBoot,
		CheckAtBoot: checkAtBoot,
		Managed:     e.Managed,
		Enabled:     e.Enabled,
		Mounted:     activeMounts[e.MountPoint],
	}

	if blk := findBlockDeviceByUUID(blkList, uuid); blk != nil {
		fm.DriveLabel = blk.Label
		fm.DrivePath = blk.Path
		fm.Size = blk.Size
	}

	return fm
}

func (d *diskService) getFstabMountByMountPoint(mountPoint string) (*model.FstabMount, error) {
	entry, err := findManagedEntry(mountPoint)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, newFstabError(common_err.FSTAB_ENTRY_NOT_FOUND, "entry not found")
	}

	activeMounts, err := currentMountPoints()
	if err != nil {
		logger.Error("error checking active mounts", zap.Error(err))
		activeMounts = map[string]bool{}
	}

	fm := buildFstabMountView(entry, d.LSBLK(false), activeMounts)
	return &fm, nil
}

// ListFstabMounts returns every fstab entry NivaroOS created (enabled or disabled).
// Pre-existing system entries are intentionally excluded here - see ListFstabSystemEntries.
func (d *diskService) ListFstabMounts() ([]model.FstabMount, error) {
	entries, err := fstab.Get().GetAllEntries()
	if err != nil {
		return nil, err
	}

	activeMounts, err := currentMountPoints()
	if err != nil {
		logger.Error("error listing active mounts for fstab view", zap.Error(err))
		activeMounts = map[string]bool{}
	}

	blkList := d.LSBLK(false)

	result := make([]model.FstabMount, 0, len(entries))
	for _, e := range entries {
		if !e.Managed {
			continue
		}
		result = append(result, buildFstabMountView(e, blkList, activeMounts))
	}

	return result, nil
}

// ListFstabSystemEntries returns active, non-NivaroOS fstab entries (root, swap, EFI, or
// anything an admin added by hand) purely for read-only display - never mutated here.
func (d *diskService) ListFstabSystemEntries() ([]model.FstabMount, error) {
	entries, err := fstab.Get().GetEntries()
	if err != nil {
		return nil, err
	}

	activeMounts, err := currentMountPoints()
	if err != nil {
		logger.Error("error listing active mounts for fstab system view", zap.Error(err))
		activeMounts = map[string]bool{}
	}

	blkList := d.LSBLK(false)

	result := make([]model.FstabMount, 0, len(entries))
	for _, e := range entries {
		if e.Managed {
			continue
		}
		result = append(result, buildFstabMountView(e, blkList, activeMounts))
	}

	return result, nil
}

// ListFstabCandidates returns already-formatted partitions that aren't yet managed by
// this feature and aren't a reserved system/boot/swap volume - i.e. what the "Add drive"
// picker should offer. A partition with no filesystem yet is not included: it needs to be
// formatted first (via the existing Disks panel), which is a separate, destructive step
// this feature deliberately does not perform.
func (d *diskService) ListFstabCandidates() ([]model.FstabCandidate, error) {
	entries, err := fstab.Get().GetAllEntries()
	if err != nil {
		return nil, err
	}

	managedUUIDs := make(map[string]bool, len(entries))
	for _, e := range entries {
		if uuid := extractUUID(e.Source); uuid != "" {
			managedUUIDs[uuid] = true
		}
	}

	activeMounts, err := currentMountPoints()
	if err != nil {
		logger.Error("error listing active mounts for fstab candidates", zap.Error(err))
		activeMounts = map[string]bool{}
	}

	candidates := []model.FstabCandidate{}

	for _, disk := range d.LSBLK(false) {
		children := disk.Children
		if len(children) == 0 && IsDiskSupported(disk) {
			children = []model.LSBLKModel{disk}
		}

		for _, child := range children {
			if child.UUID == "" || child.FsType == "" || child.FsType == "swap" {
				continue
			}
			if managedUUIDs[child.UUID] {
				continue
			}
			if isReservedMountPoint(child.MountPoint) {
				continue
			}

			candidates = append(candidates, model.FstabCandidate{
				Path:        child.Path,
				UUID:        child.UUID,
				Label:       child.Label,
				FSType:      child.FsType,
				Size:        child.Size,
				ParentModel: disk.Model,
				Mounted:     child.MountPoint != "" && activeMounts[child.MountPoint],
				MountPoint:  child.MountPoint,
			})
		}
	}

	return candidates, nil
}

// AddFstabMount validates the request, test-mounts the chosen drive to confirm the
// parameters actually work, and only then writes the /etc/fstab entry - so a bad fstype
// or options string never ends up persisted. If the drive already happens to be mounted
// exactly where the caller wants it (e.g. via the older DB-backed "nivaroos" persistence
// mechanism), the test-mount step is skipped and the existing mount is simply adopted
// into a real fstab entry instead of being torn down and remounted.
func (d *diskService) AddFstabMount(req model.AddFstabMountRequest) (*model.FstabMount, error) {
	if err := isSafeMountPoint(req.MountPoint); err != nil {
		return nil, newFstabError(common_err.FSTAB_MOUNT_POINT_UNSAFE, err.Error())
	}

	if req.UUID == "" {
		return nil, newFstabError(common_err.FSTAB_INVALID_FIELD, "a drive UUID is required")
	}

	if req.FSType != "" && !validFSTypeRe.MatchString(req.FSType) {
		return nil, newFstabError(common_err.FSTAB_INVALID_FIELD, "invalid filesystem type")
	}

	if !validOptionsRe.MatchString(req.Options) {
		return nil, newFstabError(common_err.FSTAB_INVALID_FIELD, "invalid mount options")
	}

	if existing, err := fstab.Get().GetEntryByMountPoint(req.MountPoint); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, newFstabError(common_err.FSTAB_ENTRY_EXISTS, "that mount point is already used by another fstab entry")
	}

	allEntries, err := fstab.Get().GetAllEntries()
	if err != nil {
		return nil, err
	}
	for _, e := range allEntries {
		if e.MountPoint == req.MountPoint {
			return nil, newFstabError(common_err.FSTAB_ENTRY_EXISTS, "that mount point is already used by a disabled fstab entry - remove or re-enable it first")
		}
	}

	blk := findBlockDeviceByUUID(d.LSBLK(false), req.UUID)
	if blk == nil {
		return nil, newFstabError(common_err.FSTAB_DEVICE_NOT_FOUND, "drive not found")
	}

	fstype := req.FSType
	if fstype == "" {
		fstype = blk.FsType
	}
	if fstype == "" {
		return nil, newFstabError(common_err.FSTAB_INVALID_FIELD, "could not detect a filesystem type for this drive - please specify one")
	}

	source := "UUID=" + req.UUID
	options := composeOptions(req.Options, req.ReadOnly, req.MountAtBoot)
	pass := 0
	if req.CheckAtBoot {
		pass = 2
	}

	activeMounts, err := currentMountPoints()
	if err != nil {
		logger.Error("error checking active mounts before adding fstab entry", zap.Error(err))
		activeMounts = map[string]bool{}
	}

	alreadyMountedHere := blk.MountPoint == req.MountPoint && activeMounts[req.MountPoint]

	if !alreadyMountedHere {
		if err := file.IsNotExistMkDir(req.MountPoint); err != nil {
			return nil, fmt.Errorf("could not create mount point directory: %w", err)
		}

		if empty, err := file.IsDirEmpty(req.MountPoint); err != nil {
			return nil, fmt.Errorf("could not inspect mount point directory: %w", err)
		} else if !empty {
			return nil, newFstabError(common_err.FSTAB_INVALID_FIELD, "mount point directory is not empty")
		}

		fstypeArg, optionsArg := fstype, options
		if err := mount.Mount(source, req.MountPoint, &fstypeArg, &optionsArg); err != nil {
			return nil, newFstabError(common_err.FSTAB_TEST_MOUNT_FAILED, err.Error())
		}
		if err := verifyMounted(req.MountPoint); err != nil {
			return nil, newFstabError(common_err.FSTAB_TEST_MOUNT_FAILED, err.Error())
		}
	}

	entry := fstab.Entry{
		Source:     source,
		MountPoint: req.MountPoint,
		FSType:     fstype,
		Options:    options,
		Dump:       0,
		Pass:       pass,
	}

	if err := fstab.Get().Add(entry, false); err != nil {
		if !alreadyMountedHere {
			if uErr := mount.UmountByMountPoint(req.MountPoint); uErr != nil {
				logger.Error("error rolling back test mount after failed fstab write", zap.Error(uErr), zap.String("mount point", req.MountPoint))
			}
		}
		return nil, err
	}

	return d.getFstabMountByMountPoint(req.MountPoint)
}

// UpdateFstabMount changes an existing managed entry's options/mount point. If the
// drive is currently mounted, it is unmounted and remounted with the new parameters; if
// the new parameters fail to mount, the previous fstab line is left in place untouched
// (only the live mount was torn down), matching how this service's v2 UpdateMount
// endpoint already behaves in the same failure case.
func (d *diskService) UpdateFstabMount(req model.UpdateFstabMountRequest) (*model.FstabMount, error) {
	existing, err := findManagedEntry(req.MountPoint)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, newFstabError(common_err.FSTAB_ENTRY_NOT_FOUND, "no managed fstab entry at that mount point")
	}

	newMountPoint := req.NewMountPoint
	if newMountPoint == "" {
		newMountPoint = req.MountPoint
	}
	if err := isSafeMountPoint(newMountPoint); err != nil {
		return nil, newFstabError(common_err.FSTAB_MOUNT_POINT_UNSAFE, err.Error())
	}

	renaming := newMountPoint != req.MountPoint
	if renaming {
		if conflict, err := fstab.Get().GetEntryByMountPoint(newMountPoint); err != nil {
			return nil, err
		} else if conflict != nil {
			return nil, newFstabError(common_err.FSTAB_ENTRY_EXISTS, "that mount point is already used by another fstab entry")
		}
	}

	fstype := req.FSType
	if fstype == "" {
		fstype = existing.FSType
	}
	if !validFSTypeRe.MatchString(fstype) {
		return nil, newFstabError(common_err.FSTAB_INVALID_FIELD, "invalid filesystem type")
	}
	if !validOptionsRe.MatchString(req.Options) {
		return nil, newFstabError(common_err.FSTAB_INVALID_FIELD, "invalid mount options")
	}

	options := composeOptions(req.Options, req.ReadOnly, req.MountAtBoot)
	pass := 0
	if req.CheckAtBoot {
		pass = 2
	}

	mounted, err := currentMountPoints()
	if err != nil {
		logger.Error("error checking active mounts before updating fstab entry", zap.Error(err))
		mounted = map[string]bool{}
	}
	if mounted[req.MountPoint] {
		if err := mount.UmountByMountPoint(req.MountPoint); err != nil {
			return nil, fmt.Errorf("could not unmount %s to apply changes: %w", req.MountPoint, err)
		}
	}

	if err := file.IsNotExistMkDir(newMountPoint); err != nil {
		return nil, fmt.Errorf("could not create mount point directory: %w", err)
	}
	if renaming {
		if empty, err := file.IsDirEmpty(newMountPoint); err != nil {
			return nil, fmt.Errorf("could not inspect mount point directory: %w", err)
		} else if !empty {
			return nil, newFstabError(common_err.FSTAB_INVALID_FIELD, "mount point directory is not empty")
		}
	}

	fstypeArg, optionsArg := fstype, options
	if err := mount.Mount(existing.Source, newMountPoint, &fstypeArg, &optionsArg); err != nil {
		return nil, newFstabError(common_err.FSTAB_TEST_MOUNT_FAILED, err.Error())
	}
	if err := verifyMounted(newMountPoint); err != nil {
		return nil, newFstabError(common_err.FSTAB_TEST_MOUNT_FAILED, err.Error())
	}

	if err := fstab.Get().RemoveByMountPoint(req.MountPoint, false); err != nil {
		return nil, err
	}

	newEntry := fstab.Entry{
		Source:     existing.Source,
		MountPoint: newMountPoint,
		FSType:     fstype,
		Options:    options,
		Dump:       0,
		Pass:       pass,
	}
	if err := fstab.Get().Add(newEntry, false); err != nil {
		return nil, err
	}

	return d.getFstabMountByMountPoint(newMountPoint)
}

// RemoveFstabMount unmounts (if currently mounted) and permanently deletes a managed
// entry. Refuses to act on anything that isn't Managed.
func (d *diskService) RemoveFstabMount(mountPoint string) error {
	existing, err := findManagedEntry(mountPoint)
	if err != nil {
		return err
	}
	if existing == nil {
		return newFstabError(common_err.FSTAB_ENTRY_NOT_FOUND, "no managed fstab entry at that mount point")
	}

	mounted, err := currentMountPoints()
	if err != nil {
		logger.Error("error checking active mounts before removing fstab entry", zap.Error(err))
		mounted = map[string]bool{}
	}
	if mounted[mountPoint] {
		if err := mount.UmountByMountPoint(mountPoint); err != nil {
			return fmt.Errorf("could not unmount %s: %w", mountPoint, err)
		}
	}

	if !existing.Enabled {
		// A disabled entry is commented out, so RemoveByMountPoint's normal (active-line-
		// only) matching would silently no-op on it - uncomment it first so it can be found.
		if err := fstab.Get().Enable(mountPoint); err != nil {
			return err
		}
	}

	return fstab.Get().RemoveByMountPoint(mountPoint, false)
}

// SetFstabMountEnabled toggles whether a managed entry is active at next boot, without
// touching whatever is currently mounted right now - exactly like commenting/uncommenting
// a line in /etc/fstab by hand.
func (d *diskService) SetFstabMountEnabled(mountPoint string, enabled bool) error {
	existing, err := findManagedEntry(mountPoint)
	if err != nil {
		return err
	}
	if existing == nil {
		return newFstabError(common_err.FSTAB_ENTRY_NOT_FOUND, "no managed fstab entry at that mount point")
	}

	if enabled == existing.Enabled {
		return nil
	}

	if enabled {
		return fstab.Get().Enable(mountPoint)
	}
	return fstab.Get().RemoveByMountPoint(mountPoint, true)
}

// MountFstabEntry mounts a configured fstab entry on demand.
func (d *diskService) MountFstabEntry(mountPoint string) error {
	entry, err := fstab.Get().GetEntryByMountPoint(mountPoint)
	if err != nil {
		return err
	}
	if entry == nil {
		allEntries, err := fstab.Get().GetAllEntries()
		if err == nil {
			for _, e := range allEntries {
				if e.MountPoint == mountPoint {
					entry = e
					break
				}
			}
		}
	}
	if entry == nil {
		return newFstabError(common_err.FSTAB_ENTRY_NOT_FOUND, "fstab entry not found")
	}

	activeMounts, _ := currentMountPoints()
	if activeMounts[mountPoint] {
		return nil
	}

	if err := file.IsNotExistMkDir(mountPoint); err != nil {
		return fmt.Errorf("could not create mount point directory: %w", err)
	}

	fstypeArg, optionsArg := entry.FSType, entry.Options
	if err := mount.Mount(entry.Source, entry.MountPoint, &fstypeArg, &optionsArg); err != nil {
		return newFstabError(common_err.FSTAB_TEST_MOUNT_FAILED, err.Error())
	}
	if err := verifyMounted(entry.MountPoint); err != nil {
		return newFstabError(common_err.FSTAB_TEST_MOUNT_FAILED, err.Error())
	}

	return nil
}

// UmountFstabEntry unmounts an active fstab entry.
func (d *diskService) UmountFstabEntry(mountPoint string) error {
	if isReservedMountPoint(mountPoint) {
		return newFstabError(common_err.FSTAB_MOUNT_POINT_UNSAFE, "cannot unmount a system root or boot directory")
	}

	activeMounts, _ := currentMountPoints()
	if !activeMounts[mountPoint] {
		return nil
	}

	if err := mount.UmountByMountPoint(mountPoint); err != nil {
		return fmt.Errorf("could not unmount %s: %w", mountPoint, err)
	}

	return nil
}

// AdoptFstabEntry converts an unmanaged system fstab entry into a managed one.
func (d *diskService) AdoptFstabEntry(mountPoint string) (*model.FstabMount, error) {
	if isReservedMountPoint(mountPoint) {
		return nil, newFstabError(common_err.FSTAB_MOUNT_POINT_UNSAFE, "cannot adopt a system root or boot partition")
	}

	if err := fstab.Get().Adopt(mountPoint); err != nil {
		return nil, err
	}

	return d.getFstabMountByMountPoint(mountPoint)
}

