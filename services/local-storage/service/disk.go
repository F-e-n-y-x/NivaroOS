package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/constants"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/codegen/message_bus"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/common"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/fstab"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/mount"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/partition"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/utils/command"

	"github.com/moby/sys/mountinfo"

	model2 "github.com/F-e-n-y-x/NivaroOS/services/local-storage/service/model"
	v2 "github.com/F-e-n-y-x/NivaroOS/services/local-storage/service/v2"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service/v2/fs"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type DiskService interface {
	// FstabManagedMountPoint returns a mount point of d that has an fstab
	// entry (managed in Persistent Mounts), or "".
	FstabManagedMountPoint(d model.LSBLKModel) string
	EnsureDefaultMergePoint() bool
	// GuardDiskOperation: the one safety check for create/mount/format/umount
	// (see disk_guard.go). Errors are *InvalidDeviceError (400) or *GuardError (409).
	GuardDiskOperation(path string, op DiskOp) (disk model.LSBLKModel, node model.LSBLKModel, err error)
	// ResolveListedBlockDevice: path is a valid, lsblk-listed block device.
	ResolveListedBlockDevice(path string) (disk model.LSBLKModel, node model.LSBLKModel, err error)
	AddPartition(path string) error
	DeletePartition(path string) error
	CheckSerialDiskMount()
	FormatDisk(path string) error
	GetDiskInfo(path string) model.LSBLKModel
	GetPersistentTypeByUUID(uuid string) string
	GetUSBDriveStatusList() []model.USBDriveStatus
	LSBLK(isUseCache bool) []model.LSBLKModel
	MountDisk(path, volume string) (string, error)
	RemoveLSBLKCache()
	SmartCTL(path string) model.SmartctlA
	SmartCTLFull(path string) model.SmartctlA
	SmartTest(path, testType string) error
	// ClassifyUSBTransport reports "usb3" for a USB-attached disk whose real
	// sysfs link speed is SuperSpeed (5 Gbit/s) or faster, otherwise "usb" -
	// see service/usb_speed.go.
	ClassifyUSBTransport(path string) string
	GetStandby(path string) int
	SetStandby(path string, minutes int) error
	UmountPointAndRemoveDir(m model.LSBLKModel) error
	UmountUSB(path string) error

	UpdateMountPointInDB(m model2.Volume) error
	DeleteMountPointFromDB(path, mountPoint string) error
	GetSerialAllFromDB() ([]model2.Volume, error)
	SaveMountPointToDB(m model2.Volume) error
	InitCheck()
	GetSystemDf() (model.DFDiskSpace, error)

	// fstab-backed mount management: real /etc/fstab entries the UI can create, edit,
	// disable, and remove, as an alternative to the DB-backed "nivaroos" persistence
	// mechanism above.
	ListFstabMounts() ([]model.FstabMount, error)
	ListFstabSystemEntries() ([]model.FstabMount, error)
	ListFstabCandidates() ([]model.FstabCandidate, error)
	AddFstabMount(req model.AddFstabMountRequest) (*model.FstabMount, error)
	UpdateFstabMount(req model.UpdateFstabMountRequest) (*model.FstabMount, error)
	RemoveFstabMount(mountPoint string) error
	SetFstabMountEnabled(mountPoint string, enabled bool) error
	MountFstabEntry(mountPoint string) error
	UmountFstabEntry(mountPoint string) error
	AdoptFstabEntry(mountPoint string) (*model.FstabMount, error)
}

type diskService struct {
	db *gorm.DB
}

const (
	PersistentTypeNone     = "none"
	PersistentTypeFStab    = "fstab"
	PersistentTypeNivaroOS = "nivaroos"
)

var (
	ErrVolumeWithEmptyUUID = errors.New("cannot save volume with empty uuid")
	json2                  = jsoniter.ConfigCompatibleWithStandardLibrary
)

func (d *diskService) EnsureDefaultMergePoint() bool {
	mountPoint := "/DATA"
	sourceBasePath := constants.DefaultFilePath

	logger.Info("ensure default merge point exists", zap.String("mount point", mountPoint), zap.String("sourceBasePath", sourceBasePath))

	existingMerges, err := MyService.LocalStorage().GetMergeAllFromDB(&mountPoint)
	if err != nil {
		panic(err)
	}

	// check if /DATA is already a merge point
	if len(existingMerges) > 0 {
		if len(existingMerges) > 1 {
			logger.Error("more than one merge point with the same mount point found", zap.String("mount point", mountPoint))
		}
		return true
	}

	merge := &model2.Merge{
		FSType:         fs.MergerFSFullName,
		MountPoint:     mountPoint,
		SourceBasePath: &sourceBasePath,
	}

	if err := MyService.LocalStorage().CreateMerge(merge); err != nil {
		if errors.Is(err, v2.ErrMergeMountPointAlreadyExists) {
			logger.Info(err.Error(), zap.String("mount point", mountPoint))
		} else if errors.Is(err, v2.ErrMountPointIsNotEmpty) {
			logger.Error("Mount point "+mountPoint+" is not empty", zap.String("mount point", mountPoint))
			return false
		} else {
			panic(err)
		}
	}

	if err := MyService.LocalStorage().CreateMergeInDB(merge); err != nil {
		panic(err)
	}

	return true
}
func (d *diskService) RemoveLSBLKCache() {
	key := "system_lsblk"
	Cache.Delete(key)
}

