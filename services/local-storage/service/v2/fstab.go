package v2

import (
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/fstab"
	"go.uber.org/zap"
)

func (s *LocalStorageService) SaveToFStab(m codegen.Mount) error {
	ft := fstab.Get()

	if err := ft.Add(fstab.Entry{
		MountPoint: m.MountPoint,

		Source:  *m.Source,
		FSType:  *m.Fstype,
		Options: *m.Options,
		Dump:    0,
		Pass:    fstab.PassDoNotCheck,
	}, true); err != nil {
		logger.Error("Error when trying to persist mount", zap.Error(err), zap.Any("mount", m))
		return err
	}
	return nil
}

func (s *LocalStorageService) RemoveFromFStab(mountpoint string) error {
	ft := fstab.Get()

	if err := ft.RemoveByMountPoint(mountpoint, false); err != nil {
		logger.Error("Error when trying to unpersist mount", zap.Error(err), zap.String("mount point", mountpoint))
		return err
	}
	return nil
}
