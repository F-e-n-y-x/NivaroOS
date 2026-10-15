#!/bin/bash
# restart.sh "<extra Xkasmvnc args>"
docker restart hdalt-kasm >/dev/null; docker exec hdalt-kasm rm -f /tmp/.X11-unix/X1 /tmp/.X1-lock
docker exec -d hdalt-kasm bash -c "Xkasmvnc :1 -geometry 3398x1220 -depth 24 -websocketPort 8444 -httpd /usr/share/kasmvnc/www -SecurityTypes None -DisableBasicAuth 1 -sslOnly 0 -FrameRate 60 -interface 0.0.0.0 -AlwaysShared $1 > /tmp/xk.log 2>&1"
sleep 3
docker exec -d hdalt-kasm bash -c 'while true; do kasmxproxy -a :97 -v :1 -f 60 >> /tmp/kp.log 2>&1; sleep 1; done'
sleep 2