func (d *diskService) ClassifyUSBTransport(path string) string {
	return classifyUSBTransport(path)
}

// isCurrentMountPoint: path is exactly a mount point right now (read from
// the kernel's mount table, not trusted from the request).
func isCurrentMountPoint(path string) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	raw, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) > 4 && strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\\`).Replace(f[4]) == path {
			return true
		}
	}
	return false
}

// isSystemMountPoint: a mount the running system can't lose.
func isSystemMountPoint(mp string) bool {
	switch mp {
	case "/", "/boot", "/boot/efi", "/efi", "[SWAP]", "/usr", "/var", "/home", "/opt", "/srv":
		return true
	}
	return false
}

// NodeMountPoints: every mount point lsblk reports for this node.
func NodeMountPoints(m model.LSBLKModel) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(m.MountPoint)
	for _, p := range m.MountPoints {
		if p != nil {
			add(*p)
		}
	}
	return out
}

// diskHoldsSystem: the disk carries /, /boot, /boot/efi or swap - removing
// (unmounting) it from the UI would break the running system.
func diskHoldsSystem(d model.LSBLKModel) bool {
	for _, mp := range NodeMountPoints(d) {
		if isSystemMountPoint(mp) {
			return true
		}
	}
	for _, c := range d.Children {
		if diskHoldsSystem(c) {
			return true
		}
	}
	return false
}

// DiskHoldsSystem is diskHoldsSystem for the routes.
func DiskHoldsSystem(d model.LSBLKModel) bool { return diskHoldsSystem(d) }

func (d *diskService) FstabManagedMountPoint(disk model.LSBLKModel) string {
	entries, err := fstab.Get().GetAllEntries()
	if err != nil {
		return ""
	}
	var mps []string
	var walk func(m model.LSBLKModel)
	walk = func(m model.LSBLKModel) {
		if m.MountPoint != "" {
			mps = append(mps, m.MountPoint)
		}
		for _, c := range m.Children {
			walk(c)
		}
	}
	walk(disk)
	for _, e := range entries {
		for _, mp := range mps {
			if e != nil && e.MountPoint == mp {
				return mp
			}
		}
	}
	return ""
}

// removeEmptyMountDir removes a mount folder after unmounting - only if it
// is empty. (RemoveAll deleted whatever was still there if the unmount
// hadn't really happened.)
func removeEmptyMountDir(dir string) {
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		logger.Info("mount folder kept (not empty)", zap.String("dir", dir))
	}
}

func (d *diskService) UmountUSB(path string) error {
	// A real mount point only, and the path goes to udevil as one argument
	// (it was pasted into a bash command line: `;cmd` ran as root, and a
	// label with a space couldn't be ejected).
	if !isCurrentMountPoint(path) {
		return fmt.Errorf("%s is not a mounted drive", path)
	}
	if out, err := exec.Command("udevil", "umount", "-f", path).CombinedOutput(); err != nil {
		if out2, err2 := exec.Command("umount", path).CombinedOutput(); err2 != nil {
			return fmt.Errorf("couldn't eject: %s", strings.TrimSpace(string(out)+" "+string(out2)))
		}
	}

	return nil
}

const (
	smartCacheTTL        = 10 * time.Minute // was 24h: a failing drive stayed "healthy" for a day
	smartSleepingTTL     = 2 * time.Minute  // re-check soon; -n standby doesn't wake the drive
	smartLastKnownTTL    = 7 * 24 * time.Hour
	smartCacheKeyPrefix  = "system_smart_"
	smartLastKnownPrefix = "system_smart_last_"
)

// IsSmartStandby: smartctl -n standby skipped the drive because it is asleep.
func IsSmartStandby(m model.SmartctlA) bool {
	for _, v := range m.Smartctl.Messages {
		s := strings.ToUpper(v.String)
		if strings.Contains(s, "STANDBY") || strings.Contains(s, "SLEEP") {
			return true
		}
	}
	return false
}

// SmartForSleepingDrive: a sleeping drive keeps its last awake reading
// (marked Sleeping+StaleHealth), or, never read awake, is "unknown" -
// never "unhealthy".
func SmartForSleepingDrive(cur, prev model.SmartctlA, havePrev bool) model.SmartctlA {
	if havePrev {
		prev.Sleeping = true
		prev.StaleHealth = true
		return prev
	}
	cur.Sleeping = true
	cur.StaleHealth = false
	return cur
}

// SmartHealth: "true", "false" or "unknown" (asleep, never read awake).
// An empty result (smartctl missing/unsupported) stays "true", as before.
func SmartHealth(m model.SmartctlA) string {
	if m.Sleeping && !m.StaleHealth {
		return "unknown"
	}
	if reflect.DeepEqual(m, model.SmartctlA{}) {
		return "true"
	}
	return strconv.FormatBool(m.SmartStatus.Passed)
}

func (d *diskService) SmartCTL(path string) model.SmartctlA {
	key := smartCacheKeyPrefix + path
	if result, ok := Cache.Get(key); ok {
		if res, ok := result.(model.SmartctlA); ok {
			return res
		}
	}
	var m model.SmartctlA
	buf := command.ExecSmartCTLByPath(path)
	if buf == nil {
		logger.Error("failed to exec shell - smartctl exec error", zap.String("path", path))
		Cache.Set(key, m, smartCacheTTL)
		return m
	}

	if err := json2.Unmarshal(buf, &m); err != nil {
		logger.Error("failed to unmarshal json", zap.Error(err), zap.String("json", string(buf)))
	}

	if IsSmartStandby(m) {
		var prev model.SmartctlA
		havePrev := false
		if v, ok := Cache.Get(smartLastKnownPrefix + path); ok {
			prev, havePrev = v.(model.SmartctlA)
		}
		m = SmartForSleepingDrive(m, prev, havePrev)
		Cache.Set(key, m, smartSleepingTTL)
		return m
	}

	if !reflect.DeepEqual(m, model.SmartctlA{}) {
		Cache.Set(key, m, smartCacheTTL)
		Cache.Set(smartLastKnownPrefix+path, m, smartLastKnownTTL)
	}
	return m
}

// SmartCTLFull is an uncached, always-fresh SMART read - used for the
// explicit "show drive info" view and for polling self-test progress, where
// a stale cached result (up to 24h old, see SmartCTL above) would be wrong.
func (d *diskService) SmartCTLFull(path string) model.SmartctlA {
	var m model.SmartctlA
	buf := command.ExecSmartCTLFullByPath(path)
	if buf == nil {
		logger.Error("failed to exec shell - smartctl exec error")
		return m
	}
	if err := json2.Unmarshal(buf, &m); err != nil {
		logger.Error("failed to unmarshal json", zap.Error(err), zap.String("json", string(buf)))
	}
	return m
}

func (d *diskService) SmartTest(path, testType string) error {
	if testType != "short" && testType != "long" {
		return fmt.Errorf("invalid self-test type: %s", testType)
	}
	output, err := command.ExecSmartCTLSelfTest(path, testType)
	if err != nil {
		// smartctl's exit status alone (e.g. "exit status 4") isn't useful to
		// show a user - the actual reason (e.g. "Self-test functions not
		// supported") is in its own output.
		if trimmed := strings.TrimSpace(output); trimmed != "" {
			return fmt.Errorf("%s", trimmed)
		}
		return err
	}
	return nil
}

// standbyDisk: path must be a whole disk listed by lsblk (it is written into
// a udev rule / passed to hdparm - the old code put the raw request string
// into /etc/hdparm.conf, so a newline injected config).
func (d *diskService) standbyDisk(path string) error {
	disk, node, err := d.ResolveListedBlockDevice(path)
	if err != nil {
		return err
	}
	if node.Path != disk.Path || (node.Type != "" && node.Type != "disk") {
		return &InvalidDeviceError{Msg: path + " is not a whole disk"}
	}
	return nil
}

func udevPropertiesOf(path string) map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "udevadm", "info", "--query=property", "--name="+path).Output()
	if err != nil {
		return map[string]string{}
	}
	return ParseUdevProperties(string(out))
}

// GetStandby returns the currently configured spindown timer for path, in
// minutes (0 = disabled/never configured).
func (d *diskService) GetStandby(path string) int {
	if err := d.standbyDisk(path); err != nil {
		return 0
	}
	if key, value, err := StandbyRuleKey(udevPropertiesOf(path)); err == nil {
		if raw, err := os.ReadFile(standbyRulesPath); err == nil {
			if code, ok := ReadStandbyRuleCode(string(raw), key, value); ok {
				return spindownCodeToMinutes(code)
			}
		}
	}
	// legacy (Debian hdparm.conf, written by older versions)
	code, ok := readHdparmSpindownCode(resolveStableDiskID(path))
	if !ok {
		return 0
	}
	return spindownCodeToMinutes(code)
}

// SetStandby persists a spindown timer for path as a udev rule keyed by the
// disk's serial (see standbyRulesPath - works on any udev distro, not just
// Debian's hdparm.conf) and applies it immediately via hdparm -S.
func (d *diskService) SetStandby(path string, minutes int) error {
	if err := d.standbyDisk(path); err != nil {
		return err
	}
	if minutes < 0 || minutes > 330 {
		return &InvalidDeviceError{Msg: "minutes must be between 0 and 330"}
	}
	hdparm, err := exec.LookPath("hdparm")
	if err != nil {
		return errors.New("hdparm is not installed")
	}
	if abs, err := filepath.Abs(hdparm); err == nil {
		hdparm = abs
	}
	key, value, err := StandbyRuleKey(udevPropertiesOf(path))
	if err != nil {
		return err
	}
	code := minutesToSpindownCode(minutes)

	raw, err := os.ReadFile(standbyRulesPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content, err := UpsertStandbyRule(string(raw), key, value, hdparm, code)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(standbyRulesPath), 0o755); err != nil {
		return err
	}
	tmp := standbyRulesPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, standbyRulesPath); err != nil {
		return err
	}
	_ = exec.Command("udevadm", "control", "--reload").Run()

	if err := removeHdparmConfSpindown(hdparmConfPath, resolveStableDiskID(path), path); err != nil {
		logger.Error("couldn't clear the old hdparm.conf standby entry", zap.Error(err), zap.String("path", path))
	}

	if _, err := command.ExecHdparmSetStandby(path, code); err != nil {
		logger.Error("failed to apply hdparm standby immediately - persisted rule will still apply on next boot/hotplug", zap.Error(err), zap.String("path", path))
	}
	return nil
}

// 格式化硬盘
func (d *diskService) FormatDisk(path string) error {
	// wait for partition path to be ready
	count := 5
	for count > 0 {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				time.Sleep(1 * time.Second)
				count--
				continue
			}
			logger.Error("error when checking partition path", zap.Error(err), zap.String("path", path))
			return err
		}
		break
	}

	logger.Info("formatting partition...", zap.String("path", path))
	if err := partition.FormatPartition(path); err != nil {
		logger.Error("failed to format partition", zap.Error(err), zap.String("path", path))
		return err
	}

	return nil
}

// 移除挂载点,删除目录
func (d *diskService) UmountPointAndRemoveDir(m model.LSBLKModel) error {
	if len(m.MountPoint) > 0 {
		if err := mount.UmountByMountPoint(m.MountPoint); err != nil {
			logger.Error("error when umounting partition", zap.Error(err), zap.String("path", m.Path), zap.String("mount point", m.MountPoint))
			return err
		}
		removeEmptyMountDir(m.MountPoint)
	}
	for _, p := range m.Children {
		if len(p.MountPoint) > 0 {

			if err := mount.UmountByMountPoint(p.MountPoint); err != nil {
				logger.Error("error when umounting partition", zap.Error(err), zap.String("path", p.Path), zap.String("mount point", p.MountPoint))
				return err
			}
			removeEmptyMountDir(p.MountPoint)
		}
	}

	return nil
}

// part
func (d *diskService) AddPartition(path string) error {
	logger.Info("creating partition table...", zap.String("path", path))
	if err := partition.CreatePartitionTable(path); err != nil {
		logger.Error("failed to create partition table", zap.Error(err), zap.String("path", path))
		return err
	}

	logger.Info("creating partition...", zap.String("path", path))
	partitions, err := partition.AddPartition(path)
	if err != nil {
		logger.Error("failed to create partition", zap.Error(err), zap.String("path", path))
		return err
	}

	for _, p := range partitions {
		partitionPath := p.LSBLKProperties["PATH"]

		// wait for partition path to be ready
		count := 5
		for count > 0 {
			if _, err := os.Stat(partitionPath); err != nil {
				if os.IsNotExist(err) {
					time.Sleep(1 * time.Second)
					count--
					continue
				}
				logger.Error("error when checking partition path", zap.Error(err), zap.String("path", partitionPath))
				return err
			}
			break
		}

		logger.Info("formatting partition...", zap.String("path", partitionPath))
		if err := partition.FormatPartition(partitionPath); err != nil {
			logger.Error("failed to format partition", zap.Error(err), zap.String("path", partitionPath))
			return err
		}
	}

	return nil
}

func (d *diskService) DeletePartition(path string) error {
	// check if path exists
	if !file.Exists(path) {
		return errors.New("device " + path + " does not exists")
	}

	logger.Info("trying to get all partitions of device...", zap.String("path", path))
	partitions, err := partition.GetPartitions(path)
	if err != nil {
		logger.Error("error when getting all partitions of device", zap.Error(err), zap.String("path", path))
		return err
	}

	for _, p := range partitions {

		n, err := strconv.Atoi(p.PARTXProperties["NR"])
		if err != nil {
			logger.Error("error when converting partition number to int", zap.Error(err), zap.String("path", path), zap.String("partition number", p.PARTXProperties["NR"]))
			return err
		}

		logger.Info("trying to delete partition...", zap.String("path", p.LSBLKProperties["PATH"]))
		if err := partition.DeletePartition(path, n); err != nil {
			logger.Error("error when deleting partition", zap.Error(err), zap.String("path", p.LSBLKProperties["PATH"]))
			return err
		}
	}

	return nil
}

// get disk details
func (d *diskService) LSBLK(isUseCache bool) []model.LSBLKModel {
	key := "system_lsblk"

	if isUseCache {
		if result, ok := Cache.Get(key); ok {
			if res, ok := result.([]model.LSBLKModel); ok {
				return res
			}
		}
	}

	str := command.ExecLSBLK()
	if str == nil {
		logger.Error("Failed to exec shell - lsblk exec error")
		return nil
	}

	blkList, err := ParseBlockDevices(str)
	if err != nil {
		logger.Error("Failed to parse block devices from output of lsblk", zap.Error(err))
	}

	var fsused uint64

	result := make([]model.LSBLKModel, 0)

	for _, blk := range blkList {

		if blk.Type == "loop" || blk.RO {
			continue
		}

		fsused = 0

		var blkChildren []model.LSBLKModel
		smart := MyService.Disk().SmartCTL(blk.Path)
		for _, child := range blk.Children {
			if child.RM {

				// if strings.ToLower(strings.TrimSpace(child.State)) != "ok" {
				// 	health = false
				// }
				f, _ := strconv.ParseUint(child.FSUsed.String(), 10, 64)
				fsused += f
			}
			blkChildren = append(blkChildren, child)
		}
		if smart.SmartStatus.Passed || (smart.Sleeping && !smart.StaleHealth) {
			blk.Health = "OK"
		}

		blk.FSUsed = json.Number(fmt.Sprintf("%d", fsused))
		blk.Children = blkChildren
		if fsused > 0 {
			blk.UsedPercent, err = strconv.ParseFloat(fmt.Sprintf("%.4f", float64(fsused)/float64(blk.Size)), 64)
			if err != nil {
				logger.Error("Failed to parse float", zap.Error(err))
			}
		}
		result = append(result, blk)
	}

	if len(result) > 0 {
		Cache.Set(key, result, time.Second*100)
	}

	return result
}

func (d *diskService) GetDiskInfo(path string) model.LSBLKModel {

	str := command.ExecLSBLKByPath(path)
	if str == nil {
		logger.Error("Failed to exec shell - lsblk exec error")
		return model.LSBLKModel{}
	}

	blkList, err := ParseBlockDevices(str)
	if err != nil {
		logger.Error("Failed to parse block devices from output of lsblk", zap.Error(err))
		return model.LSBLKModel{}
	}

	blk := model.LSBLKModel{}
	if len(blkList) > 0 {
		blk = blkList[0]
	}
	return blk
}

// ValidateMountRequest is the input check MountDisk applies: path must be a
// real block device listed by lsblk, mountPoint a clean /mnt|/media|/DATA/<name>.
func ValidateMountRequest(listed []model.LSBLKModel, path, mountPoint string) error {
	if err := ValidateBlockDevicePath(path); err != nil {
		return err
	}
	if _, _, ok := FindBlockDevice(listed, path); !ok {
		return &InvalidDeviceError{Msg: path + " is not a block device listed by lsblk"}
	}
	return model.ValidateMountPoint(mountPoint)
}

func (d *diskService) MountDisk(path, mountPoint string) (string, error) {
	logger.Info("trying to mount...", zap.String("path", path), zap.String("mountPoint", mountPoint))

	// Both values used to be pasted into `bash -c "source helper.sh ;do_mount
	// <path> <mountPoint>"` - a mount point or label containing `;cmd` ran
	// as root. Now they are validated and passed as separate argv entries.
	listed := d.LSBLK(true)
	if _, _, ok := FindBlockDevice(listed, path); !ok {
		listed = d.LSBLK(false)
	}
	if err := ValidateMountRequest(listed, path, mountPoint); err != nil {
		logger.Error("refusing to mount", zap.Error(err), zap.String("path", path), zap.String("mount point", mountPoint))
		return err.Error(), err
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode()&os.ModeDevice == 0 {
		err := &InvalidDeviceError{Msg: path + " is not a block device"}
		return err.Error(), err
	}

	// check if path is already mounted at mountPoint
	if mountInfoList, err := mountinfo.GetMounts(func(i *mountinfo.Info) (skip bool, stop bool) {
		if i.Source == path && i.Mountpoint == mountPoint {
			return false, true
		}
		return true, false
	}); err != nil {
		logger.Error("error when trying to get mount info", zap.Error(err))
		return "", err
	} else if len(mountInfoList) > 0 {
		logger.Info("already mounted", zap.String("path", path), zap.String("mount point", mountPoint))
		return "", nil
	}

	if err := file.IsNotExistMkDir(mountPoint); err != nil {
		logger.Error("error when checking if mount point already exists, or when creating the mount point if it does not exists", zap.Error(err), zap.String("mount point", mountPoint))
		return "", err
	}

	if out, err := RunHelper("do_mount", path, mountPoint); err != nil {
		logger.Error("error when mounting", zap.Error(err), zap.String("path", path), zap.String("mount point", mountPoint), zap.String("output", string(out)))
		return out, err
	}

	// return "", partition.ProbePartition(path)
	return "", nil
}

