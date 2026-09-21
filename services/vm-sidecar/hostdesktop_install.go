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
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
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

x_is_up() {
    [ -S /tmp/.X11-unix/X0 ] || xdpyinfo -display :0 >/dev/null 2>&1
}

# No physical monitor connected on any output? Every mainstream KMS driver
# (modesetting/amdgpu/intel/nouveau/nvidia) refuses to bring up a display at
# all in that case unless told otherwise - this, not x11vnc or websockify,
# is what actually breaks Host Desktop on a headless/rack server: lightdm's
# own Xorg never starts, so :0 never appears no matter how long the wait
# loop below runs.
no_monitor_connected() {
    local f
    for f in /sys/class/drm/*/status; do
        [ -f "$f" ] || continue
        if [ "$(cat "$f" 2>/dev/null)" = "connected" ]; then
            return 1
        fi
    done
    return 0
}

XORG_HEADLESS_CONF=/etc/X11/xorg.conf.d/10-nivaroos-headless.conf

# AllowEmptyInitialConfiguration is a no-op when a monitor genuinely is
# connected, and only takes effect for whichever driver actually binds the
# card - so writing one Device section per common driver name is safe
# rather than needing to guess which one this machine uses.
write_headless_xorg_conf() {
    [ -f "$XORG_HEADLESS_CONF" ] && return 0
    mkdir -p /etc/X11/xorg.conf.d
    : > "$XORG_HEADLESS_CONF"
    for drv in modesetting amdgpu intel nouveau nvidia; do
        cat >> "$XORG_HEADLESS_CONF" <<CONFEOF
Section "Device"
    Identifier "NivaroOSHeadless-${drv}"
    Driver "${drv}"
    Option "AllowEmptyInitialConfiguration" "true"
EndSection

CONFEOF
    done
}

# Wait for X server on :0 if not yet ready. If it still isn't up halfway
# through and no monitor is plugged in, apply the headless fix and restart
# the display manager once - only when X genuinely isn't up yet, so a
# working headed session is never disrupted just because this check ran.
HEADLESS_FIX_APPLIED=false
for i in {1..30}; do
    if x_is_up; then
        break
    fi
    if [ "$i" -eq 10 ] && [ "$HEADLESS_FIX_APPLIED" = false ] && no_monitor_connected; then
        write_headless_xorg_conf
        systemctl restart display-manager.service 2>/dev/null || systemctl restart lightdm.service 2>/dev/null || systemctl restart sddm.service 2>/dev/null || true
        HEADLESS_FIX_APPLIED=true
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
#
# -repeat (not -norepeat): -norepeat disables the X server's own key
# autorepeat while a client is connected, on the assumption the VNC
# viewer re-sends its own down events for a genuinely held key - but that
# broke holding a key (Backspace, arrow keys, etc) entirely, which is a
# worse trade than the rarer runaway-duplicate-character bug -repeat can
# cause under real network delay between a key's down/up events. If that
# resurfaces, -skip_dups is the next thing to try instead of -norepeat.
#
# -capslock: without it, x11vnc's default modtweak logic fakes a Shift
# press to force an uppercase keysym whenever one arrives - even if the
# host's CapsLock is already on, where Shift+CapsLock actually produces
# LOWERCASE, inverting the typed case. -capslock makes x11vnc check the
# host's real CapsLock state first and skip the fake Shift when it's
# already set, which is what was showing up as the host desktop typing as
# if CapsLock were on regardless of the client's real key state.
if [ -n "$AUTH" ]; then
    exec /usr/bin/x11vnc -display :0 -auth "$AUTH" -xrandr resize -forever -shared -repeat -capslock -noxdamage -fixscreen X=5 -localhost -rfbport 5900 -nopw
else
    exec /usr/bin/x11vnc -display :0 -auth guess -xrandr resize -forever -shared -repeat -capslock -noxdamage -fixscreen X=5 -localhost -rfbport 5900 -nopw
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
	_ = exec.Command("apt-get", "install", "-y", "x11vnc", "websockify", "xdotool").Run()

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

	// The statusbar CapsLock indicator needs the HOST's real lock state, not
	// whatever the browser/client device's own keyboard is doing - those are
	// two different keyboards with two different CapsLock states, and a
	// client-side-only check (e.g. KeyboardEvent.getModifierState in the
	// panel's own JS) was answering the wrong one. `xset q`'s XKB indicators
	// section reports the X server's actual state directly, same
	// getHostXAuth()/xrandrEnv() auth resolution GetHostDisplay() already
	// uses for xrandr.
	mux.HandleFunc("GET /host/desktop/capslock", func(w http.ResponseWriter, r *http.Request) {
		on, err := GetHostCapsLock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"caps_lock": on})
	})
}

var capsLockLineRe = regexp.MustCompile(`(?i)Caps Lock:\s*(on|off)`)

// GetHostCapsLock reports whether the host X server's CapsLock lock is
// currently engaged, straight from `xset q`'s "XKB indicators" section -
// this is the actual host state, independent of (and frequently different
// from) whatever the accessing browser/device's own keyboard reports.
func GetHostCapsLock() (bool, error) {
	cmd := exec.Command("xset", "q")
	cmd.Env = xrandrEnv(getHostXAuth())
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	m := capsLockLineRe.FindSubmatch(out)
	if m == nil {
		return false, nil
	}
	return string(m[1]) == "on", nil
}

// releaseHostCapsLock sends a real CapsLock press+release to the host X
// server via xdotool - the same effect as someone physically tapping the
// key - independent of whether anyone's viewing the Host Desktop stream
// right now, since this watcher runs regardless of client connections.
func releaseHostCapsLock() error {
	cmd := exec.Command("xdotool", "key", "--clearmodifiers", "Caps_Lock")
	cmd.Env = xrandrEnv(getHostXAuth())
	return cmd.Run()
}

const (
	// hostCapsLockPollInterval trades detection latency for how often this
	// shells out to `xset q` - 15s is frequent enough that the auto-release
	// below fires close to on time without polling so often it shows up in
	// process accounting.
	hostCapsLockPollInterval = 15 * time.Second
	// hostCapsLockAutoOffAfter: how long CapsLock has to stay continuously
	// on, host-side, before this releases it on its own. Matches the 5min
	// default x11vnc itself uses for X11VNC_IDLE_TIMEOUT elsewhere in this
	// same feature, for the same reason - long enough that a deliberate,
	// actively-used CapsLock (someone typing a long uppercase string) is
	// never touched, short enough that a host left stuck in CapsLock after
	// everyone's disconnected doesn't stay that way indefinitely with
	// nobody around to notice.
	hostCapsLockAutoOffAfter = 5 * time.Minute
)

// StartHostCapsLockWatcher runs for the lifetime of the process. It does
// not require a Host Desktop viewer to be connected - the host's own
// physical keyboard, or a session left mid-typing, can leave CapsLock
// stuck on just as easily as a VNC client can.
func StartHostCapsLockWatcher() {
	go func() {
		var onSince time.Time
		for {
			time.Sleep(hostCapsLockPollInterval)

			if !IsHostDesktopInstalled() {
				onSince = time.Time{}
				continue
			}

			on, err := GetHostCapsLock()
			if err != nil || !on {
				// Treat "can't tell" the same as "off" rather than letting a
				// transient X/auth hiccup masquerade as CapsLock having been
				// on continuously the whole time it couldn't be checked.
				onSince = time.Time{}
				continue
			}

			if onSince.IsZero() {
				onSince = time.Now()
				continue
			}

			if time.Since(onSince) >= hostCapsLockAutoOffAfter {
				if err := releaseHostCapsLock(); err != nil {
					log.Printf("host desktop: CapsLock was on for %s but auto-release failed: %v", hostCapsLockAutoOffAfter, err)
				} else {
					log.Printf("host desktop: CapsLock was on for %s - auto-released", hostCapsLockAutoOffAfter)
				}
				onSince = time.Time{}
			}
		}
	}()
}
