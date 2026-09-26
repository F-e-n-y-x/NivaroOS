package service

import (
	"encoding/json"
	"errors"
	"fmt"
	net2 "net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/command"
	exec2 "github.com/F-e-n-y-x/NivaroOS/services/common/utils/exec"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/core/common"
	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/common_err"
	corefile "github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/httper"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/ip_helper"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type SystemService interface {
	UpdateSystemVersion(version string)
	GetSystemConfigDebug() []string
	GetNivaroOSLogs(lineNumber int) string
	UpdateAssist()
	UpSystemPort(port string)
	GetTimeZone() string
	UpAppOrderFile(str, id string)
	GetAppOrderFile(id string) []byte
	GetNet(physics bool) []string
	GetNetInfo() []net.IOCountersStat
	GetCpuCoreNum() int
	GetCpuPercent() float64
	GetCpuPercentPerCore() []float64
	GetMemoryDIMMs() []model.DIMMInfo
	GetMemInfo() map[string]interface{}
	GetCpuInfo() []cpu.InfoStat
	GetDirPath(path string) ([]model.Path, error)
	GetDirPathOne(path string) (m model.Path)
	GetNetState(name string) string
	GetDiskInfo() *disk.UsageStat
	GetSysInfo() host.InfoStat
	GetDeviceTree() string
	GetDeviceInfo() model.DeviceInfo
	CreateFile(path string) (int, error)
	RenameFile(oldF, newF string) (int, error)
	MkdirAll(path string) (int, error)
	GetCPUTemperature() *int
	GetCPUPower() map[string]interface{}
	GetMacAddress() (string, error)
	SystemReboot() error
	SystemShutdown() error
	GetSystemEntry() string
	GenreateSystemEntry()
}
type systemService struct{}

func (c *systemService) GetDeviceInfo() model.DeviceInfo {
	m := model.DeviceInfo{}
	m.OS_Version = common.VERSION
	err, portStr := MyService.Gateway().GetPort()
	if err != nil {
		m.Port = 80
	} else {
		port := gjson.Get(portStr, "data")
		if len(port.Raw) == 0 {
			m.Port = 80
		} else {
			p, err := strconv.Atoi(port.Raw)
			if err != nil {
				m.Port = 80
			} else {
				m.Port = p
			}
		}
	}
	allIpv4 := ip_helper.GetDeviceAllIPv4()
	ip := []string{}
	nets := MyService.System().GetNet(true)
	for _, n := range nets {
		if v, ok := allIpv4[n]; ok {
			{
				ip = append(ip, v)
			}
		}
	}

	m.LanIpv4 = ip
	h, err := host.Info() /*  */
	if err == nil {
		m.DeviceName = h.Hostname
	}
	mb := model.BaseInfo{}

	err = json.Unmarshal(file.ReadFullFile(config.AppInfo.DBPath+"/baseinfo.conf"), &mb)
	if err == nil {
		m.Hash = mb.Hash
	}

	osRelease, _ := file.ReadOSRelease()
	m.DeviceModel = osRelease["MODEL"]
	m.DeviceSN = osRelease["SN"]
	res := httper.Get("http://127.0.0.1:"+strconv.Itoa(m.Port)+"/v1/users/status", nil)
	init := gjson.Get(res, "data.initialized")
	m.Initialized, _ = strconv.ParseBool(init.Raw)

	return m
}

func (c *systemService) GenreateSystemEntry() {
	modelsPath := "/var/lib/nivaroos/www/modules"
	entryFileName := "entry.json"
	entryFilePath := filepath.Join(config.AppInfo.DBPath, "db", entryFileName)
	file.IsNotExistCreateFile(entryFilePath)

	dir, err := os.ReadDir(modelsPath)
	if err != nil {
		logger.Error("read dir error", zap.Error(err))
		return
	}
	json := "["
	for _, v := range dir {
		data, err := os.ReadFile(filepath.Join(modelsPath, v.Name(), entryFileName))
		if err != nil {
			logger.Error("read entry file error", zap.Error(err))
			continue
		}
		json += string(data) + ","
	}
	json = strings.TrimRight(json, ",")
	json += "]"
	err = os.WriteFile(entryFilePath, []byte(json), 0o666)
	if err != nil {
		logger.Error("write entry file error", zap.Error(err))
		return
	}
}