func (d *diskService) SaveMountPointToDB(m model2.Volume) error {
	if m.UUID == "" {
		return ErrVolumeWithEmptyUUID
	}

	var existing model2.Volume

	result := d.db.Where(&model2.Volume{UUID: m.UUID}).Limit(1).Find(&existing)

	if result.Error != nil {
		logger.Error("error when querying volume by UUID", zap.Error(result.Error), zap.Any("uuid", m.UUID))
		return result.Error
	}

	if result.RowsAffected > 0 {
		m.ID = existing.ID
	}

	if result := d.db.Save(&m); result.Error != nil {
		logger.Error("error when saving volume to db", zap.Error(result.Error), zap.Any("volume", m))
		return result.Error
	}

	return nil
}

func (d *diskService) UpdateMountPointInDB(m model2.Volume) error {
	result := d.db.Model(&model2.Volume{}).Where(&model2.Volume{UUID: m.UUID}).Update("mount_point", m.MountPoint)
	if result.Error != nil {
		logger.Error("error when updating mount point in db by UUID", zap.Error(result.Error), zap.String("uuid", m.UUID), zap.String("mount point", m.MountPoint))
		return result.Error
	}

	logger.Info(strconv.Itoa(int(result.RowsAffected))+" volume(s) with mount point updated in db by UUID", zap.String("uuid", m.UUID), zap.String("mount point", m.MountPoint))

	return nil
}

