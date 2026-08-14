#!/usr/bin/env zsh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/ClipmanServer/version.txt")"
TEMP_ROOT="${CLIPMAN_TEMP_ROOT:-$HOME/Projects/Codex/Temp/clipman}"
DIST="${CLIPMAN_SERVER_RELEASE_OUTPUT_DIR:-$TEMP_ROOT/server-release-dist}"
WINDOWS_WRAPPER="${CLIPMAN_SERVER_WINDOWS_EXE:-$ROOT/ClipmanServerWindows/dist/Clipman Server.exe}"
MAC_APP="${CLIPMAN_SERVER_MAC_APP:-$ROOT/ClipmanServerMac/dist/Clipman Server.app}"

[[ "$VERSION" =~ '^[0-9]+\.[0-9]+\.[0-9]+$' ]] || {
  echo "Invalid Clipman Server version: $VERSION" >&2
  exit 1
}
case "$DIST" in
  ""|"/"|"$HOME"|"$ROOT"|"$ROOT"/*)
    echo "Refusing unsafe release output directory: $DIST" >&2
    exit 1
    ;;
esac

for required in \
  "$WINDOWS_WRAPPER" \
  "$MAC_APP/Contents/MacOS/Clipman Server" \
  "$MAC_APP/Contents/Resources/clipman-server" \
  "$ROOT/ClipmanServerWindows/Install-ClipmanServer.ps1" \
  "$ROOT/ClipmanServerMac/Scripts/install.sh" \
  "$ROOT/ClipmanServerMac/Scripts/clipmanserver" \
  "$ROOT/ClipmanServerLinux/install-clipman-server.sh" \
  "$ROOT/ClipmanServerLinux/install-clipman-server-system-helper.sh" \
  "$ROOT/ClipmanServerLinux/clipmanserver" \
  "$ROOT/ClipmanServerDocker/Dockerfile.release" \
  "$ROOT/ClipmanServerDocker/docker-entrypoint.sh" \
  "$ROOT/ClipmanServer/Manual.html" \
  "$ROOT/ClipmanServer/clipman-server-settings.example.jsonc" \
  "$ROOT/LICENSE.txt"; do
  [[ -e "$required" ]] || { echo "Required release input is missing: $required" >&2; exit 1; }
done

mkdir -p "$TEMP_ROOT" "$DIST"
STAGING="$(mktemp -d "$TEMP_ROOT/server-release.XXXXXX")"
PACKAGE_ROOT="$STAGING/ClipmanServer-$VERSION"
FINAL="$DIST/ClipmanServer-$VERSION"

cleanup() {
  rm -rf "$STAGING"
}
trap cleanup EXIT

if [[ "${CLIPMAN_SERVER_RELEASE_SKIP_TESTS:-}" != "1" ]]; then
  (cd "$ROOT/ClipmanServer" && go test ./... && go vet ./...)
fi

build_server() {
  local goos="$1" goarch="$2" goarm="$3" output="$4"
  mkdir -p "$(dirname "$output")"
  if [[ -n "$goarm" ]]; then
    (cd "$ROOT/ClipmanServer" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
      go build -trimpath -ldflags="-s -w -X github.com/OnjLouis/Clipman/ClipmanServer/internal/buildinfo.Version=$VERSION" \
      -o "$output" ./cmd/clipman-server)
  else
    (cd "$ROOT/ClipmanServer" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags="-s -w -X github.com/OnjLouis/Clipman/ClipmanServer/internal/buildinfo.Version=$VERSION" \
      -o "$output" ./cmd/clipman-server)
  fi
}

build_updater() {
  local goos="$1" goarch="$2" goarm="$3" output="$4"
  mkdir -p "$(dirname "$output")"
  if [[ -n "$goarm" ]]; then
    (cd "$ROOT/ClipmanServer" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
      go build -trimpath -ldflags="-s -w" -o "$output" ./cmd/clipman-server-updater)
  else
    (cd "$ROOT/ClipmanServer" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags="-s -w" -o "$output" ./cmd/clipman-server-updater)
  fi
}

copy_support() {
  local target="$1"
  mkdir -p "$target/support"
  cp "$ROOT/ClipmanServer/Manual.html" "$target/support/Manual.html"
  cp "$ROOT/ClipmanServer/clipman-server-settings.example.jsonc" "$target/support/clipman-server-settings.example.jsonc"
  cp "$ROOT/LICENSE.txt" "$target/support/LICENSE.txt"
  {
    echo "Clipman Server Go modules"
    (cd "$ROOT/ClipmanServer" && go list -m all)
  } > "$target/support/DEPENDENCIES.txt"
}

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

write_sums() {
  local directory="$1"
  (
    cd "$directory"
    find . -type f ! -path './SHA256SUMS' -print | LC_ALL=C sort | while IFS= read -r file; do
      relative="${file#./}"
      printf '%s  %s\n' "$(shasum -a 256 "$file" | awk '{print $1}')" "$relative"
    done > SHA256SUMS
  )
}

WINDOWS="$PACKAGE_ROOT/windows-amd64"
MACOS="$PACKAGE_ROOT/macos-universal"
mkdir -p "$WINDOWS" "$MACOS"
copy_support "$WINDOWS"
copy_support "$MACOS"

cp "$WINDOWS_WRAPPER" "$WINDOWS/clipmanserver.exe"
cp "$WINDOWS_WRAPPER" "$WINDOWS/Clipman Server.exe"
cp "$ROOT/ClipmanServerWindows/Install-ClipmanServer.ps1" "$WINDOWS/install.ps1"

COPYFILE_DISABLE=1 ditto --norsrc "$MAC_APP" "$MACOS/Clipman Server.app"
cp "$ROOT/ClipmanServerMac/Scripts/clipmanserver" "$MACOS/clipmanserver"
cp "$ROOT/ClipmanServerMac/Scripts/install.sh" "$MACOS/install.sh"
chmod 755 "$MACOS/clipmanserver" "$MACOS/install.sh"

build_linux_target() {
  local directory="$1" goarch="$2" goarm="$3"
  local target="$PACKAGE_ROOT/$directory"
  mkdir -p "$target/support"
  copy_support "$target"
  build_server linux "$goarch" "$goarm" "$target/support/clipman-server"
  build_updater linux "$goarch" "$goarm" "$target/support/clipman-server-updater"
  cp "$ROOT/ClipmanServerLinux/clipmanserver" "$target/clipmanserver"
  cp "$ROOT/ClipmanServerLinux/install-clipman-server.sh" "$target/install.sh"
  cp "$ROOT/ClipmanServerLinux/install-clipman-server.sh" "$target/install-clipman-server.sh"
  cp "$ROOT/ClipmanServerLinux/install-clipman-server-system-helper.sh" "$target/install-clipman-server-system-helper.sh"
  cat > "$target/clipman-server" <<'SH'
#!/usr/bin/env sh
set -eu
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$SCRIPT_DIR/support/clipman-server" \
  --config "${CLIPMAN_SERVER_CONFIG:-$SCRIPT_DIR/Settings/clipman-server-settings.json}" "$@"
SH
  cp "$target/clipman-server" "$target/run-clipman-server.sh"
  chmod 755 \
    "$target/clipmanserver" \
    "$target/clipman-server" \
    "$target/run-clipman-server.sh" \
    "$target/install.sh" \
    "$target/install-clipman-server.sh" \
    "$target/install-clipman-server-system-helper.sh" \
    "$target/support/clipman-server" \
    "$target/support/clipman-server-updater"
}

build_linux_target linux-amd64 amd64 ""
build_linux_target linux-arm64 arm64 ""
build_linux_target linux-armv7 arm 7

cat > "$WINDOWS/manifest-v2.json" <<JSON
{
  "format_version": 2,
  "name": "Clipman Server",
  "version": "$VERSION",
  "artifacts": [
    {
      "os": "windows",
      "architecture": "amd64",
      "path": "clipmanserver.exe",
      "sha256": "$(sha256_file "$WINDOWS/clipmanserver.exe")",
      "executable": true
    }
  ]
}
JSON

cat > "$MACOS/manifest-v2.json" <<JSON
{
  "format_version": 2,
  "name": "Clipman Server",
  "version": "$VERSION",
  "artifacts": [
    {
      "os": "darwin",
      "architecture": "universal",
      "path": "Clipman Server.app/Contents/MacOS/Clipman Server",
      "sha256": "$(sha256_file "$MACOS/Clipman Server.app/Contents/MacOS/Clipman Server")",
      "executable": true
    }
  ]
}
JSON

for directory in linux-amd64 linux-arm64 linux-armv7; do
  case "$directory" in
    linux-amd64) architecture=amd64 ;;
    linux-arm64) architecture=arm64 ;;
    linux-armv7) architecture=arm ;;
  esac
  target="$PACKAGE_ROOT/$directory"
  cat > "$target/manifest-v2.json" <<JSON
{
  "format_version": 2,
  "name": "Clipman Server",
  "version": "$VERSION",
  "artifacts": [
    {
      "os": "linux",
      "architecture": "$architecture",
      "path": "support/clipman-server",
      "sha256": "$(sha256_file "$target/support/clipman-server")",
      "executable": true
    }
  ]
}
JSON
done

DOCKER="$PACKAGE_ROOT/docker"
mkdir -p "$DOCKER/binaries/amd64" "$DOCKER/binaries/arm64" "$DOCKER/binaries/armv7"
cp "$ROOT/ClipmanServerDocker/Dockerfile.release" "$DOCKER/Dockerfile"
cp "$ROOT/ClipmanServerDocker/docker-entrypoint.sh" "$DOCKER/docker-entrypoint.sh"
cp "$PACKAGE_ROOT/linux-amd64/support/clipman-server" "$DOCKER/binaries/amd64/clipman-server"
cp "$PACKAGE_ROOT/linux-arm64/support/clipman-server" "$DOCKER/binaries/arm64/clipman-server"
cp "$PACKAGE_ROOT/linux-armv7/support/clipman-server" "$DOCKER/binaries/armv7/clipman-server"
chmod 755 "$DOCKER/docker-entrypoint.sh" "$DOCKER"/binaries/*/clipman-server

