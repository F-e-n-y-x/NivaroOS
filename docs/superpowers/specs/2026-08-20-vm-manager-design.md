# VM Manager — Design

## Status: DESIGN APPROVED (2026-08-20)

## Context

The user wants to create and run VMs from the CasaOS desktop, with a
native-feeling windowed app (same pattern as Files/Terminal/Settings),
backed by a new small Go sidecar service (same pattern as
`casaos-gpu-sidecar`). No virtualization stack (QEMU/KVM/libvirt) is
currently installed on this PC. Host CPU is AMD with `svm` present and
`kvm_amd` already loaded; host GPU is an NVIDIA GTX 1080 Ti currently
used by the host itself.

Confirmed with the user:
- Networking: support both NAT and bridged, default to NAT, pick
  per-VM at creation time (not decided upfront).
- Storage: VM disks default to `/DATA/VMs`, overridable per VM.
- GPU passthrough: explicitly out of scope for this build — queued to
  the backlog, same treatment as other deferred items.
- App icon already provided by the user at
  `/DATA/casaos_icons/vm_manager.png`.

## Approaches considered

**A. Wrap `virsh` CLI (shell-out)** — same shape as the GPU sidecar
shelling to `nvidia-smi`. Simplest to build, but VM define/list/stats
from `virsh` is fragile text to parse, worse than GPU stats since VM
management needs structured XML editing, not just read-only queries.

**B. Embed an existing web VM manager (e.g. Cockpit Machines) in an
iframe.** Almost no code, but it's a separate service/port with its
own login and visual language, and can't be made to feel native
(no matching console tab inside the existing Dock/Window chrome).

**C. Chosen approach.** A dedicated `casaos-vm-sidecar` Go service
using real libvirt bindings (`libvirt.org/go/libvirt`, talking to
`qemu:///system`) — same "one small standalone systemd-managed Go
service" shape as `casaos-gpu-sidecar` — plus a new windowed app in
CasaOS-UI. Libvirt bindings give structured, reliable data instead of
text-scraping, and libvirt's `test:///default` driver is a full
in-memory fake hypervisor, which makes the sidecar's handlers unit
-testable without needing real KVM in CI.

## Architecture

```
CasaOS-UI (VmManagerApp.vue, windowed app)
      |  REST + WebSocket (localhost:28641)
      v
casaos-vm-sidecar (Go, systemd, port 28641)
      |  libvirt-go bindings, qemu:///system
      v
libvirtd  -->  QEMU/KVM VMs
```

### 1. `casaos-vm-sidecar`