func (d *diskService) DeleteMountPointFromDB(path, mountPoint string) error {
	partitions, err := partition.GetPartitions(path)
	if err != nil {
		logger.Error("error when getting partitions by path", zap.Error(err), zap.String("path", path))
		return err
	}

	if len(partitions) != 1 {
		logger.Error("there should be only 1 partition returned", zap.Any("partitions", partitions))
	}

	var existingVolumes []model2.Volume
	f := model2.Volume{MountPoint: mountPoint}
	if len(partitions) > 0 {
		f.UUID = partitions[0].LSBLKProperties[`UUID`]
		logger.Info("trying to delete volume by path and mount point", zap.String("path", path), zap.String("mount point", mountPoint), zap.Any("uuid", partitions[0].LSBLKProperties[`UUID`]), zap.Any("partitons", partitions))
	}

	result := d.db.Where(&f).Limit(1).Find(&existingVolumes)
	logger.Info("result", zap.Any("result", result))
	if result.Error != nil {
		logger.Error("error when finding the volume by path and mount point", zap.Error(result.Error), zap.String("path", path), zap.String("mount point", mountPoint))
	}

	if result.RowsAffected == 0 {
		logger.Info("no volume found by path and mount point", zap.String("path", path), zap.String("mount point", mountPoint))
		return nil
	}

	if result := d.db.Delete(&existingVolumes); result.Error != nil {
		logger.Error("error when deleting volume", zap.Error(result.Error), zap.Any("volume", existingVolumes))
		return result.Error
	}

	return nil
}

