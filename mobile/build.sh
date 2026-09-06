#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "==> Building NivaroOS Android Release APK..."
/opt/flutter/bin/flutter build apk --release

OUTPUT_APK="$SCRIPT_DIR/build/app/outputs/flutter-apk/app-release.apk"
DEST_DIR="/DATA/Downloads"

if [ -f "$OUTPUT_APK" ]; then
    mkdir -p "$DEST_DIR"
    cp -f "$OUTPUT_APK" "$DEST_DIR/NivaroOS.apk"
    cp -f "$OUTPUT_APK" "$DEST_DIR/app-release.apk"
    chmod 666 "$DEST_DIR"/*.apk 2>/dev/null || true
    echo "==> Successfully copied APK to $DEST_DIR/NivaroOS.apk"
    ls -lh "$DEST_DIR"/NivaroOS.apk
else
    echo "==> Error: Output APK not found at $OUTPUT_APK"
    exit 1
fi
