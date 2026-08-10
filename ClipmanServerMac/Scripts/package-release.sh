#!/usr/bin/env zsh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SERVER_ROOT="$ROOT/ClipmanServerMac"
TEMP_ROOT="${CLIPMAN_TEMP_ROOT:-$HOME/Projects/Codex/Temp/clipman}"
DIST="${CLIPMAN_SERVER_MAC_DIST_DIR:-$TEMP_ROOT/server-mac-dist}"
APP="$DIST/Clipman Server.app"
CONTENTS="$APP/Contents"
MACOS="$CONTENTS/MacOS"
RESOURCES="$CONTENTS/Resources"
VERSION="$(tr -d '[:space:]' < "$ROOT/ClipmanServer/version.txt")"
BUILD_VERSION="$VERSION.0"
if [[ ! "$VERSION" =~ '^[0-9]+\.[0-9]+\.[0-9]+$' ]]; then
  echo "Invalid Clipman Server version: $VERSION" >&2
  exit 1
fi

rm -rf "$DIST"
mkdir -p "$MACOS" "$RESOURCES"

swiftc \
  -o "$MACOS/Clipman Server" \
  "$SERVER_ROOT/Sources/ClipmanServer/main.swift" \
  -framework AppKit

GO_BUILD="$DIST/go-build"
mkdir -p "$GO_BUILD"
for arch in amd64 arm64; do
  (cd "$ROOT/ClipmanServer" && CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath \
    -ldflags="-s -w -X github.com/OnjLouis/Clipman/ClipmanServer/internal/buildinfo.Version=$VERSION" \
    -o "$GO_BUILD/clipman-server-$arch" ./cmd/clipman-server)
done
lipo -create "$GO_BUILD/clipman-server-amd64" "$GO_BUILD/clipman-server-arm64" -output "$RESOURCES/clipman-server"
chmod +x "$RESOURCES/clipman-server"
rm -rf "$GO_BUILD"
cp "$ROOT/ClipmanServer/Manual.html" "$RESOURCES/Manual.html"
cp "$ROOT/LICENSE.txt" "$RESOURCES/LICENSE.txt"

cat > "$CONTENTS/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>Clipman Server</string>
  <key>CFBundleIdentifier</key>
  <string>com.andrelouis.clipman-server</string>
  <key>CFBundleName</key>
  <string>Clipman Server</string>
  <key>CFBundleDisplayName</key>
  <string>Clipman Server</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>$VERSION</string>
  <key>CFBundleVersion</key>
  <string>$BUILD_VERSION</string>
  <key>LSMinimumSystemVersion</key>
  <string>10.13</string>
  <key>LSUIElement</key>
  <true/>
</dict>
</plist>
PLIST

codesign --force --deep --sign - "$APP" >/dev/null

COPYFILE_DISABLE=1 ditto -c -k --norsrc --keepParent "$APP" "$DIST/ClipmanServerMac-$VERSION.zip"
echo "Built $DIST/ClipmanServerMac-$VERSION.zip"