func (d *diskService) GetSerialAllFromDB() ([]model2.Volume, error) {
	var volumes []model2.Volume

	result := d.db.Find(&volumes)
	if result.Error != nil {
		logger.Error("error when querying all volumes from db", zap.Error(result.Error))
		return nil, result.Error
	}

	return volumes, nil
}

func (d *diskService) GetPersistentTypeByUUID(uuid string) string {
	// check if path is in database
	var m model2.Volume

	if result := d.db.Where(&model2.Volume{UUID: uuid}).Limit(1).Find(&m); result.Error != nil {
		logger.Error("error when finding the volume by uuid in database", zap.Error(result.Error), zap.String("uuid", uuid))
	} else if result.RowsAffected > 0 {
		return PersistentTypeNivaroOS
	}

	// check if it is in fstab
	if entry, err := fstab.Get().GetEntryByUUID(uuid); err != nil {
		logger.Error("error when finding the volume by uuid in fstab", zap.Error(err), zap.String("uuid", uuid))
	} else if entry != nil {
		return PersistentTypeFStab
	}

	// return none if not found
	return PersistentTypeNone
}

func (d *diskService) CheckSerialDiskMount() {
	logger.Info("Checking serial disk mount...")

	// check mount point
	dbList, err := d.GetSerialAllFromDB()
	if err != nil {
		logger.Error("error when getting all volumes from db", zap.Error(err))
		return
	}

	list := d.LSBLK(true)
	mountPointMap := make(map[string]string, len(dbList))

	defer d.RemoveLSBLKCache()

	// remount
	for _, v := range dbList {
		logger.Info("previously persisted mount point", zap.Any("volume", v))
		mountPointMap[v.UUID] = v.MountPoint
	}

	for _, currentDisk := range list {
		output, err := command.ExecEnabledSMART(currentDisk.Path)
		if err != nil {
			if output != nil {
				logger.Error("failed to enable S.M.A.R.T: "+string(output), zap.Error(err), zap.String("path", currentDisk.Path))
			} else {
				logger.Error("failed to enable S.M.A.R.T", zap.Error(err), zap.String("path", currentDisk.Path))
			}
		}

		for _, blkChild := range currentDisk.Children {
			m, ok := mountPointMap[blkChild.UUID]
			if !ok {
				continue
			}
			if blkChild.MountPoint == m {
				continue
			}
			logger.Info("trying to re-mount...", zap.String("path", blkChild.Path), zap.String("mount point", m))
			// mount point check
			mountPoint := m
			mount.UmountByMountPoint(m)
			dir, _ := ioutil.ReadDir(m)
			if len(dir) > 0 {
				i := 1
				for {
					mountPoint = m + "-" + strconv.Itoa(i)
					if file.CheckNotExist(mountPoint) {
						break
					}
					i++
				}
				logger.Info("mount point already exists, using new mount point", zap.String("path", blkChild.Path), zap.String("mount point", mountPoint))
			}

			if output, err := d.MountDisk(blkChild.Path, mountPoint); err != nil {
				logger.Error(output, zap.Error(err), zap.String("path", blkChild.Path), zap.String("volume", mountPoint))
			}

			// obtain the actual mount path (just in case)
			partitions, err := partition.GetPartitions(blkChild.Path)
			if err != nil {
				logger.Error("error when getting partitions by path", zap.Error(err), zap.String("path", blkChild.Path))
				continue
			}

			mountPoint = partitions[0].LSBLKProperties["MOUNTPOINT"]

			if mountPoint != m {
				v := model2.Volume{
					UUID:       blkChild.UUID,
					MountPoint: mountPoint,
				}
				if err := d.UpdateMountPointInDB(v); err != nil {
					logger.Error("error when updating mount point in db", zap.Error(err), zap.Any("volume", v))
				}
			}
		}
	}
}

