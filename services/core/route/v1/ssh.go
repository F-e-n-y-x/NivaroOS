package v1

import (
	"time"

	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	sshHelper "github.com/F-e-n-y-x/NivaroOS/services/common/utils/ssh"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	modelCommon "github.com/F-e-n-y-x/NivaroOS/services/common/model"
)

// The old GET /v1/sys/wsssh handler (WsSsh) was removed: it had no UI
// caller, took the SSH password in the query string (so it landed in
// request logs), retried bad credentials in a tight loop forever and
// dereferenced a nil connection when the upgrade failed. The local
// terminal is /v1/sys/wsterm (localterm.go).

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Non-browser clients (no Origin) or pages served from this same host
	// only - a blanket `return true` let any website a logged-in admin
	// visited open the local terminal socket (CSWSH).
	CheckOrigin:      nivaroos_middleware.CheckWebSocketOrigin,
	HandshakeTimeout: time.Duration(time.Second * 5),
}

func PostSshLogin(ctx echo.Context) error {
	j := make(map[string]string)
	ctx.Bind(&j)
	userName := j["username"]
	password := j["password"]
	port := j["port"]
	if userName == "" || password == "" || port == "" {
		return ctx.JSON(common_err.CLIENT_ERROR, modelCommon.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS), Data: "Username or password or port is empty"})
	}
	_, err := sshHelper.NewSshClient(userName, password, port)
	if err != nil {
		logger.Error("connect ssh error", zap.Any("error", err))
		return ctx.JSON(common_err.CLIENT_ERROR, modelCommon.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.CLIENT_ERROR), Data: "Please check if the username and port are correct, and make sure that ssh server is installed."})
	}
	return ctx.JSON(common_err.SUCCESS, modelCommon.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}
