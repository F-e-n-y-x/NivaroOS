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
	"regexp"
	"runtime"
	"strconv"
	"strings"
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
	need, version := version.IsNeedUpdate(service.MyService.Casa().GetCasaosVersion())
	if need {
		installLog := model2.AppNotify{}
		installLog.State = 0
		installLog.Message = "New version " + version.Version + " is ready, ready to upgrade"
		installLog.Type = types.NOTIFY_TYPE_NEED_CONFIRM
		installLog.CreatedAt = strconv.FormatInt(time.Now().Unix(), 10)
		installLog.UpdatedAt = strconv.FormatInt(time.Now().Unix(), 10)
		installLog.Name = "Recasa System"
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
	need, version := version.IsNeedUpdate(service.MyService.Casa().GetCasaosVersion())
	if need {
		service.MyService.System().UpdateSystemVersion(version.Version)
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary  get logs
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/error/logs [get]
func GetCasaOSErrorLogs(ctx echo.Context) error {
	line, _ := strconv.Atoi(utils.DefaultQuery(ctx, "line", "100"))
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: service.MyService.System().GetCasaOSLogs(line)})
}

// 系统配置
func GetSystemConfigDebug(ctx echo.Context) error {
	array := service.MyService.System().GetSystemConfigDebug()
	disk := service.MyService.System().GetDiskInfo()
	sys := service.MyService.System().GetSysInfo()
	version := service.MyService.Casa().GetCasaosVersion()
	var bugContent string = fmt.Sprintf(`
	 - OS: %s
	 - Recasa Version: %s
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

// @Summary get casaos server port
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/port [get]
func GetCasaOSPort(ctx echo.Context) error {
	return ctx.JSON(common_err.SUCCESS,
		model.Result{
			Success: common_err.SUCCESS,
			Message: common_err.GetMsg(common_err.SUCCESS),
			Data:    config.ServerInfo.HttpPort,
		})
}

// @Summary edit casaos server port
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Param port json string true "port"
// @Success 200 {string} string "ok"
// @Router /sys/port [put]
func PutCasaOSPort(ctx echo.Context) error {
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

// @Summary active killing casaos
// @Produce  application/json
// @Accept application/json
// @Tags sys
// @Security ApiKeyAuth
// @Success 200 {string} string "ok"
// @Router /sys/restart [post]
func PostKillCasaOS(ctx echo.Context) error {
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
	Total      string `json:"total"`
	Used       string `json:"used"`
	Percent    string `json:"percent"`
}

// GetSystemDisksUsage shells `df` for a human-readable, per-mount usage
// breakdown across every real filesystem (excluding virtual ones like
// tmpfs/overlay) - the single-mount GetSystemDiskInfo/GetDiskInfo only
// ever reports on "/", which misses every other mounted data volume.
func GetSystemDisksUsage(ctx echo.Context) error {
	out, err := exec.Command("sh", "-c", "df -h --output=target,fstype,size,used,pcent -x tmpfs -x devtmpfs -x overlay -x squashfs -x efivarfs 2>/dev/null | tail -n +2").Output()
	disks := []diskUsageEntry{}
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				continue
			}
			disks = append(disks, diskUsageEntry{
				MountPoint: fields[0],
				Fstype:     fields[1],
				Total:      fields[2],
				Used:       fields[3],
				Percent:    fields[4],
			})
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
