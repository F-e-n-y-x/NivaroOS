package v1

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strconv"
	"strings"

	modelCommon "github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/labstack/echo/v4"
)

// isProtectedSystemUser: the owner's own login account (the one who ran
// sudo, or the first regular login user) can't be deleted, demoted or have
// its password changed from the web UI.
func isProtectedSystemUser(username string) bool {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && username == sudoUser {
		return true
	}
	u, err := getDefaultDesktopUser()
	if err == nil && u.Username == username {
		return true
	}
	return false
}

var validUsername = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

const minSystemPasswordLen = 8

// checkSystemPassword: long enough for a host login, and no control
// characters - chpasswd reads "user:password" lines, so a newline in the
// password would start a line for another account (e.g. root).
func checkSystemPassword(pw string) error {
	if len([]rune(pw)) < minSystemPasswordLen {
		return fmt.Errorf("password must be at least %d characters", minSystemPasswordLen)
	}
	for _, r := range pw {
		if r < 0x20 || r == 0x7f {
			return errors.New("password can't contain line breaks or control characters")
		}
	}
	return nil
}

// manageableAccount reports why username can't be changed from the web UI:
// only existing regular accounts (UID 1000-59999) that aren't protected.
func manageableAccount(username string, uidOf func(string) (int, error), protected func(string) bool) error {
	if !validUsername.MatchString(username) {
		return errors.New("invalid username")
	}
	uid, err := uidOf(username)
	if err != nil {
		return errors.New("no such user")
	}
	if uid < 1000 || uid >= 60000 {
		return errors.New("system and service accounts can't be changed here")
	}
	if protected(username) {
		return errors.New("this account is protected - change it on the server itself")
	}
	return nil
}

func systemUID(username string) (int, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(u.Uid)
}

type systemUserInfo struct {
	Username  string `json:"username"`
	Uid       string `json:"uid"`
	FullName  string `json:"full_name"`
	Home      string `json:"home"`
	Shell     string `json:"shell"`
	Sudo      bool   `json:"sudo"`
	Docker    bool   `json:"docker"`
	Protected bool   `json:"protected"`
}

func badParams(ctx echo.Context, msg string) error {
	return ctx.JSON(common_err.CLIENT_ERROR, modelCommon.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS), Data: msg})
}

func serviceError(ctx echo.Context, err error) error {
	return ctx.JSON(common_err.SERVICE_ERROR, modelCommon.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
}

func ok(ctx echo.Context, data interface{}) error {
	return ctx.JSON(common_err.SUCCESS, modelCommon.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

func groupMembers(groupName string) map[string]bool {
	members := map[string]bool{}
	out, err := exec.Command("getent", "group", groupName).Output()
	if err != nil {
		return members
	}
	// format: group:x:gid:member1,member2
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(parts) == 4 {
		for _, m := range strings.Split(parts[3], ",") {
			if m != "" {
				members[m] = true
			}
		}
	}
	return members
}

func GetSystemUsers(ctx echo.Context) error {
	f, err := exec.Command("getent", "passwd").Output()
	if err != nil {
		return serviceError(ctx, err)
	}

	sudoMembers := groupMembers("sudo")
	dockerMembers := groupMembers("docker")

	var users []systemUserInfo
	scanner := bufio.NewScanner(strings.NewReader(string(f)))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) < 7 {
			continue
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil || uid < 1000 || uid >= 60000 {
			continue
		}
		username := fields[0]
		users = append(users, systemUserInfo{
			Username:  username,
			Uid:       fields[2],
			FullName:  strings.Split(fields[4], ",")[0],
			Home:      fields[5],
			Shell:     fields[6],
			Sudo:      sudoMembers[username],
			Docker:    dockerMembers[username],
			Protected: isProtectedSystemUser(username),
		})
	}
	return ok(ctx, users)
}

type createSystemUserReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Sudo     bool   `json:"sudo"`
}

func PostSystemUser(ctx echo.Context) error {
	req := new(createSystemUserReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid body")
	}
	if !validUsername.MatchString(req.Username) {
		return badParams(ctx, "invalid username")
	}
	if err := checkSystemPassword(req.Password); err != nil {
		return badParams(ctx, err.Error())
	}
	if _, err := user.Lookup(req.Username); err == nil {
		return badParams(ctx, "user already exists")
	}

	if err := exec.Command("useradd", "-m", "-s", "/bin/bash", req.Username).Run(); err != nil {
		return serviceError(ctx, err)
	}
	if err := setSystemPassword(req.Username, req.Password); err != nil {
		// Don't leave an account behind without the password the user chose.
		_ = exec.Command("userdel", "-r", req.Username).Run()
		return serviceError(ctx, err)
	}
	if req.Sudo {
		if err := exec.Command("usermod", "-aG", "sudo", req.Username).Run(); err != nil {
			return serviceError(ctx, err)
		}
	}
	return ok(ctx, "created")
}

func DeleteSystemUser(ctx echo.Context) error {
	username := ctx.Param("username")
	if err := manageableAccount(username, systemUID, isProtectedSystemUser); err != nil {
		return badParams(ctx, err.Error())
	}
	if err := exec.Command("userdel", "-r", username).Run(); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, "deleted")
}

type setPasswordReq struct {
	Password string `json:"password"`
}

func setSystemPassword(username, password string) error {
	if err := checkSystemPassword(password); err != nil {
		return err
	}
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s\n", username, password))
	return cmd.Run()
}

func PutSystemUserPassword(ctx echo.Context) error {
	username := ctx.Param("username")
	if err := manageableAccount(username, systemUID, isProtectedSystemUser); err != nil {
		return badParams(ctx, err.Error())
	}
	req := new(setPasswordReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid body")
	}
	if err := checkSystemPassword(req.Password); err != nil {
		return badParams(ctx, err.Error())
	}
	if err := setSystemPassword(username, req.Password); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, "updated")
}

type setGroupsReq struct {
	Sudo   *bool `json:"sudo"`
	Docker *bool `json:"docker"`
}

func PutSystemUserGroups(ctx echo.Context) error {
	username := ctx.Param("username")
	if err := manageableAccount(username, systemUID, isProtectedSystemUser); err != nil {
		return badParams(ctx, err.Error())
	}
	req := new(setGroupsReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid body")
	}
	if err := applyGroupChange(username, "sudo", req.Sudo); err != nil {
		return serviceError(ctx, err)
	}
	if err := applyGroupChange(username, "docker", req.Docker); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, "updated")
}

func applyGroupChange(username, group string, want *bool) error {
	if want == nil {
		return nil
	}
	if *want {
		return exec.Command("usermod", "-aG", group, username).Run()
	}
	return exec.Command("gpasswd", "-d", username, group).Run()
}
