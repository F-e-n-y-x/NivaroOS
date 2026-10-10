# nivaroos-vm-sidecar

Backs the VM Manager windowed app: create/run/manage QEMU/KVM VMs via
libvirt (`qemu:///system`), on port `:28641`. Installed as a systemd
service (`nivaroos-vm-sidecar.service`), independent of the NivaroOS services
proper - same shape as `nivaroos-gpu-sidecar`.

Connects to libvirt lazily (and reconnects automatically if the
connection drops), so the sidecar can start and serve `/setup/status`
before `qemu-system-x86`/`libvirt-daemon-system` are even installed.

## Endpoints

- `GET /setup/status`, `POST /setup/install` - detects/installs the
  virtualization stack (`qemu-system-x86`, `qemu-utils`,
  `libvirt-daemon-system`, `libvirt-clients`, `ovmf`), enables `libvirtd`,
  and ensures the default storage pool (`/DATA/VMs`) and NAT network
  exist. Never runs on its own - only in response to `/setup/install`.
- `GET /vms`, `GET /vms/{name}` - list/inspect VMs.
- `POST /vms` - create + immediately start a VM (provisions a qcow2 disk
  via `qemu-img`, defines the domain, starts it).
- `POST /vms/{name}/{start,shutdown,reboot,force-off}`,
  `DELETE /vms/{name}[?wipe_disk=true]` - lifecycle.
- `GET /vms/{name}/console` (WebSocket) - proxies to the VM's VNC server
  for the noVNC client embedded in the UI.
- `GET /isos`, `POST /isos` - list/upload ISOs under `/DATA/VMs/isos`.
- `GET /networks`, `GET /networks/interfaces`, `POST /networks/bridge` -
  the always-present default NAT network, available physical host NICs,
  and creating a bridged network. Bridging refuses the interface
  currently carrying the host's default route, so it can never drop the
  connection the request itself arrived over. This box uses classic
  Debian ifupdown (`/etc/network/interfaces.d/`), not netplan.

## Host Desktop

`GET /host/console` (WebSocket) streams this machine's own desktop from a
root-only RFB socket, `/run/nivaroos/hostvnc.sock`, served by the wrapper
`hostdesktop/nivaroos-host-desktop.sh` (embedded; installed by
`install-host-desktop`):

- X11 session: x11vnc, as always.
- Wayland session: `nivaroos-vm-sidecar host-vnc-wayland`
  (`hostvnc_wayland.go`) picks a backend by the compositor running as the
  session user:
  - GNOME (mutter): mutter's own RemoteDesktop/ScreenCast D-Bus API, frames
    via PipeWire (`gst-launch-1.0 pipewiresrc`), keysym input. No prompt;
    GNOME refuses to share a locked screen, so the panel offers
    `POST /host/desktop/unlock` (`loginctl unlock-session`; re-locked a
    minute after the last viewer leaves).
  - wlroots-style (sway, Hyprland, labwc, river, wayfire): `wayvnc
    --unix-socket`.
  - anything else (KDE Plasma 6, ...): xdg-desktop-portal RemoteDesktop. The
    host user clicks "Allow" once; the restore token is kept in
    `~/.local/state/nivaroos/host-desktop-restore-token`.
  `GET /host/desktop/status` reports `backend`, `needs_consent`,
  `consent_denied` and `locked` so the clients can say what's needed. The
  Wayland login screen itself can't be shared. Not on Wayland (yet): host
  clipboard sync, resolution changes, keysyms the host keymap lacks.
  Wayland packages (GStreamer PipeWire plugin, wayvnc) are installed where a
  Wayland desktop exists, and on sidecar start for older installs once the
  active session is Wayland.

## Testing

Handlers are unit-tested against libvirt's `test:///default` fake driver
(a full in-memory hypervisor), so `go test ./...` needs no real KVM.
Install/apt logic and the live bridge `ifup`/`ifdown` step are
intentionally left to manual verification - see the design spec.
