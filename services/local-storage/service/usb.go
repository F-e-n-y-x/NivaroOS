package service

import (
	"os"

	"github.com/F-e-n-y-x/recasa/services/common/utils/logger"
	"github.com/F-e-n-y-x/recasa/services/local-storage/pkg/config"
	command2 "github.com/F-e-n-y-x/recasa/services/local-storage/pkg/utils/command"
	"github.com/shirou/gopsutil/host"
	"go.uber.org/zap"
)

type USBService interface {
	UpdateUSBAutoMount(state string)
	ExecUSBAutoMountShell(state string)

	GetSysInfo() host.InfoStat
	GetDeviceTree() (string, error)
}

type usbService struct{}

func (s *usbService) UpdateUSBAutoMount(state string) {
	config.ServerInfo.USBAutoMount = state
	config.Cfg.Section("server").Key("USBAutoMount").SetValue(state)
	if err := config.Cfg.SaveTo(config.ConfigFilePath); err != nil {
		logger.Error("error when saving USB automount configuration", zap.Error(err), zap.String("path", config.ConfigFilePath))
	}
}

func (s *usbService) ExecUSBAutoMountShell(state string) {
	if state == "False" {
		if _, err := command2.OnlyExec("source " + config.AppInfo.ShellPath + "/local-storage-helper.sh ;USB_Stop_Auto"); err != nil {
			logger.Error("error when executing shell script to stop USB automount", zap.Error(err))
		}
	} else {
		if _, err := command2.OnlyExec("source " + config.AppInfo.ShellPath + "/local-storage-helper.sh ;USB_Start_Auto"); err != nil {
			logger.Error("error when executing shell script to start USB automount", zap.Error(err))
		}
	}
}

func (s *usbService) GetSysInfo() host.InfoStat {
	info, _ := host.Info()
	return *info
}

func (s *usbService) GetDeviceTree() (string, error) {
	deviceTreeFilePath := "/proc/device-tree/model"

	if _, err := os.Stat(deviceTreeFilePath); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	// read string from deviceTreeFilePath
	deviceTree, err := os.ReadFile(deviceTreeFilePath)
	if err != nil {
		return "", err
	}

	return string(deviceTree), nil
}

func NewUSBService() USBService {
	return &usbService{}
}
