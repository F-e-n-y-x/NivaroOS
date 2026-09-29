/*
 * @Author: LinkLeong link@icewhale.org
 * @Date: 2022-07-26 17:17:57
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-08-01 17:08:08
 * @FilePath: /CasaOS/service/model/o_connections.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package model

import (
	"errors"
	"sync"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/secret"
	"gorm.io/gorm"
)

type ConnectionsDBModel struct {
	ID          uint   `gorm:"column:id;primary_key" json:"id"`
	Updated     int64  `gorm:"autoUpdateTime"`
	Created     int64  `gorm:"autoCreateTime"`
	Username    string `json:"username"`
	// Password is plaintext in memory and sealed (secret.Box, the host
	// key) in the database: the hooks below seal it on every write and
	// open it on every read. It is never sent to a client.
	Password    string `json:"-"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	Status      string `json:"status"`
	Directories string `json:"directories"` // string array
	MountPoint  string `json:"mount_point"` //parent directory of mount point
}

func (p *ConnectionsDBModel) TableName() string {
	return "o_connections"
}

// PasswordContext binds a sealed password to this column.
const PasswordContext = secret.ConnectionPassword

var (
	boxMu sync.RWMutex
	box   *secret.Box
)

// SetSecretBox sets the key connection passwords are sealed with (core's
// startup, from secret.LoadOrCreate). Without one no password is written.
func SetSecretBox(b *secret.Box) {
	boxMu.Lock()
	box = b
	boxMu.Unlock()
}

func secretBox() *secret.Box {
	boxMu.RLock()
	defer boxMu.RUnlock()
	return box
}

// ErrNoSecretBox: a password was about to be stored without the host key.
var ErrNoSecretBox = errors.New("connection passwords can't be stored: the host secret key isn't loaded")

// BeforeSave seals the password; a plaintext password is never written.
func (p *ConnectionsDBModel) BeforeSave(tx *gorm.DB) error {
	if p.Password == "" || secret.IsSealed(p.Password) {
		return nil
	}
	b := secretBox()
	if b == nil {
		return ErrNoSecretBox
	}
	sealed, err := b.Seal(p.Password, PasswordContext)
	if err != nil {
		return err
	}
	p.Password = sealed
	return nil
}

// AfterSave gives the caller its plaintext back.
func (p *ConnectionsDBModel) AfterSave(tx *gorm.DB) error {
	p.openPassword()
	return nil
}

// AfterFind opens the stored password (legacy plaintext passes through).
func (p *ConnectionsDBModel) AfterFind(tx *gorm.DB) error {
	p.openPassword()
	return nil
}

// openPassword: a password that can't be opened (the key was replaced) is
// dropped - the share then fails to mount with a login error instead of
// being sent the ciphertext as a password.
func (p *ConnectionsDBModel) openPassword() {
	if !secret.IsSealed(p.Password) {
		return
	}
	b := secretBox()
	if b == nil {
		p.Password = ""
		return
	}
	plain, err := b.Open(p.Password, PasswordContext)
	if err != nil {
		plain = ""
	}
	p.Password = plain
}
