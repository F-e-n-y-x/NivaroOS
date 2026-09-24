package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	http2 "github.com/F-e-n-y-x/NivaroOS/services/common/utils/http"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/port"
	"github.com/F-e-n-y-x/NivaroOS/services/core/common"
	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/version"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/types"
	"github.com/labstack/echo/v4"
	"github.com/tidwall/gjson"
)

// @Summary check version
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/version/check [get]
func GetSystemCheckVersion(ctx echo.Context) error {
	need, version := version.IsNeedUpdate(service.MyService.Casa().GetNivaroOSVersion())
	if need {
		installLog := model2.AppNotify{}
		installLog.State = 0
		installLog.Message = "New version " + version.Version + " is ready, ready to upgrade"
		installLog.Type = types.NOTIFY_TYPE_NEED_CONFIRM
		installLog.CreatedAt = strconv.FormatInt(time.Now().Unix(), 10)
		installLog.UpdatedAt = strconv.FormatInt(time.Now().Unix(), 10)
		installLog.Name = "NivaroOS System"
		service.MyService.Notify().AddLog(installLog)
	}
	data := make(map[string]interface{}, 3)
	data["need_update"] = need
	data["version"] = version
	data["current_version"] = common.VERSION
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

// @Summary 系统信息
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/update [post]
func SystemUpdate(ctx echo.Context) error {
	need, version := version.IsNeedUpdate(service.MyService.Casa().GetNivaroOSVersion())
	if !need {
		// Nothing to install (or no update source configured) - this used
		// to answer "success", and the UI showed a finished update.
		return ctx.JSON(http.StatusConflict, model.Result{Success: http.StatusConflict, Message: "no update is available to install from here - run the NivaroOS installer to update"})
	}
	service.MyService.System().UpdateSystemVersion(version.Version)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary  get logs
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/error/logs [get]
func GetNivaroOSErrorLogs(ctx echo.Context) error {
	line, _ := strconv.Atoi(utils.DefaultQuery(ctx, "line", "100"))
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: service.MyService.System().GetNivaroOSLogs(line)})
}

// 系统配置
func GetSystemConfigDebug(ctx echo.Context) error {
	array := service.MyService.System().GetSystemConfigDebug()
	disk := service.MyService.System().GetDiskInfo()
	sys := service.MyService.System().GetSysInfo()
	version := service.MyService.Casa().GetNivaroOSVersion()
	var bugContent string = fmt.Sprintf(`
	 - OS: %s
	 - NivaroOS Version: %s
	 - Disk Total: %v 
	 - Disk Used: %v 
	 - System Info: %s
	 - Remote Version: %s
	 - Browser: $Browser$ 
	 - Version: $Version$
`, sys.OS, common.VERSION, disk.Total>>20, disk.Used>>20, array, version.Version)

	//	array = append(array, fmt.Sprintf("disk,total:%v,used:%v,UsedPercent:%v", disk.Total>>20, disk.Used>>20, disk.UsedPercent))

	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: bugContent})
}

// @Summary get nivaroos server port
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/port [get]
func GetNivaroOSPort(ctx echo.Context) error {
	return ctx.JSON(common_err.SUCCESS,
		model.Result{
			Success: common_err.SUCCESS,
			Message: common_err.GetMsg(common_err.SUCCESS),
			Data:    config.ServerInfo.HttpPort,
		})
}

// @Summary edit nivaroos server port
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Param port json string true "port"
// @Success 200 {string} string "ok"
// @Router /sys/port [put]
func PutNivaroOSPort(ctx echo.Context) error {
	json := make(map[string]string)
	ctx.Bind(&json)
	portStr := json["port"]
	portNumber, err := strconv.Atoi(portStr)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR,
			model.Result{
				Success: common_err.SERVICE_ERROR,
				Message: err.Error(),
			})
	}

	isAvailable := port.IsPortAvailable(portNumber, "tcp")
	if !isAvailable {
		return ctx.JSON(common_err.SERVICE_ERROR,
			model.Result{
				Success: common_err.PORT_IS_OCCUPIED,
				Message: common_err.GetMsg(common_err.PORT_IS_OCCUPIED),
			})
	}
	service.MyService.System().UpSystemPort(strconv.Itoa(portNumber))
	return ctx.JSON(common_err.SUCCESS,
		model.Result{
			Success: common_err.SUCCESS,
			Message: common_err.GetMsg(common_err.SUCCESS),
		})
}