func (d *diskService) GetUSBDriveStatusList() []model.USBDriveStatus {
	blockList := d.LSBLK(false)
	statusList := []model.USBDriveStatus{}
	for _, v := range blockList {
		if v.Tran != "usb" {
			continue
		}

		isMount := false
		status := model.USBDriveStatus{Model: v.Model, Name: v.Name, Size: v.Size}
		for _, child := range v.Children {
			if len(child.MountPoint) > 0 {
				isMount = true
				avail, _ := strconv.ParseUint(child.FSAvail.String(), 10, 64)
				status.Avail += avail
			}
		}
		if !isMount && len(v.MountPoint) > 0 {
			isMount = true
			avail, _ := strconv.ParseUint(v.FSAvail.String(), 10, 64)
			status.Avail += avail
		}

		if isMount {
			statusList = append(statusList, status)
		}
	}
	return statusList
}

func (d *diskService) InitCheck() {
	time.Sleep(time.Second * 5)
	var fileName string = "local-storage.json"
	diskMap := make(map[string]model.LSBLKModel)
	diskMapNew := make(map[string]model.LSBLKModel)
	diskTempFilePath := filepath.Join(config.AppInfo.DBPath, fileName)
	if file.Exists(diskTempFilePath) {
		tempData := file.ReadFullFile(diskTempFilePath)
		err := json.Unmarshal(tempData, &diskMap)
		if err != nil {
			os.Remove(diskTempFilePath)
		}
	}

	diskList := MyService.Disk().LSBLK(false)
	for _, v := range diskList {
		if IsDiskSupported(v) {
			if _, ok := diskMap[v.Serial]; !ok {
				properties := common.AdditionalProperties(v)
				eventModel := message_bus.Event{
					SourceID:   "local-storage",
					Name:       "local-storage:disk:added",
					Properties: properties,
				}
				// add UI properties to applicable events so that the NivaroOS UI can render it
				event := common.EventAdapterWithUIProperties(&eventModel)

				bk := false
				for _, k := range v.Children {
					if k.MountPoint == "/" {
						bk = true
						break
					}
					for _, s := range k.Children {
						if s.MountPoint == "/" {
							bk = true
							break
						}
					}
					if bk {
						break
					}
				}
				if bk {
					continue
				}

				logger.Info("disk added", zap.Any("eventModel", eventModel))

				response, err := MyService.MessageBus().PublishEventWithResponse(context.Background(), event.SourceID, event.Name, event.Properties)
				if err != nil {
					logger.Error("failed to publish event to message bus", zap.Error(err), zap.Any("event", event))
					continue
				}

				if response.StatusCode() != http.StatusOK {
					logger.Error("failed to publish event to message bus", zap.String("status", response.Status()), zap.Any("response", response))
				}

			}
			diskMapNew[v.Serial] = v
		}
	}
	for k, v := range diskMap {
		if _, ok := diskMapNew[k]; !ok {
			logger.Info("disk removed", zap.Any("disk", v))
			properties := common.AdditionalProperties(v)
			eventModel := message_bus.Event{
				SourceID:   "local-storage",
				Name:       "local-storage:disk:removed",
				Properties: properties,
			}
			event := common.EventAdapterWithUIProperties(&eventModel)
			logger.Info("InitCheck disk removed", zap.Any("eventModel", eventModel))
			response, err := MyService.MessageBus().PublishEventWithResponse(context.Background(), event.SourceID, event.Name, event.Properties)
			if err != nil {
				logger.Error("failed to publish event to message bus", zap.Error(err), zap.Any("event", event))
			}

			if response.StatusCode() != http.StatusOK {
				logger.Error("failed to publish event to message bus", zap.String("status", response.Status()), zap.Any("response", response))
			}
		}
	}
	data, err := json.Marshal(diskMapNew)
	if err != nil {
		return
	}
	file.WriteToPath(data, config.AppInfo.DBPath, fileName)

}

