package service

import (
	"testing"

	"github.com/compose-spec/compose-go/v2/types"

	"gotest.tools/v3/assert"
)

func appOf(t *testing.T, yaml string) *ComposeApp {
	t.Helper()
	app, err := NewComposeAppFromYAML([]byte(yaml), true, true)
	assert.NilError(t, err)
	return app
}

// H1: everything that hands a container the server is named, in words.
func TestHostAccessClassifier(t *testing.T) {
	app := appOf(t, `name: x
services:
  web:
    image: x
    pid: host
    ipc: host
    userns_mode: host
    cap_add: [ALL, cap_sys_admin, NET_ADMIN, SYS_PTRACE, SYS_MODULE, DAC_READ_SEARCH, SYS_NICE]
    devices: ["/dev/dri:/dev/dri"]
    security_opt: ["apparmor:unconfined", "seccomp=unconfined", "no-new-privileges:true"]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /run/user/1000/podman/podman.sock:/p.sock
      - /:/host:ro
      - /etc:/hostetc
      - /root/.ssh:/ssh
      - /var:/hostvar
      - /var/lib/nivaroos/x:/n
      - /proc:/host/proc:ro
      - /sys/:/host/sys:ro
      - /dev/bus/usb:/dev/bus/usb
      - /etc/localtime:/etc/localtime:ro
      - /DATA/AppData/x:/config
      - data:/data
volumes:
  data: {}
`)
	assert.DeepEqual(t, app.HostAccess(), []string{
		"web: AppArmor turned off (security_opt apparmor:unconfined)",
		"web: capability DAC_READ_SEARCH - can read any file on the server",
		"web: capability NET_ADMIN - can change the server's network and firewall",
		"web: capability SYS_ADMIN - near root on the server",
		"web: capability SYS_MODULE - can load kernel modules",
		"web: capability SYS_NICE",
		"web: capability SYS_PTRACE - can look inside other processes",
		"web: controls the server's containers (mounts /run/user/1000/podman/podman.sock)",
		"web: controls the server's containers (mounts /var/run/docker.sock)",
		"web: every Linux capability (cap_add: ALL) - root on the server",
		"web: mounts the server's /dev/bus/usb",
		"web: mounts the server's /etc",
		"web: mounts the server's /proc",
		"web: mounts the server's /root/.ssh",
		"web: mounts the server's /sys",
		"web: mounts the server's /var",
		"web: mounts the server's /var/lib/nivaroos/x",
		"web: mounts the server's whole disk (/)",
		"web: runs in the server's user namespace (userns_mode: host)",
		"web: seccomp turned off (security_opt seccomp=unconfined)",
		"web: sees and signals every process on the server (pid: host)",
		"web: shares the server's memory (ipc: host)",
		"web: uses the server's device /dev/dri",
	})
	assert.DeepEqual(t, appOf(t, "name: p\nservices:\n  web:\n    image: nginx\n    volumes: [\"/DATA/AppData/p:/data\", \"/etc/localtime:/etc/localtime:ro\"]\n").HostAccess(), []string(nil))
}

const storeV1 = "name: app\nservices:\n  web:\n    image: app:1\n    ports: [\"8080:80\"]\n    volumes: [\"/DATA/AppData/app:/data\"]\n"

// H1: a store update that keeps the installed access goes ahead; one that
// adds access waits for the admin, and their OK grants exactly that access.
func TestUpdateGateOldVsNewCompose(t *testing.T) {
	installed := appOf(t, storeV1)
	same := appOf(t, "name: app\nservices:\n  web:\n    image: app:2\n    volumes: [\"/DATA/AppData/app:/data\"]\n")
	assert.Equal(t, len(newStoreAccess(installed, same)), 0)

	wants := appOf(t, "name: app\nservices:\n  web:\n    image: app:2\n    cap_add: [NET_ADMIN]\n    volumes: [\"/DATA/AppData/app:/data\", \"/var/run/docker.sock:/var/run/docker.sock\"]\n")
	assert.DeepEqual(t, newStoreAccess(installed, wants), []string{
		"web: capability NET_ADMIN - can change the server's network and firewall",
		"web: controls the server's containers (mounts /var/run/docker.sock)",
	})

	// Already approved (the installed app has it): no new question.
	approved := appOf(t, "name: app\nservices:\n  web:\n    image: app:1\n    cap_add: [NET_ADMIN]\n    volumes: [\"/var/run/docker.sock:/var/run/docker.sock\"]\n")
	assert.Equal(t, len(newStoreAccess(approved, wants)), 0)

	granted := grantStoreAccess(installed.Services["web"], wants.Services["web"])
	assert.DeepEqual(t, granted.CapAdd, []string{"NET_ADMIN"})
	assert.Equal(t, len(granted.Volumes), 2)
	assert.Equal(t, granted.Volumes[1].Source, "/var/run/docker.sock")
	assert.Equal(t, len(granted.Ports), 1) // the installed settings stay
	assert.Equal(t, len(installed.Services["web"].Volumes), 1)
	after := installed.withServices(types.Services{"web": granted})
	assert.Equal(t, len(newStoreAccess(after, wants)), 0)
}

func TestHeldUpdateNotifiesOnce(t *testing.T) {
	var told []string
	defer func(f func(string, []string)) { notifyHeldUpdate = f }(notifyHeldUpdate)
	notifyHeldUpdate = func(app string, reasons []string) { told = append(told, app) }

	err := holdUpdate("gate-app", []string{"web: privileged"})
	assert.ErrorIs(t, err, ErrUpdateNeedsApproval)
	assert.ErrorContains(t, err, "gate-app update wants: web: privileged")
	holdUpdate("gate-app", []string{"web: privileged"})
	assert.DeepEqual(t, told, []string{"gate-app"})
	holdUpdate("gate-app", []string{"web: host network", "web: privileged"})
	assert.DeepEqual(t, told, []string{"gate-app", "gate-app"})
}
