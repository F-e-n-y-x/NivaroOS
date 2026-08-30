# GPU Widget — Design & Status

## Status: DONE (2026-08-19)

## What was built
- `extras/casaos-gpu-sidecar/`: a small Go HTTP service (`casaos-gpu-sidecar`,
  systemd-managed, port 28640) that shells out to `nvidia-smi` per request
  and returns utilization %, VRAM used/total, temperature, power draw/limit,
  and per-process GPU usage (via `nvidia-smi pmon`, covering **all** host
  processes, not just CasaOS-managed containers).
- `CasaOS-UI/src/widgets/Gpu.vue`: new dashboard widget, following the exact
  same convention as the existing `Cpu.vue`/`Disks.vue`/`Network.vue`
  widgets (auto-discovered via `require.context('@/widgets', ...)` in
  `SideBar.vue` and `Settings.vue` — dropping the file in was enough to
  register it, no other source changes needed). Two radial bars
  (utilization, VRAM), click-to-expand shows the per-process table. Polls
  the sidecar every 2s via `fetch`.

## Correction to earlier assumption
CasaOS widgets are a fixed vertical sidebar list (show/hide toggle per
widget), **not** a draggable/resizable grid as assumed when this was first
scoped. The design (radial-bar tile matching CPU/RAM) still applies; there
was just nothing to drag/resize to begin with.

## Verified
- Sidecar builds, runs as `casaos-gpu-sidecar.service`, reachable on both
  localhost and the LAN IP (`192.168.10.10:28640`), CORS header present.
- UI builds with the new widget bundled into the Home view chunk, swapped
  into the live install.

## Not yet verified
Visual confirmation in an actual browser — needs the user to look at the
dashboard and confirm the GPU tile renders and the click-to-expand process
list works.