func (c *systemService) GetSystemEntry() string {
	modelsPath := "/var/lib/nivaroos/www/modules"
	entryFileName := "entry.json"
	dir, err := os.ReadDir(modelsPath)
	if err != nil {
		logger.Error("read dir error", zap.Error(err))
		return ""
	}
	json := "["
	for _, v := range dir {
		data, err := os.ReadFile(filepath.Join(modelsPath, v.Name(), entryFileName))
		if err != nil {
			logger.Error("read entry file error", zap.Error(err))
			continue
		}
		json += string(data) + ","
	}
	json = strings.TrimRight(json, ",")
	json += "]"
	if err != nil {
		logger.Error("write entry file error", zap.Error(err))
		return ""
	}
	return json
}

func (c *systemService) GetMacAddress() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	nets := MyService.System().GetNet(true)
	for _, v := range interfaces {
		for _, n := range nets {
			if v.Name == n {
				return v.HardwareAddr, nil
			}
		}
	}
	return "", errors.New("not found")
}

func (c *systemService) MkdirAll(path string) (int, error) {
	_, err := os.Stat(path)
	if err == nil {
		return common_err.DIR_ALREADY_EXISTS, nil
	} else {
		if os.IsNotExist(err) {
			os.MkdirAll(path, os.ModePerm)
			return common_err.SUCCESS, nil
		} else if strings.Contains(err.Error(), ": not a directory") {
			return common_err.FILE_OR_DIR_EXISTS, err
		}
	}
	return common_err.SERVICE_ERROR, err
}

func (c *systemService) RenameFile(oldF, newF string) (int, error) {
	_, err := os.Stat(newF)
	if err == nil {
		return common_err.DIR_ALREADY_EXISTS, nil
	}
	if os.IsNotExist(err) {
		err := os.Rename(oldF, newF)
		if err != nil {
			var linkErr *os.LinkError
			if (errors.As(err, &linkErr) && (errors.Is(linkErr.Err, syscall.EXDEV) || strings.Contains(strings.ToLower(linkErr.Error()), "cross-device"))) ||
				errors.Is(err, syscall.EXDEV) || strings.Contains(strings.ToLower(err.Error()), "cross-device") {
				// Cross-filesystem rename (e.g. some FUSE mounts): copy to
				// exactly newF, and only remove the original once every file
				// arrived. The old fallback used CopyDir, which copies into
				// newF/<oldname>, and then deleted the original regardless.
				if cpErr := corefile.CopyTree(oldF, newF); cpErr != nil {
					return common_err.SERVICE_ERROR, cpErr
				}
				if rmErr := os.RemoveAll(oldF); rmErr != nil {
					return common_err.SERVICE_ERROR, rmErr
				}
				return common_err.SUCCESS, nil
			}
			return common_err.SERVICE_ERROR, err
		}
		return common_err.SUCCESS, nil
	}
	return common_err.SERVICE_ERROR, err
}

func (c *systemService) CreateFile(path string) (int, error) {
	_, err := os.Stat(path)
	if err == nil {
		return common_err.FILE_OR_DIR_EXISTS, nil
	} else {
		if os.IsNotExist(err) {
			file.CreateFile(path)
			return common_err.SUCCESS, nil
		}
	}
	return common_err.SERVICE_ERROR, err
}

func (c *systemService) GetDeviceTree() string {
	if output, err := command.OnlyExec("source " + config.AppInfo.ShellPath + "/helper.sh ;GetDeviceTree"); err != nil {
		return ""
	} else {
		return output
	}
}

