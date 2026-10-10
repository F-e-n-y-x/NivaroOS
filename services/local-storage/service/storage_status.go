package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/config"
	"go.uber.org/zap"
)

// Disk and USB status pushed to core (sys_disk, sys_usb) every 5 s.

func sendDiskBySocket() {
	blkList := MyService.Disk().LSBLK(true)

	status := model.DiskStatus{}
	healthy := true

	var systemDisk *model.LSBLKModel

	for _, currentDisk := range blkList {

		if systemDisk == nil {
			// go 5 level deep to look for system block device by mount point being "/"
			systemDisk = WalkDisk(currentDisk, 5, func(blk model.LSBLKModel) bool { return blk.MountPoint == "/" })

			if systemDisk != nil {
				s, _ := strconv.ParseUint(systemDisk.FSSize.String(), 10, 64)
				a, _ := strconv.ParseUint(systemDisk.FSAvail.String(), 10, 64)
				u, _ := strconv.ParseUint(systemDisk.FSUsed.String(), 10, 64)
				status.Size += s
				status.Avail += a
				status.Used += u

				continue
			}
		}

		if !IsDiskSupported(currentDisk) {
			continue
		}
		temp := MyService.Disk().SmartCTL(currentDisk.Path)
		// a sleeping drive isn't reported unhealthy (last known health or unknown)
		if SmartHealth(temp) == "false" {
			healthy = false
		}
		if len(currentDisk.Children) > 0 {
			for _, v := range currentDisk.Children {
				if len(v.MountPoint) > 0 {
					s, _ := strconv.ParseUint(v.FSSize.String(), 10, 64)
					a, _ := strconv.ParseUint(v.FSAvail.String(), 10, 64)
					u, _ := strconv.ParseUint(v.FSUsed.String(), 10, 64)
					status.Size += s
					status.Avail += a
					status.Used += u
				}
			}
		} else {
			if len(currentDisk.MountPoint) > 0 {
				s, _ := strconv.ParseUint(currentDisk.FSSize.String(), 10, 64)
				a, _ := strconv.ParseUint(currentDisk.FSAvail.String(), 10, 64)
				u, _ := strconv.ParseUint(currentDisk.FSUsed.String(), 10, 64)
				status.Size += s
				status.Avail += a
				status.Used += u
			}
		}

	}

	status.Health = healthy

	sendStatus("sys_disk", status)
}

func SendUSBStatus() {
	sendStatus("sys_usb", MyService.Disk().GetUSBDriveStatusList())
}

var (
	statusMu   sync.Mutex
	statusLast = map[string]sentStatus{}
	// coreStarted: mtime of core's address file, which core rewrites at
	// every start - a change means a new core with no status yet.
	coreStarted time.Time
	// seams for tests
	coreURLFile = func() string { return filepath.Join(config.CommonInfo.RuntimePath, external.NivaroOSURLFilename) }
	pushStatus  = func(m map[string]interface{}) error { return MyService.NotifySystem().SendSystemStatusNotify(m) }
)

type sentStatus struct {
	body string
	at   time.Time
}

// statusResend: an unchanged status is sent again after this long, so a
// restarted core (it only keeps the last value in memory) has it again.
const statusResend = time.Minute

// sendStatus pushes one system status key to core when it changed (or
// statusResend passed, or core restarted), not every 5 s tick.
func sendStatus(key string, v any) {
	statusMu.Lock()
	defer statusMu.Unlock()
	if fi, err := os.Stat(coreURLFile()); err == nil && !fi.ModTime().Equal(coreStarted) {
		coreStarted = fi.ModTime()
		clear(statusLast)
	}
	b, _ := json.Marshal(v)
	if l, ok := statusLast[key]; ok && l.body == string(b) && time.Since(l.at) < statusResend {
		return
	}
	if err := pushStatus(map[string]interface{}{key: v}); err != nil {
		delete(statusLast, key)
		logger.Error("failed to send notify", zap.String("key", key), zap.Any("message", v), zap.Error(err))
		return
	}
	statusLast[key] = sentStatus{body: string(b), at: time.Now()}
}

// SendStorageStats: one fresh lsblk per 5 s tick, which disk usage,
// SATA/NVMe hot-plugs and the USB list all read from the cache.
func SendStorageStats() {
	MyService.Disk().LSBLK(false)
	sendDiskBySocket()
	SendUSBStatus()
}
