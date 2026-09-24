package route

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	message_bus "github.com/F-e-n-y-x/NivaroOS/services/user/codegen/message_bus"
	"github.com/F-e-n-y-x/NivaroOS/services/user/model"
	"github.com/F-e-n-y-x/NivaroOS/services/user/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// maxDialFailures: consecutive failed connection attempts (one a second)
// before giving up, as before. A successful connection resets the count,
// so a message-bus restart is survived.
const maxDialFailures = 1000

// EventListen records local-storage events from the message bus.
//
// It connects the way same-host automation must (see
// services/common/middleware/localauth.go): straight to the bus on
// loopback with no Origin header - the bus requires a user token from
// anything that looks like a browser, and golang.org/x/net/websocket
// (used here before) always sent "Origin: http://localhost".
func EventListen() {
	failures := 0
	for failures < maxDialFailures {
		messageBusUrl, err := external.GetMessageBusAddress(config.CommonInfo.RuntimePath)
		if err != nil {
			logger.Error("get message bus url error", zap.Any("err", err))
			return
		}

		wsURL := fmt.Sprintf("ws://%s/event/%s", strings.TrimPrefix(messageBusUrl, "http://"), "local-storage")
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			failures++
			logger.Error("connect websocket err"+strconv.Itoa(failures), zap.Any("error", err))
			time.Sleep(time.Second * 1)
			continue
		}
		failures = 0

		logger.Info("subscribed to", zap.Any("url", wsURL))
		readEvents(conn)
		conn.Close()

		// the bus went away (restart/update): reconnect
		time.Sleep(time.Second * 1)
	}
	logger.Error("error when try to connect to message bus")
}

func readEvents(conn *websocket.Conn) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			logger.Error("message bus websocket closed", zap.Any("err", err.Error()))
			return
		}

		var event message_bus.Event
		if err := json.Unmarshal(msg, &event); err != nil {
			logger.Error("err", zap.Any("err", err.Error()))
			continue
		}
		if event.Name == "local-storage:raid_status" {
			continue
		}
		propertiesStr, err := json.Marshal(event.Properties)
		if err != nil {
			logger.Error("marshal error", zap.Any("err", err.Error()), zap.Any("event", event))
			continue
		}
		uuid := ""
		if event.Uuid != nil {
			uuid = *event.Uuid
		}
		service.MyService.Event().CreateEvemt(model.EventModel{
			SourceID:   event.SourceID,
			Name:       event.Name,
			Properties: string(propertiesStr),
			UUID:       uuid,
		})
	}
}
