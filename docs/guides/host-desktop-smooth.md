# Host Desktop: WebRTC video

Host Desktop streams your server's desktop as H.264 video over **WebRTC**. Video, scrolling and window dragging look much smoother than over VNC, and it uses less bandwidth. It's the default on the web dashboard and in the Android app. **Compatible** (VNC) is still there, both as an explicit choice and as the automatic fallback whenever WebRTC can't connect.

## Stream mode

In **Host Desktop → Display & Bandwidth Quality** (in the app: **More → Stream mode**), set **Stream mode**:

| Mode | What it does |
|---|---|
| **Auto** (default) | Tries WebRTC first. It switches to Compatible for that session, and shows a **Compatible mode** chip with the reason, when: the page was opened through the internet (the Cloudflare tunnel or a public domain), the server has no encoder, or no picture arrives (no connection within 8 seconds, for example because UDP is blocked, or no picture within 4 seconds of connecting) |
| **WebRTC only** | WebRTC, never VNC. It keeps retrying instead of falling back |
| **Compatible (VNC)** | VNC, the way Host Desktop always worked |

If you never changed the stream mode, you get Auto. If you picked a mode before this release, NivaroOS keeps it.

## Quality presets

The same menu offers one preset control. The server picks the numbers for the encoder it has: a GPU (NVIDIA NVENC, or Intel/AMD VA-API) or the CPU (x264). The status bar and the menu header show which one is in use, for example **GPU · NVENC** or **CPU · x264**.

| Preset | GPU (NVENC / VA-API) | CPU (x264) | Encoder tuning |
|---|---|---|---|
| **Low bandwidth** | 1280 wide · 24 fps · up to 2.5 Mbit/s | 1280 wide · 20 fps · up to 2 Mbit/s | GPU: slower preset (p4, VA target-usage 4), lower quality target. CPU: `veryfast` + CABAC (Main profile), so fewer bits for the same picture |
| **Balanced** (default) | 1920 wide · 30 fps · up to 8 Mbit/s | 1920 wide · 30 fps · up to 8 Mbit/s | Lowest latency (NVENC p1, VA target-usage 7, x264 `ultrafast`) |
| **High quality** | Full size · 60 fps · up to 25 Mbit/s | Full size · 30 fps · up to 20 Mbit/s | GPU: p4, higher quality target, spatial AQ. CPU: `ultrafast` at full size |
| **Custom** | Your frame rate (30/60) and bitrate (4 to 40 Mbit/s) | Same, capped at 30 fps and 1920 wide | Like Balanced |

All presets use capped variable bitrate, so an idle screen uses well under 1 Mbit/s whatever the preset. NVENC scales on the GPU when GStreamer has `cudaconvertscale` (GStreamer 1.22 and later).

### Measured

On a 3398×1220 desktop (Ryzen 5 3600, GTX 1080 Ti), using a throwaway X display. CPU is the encoder process, in % of one core, and includes the screen capture.

| Preset | Encoder | Full-screen video: fps · Mbit/s · CPU | One moving 960×540 window: fps · Mbit/s · CPU |
|---|---|---|---|
| Low | NVENC | 24 · 1.7 · 9 % | 24 · 0.7 · 8 % |
| Low | x264 | 20 · 2.1 · 27 % | 20 · 1.7 · 21 % |
| Balanced | NVENC | 30 · 7.6 · 13 % | 30 · 2.3 · 9 % |
| Balanced | x264 | 30 · 8.0 · 56 % | 30 · 6.0 · 48 % |
| High | NVENC | 60 · 25.7 · 27 % | 60 · 5.6 · 21 % |
| High | x264 | 30 · 19.7 · 102 % | 30 · 12.1 · 86 % |

To measure your own server: `WEBRTC_BENCH=1 go test -tags webrtc_integration -run BenchPresets -v ./services/vm-sidecar` (it needs Xvfb and ffplay, and never touches your real display).

### Adapt to the network

With **Adapt to the network** on (the default; not available with Custom), the viewer watches round-trip time and packet loss:

- If either stays high for 3 seconds (RTT over 250 ms or loss over 5 %), it steps down one preset, for example from High quality to Balanced, and says so.
- After 30 good seconds (RTT under 120 ms and loss under 1 %), it steps back up, but never above the preset you chose.
- After each change it waits 10 seconds before deciding again. The connection stays open while the encoder restarts with the new settings, so the picture continues.

## Switching tabs and leaving the app

Switching to another browser tab keeps the stream running, so it's still there, live, when you come back. A tab that stays hidden for 10 minutes stops streaming, so the server's encoder can rest. When you return, it shows **Paused while hidden** with a **Resume** button.

In the Android app, a short trip out of the app (under 90 seconds) keeps the stream. If you're away longer, it stops and reconnects when you come back.

## Requirements

- **Your network: LAN or Tailscale only.** WebRTC sends video over **UDP port 28650** straight from the server to your device. It works on the same network as the server, or over Tailscale. It does **not** work through the Cloudflare tunnel or a public link, because those carry TCP only. There, Auto uses Compatible right away.
- **A browser with WebRTC:** current Chrome, Edge, Firefox or Safari, or the NivaroOS Android app.
- **An encoder on the server.** NivaroOS picks the first one it finds:
  1. NVIDIA GPU: `nvh264enc` (GStreamer *bad* plugins plus the NVIDIA driver)
  2. Intel or AMD GPU: `vah264enc` (GStreamer *bad* plugins plus VA-API drivers)
  3. No GPU: `x264enc` (GStreamer *ugly* plugins). See the table above for its CPU cost.
- **The GStreamer packages.** NivaroOS installs them with Host Desktop. Existing installs pick them up the next time the VM sidecar starts after an update. Exceptions:
  - **Arch:** NivaroOS never installs single packages there automatically, because that would be a partial upgrade. Use **Reinstall** in Host Desktop, or run `pacman -S gst-plugins-good gst-plugins-bad gst-plugins-ugly`.
  - **Fedora/RHEL and openSUSE:** `x264enc` is only in RPM Fusion or Packman. Without them, WebRTC needs a GPU encoder.

## Firewall

The installer handles the firewall:

- Where **ufw** or **firewalld** is active, it opens UDP 28650 only on the LAN interface (the one with the default route) and on Tailscale (`tailscale*`). Docker and libvirt bridges stay closed. With firewalld the port opens per zone, so other interfaces in the LAN's zone get it too.
- It also keeps **TCP 5900** (VNC) blocked on every interface except loopback, for IPv4 and IPv6. Host Desktop never needs that port, because VNC runs over a local socket. The block is a safety net against an exposed VNC server.

These rules come from `nivaroos-host-firewall.service`, which re-applies them at every boot. The uninstaller removes them. If you run your own nftables or iptables rules with a drop policy, allow UDP 28650 from your LAN and Tailscale yourself.

## Troubleshooting

| Symptom | Check |
|---|---|
| Always ends up in **Compatible mode** | Are you on the LAN or Tailscale? Over the Cloudflare tunnel this is expected. |
| "No encoder" | Run `gst-inspect-1.0 nvh264enc vah264enc x264enc` on the server. At least one must be found. If none is, install the missing GStreamer plugins (see Requirements) or use **Reinstall**. |
| Shows **CPU · x264** on a server with a GPU | Run `gst-inspect-1.0 nvh264enc cudaupload` (NVIDIA) or `gst-inspect-1.0 vah264enc` (Intel/AMD). Restart the VM sidecar after installing the plugins. |
| Works on the LAN, not over Tailscale | Your Tailscale ACLs must allow UDP 28650 to the server. |
| Never connects on the LAN | Run `sudo systemctl status nivaroos-host-firewall`, then `sudo ufw status \| grep 28650` or `sudo firewall-cmd --list-ports --zone=<your zone>`. If the LAN interface changed, `sudo systemctl restart nivaroos-host-firewall` re-detects it. |
| Picture freezes or stutters | The viewer reconnects by itself after about 3 to 6 seconds. With **Adapt to the network** on, it also lowers the quality on a slow link. If it keeps happening, pick **Low bandwidth**, or switch to Compatible. |

WebRTC only reads the screen. It never changes the resolution, the monitors or the keyboard layout. Keyboard and mouse input go through the same path as Compatible mode.
