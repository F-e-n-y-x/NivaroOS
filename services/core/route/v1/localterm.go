package v1

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils"
)

func getDefaultDesktopUser() (*user.User, error) {
	// Try primary desktop user candidates
	candidates := []string{"ayush"}
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && sudoUser != "root" {
		candidates = append([]string{sudoUser}, candidates...)
	}
	for _, username := range candidates {
		if u, err := user.Lookup(username); err == nil {
			return u, nil
		}
	}

	// Search for the first regular login user (UID 1000..59999)
	out, err := exec.Command("getent", "passwd").Output()
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			fields := strings.Split(scanner.Text(), ":")
			if len(fields) >= 7 {
				uid, err := strconv.Atoi(fields[2])
				if err == nil && uid >= 1000 && uid < 60000 {
					shell := fields[6]
					if !strings.HasSuffix(shell, "nologin") && !strings.HasSuffix(shell, "false") {
						if u, err := user.Lookup(fields[0]); err == nil {
							return u, nil
						}
					}
				}
			}
		}
	}

	return user.Current()
}

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

	u, err := getDefaultDesktopUser()
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
