// hostdesktop_install.go lets the dashboard's Host Desktop panel detect
// whether streaming is installed and install it on the fly, instead of the
// panel just failing to connect with no explanation when someone opens it
// on a machine where Host Desktop was never selected during setup.
//
// The script/unit content here is deliberately identical to what
// installer/install.sh's install_host_desktop() and
// cli/cmd/hostDesktopEnable.go write - kept in sync by hand, matching the
// pattern those two already established, so a machine ends up with exactly
// the same Host Desktop setup regardless of which of the three ever
// provisioned it.
package main

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const hostDesktopScriptPath = "/usr/local/bin/nivaroos-host-desktop.sh"

// deStatusScriptPath is where installer/install.sh copies
// installer/host-desktop-de-install.sh (same pattern as gpu-sidecar's own
// driverScriptPath for gpu-driver-install.sh).
const deStatusScriptPath = "/usr/local/bin/nivaroos-host-desktop-de-install.sh"

// deStatusTimeout only needs to cover a few sysfs/systemctl/pgrep checks -
// all fast and local - so this stays short, unlike an actual DE install
// (which the dashboard runs in a real terminal instead, not through an
// HTTP endpoint - see the panel's installDesktopEnvironment(), mirroring
// how the GPU widget's driver install works, for why: a package install
// can prompt interactively and take minutes, which a blocking HTTP
// request can't usefully show or answer).
const deStatusTimeout = 10 * time.Second

const hostDesktopScriptContent = `#!/bin/bash
set -e

# Find X authority file
find_auth() {
    for f in /var/run/lightdm/root/:0 /run/lightdm/root/:0 /root/.Xauthority /home/*/.Xauthority; do
        if [ -f "$f" ]; then
            echo "$f"
            return 0
        fi
    done
    echo ""
}

# Wait for X server on :0 if not yet ready
for i in {1..30}; do
    if [ -S /tmp/.X11-unix/X0 ] || xdpyinfo -display :0 >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

AUTH=$(find_auth)

# Set initial default framebuffer resolution to 1920x1080 if currently lower (e.g. 640x480 headless default)
if [ -n "$AUTH" ]; then
    DISPLAY=:0 XAUTHORITY="$AUTH" xrandr --fb 1920x1080 2>/dev/null || true
else
    xrandr -display :0 --fb 1920x1080 2>/dev/null || true
fi

# Start websockify proxy on port 28642 if not already running
if ! ss -tulpn | grep -q ":28642 "; then
    /usr/bin/websockify -D 28642 127.0.0.1:5900 2>/dev/null || true
fi

# -noxdamage/-fixscreen X=5/-localhost: see installer/install.sh's
# install_host_desktop() for why - kept identical here.
if [ -n "$AUTH" ]; then
    exec /usr/bin/x11vnc -display :0 -auth "$AUTH" -xrandr resize -forever -shared -repeat -noxdamage -fixscreen X=5 -localhost -rfbport 5900 -nopw
else
    exec /usr/bin/x11vnc -display :0 -auth guess -xrandr resize -forever -shared -repeat -noxdamage -fixscreen X=5 -localhost -rfbport 5900 -nopw
fi
`

const hostDesktopUnitPath = "/usr/lib/systemd/system/nivaroos-host-desktop.service"

const hostDesktopUnitContent = `[Unit]
Description=NivaroOS Host Desktop Remote VNC Server
After=network.target lightdm.service display-manager.service
Wants=lightdm.service

[Service]
Type=simple
ExecStart=/usr/local/bin/nivaroos-host-desktop.sh
Restart=always
RestartSec=3
KillMode=mixed

[Install]
WantedBy=multi-user.target
`

// IsHostDesktopInstalled treats the systemd unit's presence as the source
// of truth (not whether x11vnc happens to be on PATH) - that's exactly
// what install_host_desktop()/hostDesktopEnable write last, after
// confirming/attempting the package install, so its existence means a
// provisioning attempt actually completed rather than just having the
// binaries present for an unrelated reason.
func IsHostDesktopInstalled() bool {
	_, err := os.Stat(hostDesktopUnitPath)
	return err == nil
}

// InstallHostDesktop mirrors cli/cmd/hostDesktopEnable.go exactly - see
// that file's comments for why each step is best-effort. vm-sidecar
// already runs as root (it bind-mounts host directories and drives
// libvirt), so unlike the CLI command this never needs its own
// sudo-elevation check.
func InstallHostDesktop() error {
	_ = exec.Command("apt-get", "update").Run()
	_ = exec.Command("apt-get", "install", "-y", "x11vnc", "websockify").Run()

	if err := os.WriteFile(hostDesktopScriptPath, []byte(hostDesktopScriptContent), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(hostDesktopUnitPath, []byte(hostDesktopUnitContent), 0o644); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return err
	}
	if _, err := exec.LookPath("x11vnc"); err == nil {
		_ = exec.Command("systemctl", "enable", "--now", "nivaroos-host-desktop.service").Run()
	}
	return nil
}

// RegisterHostDesktopInstallRoutes is called alongside RegisterHostRoutes -
// kept in its own file/function since it's a distinct concern (provisioning
// the feature) from hostdevices.go's existing display/capability routes.
func RegisterHostDesktopInstallRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /host/desktop/installed", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"installed": IsHostDesktopInstalled()})
	})

	mux.HandleFunc("POST /host/desktop/install", func(w http.ResponseWriter, r *http.Request) {
		if err := InstallHostDesktop(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"installed": IsHostDesktopInstalled()})
	})

	// Whether streaming is installed (above) and whether there's a
	// compatible (X11) desktop for it to actually stream are separate
	// questions - see installer/host-desktop-de-install.sh's own header
	// comment for the full reasoning. This just passes its --status
	// output straight through, exactly like gpu-sidecar's /driver-status
	// does for gpu-driver-install.sh.
	mux.HandleFunc("GET /host/desktop/de-status", func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(deStatusScriptPath); err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), deStatusTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "bash", deStatusScriptPath, "--status").Output()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
	})
}
