/*
 * @Author: LinkLeong link@icewhale.org
 * @Date: 2022-11-15 15:51:44
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-11-15 15:55:16
 * @FilePath: /CasaOS/route/init.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package route

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	file1 "github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/core/common"
	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/samba"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/encryption"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/file"
	v1 "github.com/F-e-n-y-x/NivaroOS/services/core/route/v1"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"go.uber.org/zap"
)

func InitFunction() {
	go InitNetworkMount()
	go InitInfo()
	//go InitZerotier()
}

func InitInfo() {
	mb := model.BaseInfo{}
	if file.Exists(config.AppInfo.DBPath + "/baseinfo.conf") {
		err := json.Unmarshal(file.ReadFullFile(config.AppInfo.DBPath+"/baseinfo.conf"), &mb)
		if err != nil {
			logger.Error("baseinfo.conf", zap.String("error", err.Error()))
		}
	}
	if file.Exists("/etc/CHANNEL") {
		channel := file.ReadFullFile("/etc/CHANNEL")
		mb.Channel = string(channel)
	}
	mac, err := service.MyService.System().GetMacAddress()
	if err != nil {
		logger.Error("GetMacAddress", zap.String("error", err.Error()))
	}
	mb.Hash = encryption.GetMD5ByStr(mac)
	mb.Version = common.VERSION
	osRelease, _ := file1.ReadOSRelease()

	mb.DriveModel = osRelease["MODEL"]
	if len(mb.DriveModel) == 0 {
		mb.DriveModel = "Casa"
	}
	os.Remove(config.AppInfo.DBPath + "/baseinfo.conf")
	by, err := json.Marshal(mb)
	if err != nil {
		logger.Error("init info err", zap.Any("err", err))
		return
	}
	file.WriteToFullPath(by, config.AppInfo.DBPath+"/baseinfo.conf", 0o666)
}

func InitNetworkMount() {
	time.Sleep(time.Second * 10)
	connections := service.MyService.Connections().GetConnectionsList()
	for _, v := range connections {
		connection := service.MyService.Connections().GetConnectionByID(fmt.Sprint(v.ID))
		directories, err := samba.GetSambaSharesList(connection.Host, connection.Port, connection.Username, connection.Password)
		if err != nil {
			// Do NOT delete the connection if host is temporarily unreachable on boot!
			logger.Info("samba host currently unreachable on boot, keeping connection", zap.Any("err", err), zap.Any("host", connection.Host))
			continue
		}
		baseHostPath := "/mnt/" + connection.Host
		file.IsNotExistMkDir(baseHostPath)

		for _, dirName := range directories {
			mountPoint := baseHostPath + "/" + dirName
			file.IsNotExistMkDir(mountPoint)
			if !service.IsMounted(mountPoint) {
				_ = service.MyService.Connections().MountSmaba(connection.Username, connection.Host, dirName, connection.Port, mountPoint, connection.Password)
			}
		}
		connection.Directories = strings.Join(directories, ",")
		service.MyService.Connections().UpdateConnection(&connection)
	}
	err := service.MyService.Storage().CheckAndMountAll()
	if err != nil {
		logger.Error("mount storage err", zap.Any("err", err))
	}
}
func InitZerotier() {
	v1.CheckNetwork()
}
