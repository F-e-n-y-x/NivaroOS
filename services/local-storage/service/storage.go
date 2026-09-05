package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	_ "github.com/F-e-n-y-x/NivaroOS/services/local-storage/backend/terabox"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/mount"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/utils/command"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/utils/httper"
	_ "github.com/rclone/rclone/backend/all"
	"github.com/rclone/rclone/cmd/mountlib"
	"github.com/rclone/rclone/fs"
	rconfig "github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/rc"
	"github.com/rclone/rclone/vfs/vfscommon"
	"go.uber.org/zap"
)

type StorageService interface {
	MountStorage(mountPoint, fs string) error
	UnmountStorage(mountPoint string) error
	UnmountAllStorage()
	GetStorages() (httper.MountList, error)
	CreateConfig(data rc.Params, name string, t string) error
	CheckAndMountByName(name string) error
	CheckAndMountAll() error
	GetConfigByName(name string) []string
	GetAttributeValueByName(name, key string) string
	SetAttributeValue(name, key, value string, isPassword bool) error
	DeleteConfigByName(name string)
	GetConfig() (httper.RemotesResult, error)
}

type storageStruct struct {
}

var MountLists map[string]*mountlib.MountPoint
var mountMu sync.Mutex

func (s *storageStruct) MountStorage(mountPoint, deviceName string) error {
	file.IsNotExistMkDir(mountPoint)
	mountMu.Lock()
	defer mountMu.Unlock()
	currentFS, err := fs.NewFs(context.TODO(), deviceName+":")
	if err != nil {
		logger.Error("when CheckAndMountAll then", zap.Error(err))
		return err
	}
	mountOptin := mountlib.Options{
		MaxReadAhead:  128 * 1024,
		AttrTimeout:   fs.Duration(1 * time.Second),
		DaemonWait:    fs.Duration(60 * time.Second),
		NoAppleDouble: true,
		NoAppleXattr:  false,
		AsyncRead:     true,
		AllowOther:    true,
	}
	vfsOpt := vfscommon.Options{
		NoModTime:          false,
		NoChecksum:         false,
		NoSeek:             false,
		DirCacheTime:       fs.Duration(5 * 60 * time.Second),
		PollInterval:       fs.Duration(time.Minute),
		ReadOnly:           false,
		Umask:              18,
		UID:                0,
		GID:                0,
		DirPerms:           vfscommon.FileMode(0777),
		FilePerms:          vfscommon.FileMode(0666),
		CacheMode:          3,
		CacheMaxAge:        fs.Duration(3600 * time.Second),
		CachePollInterval:  fs.Duration(60 * time.Second),
		ChunkSize:          128 * fs.Mebi,
		ChunkSizeLimit:     -1,
		CacheMaxSize:       -1,
		CaseInsensitive:    runtime.GOOS == "windows" || runtime.GOOS == "darwin", // default to true on Windows and Mac, false otherwise
		WriteWait:          fs.Duration(1000 * time.Millisecond),
		ReadWait:           fs.Duration(20 * time.Millisecond),
		WriteBack:          fs.Duration(5 * time.Second),
		ReadAhead:          0 * fs.Mebi,
		UsedIsSize:         false,
		DiskSpaceTotalSize: -1,
	}

	mnt := mountlib.NewMountPoint(mount.MountFn, mountPoint, currentFS, &mountOptin, &vfsOpt)
	_, err = mnt.Mount()
	if err != nil {
		logger.Error("when CheckAndMountAll then", zap.Error(err))
		return err
	}
	go func() {
		if err = mnt.Wait(); err != nil {
			log.Printf("unmount FAILED: %v", err)
			return
		}
		mountMu.Lock()
		defer mountMu.Unlock()
		delete(MountLists, mountPoint)
	}()
	MountLists[mountPoint] = mnt
	return nil
}
func (s *storageStruct) UnmountStorage(mountPoint string) error {

	err := MountLists[mountPoint].Unmount()
	if err != nil {
		logger.Error("when umount then", zap.Error(err))
		return err
	}
	return nil
}
func (s *storageStruct) UnmountAllStorage() {
	for _, v := range MountLists {
		err := v.Unmount()
		if err != nil {
			logger.Error("when umount then", zap.Error(err))
		}
	}
}
func (s *storageStruct) GetStorages() (httper.MountList, error) {
	ls := httper.MountList{}
	list := []httper.MountPoints{}
	for _, v := range MountLists {
		list = append(list, httper.MountPoints{
			MountPoint: v.MountPoint,
			Fs:         v.Fs.Name(),
		})
	}
	ls.MountPoints = list
	return ls, nil
	// return httper.GetMountList()
}
// CreateConfig writes a fully-formed remote config section directly
// (LoadedData().SetValue per key, then SaveConfig) rather than going
// through rclone's interactive Config state machine - that machinery is
// meant for prompting a user through a flow one step at a time, and even
// with NonInteractive:true it still requires a state to resume, so it
// isn't a generic "just write these values" primitive.
//
// This is intentionally backend-agnostic: it's used both for
// non-interactive "form" providers (S3/B2/WebDAV/SFTP/SMB - every value
// is already known upfront) and for OAuth providers connected via a
// pasted `rclone authorize` token (Drive/Dropbox/OneDrive - the token is
// already valid, there's no auth step left to run).
func (s *storageStruct) CreateConfig(data rc.Params, name string, t string) error {
	ri, err := fs.Find(t)
	if err != nil {
		return err
	}
	needsObscure := make(map[string]struct{})
	for _, option := range ri.Options {
		if option.IsPassword {
			needsObscure[option.Name] = struct{}{}
		}
	}

	rconfig.LoadedData().SetValue(name, "type", t)
	for k, v := range data {
		vStr := fmt.Sprint(v)
		if _, ok := needsObscure[k]; ok {
			// Store passwords obscured, same as rclone's own config UI -
			// leave already-obscured values (e.g. round-tripped from a
			// previous read) alone rather than double-obscuring them.
			if _, revealErr := obscure.Reveal(vStr); revealErr != nil {
				obscured, obscureErr := obscure.Obscure(vStr)
				if obscureErr != nil {
					return fmt.Errorf("failed to obscure %q: %w", k, obscureErr)
				}
				vStr = obscured
			}
		}
		rconfig.LoadedData().SetValue(name, k, vStr)
	}
	rconfig.SaveConfig()
	return nil
}
func (s *storageStruct) CheckAndMountByName(name string) error {

	mountPoint, found := rconfig.LoadedData().GetValue(name, "mount_point")
	if !found && len(mountPoint) == 0 {
		logger.Error("when CheckAndMountAll then mountpint is empty", zap.String("mountPoint", mountPoint), zap.String("fs", name))
		return errors.New("mountpoint is empty")
	}
	return MyService.Storage().MountStorage(mountPoint, name)
}