func (c *systemService) GetSysInfo() host.InfoStat {
	info, _ := host.Info()
	return *info
}

func (c *systemService) GetDiskInfo() *disk.UsageStat {
	path := "/"
	if runtime.GOOS == "windows" {
		path = "C:"
	}
	diskInfo, _ := disk.Usage(path)
	diskInfo.UsedPercent, _ = strconv.ParseFloat(fmt.Sprintf("%.1f", diskInfo.UsedPercent), 64)
	diskInfo.InodesUsedPercent, _ = strconv.ParseFloat(fmt.Sprintf("%.1f", diskInfo.InodesUsedPercent), 64)
	return diskInfo
}

func (c *systemService) GetNetState(name string) string {
	if output, err := command.OnlyExec("source " + config.AppInfo.ShellPath + "/helper.sh ;CatNetCardState " + name); err != nil {
		return ""
	} else {
		return output
	}
}

func (c *systemService) GetDirPathOne(path string) (m model.Path) {
	f, err := os.Stat(path)
	if err != nil {
		return
	}
	m.IsDir = f.IsDir()
	m.Name = f.Name()
	m.Path = path
	m.Size = f.Size()
	m.Date = f.ModTime()
	return
}

func (c *systemService) GetDirPath(path string) ([]model.Path, error) {
	if path == "/DATA" {
		sysType := runtime.GOOS
		if sysType == "windows" {
			path = "C:\\NivaroOS\\DATA"
		}
		if sysType == "darwin" {
			path = "./NivaroOS/DATA"
		}
	}

	ls, err := os.ReadDir(path)
	if err != nil {
		logger.Error("when read dir", zap.Error(err))
		return []model.Path{}, err
	}
	if len(path) == 0 {
		return []model.Path{{Name: "DATA", Path: "/DATA/", IsDir: true, Date: time.Now()}}, nil
	}

	dirs := make([]model.Path, len(ls))
	var wg sync.WaitGroup
	// Concurrency pool (max 16 simultaneous FUSE stats)
	semaphore := make(chan struct{}, 16)

	for i, l := range ls {
		wg.Add(1)
		go func(idx int, entry os.DirEntry) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			filePath := filepath.Join(path, entry.Name())
			isDir := entry.IsDir()
			modTime := time.Now()
			var size int64

			// Only evaluate symlinks if the directory entry indicates a symlink
			if entry.Type()&os.ModeSymlink != 0 {
				link, err := filepath.EvalSymlinks(filePath)
				if err == nil && link != filePath {
					if fileInfo, err := os.Stat(link); err == nil {
						isDir = fileInfo.IsDir()
						modTime = fileInfo.ModTime()
						size = fileInfo.Size()
					}
				}
			} else {
				if info, err := entry.Info(); err == nil {
					modTime = info.ModTime()
					size = info.Size()
				}
			}

			dirs[idx] = model.Path{
				Name:  entry.Name(),
				Path:  filePath,
				IsDir: isDir,
				Date:  modTime,
				Size:  size,
			}
		}(i, l)
	}
	wg.Wait()

	return dirs, nil
}

func (c *systemService) GetCpuInfo() []cpu.InfoStat {
	info, _ := cpu.Info()
	return info
}

func (c *systemService) GetMemInfo() map[string]interface{} {
	memInfo, _ := mem.VirtualMemory()
	memInfo.UsedPercent, _ = strconv.ParseFloat(fmt.Sprintf("%.1f", memInfo.UsedPercent), 64)
	memData := make(map[string]interface{})
	memData["total"] = memInfo.Total
	memData["available"] = memInfo.Available
	memData["used"] = memInfo.Used
	memData["free"] = memInfo.Free
	memData["usedPercent"] = memInfo.UsedPercent
	// Swap, so "Free up memory" can offer to empty it only when it's in use.
	if swap, err := mem.SwapMemory(); err == nil {
		memData["swapTotal"] = swap.Total
		memData["swapUsed"] = swap.Used
	}
	return memData
}