// @Summary active killing nivaroos
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/restart [post]
func PostKillNivaroOS(ctx echo.Context) error {
	os.Exit(0)
	return nil
}

// @Summary get system hardware info
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/hardware/info [get]
// shellOut runs a shell one-liner and returns its trimmed stdout, or ""
// on any error - every field gathered this way is best-effort diagnostic
// info for a UI panel, never something the caller should fail hard on.
func shellOut(script string) string {
	out, err := exec.Command("sh", "-c", script).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var validHostname = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// PutSystemHostname sets the machine's hostname via hostnamectl (updates
// both the transient and static hostname, matching how the box would be
// renamed from a terminal) - RFC 1123 label rules enforced client-side
// isn't enough, since the request could come from anywhere the JWT
// middleware allows, so it's re-validated here too.
func PutSystemHostname(ctx echo.Context) error {
	var body struct {
		Hostname string `json:"hostname"`
	}
	if err := ctx.Bind(&body); err != nil {
		return badParams(ctx, "invalid body")
	}
	if !validHostname.MatchString(body.Hostname) {
		return badParams(ctx, "hostname must be 1-63 characters, alphanumeric or hyphens, not starting/ending with a hyphen")
	}
	if err := exec.Command("hostnamectl", "set-hostname", body.Hostname).Run(); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, body.Hostname)
}

type networkInterfaceEntry struct {
	Interface string `json:"interface"`
	Ip        string `json:"ip"`
	Cidr      string `json:"cidr"`
}

// virtualInterfacePrefixes excludes Docker/Compose bridge networks
// (br-<hash> per project, docker0, veth pairs), libvirt/VM networking
// aliases, VPN tunnels, and loopback - what's left is the box's real,
// physical-or-meaningful network presence.
var virtualInterfacePrefixes = []string{"docker", "br-", "veth", "virbr", "tailscale", "tap", "tun", "lo"}

func isVirtualInterface(name string) bool {
	for _, prefix := range virtualInterfacePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// GetSystemNetworkInterfaces shells `ip addr` for this device's real
// network presence (its own IPs), filtered down from the dozens of
// virtual bridges a Docker-heavy box accumulates - GetSystemNetInfo's
// per-interface IO counters have no IP address at all, which is what a
// "what network am I actually on" view needs.
func GetSystemNetworkInterfaces(ctx echo.Context) error {
	out, err := exec.Command("sh", "-c", "ip -o -4 addr show scope global 2>/dev/null").Output()
	interfaces := []networkInterfaceEntry{}
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			name := fields[1]
			if isVirtualInterface(name) {
				continue
			}
			cidr := fields[3]
			ip := strings.Split(cidr, "/")[0]
			interfaces = append(interfaces, networkInterfaceEntry{Interface: name, Ip: ip, Cidr: cidr})
		}
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: interfaces})
}