func (s *storageStruct) CheckAndMountAll() error {
	section := rconfig.LoadedData().GetSectionList()

	logger.Info("when CheckAndMountAll section", zap.Any("section", section))
	for _, v := range section {
		command.OnlyExec("umount /mnt/" + v)
		mountPoint, found := rconfig.LoadedData().GetValue(v, "mount_point")

		if !found && len(mountPoint) == 0 {
			logger.Info("when CheckAndMountAll then mountpint is empty", zap.String("mountPoint", mountPoint), zap.String("fs", v))
			continue
		}
		err := MyService.Storage().MountStorage(mountPoint, v)
		if err != nil {
			logger.Error("when CheckAndMountAll then", zap.Error(err))
			return err
		}
	}
	return nil
}

func (s *storageStruct) GetConfigByName(name string) []string {
	return rconfig.LoadedData().GetKeyList(name)
}

func (s *storageStruct) GetAttributeValueByName(name, key string) string {
	value, found := rconfig.LoadedData().GetValue(name, key)
	if !found {
		return ""
	}
	return value
}

// SetAttributeValue writes a single config value for an existing remote (e.g.
// renaming its display label, or replacing its token/password when
// reconnecting) without touching anything else in its config section.
func (s *storageStruct) SetAttributeValue(name, key, value string, isPassword bool) error {
	if isPassword {
		if _, revealErr := obscure.Reveal(value); revealErr != nil {
			obscured, err := obscure.Obscure(value)
			if err != nil {
				return fmt.Errorf("failed to obscure %q: %w", key, err)
			}
			value = obscured
		}
	}
	rconfig.LoadedData().SetValue(name, key, value)
	rconfig.SaveConfig()
	return nil
}

func (s *storageStruct) DeleteConfigByName(name string) {
	rconfig.DeleteRemote(name)
}
func (s *storageStruct) GetConfig() (httper.RemotesResult, error) {
	//TODO: check data
	// section, err := httper.GetAllConfigName()
	// if err != nil {
	// 	return httper.RemotesResult{}, err
	// }
	// return section, nil
	return httper.RemotesResult{}, nil
}
func NewStorageService() StorageService {
	return &storageStruct{}
}
