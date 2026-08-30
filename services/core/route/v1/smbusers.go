package v1

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"

	"github.com/labstack/echo/v4"
)

type smbUserInfo struct {
	Username string `json:"username"`
}

func GetSmbUsers(ctx echo.Context) error {
	out, err := exec.Command("pdbedit", "-L").Output()
	if err != nil {
		// No SMB users yet (pdbedit exits non-zero on an empty database) -
		// treat as an empty list rather than an error.
		return ok(ctx, []smbUserInfo{})
	}
	var users []smbUserInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) > 0 && fields[0] != "" {
			users = append(users, smbUserInfo{Username: fields[0]})
		}
	}
	return ok(ctx, users)
}

func setSmbPassword(username, password string) error {
	if _, err := user.Lookup(username); err != nil {
		return fmt.Errorf("%s must exist as a system user before it can be added as an SMB user", username)
	}
	cmd := exec.Command("smbpasswd", "-a", "-s", username)
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s\n%s\n", password, password))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err.Error(), string(out))
	}
	return nil
}

type smbUserReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func PostSmbUser(ctx echo.Context) error {
	req := new(smbUserReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid body")
	}
	if !validUsername.MatchString(req.Username) {
		return badParams(ctx, "invalid username")
	}
	if len(req.Password) < 4 {
		return badParams(ctx, "password too short")
	}
	if err := setSmbPassword(req.Username, req.Password); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, "created")
}

func PutSmbUserPassword(ctx echo.Context) error {
	username := ctx.Param("username")
	if !validUsername.MatchString(username) {
		return badParams(ctx, "invalid username")
	}
	req := new(setPasswordReq)
	if err := ctx.Bind(req); err != nil || len(req.Password) < 4 {
		return badParams(ctx, "password too short")
	}
	if err := setSmbPassword(username, req.Password); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, "updated")
}

func DeleteSmbUser(ctx echo.Context) error {
	username := ctx.Param("username")
	if !validUsername.MatchString(username) {
		return badParams(ctx, "invalid username")
	}
	if err := exec.Command("smbpasswd", "-x", username).Run(); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, "deleted")
}
