package v1

import (
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"github.com/F-e-n-y-x/recasa/services/core/pkg/utils"
)

// localTermUser is the desktop user the built-in Terminal app logs in as.
// Since CasaOS itself runs as root, dropping privileges to this user needs
// no password - same as how a real desktop terminal just opens a shell in
// your own session.
const localTermUser = "ayush"

type localTermMsg struct {
	Type string `json:"type"`
	Cmd  string `json:"cmd"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func WsLocalTerm(ctx echo.Context) error {
	wsConn, err := upgrader.Upgrade(ctx.Response().Writer, ctx.Request(), nil)
	if err != nil {
		return err
	}
	defer wsConn.Close()

	cols, _ := strconv.Atoi(utils.DefaultQuery(ctx, "cols", "120"))
	rows, _ := strconv.Atoi(utils.DefaultQuery(ctx, "rows", "32"))

	u, err := user.Lookup(localTermUser)
	if err != nil {
		_ = wsConn.WriteMessage(websocket.TextMessage, []byte("local terminal user not found: "+err.Error()))
		return nil
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	shell := "/bin/bash"
	if _, statErr := os.Stat(shell); statErr != nil {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell, "-l")
	cmd.Dir = u.HomeDir
	cmd.Env = []string{
		"TERM=xterm-256color",
		"HOME=" + u.HomeDir,
		"USER=" + u.Username,
		"LOGNAME=" + u.Username,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
		Setsid:     true,
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		_ = wsConn.WriteMessage(websocket.TextMessage, []byte("failed to start terminal: "+err.Error()))
		return nil
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	quit := make(chan bool, 2)

	go func() {
		buf := make([]byte, 8192)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				if writeErr := wsConn.WriteMessage(websocket.TextMessage, buf[:n]); writeErr != nil {
					break
				}
			}
			if readErr != nil {
				break
			}
		}
		quit <- true
	}()

	go func() {
		for {
			_, data, readErr := wsConn.ReadMessage()
			if readErr != nil {
				break
			}
			msgObj := localTermMsg{}
			if jsonErr := json.Unmarshal(data, &msgObj); jsonErr != nil || msgObj.Type == "" {
				_, _ = ptmx.Write(data)
				continue
			}
			switch msgObj.Type {
			case "resize":
				if msgObj.Cols > 0 && msgObj.Rows > 0 {
					_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(msgObj.Rows), Cols: uint16(msgObj.Cols)})
				}
			case "cmd":
				_, _ = ptmx.Write([]byte(msgObj.Cmd))
			default:
				_, _ = ptmx.Write(data)
			}
		}
		quit <- true
	}()

	<-quit
	return nil
}
