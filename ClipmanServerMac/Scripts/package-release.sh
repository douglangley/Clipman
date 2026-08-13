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

case "$DIST" in
  ""|"/"|"$HOME") echo "Refusing unsafe distribution directory: $DIST" >&2; exit 1 ;;
esac
rm -rf "$DIST"
mkdir -p "$MACOS" "$RESOURCES"

SWIFT_BUILD="$DIST/swift-build"
mkdir -p "$SWIFT_BUILD"
for arch in arm64 x86_64; do
  swiftc \
    -target "$arch-apple-macosx13.0" \
    -module-cache-path "$SWIFT_BUILD/module-cache-$arch" \
    -o "$SWIFT_BUILD/clipman-server-wrapper-$arch" \
    "$SERVER_ROOT/Sources/ClipmanServer/main.swift" \
    -framework AppKit
done
lipo -create "$SWIFT_BUILD/clipman-server-wrapper-arm64" "$SWIFT_BUILD/clipman-server-wrapper-x86_64" -output "$MACOS/Clipman Server"
rm -rf "$SWIFT_BUILD"

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
  <string>13.0</string>
  <key>LSUIElement</key>
  <true/>
</dict>
</plist>
PLIST

SIGNING_IDENTITY="${CLIPMAN_SERVER_MAC_SIGNING_IDENTITY:--}"
SIGNING_FLAGS=(--force --sign "$SIGNING_IDENTITY")
if [[ "$SIGNING_IDENTITY" != "-" ]]; then
  SIGNING_FLAGS+=(--options runtime --timestamp)
fi
codesign "${SIGNING_FLAGS[@]}" "$RESOURCES/clipman-server"
codesign "${SIGNING_FLAGS[@]}" "$MACOS/Clipman Server"
codesign "${SIGNING_FLAGS[@]}" "$APP"
codesign --verify --deep --strict "$APP"

WRAPPER_ARCHES="$(lipo -archs "$MACOS/Clipman Server")"
CORE_ARCHES="$(lipo -archs "$RESOURCES/clipman-server")"
for required in arm64 x86_64; do
  [[ " $WRAPPER_ARCHES " == *" $required "* ]] || { echo "Wrapper is missing $required" >&2; exit 1; }
  [[ " $CORE_ARCHES " == *" $required "* ]] || { echo "Core is missing $required" >&2; exit 1; }
done
CORE_VERSION="$("$RESOURCES/clipman-server" --version)"
[[ "$CORE_VERSION" == "$VERSION" ]] || { echo "Core version $CORE_VERSION does not match $VERSION" >&2; exit 1; }

ZIP="$DIST/ClipmanServer-macOS-universal-$VERSION.zip"
COPYFILE_DISABLE=1 ditto -c -k --norsrc --keepParent "$APP" "$ZIP"
echo "Built $ZIP"