func (c *systemService) GetCpuPercent() float64 {
	percent, _ := cpu.Percent(0, false)
	value, _ := strconv.ParseFloat(fmt.Sprintf("%.1f", percent[0]), 64)
	return value
}

func (c *systemService) GetCpuPercentPerCore() []float64 {
	percents, _ := cpu.Percent(0, true)
	values := make([]float64, len(percents))
	for i, p := range percents {
		values[i], _ = strconv.ParseFloat(fmt.Sprintf("%.1f", p), 64)
	}
	return values
}

// GetMemoryDIMMs parses `dmidecode -t memory` for populated DIMM slots.
// dmidecode output has no stable machine-readable format on all versions
// (--json is only in newer releases), so this scans "Memory Device" blocks
// and pulls out the handful of fields the RAM widget needs.
func (c *systemService) GetMemoryDIMMs() []model.DIMMInfo {
	out, err := exec2.Command("dmidecode", "-t", "memory").CombinedOutput()
	if err != nil {
		return nil
	}

	var dimms []model.DIMMInfo
	var current *model.DIMMInfo
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Memory Device" {
			if current != nil && current.Size != "" && current.Size != "No Module Installed" {
				dimms = append(dimms, *current)
			}
			current = &model.DIMMInfo{}
			continue
		}
		if current == nil {
			continue
		}
		key, value, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "Locator":
			current.Locator = value
		case "Size":
			current.Size = value
		case "Type":
			current.Type = value
		case "Speed":
			current.Speed = value
		case "Manufacturer":
			current.Manufacturer = value
		case "Part Number":
			current.PartNumber = value
		}
	}
	if current != nil && current.Size != "" && current.Size != "No Module Installed" {
		dimms = append(dimms, *current)
	}
	return dimms
}

func (c *systemService) GetCpuCoreNum() int {
	count, _ := cpu.Counts(false)
	return count
}

func (c *systemService) GetNetInfo() []net.IOCountersStat {
	parts, _ := net.IOCounters(true)
	return parts
}

func (c *systemService) GetNet(physics bool) []string {
	t := "1"
	if physics {
		t = "2"
	}

	if output, err := command.OnlyExec("source " + config.AppInfo.ShellPath + "/helper.sh ;GetNetCard " + t); err != nil {
		return []string{}
	} else {
		return strings.Split(output, "\n")
	}
}

func (s *systemService) UpdateSystemVersion(version string) {
	keyName := "casa_version"
	Cache.Delete(keyName)
	if file.Exists(config.AppInfo.LogPath + "/upgrade.log") {
		os.Remove(config.AppInfo.LogPath + "/upgrade.log")
	}
	file.CreateFile(config.AppInfo.LogPath + "/upgrade.log")
	if len(config.ServerInfo.UpdateUrl) > 0 {
		go command.OnlyExec("curl -fsSL " + config.ServerInfo.UpdateUrl + " | bash")
	} else {
		logger.Info("no default update URL configured; skipping self-update")
	}

	// s.log.Error(config.AppInfo.ProjectPath + "/shell/tool.sh -r " + version)
	// s.log.Error(command2.ExecResultStr(config.AppInfo.ProjectPath + "/shell/tool.sh -r " + version))
}

func (s *systemService) UpdateAssist() {
	command.ExecResultStrArray("source " + config.AppInfo.ShellPath + "/assist.sh")
}

func (s *systemService) GetTimeZone() string {
	if output, err := command.OnlyExec("source " + config.AppInfo.ShellPath + "/helper.sh ;GetTimeZone"); err != nil {
		return ""
	} else {
		return output
	}
}

func (s *systemService) GetSystemConfigDebug() []string {
	if output, err := command.OnlyExec("source " + config.AppInfo.ShellPath + "/helper.sh ;GetSysInfo"); err != nil {
		return []string{}
	} else {
		return strings.Split(output, "\n")
	}
}