func (d *diskService) GetSystemDf() (model.DFDiskSpace, error) {
	out, err := exec.Command("df", "-kPT").Output()
	if err != nil {
		// A broken/disconnected FUSE mount (or any other mounted filesystem
		// df can't stat) makes `df` exit non-zero - that must not take the
		// whole service down. This is a per-request check, not a startup
		// precondition; log.Fatal here previously crashed the entire
		// process on every such failure.
		logger.Error("GetSystemDf: df command failed", zap.Error(err))
		return model.DFDiskSpace{}, err
	}

	outputStr := string(out)
	// 按行分割字符串
	lines := strings.Split(outputStr, "\n")
	// 忽略第一行（标题行）
	lines = lines[1:]
	// 遍历每一行，解析文件信息
	for _, line := range lines {
		// 分割行，获取各个字段
		fields := strings.Fields(line)
		// 如果行为空，则跳过
		if len(fields) == 0 {
			continue
		}
		if len(fields) == 7 && fields[6] == "/" {
			m := model.DFDiskSpace{
				FileSystem: fields[0],
				Type:       fields[1],

				UsePercent: fields[5],
				MountedOn:  fields[6],
			}
			b, _ := strconv.ParseInt(fields[2], 10, 64)
			u, _ := strconv.ParseInt(fields[3], 10, 64)
			a, _ := strconv.ParseInt(fields[4], 10, 64)
			m.Blocks = strconv.FormatInt(b*1024, 10)
			m.Used = strconv.FormatInt(u*1024, 10)
			m.Available = strconv.FormatInt(a*1024, 10)
			return m, nil
		} else {
			continue
		}
	}
	return model.DFDiskSpace{}, errors.New("not found")
}

