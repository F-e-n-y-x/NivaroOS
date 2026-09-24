package v2

import (
	"errors"
	"testing"
)

func TestUmountRefusal(t *testing.T) {
	fstab := map[string]bool{"/mnt/backup": true, "/srv/data": true}
	for _, mp := range []string{"/", "/boot", "/boot/efi", "/DATA", "/usr", "/var", "/home", "/proc", "/sys/fs/cgroup", "/run/user/0", "/dev/shm", "/mnt/backup", "", "relative", "/mnt/x/../../etc", "/mnt/x/"} {
		if err := UmountRefusal(mp, fstab); err == nil || !errors.Is(err, ErrUmountRefused) {
			t.Errorf("%q allowed (%v)", mp, err)
		}
	}
	for _, mp := range []string{"/mnt/Storage_sdb1", "/DATA/Media", "/media/usb", "/mnt/gdrive"} {
		if err := UmountRefusal(mp, fstab); err != nil {
			t.Errorf("%q refused: %v", mp, err)
		}
	}
}