func (s *systemService) UpAppOrderFile(str, id string) {
	file.WriteToPath([]byte(str), config.AppInfo.DBPath+"/"+id, "app_order.json")
}

func (s *systemService) GetAppOrderFile(id string) []byte {
	return file.ReadFullFile(config.AppInfo.UserDataPath + "/" + id + "/app_order.json")
}

func (s *systemService) UpSystemPort(port string) {
	if len(port) > 0 && port != config.ServerInfo.HttpPort {
		config.Cfg.Section("server").Key("HttpPort").SetValue(port)
		config.ServerInfo.HttpPort = port
	}
	config.Cfg.SaveTo(config.SystemConfigInfo.ConfigPath)
}

func (s *systemService) GetNivaroOSLogs(lineNumber int) string {
	if lineNumber <= 0 || lineNumber > 5000 {
		lineNumber = 5000
	}
	out, err := tailLines(filepath.Join(config.AppInfo.LogPath, fmt.Sprintf("%s.%s",
		config.AppInfo.LogSaveName,
		config.AppInfo.LogFileExt,
	)), lineNumber)
	if err != nil {
		return err.Error()
	}
	return out
}

func GetDeviceAllIP() []string {
	var address []string
	addrs, err := net2.InterfaceAddrs()
	if err != nil {
		return address
	}
	for _, a := range addrs {
		if ipNet, ok := a.(*net2.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To16() != nil {
				address = append(address, ipNet.IP.String())
			}
		}
	}
	return address
}

// find thermal_zone of cpu.
// assertions:
//   - thermal_zone "type" and "temp" are required fields
//     (https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-class-thermal)
func GetCPUThermalZone() string {
	keyName := "cpu_thermal_zone"

	var path string
	if result, ok := Cache.Get(keyName); ok {
		path, ok = result.(string)
		if ok {
			return path
		}
	}

	var name string
	cpu_types := []string{"x86_pkg_temp", "cpu", "CPU", "soc"}
	stub := "/sys/devices/virtual/thermal/thermal_zone"
	for i := 0; i < 100; i++ {
		path = stub + strconv.Itoa(i)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			name = strings.TrimSuffix(string(file.ReadFullFile(path+"/type")), "\n")
			for _, s := range cpu_types {
				if strings.HasPrefix(name, s) {
					logger.Info(fmt.Sprintf("CPU thermal zone found: %s, path: %s.", name, path))
					Cache.SetDefault(keyName, path)
					return path
				}
			}
		} else {
			if len(name) > 0 { // proves at least one zone
				path = stub + "0"
			} else {
				path = ""
			}
			break
		}
	}

	Cache.SetDefault(keyName, path)
	return path
}

// hwmon drivers that report the CPU package/die temperature, best first.
// k10temp/zenpower: AMD, coretemp: Intel, cpu_thermal/soc_thermal & co: ARM
// SoCs (Raspberry Pi, Rockchip, Allwinner, Amlogic...).
var cpuHwmonDrivers = []string{"k10temp", "zenpower", "coretemp", "cpu_thermal", "cpu-thermal", "soc_thermal", "soc-thermal", "cpuss0_thermal", "x86_pkg_temp"}

// Preferred sensor labels inside a CPU hwmon: Tdie is the real die temp
// on AMD parts where Tctl carries a fan-control offset.
var cpuHwmonLabels = []string{"tdie", "tctl", "package id 0", "physical id 0", "cpu"}

