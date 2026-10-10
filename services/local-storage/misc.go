package main

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/common"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/pilebones/go-udev/netlink"
	"go.uber.org/zap"
)

func sendDiskBySocket() {
	blkList := service.MyService.Disk().LSBLK(true)

	status := model.DiskStatus{}
	healthy := true

	var systemDisk *model.LSBLKModel

	for _, currentDisk := range blkList {

		if systemDisk == nil {
			// go 5 level deep to look for system block device by mount point being "/"
			systemDisk = service.WalkDisk(currentDisk, 5, func(blk model.LSBLKModel) bool { return blk.MountPoint == "/" })

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

		if !service.IsDiskSupported(currentDisk) {
			continue
		}
		temp := service.MyService.Disk().SmartCTL(currentDisk.Path)
		// a sleeping drive isn't reported unhealthy (last known health or unknown)
		if service.SmartHealth(temp) == "false" {
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

func sendUSBBySocket() {
	sendStatus("sys_usb", service.MyService.Disk().GetUSBDriveStatusList())
}

var (
	statusMu   sync.Mutex
	statusLast = map[string]sentStatus{}
)

type sentStatus struct {
	body string
	at   time.Time
}

// statusResend: an unchanged status is sent again after this long, so a
// restarted core (it only keeps the last value in memory) has it again.
const statusResend = time.Minute

// sendStatus pushes one system status key to core when it changed (or
// statusResend passed), not every 5 s tick.
func sendStatus(key string, v any) {
	statusMu.Lock()
	defer statusMu.Unlock()
	b, _ := json.Marshal(v)
	if l, ok := statusLast[key]; ok && l.body == string(b) && time.Since(l.at) < statusResend {
		return
	}
	if err := service.MyService.NotifySystem().SendSystemStatusNotify(map[string]interface{}{key: v}); err != nil {
		delete(statusLast, key)
		logger.Error("failed to send notify", zap.String("key", key), zap.Any("message", v), zap.Error(err))
		return
	}
	statusLast[key] = sentStatus{body: string(b), at: time.Now()}
}

func monitorUEvent(ctx context.Context) {
	var matcher netlink.Matcher

	conn := new(netlink.UEventConn)
	if err := conn.Connect(netlink.UdevEvent); err != nil {
		logger.Error("udev err", zap.Any("Unable to connect to Netlink Kobject UEvent socket", err))
	}
	defer conn.Close()

	queue := make(chan netlink.UEvent)
	defer close(queue)

	errors := make(chan error)
	defer close(errors)

	quit := conn.Monitor(queue, errors, matcher)
	defer close(quit)

	for {
		select {

		case <-ctx.Done():
			return

		case uevent := <-queue:

			if event := common.EventAdapter(uevent); event != nil {

				// add UI properties to applicable events so that the NivaroOS UI can render it
				event := common.EventAdapterWithUIProperties(event)

				if v, ok := event.Properties["local-storage:path"]; ok && strings.Contains(event.Name, "disk") {

					diskModel := service.MyService.Disk().GetDiskInfo(v)
					if !reflect.DeepEqual(diskModel, model.LSBLKModel{}) {

						properties := common.AdditionalProperties(diskModel)
						for k, v := range properties {
							event.Properties[k] = v
						}
					}
				}
				logger.Info("disk model", zap.Any("diskModel", event.Name))
				response, err := service.MyService.MessageBus().PublishEventWithResponse(ctx, event.SourceID, event.Name, event.Properties)
				if err != nil {
					logger.Error("failed to publish event to message bus", zap.Error(err), zap.Any("event", event))
				}

				if response.StatusCode() != http.StatusOK {
					logger.Error("failed to publish event to message bus", zap.String("status", response.Status()), zap.Any("response", response))
				}
			}

			switch uevent.Env["DEVTYPE"] {
			case "partition":

				switch uevent.Env["ID_BUS"] {
				case "usb":
					time.Sleep(1 * time.Second)
					service.MyService.Disk().RemoveLSBLKCache()
					sendUSBBySocket()
					continue
				}
			}

		case err := <-errors:
			logger.Error("udev err", zap.Error(err))
		}
	}
}

func sendStorageStats() {
	sendDiskBySocket()
	sendUSBBySocket()
}
