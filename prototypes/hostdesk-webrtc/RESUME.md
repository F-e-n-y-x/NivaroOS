# Host Desktop alternatives prototype (hostdesk-alt, 2026-10-16)

Not product code. Full write-up: /DATA/Downloads/NivaroOS-host-desktop-options-2026-10.md

- `main.go` + `xtest.go` + `index.html`: WebRTC lane. Encoder child (ffmpeg x11grab+h264_nvenc/libx264,
  or any `-pipeline` command such as gst-launch ... rtph264pay ! udpsink) sends RTP to a loopback UDP
  port; pion forwards it to one browser peer (non-trickle POST /offer, single ICE UDP port 28650).
  Input over a data channel -> XTEST (`-xtest`) or RFB PointerEvent/KeyEvent on the VNC socket (`-rfb`).
  Page self-heals: dead peer or >3 s without a decoded frame re-offers.
- `bench/`: CDP harness (bench.mjs/measure.js from refs/wip/xpra-eval), conn.mjs (connect time + freeze
  gaps), srvlat.py (server-side capture/encode latency), kasm/run.sh (KasmVNC 1.5 + kasmxproxy container),
  results.jsonl (all raw runs).
- Throwaway display only: Xvfb :97 3398x1220 + ffplay testsrc2 + GTK click target. Never :0.

Build: GOWORK=off GOTOOLCHAIN=local go build . ; run: ./hostdesk-webrtc -display :97 -xtest
