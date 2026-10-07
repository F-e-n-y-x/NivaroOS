# Host Desktop: xpra shadow evaluated, not shipped (2026-10-08)

Decision: **not clearly better than x11vnc/noVNC on this box, so nothing was integrated.**
Host Desktop stays on x11vnc. The design from `refs/wip/selkies-design:RESUME.md` (mode seam
vnc|selkies|nova, cookie-auth proxy, on-demand unit, panel fallback) still applies to a future
streamer. xpra just doesn't earn a slot in it.

## Safety result (the part that went wrong with Selkies)

xpra shadow on the live `:0` did **not** touch the display. Before and after the run I compared
`xrandr --query`, `xrandr --listmonitors`, `setxkbmap -query` and the list of root-window
properties, and they were identical. The xrandr output also matched the saved baseline (Screen 0
3398x1220, HDMI-0 connected primary 3398x1220+0+0). While the server ran, the only change was the
root properties `XPRA_SERVER*`, and xpra removed them when it exited.

From reading the code (xpra 6.5.4):

- `ShadowX11Server.set_initial_resolution` and `set_desktop_geometry` do nothing, so it never
  changes the resolution.
- It only touches the keymap with `--keyboard-layout=client|auto`. With `--readonly=yes`, the
  server also rejects the client's `keymap-changed`.
- `XPRA_SHADOW_NVFBC=0` keeps it away from NvFBC, where the Selkies crash came from, and away
  from Nova.
- When the display isn't accessible it may `xauth add` to `$XAUTHORITY`. Hand it a private copy
  of the cookie, not the original.

## Measurements

Same headless Chrome client (3398x1220, DPR 1) for every run, same resolution. Server CPU comes
from `/proc` utime+stime and bandwidth from `ss bytes_sent` on the server port. "fps seen" counts
distinct changes of the ffplay frame-counter patch on the client canvas. Latency is a CDP mouse
press, then the click target in the streamed canvas turning white (rAF polling).

Synthetic `:99` (Xvfb 3398x1220, ffplay testsrc2 1280x720@60, GTK click target), 20 s windows,
2 reps:

| mode | server CPU | client CPU | Mbit/s | fps seen | latency p50 / p90 |
|---|---|---|---|---|---|
| x11vnc (prod args, snapfb) + noVNC balanced | 17-21 % | 40-52 % | 13.6-14.5 | 28-30 | 93-97 / 99-114 ms |
| xpra over http (what LAN/Tailscale gets: jpeg/webp only) | 18-19 % | 197-206 % | 5.0-6.8 | 22-23 | 115-123 / 153 ms |
| xpra over https (h264 via x264 on CPU + WebCodecs) | 77-123 % | 172-179 % | 3.2-11 | 11 | 193-212 / 252-271 ms |
| xpra tuned (speed 100, min-speed 80, quality 70), http | 19 % | 208 % | 2.8 | 24 | 131 / 148 ms |
| xpra tuned, https | 47 % | 178 % | 10.6 | 17 | 181 / 234 ms |

Live `:0` (owner's mostly idle Cinnamon desktop), view-only, 30 s, 2 reps:

| mode | server CPU | Mbit/s |
|---|---|---|
| x11vnc (live instance, extra view-only noVNC viewer) | 1.9-2.0 % | 0.02-0.04 |
| xpra over http | 20.3-20.5 % | 16-17.7 (while idle) |
| xpra over https | 66-76 % | 0.03 |

- GPU encoder: no NVENC sessions in any run (nvidia-smi showed 0 sessions, 0 %).
- Visuals: the x11vnc frames were crisp. xpra over http was visibly lossy (smeared edges,
  unreadable small text). xpra over https was OK but softer. Screenshot sequences are in
  `/root/nivaroos-wip/xpra-research/bench/shots-*` and `crop3.png` / `cmp3.png` (vnc / xpra-http /
  xpra-https).

## Why xpra loses here

1. **No NVENC.** xpra's nvenc needs `pycuda`. Debian's `python3-pycuda` pulls `libcuda1` 550,
   `nvidia-alternative` and related packages over the running 580.178.04 .run driver. That would
   break the GPU stack and Nova. A pip pycuda build needs the CUDA toolkit. So xpra encodes on the
   CPU, and x264 at 3398x1220 costs 50-120 % of a core for 11-17 fps.
2. **No video over plain http.** The xpra-html5 client only enables offscreen decoding and
   h264/vp8 on `https:`. Plain http (LAN/Tailscale through the gateway on :80) gets jpeg/webp
   only. That is lossier than VNC, slower and 4-5x heavier on the client.
3. **Idle cost.** Shadow mode polls the root window. On an idle desktop it uses about 20 % CPU and
   16 Mbit/s, against x11vnc's 2 % and 0.03 Mbit/s.

## Packaging notes (if xpra is ever revisited)

- xpra is not in Debian trixie. The official repo is `https://xpra.org` (dists/trixie, key
  `B4993B57323148E37977E5D873254CAD17978FAF`, `https://xpra.org/xpra.asc`). InRelease plus the
  Packages sha256 verified OK for 6.5.4-r0. Packages needed: xpra-common, xpra-server, xpra-x11,
  xpra-codecs, xpra-html5, plus libjs-jquery and libjs-jquery-ui from Debian (html5 symlinks to
  them). Also libxres1 and libturbojpeg0.
- **The xpra-server postinst enables and starts `xpra-server.socket` on TCP 14500 on all
  interfaces**, which would expose it on the LAN. An installer would have to
  `systemctl mask xpra-server.socket xpra-server.service` before `apt install`.
- `auth=file` checks the username against the server's user (verify-username is on for root), and
  a password file must have no trailing newline. Behind the sidecar, a 0600 unix socket and no
  password (like x11vnc) would be simpler.
- Flags used for a safe shadow: `--readonly=yes` (or input only), `--keyboard-layout=none`,
  `--no-clipboard --no-notifications --no-system-tray --no-mdns --no-audio --no-pulseaudio
  --no-dbus-control --dbus-launch= --no-printing --no-file-transfer --no-webcam
  --rfb-upgrade=0 --resize-display=no --sharing=no --bind=none --bind-ws=127.0.0.1:PORT`,
  `--sessions-dir=<private>`, env `XPRA_SHADOW_NVFBC=0`, and XAUTHORITY pointing at a private
  cookie copy. The launcher is `/root/nivaroos-wip/xpra-research/xpra-shadow.sh`.

## When to look again

Only if NVENC becomes usable without touching the driver (for example a distro pycuda that
matches the installed driver, or xpra dropping pycuda), **and** Host Desktop is served over https
everywhere. Even then, the idle polling cost stays. The better bet for "smooth" is still the Nova
web client.

Research files: `/root/nivaroos-wip/xpra-research/` (extracted debs in `root/`, signed index,
`bench/` harness: `bench.mjs`, `measure.js`, `latency.py`, `tlsfwd.py`, results `r0/r1/r2/r3.jsonl`,
`live/fp-*.txt` display fingerprints). Nothing was installed system-wide. Delete the directory
when it's no longer needed.