type diskUsageEntry struct {
	MountPoint string `json:"mount_point"`
	Fstype     string `json:"fstype"`
	// Human-readable sizes, 1024-based with the KB/MB/GB/TB labels the
	// rest of the UI uses (ui/src/mixins/file_utils.js renderSize).
	Total   string `json:"total"`
	Used    string `json:"used"`
	Free    string `json:"free"`
	Percent string `json:"percent"` // used/size, "NN%"
	IsUSB   bool   `json:"is_usb"`
	// Media/transport of the backing device: nvme, ssd, hdd, usb, mmc,
	// virtual, raid, network, pool or "" when it can't be told.
	Kind       string `json:"kind"`
	IsSystem   bool   `json:"is_system"`
	Device     string `json:"device"`
	Model      string `json:"model"`
	Label      string `json:"label"`
	SizeBytes  uint64 `json:"size_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	AvailBytes uint64 `json:"avail_bytes"`
}

func formatDiskBytes(b uint64) string {
	const unit = 1024.0
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / 1024; n >= 1024 && exp < 4; n /= 1024 {
		div *= 1024
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	val := float64(b) / float64(div)
	if exp >= 2 {
		return fmt.Sprintf("%.1f %s", val, units[exp])
	}
	return fmt.Sprintf("%.0f %s", val, units[exp])
}

// Filesystems that never hold user data (kernel/pseudo/container layers).
var diskPseudoFs = map[string]bool{
	"proc": true, "sysfs": true, "tmpfs": true, "devtmpfs": true, "devpts": true,
	"cgroup": true, "cgroup2": true, "overlay": true, "aufs": true, "squashfs": true,
	"efivarfs": true, "debugfs": true, "tracefs": true, "securityfs": true, "pstore": true,
	"bpf": true, "autofs": true, "mqueue": true, "hugetlbfs": true, "configfs": true,
	"fusectl": true, "binfmt_misc": true, "nsfs": true, "rpc_pipefs": true, "ramfs": true,
	"selinuxfs": true, "nfsd": true, "tmpfs.lxcfs": true, "fuse.lxcfs": true,
	"fuse.gvfsd-fuse": true, "fuse.portal": true, "fuse.snapfuse": true, "iso9660": true,
	"udf": true, "shiftfs": true, "zram": true,
}

// Remote filesystems worth showing. statfs on these can block when the
// server is gone, so they are measured with a timeout.
var diskNetworkFs = map[string]bool{
	"nfs": true, "nfs4": true, "cifs": true, "smb3": true, "smbfs": true,
	"fuse.sshfs": true, "9p": true, "glusterfs": true, "fuse.glusterfs": true, "ceph": true, "fuse.ceph": true,
}

var diskPoolFs = map[string]bool{"fuse.mergerfs": true, "mergerfs": true, "zfs": true, "fuse.unionfs": true}

// Mount trees that belong to container runtimes, snaps, etc.
var diskSkipPrefixes = []string{
	"/proc", "/sys", "/dev", "/run", "/snap/", "/var/snap/", "/var/lib/docker/",
	"/var/lib/containers/", "/var/lib/kubelet/", "/var/lib/lxc/", "/var/lib/lxd/",
	"/var/lib/incus/", "/var/lib/snapd/",
}

type mountInfoEntry struct {
	devID  string // "major:minor"
	root   string
	mount  string
	fstype string
	source string
}

// /proc/self/mountinfo escapes space, tab, newline and backslash as \ooo.
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func parseMountInfo(data string) []mountInfoEntry {
	var out []mountInfoEntry
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		sep := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(fields) {
			continue
		}
		out = append(out, mountInfoEntry{
			devID:  fields[2],
			root:   unescapeMountField(fields[3]),
			mount:  unescapeMountField(fields[4]),
			fstype: fields[sep+1],
			source: unescapeMountField(fields[sep+2]),
		})
	}
	return out
}

// statfsWithTimeout guards against a hung NFS/SMB server blocking the request.
func statfsWithTimeout(path string, timeout time.Duration) (*syscall.Statfs_t, error) {
	type res struct {
		st  syscall.Statfs_t
		err error
	}
	ch := make(chan res, 1)
	go func() {
		var st syscall.Statfs_t
		err := syscall.Statfs(path, &st)
		ch <- res{st, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return &r.st, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("statfs %s: timeout", path)
	}
}

func readSysTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// diskOfBlock resolves a block device name (sda1, nvme0n1p2, dm-0, md0)
// to the whole-disk name that carries the transport/rotational info.
func diskOfBlock(kname string, depth int) string {
	if kname == "" || depth > 5 {
		return kname
	}
	sys := "/sys/class/block/" + kname
	if _, err := os.Stat(sys + "/partition"); err == nil {
		if link, err := filepath.EvalSymlinks(sys); err == nil {
			return filepath.Base(filepath.Dir(link))
		}
	}
	// device-mapper (LVM/LUKS) and md: follow the first underlying device.
	if strings.HasPrefix(kname, "dm-") {
		if slaves, err := os.ReadDir(sys + "/slaves"); err == nil && len(slaves) > 0 {
			return diskOfBlock(slaves[0].Name(), depth+1)
		}
	}
	return kname
}

var virtualDiskModel = regexp.MustCompile(`(?i)qemu|vbox|virtual|vmware|msft|xen|bochs`)

// blockKind labels a whole disk from sysfs, so it works with any lsblk
// version (and without lsblk at all).
func blockKind(disk string) (kind, model string) {
	sys := "/sys/block/" + disk
	model = readSysTrim(sys + "/device/model")
	if model == "" {
		model = readSysTrim(sys + "/device/name") // mmc
	}
	link, _ := filepath.EvalSymlinks(sys)
	switch {
	case strings.Contains(link, "/usb"):
		return "usb", model
	case strings.HasPrefix(disk, "nvme"):
		return "nvme", model
	case strings.HasPrefix(disk, "mmcblk"):
		return "mmc", model
	case strings.HasPrefix(disk, "md"):
		return "raid", model
	case strings.HasPrefix(disk, "vd") || strings.HasPrefix(disk, "xvd") || strings.Contains(link, "/virtio"):
		return "virtual", model
	case virtualDiskModel.MatchString(model):
		return "virtual", model
	case strings.HasPrefix(disk, "loop") || strings.HasPrefix(disk, "zram") || strings.HasPrefix(disk, "ram"):
		return "", model
	}
	switch readSysTrim(sys + "/queue/rotational") {
	case "1":
		return "hdd", model
	case "0":
		return "ssd", model
	}
	return "", model
}

// blockLabels maps a resolved device path to its filesystem label.
func blockLabels() map[string]string {
	labels := map[string]string{}
	entries, err := os.ReadDir("/dev/disk/by-label")
	if err != nil {
		return labels
	}
	for _, e := range entries {
		target, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-label", e.Name()))
		if err != nil {
			continue
		}
		labels[target] = unescapeUdevLabel(e.Name())
	}
	return labels
}

// udev escapes unsafe characters in by-label names as \xHH.
func unescapeUdevLabel(s string) string {
	if !strings.Contains(s, `\x`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x' {
			if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// zpoolUsage returns pool -> [size, alloc, free] from `zpool list`, so a
// ZFS pool is reported once with its real capacity instead of once per
// dataset (each dataset's statfs only shows its own used + pool free).
func zpoolUsage() map[string][3]uint64 {
	pools := map[string][3]uint64{}
	if _, err := exec.LookPath("zpool"); err != nil {
		return pools
	}
	out, err := exec.Command("zpool", "list", "-Hp", "-o", "name,size,alloc,free").Output()
	if err != nil {
		return pools
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			continue
		}
		size, e1 := strconv.ParseUint(f[1], 10, 64)
		alloc, e2 := strconv.ParseUint(f[2], 10, 64)
		free, e3 := strconv.ParseUint(f[3], 10, 64)
		if e1 == nil && e2 == nil && e3 == nil {
			pools[f[0]] = [3]uint64{size, alloc, free}
		}
	}
	return pools
}

func diskMountSkipped(m mountInfoEntry) bool {
	if diskPseudoFs[m.fstype] {
		return true
	}
	if strings.HasPrefix(m.source, "/dev/loop") || strings.HasPrefix(m.source, "/dev/zram") {
		return true
	}
	if m.mount == "/" {
		return false
	}
	for _, p := range diskSkipPrefixes {
		if m.mount == strings.TrimSuffix(p, "/") || strings.HasPrefix(m.mount, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	// Other FUSE filesystems (rclone cloud drives, app-specific mounts)
	// report made-up capacities; only the known pooled/remote ones are shown.
	if strings.HasPrefix(m.fstype, "fuse.") && !diskPoolFs[m.fstype] && !diskNetworkFs[m.fstype] {
		return true
	}
	return false
}

// collectDisksUsage lists every real data filesystem once: kernel mount
// table + statfs (no dependency on the lsblk version - MOUNTPOINTS only
// exists since util-linux 2.37), deduplicated per filesystem (btrfs
// subvolumes and bind mounts share one), with the backing device's
// transport/rotational flags from sysfs.
func collectDisksUsage() []diskUsageEntry {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	mounts := parseMountInfo(string(data))
	// Shortest mount point first, so "/" wins over "/home" for a shared
	// btrfs filesystem and a bind mount never hides the original.
	sort.SliceStable(mounts, func(i, j int) bool {
		if len(mounts[i].mount) != len(mounts[j].mount) {
			return len(mounts[i].mount) < len(mounts[j].mount)
		}
		return mounts[i].mount < mounts[j].mount
	})

	labels := blockLabels()
	var pools map[string][3]uint64
	seen := map[string]bool{}
	disks := []diskUsageEntry{}
	for _, m := range mounts {
		if diskMountSkipped(m) {
			continue
		}
		isNet := diskNetworkFs[m.fstype]
		timeout := 2 * time.Second
		if !isNet {
			timeout = 5 * time.Second
		}
		st, err := statfsWithTimeout(m.mount, timeout)
		if err != nil || st.Blocks == 0 {
			continue
		}
		bsize := uint64(st.Bsize)
		if st.Frsize > 0 {
			bsize = uint64(st.Frsize)
		}
		size := st.Blocks * bsize
		free := st.Bfree * bsize
		avail := st.Bavail * bsize
		used := uint64(0)
		if size > free {
			used = size - free
		}

		// Dedupe key: one entry per filesystem.
		key := "dev:" + m.devID
		switch {
		case m.fstype == "btrfs":
			key = fmt.Sprintf("btrfs:%x:%x", st.Fsid.X__val[0], st.Fsid.X__val[1])
		case m.fstype == "zfs":
			key = "zfs:" + strings.SplitN(m.source, "/", 2)[0]
		case isNet:
			key = "net:" + m.source
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		e := diskUsageEntry{MountPoint: m.mount, Fstype: m.fstype, IsSystem: m.mount == "/"}
		switch {
		case isNet:
			e.Kind = "network"
			e.Device = m.source
		case diskPoolFs[m.fstype]:
			e.Kind = "pool"
			e.Device = m.source
			if m.fstype == "zfs" {
				if pools == nil {
					pools = zpoolUsage()
				}
				pool := strings.SplitN(m.source, "/", 2)[0]
				if p, ok := pools[pool]; ok && p[0] > 0 {
					size, used, avail = p[0], p[1], p[2]
				}
				e.Label = pool
			}
		case strings.HasPrefix(m.source, "/dev/"):
			dev := m.source
			if real, err := filepath.EvalSymlinks(dev); err == nil {
				dev = real
			}
			e.Device = dev
			e.Label = labels[dev]
			disk := diskOfBlock(filepath.Base(dev), 0)
			e.Kind, e.Model = blockKind(disk)
		}
		e.IsUSB = e.Kind == "usb"
		e.SizeBytes, e.UsedBytes, e.AvailBytes = size, used, avail
		e.Total = formatDiskBytes(size)
		e.Used = formatDiskBytes(used)
		e.Free = formatDiskBytes(avail)
		pct := uint64(0)
		if size > 0 {
			pct = (used*100 + size/2) / size
		}
		e.Percent = fmt.Sprintf("%d%%", pct)
		disks = append(disks, e)
	}
	// Stable, meaningful order: system first, then by mount point.
	sort.SliceStable(disks, func(i, j int) bool {
		if disks[i].IsSystem != disks[j].IsSystem {
			return disks[i].IsSystem
		}
		return disks[i].MountPoint < disks[j].MountPoint
	})
	return disks
}

// GetSystemDisksUsage lists mounted data filesystems (loop/snap/squashfs,
// container layers and pseudo filesystems filtered out) with bytes, a
// consistent used/size percentage and the device's media kind.
func GetSystemDisksUsage(ctx echo.Context) error {
	disks := collectDisksUsage()

	// Fallback to df when the mount table could not be read at all.
	if len(disks) == 0 {
		dfOut, dfErr := exec.Command("sh", "-c", "df -B1 --output=fstype,size,used,avail,target -x tmpfs -x devtmpfs -x overlay -x squashfs -x efivarfs 2>/dev/null | tail -n +2").Output()
		if dfErr == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(dfOut)), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 5 {
					continue
				}
				size, _ := strconv.ParseUint(fields[1], 10, 64)
				used, _ := strconv.ParseUint(fields[2], 10, 64)
				avail, _ := strconv.ParseUint(fields[3], 10, 64)
				mountPoint := strings.Join(fields[4:], " ")
				if size == 0 || strings.HasPrefix(mountPoint, "/snap/") {
					continue
				}
				pct := (used*100 + size/2) / size
				disks = append(disks, diskUsageEntry{
					Fstype:     fields[0],
					MountPoint: mountPoint,
					IsSystem:   mountPoint == "/",
					SizeBytes:  size,
					UsedBytes:  used,
					AvailBytes: avail,
					Total:      formatDiskBytes(size),
					Used:       formatDiskBytes(used),
					Free:       formatDiskBytes(avail),
					Percent:    fmt.Sprintf("%d%%", pct),
				})
			}
		}
	}

	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: disks})
}

func GetSystemHardwareInfo(ctx echo.Context) error {
	data := make(map[string]string, 1)
	data["drive_model"] = service.MyService.System().GetDeviceTree()
	data["arch"] = runtime.GOARCH
	data["os_name"] = shellOut(". /etc/os-release 2>/dev/null && echo \"$PRETTY_NAME\"")
	data["kernel"] = shellOut("uname -r")
	data["hostname"] = shellOut("hostname")
	data["uptime"] = shellOut("uptime -p")
	data["packages"] = shellOut("dpkg -l 2>/dev/null | grep -c '^ii'")
	data["shell"] = shellOut("getent passwd root | cut -d: -f7")
	data["locale"] = shellOut(". /etc/default/locale 2>/dev/null && echo \"$LANG\"")
	data["docker_version"] = shellOut("docker version --format '{{.Server.Version}}' 2>/dev/null")
	// Reflects apt's local cache (whatever "apt update" last saw), not a
	// live check against Docker's registry - good enough to surface "an
	// update exists" without this endpoint making its own network calls.
	data["docker_update_available"] = strconv.FormatBool(shellOut("apt list --upgradable 2>/dev/null | grep -icE 'docker-ce|docker-ee|containerd.io'") != "")

	if cpu := service.MyService.System().GetCpuInfo(); len(cpu) > 0 {
		return ctx.JSON(common_err.SUCCESS,
			model.Result{
				Success: common_err.SUCCESS,
				Message: common_err.GetMsg(common_err.SUCCESS),
				Data:    data,
			})
	}
	return nil
}

// @Summary system utilization
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/utilization [get]
func GetSystemUtilization(ctx echo.Context) error {
	data := make(map[string]interface{})
	cpu := service.MyService.System().GetCpuPercent()
	num := service.MyService.System().GetCpuCoreNum()
	cpuModel := "arm"
	modelName := ""
	mhz := float64(0)
	if cpuInfo := service.MyService.System().GetCpuInfo(); len(cpuInfo) > 0 {
		modelName = strings.TrimSpace(cpuInfo[0].ModelName)
		mhz = cpuInfo[0].Mhz
		if strings.Count(strings.ToLower(modelName), "intel") > 0 {
			cpuModel = "intel"
		} else if strings.Count(strings.ToLower(modelName), "amd") > 0 {
			cpuModel = "amd"
		}
	}
	cpuData := make(map[string]interface{})
	cpuData["percent"] = cpu
	cpuData["percpu"] = service.MyService.System().GetCpuPercentPerCore()
	cpuData["num"] = num
	cpuData["temperature"] = service.MyService.System().GetCPUTemperature()
	cpuData["power"] = service.MyService.System().GetCPUPower()
	cpuData["model"] = cpuModel
	cpuData["model_name"] = modelName
	cpuData["mhz"] = mhz

	data["cpu"] = cpuData
	memData := service.MyService.System().GetMemInfo()
	memData["dimms"] = service.MyService.System().GetMemoryDIMMs()
	data["mem"] = memData

	// 拼装网络信息
	netList := service.MyService.System().GetNetInfo()
	newNet := []model.IOCountersStat{}
	nets := service.MyService.System().GetNet(true)
	for _, n := range netList {
		for _, netCardName := range nets {
			if n.Name == netCardName {
				item := *(*model.IOCountersStat)(unsafe.Pointer(&n))
				item.State = strings.TrimSpace(service.MyService.System().GetNetState(n.Name))
				item.Time = time.Now().Unix()
				newNet = append(newNet, item)
				break
			}
		}
	}

	data["net"] = newNet
	systemMap := service.MyService.Notify().GetSystemTempMap()
	systemMap.Range(func(key, value interface{}) bool {
		data[key.(string)] = value
		return true
	})
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

// @Summary get cpu info
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/cpu [get]
func GetSystemCupInfo(ctx echo.Context) error {
	cpu := service.MyService.System().GetCpuPercent()
	num := service.MyService.System().GetCpuCoreNum()
	data := make(map[string]interface{})
	data["percent"] = cpu
	data["num"] = num
	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

// @Summary get mem info
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/mem [get]
func GetSystemMemInfo(ctx echo.Context) error {
	mem := service.MyService.System().GetMemInfo()
	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: mem})
}

// @Summary get disk info
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/disk [get]
func GetSystemDiskInfo(ctx echo.Context) error {
	disk := service.MyService.System().GetDiskInfo()
	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: disk})
}

// @Summary get Net info
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/net [get]
func GetSystemNetInfo(ctx echo.Context) error {
	netList := service.MyService.System().GetNetInfo()
	newNet := []model.IOCountersStat{}
	for _, n := range netList {
		for _, netCardName := range service.MyService.System().GetNet(true) {
			if n.Name == netCardName {
				item := *(*model.IOCountersStat)(unsafe.Pointer(&n))
				item.State = strings.TrimSpace(service.MyService.System().GetNetState(n.Name))
				item.Time = time.Now().Unix()
				newNet = append(newNet, item)
				break
			}
		}
	}

	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: newNet})
}

func GetSystemProxy(ctx echo.Context) error {
	url := ctx.QueryParam("url")
	resp, err := http2.Get(url, 30*time.Second)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
	}
	defer resp.Body.Close()
	for k, v := range ctx.Request().Header {
		ctx.Request().Header.Add(k, v[0])
	}
	rda, _ := ioutil.ReadAll(resp.Body)
	//	json.NewEncoder(c.Writer).Encode(json.RawMessage(string(rda)))
	// 响应状态码
	ctx.Response().Writer.WriteHeader(resp.StatusCode)
	// 复制转发的响应Body到响应Body
	io.Copy(ctx.Response().Writer, ioutil.NopCloser(bytes.NewBuffer(rda)))
	return nil
}

func PutSystemState(ctx echo.Context) error {
	state := ctx.Param("state")
	if strings.ToLower(state) == "off" {
		service.MyService.System().SystemShutdown()
	} else if strings.ToLower(state) == "restart" {
		service.MyService.System().SystemReboot()
	}
	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: "The operation will be completed shortly."})
}

// @Summary 获取一个可用端口
// @Produce  application/json
// @Accept application/json
// @Tags app
// @Param  type query string true "端口类型 udp/tcp"
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /app/getport [get]
func GetPort(ctx echo.Context) error {
	t := utils.DefaultQuery(ctx, "type", "tcp")
	var p int
	ok := true
	for ok {
		p, _ = port.GetAvailablePort(t)
		ok = !port.IsPortAvailable(p, t)
	}
	// @tiger 这里最好封装成 {'port': ...} 的形式，来体现出参的上下文
	return ctx.JSON(common_err.SUCCESS, &model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: p})
}

// @Summary 检查端口是否可用
// @Produce  application/json
// @Accept application/json
// @Tags app
// @Param  port path int true "端口号"
// @Param  type query string true "端口类型 udp/tcp"
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /app/check/{port} [get]
func PortCheck(ctx echo.Context) error {
	p, _ := strconv.Atoi(ctx.Param("port"))
	t := utils.DefaultQuery(ctx, "type", "tcp")
	return ctx.JSON(common_err.SUCCESS, &model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: port.IsPortAvailable(p, t)})
}

func GetSystemEntry(ctx echo.Context) error {
	entry := service.MyService.System().GetSystemEntry()
	str := json.RawMessage(entry)
	if !gjson.ValidBytes(str) {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: entry, Data: json.RawMessage("[]")})
	}
	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: str})
}

// @Summary Server Internet Speed Test
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/speedtest [get]
func GetSystemSpeedTest(ctx echo.Context) error {
	res, err := runSpeedTest(ctx.Request().Context())
	if err != nil {
		return ctx.JSON(http.StatusOK, &model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, &model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    res,
	})
}

// POST /sys/speedtest - start the internet test in the background.
func PostSystemSpeedTest(ctx echo.Context) error {
	if err := StartSpeedTest(); err != nil {
		return ctx.JSON(http.StatusConflict, &model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, &model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: currentProgress()})
}

// GET /sys/speedtest/status - phase, live Mbps and the (partial) result.
func GetSystemSpeedTestStatus(ctx echo.Context) error {
	return ctx.JSON(common_err.SUCCESS, &model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: currentProgress()})
}
