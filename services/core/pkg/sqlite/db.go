/*
 * @Author: LinkLeong link@icewhale.com
 * @Date: 2022-05-13 18:15:46
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-08-31 13:39:24
 * @FilePath: /CasaOS/pkg/sqlite/db.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package sqlite

import (
	"fmt"
	"os"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/file"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var gdb *gorm.DB

func GetDb(dbPath string) *gorm.DB {
	if gdb != nil {
		return gdb
	}
	// Refer https://github.com/go-sql-driver/mysql#dsn-data-source-name
	// dsn := fmt.Sprintf("%v:%v@tcp(%v:%v)/%v?charset=utf8mb4&parseTime=True&loc=Local", m.User, m.PWD, m.IP, m.Port, m.DBName)
	// db, err := gorm.Open(mysql2.Open(dsn), &gorm.Config{})
	file.IsNotExistMkDir(dbPath)
	// The database directory held every service's database world-writable
	// (0777: any local account could swap user.db or the JWT signing key)
	// and this one world-readable (saved share credentials). Root only;
	// every service using it runs as root. Repaired on every start.
	_ = os.Chmod(dbPath, 0o700)
	db, err := gorm.Open(sqlite.Open(dbPath+"/casaOS.db"), &gorm.Config{})
	if err != nil {
		panic("sqlite connect error")
	}
	_ = os.Chmod(dbPath+"/casaOS.db", 0o600)

	c, _ := db.DB()
	c.SetMaxIdleConns(10)
	c.SetMaxOpenConns(1)
	c.SetConnMaxIdleTime(time.Second * 1000)
	gdb = db

	err = db.AutoMigrate(&model2.AppNotify{}, model2.SharesDBModel{}, model2.ConnectionsDBModel{}, model2.PeerDriveDBModel{}, model2.QuickShareDBModel{})
	if err != nil {
		fmt.Println(err)
	}

	db.Exec("DROP TABLE IF EXISTS o_application")
	db.Exec("DROP TABLE IF EXISTS o_friend")
	db.Exec("DROP TABLE IF EXISTS o_person_download")
	db.Exec("DROP TABLE IF EXISTS o_person_down_record")
	return db
}
