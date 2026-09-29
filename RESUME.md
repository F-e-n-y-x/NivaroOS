# Host Desktop: Selkies streaming mode (stopped, not shipped)

Status on 2026-09-30: research and a prototype only. Nothing is committed. Host Desktop
stays on x11vnc/noVNC, unchanged.

## Why it stopped

Selkies 2.0.0 is the current release, from 2026-09-23. It can't be run reliably against
this box's real desktop (Xorg :0, GTX 1080 Ti, driver 580.178.04), and every further test
risks changing the owner's live display.

1. **The server crashes when a viewer connects**, in both transports (WebSocket and WebRTC).
   pixelflux's NvFBC probe thread segfaults inside the host libxcb and takes the whole
   process down:
   ```
   pf-nvfbc-probe[151122]: segfault at 0 ... in libxcb.so.1.1.0   (webrtc run)
   pf-nvfbc-probe[155548]: segfault at 0 ... in libxcb.so.1.1.0   (websockets run, exit 139)
   ```
   Source: pixelflux `src/x11/nvfbc.rs` `open()` / `answered_within()`. It has no setting to
   turn NvFBC off. The only paths that skip it are software encoding (`--use-cpu`, which
   loses NVENC), a non-NVIDIA encode node, or `--watermark-path`, which would be a hack.
2. **It reconfigures the physical display even with `--enable-resize=false`.** When a
   client attaches, `websockets_mode.py` (the layout code around line 5080) does three
   things:
   - rounds the width up to a multiple of 8 and mode-sets it (3390 -> 3392). That failed,
     so it grew the framebuffer instead;
   - adds a RandR monitor `selkies-primary` on the first *connected* output, HDMI-0, which
     is connected but inactive;
   - marks HDMI-0 as the primary output (`designate_primary_output`).
   The XRANDR event restarted the owner's live x11vnc session at 03:54:24. After the test,
   `selkies-primary` was deleted. The screen size was already 3390x1220 before the test.
   **HDMI-0 is still flagged primary.** The owner may want `xrandr --output DP-4 --primary`.
   The agent was not allowed to run it.
3. **The dashboard is plain http on LAN and Tailscale** (gateway on :80). WebCodecs needs a
   secure context, so Selkies' default WebSocket/H.264 path would drop to JPEG stripes
   there. WebRTC mode (RTCPeerConnection works over http) would be needed on LAN and
   Tailscale. The WebSocket path only works over the Cloudflare tunnel (https).

## What was learned about Selkies 2.0 (for a retry, or for the Nova web client)

- GStreamer is gone. Capture and encode are done by the `pixelflux` Rust extension
  (NVENC/VA-API/x264) and audio by `pcmflux`. It is one aiohttp app on one port. The
  default transport is WebSockets; WebRTC is opt-in (`--mode=webrtc`, `--enable-dual-mode`,
  `POST /api/switch {"mode":...}`).
- Packaging: `pip install selkies==2.0.0` into a venv works on Python 3.13. The venv is
  283 MB and pulls 31 packages. pixelflux and pcmflux ship manylinux_2_28 wheels for
  x86_64 and aarch64, cp39 to cp315. `python3 -m venv --without-pip` plus a pinned pip
  wheel avoids needing Debian's `python3-venv`. Hash-locking all platforms is possible
  with the PyPI JSON digests. Release assets also carry sha256 digests (debs, rpms,
  AppImage).
- Frozen dependency set, py3.13 x86_64:
  `/root/nivaroos-wip/selkies-research/freeze.txt`.
- `--unix-socket` exists, but WebRTC mode's in-process peer dials
  `ws://localhost:<port>/api/ws` over TCP (`webrtc_mode.py:476`). WebRTC therefore needs
  a loopback TCP port, plus basic auth or the master token to keep other local users out.
- To avoid public relays, use `--rtc-config-json` with `{"iceServers":[]}`. The file must
  be root-owned and not group- or world-writable. Without it, Selkies falls back to Google
  STUN and the public `openrelay.metered.ca` TURN relay.
- Embedding: the web client uses relative assets. Its localStorage namespace is
  origin+pathname, so don't put per-session ids in the path. Sidebar-less mode is
  `--ui-show-sidebar=false`. The parent page drives it with `postMessage` (`settings`
  {framerate, video_bitrate, encoder}, `getStats` -> `stats`, `setScaleLocally`,
  `clipboardUpdateFromUI`); see `addons/selkies-web-core/README.md`.
- Other flags a physical host needs: `--uinput-gamepad=false`, `--gamepad-enabled=false`,
  `--publish-input-devices=false`, `--file-transfers=none` (it runs as whatever user
  starts it), `--second-screen=false`, `--audio-enabled=false` (the user's PipeWire is not
  reachable from a system unit).

Research files: `/root/nivaroos-wip/selkies-research/`. It holds the docs, core client
sources, the venv, `run/start.sh`, the CDP driver `cdp/cdp.mjs` and `bench.py`. Delete it
when no longer needed.

## Planned design (for when a streamer can attach to a physical desktop safely)

- Sidecar seam: `GET /host/stream` returns
  `{modes:{vnc:{available},selkies:{installed,available,reason}}}`. `POST
  /host/stream/<mode>/session` (JWT) returns `{id, url}` and sets an HttpOnly SameSite
  cookie. Proxy `/host/<mode>/...` is cookie-authenticated, strips Origin after the
  sidecar's own same-origin check, and adds the streamer's random basic-auth header. One
  viewer at a time: a newer session revokes the older one.
- A unit `nivaroos-host-selkies.service` runs on demand: the sidecar starts it on session
  create and stops it about 60 s after the last WebSocket closes, which frees the NVENC
  session. It runs as a system user, gets a copy of the X cookie from an `ExecStartPre=+`
  step, and listens on 127.0.0.1 only.
- Install: from `InstallHostDesktop`, as an optional step that only warns on failure.
  Sidecar-startup sync carries upgrades. Uninstall removes `/opt/nivaroos/selkies`.
- Panel: a pure `streamMode.ts` chooses auto/smooth/compatible. Use WebRTC when
  `!isSecureContext`, WebSocket otherwise. Fall back to VNC on install/connection
  failure or after a timeout. The mobile app can use the same session API in a WebView.

## Measurements (this box)

- x11vnc, live, one viewer connected (the owner), settings FPS 60 + snapfb, mostly idle
  desktop: **8.0 % of one core** over 20 s (read-only `/proc` sampling). It uses no GPU.
- Encoder benchmark (ffmpeg, synthetic testsrc2, 600 frames; encoder CPU is measured
  after subtracting the source-generation baseline):

  | size | encoder | max fps | CPU per frame | CPU at 60 fps |
  |---|---|---|---|---|
  | 1920x1080 | h264_nvenc p1/ll 8M | 407 | 1.05 ms | 6.3 % of a core |
  | 1920x1080 | libx264 ultrafast/zerolatency 8M | 286 | 8.84 ms | 53 % |
  | 3392x1220 | h264_nvenc p1/ll 8M | 309 | 1.54 ms | 9.2 % |
  | 3392x1220 | libx264 ultrafast/zerolatency 8M | 163 | 16.1 ms | 97 % |

  NVENC paced at 3392x1220@60 used 8 % of the encoder engine, about 3.7 ms average encode
  latency, and 1 session, released on exit.
- A live Selkies stream could not be measured: it crashes as described above.
