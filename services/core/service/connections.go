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
	"net"
	"strings"

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

// cifsOptions builds the kernel CIFS mount options. The kernel can't
// resolve names (ip= is required for a hostname), the port was ignored,
// and a comma in the user name or password ended the field - CIFS escapes
// a literal comma by doubling it.
func cifsOptions(username, password, port, ip string) string {
	esc := func(v string) string { return strings.ReplaceAll(v, ",", ",,") }
	opts := []string{"username=" + esc(username), "password=" + esc(password)}
	if port != "" {
		opts = append(opts, "port="+port)
	}
	if ip != "" {
		opts = append(opts, "ip="+ip)
	}
	return strings.Join(opts, ",")
}

func (s *connectionsStruct) MountSmaba(username, host, directory, port, mountPoint, password string) error {
	ip := host
	if net.ParseIP(host) == nil {
		addrs, err := net.LookupHost(host)
		if err != nil || len(addrs) == 0 {
			return fmt.Errorf("can't resolve %s", host)
		}
		ip = addrs[0]
	}
	return unix.Mount(
		fmt.Sprintf("//%s/%s", host, directory),
		mountPoint,
		"cifs",
		unix.MS_NOATIME|unix.MS_NODEV|unix.MS_NOSUID,
		cifsOptions(username, password, port, ip),
	)
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
