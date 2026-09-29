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

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/secret"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	"github.com/moby/sys/mount"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
	"gorm.io/gorm"
)

type ConnectionsService interface {
	GetConnectionsList() (connections []model2.ConnectionsDBModel)
	GetConnectionByHost(host string) (connections []model2.ConnectionsDBModel)
	GetConnectionByID(id string) (connections model2.ConnectionsDBModel)
	CreateConnection(connection *model2.ConnectionsDBModel) error
	DeleteConnection(id string)
	UpdateConnection(connection *model2.ConnectionsDBModel)
	MountSmaba(username, host, directory, port, mountPoint, password string) error
	UnmountSmaba(mountPoint string) error
	SealStoredPasswords() (int, error)
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

func (s *connectionsStruct) CreateConnection(connection *model2.ConnectionsDBModel) error {
	return s.db.Create(connection).Error
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

// SealStoredPasswords encrypts every password still stored in plaintext
// (connections saved before passwords were sealed). It runs at every start
// and only touches plaintext rows, so running it again changes nothing.
func (s *connectionsStruct) SealStoredPasswords() (int, error) {
	type row struct {
		ID       uint
		Password string
	}
	var rows []row
	if err := s.db.Model(&model2.ConnectionsDBModel{}).Select("id", "password").Where("password <> ''").Scan(&rows).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if secret.IsSealed(r.Password) {
			continue
		}
		c := model2.ConnectionsDBModel{ID: r.ID, Password: r.Password}
		if err := c.BeforeSave(s.db); err != nil {
			return n, err
		}
		// UpdateColumn: the value is already sealed; no hooks, no
		// updated-time bump.
		if err := s.db.Model(&model2.ConnectionsDBModel{ID: r.ID}).UpdateColumn("password", c.Password).Error; err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// InitConnectionSecrets loads (on first start: creates) the host key that
// seals saved connection passwords, then seals any still in plaintext.
func InitConnectionSecrets(keyPath string) error {
	box, err := secret.LoadOrCreate(keyPath)
	if err != nil {
		return fmt.Errorf("host secret key %s: %w", keyPath, err)
	}
	model2.SetSecretBox(box)
	n, err := MyService.Connections().SealStoredPasswords()
	if err != nil {
		return fmt.Errorf("sealing stored connection passwords: %w", err)
	}
	if n > 0 {
		logger.Info("sealed stored network-share passwords", zap.Int("count", n))
	}
	return nil
}

func NewConnectionsService(db *gorm.DB) ConnectionsService {
	return &connectionsStruct{db: db}
}