COMMIT="$(git -C "$ROOT" rev-parse HEAD)"
cat > "$PACKAGE_ROOT/release-manifest.json" <<JSON
{
  "format_version": 1,
  "name": "Clipman Server",
  "server_version": "$VERSION",
  "commit": "$COMMIT",
  "directories": [
    "windows-amd64",
    "macos-universal",
    "linux-amd64",
    "linux-arm64",
    "linux-armv7"
  ],
  "primary_commands": {
    "clipmanserver": "platform Clipman Server management entrypoint"
  },
  "compatibility_names": [
    "clipman-server",
    "run-clipman-server.sh",
    "install-clipman-server.sh",
    "Clipman Server.exe",
    "Clipman Server.app",
    "ClipmanServer-$VERSION.zip"
  ]
}
JSON
cp "$ROOT/LICENSE.txt" "$PACKAGE_ROOT/LICENSE.txt"

for directory in windows-amd64 macos-universal linux-amd64 linux-arm64 linux-armv7; do
  write_sums "$PACKAGE_ROOT/$directory"
done
write_sums "$PACKAGE_ROOT"

cmp -s "$WINDOWS/clipmanserver.exe" "$WINDOWS/Clipman Server.exe" || {
  echo "Windows Clipman Server compatibility alias differs from clipmanserver.exe." >&2
  exit 1
}
for directory in linux-amd64 linux-arm64 linux-armv7; do
  cmp -s "$PACKAGE_ROOT/$directory/clipman-server" "$PACKAGE_ROOT/$directory/run-clipman-server.sh" || {
    echo "$directory legacy server launcher differs from clipman-server." >&2
    exit 1
  }
  cmp -s "$PACKAGE_ROOT/$directory/install.sh" "$PACKAGE_ROOT/$directory/install-clipman-server.sh" || {
    echo "$directory legacy installer name differs from install.sh." >&2
    exit 1
  }
