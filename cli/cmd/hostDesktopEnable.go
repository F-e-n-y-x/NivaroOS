/*
Copyright © 2022 NivaroOS

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// hostDesktopScriptContent and hostDesktopUnitContent are the exact same
// content installer/install.sh's install_host_desktop() writes, kept in
// sync by hand - this command exists to enable Host Desktop streaming
// after the fact, for someone who skipped it during the initial install,
// so it needs to produce exactly what a fresh install would have.
const hostDesktopScriptPath = "/usr/local/bin/nivaroos-host-desktop.sh"

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

var hostDesktopEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Install and enable Host Desktop streaming (x11vnc & websockify)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureRoot(); err != nil {
			return err
		}

		// Best-effort, same as installer/install.sh: x11vnc/websockify
		// aren't in every distro's repos, so a missing package here
		// shouldn't fail the whole command - just leave the feature
		// unavailable until installed manually, matching the installer's
		// own behavior exactly.
		update := exec.Command("apt-get", "update")
		update.Stdout = os.Stdout
		update.Stderr = os.Stderr
		_ = update.Run()

		deps := exec.Command("apt-get", "install", "-y", "x11vnc", "websockify")
		deps.Stdout = os.Stdout
		deps.Stderr = os.Stderr
		_ = deps.Run()

		if _, err := exec.LookPath("x11vnc"); err != nil {
			fmt.Fprintln(os.Stderr, "x11vnc could not be installed automatically on this distro - Host Desktop streaming will be unavailable until it is installed manually.")
		}

		if err := os.WriteFile(hostDesktopScriptPath, []byte(hostDesktopScriptContent), 0o755); err != nil {
			return fmt.Errorf("writing %s: %w", hostDesktopScriptPath, err)
		}

		if err := os.WriteFile(hostDesktopUnitPath, []byte(hostDesktopUnitContent), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", hostDesktopUnitPath, err)
		}

		reload := exec.Command("systemctl", "daemon-reload")
		reload.Stdout = os.Stdout
		reload.Stderr = os.Stderr
		if err := reload.Run(); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w", err)
		}

		if _, err := exec.LookPath("x11vnc"); err == nil {
			enable := exec.Command("systemctl", "enable", "--now", "nivaroos-host-desktop.service")
			enable.Stdout = os.Stdout
			enable.Stderr = os.Stderr
			if err := enable.Run(); err != nil {
				return fmt.Errorf("systemctl enable --now nivaroos-host-desktop.service: %w", err)
			}
		}

		fmt.Println("Host Desktop streaming is enabled. Open the Host Desktop app in the web UI to view the stream.")
		return nil
	},
}

func init() {
	hostDesktopCmd.AddCommand(hostDesktopEnableCmd)
}
