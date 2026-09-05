/*
 * @Author: LinkLeong link@icewhale.org
 * @Date: 2022-07-26 11:21:14
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-08-18 11:16:25
 * @FilePath: /CasaOS/service/shares.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package service

import (
	"path/filepath"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/command"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	"gorm.io/gorm"
)

type SharesService interface {
	GetSharesList() (shares []model2.SharesDBModel)
	GetSharesByPath(path string) (shares []model2.SharesDBModel)
	GetSharesByName(name string) (shares []model2.SharesDBModel)
	GetShareByID(id string) (share model2.SharesDBModel)
	CreateShare(share model2.SharesDBModel)
	UpdateShare(id string, name string, readOnly bool, anonymous bool) error
	DeleteShare(id string)
	UpdateConfigFile()
	InitSambaConfig()
	DeleteShareByPath(path string)
}

type sharesStruct struct {
	db *gorm.DB
}

func (s *sharesStruct) DeleteShareByPath(path string) {
	s.db.Where("path LIKE ?", path+"%").Delete(&model.SharesDBModel{})
	s.UpdateConfigFile()
}

func (s *sharesStruct) GetSharesByName(name string) (shares []model2.SharesDBModel) {
	s.db.Where("name = ?", name).Find(&shares)

	return
}

func (s *sharesStruct) GetSharesByPath(path string) (shares []model2.SharesDBModel) {
	s.db.Where("path = ?", path).Find(&shares)
	return
}

func (s *sharesStruct) GetSharesList() (shares []model2.SharesDBModel) {
	s.db.Find(&shares)
	return
}

func (s *sharesStruct) GetShareByID(id string) (share model2.SharesDBModel) {
	s.db.Where("id = ?", id).First(&share)
	return
}

func (s *sharesStruct) CreateShare(share model2.SharesDBModel) {
	s.db.Create(&share)
	s.InitSambaConfig()
	s.UpdateConfigFile()
}

func (s *sharesStruct) UpdateShare(id string, name string, readOnly bool, anonymous bool) error {
	updates := map[string]interface{}{
		"name":      name,
		"read_only": readOnly,
		"anonymous": anonymous,
	}
	if err := s.db.Model(&model.SharesDBModel{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	s.UpdateConfigFile()
	return nil
}

func (s *sharesStruct) DeleteShare(id string) {
	s.db.Where("id= ?", id).Delete(&model.SharesDBModel{})
	s.UpdateConfigFile()
}

func (s *sharesStruct) UpdateConfigFile() {
	shares := []model2.SharesDBModel{}
	s.db.Find(&shares)
	// generated config file
	configStr := ""
	for _, share := range shares {
		sectionName := share.Name
		if sectionName == "" {
			sectionName = filepath.Base(share.Path)
		}
		readOnly := "No"
		if share.ReadOnly {
			readOnly = "Yes"
		}
		// A guest ("Anonymous") share is world-writable-by-default and owned
		// by root regardless of who connects, matching what this always did
		// before per-share options existed. A non-guest share instead
		// requires a real SMB login (any account created via smbusers.go/
		// pdbedit) and lets that connecting user's own Unix permissions on
		// the path apply - "force user" and "guest ok = Yes" together would
		// otherwise make the guest-only behavior impossible to ever turn
		// off. There's no per-share user allowlist (every SMB user can
		// access every non-guest share) - a "valid users" restriction would
		// need its own dedicated group/UI concept this doesn't have yet.
		access := `guest ok = Yes
create mask = 0777
directory mask = 0777
force user = root`
		if !share.Anonymous {
			access = `guest ok = No`
		}
		configStr += `
[` + sectionName + `]
comment = NivaroOS share ` + sectionName + `
public = ` + map[bool]string{true: "Yes", false: "No"}[share.Anonymous] + `
path = ` + share.Path + `
browseable = Yes
read only = ` + readOnly + `
` + access + `

`
	}
	// write config file
	file.WriteToPath([]byte(configStr), "/etc/samba", "smb.nivaroos.conf")
	// restart samba
	command.OnlyExec("source " + config.AppInfo.ShellPath + "/helper.sh ;RestartSMBD")
}

func (s *sharesStruct) InitSambaConfig() {
	if file.Exists("/etc/samba/smb.conf") {
		str := file.ReadLine(1, "/etc/samba/smb.conf")
		if strings.Contains(str, "# Copyright (c) 2021-2022 NivaroOS Inc. All rights reserved.") {
			return
		}
		file.MoveFile("/etc/samba/smb.conf", "/etc/samba/smb.conf.bak")
		smbConf := ""
		smbConf += `# Copyright (c) 2021-2022 NivaroOS Inc. All rights reserved.
#
#
#                          ______     _______
#                        (  __  \   (  ___  )
#                        | (  \  )  | (   ) |
#                        | |   ) |  | |   | |
#                        | |   | |  | |   | |
#                        | |   ) |  | |   | |
#                        | (__/  )  | (___) |
#                        (______/   (_______)
#
#                   _          _______   _________
#                  ( (    /|  (  ___  )  \__   __/
#                  |  \  ( |  | (   ) |     ) (
#                  |   \ | |  | |   | |     | |
#                  | (\ \) |  | |   | |     | |
#                  | | \   |  | |   | |     | |
#                  | )  \  |  | (___) |     | |
#                  |/    )_)  (_______)     )_(
#
#   _______    _______    ______    _________   _______
#  (       )  (  ___  )  (  __  \   \__   __/  (  ____ \  |\     /|
#  | () () |  | (   ) |  | (  \  )     ) (     | (    \/  ( \   / )
#  | || || |  | |   | |  | |   ) |     | |     | (__       \ (_) /
#  | |(_)| |  | |   | |  | |   | |     | |     |  __)       \   /
#  | |   | |  | |   | |  | |   ) |     | |     | (           ) (
#  | )   ( |  | (___) |  | (__/  )  ___) (___  | )           | |
#  |/     \|  (_______)  (______/   \_______/  |/            \_/
#
#
# IMPORTANT: NivaroOS will not provide technical support for any issues
#            caused by unauthorized modification to the configuration.

[global]
## fruit settings
   min protocol = SMB2
   ea support = yes
## vfs objects = fruit streams_xattr
   fruit:metadata = stream
   fruit:model = Macmini
   fruit:veto_appledouble = no
   fruit:posix_rename = yes
   fruit:zero_file_id = yes
   fruit:wipe_intentionally_left_blank_rfork = yes
   fruit:delete_empty_adfiles = yes
   map to guest = bad user
   include=/etc/samba/smb.nivaroos.conf`
		file.WriteToPath([]byte(smbConf), "/etc/samba", "smb.conf")
	}
}

func NewSharesService(db *gorm.DB) SharesService {
	return &sharesStruct{db: db}
}
