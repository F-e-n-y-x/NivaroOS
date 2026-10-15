# Host Desktop: Smooth (beta)

**Smooth** streams your server's desktop as H.264 video over WebRTC instead of VNC. Video, scrolling and window dragging look much smoother, and it uses less bandwidth. It's a beta, so it is off by default. **Compatible** (VNC) stays the default, and NivaroOS falls back to it automatically whenever Smooth can't connect.

## Turn it on

In **Host Desktop → settings**, set **Stream mode**:

| Mode | What it does |
|---|---|
| **Compatible** (default) | VNC, the way Host Desktop always worked |
| **Auto** | Tries Smooth first. If no picture arrives within 4 seconds, it switches to Compatible for that session and shows a **Compatible mode** chip with the reason |
| **Smooth** | WebRTC only |

You can also pick **30 or 60 fps** (default 30) and a **maximum bitrate** (default 12000 kbps). 60 fps costs about twice the CPU or GPU time.

## Requirements

- **Your network: LAN or Tailscale only.** Smooth sends video over **UDP port 28650** straight from the server to your browser. It works on the same network as the server, or over Tailscale. It does **not** work through the Cloudflare tunnel or a public link, because those carry TCP only. There, Auto uses Compatible.
- **A browser with WebRTC:** current Chrome, Edge, Firefox or Safari, or the NivaroOS Android app.
- **An encoder on the server.** NivaroOS picks the first one it finds:
  1. NVIDIA GPU: `nvh264enc` (GStreamer *bad* plugins plus the NVIDIA driver)
  2. Intel or AMD GPU: `vah264enc` (GStreamer *bad* plugins plus VA-API drivers)
  3. No GPU: `x264enc` (GStreamer *ugly* plugins). This uses about one CPU core, and the stream is scaled down to at most 1920 pixels wide.
- **The GStreamer packages.** NivaroOS installs them with Host Desktop. Existing installs pick them up the next time the VM sidecar starts after an update. Exceptions:
  - **Arch:** NivaroOS never installs single packages there automatically, because that would be a partial upgrade. Use **Reinstall** in Host Desktop, or run `pacman -S gst-plugins-good gst-plugins-bad gst-plugins-ugly`.
  - **Fedora/RHEL and openSUSE:** `x264enc` is only in RPM Fusion or Packman. Without them, Smooth needs a GPU encoder.

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
| Works on the LAN, not over Tailscale | Your Tailscale ACLs must allow UDP 28650 to the server. |
| Never connects on the LAN | Run `sudo systemctl status nivaroos-host-firewall`, then `sudo ufw status \| grep 28650` or `sudo firewall-cmd --list-ports --zone=<your zone>`. If the LAN interface changed, `sudo systemctl restart nivaroos-host-firewall` re-detects it. |
| Picture freezes | The viewer reconnects by itself after about 3 to 6 seconds. If it keeps freezing, lower the fps or bitrate, or switch to Compatible. |

Smooth only reads the screen. It never changes the resolution, the monitors or the keyboard layout. Keyboard and mouse input go through the same path as Compatible mode.
