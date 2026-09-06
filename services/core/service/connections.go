/*
 * @Author: LinkLeong link@icewhale.org
 * @Date: 2022-07-26 18:13:22
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-08-04 20:10:31
 * @FilePath: /CasaOS/service/connections.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package service

import (
	"fmt"

	"github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	"github.com/moby/sys/mount"
	"golang.org/x/sys/unix"
	"gorm.io/gorm"
)

type ConnectionsService interface {
	GetConnectionsList() (connections []model2.ConnectionsDBModel)
	GetConnectionByHost(host string) (connections []model2.ConnectionsDBModel)
	GetConnectionByID(id string) (connections model2.ConnectionsDBModel)
	CreateConnection(connection *model2.ConnectionsDBModel)
	DeleteConnection(id string)
	UpdateConnection(connection *model2.ConnectionsDBModel)
	MountSmaba(username, host, directory, port, mountPoint, password string) error
	UnmountSmaba(mountPoint string) error
}

type connectionsStruct struct {
	db *gorm.DB
}

func (s *connectionsStruct) GetConnectionByHost(host string) (connections []model2.ConnectionsDBModel) {
	s.db.Where("host = ?", host).Find(&connections)
	return
}

func (s *connectionsStruct) GetConnectionByID(id string) (connections model2.ConnectionsDBModel) {
	s.db.Where("id = ?", id).First(&connections)
	return
}

// GetConnectionsList previously restricted its column selection to exclude
// "directories" - since every share this connection found on the remote
// host is stored there (comma-joined), the list this feeds (Settings and
// Files' Network Storage sidebar) always showed an empty directories value
// even for a connection that mounted real, browsable shares.
func (s *connectionsStruct) GetConnectionsList() (connections []model2.ConnectionsDBModel) {
	s.db.Find(&connections)
	return
}

func (s *connectionsStruct) CreateConnection(connection *model2.ConnectionsDBModel) {
	s.db.Create(connection)
}

func (s *connectionsStruct) UpdateConnection(connection *model2.ConnectionsDBModel) {
	s.db.Save(connection)
}

func (s *connectionsStruct) DeleteConnection(id string) {
	s.db.Where("id= ?", id).Delete(&model.ConnectionsDBModel{})
}

func (s *connectionsStruct) MountSmaba(username, host, directory, port, mountPoint, password string) error {
	err := unix.Mount(
		fmt.Sprintf("//%s/%s", host, directory),
		mountPoint,
		"cifs",
		unix.MS_NOATIME|unix.MS_NODEV|unix.MS_NOSUID,
		fmt.Sprintf("username=%s,password=%s", username, password),
	)
	return err
	// str := command2.ExecResultStr("source " + config.AppInfo.ShellPath + "/helper.sh ;MountCIFS " + username + " " + host + " " + directory + " " + port + " " + mountPoint + " " + password)
	// return str
}

func (s *connectionsStruct) UnmountSmaba(mountPoint string) error {
	if err := mount.Unmount(mountPoint); err == nil {
		return nil
	}
	return unix.Unmount(mountPoint, unix.MNT_DETACH)
}

func NewConnectionsService(db *gorm.DB) ConnectionsService {
	return &connectionsStruct{db: db}
}