done
[[ "$("$MACOS/Clipman Server.app/Contents/Resources/clipman-server" --version)" == "$VERSION" ]] || {
  echo "The packaged macOS server core version does not match $VERSION." >&2
  exit 1
}

for directory in windows-amd64 macos-universal linux-amd64 linux-arm64 linux-armv7; do
  (cd "$PACKAGE_ROOT/$directory" && shasum -a 256 -c SHA256SUMS)
done
(cd "$PACKAGE_ROOT" && shasum -a 256 -c SHA256SUMS)

rm -rf "$FINAL"
mv "$PACKAGE_ROOT" "$FINAL"

WINDOWS_ZIP="$DIST/ClipmanServer-Windows-x64-$VERSION.zip"
MACOS_ZIP="$DIST/ClipmanServer-macOS-universal-$VERSION.zip"
LAYOUT_ZIP="$DIST/ClipmanServer-$VERSION-layout.zip"
LINUX_AMD64="$DIST/ClipmanServer-Linux-amd64-$VERSION.tar.gz"
LINUX_ARM64="$DIST/ClipmanServer-Linux-arm64-$VERSION.tar.gz"
LINUX_ARMV7="$DIST/ClipmanServer-Linux-armv7-$VERSION.tar.gz"
rm -f "$WINDOWS_ZIP" "$MACOS_ZIP" "$LAYOUT_ZIP" "$LINUX_AMD64" "$LINUX_ARM64" "$LINUX_ARMV7"

COPYFILE_DISABLE=1 ditto -c -k --norsrc --keepParent "$FINAL/windows-amd64" "$WINDOWS_ZIP"
COPYFILE_DISABLE=1 ditto -c -k --norsrc --keepParent "$FINAL/macos-universal" "$MACOS_ZIP"
COPYFILE_DISABLE=1 tar -czf "$LINUX_AMD64" -C "$FINAL" linux-amd64
COPYFILE_DISABLE=1 tar -czf "$LINUX_ARM64" -C "$FINAL" linux-arm64
COPYFILE_DISABLE=1 tar -czf "$LINUX_ARMV7" -C "$FINAL" linux-armv7
COPYFILE_DISABLE=1 ditto -c -k --norsrc --keepParent "$FINAL" "$LAYOUT_ZIP"

echo "Built release layout: $FINAL"
echo "Built native assets in: $DIST"