func NewDiskService(db *gorm.DB) DiskService {
	return &diskService{db: db}
}

func IsDiskSupported(d model.LSBLKModel) bool {
	return d.Tran == "sata" ||
		d.Tran == "nvme" ||
		d.Tran == "spi" ||
		d.Tran == "sas" ||
		strings.Contains(d.SubSystems, "virtio") ||
		strings.Contains(d.SubSystems, "block:scsi:vmbus:acpi") || // Microsoft Hyper-V
		strings.Contains(d.SubSystems, "block:mmc:mmc_host:pci") ||
		(d.Tran == "ata" && d.Type == "disk")
}

func WalkDisk(rootBlk model.LSBLKModel, depth uint, shouldStopAt func(blk model.LSBLKModel) bool) *model.LSBLKModel {
	if shouldStopAt(rootBlk) {
		return &rootBlk
	}

	if depth == 0 {
		return nil
	}

	for _, blkChild := range rootBlk.Children {
		if blk := WalkDisk(blkChild, depth-1, shouldStopAt); blk != nil {
			return blk
		}
	}

	return nil
}

func ParseBlockDevices(str []byte) ([]model.LSBLKModel, error) {
	var blkList []model.LSBLKModel
	if err := json2.Unmarshal([]byte(jsoniter.Get(str, "blockdevices").ToString()), &blkList); err != nil {
		return nil, err
	}

	return blkList, nil
}