// findCPUHwmonInput returns the tempN_input file of the best CPU sensor
// found under /sys/class/hwmon, or "".
func findCPUHwmonInput() string {
	dirs, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	byDriver := map[string]string{}
	for _, dir := range dirs {
		name := strings.TrimSpace(string(file.ReadFullFile(filepath.Join(dir, "name"))))
		if name == "" {
			continue
		}
		if _, dup := byDriver[name]; !dup {
			byDriver[name] = dir
		}
	}
	for _, drv := range cpuHwmonDrivers {
		dir, ok := byDriver[drv]
		if !ok {
			continue
		}
		inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		if len(inputs) == 0 {
			continue
		}
		labels := map[string]string{}
		for _, in := range inputs {
			label := strings.ToLower(strings.TrimSpace(string(file.ReadFullFile(strings.TrimSuffix(in, "_input") + "_label"))))
			labels[label] = in
		}
		for _, want := range cpuHwmonLabels {
			if in, ok := labels[want]; ok {
				return in
			}
		}
		// No known label (ARM drivers usually have none): temp1, else the first.
		if in := filepath.Join(dir, "temp1_input"); file.Exists(in) {
			return in
		}
		return inputs[0]
	}
	return ""
}

// readMilliCelsius parses a sysfs temperature (millidegrees, or degrees on
// a few old drivers) and rejects values no working CPU sensor reports.
func readMilliCelsius(path string) (int, bool) {
	raw := strings.TrimSpace(string(file.ReadFullFile(path)))
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if v > 1000 || v < -1000 {
		v = v / 1000
	}
	if v <= 0 || v > 150 {
		return 0, false
	}
	return v, true
}

// GetCPUTemperature returns the CPU temperature in °C, or nil when the
// machine exposes no usable sensor (VMs, LXC, many boards) - callers must
// show "n/a", never 0.
func (s *systemService) GetCPUTemperature() *int {
	const keyName = "cpu_temperature_input"
	path := ""
	if cached, ok := Cache.Get(keyName); ok {
		path, _ = cached.(string)
	} else {
		path = findCPUHwmonInput()
		if path == "" {
			// GetCPUThermalZone falls back to thermal_zone0 even when that is
			// e.g. acpitz or a Wi-Fi chip; only accept a CPU/SoC zone here.
			if zone := GetCPUThermalZone(); zone != "" {
				zt := strings.ToLower(strings.TrimSpace(string(file.ReadFullFile(zone + "/type"))))
				if strings.Contains(zt, "cpu") || strings.Contains(zt, "soc") || strings.Contains(zt, "x86_pkg") {
					path = zone + "/temp"
				}
			}
		}
		if path != "" {
			logger.Info("CPU temperature sensor", zap.String("path", path))
		}
		Cache.SetDefault(keyName, path)
	}
	if path == "" {
		return nil
	}
	if v, ok := readMilliCelsius(path); ok {
		return &v
	}
	return nil
}

// raplZone is the package-0 energy counter (Intel, and AMD Zen via the
// same powercap interface on Linux 5.8+). Absent in VMs/containers/ARM.
func raplZone() string {
	for _, dir := range []string{"/sys/class/powercap/intel-rapl:0", "/sys/class/powercap/intel-rapl/intel-rapl:0"} {
		if file.Exists(dir + "/energy_uj") {
			return dir
		}
	}
	return ""
}

// GetCPUPower returns the raw package energy counter:
// {"value": µJ or nil when there is no RAPL, "max": counter range in µJ
// (it wraps back to 0 there), "timestamp": unix ms}. The UI derives watts
// from two samples.
func (s *systemService) GetCPUPower() map[string]interface{} {
	data := map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
		"value":     nil,
		"max":       0,
	}
	zone := raplZone()
	if zone == "" {
		return data
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(file.ReadFullFile(zone+"/energy_uj"))), 10, 64)
	if err != nil || v == 0 {
		return data
	}
	data["value"] = v
	if max, err := strconv.ParseUint(strings.TrimSpace(string(file.ReadFullFile(zone+"/max_energy_range_uj"))), 10, 64); err == nil {
		data["max"] = max
	}
	return data
}

func (s *systemService) SystemReboot() error {
	arg := []string{"6"}
	cmd := exec2.Command("init", arg...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}

func (s *systemService) SystemShutdown() error {
	arg := []string{"0"}
	cmd := exec2.Command("init", arg...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}

func NewSystemService() SystemService {
	return &systemService{}
}
