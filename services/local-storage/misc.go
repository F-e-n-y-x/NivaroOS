package main

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/common"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/pilebones/go-udev/netlink"
	"go.uber.org/zap"
)

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
					service.SendUSBStatus()
					continue
				}
			}

		case err := <-errors:
			logger.Error("udev err", zap.Error(err))
		}
	}
}
