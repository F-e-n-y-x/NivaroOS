#!/bin/bash
# Throwaway KasmVNC 1.5.0 in a container, mirroring :97 via kasmxproxy (attach mode), web on 127.0.0.1:14699.
set -e
docker rm -f hdalt-kasm >/dev/null 2>&1 || true
docker run -d --name hdalt-kasm --ipc host --device nvidia.com/gpu=all -e NVIDIA_DRIVER_CAPABILITIES=all \
  -v /tmp/.X11-unix/X97:/tmp/.X11-unix/X97 -v /root/nivaroos-wip/hostdesk-alt-proto/kasm:/k:ro \
  -p 127.0.0.1:14699:8444 debian:latest sleep infinity
docker exec hdalt-kasm bash -c 'apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq /k/kasmvncserver_trixie_1.5.0_amd64.deb libavcodec61 >/dev/null && echo installed'
docker exec -d hdalt-kasm bash -c 'Xkasmvnc :1 -geometry 3398x1220 -depth 24 -websocketPort 8444 -httpd /usr/share/kasmvnc/www -SecurityTypes None -DisableBasicAuth 1 -sslOnly 0 -FrameRate 60 -interface 0.0.0.0 -AlwaysShared ${XK_EXTRA} > /tmp/xk.log 2>&1'
sleep 3
docker exec -d hdalt-kasm bash -c 'while true; do kasmxproxy -a :97 -v :1 -f 60 >> /tmp/kp.log 2>&1; sleep 1; done'
