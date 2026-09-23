/*
 * @Author: LinkLeong link@icewhale.com
 * @Date: 2022-07-26 11:08:48
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-08-17 18:25:42
 * @FilePath: /CasaOS/route/v1/samba.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package v1

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/systemctl"
	"github.com/labstack/echo/v4"
	"golang.org/x/sys/unix"

	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/samba"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
)

// service

func GetSambaStatus(ctx echo.Context) error {
	if status, err := systemctl.IsServiceRunning("smbd.service"); err != nil || !status {
		return ctx.JSON(http.StatusInternalServerError, model.Result{
			Success: common_err.SERVICE_NOT_RUNNING,
			Message: common_err.GetMsg(common_err.SERVICE_NOT_RUNNING),
		})
	}

	needInit := true
	if file.Exists("/etc/samba/smb.conf") {
		str := file.ReadLine(1, "/etc/samba/smb.conf")
		if strings.Contains(str, "# Copyright (c) 2021-2022 NivaroOS Inc. All rights reserved.") {
			needInit = false
		}
	}
	data := make(map[string]string, 1)
	data["need_init"] = fmt.Sprintf("%v", needInit)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

func GetSambaSharesList(ctx echo.Context) error {
	shares := service.MyService.Shares().GetSharesList()
	shareList := []model.Shares{}
	for _, v := range shares {
		shareList = append(shareList, model.Shares{
			Anonymous: v.Anonymous,
			Path:      v.Path,
			ID:        v.ID,
			Name:      v.Name,
			ReadOnly:  v.ReadOnly,
		})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: shareList})
}

// shareRoots: the only places a network share may point into.
var shareRoots = []string{"/DATA", "/mnt", "/media"}

var validShareName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)

// validateShareName: the name becomes an smb.conf section header, so no
// brackets, newlines or other syntax - a name like "x]\nroot preexec = ..."
// ran commands as root - and not one of Samba's own sections.
func validateShareName(name string) error {
	if !validShareName.MatchString(name) {
		return errors.New("share names use letters, digits, spaces, dot, dash and underscore")
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "global", "homes", "printers", "print$", "ipc$":
		return errors.New("that name is reserved by Samba")
	}
	return nil
}

// validateSharePath: an existing folder inside one of the roots (any path,
// "/" or "/etc" included, used to be shared - and chmod 0777-ed).
func validateSharePath(p string, roots []string) error {
	if !filepath.IsAbs(p) || strings.ContainsAny(p, "\r\n\x00") {
		return errors.New("invalid folder path")
	}
	p = filepath.Clean(p)
	inside := false
	for _, r := range roots {
		if p == r || strings.HasPrefix(p, r+"/") {
			inside = true
			break
		}
	}
	if !inside {
		return fmt.Errorf("only folders under %s can be shared", strings.Join(roots, ", "))
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.IsDir() {
		return errors.New("that folder doesn't exist")
	}
	return nil
}

func isShareRoot(p string) bool {
	for _, r := range shareRoots {
		if filepath.Clean(p) == r {
			return true
		}
	}
	return false
}

func PostSambaSharesCreate(ctx echo.Context) error {
	shares := []model.Shares{}
	ctx.Bind(&shares)
	for i, v := range shares {
		if v.Path == "" {
			return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INSUFFICIENT_PERMISSIONS, Message: common_err.GetMsg(common_err.INSUFFICIENT_PERMISSIONS)})
		}
		if err := validateSharePath(v.Path, shareRoots); err != nil {
			return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: err.Error()})
		}
		shares[i].Path = filepath.Clean(v.Path)
		if shares[i].Name == "" {
			shares[i].Name = filepath.Base(shares[i].Path)
		}
		if err := validateShareName(shares[i].Name); err != nil {
			return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: err.Error()})
		}
		if len(service.MyService.Shares().GetSharesByPath(shares[i].Path)) > 0 {
			return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.SHARE_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.SHARE_ALREADY_EXISTS)})
		}
		// (this looked the name up in the path column, so duplicate names slipped through)
		if len(service.MyService.Shares().GetSharesByName(shares[i].Name)) > 0 {
			return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.SHARE_NAME_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.SHARE_NAME_ALREADY_EXISTS)})
		}
	}
	for i, v := range shares {
		shareDBModel := model2.SharesDBModel{}
		shareDBModel.Anonymous = v.Anonymous
		shareDBModel.ReadOnly = v.ReadOnly
		shareDBModel.Path = v.Path
		shareDBModel.Name = v.Name
		// Opening the folder up for SMB users is kept, but never for the
		// data roots themselves (and only ever inside them - see above).
		if !isShareRoot(v.Path) {
			os.Chmod(v.Path, 0o777)
		}
		service.MyService.Shares().CreateShare(shareDBModel)
		shares[i].Name = shareDBModel.Name
	}

	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: shares})
}

func PutSambaShare(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INSUFFICIENT_PERMISSIONS, Message: common_err.GetMsg(common_err.INSUFFICIENT_PERMISSIONS)})
	}
	existing := service.MyService.Shares().GetShareByID(id)
	if existing.ID == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.Record_NOT_EXIST, Message: common_err.GetMsg(common_err.Record_NOT_EXIST)})
	}

	req := model.Shares{}
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INSUFFICIENT_PERMISSIONS, Message: common_err.GetMsg(common_err.INSUFFICIENT_PERMISSIONS)})
	}
	name := req.Name
	if name == "" {
		name = filepath.Base(existing.Path)
	}
	if err := validateShareName(name); err != nil {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: err.Error()})
	}
	if other := service.MyService.Shares().GetSharesByName(name); len(other) > 0 && strconv.Itoa(int(other[0].ID)) != id {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.SHARE_NAME_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.SHARE_NAME_ALREADY_EXISTS)})
	}
	if err := service.MyService.Shares().UpdateShare(id, name, req.ReadOnly, req.Anonymous); err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: id})
}

func DeleteSambaShares(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INSUFFICIENT_PERMISSIONS, Message: common_err.GetMsg(common_err.INSUFFICIENT_PERMISSIONS)})
	}
	service.MyService.Shares().DeleteShare(id)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: id})
}

// statfsUsage reads real block-device usage for an already-mounted path via
// statfs(2) - the same syscall `df` itself uses - so it works for any live
// mount (cifs, rclone/FUSE, local) without shelling out. Returns ok=false for
// an unmounted/stale mountpoint (e.g. a samba connection that's registered
// but not currently connected) so callers can omit the usage fields instead
// of reporting 0/0.
func statfsUsage(path string) (size, avail, used uint64, ok bool) {
	if path == "" {
		return 0, 0, 0, false
	}
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, 0, false
	}
	bsize := uint64(st.Bsize)
	size = st.Blocks * bsize
	avail = st.Bavail * bsize
	used = size - (st.Bfree * bsize)
	return size, avail, used, true
}

// client
func GetSambaConnectionsList(ctx echo.Context) error {
	connections := service.MyService.Connections().GetConnectionsList()
	connectionList := []model.Connections{}
	for _, v := range connections {
		item := model.Connections{
			ID:          v.ID,
			Username:    v.Username,
			Port:        v.Port,
			Host:        v.Host,
			MountPoint:  v.MountPoint,
			Directories: v.Directories,
		}
		if size, avail, used, ok := statfsUsage(v.MountPoint); ok {
			item.Size = strconv.FormatUint(size, 10)
			item.Avail = strconv.FormatUint(avail, 10)
			item.Used = strconv.FormatUint(used, 10)
		}
		connectionList = append(connectionList, item)
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: connectionList})
}

func PostSambaConnectionsCreate(ctx echo.Context) error {
	connection := model.Connections{}
	ctx.Bind(&connection)
	if connection.Port == "" {
		connection.Port = "445"
	}
	if connection.Username == "" || connection.Host == "" {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CHARACTER_LIMIT, Message: common_err.GetMsg(common_err.CHARACTER_LIMIT)})
	}

	// if ok, _ := regexp.MatchString(`^[\w@#*.]{4,30}$`, connection.Password); !ok {
	// 	return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CHARACTER_LIMIT, Message: common_err.GetMsg(common_err.CHARACTER_LIMIT)})
	// 	return
	// }
	// if ok, _ := regexp.MatchString(`^[\w@#*.]{4,30}$`, connection.Username); !ok {
	// 	return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	// 	return
	// }
	// if !ip_helper.IsIPv4(connection.Host) && !ip_helper.IsIPv6(connection.Host) {
	// 	return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	// 	return
	// }
	// if ok, _ := regexp.MatchString("^[0-9]{1,6}$", connection.Port); !ok {
	// 	return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	// 	return
	// }

	connection.Host = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(connection.Host), "\\\\"), "//")
	connection.Host = strings.Split(connection.Host, "/")[0]
	if connection.Host == "" || strings.ContainsAny(connection.Host, " ,;\\\r\n") {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: "enter the server's name or IP address"})
	}
	if p, err := strconv.Atoi(connection.Port); err != nil || p < 1 || p > 65535 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: "the port must be a number from 1 to 65535"})
	}
	// check is exists
	connections := service.MyService.Connections().GetConnectionByHost(connection.Host)
	if len(connections) > 0 {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.Record_ALREADY_EXIST, Message: common_err.GetMsg(common_err.Record_ALREADY_EXIST), Data: common_err.GetMsg(common_err.Record_ALREADY_EXIST)})
	}
	// check connect is ok
	directories, err := samba.GetSambaSharesList(connection.Host, connection.Port, connection.Username, connection.Password)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}

	connectionDBModel := model2.ConnectionsDBModel{}
	connectionDBModel.Username = connection.Username
	connectionDBModel.Password = connection.Password
	connectionDBModel.Host = connection.Host
	connectionDBModel.Port = connection.Port
	connectionDBModel.Directories = strings.Join(directories, ",")
	baseHostPath := "/mnt/" + connection.Host
	connectionDBModel.MountPoint = baseHostPath
	connection.MountPoint = baseHostPath
	file.IsNotExistMkDir(baseHostPath)
	// Every mount is checked (they were ignored, so a connection whose
	// shares all failed to mount still said "Connected").
	var mountedDirs, failed []string
	for _, v := range directories {
		if strings.HasSuffix(v, "$") {
			continue // IPC$, ADMIN$, C$: not browsable shares
		}
		mountPoint := baseHostPath + "/" + v
		file.IsNotExistMkDir(mountPoint)
		if err := service.MyService.Connections().MountSmaba(connectionDBModel.Username, connectionDBModel.Host, v, connectionDBModel.Port, mountPoint, connectionDBModel.Password); err != nil {
			os.Remove(mountPoint)
			failed = append(failed, v+": "+err.Error())
			continue
		}
		mountedDirs = append(mountedDirs, v)
	}
	if len(mountedDirs) == 0 {
		os.Remove(baseHostPath)
		msg := "no shares could be mounted"
		if len(failed) > 0 {
			msg += " (" + strings.Join(failed, "; ") + ")"
		}
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: msg, Data: msg})
	}
	connectionDBModel.Directories = strings.Join(mountedDirs, ",")

	service.MyService.Connections().CreateConnection(&connectionDBModel)

	connection.ID = connectionDBModel.ID
	connection.Directories = connectionDBModel.Directories
	msg := common_err.GetMsg(common_err.SUCCESS)
	if len(failed) > 0 {
		msg = "connected, but some shares could not be mounted: " + strings.Join(failed, "; ")
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: msg, Data: connection})
}

func DeleteSambaConnections(ctx echo.Context) error {
	id := ctx.Param("id")
	connection := service.MyService.Connections().GetConnectionByID(id)
	if connection.Username == "" {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.Record_NOT_EXIST, Message: common_err.GetMsg(common_err.Record_NOT_EXIST)})
	}

	baseHostPath := "/mnt/" + connection.Host
	dirSet := make(map[string]bool)
	if connection.Directories != "" {
		for _, d := range strings.Split(connection.Directories, ",") {
			d = strings.TrimSpace(d)
			if d != "" {
				dirSet[d] = true
			}
		}
	}
	if mountPointList, err := samba.GetSambaSharesList(connection.Host, connection.Port, connection.Username, connection.Password); err == nil {
		for _, d := range mountPointList {
			if d != "" {
				dirSet[d] = true
			}
		}
	}
	if entries, err := os.ReadDir(baseHostPath); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				dirSet[e.Name()] = true
			}
		}
	}

	for d := range dirSet {
		sharePath := filepath.Join(baseHostPath, d)
		_ = service.MyService.Connections().UnmountSmaba(sharePath)
		_ = os.Remove(sharePath) // Safe: only removes empty dir, never deletes files
	}
	_ = os.Remove(baseHostPath)
	if connection.MountPoint != "" && connection.MountPoint != baseHostPath {
		_ = os.Remove(connection.MountPoint)
	}

	service.MyService.Connections().DeleteConnection(id)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: id})
}