New directory: `extras/casaos-vm-sidecar/`, new systemd unit
`casaos-vm-sidecar.service`, port `28641` (next free port after the
GPU sidecar's `28640`).

**Setup / install** (explicit, user-triggered — never silent, since it
touches system packages and services):
- `GET /setup/status` — reports whether `qemu-kvm`,
  `libvirt-daemon-system`, `libvirt-clients`, `ovmf` are installed and
  whether `libvirtd` is active.
- `POST /setup/install` — apt-installs whatever's missing, enables
  and starts `libvirtd`, creates the default storage pool at
  `/DATA/VMs` (if absent) and ensures libvirt's default NAT network
  (`virbr0`, `default`) is defined and active.

**VMs**:
- `GET /vms` — list: name, state, vcpus, ram, disk path(s)/size,
  network(s). Polled by the UI (same 2s-poll convention as the GPU
  widget).
- `GET /vms/{name}` — detail.
- `POST /vms` — create: `{name, vcpus, ram_mib, disk: {path, size_gib},
  iso_path, network: {mode: "nat"|"bridge", bridge_name?}}`. Backend
  generates the libvirt domain XML from a template and calls
  `virDomainDefineXML` + `virDomainCreate`.
- `POST /vms/{name}/start`, `/shutdown` (graceful ACPI), `/force-off`,
  `/reboot`, `DELETE /vms/{name}` (with `?wipe_disk=true` to also
  remove the backing disk file).

**Console**:
- `GET /vms/{name}/console` (WebSocket) — sidecar reads the VNC port
  from the domain's live XML (`virDomainGetXMLDesc`) and proxies
  browser↔VNC bytes directly using `gorilla/websocket` (binary frames
  raw passthrough, no separate `websockify` process). UI embeds
  noVNC's client canvas against this endpoint.

**Storage / ISOs**:
- `GET /isos` — lists files under `/DATA/VMs/isos`.
- `POST /isos` — upload (multipart).

**Networks**:
- `GET /networks` — lists libvirt networks (always includes default
  NAT) plus any bridges.
- `POST /networks/bridge` — `{name, host_nic}`: writes a
  netplan/systemd-networkd bridge definition binding the chosen host
  NIC, applies it, defines a matching libvirt bridged network. This
  endpoint is only ever called from an explicit "Create bridged
  network" action behind a confirmation dialog in the UI — misbinding
  the wrong NIC can drop LAN connectivity to the host, so this must
  never run as part of automatic setup.

### 2. CasaOS-UI "VMs" app

New `CasaOS-UI/src/components/desktop/VmManagerApp.vue`, registered in
`DesktopWindow.vue`'s `COMPONENT_REGISTRY` and added to `Dock.vue`'s
`PINNED` array (icon: `/DATA/casaos_icons/vm_manager.png`, copied into
`CasaOS-UI/src/assets/img/app/vm-manager.png` at build time, same as
the existing `terminal.png`/`settings.png` pinned icons), opened via
the same `OPEN_WINDOW` store mutation pattern as Files/Terminal/
Settings.

Sub-views (tabs within the one window, same convention as Settings'
sectioned layout):
- **VMs** (default): grid/list of VMs with live status dot,
  CPU/RAM at a glance; per-VM action menu (start/shutdown/force-off/
  reboot/delete); "Create VM" button opens a wizard (name, vCPU/RAM,
  disk size + path — pre-filled `/DATA/VMs/<name>.qcow2`, editable;
  ISO picker; network picker defaulting to NAT, listing any existing
  bridged networks).
- **Console**: opened per-VM, embeds noVNC pointed at the sidecar's
  websocket endpoint for that VM.
- **Networks**: list networks; "Create bridged network" flow (NIC
  picker + explicit warning + confirm).
- **Storage**: ISO browser/upload.

**First-run state**: if `/setup/status` reports anything missing, the
VMs tab shows a "Set up virtualization" screen instead of the VM list,
with an explicit Install button (calls `/setup/install`) and shows
exactly which step is running/failed.

## Data flow

- VM list/stats: UI polls `GET /vms` every 2s while the window is open
  (paused while minimized, matching the existing CPU/RAM/GPU widget
  poll-when-visible convention).
- Console: persistent WebSocket only while the Console tab is active
  for that VM; closed on tab switch/window close.

## Error handling

- Install failures: `/setup/install` reports the exact step that
  failed (e.g. `"apt-get install libvirt-daemon-system: exit 100"`),
  not a generic error — UI shows this verbatim.
- `libvirtd` unreachable at request time: sidecar returns 503 with a
  clear message; UI shows a banner with a retry button rather than
  failing silently.
- VM operation failures (name collision, insufficient disk space,
  invalid XML): libvirt's own error string is passed through to the
  UI as-is.
- Bridge creation failure or wrong-NIC selection: confirmation dialog
  before the call is the primary mitigation; the endpoint also
  validates the chosen NIC exists and is not the one carrying the
  host's default route before applying, refusing with a clear error
  if it is.

## Testing

- Sidecar handlers get unit tests against libvirt's `test:///default`
  fake driver — covers create/start/shutdown/force-off/delete/list
  without needing real KVM, runnable in this environment.
- Manual smoke test on this PC (real KVM): run setup, create a small
  Linux VM, boot it, confirm console renders and accepts input, delete
  it and confirm disk cleanup.

## Out of scope for this build (backlog for later)

- GPU passthrough (confirmed explicitly deferred by the user).
- Snapshots, cloning, live migration.
- Cloud-init / automated OS provisioning — manual ISO installs only.
- SPICE — VNC only.
